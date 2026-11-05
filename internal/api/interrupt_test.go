package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"alexagent/internal/llm"
)

// POST /api/interrupt ("Stop and do this instead"): the message is sent,
// the step Alexa is on ends at once, and her next turn reads both.
func TestInterrupt(t *testing.T) {
	e := newTestServer(t)
	c := e.client(t)
	c.signup("alice")
	e.model.script(
		reply(1000, fn("run_shell", map[string]any{"status": "Checking the timetable", "command": "sleep 5"})),
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			in := inputText(req)
			if !strings.Contains(in, "Stop that, check Friday") || !strings.Contains(in, "the user sent a new message while this was running") {
				t.Errorf("after the interrupt Alexa read: …%s", tail(in, 600))
			}
			return []any{fn("say", map[string]any{"text": "Friday then.", "final": true})}, 1000
		},
	)
	c.send("When does the 8:15 leave?")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st := last(c.session().Feed, "step"); st != nil && st.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the step never started")
		}
		time.Sleep(15 * time.Millisecond)
	}
	t0 := time.Now()
	code, m, _ := c.do("POST", "/api/interrupt", map[string]any{"text": "Stop that, check Friday", "clientId": "c-interrupt-1"}, true)
	if code != 200 || m["entry"] == nil {
		t.Fatalf("interrupt: %d %v", code, m)
	}
	// The same clientId again (a retry) doesn't send it twice.
	if code, _, _ := c.do("POST", "/api/interrupt", map[string]any{"text": "Stop that, check Friday", "clientId": "c-interrupt-1"}, true); code != 200 {
		t.Fatalf("retried interrupt: %d", code)
	}
	s := c.wait("idle")
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("the interrupt took %s; the step should end at once", d)
	}
	users := 0
	for _, x := range s.Feed {
		if x.Kind == "user" {
			users++
		}
	}
	if users != 2 {
		t.Errorf("%d user entries, want 2: %v", users, lastN(s.Feed, 6))
	}
	if sy := last(s.Feed, "say"); sy == nil || sy.Text != "Friday then." {
		t.Errorf("feed: %v", lastN(s.Feed, 6))
	}
	if code, _, _ := c.do("POST", "/api/interrupt", map[string]any{"text": "x", "clientId": "bad id!"}, true); code != 400 {
		t.Errorf("a bad clientId gave %d", code)
	}
}
