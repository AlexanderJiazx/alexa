package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alexagent/internal/browser"
	"alexagent/internal/config"
	"alexagent/internal/hub"
	"alexagent/internal/llm"
	"alexagent/internal/notify"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
	"alexagent/internal/vault"
)

// ---------- a runtime against a scripted model ----------

// progressEnv runs a Runtime for one user against a scripted model, for the
// plan, step, lifecycle and surface tests.
type progressEnv struct {
	t     *testing.T
	ctx   context.Context
	rt    *Runtime
	st    *store.Store
	hub   *hub.Hub
	cfg   *config.Config
	user  *store.User
	model *scriptModel
}

// modelTurn is one scripted model response: output items, plus stream
// events (sse) and the prompt size to report (usage).
type modelTurn func(req *llm.Request) []any

type sse map[string]any

type usage int64

type scriptModel struct {
	t     *testing.T
	mu    sync.Mutex
	turns []modelTurn
	reqs  []*llm.Request
	// summary, if set, runs before a compaction summary is answered (to
	// hold it); it gets the request.
	summary func(r *http.Request)
}

// holdSummaries makes compaction summaries wait: started is closed when one
// is asked for, and it's answered once release is closed or the request
// goes away.
func (m *scriptModel) holdSummaries() (started, release chan struct{}) {
	started, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.mu.Lock()
	m.summary = func(r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	m.mu.Unlock()
	return started, release
}

func (m *scriptModel) script(ts ...modelTurn) {
	m.mu.Lock()
	m.turns = append(m.turns, ts...)
	m.mu.Unlock()
}

func (m *scriptModel) requests() []*llm.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*llm.Request{}, m.reqs...)
}

func (m *scriptModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/responses" {
		_, _ = w.Write([]byte(`{"data":[]}`))
		return
	}
	var req llm.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":{"message":"bad request"}}`, 400)
		return
	}
	if !req.Stream { // a compaction summary
		m.mu.Lock()
		hold := m.summary
		m.mu.Unlock()
		if hold != nil {
			hold(r)
			if r.Context().Err() != nil {
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{text("## In progress\nFinding a hotel in Lisbon.")},
			"usage": map[string]any{"input_tokens": 100, "output_tokens": 20}})
		return
	}
	m.mu.Lock()
	m.reqs = append(m.reqs, &req)
	if len(m.turns) == 0 {
		m.mu.Unlock()
		m.t.Errorf("unexpected model call #%d", len(m.reqs))
		http.Error(w, `{"error":{"message":"no more turns scripted"}}`, 400)
		return
	}
	next := m.turns[0]
	m.turns = m.turns[1:]
	m.mu.Unlock()
	out := next(&req)
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	tokens, i := int64(1000), 0
	for _, it := range out {
		switch it := it.(type) {
		case sse:
			writeSSE(w, it)
		case usage:
			tokens = int64(it)
		default:
			writeSSE(w, map[string]any{"type": "response.output_item.added", "output_index": i, "item": it})
			writeSSE(w, map[string]any{"type": "response.output_item.done", "output_index": i, "item": it})
			i++
		}
	}
	writeSSE(w, map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed",
		"usage": map[string]any{"input_tokens": tokens, "output_tokens": 5}}})
}

func writeSSE(w http.ResponseWriter, ev any) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", b)
	w.(http.Flusher).Flush()
}

var testCallSeq atomic.Int64

// fc is a tool call from the model.
func fc(name string, args map[string]any) any {
	b, _ := json.Marshal(args)
	return map[string]any{"type": "function_call", "call_id": fmt.Sprintf("call_%d", testCallSeq.Add(1)), "name": name, "arguments": string(b)}
}

// text is plain model text, which the user never sees.
func text(s string) any {
	return map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": s}}}
}

// does is a turn that returns fixed output.
func does(out ...any) modelTurn { return func(*llm.Request) []any { return out } }

// gate holds a model turn: wait returns once the run is in the model call
// (so the test may act as the run's own goroutine would while it streams),
// and open lets the call answer.
type gate struct{ in, out chan struct{} }

func newGate() *gate { return &gate{in: make(chan struct{}), out: make(chan struct{})} }

func (g *gate) turn(out ...any) modelTurn {
	return func(*llm.Request) []any { close(g.in); <-g.out; return out }
}

func (g *gate) wait() { <-g.in }
func (g *gate) open() { close(g.out) }

func inputOf(req *llm.Request) string {
	var b strings.Builder
	for _, raw := range req.Input {
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

func newProgressEnv(t *testing.T) *progressEnv {
	t.Helper()
	dir := t.TempDir()
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
	// No Chrome: a browser it starts fails at once. Browser tools that need
	// a page are faked per test (fakeTool).
	br := browser.NewManager(browser.Options{ChromePath: filepath.Join(dir, "no-chrome"), Headless: true, Width: 1280, Height: 800}, func(id int64) browser.UserEnv {
		e := envs.For(id)
		return browser.UserEnv{UserID: id, ProfileDir: e.Profile, DownloadDir: e.Downloads, HomeDir: e.Home}
	}, nil)
	v, err := vault.Open(filepath.Join(dir, "vault.key"), st)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptModel{t: t}
	srv := httptest.NewServer(model)
	cfg := &config.Config{Model: "test-model", ReasoningEffort: "low", MaxTurns: 30, CompactTokens: 200_000, DataDir: dir}
	rt := New(ctx, cfg, st, llm.New(srv.URL, "test"), br, envs, h, n, v)
	if rt.toolMap["plan"] == nil {
		// Until buildTools lists it.
		pt := planTool()
		rt.tools, rt.toolMap["plan"], rt.toolDefs = append(rt.tools, pt), pt, append(rt.toolDefs, pt.schema())
	}
	u, err := st.CreateUser(ctx, "ana", "x", "Ana", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Shutdown(); br.CloseAll(); srv.Close(); cancel(); st.Close() })
	return &progressEnv{t: t, ctx: ctx, rt: rt, st: st, hub: h, cfg: cfg, user: u, model: model}
}

// fakeTool replaces a tool's work, keeping its name and status labels.
func (e *progressEnv) fakeTool(name string, run func(a map[string]any) (*toolResult, error)) {
	old := e.rt.toolMap[name]
	e.rt.toolMap[name] = &tool{Name: name, NoStatus: old != nil && old.NoStatus, Run: func(_ *toolCtx, a map[string]any) (*toolResult, error) { return run(a) }}
}

// page is a browser state as the browser tools return it.
func page(msg, url, title string, elements ...string) *toolResult {
	var b strings.Builder
	if msg != "" {
		b.WriteString(msg + "\n\n")
	}
	fmt.Fprintf(&b, "URL: %s\nTitle: %s\nViewport 1280x800, scrolled to y=0 of page height 2400.\n\nInteractive elements in view ([index] description; boxes with these numbers are drawn on the screenshot):\n", url, title)
	for i, el := range elements {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, el)
	}
	return &toolResult{Text: b.String(), Kind: "browser_state", Short: msg}
}

func (e *progressEnv) send(text string) {
	e.t.Helper()
	if err := e.rt.Send(e.ctx, e.user, Message{Text: text}); err != nil {
		e.t.Fatal(err)
	}
}

// settle waits until Alexa stops working and returns the view.
func (e *progressEnv) settle() View {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v := e.rt.View(e.ctx, e.user.ID)
		if v.Status != "working" && e.rt.running(e.user.ID) == nil {
			return v
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("still working (%s)", v.Activity)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *progressEnv) feed() []*store.Entry {
	e.t.Helper()
	f, err := e.st.Feed(e.ctx, e.user.ID, 0, 500)
	if err != nil {
		e.t.Fatal(err)
	}
	return f
}

func entriesOf(feed []*store.Entry, kind string) []*store.Entry {
	var out []*store.Entry
	for _, x := range feed {
		if x.Kind == kind {
			out = append(out, x)
		}
	}
	return out
}

func stepNamed(t *testing.T, feed []*store.Entry, label string) (*store.Entry, StepData) {
	t.Helper()
	for _, x := range feed {
		if x.Kind == "step" && x.Text == label {
			var d struct{ Step StepData }
			if err := json.Unmarshal(x.Data, &d); err != nil {
				t.Fatalf("step %q has data %s: %v", label, x.Data, err)
			}
			return x, d.Step
		}
	}
	t.Fatalf("no step %q in the feed", label)
	return nil, StepData{}
}

type hubEvent struct {
	Seq  int64           `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// events returns what the user's screens were sent so far, of one type.
func (e *progressEnv) events(typ string) []hubEvent {
	e.t.Helper()
	s := e.hub.Subscribe(e.user.ID, "test", e.hub.Epoch()+":0")
	defer s.Close()
	if s.Resync {
		e.t.Fatal("the hub no longer holds every event")
	}
	var out []hubEvent
	for _, b := range s.Missed {
		var ev hubEvent
		if err := json.Unmarshal(b, &ev); err != nil {
			e.t.Fatal(err)
		}
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

func (e *progressEnv) runEvents() []runEvent {
	var out []runEvent
	for _, ev := range e.events("run") {
		var r runEvent
		if err := json.Unmarshal(ev.Data, &r); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func planArgs(title string, steps ...string) map[string]any {
	var list []any
	for _, s := range steps {
		status, text, _ := strings.Cut(s, ":")
		list = append(list, map[string]any{"text": text, "status": status})
	}
	return map[string]any{"title": title, "steps": list}
}

// ---------- the plan ----------

// A plan stays one entry, updated in place, across many updates, and
// survives compaction word for word: in the summary and recited after it
// (WP-05 acceptance 1).
func TestPlanStaysOneEntryAndSurvivesCompaction(t *testing.T) {
	e := newProgressEnv(t)
	e.cfg.CompactTokens = 4000
	updates := []map[string]any{
		planArgs("Book a quiet hotel in Lisbon", "active:Search hotels", "pending:Compare prices", "pending:Book the best one"),
		planArgs("Book a quiet hotel in Lisbon", "done:Search hotels", "active:Compare prices", "pending:Book the best one"),
		planArgs("Book a quiet hotel in Lisbon", "done:Search hotels", "done:Compare prices", "active:Book the best one"),
		planArgs("Book a quiet hotel in Lisbon", "done:Search hotels", "done:Compare prices", "active:Book the best one", "pending:Email the confirmation"),
		planArgs("Book a quiet hotel in Lisbon", "done:Search hotels", "done:Compare prices", "done:Book the best one", "active:Email the confirmation"),
	}
	updates[4]["steps"].([]any)[1].(map[string]any)["note"] = "Two under €150"
	var recited string
	e.model.script(
		does(fc("plan", updates[0])),
		does(fc("plan", updates[1])),
		does(fc("plan", updates[2])),
		does(fc("plan", updates[3])),
		does(fc("plan", updates[4]), usage(9000)), // over budget: the next turn compacts first
		func(req *llm.Request) []any {
			recited = inputOf(req)
			return []any{fc("say", map[string]any{"text": "Booked Casa do Bairro; sending the confirmation next."})}
		},
		does(),
	)
	e.send("Book me a quiet hotel in Lisbon for next weekend")
	v := e.settle()

	plans := entriesOf(e.feed(), "plan")
	if len(plans) != 1 {
		t.Fatalf("got %d plan entries, want 1", len(plans))
	}
	pl, _ := parsePlan(updates[4])
	var shown struct{ Plan Plan }
	_ = json.Unmarshal(plans[0].Data, &shown)
	if !samePlan(&shown.Plan, pl) || plans[0].Text != "Book a quiet hotel in Lisbon" || plans[0].Status != "running" {
		t.Errorf("plan entry = %q %s %s, want the last update", plans[0].Text, plans[0].Status, plans[0].Data)
	}
	if v.Plan == nil || !samePlan(v.Plan, pl) {
		t.Errorf("view plan = %+v, want the last update", v.Plan)
	}
	updatesSeen := 0
	for _, ev := range e.events("entry") {
		var x store.Entry
		_ = json.Unmarshal(ev.Data, &x)
		if x.Kind == "plan" {
			updatesSeen++
			if x.ID != plans[0].ID || x.CreatedAt != plans[0].CreatedAt {
				t.Errorf("plan event for entry %d (created %d), want %d (created %d)", x.ID, x.CreatedAt, plans[0].ID, plans[0].CreatedAt)
			}
		}
	}
	if updatesSeen != 5 {
		t.Errorf("saw %d plan entry events, want 5", updatesSeen)
	}

	ss, _ := e.st.GetSession(e.ctx, e.user.ID)
	if ss.Compactions != 1 {
		t.Fatalf("compactions = %d, want 1", ss.Compactions)
	}
	if !strings.HasSuffix(ss.Summary, "## Plan\n"+pl.text()) {
		t.Errorf("summary doesn't end with the plan verbatim:\n%s", ss.Summary)
	}
	want := pl.text()
	if !strings.Contains(recited, jsonText(want)) || strings.Count(recited, jsonText("2. [done] Compare prices (Two under €150)")) != 2 {
		t.Errorf("after compaction the model wasn't shown the plan in the summary and recited:\n%s", tailOf(recited, 1500))
	}
}

// jsonText is s as it appears inside a JSON string.
func jsonText(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}

func tailOf(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func samePlan(a, b *Plan) bool { return string(marshal(a)) == string(marshal(b)) }

// Two active steps are refused with a message the model can act on, and
// nothing is shown (acceptance 2).
func TestPlanRefusesTwoActiveSteps(t *testing.T) {
	e := newProgressEnv(t)
	var answer string
	e.model.script(
		does(fc("plan", planArgs("Renew my books", "active:Sign in to the library", "active:Renew the books", "pending:Tell me the due dates"))),
		func(req *llm.Request) []any {
			answer = inputOf(req)
			return []any{fc("say", map[string]any{"text": "Renewed all three."})}
		},
		does(),
	)
	e.send("Renew my library books")
	v := e.settle()
	if !strings.Contains(answer, "Not updated: steps 1 and 2 are all active. Keep exactly one step active") {
		t.Errorf("the model wasn't told how to fix the plan:\n%s", tailOf(answer, 600))
	}
	if n := len(entriesOf(e.feed(), "plan")); n != 0 || v.Plan != nil {
		t.Errorf("a refused plan was shown: %d entries, view %+v", n, v.Plan)
	}
}

func TestParsePlan(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		fix  string // the start of the fixable error, or ""
		want string // the statuses, when accepted
	}{
		{"one step", planArgs("", "active:Look it up"), "a plan has 2 to 8 steps (this one has 1). For a quick job", ""},
		{"nine steps", planArgs("", "active:1", "pending:2", "pending:3", "pending:4", "pending:5", "pending:6", "pending:7", "pending:8", "pending:9"), "a plan has 2 to 8 steps (this one has 9). Merge", ""},
		{"unknown status", planArgs("", "active:Search", "maybe:Book"), `step 2 has status "maybe"; use pending`, ""},
		{"empty text", planArgs("", "active:Search", "pending: "), "step 2 has no text", ""},
		{"no steps", map[string]any{"steps": "search, book"}, "give steps as a list", ""},
		{"two active", planArgs("", "active:a", "done:b", "active:c"), "steps 1 and 3 are all active", ""},
		{"synonyms", planArgs("", "completed:a", "in_progress:b", "todo:c"), "", "done active pending"},
		{"the next step becomes active", planArgs("", "done:a", "pending:b", "pending:c"), "", "done active pending"},
		{"all finished", planArgs("", "done:a", "skipped:b", "failed:c"), "", "done skipped failed"},
		{"plain strings", map[string]any{"steps": []any{"Search", "Book"}}, "", "active pending"},
	} {
		pl, fix := parsePlan(c.args)
		if c.fix != "" {
			if !strings.HasPrefix(fix, c.fix) {
				t.Errorf("%s: fix = %q, want %q…", c.name, fix, c.fix)
			}
			continue
		}
		if fix != "" {
			t.Errorf("%s: refused: %s", c.name, fix)
			continue
		}
		var got []string
		for _, st := range pl.Steps {
			got = append(got, st.Status)
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s: statuses %v, want %s", c.name, got, c.want)
		}
	}
	pl, _ := parsePlan(map[string]any{"title": strings.Repeat("t", 80), "steps": []any{
		map[string]any{"text": strings.Repeat("s", 90), "status": "active", "note": strings.Repeat("n", 100)}, "Book"}})
	if n := len([]rune(pl.Title)); n != 60 {
		t.Errorf("title is %d characters, want 60", n)
	}
	if n, m := len([]rune(pl.Steps[0].Text)), len([]rune(pl.Steps[0].Note)); n != 60 || m != 80 {
		t.Errorf("step text and note are %d and %d characters, want 60 and 80", n, m)
	}
	if got := planSummary(&Plan{Steps: []PlanStep{{Text: "Search", Status: "done"}, {Text: "Compare prices", Status: "active"}, {Text: "Book", Status: "pending"}, {Text: "Email", Status: "pending"}}}); got != "Plan updated (1 of 4 done). Current: Compare prices." {
		t.Errorf("summary = %q", got)
	}
	if got := planSummary(&Plan{Steps: []PlanStep{{Text: "a", Status: "done"}, {Text: "b", Status: "failed"}, {Text: "c", Status: "skipped"}}}); got != "Plan updated (1 of 3 done, 1 failed, 1 skipped)." {
		t.Errorf("summary = %q", got)
	}
}

func TestWithPlan(t *testing.T) {
	pl := &Plan{Title: "Trip", Steps: []PlanStep{{Text: "Flights", Status: "done", Note: "TAP, €212"}, {Text: "Hotel", Status: "active"}}, Note: "Under €600 in all"}
	want := "Trip\n1. [done] Flights (TAP, €212)\n2. [active] Hotel\nNote: Under €600 in all"
	if got := pl.text(); got != want {
		t.Fatalf("text = %q", got)
	}
	summary := "## In progress\nA trip.\n\n## Plan\n1. flights, then hotel (paraphrased)\n\n## Done\nNothing yet."
	got := withPlan(summary, pl)
	if got != "## In progress\nA trip.\n\n## Done\nNothing yet.\n\n## Plan\n"+want {
		t.Errorf("withPlan = %q", got)
	}
	if got := withPlan(summary, nil); strings.Contains(got, "## Plan") || !strings.Contains(got, "## Done") {
		t.Errorf("an obsolete plan section stayed: %q", got)
	}
}

// A final say while plan steps are open gets one reminder, once per
// exchange (acceptance 7).
func TestPlanReminderOnce(t *testing.T) {
	e := newProgressEnv(t)
	var last string
	e.model.script(
		does(fc("plan", planArgs("Renew my books", "active:Sign in", "pending:Renew the books", "pending:Tell the due dates"))),
		does(fc("say", map[string]any{"text": "All renewed.", "final": true})),
		// The reminded say wasn't final; this one is, and ends the run (H3).
		func(req *llm.Request) []any {
			last = inputOf(req)
			return []any{fc("say", map[string]any{"text": "All three are renewed until Nov 20.", "final": true})}
		},
	)
	e.send("Renew my library books")
	e.settle()
	if n := strings.Count(last, "Your plan still has open steps"); n != 1 {
		t.Errorf("got %d reminders, want 1:\n%s", n, tailOf(last, 800))
	}
	if !strings.Contains(last, jsonText("open steps: “Sign in”, “Renew the books”, “Tell the due dates”")) {
		t.Errorf("the reminder doesn't name the open steps:\n%s", tailOf(last, 800))
	}
	// A new exchange gets a new plan and a new check.
	e.model.script(
		does(fc("plan", planArgs("Pay the bill", "active:Open the bill", "pending:Pay it"))),
		does(fc("say", map[string]any{"text": "Paid.", "final": true})),
		func(req *llm.Request) []any { last = inputOf(req); return nil },
	)
	e.send("Pay the electricity bill")
	v := e.settle()
	if !strings.Contains(tailOf(last, 1500), "Your plan still has open steps: “Open the bill”") {
		t.Errorf("the new exchange's plan wasn't checked:\n%s", tailOf(last, 800))
	}
	if v.Plan == nil || v.Plan.Title != "Pay the bill" {
		t.Errorf("view plan = %+v, want the new exchange's", v.Plan)
	}
	// A final report finishes too.
	e.model.script(
		does(fc("plan", planArgs("Plan the trip", "active:Find flights", "pending:Find a hotel"))),
		does(report("Lisbon trip", nil)),
		func(req *llm.Request) []any { last = inputOf(req); return nil },
	)
	e.send("Plan my Lisbon trip")
	e.settle()
	if !strings.Contains(tailOf(last, 1500), planReminderLead+": “Find flights”, “Find a hotel”") {
		t.Errorf("a final report wasn't checked:\n%s", tailOf(last, 800))
	}
}

// ---------- steps ----------

// A failed click is a failed step with its error, never its predicted done
// text; browser steps carry the site and the element (acceptance 3, 4).
func TestStepsAreHonest(t *testing.T) {
	e := newProgressEnv(t)
	shop := []string{`<a href="/"> Home`, `<button aria-label="Add Sony WH-1000XM6 to cart"> Add to cart`, `<input type=search name=q placeholder="Search the shop"> value="headphones"`}
	e.fakeTool("browser_open", func(a map[string]any) (*toolResult, error) {
		return page("Opened "+str(a, "url"), "https://www.shop.example/p/sony?ref=mail#top", "Sony WH-1000XM6", shop...), nil
	})
	e.fakeTool("browser_click", func(a map[string]any) (*toolResult, error) {
		return page("Action failed: element not found; the page may have changed — get a fresh browser state and use a current index", "https://www.shop.example/p/sony", "Sony WH-1000XM6", shop...), nil
	})
	e.fakeTool("browser_type", func(a map[string]any) (*toolResult, error) {
		return nil, fmt.Errorf("browser unavailable: launch chrome: exec: %q: executable file not found in $PATH", "/opt/chrome/chrome")
	})
	e.model.script(
		does(fc("browser_open", map[string]any{"url": "https://shop.example/p/sony?ref=mail", "status": "Opening the shop", "done": "Opened the shop"})),
		does(fc("browser_click", map[string]any{"index": 2, "status": "Adding to cart", "done": "Added to cart"})),
		does(fc("browser_type", map[string]any{"index": 3, "text": "sony", "status": "Searching the shop", "done": "Searched the shop"})),
		does(fc("say", map[string]any{"text": "I couldn't add it to the cart."})),
		does(),
	)
	e.send("Buy the Sony headphones")
	e.settle()
	feed := e.feed()

	open, od := stepNamed(t, feed, "Opening the shop")
	if open.Status != "done" || open.DoneText != "Opened the shop" || od.Status != "done" || od.OK == nil || !*od.OK {
		t.Errorf("open step = %s %q %+v, want done", open.Status, open.DoneText, od)
	}
	if od.Tool != "browser_open" || od.Kind != "browse" || od.Host != "shop.example" || od.URL != "https://www.shop.example/p/sony" {
		t.Errorf("open step data = %+v", od)
	}

	click, cd := stepNamed(t, feed, "Adding to cart")
	if click.Status != "failed" || click.DoneText != "" {
		t.Errorf("failed click = %s with done text %q, want failed and none", click.Status, click.DoneText)
	}
	if cd.Status != "failed" || cd.OK == nil || *cd.OK || cd.Error != "Element not found" {
		t.Errorf("failed click data = %+v", cd)
	}
	if cd.Tool != "browser_click" || cd.Kind != "click" || cd.Host != "shop.example" || cd.Target != "Add to cart" {
		t.Errorf("click step = %+v, want the shop and the button", cd)
	}

	typ, td := stepNamed(t, feed, "Searching the shop")
	if typ.Status != "failed" || td.Error != "Browser unavailable" || td.Target != "Search the shop" || td.Kind != "type" {
		t.Errorf("type step = %s %+v", typ.Status, td)
	}
	// The typed value is the user's, never a label.
	if strings.Contains(string(typ.Data), "headphones") {
		t.Errorf("a field's value leaked into its step: %s", typ.Data)
	}
}

// A retry under the same label shows its attempt and ends done when it
// works, with its done text back.
func TestStepRetry(t *testing.T) {
	e := newProgressEnv(t)
	tries := 0
	e.fakeTool("browser_click", func(a map[string]any) (*toolResult, error) {
		if tries++; tries == 1 {
			return page("Action failed: timed out after 10s", "https://library.example/account", "Account", `<button> Renew all`), nil
		}
		return page("Clicked [1]", "https://library.example/account", "Account", `<button> Renew all`), nil
	})
	e.model.script(
		does(fc("browser_click", map[string]any{"index": 1, "status": "Renewing the books", "done": "Renewed the books"})),
		does(fc("browser_click", map[string]any{"index": 1, "status": "Renewing the books", "done": "Renewed the books"})),
		does(fc("say", map[string]any{"text": "Renewed."})),
		does(),
	)
	e.send("Renew my books")
	e.settle()
	st, sd := stepNamed(t, e.feed(), "Renewing the books")
	if st.Status != "done" || st.DoneText != "Renewed the books" || sd.Attempt != 2 || sd.OK == nil || !*sd.OK || sd.Error != "" {
		t.Errorf("retried step = %s %q %+v", st.Status, st.DoneText, sd)
	}
}

// Search steps carry their query and the pages read for them (acceptance
// 4), up to 10.
func TestSearchSteps(t *testing.T) {
	e := newProgressEnv(t)
	search := func(id string, action map[string]any) any {
		return map[string]any{"type": "web_search_call", "id": id, "status": "completed", "action": action}
	}
	e.model.script(
		does(
			search("ws1", map[string]any{"type": "search", "query": "quiet hotels Lisbon Alfama"}),
			search("ws2", map[string]any{"type": "open_page", "url": "https://www.timeout.com/lisbon/hotels?utm=x"}),
			search("ws3", map[string]any{"type": "open_page", "url": "https://www.cntraveler.com/lisbon"}),
			fc("say", map[string]any{"text": "Two good picks in Alfama."}),
		),
		does(),
	)
	e.send("Find a quiet hotel in Lisbon")
	e.settle()
	feed := e.feed()
	_, sd := stepNamed(t, feed, "Searching for “quiet hotels Lisbon Alfama”")
	if sd.Kind != "search" || sd.Tool != "web_search" || sd.Query != "quiet hotels Lisbon Alfama" {
		t.Errorf("search step = %+v", sd)
	}
	if len(sd.Sources) != 2 || sd.Sources[0].URL != "https://www.timeout.com/lisbon/hotels" || sd.Sources[1].URL != "https://www.cntraveler.com/lisbon" {
		t.Errorf("search sources = %+v", sd.Sources)
	}
	_, rd := stepNamed(t, feed, "Reading timeout.com")
	if rd.Kind != "read" || rd.Host != "timeout.com" {
		t.Errorf("read step = %+v", rd)
	}

	// The search's own list of sources, as the stream reports it, is kept
	// up to 10.
	g := newGate()
	e.model.script(g.turn(fc("say", map[string]any{"text": "Done."})), does())
	e.send("And in Porto?")
	g.wait()
	r := e.rt.running(e.user.ID)
	var sources []any
	for i := range 12 {
		sources = append(sources, map[string]any{"type": "url", "url": fmt.Sprintf("https://site%d.example/porto", i)})
	}
	item, _ := json.Marshal(search("ws4", map[string]any{"type": "search", "query": "quiet hotels Porto", "sources": sources}))
	e.rt.onStreamEvent(r, llm.Event{Type: "response.output_item.added", Item: item})
	e.rt.onStreamEvent(r, llm.Event{Type: "response.output_item.done", Item: item})
	g.open()
	e.settle()
	_, pd := stepNamed(t, e.feed(), "Searching for “quiet hotels Porto”")
	if len(pd.Sources) != 10 || pd.Sources[9].URL != "https://site9.example/porto" {
		t.Errorf("got %d sources: %+v", len(pd.Sources), pd.Sources)
	}
	if got := e.rt.runSources(r); got != nil {
		t.Errorf("an ended run still lists sources: %v", got)
	}
}

// ---------- the run lifecycle ----------

// The run events arrive in order with times that never go back, and the
// view's start time is the started event's (acceptance 5).
func TestRunEvents(t *testing.T) {
	e := newProgressEnv(t)
	e.fakeTool("browser_open", func(a map[string]any) (*toolResult, error) {
		return page("Opened", "https://united.com/", "United"), nil
	})
	g := newGate()
	e.model.script(
		does(fc("browser_open", map[string]any{"url": "united.com", "status": "Opening United", "done": "Opened United"})),
		g.turn(fc("say", map[string]any{"text": "Your flight is on time.", "final": true})),
		does(),
	)
	e.send("Is my United flight on time?")
	g.wait()
	during := e.rt.View(e.ctx, e.user.ID)
	g.open()
	e.settle()

	evs := e.runEvents()
	var phases []string
	for i, ev := range evs {
		if ev.RunID == "" || ev.RunID != evs[0].RunID {
			t.Errorf("event %d has run id %q, want %q", i, ev.RunID, evs[0].RunID)
		}
		if i > 0 && ev.At <= evs[i-1].At {
			t.Errorf("event %d (%s) at %d, not after %d", i, ev.Phase, ev.At, evs[i-1].At)
		}
		if len(phases) == 0 || phases[len(phases)-1] != ev.Phase {
			phases = append(phases, ev.Phase)
		}
	}
	if !subsequence(phases, []string{"started", "thinking", "tool", "writing", "ended"}) || phases[0] != "started" || phases[len(phases)-1] != "ended" {
		t.Errorf("phases = %v", phases)
	}
	for _, ev := range evs {
		switch ev.Phase {
		case "tool":
			if ev.Tool == nil || *ev.Tool != (runTool{Name: "browser_open", Label: "Opening United", Kind: "browse", Host: "united.com"}) {
				t.Errorf("tool event = %+v", ev.Tool)
			}
		case "writing":
			if ev.Target != "say" {
				t.Errorf("writing target = %q", ev.Target)
			}
		case "ended":
			if ev.Outcome != "done" {
				t.Errorf("outcome = %q", ev.Outcome)
			}
		}
	}
	if during.RunStartedAt != evs[0].At || during.Phase != "thinking" || during.Activity != "Thinking" {
		t.Errorf("view during the run: started %d (event %d), phase %q, activity %q", during.RunStartedAt, evs[0].At, during.Phase, during.Activity)
	}
	if v := e.rt.View(e.ctx, e.user.ID); v.Phase != "" || v.RunStartedAt != 0 {
		t.Errorf("an idle view shows phase %q from %d", v.Phase, v.RunStartedAt)
	}
}

func subsequence(have, want []string) bool {
	i := 0
	for _, h := range have {
		if i < len(want) && h == want[i] {
			i++
		}
	}
	return i == len(want)
}

// The start time is the run's, however often the view is read (a reload),
// and the outcome says how it ended (acceptance 6).
func TestRunStartAndOutcome(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	e.model.script(g.turn(fc("ask", map[string]any{"question": "Window or aisle?", "options": []any{"Window", "Aisle"}})))
	e.send("Pick my seat")
	first := e.rt.View(e.ctx, e.user.ID)
	time.Sleep(20 * time.Millisecond)
	again := e.rt.View(e.ctx, e.user.ID)
	if first.RunStartedAt == 0 || again.RunStartedAt != first.RunStartedAt || again.Status != "working" {
		t.Errorf("start time %d, then %d after a reload", first.RunStartedAt, again.RunStartedAt)
	}
	g.open()
	v := e.settle()
	if v.Status != "needs" || v.Phase != "waiting" || v.RunStartedAt != 0 {
		t.Errorf("waiting view = %s %q %d", v.Status, v.Phase, v.RunStartedAt)
	}
	evs := e.runEvents()
	if last := evs[len(evs)-1]; last.Phase != "ended" || last.Outcome != "suspended" {
		t.Errorf("last event = %+v, want ended, suspended", last)
	}

	// Stopping is an outcome too.
	stop := make(chan struct{})
	e.model.script(func(req *llm.Request) []any { <-stop; return nil })
	e.send("Window")
	time.Sleep(20 * time.Millisecond)
	if err := e.rt.Stop(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	e.settle()
	close(stop)
	evs = e.runEvents()
	if last := evs[len(evs)-1]; last.Phase != "ended" || last.Outcome != "stopped" {
		t.Errorf("last event = %+v, want ended, stopped", last)
	}
}

func TestThoughtHeadline(t *testing.T) {
	for _, c := range []struct {
		in       string
		complete bool
		want     string
		ok       bool
	}{
		{"**Comparing the two cheapest fares**\n\nI'm checking TAP and", false, "Comparing the two cheapest fares", true},
		{"**Comparing the two", false, "", false},
		{"I need to check the hotel's cancellation policy. Then", false, "I need to check the hotel's cancellation policy", true},
		{"Checking prices", false, "", false},
		{"Checking prices", true, "Checking prices", true},
		{strings.Repeat("word ", 30), false, strings.Repeat("word ", 11) + "word…", true},
	} {
		got, ok := thoughtHeadline(c.in, c.complete)
		if got != c.want || ok != c.ok {
			t.Errorf("thoughtHeadline(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// The reasoning summary's headline and the message being written reach the
// screen while the model streams.
func TestStreamEvents(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	e.model.script(g.turn(fc("say", map[string]any{"text": "Done."})), does())
	e.send("Compare the fares")
	g.wait()
	r := e.rt.running(e.user.ID)
	ev := func(m map[string]any) llm.Event {
		b, _ := json.Marshal(m)
		var x llm.Event
		_ = json.Unmarshal(b, &x)
		return x
	}
	for _, d := range []string{"**Comparing", " the two cheapest", " fares**\n\nTAP is"} {
		e.rt.onStreamEvent(r, ev(map[string]any{"type": "response.reasoning_summary_text.delta", "item_id": "rs1", "delta": d}))
	}
	e.rt.onStreamEvent(r, ev(map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "function_call", "name": "present_report", "call_id": "c1", "arguments": ""}}))
	if v := e.rt.View(e.ctx, e.user.ID); v.Phase != "writing" || v.Activity != "Writing the report" {
		t.Errorf("while writing: phase %q, activity %q", v.Phase, v.Activity)
	}
	g.open()
	e.settle()
	var headlines, targets []string
	for _, ev := range e.runEvents() {
		if ev.Headline != "" {
			headlines = append(headlines, ev.Headline)
		}
		if ev.Target != "" {
			targets = append(targets, ev.Target)
		}
	}
	if len(headlines) != 1 || headlines[0] != "Comparing the two cheapest fares" {
		t.Errorf("headlines = %q", headlines)
	}
	if len(targets) < 1 || targets[0] != "report" {
		t.Errorf("writing targets = %q", targets)
	}
}

func TestUserError(t *testing.T) {
	for in, want := range map[string]string{
		"Action failed: element not found; the page may have changed — get a fresh browser state": "Element not found",
		"Error: browser unavailable: launch chrome: exec: \"chrome\": not found":                  "Browser unavailable",
		"Error: open /data/users/3/workspace/notes.md: no such file or directory":                 "Open notes.md: no such file or directory",
		"Action failed: element [12] is a dropdown <select>; use browser_select instead":          "The element is a dropdown <select>",
		"Not sent: 412 characters is too long for a message. Keep messages under 300":             "412 characters is too long for a message",
		"Error: " + strings.Repeat("x", 200):                                                      "X" + strings.Repeat("x", 138) + "…",
	} {
		if got := userError(in); got != want {
			t.Errorf("userError(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCallOutcome(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		tool, text string
		want       string
	}{
		{"run_shell", "exit code 0\nok", "done"},
		{"run_shell", "exit code 2\nboom", "failed"},
		{"fetch_url", "HTTP 404 (text/html)\n\nNot found", "failed"},
		{"fetch_url", "HTTP 200 (text/html)\n\nHello", "done"},
		{"memory_delete", "No such memory.", "failed"},
		{"browser_click", "Stopped.", "interrupted"},
		{"browser_open", "Not done: no steps left for now.", "skipped"},
		{"write_file", "Wrote notes.md", "done"},
	} {
		if got, _ := callOutcome(ctx, c.tool, &toolResult{Text: c.text}, nil); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.tool, c.text, got, c.want)
		}
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if got, _ := callOutcome(stopped, "browser_click", &toolResult{Text: "Clicked"}, nil); got != "interrupted" {
		t.Errorf("a call cut off by Stop is %s", got)
	}
}

func TestElementLabel(t *testing.T) {
	for in, want := range map[string]string{
		`<button> Add to cart`:           "Add to cart",
		`<a href="/account"> My account`: "My account",
		`<input type=text name=email placeholder="Email address"> value="ana@example.com"`: "Email address",
		`<input type=password name=pw> value=••••`:                                         "pw",
		`<button aria-label="Close dialog">`:                                               "Close dialog",
		`<a href="/"> img: Shop logo`:                                                      "Shop logo",
		`<select name=size> options: S | *M | L`:                                           "size",
		`<div role=button title="More options">`:                                           "More options",
	} {
		if got := elementLabel(in); got != want {
			t.Errorf("elementLabel(%q) = %q, want %q", in, got, want)
		}
	}
	ps := parsePage(page("", "https://x.example/a?token=1", "X", `<button> Pay`, `<button> Cancel (in frame "checkout")`).Text)
	if ps.url != "https://x.example/a" || ps.label(2) != "Cancel" || ps.label(3) != "" {
		t.Errorf("page = %q, label 2 = %q", ps.url, ps.label(2))
	}
}

// The JSON the screen reads, field for field (web/src/api/progress.ts and
// types.ts).
func TestWireShapes(t *testing.T) {
	ok, plan, attempt := true, 1, 2
	for _, c := range []struct {
		v    any
		want string
	}{
		{View{Status: "working", Activity: "Thinking", Phase: "tool", RunStartedAt: 1700000000000, Follow: true,
			Plan: &Plan{Title: "Trip", Steps: []PlanStep{{Text: "Flights", Status: "done"}, {Text: "Hotel", Status: "active", Note: "Alfama"}}}},
			`{"status":"working","activity":"Thinking","surface":{"kind":""},"control":false,"contextTokens":0,"compactions":0,"phase":"tool","runStartedAt":1700000000000,"plan":{"title":"Trip","steps":[{"text":"Flights","status":"done"},{"text":"Hotel","status":"active","note":"Alfama"}]},"follow":true,"queued":0}`},
		{View{Status: "idle"}, `{"status":"idle","activity":"","surface":{"kind":""},"control":false,"contextTokens":0,"compactions":0,"follow":false,"queued":0}`},
		{Surface{Kind: "entry", ID: "42", Held: true}, `{"kind":"entry","id":"42","held":true}`},
		{StepData{Tool: "browser_click", Kind: "click", Status: "failed", Host: "shop.example", URL: "https://shop.example/p", Target: "Add to cart",
			MS: 812, OK: new(bool), Error: "Element not found", Plan: &plan, Attempt: attempt, Sources: []Source{{Title: "Shop", URL: "https://shop.example/p"}}},
			`{"tool":"browser_click","kind":"click","status":"failed","host":"shop.example","url":"https://shop.example/p","target":"Add to cart","ms":812,"ok":false,"error":"Element not found","plan":1,"attempt":2,"sources":[{"title":"Shop","url":"https://shop.example/p"}]}`},
		{StepData{Tool: "web_search", Kind: "search", Status: "done", Query: "q", OK: &ok}, `{"tool":"web_search","kind":"search","status":"done","query":"q","ok":true}`},
		{runEvent{RunID: "r1", At: 5, Phase: "tool", Tool: &runTool{Name: "browser_open", Label: "Opening United", Kind: "browse", Host: "united.com"}},
			`{"runId":"r1","at":5,"phase":"tool","tool":{"name":"browser_open","label":"Opening United","kind":"browse","host":"united.com"}}`},
		{runEvent{RunID: "r1", At: 6, Phase: "retrying", Retry: &runRetry{Attempt: 1, Of: 3, Reason: "rate_limited", At: 9}},
			`{"runId":"r1","at":6,"phase":"retrying","retry":{"attempt":1,"of":3,"reason":"rate_limited","at":9}}`},
		{runEvent{RunID: "r1", At: 7, Phase: "ended", Outcome: "done"}, `{"runId":"r1","at":7,"phase":"ended","outcome":"done"}`},
	} {
		if got := string(marshal(c.v)); got != c.want {
			t.Errorf("%T:\n got %s\nwant %s", c.v, got, c.want)
		}
	}
}

// Compacting while a question waits recites the plan after the answer, not
// between the question and its answer, which the model service refuses.
func TestRecitedPlanWaitsForTheAnswer(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("say", map[string]any{"text": "Hi Ana.", "final": true})), does())
	e.send("Hi")
	e.settle()
	ask := fc("ask", map[string]any{"question": "Which dates?", "options": []string{"May", "June"}})
	e.model.script(does(fc("plan", planArgs("Book a hotel in Lisbon", "active:Pick the dates", "pending:Book it")), ask))
	e.send("Book me a hotel in Lisbon")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q, want needs", v.Status)
	}
	if err := e.rt.Compact(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	var input []json.RawMessage
	e.model.script(func(req *llm.Request) []any {
		input = req.Input
		return []any{fc("say", map[string]any{"text": "May it is.", "final": true})}
	}, does())
	e.send("May")
	e.settle()
	if ss, _ := e.st.GetSession(e.ctx, e.user.ID); ss.Compactions != 1 {
		t.Fatalf("compactions = %d, want 1", ss.Compactions)
	}
	if bad := callsInterrupted(input); bad != "" {
		t.Errorf("the model was sent %s", bad)
	}
	// Stored in that order too: the answer, then the reminder.
	conv := e.conversation()
	answer, recite := strings.Index(conv, "The user replied"), strings.Index(conv, "The conversation was just summarised")
	if answer < 0 || recite < answer {
		t.Errorf("the answer is at %d and the plan's reminder at %d, want the answer first", answer, recite)
	}
}

// callsInterrupted describes the first message the input puts between a
// tool call and its output, or returns "".
func callsInterrupted(input []json.RawMessage) string {
	open := map[string]bool{}
	for _, raw := range input {
		it := llm.ParseItem(raw)
		switch {
		case it.Type == "function_call":
			open[it.CallID] = true
		case it.Type == "function_call_output":
			delete(open, it.CallID)
		case len(open) > 0:
			return fmt.Sprintf("a message between calls and their outputs: %s", raw)
		}
	}
	return ""
}
