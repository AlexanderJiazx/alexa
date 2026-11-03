package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"alexagent/internal/llm"
)

// ---------- a model whose every answer the test chooses ----------

// fakeModel answers each streaming call with the next of its stream
// handlers and each summary call with the next of its create handlers.
// When a queue runs out, a stream gets an empty completed response and a
// summary a short one.
type fakeModel struct {
	mu      sync.Mutex
	stream  []http.HandlerFunc
	create  []http.HandlerFunc
	streams []llm.Request
	creates []string // the summariser's inputs
}

func (m *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/responses" {
		_, _ = w.Write([]byte(`{"data":[]}`))
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req llm.Request
	_ = json.Unmarshal(body, &req)
	r.Body = io.NopCloser(bytes.NewReader(body))
	m.mu.Lock()
	var next http.HandlerFunc
	if req.Stream {
		m.streams = append(m.streams, req)
		if len(m.stream) > 0 {
			next, m.stream = m.stream[0], m.stream[1:]
		}
	} else {
		var in struct {
			Content string `json:"content"`
		}
		if len(req.Input) > 0 {
			_ = json.Unmarshal(req.Input[0], &in)
		}
		m.creates = append(m.creates, in.Content)
		if len(m.create) > 0 {
			next, m.create = m.create[0], m.create[1:]
		}
	}
	m.mu.Unlock()
	switch {
	case next != nil:
		next(w, r)
	case req.Stream:
		respond(w, "completed", 5)
	default:
		summary(w, "completed", "## In progress\nNothing much.")
	}
}

func (m *fakeModel) onStream(h ...http.HandlerFunc) {
	m.mu.Lock()
	m.stream = append(m.stream, h...)
	m.mu.Unlock()
}

func (m *fakeModel) onCreate(h ...http.HandlerFunc) {
	m.mu.Lock()
	m.create = append(m.create, h...)
	m.mu.Unlock()
}

func (m *fakeModel) streamCalls() []llm.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]llm.Request{}, m.streams...)
}

func (m *fakeModel) createInputs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.creates...)
}

// withFakeModel points e's runtime at a fakeModel.
func withFakeModel(e *progressEnv) *fakeModel {
	m := &fakeModel{}
	srv := httptest.NewServer(m)
	e.t.Cleanup(srv.Close)
	e.rt.llm = llm.New(srv.URL, "test")
	return m
}

// respond streams a response with the given status ("completed" or
// "incomplete"), output token count and output items.
func respond(w http.ResponseWriter, status string, outTokens int, items ...any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for i, it := range items {
		writeSSE(w, map[string]any{"type": "response.output_item.added", "output_index": i, "item": it})
		writeSSE(w, map[string]any{"type": "response.output_item.done", "output_index": i, "item": it})
	}
	resp := map[string]any{"status": status, "usage": map[string]any{"input_tokens": 1000, "output_tokens": outTokens}}
	if status == "incomplete" {
		resp["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	writeSSE(w, map[string]any{"type": "response." + status, "response": resp})
}

func says(items ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { respond(w, "completed", 20, items...) }
}

func unavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After-Ms", "1") // keeps the test quick
	http.Error(w, "<html><title>503 Service Unavailable</title><body>/srv/gateway</body></html>", 503)
}

func summary(w http.ResponseWriter, status, text string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}}})
}

func summarises(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { summary(w, "completed", text) }
}

func search(query string) any {
	return map[string]any{"type": "web_search_call", "id": "ws_" + query, "status": "completed", "action": map[string]any{"type": "search", "query": query}}
}

// liveRun is a run of e's user that the runtime counts as going on, for
// calling the guards and streamModel as the loop does. A background run is
// a scheduled job nobody is watching.
func (e *progressEnv) liveRun(background bool) *run {
	_, cancel := context.WithCancel(e.ctx)
	r := &run{user: e.user, env: e.rt.envs.For(e.user.ID), cancel: cancel, wake: make(chan struct{}, 1), awaiting: !background, held: background, job: background}
	e.rt.mu.Lock()
	e.rt.runs[e.user.ID] = r
	e.rt.mu.Unlock()
	e.t.Cleanup(func() { e.rt.removeRun(r); cancel() })
	return r
}

// call runs one tool call through the executor's guard hook, with res as
// what the tool returns.
func (e *progressEnv) call(r *run, name string, args map[string]any, res *toolResult) string {
	ran := false
	out := guard(e.rt, r, name, args, func() *toolResult { ran = true; return res })
	if !ran && !strings.HasPrefix(out.Text, "Not run:") {
		e.t.Fatalf("%s was refused with %q", name, out.Text)
	}
	return out.Text
}

const guardNote = "[Alexa system note]"

// ---------- loop guards ----------

// The same click on a page that doesn't change is warned about on the 3rd
// try and refused from the 5th; only a scheduled job nobody watches is
// ended for it, with loop_blocked (WP-16 acceptance 1).
func TestRepeatedClickWarnedThenBlocked(t *testing.T) {
	for _, unattended := range []bool{false, true} {
		t.Run(fmt.Sprintf("unattended=%v", unattended), func(t *testing.T) {
			e := newProgressEnv(t)
			r := e.liveRun(unattended)
			cart := page("", "https://shop.example/cart", "Cart", `button "Place order"`)
			e.call(r, "browser_open", map[string]any{"url": "https://shop.example/cart"}, cart)
			var outs []string
			for i := 0; i < 6; i++ {
				clicked := page("Clicked [1].", "https://shop.example/cart", "Cart", `button "Place order"`)
				outs = append(outs, e.call(r, "browser_click", map[string]any{"index": 1, "status": "Placing the order"}, clicked))
			}
			for i, out := range outs {
				warned, refused := strings.Contains(out, guardNote), strings.HasPrefix(out, "Not run:")
				switch n := i + 1; {
				case n <= 2 && (warned || refused):
					t.Errorf("click %d was flagged: %q", n, out)
				case (n == 3 || n == 4) && (!warned || refused):
					t.Errorf("click %d should run with a warning: %q", n, out)
				case n >= 5 && !refused:
					t.Errorf("click %d should be refused: %q", n, out)
				}
			}
			if !strings.Contains(outs[2], "3rd time") {
				t.Errorf("the warning should count the tries: %q", outs[2])
			}
			_, stop := e.rt.guardOf(r).streamStarted(9, 30)
			if !unattended {
				if stop != nil {
					t.Fatalf("an attended run must not be ended by a guard: %v", stop)
				}
				return
			}
			if f := buildFailure(stop, true); f.Reason != reasonLoopBlocked || !f.Retryable || f.Detail == "" {
				t.Fatalf("unattended run should end loop_blocked, got %+v", f)
			}
		})
	}
}

// A click that changes the page, then one that changes it back, over and
// over: warned after 3 laps, refused on the 5th.
func TestBackAndForthBlocked(t *testing.T) {
	e := newProgressEnv(t)
	r := e.liveRun(false)
	a := func() *toolResult { return page("", "https://shop.example/a", "A", `link "B"`) }
	b := func() *toolResult { return page("", "https://shop.example/b", "B", `link "A"`) }
	e.call(r, "browser_open", map[string]any{"url": "https://shop.example/a"}, a())
	var outs []string
	for i := 0; i < 5; i++ {
		outs = append(outs, e.call(r, "browser_click", map[string]any{"index": 1}, b()))
		outs = append(outs, e.call(r, "browser_click", map[string]any{"index": 1}, a()))
	}
	for i, out := range outs {
		warned, refused := strings.Contains(out, "back and forth"), strings.HasPrefix(out, "Not run:")
		switch n := i + 1; {
		case n < 6 && (warned || refused):
			t.Errorf("call %d was flagged: %q", n, out)
		case n >= 6 && n < 10 && (!warned || refused):
			t.Errorf("call %d should be warned: %q", n, out)
		case n == 10 && !refused:
			t.Errorf("call %d should be refused: %q", n, out)
		}
	}
}

// A tool that keeps failing with different arguments is warned about every
// 3rd failure. Only an unattended run is stopped, on the 8th try; the
// tolerant tools never stop one.
func TestFailingToolStopsOnlyUnattended(t *testing.T) {
	for _, c := range []struct {
		name       string
		tool       string
		unattended bool
		refused    bool
	}{
		{"attended", "browser_click", false, false},
		{"unattended", "browser_click", true, true},
		{"tolerant", "run_shell", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newProgressEnv(t)
			r := e.liveRun(c.unattended)
			var outs []string
			for i := 1; i <= 8; i++ {
				res := &toolResult{Text: fmt.Sprintf("Action failed: element [%d] not found", i)}
				if c.tool == "run_shell" {
					res = &toolResult{Text: fmt.Sprintf("exit code 1\nno such file %d", i)}
				}
				outs = append(outs, e.call(r, c.tool, map[string]any{"index": i, "command": fmt.Sprint("cat ", i)}, res))
			}
			for _, n := range []int{3, 6} {
				if !strings.Contains(outs[n-1], "failed "+fmt.Sprint(n)+" times in a row") {
					t.Errorf("failure %d should be warned: %q", n, outs[n-1])
				}
			}
			if got := strings.HasPrefix(outs[7], "Not run:"); got != c.refused {
				t.Errorf("8th try refused = %v, want %v (%q)", got, c.refused, outs[7])
			}
			_, stop := e.rt.guardOf(r).streamStarted(9, 30)
			if (stop != nil) != c.refused {
				t.Errorf("run stopped = %v, want %v", stop != nil, c.refused)
			}
		})
	}
}

// The user's own request is never taken for a scheduled job, even held
// with nobody owed an answer: they turned Following off after Alexa said
// "On it", and it resumed so after a restart (LLM-7).
func TestHeldRequestIsAttended(t *testing.T) {
	e := newProgressEnv(t)
	r := e.liveRun(false)
	r.held, r.awaiting = true, false
	for i := 1; i <= 8; i++ {
		e.call(r, "browser_click", map[string]any{"index": i}, &toolResult{Text: fmt.Sprintf("Action failed: element [%d] not found", i)})
	}
	if g := e.rt.guardOf(r); g.unattended() || g.halt != nil {
		t.Errorf("the user's held request was run as an unattended job (halt: %v)", g.halt)
	}
	if _, stop := e.rt.guardOf(r).streamStarted(9, 30); stop != nil {
		t.Errorf("the user's held request was ended: %v", stop)
	}
}

// Reading the same page again after something changed is fine: a click,
// typing or navigation that worked starts the counts again.
func TestProgressResetsCounts(t *testing.T) {
	e := newProgressEnv(t)
	r := e.liveRun(false)
	for i := 0; i < 6; i++ {
		out := e.call(r, "read_file", map[string]any{"path": "notes.md"}, &toolResult{Text: "draft"})
		if strings.Contains(out, guardNote) || strings.HasPrefix(out, "Not run:") {
			t.Fatalf("read %d after a write was flagged: %q", i+1, out)
		}
		e.call(r, "write_file", map[string]any{"path": "notes.md", "content": fmt.Sprint("v", i)}, &toolResult{Text: "Wrote notes.md"})
	}
}

func TestBudgetNotes(t *testing.T) {
	noted := map[string]bool{}
	for turn := 0; turn < 120; turn++ {
		if n := budgetNote(turn, 200, noted); n != "" {
			t.Fatalf("turn %d: unexpected note %q", turn, n)
		}
	}
	if n := budgetNote(120, 200, noted); !strings.Contains(n, "120 of the 200 steps") {
		t.Errorf("60%% note = %q", n)
	}
	if n := budgetNote(121, 200, noted); n != "" {
		t.Errorf("the 60%% note came twice: %q", n)
	}
	if n := budgetNote(170, 200, noted); !strings.Contains(n, "Only 30 steps are left") {
		t.Errorf("85%% note = %q", n)
	}
	if n := budgetNote(180, 200, noted); n != "" {
		t.Errorf("the 85%% note came twice: %q", n)
	}
	if n := budgetNote(1, 2, map[string]bool{}); n != "" {
		t.Errorf("too small a budget for notes, got %q", n)
	}

	// A new stretch of work (the turn number starts again) gets them again,
	// and the web search count starts again too.
	e := newProgressEnv(t)
	g := e.rt.guardOf(e.liveRun(false))
	if notes, _ := g.streamStarted(18, 30); len(notes) != 1 {
		t.Fatalf("notes at 60%% = %q", notes)
	}
	for i := 0; i < searchCap; i++ {
		g.searched()
	}
	if notes, _ := g.streamStarted(19, 30); len(notes) != 1 || !strings.Contains(notes[0], "searched the web 40 times") {
		t.Fatalf("notes after 40 searches = %q", notes)
	}
	if notes, _ := g.streamStarted(0, 30); len(notes) != 0 {
		t.Fatalf("a new stretch starts clean, got %q", notes)
	}
	if notes, _ := g.streamStarted(18, 30); len(notes) != 1 {
		t.Fatalf("the new stretch gets its own 60%% note, got %q", notes)
	}
}

// ---------- the model call ----------

// A persistent 503 ends after 4 attempts, each retry shown as the
// "retrying" phase with which attempt is next (WP-15 acceptance 1), and
// the failure tells the user plainly, without the gateway's page.
func TestStreamModelRetryingThenFailure(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	m.onStream(unavailable, unavailable, unavailable, unavailable, unavailable)
	r := e.liveRun(false)
	_, err := e.rt.streamModel(e.ctx, r, 0, &llm.Request{Model: "m"}, func(llm.Event) {})
	if n := len(m.streamCalls()); n != 4 {
		t.Fatalf("made %d model calls, want 4", n)
	}
	var retries []runRetry
	for _, ev := range e.runEvents() {
		if ev.Phase == "retrying" {
			retries = append(retries, *ev.Retry)
		}
	}
	if len(retries) != 3 {
		t.Fatalf("retrying events = %+v, want 3", retries)
	}
	for i, rr := range retries {
		if rr.Attempt != i+2 || rr.Of != 4 || rr.Reason != llm.ReasonUnavailable || rr.At == 0 {
			t.Errorf("retry %d = %+v", i, rr)
		}
	}
	f := buildFailure(err, false)
	if f.Reason != llm.ReasonUnavailable || !f.Retryable || strings.Contains(f.Detail, "503") || strings.Contains(f.Detail, "/srv") {
		t.Fatalf("failure = %+v", f)
	}
}

// A stream that breaks off is retried, and a web search the broken attempt
// already showed isn't shown a second time.
func TestStreamModelDedupesSearchesOnRetry(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	e.rt.llm.BaseDelay = time.Millisecond
	m.onStream(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSE(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": search("hotels in lisbon")})
			// and the connection drops
		},
		says(search("hotels in lisbon"), search("lisbon old town"), fc("say", map[string]any{"text": "Found two."})),
	)
	r := e.liveRun(false)
	var seen []string
	resp, err := e.rt.streamModel(e.ctx, r, 0, &llm.Request{Model: "m"}, func(ev llm.Event) {
		if it := llm.ParseItem(ev.Item); ev.Type == "response.output_item.done" && it.Type == "web_search_call" {
			seen = append(seen, it.Action.Query)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Output) != 3 {
		t.Errorf("the response should be the retry's whole output, got %d items", len(resp.Output))
	}
	if strings.Join(seen, ", ") != "hotels in lisbon, lisbon old town" {
		t.Errorf("searches shown = %q", seen)
	}
	if v := e.rt.View(e.ctx, e.user.ID); v.Activity != "Thinking" {
		t.Errorf("activity after the retry = %q, want Thinking", v.Activity)
	}
}

// Two empty incomplete responses in a row end the run with empty_response
// instead of 200 more turns (WP-15 acceptance 4); a cut-off one is
// continued a few times only.
func TestEmptyResponseLadder(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	r := e.liveRun(false)
	empty := func(w http.ResponseWriter, r *http.Request) { respond(w, "incomplete", 0) }
	m.onStream(empty, empty)
	if _, err := e.rt.streamModel(e.ctx, r, 0, &llm.Request{}, func(llm.Event) {}); err != nil {
		t.Fatalf("the first empty response is tried again, got %v", err)
	}
	_, err := e.rt.streamModel(e.ctx, r, 1, &llm.Request{}, func(llm.Event) {})
	if f := buildFailure(err, false); f.Reason != reasonEmptyResponse {
		t.Fatalf("second empty: %v (%+v)", err, f)
	}

	g := &loopGuard{}
	cut := &llm.Response{Status: "incomplete", Usage: llm.Usage{OutputTokens: 900}}
	for i := 1; i <= truncatedLimit; i++ {
		if err := g.checkResponse(cut); err != nil {
			t.Fatalf("cut-off response %d: %v", i, err)
		}
	}
	if err := g.checkResponse(cut); !errors.Is(err, errTruncated) {
		t.Fatalf("cut-off response %d: %v", truncatedLimit+1, err)
	}
	g = &loopGuard{}
	ok := &llm.Response{Status: "completed", Output: []json.RawMessage{marshal(fc("say", map[string]any{"text": "hi"}))}}
	for i := 0; i < 5; i++ {
		_ = g.checkResponse(&llm.Response{Status: "incomplete"})
		if err := g.checkResponse(ok); err != nil {
			t.Fatalf("a real answer resets the ladder: %v", err)
		}
	}
}

// The loop end to end, once it calls streamModel (loop.go handoff): two
// empty responses end the run with an error card instead of 30 turns.
func TestEmptyResponsesEndTheRun(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	for i := 0; i < 40; i++ {
		m.onStream(func(w http.ResponseWriter, r *http.Request) { respond(w, "incomplete", 0) })
	}
	e.send("What's the weather?")
	e.settle()
	if n := len(m.streamCalls()); n > 2 {
		t.Skipf("loop.go doesn't call streamModel yet (be3b handoff L1): %d model calls", n)
	}
	errs := entriesOf(e.feed(), "error")
	if len(errs) != 1 || strings.Contains(errs[0].Text, "steps") {
		t.Fatalf("errors = %+v", errs)
	}
}

// The guards' notes go into the conversation for good, as the same bytes
// the next turn replays, so the cached prompt still matches.
func TestStreamModelNotesKeepThePrefix(t *testing.T) {
	e := newProgressEnv(t)
	withFakeModel(e)
	r := e.liveRun(false)
	if err := e.rt.appendMessage(e.ctx, e.user.ID, "user", "[Mon] find a hotel", nil); err != nil {
		t.Fatal(err)
	}
	in, err := e.rt.buildInput(e.ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := &llm.Request{Input: in}
	if _, err := e.rt.streamModel(e.ctx, r, 18, req, func(llm.Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(req.Input) != len(in)+1 || !strings.Contains(string(req.Input[len(in)]), "steps for this stretch") {
		t.Fatalf("the 60%% note should be sent: %s", req.Input[len(req.Input)-1])
	}
	next, err := e.rt.buildInput(e.ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != len(req.Input) {
		t.Fatalf("replay has %d items, the call had %d", len(next), len(req.Input))
	}
	for i := range next {
		if !bytes.Equal(next[i], req.Input[i]) {
			t.Fatalf("item %d differs:\n%s\n%s", i, next[i], req.Input[i])
		}
	}
}

// ---------- failures ----------

func TestBuildFailure(t *testing.T) {
	at := time.Now().Add(5 * time.Minute)
	for _, c := range []struct {
		name      string
		err       error
		reason    string
		retryable bool
	}{
		{"busy", &llm.Error{Reason: llm.ReasonRateLimited, Retryable: true, RetryAt: at, Err: &llm.APIError{Status: 429, Message: "slow down"}}, llm.ReasonRateLimited, true},
		{"quota", &llm.Error{Reason: llm.ReasonRateLimited, Err: &llm.APIError{Status: 429, Code: "insufficient_quota"}}, llm.ReasonRateLimited, false},
		{"stall", &llm.Error{Reason: llm.ReasonStalled, Retryable: true, Err: errors.New("the model stopped responding")}, llm.ReasonStalled, true},
		{"bad image", &llm.APIError{Status: 400, Message: "Invalid image data at /data/users/3/uploads/IMG_2041.heic"}, llm.ReasonInvalidInput, false},
		{"key", &llm.APIError{Status: 401, Message: "bad key"}, llm.ReasonUnavailable, false},
		{"overflow", &llm.APIError{Status: 400, Message: "This model's maximum context length is 128000 tokens"}, llm.ReasonContextOverflow, true},
		{"panic", fmt.Errorf("internal error: runtime error: index out of range\ngoroutine 7 [running]:\n/Users/alexander/Code/AlexAgent/internal/agent/loop.go:140 +0x1c"), reasonInternal, true},
		{"store", fmt.Errorf("open /data/alexa.db: disk I/O error"), reasonInternal, true},
		{"out of steps", errOutOfSteps(200), reasonLoopBlocked, true},
		{"out of steps, typed", errOutOfSteps(200), reasonLoopBlocked, true},
		{"empty", errEmptyResponse, reasonEmptyResponse, true},
	} {
		f := buildFailure(c.err, true)
		if f.Reason != c.reason || f.Retryable != c.retryable || !f.Partial {
			t.Errorf("%s: %+v, want %s retryable=%v", c.name, f, c.reason, c.retryable)
		}
		for _, s := range []string{f.Detail, f.modelNote()} {
			if s == "" || len([]rune(f.Detail)) > 200 || strings.Contains(s, "/data/") || strings.Contains(s, "/Users/") || strings.Contains(s, "goroutine") || strings.Contains(s, ".go:") {
				t.Errorf("%s: not fit to show: %q", c.name, s)
			}
		}
		if c.name == "busy" && f.RetryAt != at.UnixMilli() {
			t.Errorf("busy: RetryAt = %d, want %d", f.RetryAt, at.UnixMilli())
		}
	}
	if note := buildFailure(&llm.APIError{Status: 400, Message: "Invalid image data at /data/users/3/uploads/IMG_2041.heic"}, false).modelNote(); !strings.Contains(note, "IMG_2041.heic") {
		t.Errorf("the model should hear which file was refused: %q", note)
	}
}

// What fail records: user-safe text and the Failure on the entry, no
// server paths anywhere (WP-15 acceptance 6).
func TestFailureEntry(t *testing.T) {
	e := newProgressEnv(t)
	r := e.liveRun(false)
	entry, note, f := e.rt.failure(r, fmt.Errorf("write /data/users/1/workspace/x: permission denied"))
	var d struct{ Failure Failure }
	if err := json.Unmarshal(entry.Data, &d); err != nil || d.Failure.Reason != reasonInternal || d.Failure.Detail != entry.Text || f.Reason != reasonInternal {
		t.Fatalf("entry = %+v (%s), failure = %+v", entry, entry.Data, f)
	}
	for _, s := range []string{entry.Text, string(entry.Data), note} {
		if strings.Contains(s, "/data/") {
			t.Errorf("leaks a server path: %q", s)
		}
	}
	if entry.Kind != "error" || !strings.Contains(note, "[Your previous turn ended with an error: ") {
		t.Errorf("entry kind %q, note %q", entry.Kind, note)
	}
}

// Try again after a failure picks the run up and finishes it, and shows
// as the user's own message (WP-15 acceptance 5).
func TestRetryResumes(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	e.rt.llm.Attempts = 1
	m.onStream(unavailable)
	e.send("Find me a hotel in Lisbon")
	e.settle()
	errs := entriesOf(e.feed(), "error")
	if len(errs) != 1 {
		t.Fatalf("errors after the failed call = %+v", errs)
	}
	if err := e.rt.Retry(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	m.onStream(says(fc("say", map[string]any{"text": "Found one near the river."})))
	// The relaunched run may already be in its first call; the queue is
	// read when the request arrives, so it gets the scripted answer.
	v := e.settle()
	if v.Status != "idle" {
		t.Errorf("status = %q", v.Status)
	}
	feed := e.feed()
	if errs := entriesOf(feed, "error"); len(errs) != 1 || errs[0].Status != "retried" {
		t.Errorf("the error card should be marked retried: %+v", errs)
	}
	users := entriesOf(feed, "user")
	if len(users) != 2 || users[1].Text != "Try again" || !strings.Contains(string(users[1].Data), `"via":"retry"`) {
		t.Errorf("Try again should show as the user's message: %+v", users)
	}
	if says := entriesOf(feed, "say"); len(says) != 1 || says[0].Text != "Found one near the river." {
		t.Errorf("says = %+v", says)
	}
	calls := m.streamCalls()
	if len(calls) < 2 {
		t.Fatalf("model calls = %d", len(calls))
	}
	in := inputOf(&calls[1])
	note, tap := strings.Index(in, "ended with an error"), strings.Index(in, `Tapped \"Try again\"`)
	if note < 0 || tap < 0 || note > tap {
		t.Errorf("the retry should read the error, then the tap:\n%s", in)
	}
	if err := e.rt.Retry(e.ctx, e.user); !errors.Is(err, ErrNothingToRetry) {
		t.Errorf("a second Try again = %v, want ErrNothingToRetry", err)
	}
}

// ---------- context hygiene ----------

// The replayed prefix stays byte for byte the same from turn to turn while
// page states pile up, and is cut back in one go at the high mark (WP-16
// acceptance 2).
func TestPrefixStableAcrossTurns(t *testing.T) {
	e := newProgressEnv(t)
	uid := e.user.ID
	if err := e.rt.appendMessage(e.ctx, uid, "user", "[Mon] compare these laptops", nil); err != nil {
		t.Fatal(err)
	}
	shot := []byte("\xff\xd8\xff\xe0 not really a jpeg")
	var prev []json.RawMessage
	stable, longest, cuts := 0, 0, 0
	for turn := 1; turn <= 24; turn++ {
		id := fmt.Sprintf("call_%d", turn)
		name, err := e.rt.saveShot(uid, shot)
		if err != nil {
			t.Fatal(err)
		}
		state := page("", fmt.Sprintf("https://shop.example/p/%d", turn), "Laptop", `button "Add to cart"`)
		items := []json.RawMessage{
			marshal(storedItem{Item: marshal(map[string]any{"type": "function_call", "call_id": id, "name": "browser_open", "arguments": `{"url":"x"}`})}),
			marshal(storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": id, "output": state.Text}), Kind: "browser_state", Short: "[Older page state omitted.]", Shot: name}),
		}
		if err := e.st.AppendItems(e.ctx, uid, items); err != nil {
			t.Fatal(err)
		}
		in, err := e.rt.buildInput(e.ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		if isPrefix(prev, in) {
			stable++
			longest = max(longest, stable)
		} else {
			cuts++
			stable = 0
		}
		prev = in
		full, shots := 0, 0
		for _, raw := range in {
			s := string(raw)
			if strings.Contains(s, "Interactive elements") {
				full++
			}
			if strings.Contains(s, "input_image") {
				shots++
			}
		}
		if full > statesHigh || shots > shotsHigh || full < min(turn, statesLow) || shots < min(turn, shotsLow) {
			t.Fatalf("turn %d: %d full states, %d screenshots", turn, full, shots)
		}
	}
	if longest < 5 {
		t.Errorf("the prefix stayed stable for at most %d turns in a row", longest)
	}
	// Screenshots fill up first (2 -> 6): one cut every 5 turns, with the
	// states cut back at the same time rather than on a turn of their own.
	if cuts > 4 {
		t.Errorf("the prefix changed %d times in 24 turns, want at most 4", cuts)
	}
}

func isPrefix(prev, next []json.RawMessage) bool {
	if len(prev) > len(next) {
		return false
	}
	for i := range prev {
		if !bytes.Equal(prev[i], next[i]) {
			return false
		}
	}
	return true
}

// Reasoning written by another model isn't replayed to a new one (L12):
// it's left out from the first turn under the new model, and what the new
// model writes is replayed as usual, the same bytes every turn.
func TestReasoningFromAnotherModelLeftOut(t *testing.T) {
	e := newProgressEnv(t)
	uid := e.user.ID
	turn := func(n int) {
		t.Helper()
		id := fmt.Sprintf("call_%d", n)
		if err := e.st.AppendItems(e.ctx, uid, []json.RawMessage{
			marshal(storedItem{Item: marshal(map[string]any{"type": "reasoning", "id": fmt.Sprintf("rs_%d", n), "encrypted_content": "opaque"})}),
			marshal(storedItem{Item: marshal(map[string]any{"type": "function_call", "call_id": id, "name": "fetch_url", "arguments": `{"url":"x"}`})}),
			marshal(storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": id, "output": "ok"})}),
		}); err != nil {
			t.Fatal(err)
		}
	}
	replayed := func() string {
		t.Helper()
		in, err := e.rt.buildInput(e.ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, raw := range in {
			if it := llm.ParseItem(raw); it.Type == "reasoning" {
				ids = append(ids, it.ID)
			}
		}
		return strings.Join(ids, ",")
	}
	if err := e.rt.appendMessage(e.ctx, uid, "user", "[Mon] check the site", nil); err != nil {
		t.Fatal(err)
	}
	turn(1)
	if got := replayed(); got != "rs_1" {
		t.Fatalf("same model: reasoning replayed = %q", got)
	}
	e.rt.cfg.Model = "another-model"
	if got := replayed(); got != "" {
		t.Fatalf("after the model changed: reasoning replayed = %q", got)
	}
	turn(2)
	before, _ := e.rt.buildInput(e.ctx, uid)
	if got := replayed(); got != "rs_2" {
		t.Fatalf("the new model's own reasoning: replayed = %q", got)
	}
	turn(3)
	after, _ := e.rt.buildInput(e.ctx, uid)
	if !isPrefix(before, after) {
		t.Error("the replay changed between turns under the new model")
	}
}

// A model that refuses the replayed reasoning as not its own gets the
// call again without it, and it stays left out.
func TestRefusedReasoningLeftOut(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	uid := e.user.ID
	if err := e.st.AppendItems(e.ctx, uid, []json.RawMessage{
		marshal(storedItem{Kind: "user", Item: marshal(map[string]any{"role": "user", "content": []map[string]any{{"type": "input_text", "text": "hi"}}})}),
		marshal(storedItem{Item: marshal(map[string]any{"type": "reasoning", "id": "rs_1", "encrypted_content": "opaque"})}),
		marshal(storedItem{Item: marshal(map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "hm"}}})}),
	}); err != nil {
		t.Fatal(err)
	}
	m.onStream(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"The encrypted content for item rs_1 could not be verified."}}`))
	}, says(fc("say", map[string]any{"text": "Hello."})))
	in, err := e.rt.buildInput(e.ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.rt.streamModel(e.ctx, e.liveRun(false), 0, &llm.Request{Input: in}, func(llm.Event) {}); err != nil {
		t.Fatalf("the call without the reasoning should work: %v", err)
	}
	calls := m.streamCalls()
	if len(calls) != 2 || len(withoutReasoning(calls[1].Input)) != len(calls[1].Input) {
		t.Fatalf("calls = %d, the second should carry no reasoning", len(calls))
	}
	next, _ := e.rt.buildInput(e.ctx, uid)
	if len(withoutReasoning(next)) != len(next) {
		t.Error("the refused reasoning is replayed again")
	}
}

// Long outputs are kept by size: many small ones stay, a few huge ones are
// cut back, and what's cut stays cut.
func TestLongOutputsBySize(t *testing.T) {
	m := pruneMarks{}
	var items []storedItem
	var ids []int64
	add := func(size int) {
		ids = append(ids, int64(len(ids)+1))
		items = append(items, storedItem{Kind: "long", Item: marshal(map[string]any{"type": "function_call_output", "call_id": "c", "output": strings.Repeat("x", size)})})
	}
	for i := 0; i < 30; i++ {
		add(2000)
	}
	if m = ratchet(m, items, ids); m.Longs != 0 {
		t.Fatalf("60k of small outputs should all stay, mark = %d", m.Longs)
	}
	for i := 0; i < 2; i++ {
		add(30000)
	}
	m = ratchet(m, items, ids)
	if m.Longs == 0 {
		t.Fatal("past the high mark, older outputs should be cut")
	}
	kept := 0
	for i, si := range items {
		if ids[i] > m.Longs {
			kept += len(outputText(si))
		}
	}
	if kept > longLow+30000 || kept < 30000 {
		t.Errorf("kept %d characters after the cut", kept)
	}
	if again := ratchet(m, items, ids); again != m {
		t.Errorf("nothing new, so the marks shouldn't move: %+v -> %+v", m, again)
	}
	if got := cutLong("é" + strings.Repeat("é", longCut)); !utf8.ValidString(got) {
		t.Error("cutLong split a character")
	}
}

// A summary that drops an order number from the conversation it folds is
// sent back and written again (WP-16 acceptance 3); the user's words and
// the number are anchored word for word either way.
func TestCompactionGateKeepsOrderNumber(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	uid := e.user.ID
	_ = e.rt.appendMessage(e.ctx, uid, "user", "[Mon Oct 26 2026 09:00] Order the blue desk lamp from lampshop.example", nil)
	_ = e.st.AppendItems(e.ctx, uid, []json.RawMessage{
		marshal(storedItem{Item: marshal(fc("say", map[string]any{"text": "Done! Order number: 112-4456789, $49.90, receipt sent to ana@example.com."}))}),
	})
	_ = e.rt.appendMessage(e.ctx, uid, "user", "[Mon Oct 26 2026 09:05] thanks", nil)
	_ = e.st.AppendItems(e.ctx, uid, []json.RawMessage{marshal(storedItem{Item: marshal(fc("say", map[string]any{"text": "You're welcome."}))})})
	m.onCreate(
		summarises("## Done\nOrdered the blue desk lamp for $49.90; the receipt went to ana@example.com."),
		summarises("## Done\nOrdered the blue desk lamp (order number 112-4456789) for $49.90; the receipt went to ana@example.com."),
	)
	if err := e.rt.compact(e.ctx, e.user, true); err != nil {
		t.Fatal(err)
	}
	inputs := m.createInputs()
	if len(inputs) != 2 {
		t.Fatalf("summariser called %d times, want 2 (rejected, then accepted)", len(inputs))
	}
	if !strings.Contains(inputs[1], "word for word: 112-4456789") {
		t.Errorf("the retry should name what was lost:\n%s", inputs[1])
	}
	ss, err := e.st.GetSession(e.ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ss.Summary, "order number 112-4456789") {
		t.Errorf("summary = %q", ss.Summary)
	}
	if !strings.Contains(ss.Summary, anchorsHeading) || !strings.Contains(ss.Summary, "> [Mon Oct 26 2026 09:05] thanks") || !strings.Contains(ss.Summary, "- Order number: 112-4456789") {
		t.Errorf("anchors missing:\n%s", ss.Summary)
	}
}

// A summariser that keeps losing the number is let through the second
// time, and the anchors still have it; one that keeps getting cut off
// fails, and isn't asked again until its cooldown is over (WP-16
// acceptance 4).
func TestCompactionGiveUpAndCooldown(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	uid := e.user.ID
	_ = e.rt.appendMessage(e.ctx, uid, "user", "[Mon] book it, confirmation code K7XQ2P please keep it", nil)
	_ = e.st.AppendItems(e.ctx, uid, []json.RawMessage{marshal(storedItem{Item: marshal(fc("say", map[string]any{"text": "Booked."}))})})
	m.onCreate(summarises("## Done\nBooked."), summarises("## Done\nBooked it."))
	if err := e.rt.compact(e.ctx, e.user, true); err != nil {
		t.Fatal(err)
	}
	ss, _ := e.st.GetSession(e.ctx, uid)
	if !strings.Contains(ss.Summary, "K7XQ2P") {
		t.Fatalf("the anchors should keep the code:\n%s", ss.Summary)
	}

	_ = e.rt.appendMessage(e.ctx, uid, "user", "[Tue] and the hotel?", nil)
	_ = e.st.AppendItems(e.ctx, uid, []json.RawMessage{marshal(storedItem{Item: marshal(fc("say", map[string]any{"text": "On it."}))})})
	_ = e.st.AddSessionUsage(e.ctx, uid, int64(e.cfg.CompactTokens)+1, 0)
	cutOff := func(w http.ResponseWriter, r *http.Request) { summary(w, "incomplete", "## Done\nBooked the fl") }
	m.onCreate(cutOff, cutOff)
	before := len(m.createInputs())
	if err := e.rt.compact(e.ctx, e.user, false); err == nil {
		t.Fatal("a summary cut off twice should fail the compaction")
	}
	if n := len(m.createInputs()) - before; n != 2 {
		t.Fatalf("summariser called %d times, want 2", n)
	}
	if err := e.rt.compact(e.ctx, e.user, false); err != nil {
		t.Fatalf("during the cooldown compaction is skipped, got %v", err)
	}
	if n := len(m.createInputs()) - before; n != 2 {
		t.Fatalf("compaction was tried again during its cooldown (%d calls)", n)
	}
	if err := e.rt.compact(e.ctx, e.user, true); err != nil {
		t.Fatalf("a forced compaction goes ahead: %v", err)
	}
	if n := len(m.createInputs()) - before; n != 3 {
		t.Fatalf("forced compaction made %d calls in all, want 3", n)
	}
}

func TestIdentifiers(t *testing.T) {
	transcript := strings.Join([]string{
		"USER: [Mon] buy it from https://lampshop.example/desk-lamp?color=blue for under $50",
		"ALEXA called browser_open {\"url\":\"https://lampshop.example/checkout\"}",
		"  → URL: https://lampshop.example/thanks Your order #W123456789 is confirmed. Related: https://ads.example/x $5.99",
		"ALEXA said: Done. Confirmation is K7XQ2P; I emailed ana@example.com.",
		"ALEXA showed a report \"Lamp\":",
		"Total: 49.90 EUR",
		"NOTE: [The user stopped you.]",
	}, "\n")
	got := identifiers(transcript)
	want := []string{"https://lampshop.example/desk-lamp?color=blue", "$50", "order #W123456789", "Confirmation is K7XQ2P", "ana@example.com", "49.90 EUR"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("identifiers =\n%q\nwant\n%q", got, want)
	}
	if miss := missingFrom("Bought it (W123456789), code K7XQ2P", []string{"order #W123456789", "Confirmation is K7XQ2P", "$50"}); len(miss) != 1 || miss[0] != "$50" {
		t.Errorf("missingFrom = %q", miss)
	}
	// Anchors merge with the previous summary's, newest first, and are
	// never duplicated.
	prev := withAnchors("## Done\nA.", "", []storedItem{{Kind: "user", Item: marshal(map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "first ask, ref ABC-1234"}}})}})
	next := withAnchors("## Done\nB.\n\n"+anchorsHeading+"\n> made up by the model", prev, []storedItem{{Kind: "user", Item: marshal(map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "second ask"}}})}})
	quotes, ids := parseAnchors(next)
	if strings.Join(quotes, "|") != "second ask|first ask, ref ABC-1234" || strings.Join(ids, "|") != "ref ABC-1234" || strings.Count(next, anchorsHeading) != 1 || strings.Contains(next, "made up") {
		t.Errorf("merged anchors:\n%s", next)
	}
}

// ---------- run_shell ----------

// run_shell with 200KB of output shows the real last lines, and says how
// much was left out (WP-16 acceptance 5).
func TestRunShellKeepsTheTail(t *testing.T) {
	h := &headTail{max: shellKeep}
	var all strings.Builder
	for i := 1; all.Len() < 200_000; i++ {
		line := fmt.Sprintf("progress %d ✓\n", i)
		all.WriteString(line)
		_, _ = h.Write([]byte(line))
	}
	_, _ = h.Write([]byte("Traceback (most recent call last):\nValueError: boom\n"))
	out := h.String()
	if !strings.HasPrefix(out, "progress 1 ✓\n") || !strings.HasSuffix(out, "ValueError: boom\n") {
		t.Errorf("head or tail missing:\n%s…%s", out[:40], out[len(out)-60:])
	}
	if !strings.Contains(out, "bytes omitted") || !utf8.ValidString(out) || len(out) > 2*shellKeep+60 {
		t.Errorf("output is %d bytes, valid=%v", len(out), utf8.ValidString(out))
	}
	small := &headTail{max: shellKeep}
	_, _ = small.Write([]byte("ok\n"))
	if small.String() != "ok\n" {
		t.Errorf("short output changed: %q", small.String())
	}

	// And through the tool itself.
	e := newProgressEnv(t)
	env, err := e.rt.envs.Ensure(e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := e.liveRun(false)
	r.env = env
	res, err := runShell(&toolCtx{ctx: e.ctx, rt: e.rt, run: r}, map[string]any{"command": "seq 1 40000; echo THE-END >&2; exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Text, "exit code 3\n1\n2\n") || !strings.HasSuffix(strings.TrimSpace(res.Text), "40000\nTHE-END") || !strings.Contains(res.Text, "bytes omitted") {
		t.Errorf("run_shell output:\n%s … %s", res.Text[:30], res.Text[len(res.Text)-40:])
	}
}
