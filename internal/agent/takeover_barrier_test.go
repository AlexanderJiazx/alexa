package agent

import (
	"testing"
	"time"
)

// A take-over while Alexa writes her turn: after hand back, the calls she
// aimed at an index or a point of the page as it was don't run (H2).
func TestTakeOverBeforeTheBatchSkipsAimedCalls(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	var c ran
	e.counted(&c, "browser_open", page("Opened", "https://shop.example/", "Shop", shop...))
	e.rt.runBatch(r.ctx, r, batch(fc("browser_open", map[string]any{"url": "shop.example"})), false)
	e.counted(&c, "browser_click_at", page("Clicked", "https://shop.example/", "Shop", shop...))
	e.counted(&c, "browser_click", page("Clicked", "https://shop.example/", "Shop", shop...))
	if err := e.rt.TakeOver(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = e.rt.HandBack(e.ctx, e.user, "")
	}()
	clickAt := fc("browser_click_at", map[string]any{"x": 400, "y": 300})
	e.rt.runBatch(r.ctx, r, batch(clickAt, fc("browser_click", map[string]any{"index": 5})), false)
	if c.of("browser_click_at") != 0 || c.of("browser_click") != 0 {
		t.Errorf("calls aimed at the page before the take-over ran: click_at %d, click %d", c.of("browser_click_at"), c.of("browser_click"))
	}
	if out := e.outputs()[clickAt.(map[string]any)["call_id"].(string)]; out != skippedControl {
		t.Errorf("the click's output = %q", out)
	}
}

// The same for a take-over in the middle of a batch.
func TestTakeOverMidBatchSkipsAimedCalls(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	var c ran
	e.counted(&c, "browser_open", page("Opened", "https://shop.example/", "Shop", shop...))
	e.rt.runBatch(r.ctx, r, batch(fc("browser_open", map[string]any{"url": "shop.example"})), false)
	in, goOn := make(chan struct{}), make(chan struct{})
	e.fakeTool("fetch_url", func(map[string]any) (*toolResult, error) {
		close(in)
		<-goOn
		return &toolResult{Text: "HTTP 200"}, nil
	})
	e.counted(&c, "browser_type", page("Typed", "https://shop.example/", "Shop", shop...))
	e.counted(&c, "browser_click", page("Clicked", "https://shop.example/", "Shop", shop...))
	go func() {
		<-in
		_ = e.rt.TakeOver(e.ctx, e.user)
		close(goOn)
		time.Sleep(200 * time.Millisecond)
		_ = e.rt.HandBack(e.ctx, e.user, "")
	}()
	e.rt.runBatch(r.ctx, r, batch(
		fc("browser_type", map[string]any{"index": 2, "text": "headphones"}),
		fc("fetch_url", map[string]any{"url": "https://example.com"}),
		fc("browser_click", map[string]any{"index": 5}),
	), false)
	if c.of("browser_type") != 1 || c.of("browser_click") != 0 {
		t.Errorf("type ran %d times, want 1; click [5], chosen before the take-over, ran %d", c.of("browser_type"), c.of("browser_click"))
	}
}

// browser_state re-indexes the page: an index batched after it was chosen
// on the list the model saw, so it doesn't run once the list has moved.
func TestStateThenClickSkippedWhenTheListMoved(t *testing.T) {
	e := newProgressEnv(t)
	r := e.batchRun()
	var c ran
	e.counted(&c, "browser_open", page("Opened", "https://shop.example/", "Shop", shop...))
	e.rt.runBatch(r.ctx, r, batch(fc("browser_open", map[string]any{"url": "shop.example"})), false)
	moved := append([]string{`<button> Accept cookies`, `<a href="/sale"> Sale`}, shop...)
	e.counted(&c, "browser_state", page("", "https://shop.example/", "Shop", moved...))
	e.counted(&c, "browser_click", page("Clicked", "https://shop.example/", "Shop", moved...))
	e.rt.runBatch(r.ctx, r, batch(
		fc("browser_state", map[string]any{"wait_seconds": 2}),
		fc("browser_click", map[string]any{"index": 5}),
	), false)
	if c.of("browser_click") != 0 {
		t.Errorf("click [5] ran against the re-indexed page (now [5] is %q)", moved[4])
	}
	// When nothing moved, it runs.
	e.counted(&c, "browser_state", page("", "https://shop.example/", "Shop", moved...))
	e.rt.runBatch(r.ctx, r, batch(
		fc("browser_state", map[string]any{"wait_seconds": 2}),
		fc("browser_click", map[string]any{"index": 5}),
	), false)
	if c.of("browser_click") != 1 {
		t.Errorf("click [5] on an unchanged page ran %d times, want 1", c.of("browser_click"))
	}
}
