package agent

import (
	"context"
	"log/slog"
	"time"
)

// removeWait is how long RemoveUser waits for a stopped run to end. A run
// winds down within moments (a tool gets stopGrace); one that takes longer
// is left to end on its own, failing on the rows and folders that are gone.
const removeWait = 10 * time.Second

// RemoveUser stops everything the server does for a user who is being
// deleted, before their rows and folders go (BH-5): nothing starts for
// them again (their folders are refused from now on, which a message, a
// resume, a sign-in and the run after this one all need), their run stops
// and ends, their browser closes for good, and what's kept in memory for
// them is dropped. The caller deletes their rows, then their folders
// (userenv.Envs.Remove).
func (rt *Runtime) RemoveUser(ctx context.Context, userID int64) {
	rt.envs.Retire(userID)
	deadline := time.Now().Add(removeWait)
	for {
		l := rt.userLock(userID)
		l.Lock()
		r := rt.running(userID)
		l.Unlock()
		if r == nil {
			break
		}
		// It ends as stopped by the user, without the note Stop leaves:
		// there's no one to read it.
		r.mu.Lock()
		r.stopping = true
		r.mu.Unlock()
		r.cancel()
		for rt.running(userID) == r && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if rt.running(userID) == r {
			slog.Warn("removing a user whose run hasn't ended", "user", userID)
			break
		}
	}
	rt.browsers.Remove(userID)
	rt.mu.Lock()
	delete(rt.control, userID)
	rt.mu.Unlock()
	// The user's lock stays: something may still be waiting on it.
	rt.memMu.Lock()
	delete(rt.mems, userID)
	rt.memMu.Unlock()
	rt.compactOK(userID)
	rt.doneCompacting(userID)
	rt.vault.Forget(userID)
	rt.hub.Forget(userID)
}
