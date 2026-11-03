package agent

import (
	"strings"
	"testing"

	"alexagent/internal/llm"
)

// A message interrupting a long compaction is read in the turn that
// follows it, so that turn's work isn't thrown away for it.
func TestInterruptDuringCompaction(t *testing.T) {
	e := newProgressEnv(t)
	e.cfg.CompactTokens = 4000
	started, release := e.model.holdSummaries()
	e.fakeTool("fetch_url", func(map[string]any) (*toolResult, error) { return &toolResult{Text: "The 8:15 runs on time."}, nil })
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	write := fc("write_file", map[string]any{"path": "friday.txt", "content": "7:40", "status": "Saving Friday’s times"})
	var input string
	e.model.script(
		does(fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"}), usage(9000)),
		func(req *llm.Request) []any { input = inputOf(req); return []any{write} },
		does(fc("say", map[string]any{"text": "Friday’s first train is at 7:40.", "final": true})),
		does(),
	)
	e.send("When does the 8:15 leave?")
	<-started
	if err := e.rt.Interrupt(e.ctx, e.user, Message{Text: "Stop that, check Friday"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	e.settle()
	if !strings.Contains(input, "Stop that, check Friday") {
		t.Errorf("the turn after compacting didn't see the message:\n%s", tailOf(input, 600))
	}
	if c.of("write_file") != 1 {
		t.Errorf("write_file ran %d times, want once", c.of("write_file"))
	}
	if out := e.outputs()[write.(map[string]any)["call_id"].(string)]; out == skippedMessage {
		t.Errorf("the turn written with the message was skipped")
	}
}

// Once the turn after compacting has read the interrupt, it's done its
// work: a message sent while that turn is written doesn't skip what it
// wrote, as for any message sent while Alexa thinks.
func TestInterruptReadAfterCompactingIsSpent(t *testing.T) {
	e := newProgressEnv(t)
	e.cfg.CompactTokens = 4000
	started, release := e.model.holdSummaries()
	e.fakeTool("fetch_url", func(map[string]any) (*toolResult, error) { return &toolResult{Text: "The 8:15 runs on time."}, nil })
	var c ran
	e.counted(&c, "write_file", &toolResult{Text: "Saved"})
	write := fc("write_file", map[string]any{"path": "friday.txt", "content": "7:40", "status": "Saving Friday’s times"})
	g := newGate()
	e.model.script(
		does(fc("fetch_url", map[string]any{"url": "https://trains.example", "status": "Reading the timetable"}), usage(9000)),
		g.turn(write),
		does(fc("say", map[string]any{"text": "Friday’s first train is at 7:40, and Saturday’s at 8:05.", "final": true})),
		does(),
	)
	e.send("When does the 8:15 leave?")
	<-started
	if err := e.rt.Interrupt(e.ctx, e.user, Message{Text: "Stop that, check Friday"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	g.wait()
	e.send("Saturday too")
	g.open()
	e.settle()
	if c.of("write_file") != 1 {
		t.Errorf("write_file ran %d times, want once: the turn that read the interrupt was skipped", c.of("write_file"))
	}
}
