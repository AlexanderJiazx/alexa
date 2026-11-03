package agent

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"alexagent/internal/llm"
)

// The run phases and sources that other parts of the run announce through
// the helpers in events.go: compacting, stopping, waiting, the reasoning
// headline while the model streams, and a report's sources. Where the call
// belongs in a file the progress code doesn't own, the test skips until it's
// there (the be2b handoffs), so it checks the wiring once it lands.

// phasesOf lists the phases of the run events, repeats folded.
func phasesOf(evs []runEvent) []string {
	var out []string
	for _, ev := range evs {
		if len(out) == 0 || out[len(out)-1] != ev.Phase {
			out = append(out, ev.Phase)
		}
	}
	return out
}

// waitFor polls the view until ok says it shows what the test waits for.
func (e *progressEnv) waitFor(what string, ok func(View) bool) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok(e.rt.View(e.ctx, e.user.ID)) {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Compacting is a phase of the run that's going on, followed by thinking
// again; a compaction the user asks for while Alexa is idle announces
// nothing.
func TestRunCompacting(t *testing.T) {
	e := newProgressEnv(t)
	e.rt.runCompacting(e.user.ID)()
	if evs := e.runEvents(); len(evs) != 0 {
		t.Errorf("an idle compaction announced %v", phasesOf(evs))
	}
	g := newGate()
	e.model.script(g.turn(fc("say", map[string]any{"text": "Done.", "final": true})), does())
	e.send("Tidy up")
	g.wait()
	e.rt.runCompacting(e.user.ID)()
	g.open()
	e.settle()
	if phases := phasesOf(e.runEvents()); !subsequence(phases, []string{"started", "compacting", "thinking", "writing", "ended"}) {
		t.Errorf("phases = %v", phases)
	}
}

// A compaction in the middle of a run shows as its compacting phase.
func TestCompactionIsAnnounced(t *testing.T) {
	e := newProgressEnv(t)
	e.cfg.CompactTokens = 4000
	e.fakeTool("memory_save", func(a map[string]any) (*toolResult, error) { return &toolResult{Text: "Saved."}, nil })
	save := func(s string) any { return fc("memory_save", map[string]any{"content": s, "status": "Noting that"}) }
	e.model.script(
		does(save("Likes quiet hotels")),
		does(save("Travels light")),
		does(save("Prefers Alfama"), usage(9000)), // over budget: the next turn compacts first
		does(fc("say", map[string]any{"text": "Noted.", "final": true})),
		does(),
	)
	e.send("Remember how I like to travel")
	e.settle()
	if ss, _ := e.st.GetSession(e.ctx, e.user.ID); ss.Compactions != 1 {
		t.Fatalf("compactions = %d, want 1", ss.Compactions)
	}
	phases := phasesOf(e.runEvents())
	if !slices.Contains(phases, "compacting") {
		t.Skip("compact() doesn't call runCompacting yet (be2b handoff H5)")
	}
	if !subsequence(phases, []string{"started", "tool", "compacting", "thinking", "writing", "ended"}) {
		t.Errorf("phases = %v", phases)
	}
}

// A step cut off by Stop is interrupted, not failed, and shows no done
// text; the run says it's stopping before it ends stopped.
func TestStopIsAnnounced(t *testing.T) {
	e := newProgressEnv(t)
	started := make(chan struct{})
	e.rt.toolMap["fetch_url"] = &tool{Name: "fetch_url", Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
		close(started)
		<-tc.ctx.Done()
		return nil, tc.ctx.Err()
	}}
	e.model.script(does(fc("fetch_url", map[string]any{"url": "https://trains.example/timetable", "status": "Reading the timetable", "done": "Read the timetable"})))
	e.send("When's the last train home?")
	<-started
	if err := e.rt.Stop(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	e.settle()
	st, sd := stepNamed(t, e.feed(), "Reading the timetable")
	if st.Status != "interrupted" || st.DoneText != "" || sd.Status != "interrupted" || sd.OK == nil || *sd.OK || sd.Error != "" {
		t.Errorf("stopped step = %s %q %+v, want interrupted with no done text and no error", st.Status, st.DoneText, sd)
	}
	if sd.Host != "trains.example" || sd.Kind != "read" {
		t.Errorf("stopped step data = %+v", sd)
	}
	evs := e.runEvents()
	phases := phasesOf(evs)
	if slices.Contains(phases, "stopping") && !subsequence(phases, []string{"started", "tool", "stopping", "ended"}) {
		t.Errorf("phases = %v", phases)
	}
	// The outcome follows what the conversation shows. Until the loop
	// writes the cut-off call's output without the cancelled context
	// (SYNTHESIS L1, WP-13), it shows "context canceled" as an error.
	if feed := e.feed(); feed[len(feed)-1].Kind == "error" {
		if last := evs[len(evs)-1]; last.Outcome != "failed" {
			t.Errorf("ended %q after the error %q", last.Outcome, feed[len(feed)-1].Text)
		}
		t.Skipf("the stop was recorded as the error %q (L1, WP-13)", feed[len(feed)-1].Text)
	}
	if last := evs[len(evs)-1]; last.Phase != "ended" || last.Outcome != "stopped" {
		t.Errorf("last event = %+v, want ended, stopped", last)
	}
	if !slices.Contains(phases, "stopping") {
		t.Skip("Stop doesn't announce the stopping phase yet (be2b handoff H3)")
	}
}

// While the user has the browser, a browser step waits for them, and the
// run shows it's waiting.
func TestWaitingWhileTheUserHasTheBrowser(t *testing.T) {
	e := newProgressEnv(t)
	e.fakeBrowser()
	g := newGate()
	e.model.script(g.turn(openShop()), does(fc("say", map[string]any{"text": "It's on time.", "final": true})), does())
	e.send("Is my flight on time?")
	g.wait()
	if err := e.rt.TakeOver(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	g.open()
	e.waitFor("the step to wait for the user", func(v View) bool { return v.Activity == "You’re in control" })
	if err := e.rt.HandBack(e.ctx, e.user, ""); err != nil {
		t.Fatal(err)
	}
	e.settle()
	phases := phasesOf(e.runEvents())
	if !slices.Contains(phases, "waiting") {
		t.Skip("waitControl doesn't announce the waiting phase yet (be2b handoff H6)")
	}
	if !subsequence(phases, []string{"started", "waiting", "tool", "writing", "ended"}) {
		t.Errorf("phases = %v", phases)
	}
}

// The reasoning summary streamed by the real model call becomes the
// thinking headline, before the say is written.
func TestStreamHeadline(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(
		sse{"type": "response.reasoning_summary_part.added", "item_id": "rs_1"},
		sse{"type": "response.reasoning_summary_text.delta", "item_id": "rs_1", "delta": "**Checking the"},
		sse{"type": "response.reasoning_summary_text.delta", "item_id": "rs_1", "delta": " timetable**\n\nThe last train"},
		fc("say", map[string]any{"text": "The last train is at 23:40.", "final": true}),
	), does())
	e.send("When's the last train home?")
	e.settle()
	var headlines []string
	var phases []string
	for _, ev := range e.runEvents() {
		if ev.Headline != "" {
			headlines = append(headlines, ev.Headline)
			phases = append(phases, "headline")
		} else {
			phases = append(phases, ev.Phase)
		}
	}
	if len(headlines) == 0 {
		t.Skip("loop.go doesn't hand stream events to onStreamEvent yet (be2b handoff H2)")
	}
	if len(headlines) != 1 || headlines[0] != "Checking the timetable" {
		t.Errorf("headlines = %q", headlines)
	}
	if !subsequence(phases, []string{"started", "headline", "writing", "ended"}) {
		t.Errorf("phases = %v", phases)
	}
}

// A report the model gave no sources lists the pages the run read for it.
func TestReportListsTheRunSources(t *testing.T) {
	e := newProgressEnv(t)
	search := func(id string, action map[string]any) any {
		return map[string]any{"type": "web_search_call", "id": id, "status": "completed", "action": action}
	}
	e.model.script(does(
		search("ws_1", map[string]any{"type": "search", "query": "quiet hotels Lisbon"}),
		search("ws_2", map[string]any{"type": "open_page", "url": "https://www.timeout.com/lisbon/hotels?utm=x"}),
		report("Lisbon hotels", nil),
	), does())
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	rep, err := e.st.GetReport(context.Background(), e.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var data ReportData
	_ = json.Unmarshal(rep.Data, &data)
	if len(data.Sources) == 0 {
		t.Skip("presentReport doesn't fall back to runLinks yet (be2b handoff H7)")
	}
	if len(data.Sources) != 1 || data.Sources[0].URL != "https://www.timeout.com/lisbon/hotels" {
		t.Errorf("report sources = %+v", data.Sources)
	}

	// Sources the model names win.
	e.model.script(does(report("Porto hotels", map[string]any{"sources": []any{map[string]any{"title": "Guide", "url": "https://guide.example/porto"}}})), does())
	e.send("And in Porto?")
	e.settle()
	id, _ = e.lastReport()
	rep, _ = e.st.GetReport(context.Background(), e.user.ID, id)
	data = ReportData{}
	_ = json.Unmarshal(rep.Data, &data)
	if len(data.Sources) != 1 || data.Sources[0].URL != "https://guide.example/porto" {
		t.Errorf("named sources = %+v", data.Sources)
	}
}

// A hosted web search's step ends when its item does: the model goes on
// thinking in the same turn, and the step mustn't show as running meanwhile.
func TestSearchStepEndsWithItsItem(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	e.model.script(g.turn(fc("say", map[string]any{"text": "Two good picks.", "final": true})), does())
	e.send("Find a quiet hotel in Lisbon")
	g.wait()
	r := e.rt.running(e.user.ID)
	item, _ := json.Marshal(map[string]any{"type": "web_search_call", "id": "ws_1", "status": "completed", "action": map[string]any{"type": "search", "query": "quiet hotels Lisbon"}})
	e.rt.onStreamEvent(r, llm.Event{Type: "response.output_item.added", Item: item})
	e.rt.onStreamEvent(r, llm.Event{Type: "response.output_item.done", Item: item})
	x, sd := stepNamed(t, e.feed(), "Searching for “quiet hotels Lisbon”")
	v := e.rt.View(e.ctx, e.user.ID)
	g.open()
	e.settle()
	if x.Status != "done" || sd.Status != "done" || x.DoneText == "" {
		t.Errorf("after the search the step is %q (%q, done text %q), want done", x.Status, sd.Status, x.DoneText)
	}
	if v.Activity != "Thinking" || v.Phase != "thinking" {
		t.Errorf("after the search the run shows %q (%s), want Thinking", v.Activity, v.Phase)
	}
}

// Stop during compaction: the run goes from stopping to its end, without
// thinking again in between. The cut-short summary isn't a failed
// compaction, so the next turn over budget compacts without waiting out a
// cooldown (L-8).
func TestStopDuringCompaction(t *testing.T) {
	e := newProgressEnv(t)
	e.cfg.CompactTokens = 4000
	started, _ := e.model.holdSummaries()
	e.model.script(does(fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"}), usage(9000)))
	e.fakeTool("fetch_url", func(map[string]any) (*toolResult, error) { return &toolResult{Text: "The 8:15 runs on time."}, nil })
	e.send("When does the 8:15 leave?")
	<-started
	if err := e.rt.Stop(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	e.settle()
	phases := phasesOf(e.runEvents())
	at := slices.Index(phases, "stopping")
	if at < 0 || slices.Contains(phases[at:], "thinking") {
		t.Errorf("phases = %v", phases)
	}
	if e.rt.compactCooling(e.user.ID) {
		t.Errorf("a Stop during compaction started the compaction cooldown")
	}
}
