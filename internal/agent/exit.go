package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"alexagent/internal/store"
)

// A run ends in one of four ways: it's done (finish), it waits for the user
// (suspend), it failed (fail) or it was stopped (stopped), by the user or by
// a shutdown it resumes after. Every way out records the outcome for the
// screen, and the last two settle the inbox: what the user sent meanwhile
// joins the conversation in the order it came and, when it's a new request,
// starts the run that answers it, so nothing they send is stranded (L2).

// stopNote tells the model, in the conversation, that the user stopped it.
const stopNote = "[The user stopped you. Don't resume that work unless they ask.]"

// finish ends the run unless a message arrived meanwhile (then the loop
// goes on with it).
func (rt *Runtime) finish(r *run) bool {
	ctx := context.Background()
	uid := r.user.ID
	l := rt.userLock(uid)
	l.Lock()
	defer l.Unlock()
	if n, err := rt.st.InboxCount(ctx, uid); err == nil && n > 0 {
		return false
	}
	rt.finishStep(r, "done")
	r.mu.Lock()
	results := r.results
	r.mu.Unlock()
	if results {
		rt.closeBrowserForResults(ctx, r)
	}
	rt.runEnded(r, "done", nil)
	rt.removeRun(r)
	_ = rt.st.SetSessionStatus(ctx, uid, "idle", "")
	rt.publishState(uid)
	rt.maybeReviewMemory(r.user) // memreview.go
	return true
}

// suspend makes the session wait for the user. A message they sent after
// the question was asked (before they could see it) is taken as the
// answer, and the run goes on; one sent before it skipped the question
// instead (exec.go). Nothing else is an answer (L6, M3): a note (the user
// handed the browser back, say) and a scheduled job stay in the inbox, and
// join the conversation after the answer, since nothing may come between
// the question and its answer.
func (rt *Runtime) suspend(r *run, nd *Needs) bool {
	ctx := context.Background()
	uid := r.user.ID
	l := rt.userLock(uid)
	l.Lock()
	defer l.Unlock()
	r.mu.Lock()
	r.asked = false
	r.mu.Unlock()
	if r.cancelled() {
		// Stopped while asking: the question is withdrawn.
		_ = rt.resolveNeeds(ctx, uid, nd, rt.withdrawn(r), nil)
		rt.stoppedLocked(r)
		return true
	}
	if rt.takeAnswer(ctx, r, nd) {
		r.mu.Lock()
		r.awaiting, r.nudged = true, false
		r.mu.Unlock()
		return false
	}
	rt.finishStep(r, "done")
	e := &store.Entry{UserID: uid, Kind: "ask", Text: nd.Message, Status: "open"}
	e.Data = askEntryData(nd)
	activity := "Waiting for your answer"
	switch nd.Kind {
	case "takeover":
		e.Kind = "takeover"
		activity = nd.Message
		_ = rt.writeSurface(ctx, uid, Surface{Kind: "browser"})
	case "login":
		e.Kind = "login"
		e.Data = marshal(map[string]any{"site": nd.Site, "username": nd.Username})
		activity = "Waiting for you to sign in to " + nd.Site
		_ = rt.writeSurface(ctx, uid, Surface{Kind: "browser"})
	}
	_ = rt.addEntry(ctx, e)
	nd.EntryID = e.ID
	_ = rt.st.SetSessionNeeds(ctx, uid, marshal(nd))
	_ = rt.st.SetSessionStatus(ctx, uid, "needs", activity)
	rt.runEnded(r, "suspended", nil)
	rt.removeRun(r)
	rt.publishState(uid)
	rt.notifyNeeds(ctx, r.user, nd)
	return true
}

// notifyNeeds tells the user Alexa is waiting on them, with what she asked
// as the title, and the note opens the entry that shows it (D1). A
// question only reaches them while they aren't looking at Alexa, since the
// screen shows it; a take-over or a sign-in always does, as the page it's
// on may not wait.
func (rt *Runtime) notifyNeeds(ctx context.Context, u *store.User, nd *Needs) {
	body := "Alexa needs you"
	switch nd.Kind {
	case "takeover":
		body = "Alexa needs you in the browser"
	case "login":
		body = "Alexa needs you to sign in to " + nd.Site
	}
	title := nd.Message
	if title == "" {
		// A take-over's reason can be empty; the note still has a title.
		title, body = body, ""
	}
	ref := ""
	if nd.EntryID > 0 {
		ref = "entry:" + strconv.FormatInt(nd.EntryID, 10)
	}
	_ = rt.notify(ctx, u, ref, title, body, nd.Kind != "question", false)
}

// takeAnswer answers the question with what the user wrote since it was
// asked, if anything, taking it from the inbox in the same step.
func (rt *Runtime) takeAnswer(ctx context.Context, r *run, nd *Needs) bool {
	moved, err := rt.st.MoveInbox(ctx, r.user.ID, func(m store.InboxMessage) bool { return m.Kind == "user" }, func(msgs []store.InboxMessage) []json.RawMessage {
		var texts []string
		var attach []Attachment
		for _, m := range msgs {
			texts = append(texts, m.Text)
			var a []Attachment
			_ = json.Unmarshal(m.Attachments, &a)
			attach = append(attach, a...)
		}
		out := storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": nd.CallID, "output": "Before seeing this, the user wrote:\n" + strings.Join(texts, "\n")})}
		items := []json.RawMessage{marshal(out)}
		if len(attach) > 0 {
			items = append(items, messageItem("user", stamp(r.loc(), "", "(Attached files)"), attach))
		}
		return items
	})
	if err != nil {
		slog.Warn("take answer", "user", r.user.ID, "err", err)
	}
	return err == nil && len(moved) > 0
}

// fail ends the run with an error. When the run was stopped or the server
// is shutting down, err is only how that surfaced (often "context
// canceled", from a write or a call cut short), so the run ends stopped
// instead: Stop never shows as an error (L1). Otherwise the error card
// (failure.go) goes on the screen, Alexa reads why her turn ended, and what
// the user sent meanwhile starts the next run (L2).
func (rt *Runtime) fail(r *run, err error) {
	if r.cancelled() {
		rt.stopped(r)
		return
	}
	ctx := context.Background()
	uid := r.user.ID
	e, note, f := rt.failure(r, err)
	l := rt.userLock(uid)
	l.Lock()
	defer l.Unlock()
	r.mu.Lock()
	r.asked = false
	r.mu.Unlock()
	rt.finishStep(r, "failed")
	_ = rt.st.SetEntriesStatus(ctx, uid, "step", "running", "failed")
	_ = rt.addEntry(ctx, e)
	rt.runEnded(r, "failed", f)
	rt.removeRun(r)
	// The model reads why its turn ended before anything sent meanwhile.
	_ = rt.appendMessage(ctx, uid, "system", note, nil)
	if !rt.launchQueued(ctx, r.user, 0) {
		_ = rt.st.SetSessionStatus(ctx, uid, "idle", "")
	}
	rt.publishState(uid)
	_ = rt.notify(ctx, r.user, "", "Alexa ran into a problem", f.Detail, false, false)
}

// stopped ends a run whose context was cancelled.
func (rt *Runtime) stopped(r *run) {
	l := rt.userLock(r.user.ID)
	l.Lock()
	defer l.Unlock()
	rt.stoppedLocked(r)
}

// stoppedLocked is stopped with the user lock held. A shutdown (not the
// user) leaves the session "working", with the run's flags saved, so it
// resumes on the next start as the same kind of run. A Stop moves the
// inbox into the conversation (Stop's note among it, where it was pressed)
// and starts a new run if the user wrote after pressing it, or a scheduled
// job is waiting.
func (rt *Runtime) stoppedLocked(r *run) {
	ctx := context.Background()
	uid := r.user.ID
	r.mu.Lock()
	stopping, mark := r.stopping, r.stopMark
	flags := resumeFlags{Awaiting: r.awaiting, Held: r.job && r.held}
	r.asked = false
	r.mu.Unlock()
	rt.finishStep(r, "interrupted")
	_ = rt.st.SetEntriesStatus(ctx, uid, "step", "running", "interrupted")
	rt.runEnded(r, "stopped", nil)
	rt.removeRun(r)
	if !stopping && rt.shuttingDown() {
		rt.saveResume(ctx, uid, flags)
		return
	}
	if !stopping {
		// Cancelled without Stop (nothing else cancels a run today): say
		// so the same way.
		_ = rt.addEntry(ctx, &store.Entry{UserID: uid, Kind: "note", Text: "Stopped"})
		_ = rt.st.AddInbox(ctx, uid, store.InboxMessage{Kind: "system", Text: stamp(r.loc(), "", stopNote)})
		mark, _ = rt.st.InboxLast(ctx, uid)
	}
	if !rt.launchQueued(ctx, r.user, mark) {
		_ = rt.st.SetSessionStatus(ctx, uid, "idle", "")
	}
	rt.publishState(uid)
}
