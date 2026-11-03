// Package llm is a small client for Meta's Model API (OpenAI Responses-compatible).
package llm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client

	// The retry layer (retry.go): how many attempts a call gets in all, how
	// long its retries may take, and the backoff between them. Zero means
	// the default; one attempt means no retries.
	Attempts    int
	RetryBudget time.Duration
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	// HeaderTimeout is how long a streaming request may wait for its
	// response headers once it has gone out, and IdleTimeout how long its
	// stream may go without sending anything. A non-streaming request sends
	// nothing until its answer is complete, so it gets CreateTimeout for its
	// headers instead. Any of them running out counts as a stall, which is
	// retried: a half-open connection would otherwise hang for minutes.
	HeaderTimeout time.Duration
	IdleTimeout   time.Duration
	CreateTimeout time.Duration

	// sleep replaces waiting between retries, for tests.
	sleep func(ctx context.Context, d time.Duration) error
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

	// OnRetry hears about each retry before its wait starts, so the user
	// can be told the model is busy instead of watching a spinner.
	OnRetry func(Retry) `json:"-"`
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

// post sends one request and returns the response if its status is a
// success. Any other status comes back as an *APIError with the service's
// message, bounded, and how long it asked to wait. sending is called as the
// request goes out: for each piece of the body the transport takes, and
// once it's all written.
func (c *Client) post(ctx context.Context, path string, body []byte, accept string, sending func()) (*http.Response, error) {
	newBody := func() io.ReadCloser { return io.NopCloser(&progressReader{r: bytes.NewReader(body), tick: sending}) }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, newBody())
	if err != nil {
		return nil, err
	}
	// Not a bytes.Reader any more: say how long it is, and how to send it
	// again on a pooled connection that turned out to be closed.
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) { return newBody(), nil }
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	var tr connTrace
	hooks := tr.hooks()
	if wrote := hooks.WroteRequest; sending != nil {
		hooks.WroteRequest = func(i httptrace.WroteRequestInfo) { wrote(i); sending() }
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), hooks))
	start := time.Now()
	resp, err := c.HTTP.Do(req)
	tr.log(path, start, err)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 300 {
		return resp, nil
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	apiErr := &APIError{Status: resp.StatusCode, Message: bounded(string(data))}
	var wrap struct {
		Error *APIError `json:"error"`
	}
	if json.Unmarshal(data, &wrap) == nil && wrap.Error != nil {
		wrap.Error.Status = resp.StatusCode
		wrap.Error.Message = bounded(wrap.Error.Message)
		apiErr = wrap.Error
	}
	apiErr.RetryAfter = retryHint(resp.StatusCode, resp.Header, apiErr.Message, time.Now())
	slog.Warn("model API returned an error", "path", path, "status", resp.StatusCode, "code", apiErr.Code, "msg", apiErr.Message)
	return nil, apiErr
}

// progressReader is a request body that calls tick each time the transport
// reads more of it, which it does as fast as the upload goes.
type progressReader struct {
	r    io.Reader
	tick func()
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 && p.tick != nil {
		p.tick()
	}
	return n, err
}

// watchdog ends a request that makes no progress for too long as stalled.
// Each feed starts the wait again: while the body goes out, a big request
// on a slow uplink keeps it fed, and one stuck on a half-open connection
// stops feeding it.
type watchdog struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
}

func newWatchdog(ctx context.Context, d time.Duration) *watchdog {
	ctx, cancel := context.WithCancelCause(ctx)
	return &watchdog{ctx: ctx, cancel: cancel, timer: time.AfterFunc(d, func() { cancel(errStalled) })}
}

// feed starts the wait again, for d.
func (w *watchdog) feed(d time.Duration) { w.timer.Reset(d) }

func (w *watchdog) stop() {
	w.timer.Stop()
	w.cancel(nil)
}

// err is errStalled when the watchdog is why the request failed, else err.
func (w *watchdog) err(err error) error {
	if errors.Is(context.Cause(w.ctx), errStalled) {
		return errStalled
	}
	return err
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
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var out *Response
	err = c.call(ctx, req.OnRetry, func(ctx context.Context) error {
		wait := orDefault(c.CreateTimeout, defaultCreateTimeout)
		w := newWatchdog(ctx, wait)
		defer w.stop()
		resp, err := c.post(w.ctx, "/responses", body, "", func() { w.feed(wait) })
		if err != nil {
			return w.err(err)
		}
		defer resp.Body.Close()
		w.feed(orDefault(c.IdleTimeout, defaultIdleTimeout))
		var r Response
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return w.err(err)
		}
		if r.Error != nil {
			// A 200 that carries an error reads like a failed stream.
			e := streamError(r.Error.Code, r.Error.Type, r.Error.Message)
			return e
		}
		out = &r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Stream performs a streaming request, invoking fn for each event, and returns
// the assembled response (output items in order). A retry after a stream
// broke off sends fn the new attempt's events from the start.
func (c *Client) Stream(ctx context.Context, req *Request, fn func(Event)) (*Response, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var out *Response
	err = c.call(ctx, req.OnRetry, func(ctx context.Context) error {
		r, err := c.streamOnce(ctx, body, fn)
		out = r
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// streamOnce makes one streaming request. A watchdog cancels it when the
// headers take longer than HeaderTimeout after the request went out (or
// its upload stops moving for that long), or the stream then goes quiet
// for IdleTimeout, and the request ends as stalled.
func (c *Client) streamOnce(ctx context.Context, body []byte, fn func(Event)) (*Response, error) {
	header, idle := orDefault(c.HeaderTimeout, defaultHeaderTimeout), orDefault(c.IdleTimeout, defaultIdleTimeout)
	watchdog := newWatchdog(ctx, header)
	defer watchdog.stop()
	stalled := watchdog.err

	resp, err := c.post(watchdog.ctx, "/responses", body, "text/event-stream", func() { watchdog.feed(header) })
	if err != nil {
		return nil, stalled(err)
	}
	defer resp.Body.Close()
	watchdog.feed(idle)

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
				e := ev.Response.Error
				return streamError(e.Code, e.Type, e.Message)
			}
			return streamError("", "", "")
		case "error":
			var e struct {
				Code    string    `json:"code"`
				Type    string    `json:"type"`
				Message string    `json:"message"`
				Error   *APIError `json:"error"`
			}
			_ = json.Unmarshal([]byte(payload), &e)
			if e.Error != nil {
				return streamError(e.Error.Code, e.Error.Type, e.Error.Message)
			}
			return streamError(e.Code, e.Type, e.Message)
		}
		if fn != nil {
			fn(ev)
		}
		return nil
	}
	for sc.Scan() {
		watchdog.feed(idle)
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
		return nil, stalled(err)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if final == nil {
		return nil, stalled(errUnfinished)
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
