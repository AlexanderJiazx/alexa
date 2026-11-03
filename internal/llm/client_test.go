package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testClient talks to h and records the waits between retries instead of
// sleeping through them.
func testClient(t *testing.T, h http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "test")
	var mu sync.Mutex
	waits := []time.Duration{}
	c.sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		return ctx.Err()
	}
	return c, &waits
}

func sse(w http.ResponseWriter, ev any) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", b)
	w.(http.Flusher).Flush()
}

// answer streams a completed response with one message.
func answer(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	item := map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}
	sse(w, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
	sse(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
	sse(w, map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "usage": map[string]any{"output_tokens": 3}}})
}

func textOf(r *Response) string {
	var b strings.Builder
	for _, raw := range r.Output {
		b.WriteString(ParseItem(raw).Text())
	}
	return b.String()
}

// A service that keeps answering 503 gets four attempts in all, the screen
// hears of each of the three retries, and the whole thing stays inside the
// two-minute budget (WP-15 acceptance 1).
func TestPersistent503EndsAfterFourAttempts(t *testing.T) {
	var calls atomic.Int32
	c, waits := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `<html><head><title>503 Service Temporarily Unavailable</title></head><body>nginx at /srv/www</body></html>`, 503)
	})
	var retries []Retry
	_, err := c.Stream(context.Background(), &Request{Model: "m", OnRetry: func(r Retry) { retries = append(retries, r) }}, nil)
	if got := calls.Load(); got != 4 {
		t.Fatalf("made %d requests, want 4", got)
	}
	if len(retries) != 3 {
		t.Fatalf("OnRetry was called %d times, want 3", len(retries))
	}
	for i, r := range retries {
		if r.Attempt != i+2 || r.Of != 4 || r.Reason != ReasonUnavailable || r.At.IsZero() {
			t.Errorf("retry %d = %+v", i, r)
		}
	}
	var total time.Duration
	for _, d := range *waits {
		total += d
		if d < time.Second {
			t.Errorf("wait %v is shorter than the floor of 1s", d)
		}
	}
	if total > 2*time.Minute {
		t.Errorf("waited %v in all, more than the budget", total)
	}
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonUnavailable || !e.Retryable || e.Attempts != 4 {
		t.Fatalf("err = %#v, want a retryable model_unavailable after 4 attempts", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 503 || apiErr.Message != "503 Service Temporarily Unavailable" {
		t.Fatalf("the API's answer should be readable through the error, got %#v", apiErr)
	}
}

// The waits are full jitter over a doubling cap: never the same fixed
// ladder, never over the cap.
func TestBackoffJitter(t *testing.T) {
	c := &Client{}
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		for n := 1; n <= 3; n++ {
			d, hinted := c.backoff(n, ReasonUnavailable, errors.New("x"))
			if hinted {
				t.Fatal("no hint was given")
			}
			if limit := time.Second + defaultBaseDelay<<(n-1); d < time.Second || d >= limit {
				t.Errorf("retry %d waits %v, want [1s, %v)", n, d, limit)
			}
			seen[d] = true
		}
	}
	if len(seen) < 100 {
		t.Errorf("only %d distinct waits in 150: not jittered", len(seen))
	}
}

// A 429 that says Retry-After: 7 is waited out for about 7 seconds
// (WP-15 acceptance 2).
func TestRetryAfterHonoured(t *testing.T) {
	var calls atomic.Int32
	c, waits := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached","type":"requests","code":"rate_limit_exceeded"}}`))
			return
		}
		answer(w, "hi")
	})
	var retries []Retry
	resp, err := c.Stream(context.Background(), &Request{OnRetry: func(r Retry) { retries = append(retries, r) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if textOf(resp) != "hi" {
		t.Errorf("text = %q", textOf(resp))
	}
	if len(*waits) != 1 || (*waits)[0] < 7*time.Second || (*waits)[0] > 7*time.Second+250*time.Millisecond {
		t.Fatalf("waits = %v, want one of about 7s", *waits)
	}
	if len(retries) != 1 || retries[0].Reason != ReasonRateLimited || time.Until(retries[0].At) < 6*time.Second {
		t.Errorf("retries = %+v", retries)
	}
}

// A wait longer than the budget isn't sat through: the call fails at once,
// saying when to come back, so the screen can count down.
func TestLongRetryAfterFailsWithRetryAt(t *testing.T) {
	var calls atomic.Int32
	c, waits := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "300")
		http.Error(w, `{"error":{"message":"quota resets soon","code":"rate_limit_exceeded"}}`, 429)
	})
	_, err := c.Stream(context.Background(), &Request{}, nil)
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonRateLimited || !e.Retryable {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 || len(*waits) != 0 {
		t.Errorf("calls = %d, waits = %v: should not have waited", calls.Load(), *waits)
	}
	if left := time.Until(e.RetryAt); left < 290*time.Second || left > 301*time.Second {
		t.Errorf("RetryAt is %v away, want about 300s", left)
	}
}

// A stream that goes quiet is cut off by the watchdog and tried again
// (WP-15 acceptance 3, with the 90s shortened).
func TestStalledStreamRetried(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, map[string]any{"type": "response.created"})
			select { // then nothing, as a half-open connection would
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		answer(w, "back")
	})
	c.IdleTimeout = 150 * time.Millisecond
	var retries []Retry
	var events []string
	resp, err := c.Stream(context.Background(), &Request{OnRetry: func(r Retry) { retries = append(retries, r) }}, func(ev Event) { events = append(events, ev.Type) })
	if err != nil {
		t.Fatal(err)
	}
	if textOf(resp) != "back" || calls.Load() != 2 {
		t.Errorf("text = %q after %d calls", textOf(resp), calls.Load())
	}
	if len(retries) != 1 || retries[0].Reason != ReasonStalled {
		t.Errorf("retries = %+v, want one for a stall", retries)
	}
	if events[0] != "response.created" {
		t.Errorf("events = %v", events)
	}
}

// The retry budget starts at the first failure: an answer that streamed
// for longer than the budget and then went quiet is still retried.
func TestLongStreamThenStallRetried(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "text/event-stream")
			for i := 0; i < 6; i++ { // streams for longer than the budget
				sse(w, map[string]any{"type": "response.output_text.delta", "delta": "."})
				time.Sleep(50 * time.Millisecond)
			}
			<-r.Context().Done()
			return
		}
		answer(w, "back")
	})
	c.RetryBudget = 200 * time.Millisecond
	c.IdleTimeout = 150 * time.Millisecond
	c.BaseDelay = time.Millisecond
	resp, err := c.Stream(context.Background(), &Request{}, nil)
	if err != nil {
		t.Fatalf("the stall after a long stream should be retried: %v", err)
	}
	if textOf(resp) != "back" || calls.Load() != 2 {
		t.Errorf("text = %q after %d calls", textOf(resp), calls.Load())
	}
}

// Headers that never come are a stall too.
func TestHeaderDeadline(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// The server only notices the client leaving once it has read the
		// body.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	})
	c.HeaderTimeout = 100 * time.Millisecond
	c.Attempts = 2
	start := time.Now()
	_, err := c.Stream(context.Background(), &Request{}, nil)
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonStalled || !e.Retryable || e.Attempts != 2 {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

// A request the model refuses is never sent again, and every class of
// failure is named.
func TestClassifiedFailures(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		body   string
		reason string
		retry  bool
	}{
		{"bad request", 400, `{"error":{"message":"Invalid image data","type":"invalid_request_error","param":"input[2].content[1]"}}`, ReasonInvalidInput, false},
		{"overflow", 400, `{"error":{"message":"This model's maximum context length is 128000 tokens","code":"context_length_exceeded"}}`, ReasonContextOverflow, false},
		{"too large", 413, `request entity too large`, ReasonContextOverflow, false},
		{"bad key", 401, `{"error":{"message":"Incorrect API key"}}`, ReasonAuth, false},
		{"quota", 429, `{"error":{"message":"You exceeded your quota","code":"insufficient_quota"}}`, ReasonRateLimited, false},
		{"gateway timeout", 504, `upstream: context deadline exceeded`, ReasonUnavailable, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			cl, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Error(w, c.body, c.status)
			})
			cl.Attempts = 3
			_, err := cl.Stream(context.Background(), &Request{}, nil)
			reason, retry := Classify(err)
			if reason != c.reason || retry != c.retry {
				t.Errorf("Classify = %s, %v; want %s, %v (%v)", reason, retry, c.reason, c.retry, err)
			}
			want := int32(1)
			if c.retry {
				want = 3
			}
			if calls.Load() != want {
				t.Errorf("made %d requests, want %d", calls.Load(), want)
			}
		})
	}
}

// A failure reported inside the stream gets a status from its code: a rate
// limit is waited out, a refused input is not retried, and nothing is
// forced to a 500.
func TestStreamFailures(t *testing.T) {
	for _, c := range []struct {
		name   string
		ev     map[string]any
		reason string
		calls  int32
	}{
		{"rate limit", map[string]any{"type": "response.failed", "response": map[string]any{"error": map[string]any{"code": "rate_limit_exceeded", "message": "Slow down. Please try again in 2s."}}}, ReasonRateLimited, 2},
		{"server error", map[string]any{"type": "response.failed", "response": map[string]any{"error": map[string]any{"code": "server_error", "message": "oops"}}}, ReasonUnavailable, 2},
		{"refused", map[string]any{"type": "response.failed", "response": map[string]any{"error": map[string]any{"code": "invalid_prompt", "message": "Invalid prompt"}}}, ReasonInvalidInput, 1},
		{"error event", map[string]any{"type": "error", "code": "context_length_exceeded", "message": "Your input exceeds the context window of this model"}, ReasonContextOverflow, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			cl, waits := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				sse(w, c.ev)
			})
			cl.Attempts = 2
			_, err := cl.Stream(context.Background(), &Request{}, nil)
			if reason, _ := Classify(err); reason != c.reason {
				t.Errorf("reason = %s, want %s (%v)", reason, c.reason, err)
			}
			if calls.Load() != c.calls {
				t.Errorf("made %d requests, want %d", calls.Load(), c.calls)
			}
			if c.name == "rate limit" && (len(*waits) != 1 || (*waits)[0] < 2*time.Second || (*waits)[0] > 3*time.Second) {
				t.Errorf("waits = %v, want about 2s from the message", *waits)
			}
		})
	}
}

// A stream cut off before it says how the response ended is retried.
func TestUnfinishedStreamRetried(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, map[string]any{"type": "response.created"})
			return
		}
		answer(w, "ok")
	})
	resp, err := c.Stream(context.Background(), &Request{}, nil)
	if err != nil || textOf(resp) != "ok" {
		t.Fatalf("resp = %v, err = %v", resp, err)
	}
}

// Create goes through the same retry layer.
func TestCreateRetries(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "busy", 502)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "summary"}}}}})
	})
	var retries int
	resp, err := c.Create(context.Background(), &Request{OnRetry: func(Retry) { retries++ }})
	if err != nil || textOf(resp) != "summary" || retries != 2 {
		t.Fatalf("resp = %v, err = %v, retries = %d", resp, err, retries)
	}
}

// The caller giving up ends the wait at once with the context's error.
func TestCancelDuringWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "busy", 503) }))
	defer srv.Close()
	c := New(srv.URL, "k")
	c.BaseDelay = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	_, err := c.Stream(ctx, &Request{OnRetry: func(Retry) { cancel() }}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRetryHint(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	h := func(kv ...string) http.Header {
		out := http.Header{}
		for i := 0; i < len(kv); i += 2 {
			out.Set(kv[i], kv[i+1])
		}
		return out
	}
	resets := h("X-Ratelimit-Reset-Requests", "1s", "X-Ratelimit-Reset-Tokens", "6m0s")
	for _, c := range []struct {
		name   string
		status int
		h      http.Header
		msg    string
		want   time.Duration
	}{
		{"seconds", 429, h("Retry-After", "7"), "", 7 * time.Second},
		{"date", 503, h("Retry-After", now.Add(90*time.Second).Format(http.TimeFormat)), "", 90 * time.Second},
		{"ms first", 429, h("Retry-After-Ms", "1500", "Retry-After", "9"), "", 1500 * time.Millisecond},
		{"reset headers", 429, resets, "", 6 * time.Minute},
		{"reset of the spent limit", 429, h("X-Ratelimit-Reset-Requests", "1s", "X-Ratelimit-Remaining-Requests", "0", "X-Ratelimit-Reset-Tokens", "6m0s", "X-Ratelimit-Remaining-Tokens", "30000"), "", time.Second},
		{"reset headers on a 503", 503, resets, "", 0},
		{"message before reset headers", 429, resets, "Rate limit reached for requests. Please try again in 2s.", 2 * time.Second},
		{"message", 429, nil, "Rate limit reached. Please try again in 7s.", 7 * time.Second},
		{"message long", 429, nil, "Your limit resets in 4hr 5min", 4*time.Hour + 5*time.Minute},
		{"message ms", 503, nil, "Please retry after 250ms", 250 * time.Millisecond},
		{"capped", 429, h("Retry-After", "999999"), "", maxHint},
		{"nothing", 500, nil, "Internal error", 0},
	} {
		if got := retryHint(c.status, c.h, c.msg, now); got != c.want {
			t.Errorf("%s: retryHint = %v, want %v", c.name, got, c.want)
		}
	}
}

// A 503 that carries the usual rate-limit reset headers is retried as any
// 503 is, not failed at once with a Try again minutes away (LLM-2).
func TestResetHeadersOn503Retried(t *testing.T) {
	var calls atomic.Int32
	c, waits := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-Ratelimit-Reset-Requests", "1s")
			w.Header().Set("X-Ratelimit-Reset-Tokens", "6m0s")
			http.Error(w, `{"error":{"message":"The server is overloaded","type":"server_error"}}`, 503)
			return
		}
		answer(w, "ok")
	})
	resp, err := c.Stream(context.Background(), &Request{}, nil)
	if err != nil || textOf(resp) != "ok" {
		t.Fatalf("resp = %v, err = %v", resp, err)
	}
	if len(*waits) != 1 || (*waits)[0] > defaultBaseDelay+time.Second {
		t.Errorf("waits = %v, want one backoff", *waits)
	}
}

// The header deadline counts from when the request has gone out: a big
// request on a slow uplink isn't a stall (LLM-4)...
func TestSlowUploadIsNotAStall(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		buf := make([]byte, 64<<10)
		for {
			if _, err := r.Body.Read(buf); err != nil {
				break
			}
			time.Sleep(5 * time.Millisecond) // about 12 MB/s
		}
		answer(w, "ok")
	})
	// The upload takes about 1.6s, well over the header timeout. Once the
	// last of it is handed to the kernel the server still has a couple of
	// megabytes of socket buffers to read (about 0.2s), well under it.
	c.HeaderTimeout = 600 * time.Millisecond
	big, _ := json.Marshal(map[string]any{"role": "user", "content": strings.Repeat("A", 20<<20)})
	resp, err := c.Stream(context.Background(), &Request{Input: []json.RawMessage{big}}, nil)
	if err != nil || textOf(resp) != "ok" || calls.Load() != 1 {
		t.Fatalf("a server that answered once it had the body was taken for stalled after %d calls: %v", calls.Load(), err)
	}
}

// ...but one whose upload stops moving is.
func TestStuckUploadIsAStall(t *testing.T) {
	release := make(chan struct{})
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-release // reads nothing, as a half-open connection would
	})
	t.Cleanup(func() { close(release) }) // before the server closes
	c.HeaderTimeout, c.Attempts = 200*time.Millisecond, 1
	big, _ := json.Marshal(map[string]any{"role": "user", "content": strings.Repeat("A", 32<<20)})
	start := time.Now()
	_, err := c.Stream(context.Background(), &Request{Input: []json.RawMessage{big}}, nil)
	if reason, _ := Classify(err); reason != ReasonStalled {
		t.Fatalf("err = %v, want a stall", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

// A non-streaming call (a compaction summary) that gets no answer is a
// stall too, and tried again, instead of waiting out the client's 15
// minutes (LLM-5).
func TestCreateStallRetried(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "summary"}}}}})
	})
	t.Cleanup(func() { close(release) })
	c.CreateTimeout = 150 * time.Millisecond
	var retries []Retry
	resp, err := c.Create(context.Background(), &Request{OnRetry: func(r Retry) { retries = append(retries, r) }})
	if err != nil || textOf(resp) != "summary" {
		t.Fatalf("resp = %v, err = %v", resp, err)
	}
	if len(retries) != 1 || retries[0].Reason != ReasonStalled {
		t.Errorf("retries = %+v, want one for a stall", retries)
	}
}

// Error text from the service is bounded and readable before it travels
// anywhere: an HTML error page becomes its title, a huge body is cut.
func TestBounded(t *testing.T) {
	if got := bounded("<!DOCTYPE html><html><head><title>502 Bad Gateway</title></head><body><h1>502</h1></body></html>"); got != "502 Bad Gateway" {
		t.Errorf("html = %q", got)
	}
	if got := bounded("<html><body><p>Upstream &amp; down</p></body></html>"); got != "Upstream & down" {
		t.Errorf("html without title = %q", got)
	}
	if got := []rune(bounded(strings.Repeat("x", 5000))); len(got) != 500 {
		t.Errorf("long body kept %d runes", len(got))
	}
}
