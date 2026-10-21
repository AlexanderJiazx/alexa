package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"alexagent/internal/agent"
	"alexagent/internal/browser"
	"alexagent/internal/config"
	"alexagent/internal/hub"
	"alexagent/internal/llm"
	"alexagent/internal/notify"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
	"alexagent/internal/vault"
	"alexagent/internal/voice"
)

// ---------- a scripted model ----------

// turn produces one model response: its output items and the prompt size
// to report.
type turn func(ctx context.Context, req *llm.Request) (out []any, inputTokens int64)

// fakeModel plays scripted turns in order, standing in for the Responses
// API, and records what it was sent.
type fakeModel struct {
	t         *testing.T
	mu        sync.Mutex
	turns     []turn
	reqs      []*llm.Request // streamed agent turns
	summaries []*llm.Request // compaction calls
}

func (f *fakeModel) script(ts ...turn) {
	f.mu.Lock()
	f.turns = append(f.turns, ts...)
	f.mu.Unlock()
}

func (f *fakeModel) summaryCalls() []*llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*llm.Request{}, f.summaries...)
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/responses" {
		_, _ = w.Write([]byte(`{"data":[]}`))
		return
	}
	var req llm.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("bad model request: %v", err)
		http.Error(w, `{"error":{"message":"bad request"}}`, 400)
		return
	}
	if !req.Stream {
		f.mu.Lock()
		f.summaries = append(f.summaries, &req)
		f.mu.Unlock()
		writeJSON(w, map[string]any{"status": "completed", "output": []any{say("SUMMARY: they asked about flights to Lisbon.")},
			"usage": map[string]any{"input_tokens": 100, "output_tokens": 20}})
		return
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, &req)
	n := len(f.reqs)
	if len(f.turns) == 0 {
		f.mu.Unlock()
		f.t.Errorf("unexpected model call #%d", n)
		http.Error(w, `{"error":{"message":"no more turns scripted"}}`, 400)
		return
	}
	next := f.turns[0]
	f.turns = f.turns[1:]
	f.mu.Unlock()
	out, in := next(r.Context(), &req)
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for i, it := range out {
		writeEvent(w, map[string]any{"type": "response.output_item.done", "output_index": i, "item": it})
	}
	writeEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed",
		"usage": map[string]any{"input_tokens": in, "output_tokens": 5}}})
}

func writeEvent(w http.ResponseWriter, ev any) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", b)
	w.(http.Flusher).Flush()
}

var callSeq atomic.Int64

// fn is a tool call from the model.
func fn(name string, args map[string]any) any {
	b, _ := json.Marshal(args)
	return map[string]any{"type": "function_call", "call_id": fmt.Sprintf("call_%d", callSeq.Add(1)), "name": name, "arguments": string(b)}
}

// say is plain model text (which the user must never see).
func say(text string) any {
	return map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}
}

// reply is a turn that returns fixed output.
func reply(tokens int64, out ...any) turn {
	return func(context.Context, *llm.Request) ([]any, int64) { return out, tokens }
}

// expect is a turn that first checks what the model was sent.
func expect(t *testing.T, wants []string, out ...any) turn {
	return func(_ context.Context, req *llm.Request) ([]any, int64) {
		in := inputText(req)
		for _, w := range wants {
			if !strings.Contains(in, w) {
				t.Errorf("model input lacks %q; got …%s", w, tail(in, 600))
			}
		}
		return out, 1000
	}
}

func inputText(req *llm.Request) string {
	var b strings.Builder
	for _, raw := range req.Input {
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// ---------- the test server ----------

type testEnv struct {
	srv   *httptest.Server
	st    *store.Store
	envs  *userenv.Envs
	cfg   *config.Config
	model *fakeModel
}

// newTestServer starts Alexa against a fake model. Without a chrome path,
// any browser it starts fails fast and harmlessly.
func newTestServer(t *testing.T, chrome ...string) *testEnv {
	t.Helper()
	dir := t.TempDir()
	chromePath := filepath.Join(dir, "no-chrome")
	if len(chrome) > 0 {
		chromePath = chrome[0]
		// Chrome's helpers can still be writing to its profile for a moment
		// after it's closed; t.TempDir would fail the test over that.
		var err error
		if dir, err = os.MkdirTemp("", "alexa-api-test-"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for i := 0; i < 10 && os.RemoveAll(dir) != nil; i++ {
				time.Sleep(200 * time.Millisecond)
			}
		})
	}
	t.Setenv("DATA_DIR", dir)
	t.Setenv("META_MODEL_API_KEY", "test")
	t.Setenv("SANDBOX_UIDS", "false")
	cfg := config.Load()
	fm := &fakeModel{t: t}
	model := httptest.NewServer(fm)
	t.Cleanup(model.Close)
	cfg.BaseURL = model.URL
	st, err := store.Open(filepath.Join(dir, "alexa.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := hub.New()
	n, err := notify.New(ctx, st, h, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	envs := userenv.New(dir, false, 0)
	br := browser.NewManager(browser.Options{ChromePath: chromePath, Headless: true, Width: 1280, Height: 800}, func(id int64) browser.UserEnv {
		e := envs.For(id)
		return browser.UserEnv{UserID: id, ProfileDir: e.Profile, DownloadDir: e.Downloads, HomeDir: e.Home}
	}, nil)
	v, err := vault.Open(filepath.Join(dir, "vault.key"), st)
	if err != nil {
		t.Fatal(err)
	}
	rt := agent.New(ctx, cfg, st, llm.New(cfg.BaseURL, cfg.APIKey), br, envs, h, n, v)
	static := fstest.MapFS{"index.html": {Data: []byte("<html>alexa</html>")}}
	srv := httptest.NewServer(New(cfg, st, rt, h, envs, n, &voice.Service{}, br, v, static).Handler())
	t.Cleanup(func() { srv.Close(); rt.Shutdown(); br.CloseAll(); cancel(); st.Close() })
	return &testEnv{srv: srv, st: st, envs: envs, cfg: cfg, model: fm}
}

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func (e *testEnv) client(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: e.srv.URL, hc: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any, csrf bool) (int, map[string]any, []any) {
	c.t.Helper()
	code, raw := c.raw(method, path, body, csrf)
	var obj map[string]any
	var arr []any
	if json.Unmarshal(raw, &obj) != nil {
		_ = json.Unmarshal(raw, &arr)
	}
	return code, obj, arr
}

func (c *client) raw(method, path string, body any, csrf bool) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("X-Alexa", "1")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (c *client) signup(name string) {
	c.t.Helper()
	if code, m, _ := c.do("POST", "/api/auth/signup", map[string]any{"username": name, "password": "password1", "timezone": "Europe/Lisbon"}, true); code != 200 {
		c.t.Fatalf("signup %s: %d %v", name, code, m)
	}
}

func (c *client) send(text string) {
	c.t.Helper()
	if code, m, _ := c.do("POST", "/api/messages", map[string]any{"text": text}, true); code != 200 {
		c.t.Fatalf("send %q: %d %v", text, code, m)
	}
}

func (c *client) post(path string, body any) {
	c.t.Helper()
	if code, m, _ := c.do("POST", path, body, true); code != 200 {
		c.t.Fatalf("POST %s: %d %v", path, code, m)
	}
}

type entry struct {
	ID     int64           `json:"id"`
	Kind   string          `json:"kind"`
	Text   string          `json:"text"`
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

type session struct {
	State struct {
		Status   string `json:"status"`
		Activity string `json:"activity"`
		Needs    *struct {
			Kind    string   `json:"kind"`
			Message string   `json:"message"`
			Options []string `json:"options"`
		} `json:"needs"`
		Surface struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"surface"`
		Control     bool  `json:"control"`
		Compactions int64 `json:"compactions"`
	} `json:"state"`
	Feed   []entry         `json:"feed"`
	Report json.RawMessage `json:"report"`
}

func (c *client) session() session {
	c.t.Helper()
	code, raw := c.raw("GET", "/api/session", nil, false)
	if code != 200 {
		c.t.Fatalf("GET /api/session = %d", code)
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		c.t.Fatal(err)
	}
	return s
}

// wait polls until the session reaches a status.
func (c *client) wait(status string) session {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := c.session()
		if s.State.Status == status {
			return s
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("session stuck in %q (%s), want %q; feed %+v", s.State.Status, s.State.Activity, status, s.Feed)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func last(feed []entry, kind string) *entry {
	for i := len(feed) - 1; i >= 0; i-- {
		if feed[i].Kind == kind {
			return &feed[i]
		}
	}
	return nil
}

func lastN(feed []entry, n int) []string {
	var out []string
	for _, e := range feed[max(0, len(feed)-n):] {
		out = append(out, e.Kind+":"+e.Text)
	}
	return out
}

// ---------- tests ----------

// TestSession drives one user's never-ending session through the main
// flows: answers through say only, the nudge when the model forgets,
// reports for long content, questions, messages that arrive mid-run,
// browser takeover, stopping and resetting.
func TestSession(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	alice := e.client(t)
	alice.signup("alice")

	// 1. The model's own text stays hidden; only say reaches the user.
	fm.script(
		reply(1000, say("Let me think about this privately."), fn("say", map[string]any{"text": "Hi Alice — what can I do?"})),
		reply(1100, say("Nothing more to add.")),
	)
	alice.send("hello")
	s := alice.wait("idle")
	if got := lastN(s.Feed, 9); strings.Join(got, "|") != "user:hello|say:Hi Alice — what can I do?" {
		t.Fatalf("feed = %v", got)
	}
	first := fm.reqs[0]
	today := time.Now().In(mustLoc("Europe/Lisbon")).Format("Jan 2 2006")
	if strings.Contains(first.Instructions, today) {
		t.Error("instructions carry the date, which breaks prompt caching")
	}
	if !strings.Contains(inputText(first), today) || !strings.Contains(inputText(first), "] hello") {
		t.Errorf("message isn't stamped with its time: %s", inputText(first))
	}

	// 2. A turn that ends without say gets one nudge, then a fallback.
	fm.script(
		reply(1200, say("The capital of Portugal is Lisbon.")),
		expect(t, []string{"only say, ask and present_report reach them"}, say("Lisbon.")),
	)
	alice.send("capital of portugal?")
	s = alice.wait("idle")
	if got := last(s.Feed, "say"); got == nil || got.Text != "Lisbon." {
		t.Fatalf("fallback message = %+v", got)
	}

	// 3. Long messages are refused; the details go in a report.
	long := strings.Repeat("Lisbon has many neighbourhoods. ", 20)
	fm.script(
		reply(1300, fn("say", map[string]any{"text": long})),
		expect(t, []string{"Not sent"}, fn("present_report", map[string]any{
			"title":   "Lisbon neighbourhoods",
			"content": "## Alfama\n\nThe old town.",
			"items":   []any{map[string]any{"label": "Alfama", "detail": "historic"}},
			"events":  []any{map[string]any{"title": "Fado night", "start": "2026-11-02T20:00:00+00:00"}},
		})),
		reply(1400, fn("say", map[string]any{"text": "Here's the rundown."})),
		reply(1500),
	)
	alice.send("tell me about the neighbourhoods")
	s = alice.wait("idle")
	for _, en := range s.Feed {
		if strings.Contains(en.Text, "many neighbourhoods") {
			t.Fatal("the over-long message was shown")
		}
	}
	if got := lastN(s.Feed, 2); strings.Join(got, "|") != "report:Lisbon neighbourhoods|say:Here's the rundown." {
		t.Fatalf("feed tail = %v", got)
	}
	if s.State.Surface.Kind != "report" || s.State.Surface.ID == "" || len(s.Report) == 0 {
		t.Fatalf("the report isn't on screen: %+v", s.State.Surface)
	}
	repID := s.State.Surface.ID
	if code, m, _ := alice.do("GET", "/api/reports/"+repID, nil, false); code != 200 || m["title"] != "Lisbon neighbourhoods" || !strings.Contains(fmt.Sprint(m["content"]), "Alfama") {
		t.Fatalf("get report: %d %v", code, m)
	}
	if _, _, list := alice.do("GET", "/api/reports", nil, false); len(list) != 1 {
		t.Fatalf("reports = %d, want 1", len(list))
	}
	if code, ics := alice.raw("GET", "/api/reports/"+repID+"/calendar.ics", nil, false); code != 200 || !bytes.Contains(ics, []byte("SUMMARY:Fado night")) {
		t.Fatalf("calendar: %d %s", code, ics)
	}
	if code, md := alice.raw("GET", "/api/reports/"+repID+"/download", nil, false); code != 200 || !bytes.HasPrefix(md, []byte("# Lisbon neighbourhoods")) {
		t.Fatalf("download: %d %s", code, md)
	}

	// The user can put the conversation (or another report) back on screen.
	alice.do("PUT", "/api/surface", map[string]any{"kind": ""}, true)
	if s := alice.session(); s.State.Surface.Kind != "" {
		t.Fatalf("surface after closing = %+v", s.State.Surface)
	}
	if code, _, _ := alice.do("PUT", "/api/surface", map[string]any{"kind": "report", "id": "nope"}, true); code != 404 {
		t.Fatalf("show a missing report = %d, want 404", code)
	}

	// 4. A question waits for the answer, which can be any message.
	fm.script(reply(1600, fn("ask", map[string]any{"question": "Window or aisle?", "options": []string{"Window", "Aisle"}})))
	alice.send("book the seat")
	s = alice.wait("needs")
	ask := last(s.Feed, "ask")
	if s.State.Needs == nil || s.State.Needs.Kind != "question" || ask == nil || ask.Status != "open" || !strings.Contains(string(ask.Data), "Aisle") {
		t.Fatalf("question state %+v, entry %+v", s.State.Needs, ask)
	}
	fm.script(
		expect(t, []string{"The user replied: Aisle"}, fn("say", map[string]any{"text": "Aisle it is."})),
		reply(1700),
	)
	alice.send("Aisle")
	s = alice.wait("idle")
	if a := last(s.Feed, "ask"); a.Status != "answered" {
		t.Fatalf("question still %q", a.Status)
	}
	if s.State.Needs != nil {
		t.Fatalf("still needs: %+v", s.State.Needs)
	}

	// 5. A message sent mid-run reaches the model on its next turn.
	gate := make(chan struct{})
	fm.script(
		func(ctx context.Context, _ *llm.Request) ([]any, int64) {
			select {
			case <-gate:
			case <-ctx.Done():
			}
			return []any{fn("say", map[string]any{"text": "On it."})}, 1800
		},
		expect(t, []string{"also check trains", "sent while you were working"}, fn("say", map[string]any{"text": "Trains too, got it."})),
		reply(1900),
	)
	alice.send("find flights to Porto")
	alice.send("also check trains")
	close(gate)
	s = alice.wait("idle")
	if got := lastN(s.Feed, 4); strings.Join(got, "|") != "user:find flights to Porto|user:also check trains|say:On it.|say:Trains too, got it." {
		t.Fatalf("feed tail = %v", got)
	}

	// 6. Takeover: the screen switches to the browser; handing back resumes.
	fm.script(reply(2000, fn("request_takeover", map[string]any{"reason": "Sign in to your airline account"})))
	alice.send("check in for my flight")
	s = alice.wait("needs")
	if s.State.Needs == nil || s.State.Needs.Kind != "takeover" || s.State.Surface.Kind != "browser" || last(s.Feed, "takeover") == nil {
		t.Fatalf("takeover state %+v surface %+v", s.State.Needs, s.State.Surface)
	}
	alice.post("/api/takeover", nil)
	if s := alice.session(); !s.State.Control {
		t.Fatal("not in control after taking over")
	}
	fm.script(
		expect(t, []string{"handed control back", "signed in"}, fn("say", map[string]any{"text": "Thanks — I'm in."})),
		reply(2100),
	)
	alice.post("/api/handback", map[string]any{"note": "signed in"})
	s = alice.wait("idle")
	if s.State.Control || last(s.Feed, "say").Text != "Thanks — I'm in." {
		t.Fatalf("after hand back: control=%v feed %v", s.State.Control, lastN(s.Feed, 3))
	}

	// 7. Stop interrupts a run; maintenance waits until Alexa is idle.
	started := make(chan struct{})
	fm.script(func(ctx context.Context, _ *llm.Request) ([]any, int64) {
		close(started)
		<-ctx.Done()
		return nil, 0
	})
	alice.send("something slow")
	<-started
	if code, _, _ := alice.do("POST", "/api/session/reset", nil, true); code != 409 {
		t.Fatalf("reset while working = %d, want 409", code)
	}
	if code, _, _ := alice.do("POST", "/api/session/compact", nil, true); code != 409 {
		t.Fatalf("compact while working = %d, want 409", code)
	}
	alice.post("/api/stop", nil)
	s = alice.wait("idle")
	if n := last(s.Feed, "note"); n == nil || n.Text != "Stopped" {
		t.Fatalf("no stop note: %v", lastN(s.Feed, 3))
	}
	// The next run learns it was stopped.
	fm.script(
		expect(t, []string{"The user stopped you"}, fn("say", map[string]any{"text": "Okay."})),
		reply(2200),
	)
	alice.send("never mind")
	alice.wait("idle")

	// 8. Reset forgets the conversation but keeps reports.
	alice.post("/api/session/reset", nil)
	s = alice.session()
	if len(s.Feed) != 0 || s.State.Status != "idle" {
		t.Fatalf("after reset: %d entries, %s", len(s.Feed), s.State.Status)
	}
	if _, _, list := alice.do("GET", "/api/reports", nil, false); len(list) != 1 {
		t.Fatal("reset deleted reports")
	}
	fm.script(
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			if in := inputText(req); strings.Contains(in, "hello") || strings.Contains(in, "Porto") {
				t.Errorf("the old conversation survived the reset: %s", tail(in, 400))
			}
			return []any{fn("say", map[string]any{"text": "Fresh start."})}, 900
		},
		reply(950),
	)
	alice.send("hi again")
	alice.wait("idle")
}

func TestCompaction(t *testing.T) {
	e := newTestServer(t)
	e.cfg.CompactTokens = 1000
	fm := e.model
	c := e.client(t)
	c.signup("carol")

	// The first turn reports a prompt over budget, so the next one starts
	// by folding the older conversation into a summary.
	fm.script(
		reply(5000, fn("say", map[string]any{"text": "Looking into Lisbon flights."})),
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			in := inputText(req)
			if !strings.Contains(string(req.Input[0]), "[Summary of the earlier part") || !strings.Contains(string(req.Input[0]), "SUMMARY: they asked") {
				t.Errorf("summary isn't first: %s", req.Input[0])
			}
			if strings.Contains(in, "flights to Lisbon please") {
				t.Errorf("the folded message is still replayed: %s", in)
			}
			return nil, 300
		},
	)
	c.send("flights to Lisbon please")
	s := c.wait("idle")
	if s.State.Compactions != 1 {
		t.Fatalf("compactions = %d, want 1", s.State.Compactions)
	}
	sums := fm.summaryCalls()
	if len(sums) != 1 || !strings.Contains(sums[0].Instructions, "working memory of Alexa") || !strings.Contains(inputText(sums[0]), "USER: [") {
		t.Fatalf("summary calls: %d", len(sums))
	}
	// The feed (what the user sees) keeps everything.
	if len(s.Feed) != 2 {
		t.Fatalf("feed = %v", lastN(s.Feed, 5))
	}
	// Later turns build on the summary.
	fm.script(
		expect(t, []string{"SUMMARY: they asked", "and hotels?"}, fn("say", map[string]any{"text": "Hotels next."})),
		reply(400),
	)
	c.send("and hotels?")
	c.wait("idle")

	// Compacting on demand folds most of what's left, even when the
	// conversation is well under budget.
	e.cfg.CompactTokens = 100_000
	fm.script(reply(800, fn("say", map[string]any{"text": "Booked the Alfama one."})), reply(900))
	c.send("book the cheapest hotel")
	c.wait("idle")
	before := c.session().State.Compactions
	c.post("/api/session/compact", nil)
	if s := c.session(); s.State.Compactions != before+1 {
		t.Fatalf("compactions after manual compact = %d, want %d", s.State.Compactions, before+1)
	}
	fm.script(func(_ context.Context, req *llm.Request) ([]any, int64) {
		if in := inputText(req); strings.Contains(in, "book the cheapest hotel") || !strings.Contains(in, "] thanks") {
			t.Errorf("after compacting on demand the input is: %s", in)
		}
		return nil, 300
	}, reply(300))
	c.send("thanks")
	c.wait("idle")
}

// Alexa's own memory_save leaves the instructions alone, so the long
// conversation cached after them survives; compaction and the user's own
// edits bring them up to date.
func TestMemorySnapshot(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	c := e.client(t)
	c.signup("dana")
	save := func(fact string) {
		t.Helper()
		from := len(fm.reqs)
		fm.script(
			reply(300, fn("memory_save", map[string]any{"content": fact})),
			reply(300, fn("say", map[string]any{"text": "Noted."})),
			reply(300),
		)
		c.send("remember: " + fact)
		c.wait("idle")
		for i, r := range fm.reqs[from:] {
			if strings.Contains(r.Instructions, fact) {
				t.Fatalf("call %d: Alexa's own memory_save rewrote the instructions", i)
			}
		}
	}
	instructionsNext := func() string {
		t.Helper()
		from := len(fm.reqs)
		fm.script(reply(300, fn("say", map[string]any{"text": "Hi."})), reply(300))
		c.send("hello")
		c.wait("idle")
		return fm.reqs[from].Instructions
	}

	save("Prefers aisle seats")
	c.post("/api/session/compact", nil)
	if s := c.session(); s.State.Compactions != 1 {
		t.Fatalf("compactions = %d", s.State.Compactions)
	}
	if got := instructionsNext(); !strings.Contains(got, "Prefers aisle seats") {
		t.Fatalf("after compaction the instructions lack the memory: …%s", tail(got, 300))
	}

	save("Has a cat named Miso")
	c.post("/api/memories", map[string]any{"content": "Vegetarian"})
	if got := instructionsNext(); !strings.Contains(got, "Vegetarian") || !strings.Contains(got, "Miso") {
		t.Fatalf("after the user's edit the instructions lack memories: …%s", tail(got, 300))
	}
}

func TestAuthAndIsolation(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	bob := e.client(t)
	anon := e.client(t)

	if code, m, _ := anon.do("GET", "/api/setup", nil, false); code != 200 || m["needsSetup"] != true {
		t.Fatalf("setup: %d %v", code, m)
	}
	if code, _, _ := anon.do("GET", "/api/session", nil, false); code != 401 {
		t.Fatalf("anonymous session = %d, want 401", code)
	}
	// Login/sign-up need the CSRF header too.
	if code, _, _ := alice.do("POST", "/api/auth/signup", map[string]any{"username": "alice", "password": "password1"}, false); code != 403 {
		t.Fatalf("signup without X-Alexa = %d, want 403", code)
	}
	code, m, _ := alice.do("POST", "/api/auth/signup", map[string]any{"username": "alice", "password": "password1", "timezone": "Europe/Berlin"}, true)
	if code != 200 || m["isAdmin"] != true {
		t.Fatalf("first signup: %d %v", code, m)
	}
	// Sign-up is closed after the first user.
	if code, _, _ := bob.do("POST", "/api/auth/signup", map[string]any{"username": "bob", "password": "password2"}, true); code != 403 {
		t.Fatalf("second open signup = %d, want 403", code)
	}
	// The admin adds bob.
	if code, m, _ := alice.do("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "password2"}, true); code != 200 {
		t.Fatalf("admin create user: %d %v", code, m)
	}
	if code, _, _ := bob.do("POST", "/api/auth/login", map[string]any{"username": "bob", "password": "wrong"}, true); code != 401 {
		t.Fatalf("bad password = %d, want 401", code)
	}
	if code, _, _ := bob.do("POST", "/api/auth/login", map[string]any{"username": "bob", "password": "password2"}, true); code != 200 {
		t.Fatalf("bob login = %d", code)
	}
	if code, _, _ := bob.do("GET", "/api/admin/users", nil, false); code != 403 {
		t.Fatalf("non-admin admin route = %d, want 403", code)
	}
	// CSRF: authenticated POST without the header is refused.
	if code, _, _ := alice.do("POST", "/api/memories", map[string]any{"content": "x"}, false); code != 403 {
		t.Fatalf("memory without X-Alexa = %d, want 403", code)
	}
	if code, _, _ := alice.do("POST", "/api/messages", map[string]any{"text": "x"}, false); code != 403 {
		t.Fatalf("message without X-Alexa = %d, want 403", code)
	}

	// Alice's data.
	ctx := context.Background()
	au, _ := e.st.GetUserByName(ctx, "alice")
	data, _ := json.Marshal(agent.ReportData{Events: []agent.CalEvent{{Title: "Dentist", Start: "2026-11-03"}}})
	if err := e.st.SaveReport(ctx, &store.Report{ID: "alicerep", UserID: au.ID, Title: "secret", Format: "markdown", Content: "x", Data: data}); err != nil {
		t.Fatal(err)
	}
	_ = e.st.AddEntry(ctx, &store.Entry{UserID: au.ID, Kind: "say", Text: "alice's secret"})
	alice.do("POST", "/api/memories", map[string]any{"content": "alice's secret"}, true)
	aenv, _ := e.envs.Ensure(au.ID)
	_ = os.WriteFile(filepath.Join(aenv.Workspace, "secret.txt"), []byte("top secret"), 0o600)

	// Bob can't see or touch any of it.
	if s := bob.session(); len(s.Feed) != 0 {
		t.Fatalf("bob sees %d feed entries", len(s.Feed))
	}
	if _, _, arr := bob.do("GET", "/api/reports", nil, false); len(arr) != 0 {
		t.Fatalf("bob sees %d reports", len(arr))
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/reports/alicerep"},
		{"DELETE", "/api/reports/alicerep"},
		{"GET", "/api/reports/alicerep/calendar.ics"},
		{"GET", "/api/reports/alicerep/download"},
	} {
		if code, _, _ := bob.do(c.method, c.path, nil, true); code != 404 {
			t.Errorf("bob %s %s = %d, want 404", c.method, c.path, code)
		}
	}
	if code, _, _ := bob.do("PUT", "/api/surface", map[string]any{"kind": "report", "id": "alicerep"}, true); code != 404 {
		t.Errorf("bob showed alice's report = %d, want 404", code)
	}
	if _, _, arr := bob.do("GET", "/api/memories", nil, false); len(arr) != 0 {
		t.Fatalf("bob sees alice's memories")
	}
	for _, p := range []string{"secret.txt", "../1/workspace/secret.txt", "/../../1/workspace/secret.txt", "%2e%2e/1/workspace/secret.txt"} {
		code, _, _ := bob.do("GET", "/api/files/download?path="+p, nil, false)
		if code == 200 {
			t.Errorf("bob downloaded alice's file via %q", p)
		}
	}
	// Alice still can.
	if code, _, _ := alice.do("GET", "/api/files/download?path=secret.txt", nil, false); code != 200 {
		t.Fatalf("alice download = %d", code)
	}
	if code, m, _ := alice.do("GET", "/api/reports/alicerep", nil, false); code != 200 || m["title"] != "secret" {
		t.Fatalf("alice get report: %d", code)
	}
	if code, ics := alice.raw("GET", "/api/reports/alicerep/calendar.ics", nil, false); code != 200 || !bytes.Contains(ics, []byte("Dentist")) {
		t.Fatalf("alice calendar: %d", code)
	}
	if s := alice.session(); len(s.Feed) != 1 {
		t.Fatalf("alice feed = %d entries", len(s.Feed))
	}
	// Logout ends the session.
	alice.do("POST", "/api/auth/logout", nil, true)
	if code, _, _ := alice.do("GET", "/api/me", nil, false); code != 401 {
		t.Fatalf("after logout /api/me = %d, want 401", code)
	}
}

// TestSignIn covers the secure sign-in form: Alexa asks, the user types
// their login into the app, it's saved encrypted, and Alexa carries on
// without ever seeing the password.
func TestSignIn(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	alice := e.client(t)
	alice.signup("alice")
	const password = "s3cret-Pa55word"

	if code, _, _ := alice.do("POST", "/api/signin", map[string]any{"username": "a", "password": "b", "save": true}, true); code != 409 {
		t.Fatalf("a login nobody asked for = %d, want 409", code)
	}
	fm.script(reply(1000, fn("request_login", map[string]any{"reason": "Your Example session expired", "site": "https://www.example.com/login"})))
	alice.send("check my example.com orders")
	s := alice.wait("needs")
	if s.State.Needs == nil || s.State.Needs.Kind != "login" || s.State.Surface.Kind != "browser" {
		t.Fatalf("needs %+v, surface %+v", s.State.Needs, s.State.Surface)
	}
	var needs struct {
		State struct {
			Needs struct {
				Site string `json:"site"`
			} `json:"needs"`
		} `json:"state"`
	}
	_, raw := alice.raw("GET", "/api/session", nil, false)
	if json.Unmarshal(raw, &needs); needs.State.Needs.Site != "example.com" {
		t.Fatalf("asked to sign in to %q", needs.State.Needs.Site)
	}
	if card := last(s.Feed, "login"); card == nil || card.Status != "open" || !strings.Contains(string(card.Data), `"site":"example.com"`) {
		t.Fatalf("login card = %+v", card)
	}

	fm.script(
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			in := inputText(req)
			if !strings.Contains(in, "entered their login for example.com") || !strings.Contains(in, "alice@example.com") || !strings.Contains(in, "browser_login") {
				t.Errorf("Alexa wasn't told about the login: …%s", tail(in, 400))
			}
			return []any{fn("say", map[string]any{"text": "Signed in."})}, 1100
		},
		reply(1200),
	)
	if code, m, _ := alice.do("POST", "/api/signin", map[string]any{"username": " alice@example.com ", "password": password, "save": true}, true); code != 200 {
		t.Fatalf("sign in: %d %v", code, m)
	}
	s = alice.wait("idle")
	if card := last(s.Feed, "login"); card.Status != "answered" {
		t.Fatalf("login card still %q", card.Status)
	}
	if st := last(s.Feed, "step"); st == nil || st.Text != "You entered your login for example.com" {
		t.Fatalf("step = %+v", st)
	}

	// Saved, encrypted, and never shown again: not to the model, not in the
	// conversation, not by the API.
	code, list := alice.raw("GET", "/api/logins", nil, false)
	if code != 200 || !bytes.Contains(list, []byte(`"site":"example.com"`)) || !bytes.Contains(list, []byte(`"username":"alice@example.com"`)) {
		t.Fatalf("logins: %d %s", code, list)
	}
	_, feed := alice.raw("GET", "/api/session", nil, false)
	for what, b := range map[string][]byte{"the logins list": list, "the session": feed} {
		if bytes.Contains(b, []byte(password)) {
			t.Errorf("%s shows the password", what)
		}
	}
	for i, req := range fm.reqs {
		if strings.Contains(inputText(req), password) || strings.Contains(req.Instructions, password) {
			t.Errorf("model call %d was sent the password", i)
		}
	}
	au, _ := e.st.GetUserByName(context.Background(), "alice")
	saved, _ := e.st.Logins(context.Background(), au.ID)
	if len(saved) != 1 || bytes.Contains(saved[0].Secret, []byte(password)) {
		t.Fatalf("stored logins: %d, in the clear: %v", len(saved), len(saved) == 1 && bytes.Contains(saved[0].Secret, []byte(password)))
	}

	// Asked again (say the password was rejected), the form suggests the
	// account; answering in a message instead goes to Alexa as a reply.
	fm.script(reply(1300, fn("request_login", map[string]any{"reason": "That password didn't work", "site": "example.com"})))
	alice.send("try again")
	alice.wait("needs")
	_, raw = alice.raw("GET", "/api/session", nil, false)
	if !bytes.Contains(raw, []byte(`"username":"alice@example.com"`)) {
		t.Fatalf("the form doesn't suggest the saved account: %s", raw)
	}
	fm.script(
		expect(t, []string{"didn't enter their login and replied instead: never mind"}, fn("say", map[string]any{"text": "Okay."})),
		reply(1400),
	)
	alice.send("never mind")
	alice.wait("idle")

	// A login entered without saving is only used for this sign-in.
	fm.script(reply(1500, fn("request_login", map[string]any{"reason": "Sign in to GitHub", "site": "github.com"})))
	alice.send("star my repo")
	alice.wait("needs")
	fm.script(
		expect(t, []string{"chose not to save it"}, fn("say", map[string]any{"text": "Done."})),
		reply(1600),
	)
	alice.post("/api/signin", map[string]any{"username": "alice", "password": "gh-pass-1", "save": false})
	alice.wait("idle")
	if _, _, arr := alice.do("GET", "/api/logins", nil, false); len(arr) != 1 {
		t.Fatalf("logins after an unsaved sign-in = %d, want 1", len(arr))
	}

	// Managing logins in the app.
	code, m, _ := alice.do("POST", "/api/logins", map[string]any{"site": "https://github.com/login", "username": "alice", "password": "gh-pass-2"}, true)
	if code != 200 || m["site"] != "github.com" || m["password"] != nil || m["secret"] != nil {
		t.Fatalf("add login: %d %v", code, m)
	}
	gh := fmt.Sprint(int64(m["id"].(float64)))
	if code, m, _ := alice.do("POST", "/api/logins", map[string]any{"site": "github", "username": "x", "password": "y"}, true); code != 400 {
		t.Fatalf("a login for %q = %d %v, want 400", "github", code, m)
	}
	if code, m, _ := alice.do("PUT", "/api/logins/"+gh, map[string]any{"username": "alice-work"}, true); code != 200 || m["username"] != "alice-work" {
		t.Fatalf("rename: %d %v", code, m)
	}
	alice.post("/api/logins", map[string]any{"site": "github.com", "username": "alice", "password": "z"})
	if code, _, _ := alice.do("PUT", "/api/logins/"+gh, map[string]any{"username": "alice"}, true); code != 409 {
		t.Fatalf("rename onto another login = %d, want 409", code)
	}

	// Bob can't see or touch them.
	bob := e.client(t)
	alice.post("/api/admin/users", map[string]any{"username": "bob", "password": "password2"})
	bob.post("/api/auth/login", map[string]any{"username": "bob", "password": "password2"})
	if _, _, arr := bob.do("GET", "/api/logins", nil, false); len(arr) != 0 {
		t.Fatalf("bob sees %d of alice's logins", len(arr))
	}
	for _, method := range []string{"PUT", "DELETE"} {
		if code, _, _ := bob.do(method, "/api/logins/"+gh, map[string]any{"password": "pwned"}, true); code != 404 {
			t.Errorf("bob %s alice's login = %d, want 404", method, code)
		}
	}
	if code, _, _ := alice.do("DELETE", "/api/logins/"+gh, nil, true); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	if code, _, _ := alice.do("DELETE", "/api/logins/"+gh, nil, true); code != 404 {
		t.Fatalf("delete again = %d, want 404", code)
	}
}

// A scheduled job that comes up while Alexa waits for the user isn't taken
// for their answer.
func TestScheduleWhileWaiting(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	c := e.client(t)
	c.signup("gina")
	fm.script(reply(900, fn("request_login", map[string]any{"reason": "Sign in to check your orders", "site": "example.com"})))
	c.send("check my orders")
	c.wait("needs")
	code, m, _ := c.do("POST", "/api/schedules", map[string]any{"name": "Water plants", "prompt": "Remind me to water the plants", "cron": "0 8 * * *"}, true)
	if code != 200 {
		t.Fatalf("create schedule: %d %v", code, m)
	}
	fm.script(
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			in := inputText(req)
			if !strings.Contains(in, "hasn't answered yet") || !strings.Contains(in, "Remind me to water the plants") || strings.Contains(in, "The user replied") || strings.Contains(in, "replied instead") {
				t.Errorf("the scheduled job reached Alexa as: …%s", tail(in, 500))
			}
			return []any{fn("say", map[string]any{"text": "Time to water the plants."})}, 900
		},
		reply(900),
	)
	c.post("/api/schedules/"+fmt.Sprint(m["id"])+"/run", nil)
	c.wait("idle")
}

// findChrome returns a Chrome to run, or skips the test.
func findChrome(t *testing.T) string {
	t.Helper()
	if os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no Chrome to run")
	return ""
}

// TestBrowserLogin signs in to a site with a real browser: with no saved
// login Alexa asks for one, the user enters it in the app, and Alexa fills
// in the site's form without ever seeing the password.
func TestBrowserLogin(t *testing.T) {
	chrome := findChrome(t)
	const password = "Tr0ub4dor-3-horse"
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>Sign in</title><body>
			<form onsubmit="event.preventDefault(); out.textContent = 'Welcome ' + user.value + (pass.value === '%s' ? ', right password' : ', wrong password')">
			<input id=user type=email placeholder="Email"> <input id=pass type=password placeholder="Password"> <button>Sign in</button></form><p id=out></p>`, password)
	}))
	defer site.Close()
	e := newTestServer(t, chrome)
	fm := e.model
	c := e.client(t)
	c.signup("frank")
	// field finds an input's index in the latest page state Alexa was sent
	// (as JSON, where "<" is escaped).
	field := func(req *llm.Request, typ string) int {
		m := regexp.MustCompile(`\[(\d+)\] (?:<|\\u003c)input type=`+typ).FindAllStringSubmatch(inputText(req), -1)
		if len(m) == 0 {
			t.Errorf("no %s field in the page state: …%s", typ, tail(inputText(req), 600))
			return 0
		}
		n, _ := strconv.Atoi(m[len(m)-1][1])
		return n
	}
	login := func(submit bool) turn {
		return func(_ context.Context, req *llm.Request) ([]any, int64) {
			return []any{fn("browser_login", map[string]any{"username_index": field(req, "email"), "password_index": field(req, "password"), "submit": submit, "status": "Signing in"})}, 900
		}
	}
	fm.script(
		reply(900, fn("browser_open", map[string]any{"url": site.URL, "status": "Opening the site"})),
		login(false),
		expect(t, []string{"no saved login for 127.0.0.1", "request_login"}, fn("request_login", map[string]any{"reason": "Sign in to the test site"})),
	)
	c.send("sign in to the test site")
	if s := c.wait("needs"); s.State.Needs == nil || s.State.Needs.Kind != "login" {
		t.Fatalf("needs = %+v", s.State.Needs)
	}
	fm.script(
		login(true),
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			if in := inputText(req); !strings.Contains(in, "Welcome frank@example.com, right password") || !strings.Contains(in, "Filled in the username and password") {
				t.Errorf("not signed in: …%s", tail(in, 1500))
			}
			return []any{fn("say", map[string]any{"text": "Signed in."})}, 900
		},
		reply(900),
	)
	c.post("/api/signin", map[string]any{"username": "frank@example.com", "password": password, "save": true})
	c.wait("idle")
	for i, req := range fm.reqs {
		if strings.Contains(inputText(req), password) {
			t.Errorf("model call %d was sent the password", i)
		}
	}
}

// The app shows the current exchange: it starts with a message that set
// Alexa to work while it was idle, not with one sent while it worked or one
// answering its question.
func TestExchangeStart(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	c := e.client(t)
	c.signup("erin")
	starts := func(s session, text string) bool {
		t.Helper()
		for _, en := range s.Feed {
			if en.Text == text {
				return strings.Contains(string(en.Data), `"start":true`)
			}
		}
		t.Fatalf("no entry %q", text)
		return false
	}
	gate := make(chan struct{})
	fm.script(
		func(ctx context.Context, _ *llm.Request) ([]any, int64) {
			select {
			case <-gate:
			case <-ctx.Done():
			}
			return []any{fn("say", map[string]any{"text": "On it."})}, 900
		},
		expect(t, []string{"for two"}, fn("ask", map[string]any{"question": "Which date?"})),
	)
	c.send("book a table")
	c.send("for two")
	close(gate)
	c.wait("needs")
	fm.script(reply(900, fn("say", map[string]any{"text": "Booked."})), reply(900))
	c.send("Friday")
	s := c.wait("idle")
	if !starts(s, "book a table") || starts(s, "for two") || starts(s, "Friday") {
		t.Fatalf("starts: %v %v %v", starts(s, "book a table"), starts(s, "for two"), starts(s, "Friday"))
	}
	fm.script(reply(900, fn("say", map[string]any{"text": "Hi."})), reply(900))
	c.send("thanks")
	if s := c.wait("idle"); !starts(s, "thanks") {
		t.Fatal("a new request doesn't start an exchange")
	}
}

func TestStaticFallback(t *testing.T) {
	e := newTestServer(t)
	c := e.client(t)
	resp, err := c.hc.Get(e.srv.URL + "/some/deep/link")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Contains(b, []byte("alexa")) {
		t.Fatalf("SPA fallback: %d %s", resp.StatusCode, b)
	}
	if code, _, _ := c.do("GET", "/api/nope", nil, false); code != 404 {
		t.Fatalf("unknown api route = %d", code)
	}
}

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
