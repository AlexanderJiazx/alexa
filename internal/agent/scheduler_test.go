package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alexagent/internal/store"
)

// breakFolders makes the user's folders impossible to make, so whatever
// needs them (a run) fails, until restore.
func breakFolders(t *testing.T, e *progressEnv) (restore func()) {
	t.Helper()
	env := e.rt.envs.For(e.user.ID)
	if err := os.RemoveAll(env.Shots); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(env.Shots), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.Shots, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Remove(env.Shots) }
}

func jobNotes(e *progressEnv) []*store.Notification {
	list, _ := e.st.Notifications(e.ctx, e.user.ID, 50)
	var out []*store.Notification
	for _, n := range list {
		if n.Title == "A scheduled job didn't run" {
			out = append(out, n)
		}
	}
	return out
}

// A job that can't start isn't lost without a word (BH-8): a one-off is
// tried again on the next ticks, and if it still can't start, it's left
// for the user to run and they're told; a recurring one is reported at
// once and stays on.
func TestScheduledJobThatCantStart(t *testing.T) {
	e := newProgressEnv(t)
	breakFolders(t, e)
	s := e.rt.Scheduler()
	if err := e.st.CreateSchedule(e.ctx, &store.Schedule{ID: "once", UserID: e.user.ID, Name: "Pay rent", Prompt: "Remind me to pay the rent",
		RunAt: time.Now().Add(-time.Minute).UnixMilli(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for try := 1; try <= oneOffTries; try++ {
		s.runDueOneOffs(e.ctx)
		sc, err := e.st.GetSchedule(e.ctx, "once")
		if err != nil {
			t.Fatal(err)
		}
		notes := jobNotes(e)
		if try < oneOffTries {
			if !sc.Enabled || len(notes) != 0 {
				t.Errorf("after try %d: enabled = %v, %d notifications; want it tried again, quietly", try, sc.Enabled, len(notes))
			}
			continue
		}
		if sc.Enabled || sc.LastRun != 0 {
			t.Errorf("after the last try: enabled = %v, last run %d; want it left off, not run", sc.Enabled, sc.LastRun)
		}
		if len(notes) != 1 || !strings.Contains(notes[0].Body, "“Pay rent” couldn't start") || !strings.Contains(notes[0].Body, "Run it from your schedules") {
			t.Errorf("notifications after the last try: %+v", notes)
		}
	}

	if err := e.st.CreateSchedule(e.ctx, &store.Schedule{ID: "daily", UserID: e.user.ID, Name: "Morning brief", Prompt: "Brief me", Cron: "0 8 * * *", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.fire("daily", false)
	notes := jobNotes(e)
	if len(notes) != 2 || !strings.Contains(notes[0].Body, "“Morning brief” couldn't start") || !strings.Contains(notes[0].Body, "next time") {
		t.Errorf("a recurring job that couldn't start: %+v", notes)
	}
	if sc, _ := e.st.GetSchedule(e.ctx, "daily"); !sc.Enabled {
		t.Error("a recurring job was turned off for failing once")
	}
}

// A one-off that starts on a later try runs once, and the user isn't
// told it failed.
func TestScheduledOneOffStartsOnRetry(t *testing.T) {
	e := newProgressEnv(t)
	restore := breakFolders(t, e)
	s := e.rt.Scheduler()
	if err := e.st.CreateSchedule(e.ctx, &store.Schedule{ID: "once", UserID: e.user.ID, Name: "Pay rent", Prompt: "Remind me to pay the rent",
		RunAt: time.Now().Add(-time.Minute).UnixMilli(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.runDueOneOffs(e.ctx)
	restore()
	e.model.script(does(fc("say", map[string]any{"text": "Time to pay the rent.", "final": true})), does())
	s.runDueOneOffs(e.ctx)
	e.settle()
	sc, _ := e.st.GetSchedule(e.ctx, "once")
	if sc.LastRun == 0 || sc.Enabled {
		t.Errorf("after the retry: last run %d, enabled %v", sc.LastRun, sc.Enabled)
	}
	if n := jobNotes(e); len(n) != 0 {
		t.Errorf("told of a failure that was retried: %+v", n)
	}
	s.mu.Lock()
	left := len(s.tries)
	s.mu.Unlock()
	if left != 0 {
		t.Errorf("%d tries still counted", left)
	}
}

// The cron table follows a change even when the request or tool call that
// made it is cut short (BH-8).
func TestReloadAfterTheCallerIsGone(t *testing.T) {
	e := newProgressEnv(t)
	if err := e.st.CreateSchedule(e.ctx, &store.Schedule{ID: "daily", UserID: e.user.ID, Name: "Morning brief", Prompt: "Brief me", Cron: "0 8 * * *", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	cancel()
	s := e.rt.Scheduler()
	s.Reload(ctx)
	s.mu.Lock()
	n := 0
	if s.c != nil {
		n = len(s.c.Entries())
		defer s.c.Stop()
	}
	s.mu.Unlock()
	if n != 1 {
		t.Errorf("the cron table has %d jobs after a reload with a cancelled context, want 1", n)
	}
}
