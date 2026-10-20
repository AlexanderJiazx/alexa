// Package llm is a small client for Meta's Model API (OpenAI Responses-compatible).
package llm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, key string) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// Several tasks stream at once and the API speaks HTTP/1.1, so keep a
	// pool of warm connections instead of Go's default of 2 idle per host;
	// every new connection pays DNS + TLS, which can take seconds.
	tr.MaxIdleConns = 64
	tr.MaxIdleConnsPerHost = 16
	tr.IdleConnTimeout = 10 * time.Minute
	return &Client{BaseURL: baseURL, APIKey: key, HTTP: &http.Client{Timeout: 15 * time.Minute, Transport: tr}}
}

// KeepWarm resolves and connects to the API right away and then pings it
// periodically, so a user's first request after a quiet spell doesn't pay
// for DNS, TCP and TLS setup.
func (c *Client) KeepWarm(ctx context.Context, every time.Duration) {
	ping := func() {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(rctx, http.MethodGet, c.BaseURL+"/models", nil)
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		var tr connTrace
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), tr.hooks()))
		start := time.Now()
		resp, err := c.HTTP.Do(req)
		tr.log("/models (warm-up)", start, err)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}
	ping()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ping()
		}
	}
}

type Reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type Request struct {
	Model             string            `json:"model"`
	Instructions      string            `json:"instructions,omitempty"`
	Input             []json.RawMessage `json:"input"`
	Tools             []any             `json:"tools,omitempty"`
	Reasoning         *Reasoning        `json:"reasoning,omitempty"`
	Include           []string          `json:"include,omitempty"`
	Store             bool              `json:"store"`
	Stream            bool              `json:"stream"`
	MaxOutputTokens   int               `json:"max_output_tokens,omitempty"`
	ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
	Text              any               `json:"text,omitempty"`
}

type Usage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	TotalTokens        int64 `json:"total_tokens"`
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("model API error %d (%s): %s", e.Status, e.Type, e.Message)
}

type Response struct {
	ID                string            `json:"id"`
	Status            string            `json:"status"`
	Output            []json.RawMessage `json:"output"`
	Usage             Usage             `json:"usage"`
	Error             *APIError         `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

// Event is a streaming event; only the fields we use are decoded.
type Event struct {
	Type        string          `json:"type"`
	ItemID      string          `json:"item_id"`
	OutputIndex int             `json:"output_index"`
	Delta       string          `json:"delta"`
	Item        json.RawMessage `json:"item"`
	Response    *Response       `json:"response"`
	Message     string          `json:"message"`
}

// Item is a loose view of an output item.
type Item struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Role      string `json:"role"`
	Phase     string `json:"phase"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
	Status    string `json:"status"`
	Content   []struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Annotations []struct {
			Type  string `json:"type"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"annotations"`
	} `json:"content"`
	Action *struct {
		Type    string   `json:"type"`
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		URL     string   `json:"url"`
		Pattern string   `json:"pattern"`
	} `json:"action"`
	Summary []struct {
		Text string `json:"text"`
	} `json:"summary"`
}

func ParseItem(raw json.RawMessage) Item {
	var it Item
	_ = json.Unmarshal(raw, &it)
	return it
}

// Text concatenates output_text parts of a message item.
func (it Item) Text() string {
	var b strings.Builder
	for _, c := range it.Content {
		if c.Type == "output_text" || c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func retryable(status int) bool {
	return status == 429 || status == 408 || status >= 500
}

func (c *Client) post(ctx context.Context, path string, body any, accept string) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			d := time.Duration(1<<attempt)*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(buf))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		var tr connTrace
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), tr.hooks()))
		start := time.Now()
		resp, err := c.HTTP.Do(req)
		tr.log(path, start, err)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			slog.Warn("model API request failed, retrying", "err", err, "attempt", attempt)
			continue
		}
		if resp.StatusCode >= 300 {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			apiErr := &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
			var wrap struct {
				Error *APIError `json:"error"`
			}
			if json.Unmarshal(data, &wrap) == nil && wrap.Error != nil {
				wrap.Error.Status = resp.StatusCode
				apiErr = wrap.Error
			}
			lastErr = apiErr
			if retryable(resp.StatusCode) {
				slog.Warn("model API returned retryable status", "status", resp.StatusCode, "msg", apiErr.Message, "attempt", attempt)
				continue
			}
			return nil, apiErr
		}
		return resp, nil
	}
	return nil, lastErr
}

// connTrace records how long each phase of a request took, so slow
// responses can be blamed on DNS, connecting, TLS or the API itself.
type connTrace struct {
	dnsStart, dnsDone, connStart, connDone, tlsStart, tlsDone, wrote, firstByte time.Time
	reused                                                                      bool
}

func (t *connTrace) hooks() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GotConn:              func(i httptrace.GotConnInfo) { t.reused = i.Reused },
		DNSStart:             func(httptrace.DNSStartInfo) { t.dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { t.dnsDone = time.Now() },
		ConnectStart:         func(string, string) { t.connStart = time.Now() },
		ConnectDone:          func(string, string, error) { t.connDone = time.Now() },
		TLSHandshakeStart:    func() { t.tlsStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.tlsDone = time.Now() },
		WroteRequest:         func(httptrace.WroteRequestInfo) { t.wrote = time.Now() },
		GotFirstResponseByte: func() { t.firstByte = time.Now() },
	}
}

func (t *connTrace) log(path string, start time.Time, err error) {
	span := func(a, b time.Time) time.Duration {
		if a.IsZero() || b.IsZero() {
			return 0
		}
		return b.Sub(a).Round(time.Millisecond)
	}
	total := time.Since(start).Round(time.Millisecond)
	level := slog.LevelDebug
	if total > 5*time.Second {
		level = slog.LevelWarn
	}
	slog.Log(context.Background(), level, "model API response headers", "path", path, "total", total,
		"reused_conn", t.reused, "dns", span(t.dnsStart, t.dnsDone), "connect", span(t.connStart, t.connDone),
		"tls", span(t.tlsStart, t.tlsDone), "server_wait", span(t.wrote, t.firstByte), "err", err)
}

// Create performs a non-streaming request.
func (c *Client) Create(ctx context.Context, req *Request) (*Response, error) {
	req.Stream = false
	resp, err := c.post(ctx, "/responses", req, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return &out, out.Error
	}
	return &out, nil
}

// Stream performs a streaming request, invoking fn for each event, and returns
// the assembled response (output items in order).
func (c *Client) Stream(ctx context.Context, req *Request, fn func(Event)) (*Response, error) {
	req.Stream = true
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		out, err := c.streamOnce(ctx, req, fn)
		if err == nil {
			return out, nil
		}
		lastErr = err
		var apiErr *APIError
		if ctx.Err() != nil || (errors.As(err, &apiErr) && !retryable(apiErr.Status)) {
			return nil, err
		}
		slog.Warn("stream failed, retrying", "err", err, "attempt", attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(2<<attempt) * time.Second):
		}
	}
	return nil, lastErr
}

func (c *Client) streamOnce(ctx context.Context, req *Request, fn func(Event)) (*Response, error) {
	resp, err := c.post(ctx, "/responses", req, "text/event-stream")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	items := map[int]json.RawMessage{}
	maxIdx := -1
	var final *Response
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		payload := data.String()
		data.Reset()
		if payload == "[DONE]" {
			return nil
		}
		var ev Event
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return nil
		}
		switch ev.Type {
		case "response.output_item.done":
			items[ev.OutputIndex] = ev.Item
			if ev.OutputIndex > maxIdx {
				maxIdx = ev.OutputIndex
			}
		case "response.completed", "response.incomplete":
			final = ev.Response
		case "response.failed":
			if ev.Response != nil && ev.Response.Error != nil {
				ev.Response.Error.Status = 500
				return ev.Response.Error
			}
			return &APIError{Status: 500, Message: "response failed"}
		case "error":
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal([]byte(payload), &e)
			return &APIError{Status: 500, Code: e.Code, Message: e.Message}
		}
		if fn != nil {
			fn(ev)
		}
		return nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if final == nil {
		return nil, errors.New("stream ended without completion")
	}
	// Prefer the items assembled from output_item.done events: completed
	// payloads may omit encrypted reasoning.
	if maxIdx >= 0 {
		out := make([]json.RawMessage, 0, maxIdx+1)
		for i := 0; i <= maxIdx; i++ {
			if it, ok := items[i]; ok {
				out = append(out, it)
			}
		}
		final.Output = out
	}
	return final, nil
}

// Quick runs a cheap single-shot completion and returns the text.
func (c *Client) Quick(ctx context.Context, model, instructions, input string) (string, error) {
	msg, _ := json.Marshal(map[string]any{"role": "user", "content": input})
	resp, err := c.Create(ctx, &Request{
		Model:        model,
		Instructions: instructions,
		Input:        []json.RawMessage{msg},
		Reasoning:    &Reasoning{Effort: "minimal"},
		Store:        false,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, raw := range resp.Output {
		it := ParseItem(raw)
		if it.Type == "message" {
			b.WriteString(it.Text())
		}
	}
	return strings.TrimSpace(b.String()), nil
}
