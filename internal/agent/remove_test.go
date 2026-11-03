package agent

import (
	"errors"
	"os"
	"testing"
	"time"

	"alexagent/internal/browser"
	"alexagent/internal/userenv"
)

// Removing a user stops their run and refuses anything that would start
// one, or their browser, or make their folders again (BH-5).
func TestRemoveUser(t *testing.T) {
	e := newProgressEnv(t)
	uid := e.user.ID
	g := newGate()
	defer g.open()
	e.model.script(g.turn(fc("say", map[string]any{"text": "Working on it.", "final": true})), does())
	e.send("Plan my week")
	g.wait() // the run is in its model call
	done := make(chan struct{})
	go func() { e.rt.RemoveUser(e.ctx, uid); close(done) }()
	select {
	case <-done:
	case <-time.After(removeWait + 5*time.Second):
		t.Fatal("RemoveUser didn't return")
	}
	if e.rt.running(uid) != nil {
		t.Error("the run is still going")
	}
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Hello?"}); err == nil {
		t.Error("a message started a run for a removed user")
	}
	if _, err := e.rt.envs.Ensure(uid); !errors.Is(err, userenv.ErrRemoved) {
		t.Errorf("Ensure = %v, want ErrRemoved", err)
	}
	if _, err := e.rt.browsers.Get(uid); !errors.Is(err, browser.ErrRemoved) {
		t.Errorf("browsers.Get = %v, want ErrRemoved", err)
	}
	env := e.rt.envs.For(uid)
	if err := e.rt.envs.Remove(uid); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(env.Root); !os.IsNotExist(err) {
		t.Errorf("the user's folder came back: %v", err)
	}
	if e.rt.running(uid) != nil {
		t.Error("a run started after the removal")
	}
}
