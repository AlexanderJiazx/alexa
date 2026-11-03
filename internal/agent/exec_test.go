package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"alexagent/internal/llm"
)

// A refused question keeps what was batched after it from running: the
// action it was to confirm waits for a question that really goes out (H1).
func TestRefusedAskStopsTheRest(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	var c ran
	e.counted(&c, "browser_click", page("Clicked [5]", "https://shop.example/done", "Order placed"))
	click := fc("browser_click", map[string]any{"index": 5, "status": "Placing the order"})
	tooLong := fc("ask", map[string]any{"question": "Shall I place the order? " + strings.Repeat("It has the headphones and the case. ", 14), "confirm": true})
	b := e.rt.runBatch(r.ctx, r, batch(tooLong, click), false)
	if b.suspend != nil || b.err != nil {
		t.Fatalf("batch = %+v, want the question refused", b)
	}
	if c.of("browser_click") != 0 {
		t.Error("the click after the refused question ran")
	}
	if out := e.outputs()[click.(map[string]any)["call_id"].(string)]; out != notRunUnasked {
		t.Errorf("the click's output = %q", out)
	}
}

// A browser step waiting for the user to hand the browser back ends when
// they write: it was chosen without their message, and doesn't run after
// they hand back (steer-skip).
func TestMessageEndsTheWaitForControl(t *testing.T) {
	e := newProgressEnv(t)
	var c ran
	e.counted(&c, "browser_click", page("Clicked [5]", "https://shop.example/done", "Order placed"))
	click := fc("browser_click", map[string]any{"index": 5, "status": "Placing the order"})
	var input string
	e.model.script(
		does(click),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "I won't buy them.", "final": true})}
		},
		does(),
	)
	if err := e.rt.TakeOver(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	e.send("Buy the headphones")
	e.waitFor("the click to wait for the user", func(v View) bool { return v.Activity == "You’re in control" })
	if err := e.rt.Interrupt(e.ctx, e.user, Message{Text: "Stop, don't buy them"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if err := e.rt.HandBack(e.ctx, e.user, ""); err != nil {
		t.Fatal(err)
	}
	if c.of("browser_click") != 0 {
		t.Error("the click chosen before the message ran")
	}
	if out := e.outputs()[click.(map[string]any)["call_id"].(string)]; out != skippedMessage {
		t.Errorf("the click's output = %q", out)
	}
	if x, _ := stepNamed(t, e.feed(), "Placing the order"); x.Status != "skipped" {
		t.Errorf("its step is %q, want skipped", x.Status)
	}
	if !strings.Contains(input, "Stop, don't buy them") {
		t.Errorf("the model saw:\n%s", tailOf(input, 600))
	}
}

// A call skipped under the label of the step before gets a skipped step of
// its own: the step's earlier call ran, and stays done.
func TestSkipKeepsTheStepThatRan(t *testing.T) {
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
	done := make(chan batchEnd)
	go func() {
		done <- e.rt.runBatch(r.ctx, r, batch(
			fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Checking the timetable"}),
			fc("write_file", map[string]any{"path": "times.txt", "content": "8:15", "status": "Checking the timetable"})), false)
	}()
	<-started
	e.send("Actually, check Friday instead")
	close(release)
	<-done
	var got []string
	for _, x := range entriesOf(e.feed(), "step") {
		if x.Text == "Checking the timetable" {
			got = append(got, x.Status)
		}
	}
	if strings.Join(got, ",") != "done,skipped" || c.of("write_file") != 0 {
		t.Errorf("steps = %v (write ran %d), want the fetch done and the write skipped", got, c.of("write_file"))
	}
}

// A browser action that starts once the user has taken the browser (they
// took over just after it stopped waiting for them) is cut at once: it
// can't act on the page they now control (B06).
func TestBrowserActionAfterTakeOverIsCut(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	live := 0
	e.ctxTool("browser_type", func(ctx context.Context, _ map[string]any) (*toolResult, error) {
		if ctx.Err() == nil {
			live++
		}
		return nil, ctx.Err()
	})
	r.mu.Lock()
	r.released = make(chan struct{}) // TakeOver, a moment too late to cut it
	r.mu.Unlock()
	res := e.rt.runCall(r.ctx, r, e.rt.toolMap["browser_type"], map[string]any{"index": 3, "text": "Ana"})
	if live != 0 {
		t.Error("the typing ran while the user had the browser")
	}
	if !strings.Contains(res.Text, "the user took control of the browser") {
		t.Errorf("result = %q", res.Text)
	}
	select {
	case <-r.ctx.Done():
		t.Error("the run was stopped")
	case <-time.After(10 * time.Millisecond):
	}
}
