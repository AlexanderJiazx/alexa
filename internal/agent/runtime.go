// Package agent runs each user's single, never-ending session with Alexa:
// the model loop, tools, context compaction, schedules and the
// human-in-the-loop states (questions, browser takeover).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"alexagent/internal/browser"
	"alexagent/internal/config"
	"alexagent/internal/hub"
	"alexagent/internal/llm"
	"alexagent/internal/notify"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
	"alexagent/internal/vault"
)

var (
	ErrNotFound = errors.New("not found")
	ErrBusy     = errors.New("Alexa is working right now; try again when it's done")
)

// tabKey names the session's browser tab; each user's session has one.
const tabKey = "main"

type Runtime struct {
	cfg      *config.Config
	st       *store.Store
	llm      *llm.Client
	browsers *browser.Manager
	envs     *userenv.Envs
	hub      *hub.Hub
	notifier *notify.Notifier
	vault    *vault.Vault
	sched    *Scheduler

	tools    []*tool
	toolMap  map[string]*tool
	toolDefs []any

	baseCtx context.Context
	closing atomic.Bool // Shutdown was called: runs that end now resume on the next start
	mu      sync.Mutex
	runs    map[int64]*run        // user -> the agent loop working for them
	control map[int64]bool        // user -> they control the browser
	locks   map[int64]*sync.Mutex // user -> serialises starting and ending runs
	wg      sync.WaitGroup

	// mems holds the memories written into each user's instructions. Alexa's
	// own memory_save and memory_delete leave it alone (the conversation
	// already shows them), so the instructions and the long conversation
	// cached after them stay the same; it's refreshed when the conversation
	// is compacted or reset, or when the user edits memory.
	memMu sync.Mutex
	mems  map[int64][]*store.Memory

	reviews map[int64]*memReview // user -> the memory review in flight (memreview.go), under mu
	moves   map[int64]uint64     // user -> how often their screen was set or asked for (surface.go), under mu

	// judgeTopic asks the model whether a message carries on the topic on
	// screen (topic.go); tests stand in for it.
	judgeTopic func(ctx context.Context, instructions, input string) (string, error)
}

// run is one stretch of work: from a message until Alexa is done or waits
// for the user.
type run struct {
	user   *store.User
	env    *userenv.Env
	ctx    context.Context // ends when the user stops the run or the server shuts down
	cancel context.CancelFunc
	wake   chan struct{}

	mu       sync.Mutex
	step     *store.Entry
	released chan struct{}           // closed when the user hands back control
	awaiting bool                    // the user wrote and hasn't been answered yet
	nudged   bool                    // reminded to answer through say
	held     bool                    // the user chose what their screen shows, or this is a scheduled job: don't switch it to the browser
	job      bool                    // a scheduled job's run the user hasn't written to: it runs in the background
	asked    bool                    // a call in this batch asked the user: nothing else runs until they answer (exec.go)
	stopping bool                    // the user pressed Stop
	stopMark int64                   // the inbox id of Stop's note: what the user sends after it is a new request
	cut      context.CancelCauseFunc // ends the tool call in flight, if any (exec.go)
	cutTool  string                  // that call's tool
	steer    bool                    // the user interrupted: a batch written without their message doesn't run (exec.go)
	steerAt  int64                   // the inbox id before that message
	results  bool                    // the last say carried blocks: when the run ends they take the stage (R5)
	// takeovers counts the times the user took the browser during the run;
	// turnSeen and turnHeld are what the model turn now running knew of
	// them (markTurn, exec.go).
	takeovers int
	turnSeen  int
	turnHeld  bool
	memFails  int // memory edits refused in this run (memory.go)
}

// cancelled reports whether the run was stopped or the server is shutting
// down.
func (r *run) cancelled() bool { return r.ctx != nil && r.ctx.Err() != nil }

func (r *run) isStopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}

func (r *run) loc() *time.Location { return userLoc(r.user) }

func userLoc(u *store.User) *time.Location {
	if loc, err := time.LoadLocation(u.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

func New(ctx context.Context, cfg *config.Config, st *store.Store, client *llm.Client, br *browser.Manager, envs *userenv.Envs, h *hub.Hub, n *notify.Notifier, v *vault.Vault) *Runtime {
	rt := &Runtime{cfg: cfg, st: st, llm: client, browsers: br, envs: envs, hub: h, notifier: n, vault: v,
		baseCtx: ctx, runs: map[int64]*run{}, control: map[int64]bool{}, locks: map[int64]*sync.Mutex{}, mems: map[int64][]*store.Memory{},
		reviews: map[int64]*memReview{}}
	rt.tools = rt.buildTools()
	rt.toolMap = map[string]*tool{}
	rt.toolDefs = []any{map[string]any{"type": "web_search", "search_context_size": "medium"}}
	for _, t := range rt.tools {
		rt.toolMap[t.Name] = t
		rt.toolDefs = append(rt.toolDefs, t.schema())
	}
	rt.sched = newScheduler(rt)
	rt.judgeTopic = func(ctx context.Context, instructions, input string) (string, error) {
		return client.Quick(ctx, cfg.Model, instructions, input)
	}
	return rt
}

func (rt *Runtime) Scheduler() *Scheduler { return rt.sched }

func (rt *Runtime) userLock(id int64) *sync.Mutex {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	l := rt.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		rt.locks[id] = l
	}
	return l
}

// memories returns the memories for the user's instructions (see mems).
func (rt *Runtime) memories(ctx context.Context, userID int64) []*store.Memory {
	rt.memMu.Lock()
	defer rt.memMu.Unlock()
	if m, ok := rt.mems[userID]; ok {
		return m
	}
	m, err := rt.st.Memories(ctx, userID)
	if err == nil {
		rt.mems[userID] = m
	}
	return m
}

// MemoryChanged shows Alexa the user's current memories from its next step
// on.
func (rt *Runtime) MemoryChanged(userID int64) {
	rt.memMu.Lock()
	delete(rt.mems, userID)
	rt.memMu.Unlock()
	rt.hub.Publish(userID, hub.Changed("memory"))
}

func (rt *Runtime) running(id int64) *run {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.runs[id]
}

func (rt *Runtime) removeRun(r *run) {
	rt.mu.Lock()
	if rt.runs[r.user.ID] == r {
		delete(rt.runs, r.user.ID)
	}
	rt.mu.Unlock()
}

// OnBrowserEvent relays the browser's navigation and cursor to the user's
// clients.
func (rt *Runtime) OnBrowserEvent(userID int64, _ string, ev browser.TabEvent) {
	rt.hub.Publish(userID, hub.Event{Type: "browser", Data: ev})
	if ev.Type == "nav" && strings.HasPrefix(ev.URL, "http") {
		_ = rt.st.SetSessionBrowserURL(context.Background(), userID, ev.URL)
	}
}

// LastURL is where the user's browser was, so it can reopen there after a
// restart or an idle shutdown.
func (rt *Runtime) LastURL(userID int64) string {
	ss, err := rt.st.GetSession(context.Background(), userID)
	if err != nil {
		return ""
	}
	return ss.BrowserURL
}

// Send puts a message into the user's session: a new request, a follow-up,
// a note while Alexa works, or the answer to its question.
func (rt *Runtime) Send(ctx context.Context, u *store.User, m Message) error {
	// Once its entry is on the screen the message must reach Alexa, even if
	// the request goes away: a retry finds the entry and isn't sent again.
	ctx = context.WithoutCancel(ctx)
	text, said, via := rt.reply(ctx, u.ID, m)
	if text == "" && len(m.Attach) == 0 {
		return errors.New("empty message")
	}
	env, err := rt.envs.Ensure(u.ID)
	if err != nil {
		return err
	}
	kind, notes := "user", []string{}
	e := &store.Entry{UserID: u.ID, Kind: "user", Text: text}
	data := map[string]any{}
	for k, v := range via {
		data[k] = v
	}
	if len(m.Attach) > 0 || m.Voice {
		data["attachments"], data["voice"] = m.Attach, m.Voice
	}
	if m.ClientID != "" {
		data["clientId"] = m.ClientID
	}
	if m.Voice {
		notes = append(notes, "spoken")
	}
	if m.Source == "schedule" {
		kind = "schedule"
		e.Kind, e.Text = "note", "Scheduled: "+m.Label
		data["scheduled"] = true
		notes = append(notes, "scheduled job “"+m.Label+"”, not typed by the user")
	}
	loc := userLoc(u)

	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	r := rt.running(u.ID)
	var nd *Needs
	if r == nil {
		nd = rt.pendingNeeds(ctx, u.ID)
	}
	// A message that sets Alexa to work while it's idle starts a new
	// exchange. A tap on what Alexa just showed, or a choice sent from the
	// report she showed in this exchange, answers it instead: the exchange,
	// its cards and its panel stay. A new exchange may still carry on the
	// topic on screen; the model decides while Alexa gets going (topic.go).
	var judge *pendingTopic
	if r == nil && nd == nil && !rt.continues(ctx, u.ID, m, via) {
		data["start"] = true
		if t, ok := rt.topicToJudge(ctx, u.ID, kind); ok {
			data["topic"] = "pending"
			rt.clearPlan(ctx, u.ID)
			// As at any new exchange, the user's hold on the screen ends
			// with their message; the panel stays until the answer is in.
			held := rt.endHold(ctx, u.ID)
			judge = &pendingTopic{thread: t, held: held, moves: rt.surfaceMoves(u.ID)}
		} else {
			rt.onNewExchange(ctx, u.ID, kind == "schedule")
		}
	}
	if len(data) > 0 {
		e.Data = marshal(data)
	}
	if err := rt.addEntry(ctx, e); err != nil {
		return err
	}
	if judge != nil {
		rt.settleTopic(u.ID, e, *judge, said)
	}
	if r != nil {
		note := strings.Join(append(notes, "sent while you were working"), ", ")
		if err := rt.st.AddInbox(ctx, u.ID, store.InboxMessage{Kind: kind, Text: stamp(loc, note, said), Attachments: marshal(m.Attach)}); err != nil {
			return err
		}
		if kind == "user" {
			rt.publishState(u.ID) // the queued count went up
		}
		select {
		case r.wake <- struct{}{}:
		default:
		}
		return nil
	}
	if nd != nil && (kind == "schedule" || m.answersEarlier(nd)) {
		// A scheduled job never answers the question, and doesn't close
		// it: it waits until the user answers, and runs after (M3). Nor
		// does a late answer to a question they skipped earlier (FE-1).
		note := strings.Join(append(notes, "it came while your question was waiting"), ", ")
		if err := rt.st.AddInbox(ctx, u.ID, store.InboxMessage{Kind: kind, Text: stamp(loc, note, said), Attachments: marshal(m.Attach)}); err != nil {
			return err
		}
		if kind == "user" {
			rt.publishState(u.ID) // the queued count went up
		}
		return nil
	}
	note := strings.Join(notes, ", ")
	if nd != nil {
		answer := "The user replied: " + said
		switch nd.Kind {
		case "takeover":
			answer = "The user didn't take over the browser and replied instead: " + said
		case "login":
			answer = "The user didn't enter their login and replied instead: " + said
		}
		if err := rt.resolveNeeds(ctx, u.ID, nd, stamp(loc, note, answer), nil); err != nil {
			return err
		}
		if len(m.Attach) > 0 {
			if err := rt.appendMessage(ctx, u.ID, kind, stamp(loc, note, "(Attached files)"), m.Attach); err != nil {
				return err
			}
		}
	} else {
		// Anything still waiting goes first, so the conversation keeps the
		// order things happened in (L13).
		if _, err := rt.moveInbox(ctx, u.ID, nil); err != nil {
			return err
		}
		if err := rt.appendMessage(ctx, u.ID, kind, stamp(loc, note, said), m.Attach); err != nil {
			return err
		}
	}
	rt.launch(u, env, kind == "user", kind == "schedule")
	return nil
}

// Interrupt sends the user's message and cuts short the step Alexa is on,
// so she turns to it at once (POST /api/interrupt): the step ends
// interrupted, the rest of her batch is skipped, and her next turn starts
// from the message. Sent while she's still thinking, it skips the batch
// she was writing without it too, which an ordinary message doesn't: the
// user asked her to stop what she was about to do. With nothing running
// it's an ordinary Send.
func (rt *Runtime) Interrupt(ctx context.Context, u *store.User, m Message) error {
	ctx = context.WithoutCancel(ctx)
	r := rt.running(u.ID)
	before, lastErr := rt.st.InboxLast(ctx, u.ID)
	// The steer is set before the message goes in: a batch that starts once
	// it's there must find it, or what the model wrote without the message
	// would run.
	steered := r != nil && lastErr == nil && r.steerAfter(before)
	if err := rt.Send(ctx, u, m); err != nil {
		if steered {
			r.mu.Lock()
			if r.steer && r.steerAt == before {
				r.steer, r.steerAt = false, 0
			}
			r.mu.Unlock()
		}
		return err
	}
	if r != nil && rt.running(u.ID) == r {
		r.cutCall(errNewMessage, false)
	}
	return nil
}

// steerAfter makes the run skip what it wrote without the messages after
// inbox id before (exec.go), and reports whether it set that. An earlier
// steer that isn't taken yet covers more, so it stays.
func (r *run) steerAfter(before int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.steer && r.steerAt <= before {
		return false
	}
	r.steer, r.steerAt = true, before
	return true
}

// TakeOver gives the user control of the shared browser.
func (rt *Runtime) TakeOver(ctx context.Context, u *store.User) error {
	if _, err := rt.envs.Ensure(u.ID); err != nil {
		return err
	}
	rt.mu.Lock()
	rt.control[u.ID] = true
	r := rt.runs[u.ID]
	rt.mu.Unlock()
	if r != nil {
		r.mu.Lock()
		if r.released == nil {
			r.released = make(chan struct{})
			r.takeovers++
		}
		r.mu.Unlock()
		// The browser action in flight ends where it is, so Alexa's typing
		// can't land in what the user clicks (B06). Her next browser step
		// waits for them to hand back.
		r.cutCall(errTookOver, true)
	}
	if tab := rt.peekTab(u.ID); tab != nil {
		// Until the cut action has let go of the tab, the user's input is
		// refused too; one that doesn't let go within the grace (or one a
		// stopped run left behind) mustn't keep them out.
		if r == nil {
			tab.Unbind()
		} else {
			time.AfterFunc(stopGrace, tab.Unbind)
		}
	}
	_ = rt.writeSurface(ctx, u.ID, Surface{Kind: "browser"})
	rt.publishState(u.ID)
	return nil
}

// HandBack returns browser control to Alexa.
func (rt *Runtime) HandBack(ctx context.Context, u *store.User, note string) error {
	ctx = context.WithoutCancel(ctx)
	note = clip(strings.Join(strings.Fields(note), " "), 280)
	l := rt.userLock(u.ID)
	l.Lock()
	rt.mu.Lock()
	had := rt.control[u.ID]
	delete(rt.control, u.ID)
	r := rt.runs[u.ID]
	rt.mu.Unlock()
	if !had && note != "" && (r != nil || rt.pendingNeeds(ctx, u.ID) == nil) {
		// The browser was already back (another screen handed it back, or
		// Stop took it): the note is an ordinary message, and Alexa reads it
		// like one.
		l.Unlock()
		return rt.Send(ctx, u, Message{Text: note, Source: "user"})
	}
	defer l.Unlock()
	loc := userLoc(u)
	suffix := ""
	if note != "" {
		suffix = " Their note: " + note
		// What the user wrote for Alexa is theirs to see in the
		// conversation too: nothing from a button reaches her unseen.
		_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "user", Text: note, Data: marshal(map[string]any{"via": "handback"})})
	}
	if r != nil {
		// The note is queued before control is let go, so a model turn that
		// starts once it's back reads it (markTurn).
		if had {
			_ = rt.st.AddInbox(ctx, u.ID, store.InboxMessage{Kind: "system", Text: stamp(loc, "", "[The user took control of the shared browser for a moment and handed it back. The page may have changed — check it before continuing.]"+suffix)})
		}
		r.mu.Lock()
		if r.released != nil {
			close(r.released)
			r.released = nil
		}
		r.mu.Unlock()
		rt.publishState(u.ID)
		return nil
	}
	if nd := rt.pendingNeeds(ctx, u.ID); nd != nil {
		// The run that reads the answer needs the user's folders: if they
		// can't be had, the question stays open to answer again, rather
		// than answered with nothing to read it (BH-10).
		env, err := rt.envs.Ensure(u.ID)
		if err != nil {
			rt.publishState(u.ID)
			return err
		}
		answer := "The user took over the browser and has handed control back."
		switch nd.Kind {
		case "question":
			answer = "The user took over the browser to handle this themselves and handed control back."
		case "login":
			answer = "Instead of entering their login in the form, the user took over the browser and has handed control back. Check whether they signed in."
		}
		answer += suffix
		_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "step", Text: "You took over", Status: "done"})
		var shot []byte
		if tab := rt.peekTab(u.ID); tab != nil {
			if st, err := tab.State(); err == nil {
				answer += "\n\nCurrent browser state:\n" + st.Text
				shot = st.Screenshot
			}
		}
		if err := rt.resolveNeeds(ctx, u.ID, nd, stamp(loc, "", answer), shot); err != nil {
			return err
		}
		rt.launch(u, env, false, false)
		return nil
	}
	if had {
		// Nothing is running, so the note can join the conversation now,
		// before whatever the user asks next (L13).
		_ = rt.appendMessage(ctx, u.ID, "system", stamp(loc, "", "[While you were idle, the user used the shared browser themselves and handed it back.]"+suffix), nil)
	}
	rt.publishState(u.ID)
	return nil
}

// InControl reports whether the user currently controls their browser.
func (rt *Runtime) InControl(userID int64) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.control[userID]
}

// Stop interrupts whatever Alexa is doing (or waiting for).
func (rt *Runtime) Stop(ctx context.Context, u *store.User) error {
	// What Stop records must be kept even if the request goes away.
	ctx = context.WithoutCancel(ctx)
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	rt.mu.Lock()
	r := rt.runs[u.ID]
	delete(rt.control, u.ID)
	rt.mu.Unlock()
	if r != nil {
		rt.stopRun(ctx, u, r)
		return nil
	}
	if nd := rt.pendingNeeds(ctx, u.ID); nd != nil {
		_ = rt.resolveNeeds(ctx, u.ID, nd, "The user dismissed this. Don't continue that work unless they ask again.", nil)
		_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "note", Text: "Stopped"})
		// Scheduled jobs that waited for the answer run now.
		if !rt.launchQueued(ctx, u, 0) {
			_ = rt.st.SetSessionStatus(ctx, u.ID, "idle", "")
		}
		rt.publishState(u.ID)
		return nil
	}
	// Nothing is running, yet the session may still say it's working (a
	// resume that couldn't start): Stop always brings it back (L23).
	if ss, err := rt.st.GetSession(ctx, u.ID); err == nil && ss.Status == "working" {
		_ = rt.st.SetSessionStatus(ctx, u.ID, "idle", "")
	}
	rt.publishState(u.ID)
	return nil
}

// stopRun asks the run to stop, and shows at once that it's stopping: the
// run winds down within moments (exec.go gives a tool stopGrace) and ends
// in stopped(). Stop's note goes in the inbox now, so the conversation gets
// it after what the user sent before pressing Stop and before what they
// send next; that, and only that, starts a new run once this one is over.
// The caller holds the user lock; r.user isn't read here, since the loop
// replaces it without one.
func (rt *Runtime) stopRun(ctx context.Context, u *store.User, r *run) {
	r.mu.Lock()
	first := !r.stopping
	r.stopping = true
	r.mu.Unlock()
	if first {
		if err := rt.st.AddInbox(ctx, u.ID, store.InboxMessage{Kind: "system", Text: stamp(userLoc(u), "", stopNote)}); err == nil {
			if mark, err := rt.st.InboxLast(ctx, u.ID); err == nil {
				r.mu.Lock()
				r.stopMark = mark
				r.mu.Unlock()
			}
		}
		_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "note", Text: "Stopped"})
	}
	rt.runPhaseFor(u.ID, "stopping")
	_ = rt.st.SetSessionActivity(ctx, u.ID, "Stopping")
	rt.publishState(u.ID)
	r.cancel()
}

// Compact summarises the older conversation now instead of waiting for the
// context to fill up. A summary dropped because the conversation changed
// while it was written is tried once more: that one finds Alexa at work
// (ErrBusy), or the conversation as it is now.
func (rt *Runtime) Compact(ctx context.Context, u *store.User) error {
	err := rt.compactNow(ctx, u)
	if errors.Is(err, errCompactChanged) {
		err = rt.compactNow(ctx, u)
	}
	rt.publishState(u.ID)
	return err
}

// compactNow is one try of Compact. It plans and commits under the user
// lock, with nothing running, but lets go of the lock for the summary's
// model call (BH-2): Stop, Send and the scheduler, which runs every user's
// jobs on one goroutine, don't wait for it. The commit doesn't go ahead if
// a run started meanwhile, or the conversation changed (commitCompaction).
func (rt *Runtime) compactNow(ctx context.Context, u *store.User) error {
	l := rt.userLock(u.ID)
	l.Lock()
	if rt.running(u.ID) != nil || !rt.startCompacting(u.ID) {
		l.Unlock()
		return ErrBusy
	}
	defer rt.doneCompacting(u.ID)
	c, err := rt.planCompaction(ctx, u, true)
	if err != nil || c == nil {
		l.Unlock()
		return err
	}
	_ = rt.st.SetSessionActivity(ctx, u.ID, "Tidying up my notes")
	rt.publishState(u.ID)
	l.Unlock()
	summary, err := rt.foldSummary(ctx, u, c)
	if err != nil {
		return err
	}
	l.Lock()
	defer l.Unlock()
	if rt.running(u.ID) != nil {
		return errCompactChanged
	}
	return rt.commitCompaction(ctx, u, c, summary)
}

// Reset forgets the conversation. Memories, files, reports and schedules
// stay.
func (rt *Runtime) Reset(ctx context.Context, u *store.User) error {
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	if rt.running(u.ID) != nil {
		return ErrBusy
	}
	rt.mu.Lock()
	delete(rt.control, u.ID)
	rt.cancelReviewLocked(u.ID)
	rt.mu.Unlock()
	if err := rt.st.ResetSession(ctx, u.ID); err != nil {
		return err
	}
	// The memory review's notes on the forgotten conversation go too.
	_ = rt.st.SetKV(ctx, reviewKey(u.ID), "")
	// The screenshots went with the conversation that showed them (BH-3).
	// Nothing runs, so none is waiting for its item.
	rt.pruneShots(ctx, u.ID, 0)
	rt.MemoryChanged(u.ID)
	rt.hub.Publish(u.ID, hub.Event{Type: "reset"})
	rt.publishState(u.ID)
	return nil
}

// Tab returns the session's browser tab, launching the browser if create is
// set.
func (rt *Runtime) Tab(ctx context.Context, u *store.User, create bool) (*browser.Tab, error) {
	if !create {
		return rt.peekTab(u.ID), nil
	}
	if _, err := rt.envs.Ensure(u.ID); err != nil {
		return nil, err
	}
	b, err := rt.browsers.Get(u.ID)
	if err != nil {
		return nil, err
	}
	return b.Tab(tabKey)
}

func (rt *Runtime) peekTab(userID int64) *browser.Tab {
	if b := rt.browsers.Existing(userID); b != nil {
		return b.PeekTab(tabKey)
	}
	return nil
}

// ResumeInterrupted restarts sessions that were working when the server
// stopped: shut down mid-run (stopped() leaves them "working") or killed.
// Each resumes once, as the kind of run it was. Messages still waiting on
// their topic call are settled first.
func (rt *Runtime) ResumeInterrupted(ctx context.Context) {
	rt.settleLeftTopics(ctx)
	ids, err := rt.st.SessionsWithStatus(ctx, "working")
	if err != nil {
		slog.Error("resume sessions", "err", err)
		return
	}
	for _, id := range ids {
		rt.resume(ctx, id)
	}
}

// resume restarts one interrupted session. It's idempotent: under the
// user's lock it does nothing once a run is going or the session isn't
// "working" any more, so calling it again can't add a second note or start
// a second run (L15).
func (rt *Runtime) resume(ctx context.Context, id int64) {
	u, err := rt.st.GetUser(ctx, id)
	if err != nil {
		slog.Warn("can't resume session", "user", id, "err", err)
		return
	}
	env, err := rt.envs.Ensure(u.ID)
	if err != nil {
		// Stop still brings the session back to idle (L23).
		slog.Warn("can't resume session", "user", u.Username, "err", err)
		return
	}
	l := rt.userLock(id)
	l.Lock()
	defer l.Unlock()
	if rt.running(id) != nil {
		return
	}
	if ss, err := rt.st.GetSession(ctx, id); err != nil || ss.Status != "working" {
		return
	}
	flags := rt.takeResume(ctx, id)
	_ = rt.st.SetEntriesStatus(ctx, id, "step", "running", "interrupted")
	_ = rt.st.AddInbox(ctx, id, store.InboxMessage{Kind: "system", Text: stamp(userLoc(u), "", "[The server restarted while you were working. What you did and found before is above, so don't redo it: the browser restarted too, so re-open the page you were on and carry on from where you stopped.]")})
	slog.Info("resuming session", "user", u.Username)
	rt.launch(u, env, flags.Awaiting, flags.Held)
}

// resumeFlags are what a run interrupted by a shutdown was, so it resumes
// the same way: a background job stays in the background, and Alexa still
// owes the user an answer only if she did. Held is a background job's: a
// screen the user held during their own request stays held by the
// session's surface, and the run resumes in the foreground.
type resumeFlags struct {
	Awaiting bool `json:"awaiting"`
	Held     bool `json:"held"`
}

func resumeKey(userID int64) string { return fmt.Sprintf("resume:%d", userID) }

// saveResume keeps the run's flags until it resumes.
func (rt *Runtime) saveResume(ctx context.Context, userID int64, f resumeFlags) {
	_ = rt.st.SetKV(ctx, resumeKey(userID), string(marshal(f)))
}

// takeResume returns the flags saved at shutdown and forgets them. A run
// killed without a shutdown saved none: it resumes in the foreground, owing
// the user an answer, as before.
func (rt *Runtime) takeResume(ctx context.Context, userID int64) resumeFlags {
	f := resumeFlags{Awaiting: true}
	if v, err := rt.st.GetKV(ctx, resumeKey(userID)); err == nil && v != "" {
		_ = json.Unmarshal([]byte(v), &f)
		_ = rt.st.SetKV(ctx, resumeKey(userID), "")
	}
	return f
}

// Shutdown cancels running loops; their sessions stay "working" and resume
// on the next start.
func (rt *Runtime) Shutdown() {
	rt.closing.Store(true)
	rt.mu.Lock()
	for _, r := range rt.runs {
		r.cancel()
	}
	rt.mu.Unlock()
	done := make(chan struct{})
	go func() { rt.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// shuttingDown reports whether the server is going down, so a run that
// ends now was interrupted rather than stopped.
func (rt *Runtime) shuttingDown() bool { return rt.closing.Load() || rt.baseCtx.Err() != nil }

// launch starts the agent loop for a user. The caller holds the user lock.
// A background run (a scheduled job) leaves the user's screen alone until
// it needs them.
func (rt *Runtime) launch(u *store.User, env *userenv.Env, awaiting, background bool) {
	rt.mu.Lock()
	if rt.runs[u.ID] != nil {
		rt.mu.Unlock()
		return
	}
	// The live run always wins over a memory review (memreview.go).
	rt.cancelReviewLocked(u.ID)
	// Most requests touch the web; start Chrome while the model thinks.
	rt.browsers.Prewarm(u.ID)
	ctx, cancel := context.WithCancel(rt.baseCtx)
	r := &run{user: u, env: env, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), awaiting: awaiting, held: background, job: background}
	if rt.control[u.ID] {
		r.released = make(chan struct{})
	}
	rt.runs[u.ID] = r
	rt.mu.Unlock()
	_ = rt.st.SetSessionStatus(context.Background(), u.ID, "working", "Thinking")
	rt.publishState(u.ID)
	rt.wg.Add(1)
	go func() {
		defer rt.wg.Done()
		defer cancel()
		rt.loop(ctx, r)
	}()
}

func (rt *Runtime) pendingNeeds(ctx context.Context, userID int64) *Needs {
	ss, err := rt.st.GetSession(ctx, userID)
	if err != nil || ss.Status != "needs" {
		return nil
	}
	var nd Needs
	if json.Unmarshal(ss.Needs, &nd) != nil || nd.CallID == "" {
		return nil
	}
	return &nd
}

func isMessage(kind string) bool { return kind == "user" || kind == "schedule" || kind == "system" }

// appendMessage adds a message from the user (or about them) to the
// conversation.
func (rt *Runtime) appendMessage(ctx context.Context, userID int64, kind, text string, attach []Attachment) error {
	return rt.st.AppendItems(ctx, userID, []json.RawMessage{messageItem(kind, text, attach)})
}

// messageItem is a message from the user (or about them) as the
// conversation stores it.
func messageItem(kind, text string, attach []Attachment) json.RawMessage {
	content := []map[string]any{{"type": "input_text", "text": text}}
	item := marshal(map[string]any{"role": "user", "content": content})
	return marshal(storedItem{Item: item, Kind: kind, Attach: attach})
}

// moveInbox moves the waiting messages keep accepts (all, if keep is nil)
// into the conversation, each as appendMessage adds one, in the same step
// as taking them from the inbox, so none is lost or read twice (L14). It
// returns what it moved.
func (rt *Runtime) moveInbox(ctx context.Context, userID int64, keep func(store.InboxMessage) bool) ([]store.InboxMessage, error) {
	return rt.st.MoveInbox(ctx, userID, keep, func(msgs []store.InboxMessage) []json.RawMessage {
		items := make([]json.RawMessage, len(msgs))
		for i, m := range msgs {
			var attach []Attachment
			_ = json.Unmarshal(m.Attachments, &attach)
			items[i] = messageItem(m.Kind, m.Text, attach)
		}
		return items
	})
}

// launchQueued moves what's waiting in the inbox into the conversation and,
// if the user wrote after mark (an inbox id; 0 counts everything) or a
// scheduled job is waiting, starts the run that handles it: in the
// foreground for the user, in the background for a job alone. It reports
// whether it started one. The caller holds the user lock.
func (rt *Runtime) launchQueued(ctx context.Context, u *store.User, mark int64) bool {
	moved, err := rt.moveInbox(ctx, u.ID, nil)
	if err != nil {
		slog.Warn("move inbox", "user", u.ID, "err", err)
	}
	user, job := false, false
	for _, m := range moved {
		user = user || m.Kind == "user" && m.ID > mark
		job = job || m.Kind == "schedule"
	}
	if !user && !job {
		return false
	}
	env, err := rt.envs.Ensure(u.ID)
	if err != nil {
		slog.Warn("can't start the next run", "user", u.ID, "err", err)
		return false
	}
	rt.launch(u, env, user, !user)
	return true
}

// resolveNeeds answers the tool call the session is waiting on.
func (rt *Runtime) resolveNeeds(ctx context.Context, userID int64, nd *Needs, answer string, shot []byte) error {
	si := storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": nd.CallID, "output": answer})}
	if len(shot) > 0 {
		if name, err := rt.saveShot(userID, shot); err == nil {
			si.Shot = name
			si.Kind = "browser_state"
			si.Short = "[The user took over and handed back control. Older page state omitted.]"
		}
	}
	if err := rt.st.AppendItems(ctx, userID, []json.RawMessage{marshal(si)}); err != nil {
		return err
	}
	if nd.EntryID > 0 {
		if err := rt.st.SetEntryStatus(ctx, userID, nd.EntryID, "answered"); err == nil {
			rt.hub.Publish(userID, hub.Event{Type: "entryStatus", Data: map[string]any{"id": nd.EntryID, "status": "answered"}})
		}
		// So is the note that told the user about it (D1).
		if read, err := rt.st.MarkNotificationRead(ctx, userID, "entry:"+strconv.FormatInt(nd.EntryID, 10)); err == nil && read {
			rt.hub.Publish(userID, hub.Changed("notifications"))
		}
	}
	return rt.st.SetSessionNeeds(ctx, userID, nil)
}

func (rt *Runtime) addEntry(ctx context.Context, e *store.Entry) error {
	if err := rt.st.AddEntry(ctx, e); err != nil {
		return err
	}
	rt.hub.Publish(e.UserID, hub.Event{Type: "entry", Data: e})
	return nil
}

func (rt *Runtime) updateEntry(ctx context.Context, e *store.Entry) {
	if err := rt.st.UpdateEntry(ctx, e); err == nil {
		rt.hub.Publish(e.UserID, hub.Event{Type: "entry", Data: e})
	}
}

// notify pushes a notification unless the user is looking at Alexa right
// now (or always, with force).
func (rt *Runtime) notify(ctx context.Context, u *store.User, ref, title, body string, force, email bool) error {
	if !force && !email && rt.hub.Online(u.ID) {
		return nil
	}
	return rt.notifier.Notify(ctx, u, ref, title, body, email)
}

func (rt *Runtime) saveShot(userID int64, img []byte) (string, error) {
	env := rt.envs.For(userID)
	if err := os.MkdirAll(env.Shots, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("s-%d.jpg", time.Now().UnixNano())
	return name, os.WriteFile(filepath.Join(env.Shots, name), img, 0o600)
}
