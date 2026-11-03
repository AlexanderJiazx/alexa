package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alexagent/internal/llm"
)

// Tests of a batch of tool calls (WP-14): nothing runs after a question or
// before it that would act, nothing aimed at a page that changed, nothing
// new once the user has written, and a final message ends the run.

// batchRun is a run the test drives itself, standing in for the loop's
// goroutine, so runBatch can be tested on its own.
func (e *progressEnv) batchRun() *run {
	e.t.Helper()
	env, err := e.rt.envs.Ensure(e.user.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	r := &run{user: e.user, env: env, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), awaiting: true}
	e.rt.mu.Lock()
	e.rt.runs[e.user.ID] = r
	e.rt.mu.Unlock()
	e.t.Cleanup(func() { cancel(); e.rt.removeRun(r) })
	return r
}

// batch turns model tool calls (fc) into what runBatch takes.
func batch(calls ...any) []llm.Item {
	var out []llm.Item
	for _, c := range calls {
		out = append(out, llm.ParseItem(marshal(c)))
	}
	return out
}

// outputs maps each call id to the output saved for it.
func (e *progressEnv) outputs() map[string]string {
	e.t.Helper()
	rows, err := e.st.Items(e.ctx, e.user.ID, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]string{}
	for _, row := range rows {
		var si storedItem
		_ = json.Unmarshal(row.JSON, &si)
		if it := llm.ParseItem(si.Item); it.Type == "function_call_output" {
			var o struct {
				Output string `json:"output"`
			}
			_ = json.Unmarshal(si.Item, &o)
			out[it.CallID] = o.Output
		}
	}
	return out
}

// ran counts the calls of fake tools.
type ran struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *ran) add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[name]++
}

func (c *ran) of(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[name]
}

func (e *progressEnv) counted(c *ran, name string, res *toolResult) {
	e.fakeTool(name, func(map[string]any) (*toolResult, error) { c.add(name); return res, nil })
}

func askArgs() map[string]any {
	return map[string]any{"question": "Shall I place the order for $400?", "options": []string{"Yes", "No"}}
}

// CallsAfterAsk (H1, acceptance 1): in [ask, write_file, browser_click]
// only the ask runs; the others are "Not run", and Alexa reads that with
// the answer.
func TestCallsAfterAskDontRun(t *testing.T) {
	e := newProgressEnv(t)
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved order.txt"})
	e.counted(&c, "browser_click", page("Clicked [5]", "https://shop.example/cart", "Cart"))
	write := fc("write_file", map[string]any{"path": "order.txt", "content": "ORDER PLACED", "status": "Placing the order"})
	click := fc("browser_click", map[string]any{"index": 5, "status": "Clicking Buy"})
	e.model.script(does(fc("ask", askArgs()), write, click))
	e.send("Buy the headphones")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q, want needs", v.Status)
	}
	if c.of("write_file")+c.of("browser_click") > 0 {
		t.Fatalf("calls after the ask ran before the answer: %v", c.n)
	}
	outs := e.outputs()
	for _, call := range []any{write, click} {
		id := call.(map[string]any)["call_id"].(string)
		if outs[id] != notRunAsked {
			t.Errorf("output of %s = %q, want %q", id, outs[id], notRunAsked)
		}
	}
	var input string
	e.model.script(func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "Ordered.", "final": true})}
	}, does())
	e.send("Yes")
	e.settle()
	if strings.Count(input, jsonText(notRunAsked)) != 2 || !strings.Contains(input, "The user replied") {
		t.Errorf("after the answer the model saw:\n%s", tailOf(input, 800))
	}
}

// H1/L7: an action batched before an ask waits for the answer too; what
// only reads or speaks runs.
func TestActionBeforeAskWaits(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved order.txt"})
	e.counted(&c, "read_file", &toolResult{Text: "headphones: $400"})
	write := fc("write_file", map[string]any{"path": "order.txt", "content": "ORDER PLACED", "status": "Placing the order"})
	read := fc("read_file", map[string]any{"path": "prices.txt"})
	b := e.rt.runBatch(r.ctx, r, batch(read, write, fc("say", map[string]any{"text": "One question first."}), fc("ask", askArgs())), false)
	if b.suspend == nil || b.err != nil {
		t.Fatalf("batch = %+v, want it to wait for the answer", b)
	}
	if c.of("write_file") != 0 || c.of("read_file") != 1 {
		t.Errorf("ran %v, want only the read", c.n)
	}
	if out := e.outputs()[write.(map[string]any)["call_id"].(string)]; out != notRunAsking {
		t.Errorf("the write's output = %q", out)
	}
	if says := entriesOf(e.feed(), "say"); len(says) != 1 {
		t.Errorf("the say before the ask didn't run: %v", says)
	}
}

// The same through the loop: the user is asked, and nothing was saved.
func TestActionBeforeAskWaitsInTheLoop(t *testing.T) {
	e := newProgressEnv(t)
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved order.txt"})
	e.model.script(does(fc("write_file", map[string]any{"path": "order.txt", "content": "ORDER PLACED"}), fc("ask", askArgs())))
	e.send("Buy the headphones")
	if v := e.settle(); v.Status != "needs" || c.of("write_file") != 0 {
		t.Errorf("status %q, write ran %d times", v.Status, c.of("write_file"))
	}
}

var shop = []string{`<a href="/"> Home`, `<input type=search name=q placeholder="Search the shop"> value=""`, `<button> Search`,
	`<a href="/p/1"> Sony WH-1000XM6`, `<button aria-label="Add Sony WH-1000XM6 to cart"> Add to cart`, `<a href="/p/2"> Bose QC Ultra`, `<button aria-label="Add Bose QC Ultra to cart"> Add to cart`}

// H2 (acceptance 2): [click(5) that navigates, click(7)] runs only the
// first; the second gets the barrier's message.
func TestPageChangeBarrier(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	var c ran
	e.counted(&c, "browser_open", page("Opened shop.example", "https://shop.example/", "Shop", shop...))
	e.fakeTool("browser_click", func(a map[string]any) (*toolResult, error) {
		c.add("click " + str(a, "index"))
		return page("Clicked", "https://shop.example/cart", "Your cart", `<a href="/"> Home`, `<button> Check out`), nil
	})
	e.rt.runBatch(r.ctx, r, batch(fc("browser_open", map[string]any{"url": "shop.example", "status": "Opening the shop"})), false)
	second := fc("browser_click", map[string]any{"index": 7, "status": "Adding the Bose"})
	b := e.rt.runBatch(r.ctx, r, batch(fc("browser_click", map[string]any{"index": 5, "status": "Adding the Sony"}), second), false)
	if b.err != nil || b.suspend != nil {
		t.Fatalf("batch = %+v", b)
	}
	if c.of("click 5") != 1 || c.of("click 7") != 0 {
		t.Errorf("clicks = %v, want only [5]", c.n)
	}
	if out := e.outputs()[second.(map[string]any)["call_id"].(string)]; out != skippedPage {
		t.Errorf("the second click's output = %q", out)
	}
}

// The barrier doesn't stop a form being filled in one batch: typing into
// fields leaves the elements where they were.
func TestFormFillPassesTheBarrier(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	form := func(name, email string) *toolResult {
		return page("", "https://shop.example/checkout", "Checkout", `<input type=text name=name> value="`+name+`"`,
			`<input type=email name=email> value="`+email+`"`, `<input type=checkbox name=terms unchecked>`, `<button> Place order`)
	}
	e.fakeTool("browser_open", func(map[string]any) (*toolResult, error) { return form("", ""), nil })
	var name, email atomic.Value
	name.Store("")
	email.Store("")
	var c ran
	e.fakeTool("browser_type", func(a map[string]any) (*toolResult, error) {
		c.add("type")
		if num(a, "index", 0) == 1 {
			name.Store(str(a, "text"))
		} else {
			email.Store(str(a, "text"))
		}
		return form(name.Load().(string), email.Load().(string)), nil
	})
	e.rt.runBatch(r.ctx, r, batch(fc("browser_open", map[string]any{"url": "shop.example/checkout", "status": "Opening checkout"})), false)
	e.rt.runBatch(r.ctx, r, batch(
		fc("browser_type", map[string]any{"index": 1, "text": "Ana"}),
		fc("browser_type", map[string]any{"index": 2, "text": "ana@example.com"}),
	), false)
	if c.of("type") != 2 {
		t.Errorf("typed %d times, want 2", c.of("type"))
	}
}

func TestPageSignature(t *testing.T) {
	a := page("", "https://shop.example/checkout?step=1", "Checkout", `<input type=text name=name> value=""`, `<input type=checkbox name=terms unchecked>`, `<button disabled> Place order`, `<button expanded=false> More`)
	b := page("Typed into [1]", "https://shop.example/checkout?step=1", "Checkout — 1 item", `<input type=text name=name> value="Ana"`, `<input type=checkbox name=terms checked>`, `<button> Place order`, `<button expanded=true> More`)
	if pageSignature(a.Text) == "" || pageSignature(a.Text) != pageSignature(b.Text) {
		t.Errorf("filling a form changed the signature:\n%s\n%s", pageSignature(a.Text), pageSignature(b.Text))
	}
	c := page("", "https://shop.example/checkout?step=1", "Checkout", `<input type=text name=name> value=""`, `<ul role=listbox> Ana Lopez`, `<input type=checkbox name=terms unchecked>`)
	if pageSignature(a.Text) == pageSignature(c.Text) {
		t.Error("an element appearing kept the signature")
	}
	if pageSignature("Read 2 paragraphs.") != "" {
		t.Error("a text without elements has a signature")
	}
}

func TestAimed(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want bool
	}{
		{"browser_click", map[string]any{"index": 7.0}, true},
		{"browser_type", map[string]any{"index": 0.0, "text": "hi"}, false}, // the focused element
		{"browser_click_at", map[string]any{"x": 10.0, "y": 20.0}, true},
		{"browser_drag", map[string]any{"from_index": 2.0, "to_x": 5.0, "to_y": 5.0}, true},
		{"browser_key", map[string]any{"key": "Enter"}, false},
		{"browser_scroll", map[string]any{"direction": "down"}, false},
		{"write_file", map[string]any{"index": 3.0}, false},
	} {
		if got := aimed(c.name, c.args); got != c.want {
			t.Errorf("aimed(%s, %v) = %v", c.name, c.args, got)
		}
	}
	if needsWatch(batch(fc("browser_click", map[string]any{"index": 3}))) {
		t.Error("one click needs no barrier")
	}
	if !needsWatch(batch(fc("browser_scroll", map[string]any{"direction": "down"}), fc("browser_click", map[string]any{"index": 3}))) {
		t.Error("a click after a scroll needs the barrier")
	}
}

// H3: a final say or report that goes out ends the batch's work; one that
// isn't last, isn't final or didn't go out doesn't.
func TestFinalBatch(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	final := func(args map[string]any) map[string]any { args["final"] = true; return args }
	if b := e.rt.runBatch(r.ctx, r, batch(fc("say", final(map[string]any{"text": "All done."}))), false); !b.final {
		t.Error("a final say didn't end the batch")
	}
	if b := e.rt.runBatch(r.ctx, r, batch(fc("say", map[string]any{"text": "Looking now."})), false); b.final {
		t.Error("a say without final ended the batch")
	}
	e.counted(&ran{}, "fetch_url", &toolResult{Text: "ok"})
	if b := e.rt.runBatch(r.ctx, r, batch(fc("say", final(map[string]any{"text": "Done."})), fc("fetch_url", map[string]any{"url": "https://example.com"})), false); b.final {
		t.Error("a final say before another call ended the batch")
	}
	if !isFinal("present_report", map[string]any{"final": true}, &toolResult{Text: "Shown the report “Lisbon”."}) {
		t.Error("a final report isn't final")
	}
	if isFinal("say", map[string]any{"final": true}, &toolResult{Text: "Sent.\n\n" + planReminderLead + ": “Pay it”."}) {
		t.Error("a final say with the plan's reminder is final")
	}
	if isFinal("say", map[string]any{"final": true}, &toolResult{Text: "Not sent: the message was empty."}) {
		t.Error("a say that didn't go out is final")
	}
}

// H3 (acceptance 3), through the loop: a final say with nothing waiting
// ends the run without another model call; with a message waiting the run
// goes on to it.
func TestFinalSavesAModelCall(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("say", map[string]any{"text": "The 8:15 runs on time.", "final": true})))
	e.send("When does the 8:15 leave?")
	e.settle()
	if n := len(e.model.requests()); n != 1 {
		t.Errorf("%d model calls, want 1", n)
	}

	var once sync.Once
	say := e.rt.toolMap["say"]
	e.rt.toolMap["say"] = &tool{Name: "say", NoStatus: true, Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
		once.Do(func() {
			// Arrives while the say goes out.
			go func() { _ = e.rt.Send(e.ctx, e.user, Message{Text: "And the 9:15?"}) }()
			time.Sleep(50 * time.Millisecond)
		})
		return say.Run(tc, a)
	}}
	var input string
	e.model.script(
		does(fc("say", map[string]any{"text": "The 8:15 runs on time.", "final": true})),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "The 9:15 too.", "final": true})}
		},
	)
	e.send("And today?")
	e.settle()
	if !strings.Contains(input, "And the 9:15?") {
		t.Errorf("the message sent during the final say wasn't answered:\n%s", tailOf(input, 600))
	}
}

// The wrap-up turn only lets Alexa speak.
func TestWrapUpBatch(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	b := e.rt.runBatch(r.ctx, r, batch(fc("write_file", map[string]any{"path": "a.txt", "content": "x"}), fc("say", map[string]any{"text": "Here’s where things stand."})), true)
	if !b.told || c.of("write_file") != 0 {
		t.Errorf("batch = %+v, writes %d", b, c.of("write_file"))
	}
}

// L1: Stop mid-batch keeps every output: the call it cut short says so,
// and the calls it kept from starting say why, so nothing is "context
// canceled" and nothing is left to the replay's guess.
func TestStopMidBatchKeepsOutputs(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	started, _ := e.blockingTool("fetch_url")
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	first := fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"})
	second := fc("write_file", map[string]any{"path": "times.txt", "content": "8:15"})
	done := make(chan batchEnd)
	go func() { done <- e.rt.runBatch(r.ctx, r, batch(first, second), false) }()
	<-started
	_ = e.rt.Stop(e.ctx, e.user)
	var b batchEnd
	select {
	case b = <-done:
	case <-time.After(time.Second):
		t.Fatal("the batch didn't stop")
	}
	if b.err != nil {
		t.Fatalf("saving the outputs failed: %v", b.err)
	}
	outs := e.outputs()
	if out := outs[first.(map[string]any)["call_id"].(string)]; !strings.HasPrefix(out, "Stopped: the user stopped you while this was running") {
		t.Errorf("the cut call's output = %q", out)
	}
	if out := outs[second.(map[string]any)["call_id"].(string)]; out != notRunStopped || c.of("write_file") != 0 {
		t.Errorf("the next call's output = %q (ran %d)", out, c.of("write_file"))
	}
}

// M3 (acceptance 4): a scheduled job that fires while a question waits
// neither answers nor closes it; it runs after the user answers, and the
// conversation has the answer before the job.
func TestScheduledJobWaitsForTheAnswer(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("ask", map[string]any{"question": "Friday or Saturday?", "options": []string{"Friday", "Saturday"}})))
	e.send("Book a table for two")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q", v.Status)
	}
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Check UA 1180's status", Source: "schedule", Label: "Flight watch"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if v := e.rt.View(e.ctx, e.user.ID); v.Status != "needs" || e.rt.pendingNeeds(e.ctx, e.user.ID) == nil {
		t.Fatalf("the job touched the question: status %q", v.Status)
	}
	if n := len(e.model.requests()); n != 1 {
		t.Fatalf("the job ran while the question waited (%d model calls)", n)
	}
	var input string
	e.model.script(func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "Booked Friday. UA 1180 is on time.", "final": true})}
	}, does())
	e.send("Friday")
	e.settle()
	answer, job := strings.Index(input, "The user replied"), strings.Index(input, "Check UA 1180")
	if answer < 0 || job < 0 || answer > job {
		t.Errorf("answer at %d, job at %d:\n%s", answer, job, tailOf(input, 800))
	}
}

// SuspendTakesSystemNote (L6): a note that came while Alexa was asking
// (the user handed the browser back) isn't the answer; the question is
// shown.
func TestSuspendIgnoresANote(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	e.model.script(g.turn(fc("ask", askArgs())))
	e.send("Find me a deal")
	g.wait()
	_ = e.rt.TakeOver(e.ctx, e.user)
	_ = e.rt.HandBack(e.ctx, e.user, "")
	g.open()
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q: the note answered the question", v.Status)
	}
	if asks := entriesOf(e.feed(), "ask"); len(asks) != 1 {
		t.Errorf("asks = %v", asks)
	}
	var input string
	e.model.script(func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "Ordered.", "final": true})}
	}, does())
	e.send("Yes")
	e.settle()
	if strings.Contains(input, "Before seeing this") {
		t.Error("the note was taken as the answer")
	}
	answer, note := strings.Index(input, "The user replied: "), strings.Index(input, "handed it back")
	if answer < 0 || note < 0 || answer > note {
		t.Errorf("answer at %d, note at %d", answer, note)
	}
}

// L6: a message the user sent while the question was being asked, before
// they could see it, is the answer.
func TestMessageDuringAskIsTheAnswer(t *testing.T) {
	e := newProgressEnv(t)
	ask := e.rt.toolMap["ask"]
	e.rt.toolMap["ask"] = &tool{Name: "ask", NoStatus: true, Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
		res, err := ask.Run(tc, a)
		go func() { _ = e.rt.Send(e.ctx, e.user, Message{Text: "Yes, go ahead"}) }()
		time.Sleep(50 * time.Millisecond)
		return res, err
	}}
	var input string
	e.model.script(does(fc("ask", askArgs())), func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "Ordered.", "final": true})}
	}, does())
	e.send("Buy the headphones")
	if v := e.settle(); v.Status != "idle" {
		t.Errorf("status = %q, want idle", v.Status)
	}
	if !strings.Contains(input, jsonText("Before seeing this, the user wrote:\n")) || !strings.Contains(input, "Yes, go ahead") {
		t.Errorf("the message wasn't the answer:\n%s", tailOf(input, 600))
	}
}

// CompactWhilePending (L5): compacting while a question waits keeps the
// question, so its answer still has its call.
func TestCompactWhilePending(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("ask", map[string]any{"question": "Which one?", "options": []string{"A", "B"}})))
	e.send("Pick something")
	e.settle()
	for range 2 {
		if err := e.rt.Compact(e.ctx, e.user); err != nil {
			t.Fatal(err)
		}
	}
	var orphans []string
	e.model.script(func(req *llm.Request) []any {
		calls := map[string]bool{}
		for _, raw := range req.Input {
			switch it := llm.ParseItem(raw); it.Type {
			case "function_call":
				calls[it.CallID] = true
			case "function_call_output":
				if !calls[it.CallID] {
					orphans = append(orphans, it.CallID)
				}
			}
		}
		return []any{fc("say", map[string]any{"text": "A it is.", "final": true})}
	}, does())
	e.send("A")
	e.settle()
	if len(orphans) > 0 {
		t.Errorf("outputs without their call: %v", orphans)
	}
}

func TestKeepPending(t *testing.T) {
	items := []storedItem{userMsg(), modelMsg(), call("c1"), output("c1"), userMsg(), reasoning(), call("c2"), call("ask"), output("c2")}
	if got := keepPending(items, len(items), "ask"); got != 5 {
		t.Errorf("cut = %d, want 5 (before the response with the question)", got)
	}
	if got := keepPending(items, 3, "ask"); got != 3 {
		t.Errorf("a cut before the question moved to %d", got)
	}
	if got := keepPending(items[6:], 3, "ask"); got != 0 {
		t.Errorf("cut = %d, want 0", got)
	}
}

// Steer-skip: a message that arrives mid-batch skips what hasn't started,
// and the step shows it.
func TestSteerSkipsTheRest(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	started, release := make(chan struct{}), make(chan struct{})
	e.fakeTool("fetch_url", func(map[string]any) (*toolResult, error) {
		close(started)
		<-release
		return &toolResult{Text: "The 8:15 runs on time."}, nil
	})
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	write := fc("write_file", map[string]any{"path": "times.txt", "content": "8:15", "status": "Saving the times"})
	done := make(chan batchEnd)
	go func() {
		done <- e.rt.runBatch(r.ctx, r, batch(fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"}), write), false)
	}()
	<-started
	e.send("Actually, check Friday instead")
	close(release)
	<-done
	if c.of("write_file") != 0 {
		t.Error("the call after the new message ran")
	}
	feed := e.feed()
	if x, _ := stepNamed(t, feed, "Saving the times"); x.Status != "skipped" {
		t.Errorf("the skipped step is %q", x.Status)
	}
	if x, _ := stepNamed(t, feed, "Reading the timetable"); x.Status != "done" {
		t.Errorf("the step that ran is %q", x.Status)
	}
	if out := e.outputs()[write.(map[string]any)["call_id"].(string)]; out != skippedMessage {
		t.Errorf("the skipped call's output = %q", out)
	}
}

// A message that came while the model was writing the batch doesn't skip
// it: the batch runs, and Alexa reads the message next turn.
func TestMessageDuringModelCallDoesntSkip(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	var input string
	e.model.script(
		g.turn(fc("say", map[string]any{"text": "On it."})),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "Trains too, got it.", "final": true})}
		},
		does(),
	)
	e.send("Find flights to Porto")
	g.wait()
	e.send("Also check trains")
	g.open()
	e.settle()
	if says := entriesOf(e.feed(), "say"); len(says) != 2 || says[0].Text != "On it." {
		t.Errorf("says = %v", says)
	}
	if !strings.Contains(input, "Also check trains") || strings.Contains(input, skippedMessage) {
		t.Errorf("the model saw:\n%s", tailOf(input, 600))
	}
}

// The same through the loop: the model hears what was skipped, and why.
func TestSteerSkipsInTheLoop(t *testing.T) {
	e := newProgressEnv(t)
	started, release := make(chan struct{}), make(chan struct{})
	e.fakeTool("fetch_url", func(map[string]any) (*toolResult, error) {
		close(started)
		<-release
		return &toolResult{Text: "The 8:15 runs on time."}, nil
	})
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	var input string
	e.model.script(
		does(fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"}),
			fc("write_file", map[string]any{"path": "times.txt", "content": "8:15", "status": "Saving the times"})),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "Friday’s first train is at 7:40.", "final": true})}
		},
		does(),
	)
	e.send("When does the 8:15 leave?")
	<-started
	e.send("Actually, check Friday instead")
	close(release)
	e.settle()
	if c.of("write_file") != 0 {
		t.Error("the call after the new message ran")
	}
	if !strings.Contains(input, skippedMessage) || !strings.Contains(input, "Actually, check Friday instead") {
		t.Errorf("the model saw:\n%s", tailOf(input, 800))
	}
}

// slowFetch makes fetch_url run until its call is cut short.
func (e *progressEnv) slowFetch() (started chan struct{}) {
	started = make(chan struct{})
	var once sync.Once
	e.ctxTool("fetch_url", func(ctx context.Context, _ map[string]any) (*toolResult, error) {
		once.Do(func() { close(started) })
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return &toolResult{Text: "slow"}, nil
		}
	})
	return started
}

// POST /api/interrupt: the message goes in and the step in flight ends at
// once, and Alexa hears why.
func TestInterruptCutsTheStep(t *testing.T) {
	e := newProgressEnv(t)
	started := e.slowFetch()
	var input string
	e.model.script(
		does(fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"})),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "Friday then.", "final": true})}
		},
		does(),
	)
	e.send("When does the 8:15 leave?")
	<-started
	t0 := time.Now()
	if err := e.rt.Interrupt(e.ctx, e.user, Message{Text: "Stop that, check Friday"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if d := time.Since(t0); d > 2*time.Second {
		t.Errorf("the interrupt took %s", d)
	}
	feed := e.feed()
	if x, _ := stepNamed(t, feed, "Reading the timetable"); x.Status != "interrupted" {
		t.Errorf("the cut step is %q", x.Status)
	}
	if !strings.Contains(input, "the user sent a new message while this was running") || !strings.Contains(input, "Stop that, check Friday") {
		t.Errorf("the model saw:\n%s", tailOf(input, 800))
	}
	if users := entriesOf(feed, "user"); len(users) != 2 {
		t.Errorf("user entries = %v", users)
	}
	noContextCanceled(t, e)
}

// An interrupt sent while Alexa is still thinking skips the batch she was
// writing without it, where an ordinary message wouldn't
// (TestMessageDuringModelCallDoesntSkip), and only that batch.
func TestInterruptWhileThinkingSkipsTheBatch(t *testing.T) {
	e := newProgressEnv(t)
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	g := newGate()
	write := fc("write_file", map[string]any{"path": "times.txt", "content": "8:15", "status": "Saving the times"})
	var input string
	e.model.script(
		g.turn(write),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("write_file", map[string]any{"path": "friday.txt", "content": "7:40", "status": "Saving Friday’s times"})}
		},
		does(fc("say", map[string]any{"text": "Friday’s first train is at 7:40.", "final": true})),
	)
	e.send("When does the 8:15 leave?")
	g.wait()
	if err := e.rt.Interrupt(e.ctx, e.user, Message{Text: "Stop that, check Friday"}); err != nil {
		t.Fatal(err)
	}
	g.open()
	e.settle()
	if c.of("write_file") != 1 {
		t.Errorf("write_file ran %d times, want once (after the interrupt was read)", c.of("write_file"))
	}
	if out := e.outputs()[write.(map[string]any)["call_id"].(string)]; out != skippedMessage {
		t.Errorf("the batch written before the interrupt gave %q", out)
	}
	if x, _ := stepNamed(t, e.feed(), "Saving the times"); x.Status != "skipped" {
		t.Errorf("its step is %q, want skipped", x.Status)
	}
	if !strings.Contains(input, "Stop that, check Friday") || !strings.Contains(input, skippedMessage) {
		t.Errorf("the model saw:\n%s", tailOf(input, 800))
	}
}

// An interrupt also skips the rest of the batch.
func TestInterruptSkipsTheRest(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	started := e.slowFetch()
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	done := make(chan batchEnd)
	go func() {
		done <- e.rt.runBatch(r.ctx, r, batch(fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"}),
			fc("write_file", map[string]any{"path": "times.txt", "content": "8:15", "status": "Saving the times"})), false)
	}()
	<-started
	if err := e.rt.Interrupt(e.ctx, e.user, Message{Text: "Stop that, check Friday"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the batch went on")
	}
	feed := e.feed()
	if x, _ := stepNamed(t, feed, "Reading the timetable"); x.Status != "interrupted" {
		t.Errorf("the cut step is %q", x.Status)
	}
	if x, _ := stepNamed(t, feed, "Saving the times"); x.Status != "skipped" || c.of("write_file") != 0 {
		t.Errorf("the next step is %q (ran %d)", x.Status, c.of("write_file"))
	}
}
