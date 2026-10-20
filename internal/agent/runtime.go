// Package agent runs each user's single, never-ending session with Alexa:
// the model loop, tools, context compaction, schedules and the
// human-in-the-loop states (questions, browser takeover).
package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"alexagent/internal/browser"
	"alexagent/internal/config"
	"alexagent/internal/hub"
	"alexagent/internal/llm"
	"alexagent/internal/notify"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
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
	sched    *Scheduler

	tools    []*tool
	toolMap  map[string]*tool
	toolDefs []any

	baseCtx context.Context
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
}

// run is one stretch of work: from a message until Alexa is done or waits
// for the user.
type run struct {
	user   *store.User
	env    *userenv.Env
	cancel context.CancelFunc
	wake   chan struct{}

	mu       sync.Mutex
	step     *store.Entry
	released chan struct{} // closed when the user hands back control
	awaiting bool          // the user wrote and hasn't been answered yet
	nudged   bool          // reminded to answer through say
	held     bool          // the user chose what their screen shows; don't switch it to the browser
}

func (r *run) loc() *time.Location { return userLoc(r.user) }

func userLoc(u *store.User) *time.Location {
	if loc, err := time.LoadLocation(u.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

func New(ctx context.Context, cfg *config.Config, st *store.Store, client *llm.Client, br *browser.Manager, envs *userenv.Envs, h *hub.Hub, n *notify.Notifier) *Runtime {
	rt := &Runtime{cfg: cfg, st: st, llm: client, browsers: br, envs: envs, hub: h, notifier: n,
		baseCtx: ctx, runs: map[int64]*run{}, control: map[int64]bool{}, locks: map[int64]*sync.Mutex{}, mems: map[int64][]*store.Memory{}}
	rt.tools = rt.buildTools()
	rt.toolMap = map[string]*tool{}
	rt.toolDefs = []any{map[string]any{"type": "web_search", "search_context_size": "medium"}}
	for _, t := range rt.tools {
		rt.toolMap[t.Name] = t
		rt.toolDefs = append(rt.toolDefs, t.schema())
	}
	rt.sched = newScheduler(rt)
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

// ---------- public API ----------

// Send puts a message into the user's session: a new request, a follow-up,
// a note while Alexa works, or the answer to its question.
func (rt *Runtime) Send(ctx context.Context, u *store.User, m Message) error {
	text := strings.TrimSpace(m.Text)
	if text == "" && len(m.Attach) == 0 {
		return errors.New("empty message")
	}
	env, err := rt.envs.Ensure(u.ID)
	if err != nil {
		return err
	}
	kind, notes := "user", []string{}
	e := &store.Entry{UserID: u.ID, Kind: "user", Text: text}
	if len(m.Attach) > 0 || m.Voice {
		e.Data = marshal(map[string]any{"attachments": m.Attach, "voice": m.Voice})
	}
	if m.Voice {
		notes = append(notes, "spoken")
	}
	if m.Source == "schedule" {
		kind = "schedule"
		e.Kind, e.Text = "note", "Scheduled: "+m.Label
		notes = append(notes, "scheduled job “"+m.Label+"”, not typed by the user")
	}
	if err := rt.addEntry(ctx, e); err != nil {
		return err
	}
	loc := userLoc(u)

	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	if r := rt.running(u.ID); r != nil {
		note := strings.Join(append(notes, "sent while you were working"), ", ")
		if err := rt.st.AddInbox(ctx, u.ID, store.InboxMessage{Kind: kind, Text: stamp(loc, note, text), Attachments: marshal(m.Attach)}); err != nil {
			return err
		}
		select {
		case r.wake <- struct{}{}:
		default:
		}
		return nil
	}
	note := strings.Join(notes, ", ")
	if nd := rt.pendingNeeds(ctx, u.ID); nd != nil {
		answer := "The user replied: " + text
		if nd.Kind == "takeover" {
			answer = "The user didn't take over the browser and replied instead: " + text
		}
		if err := rt.resolveNeeds(ctx, u.ID, nd, stamp(loc, note, answer), nil); err != nil {
			return err
		}
		if len(m.Attach) > 0 {
			if err := rt.appendMessage(ctx, u.ID, kind, stamp(loc, note, "(Attached files)"), m.Attach); err != nil {
				return err
			}
		}
	} else if err := rt.appendMessage(ctx, u.ID, kind, stamp(loc, note, text), m.Attach); err != nil {
		return err
	}
	rt.launch(u, env, kind == "user")
	return nil
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
		}
		r.mu.Unlock()
	}
	_ = rt.st.SetSessionSurface(ctx, u.ID, marshal(Surface{Kind: "browser"}))
	rt.publishState(u.ID)
	return nil
}

// HandBack returns browser control to Alexa.
func (rt *Runtime) HandBack(ctx context.Context, u *store.User, note string) error {
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	rt.mu.Lock()
	had := rt.control[u.ID]
	delete(rt.control, u.ID)
	r := rt.runs[u.ID]
	rt.mu.Unlock()
	loc := userLoc(u)
	suffix := ""
	if note != "" {
		suffix = " Their note: " + note
	}
	if r != nil {
		r.mu.Lock()
		if r.released != nil {
			close(r.released)
			r.released = nil
		}
		r.mu.Unlock()
		if had {
			_ = rt.st.AddInbox(ctx, u.ID, store.InboxMessage{Kind: "system", Text: stamp(loc, "", "[The user took control of the shared browser for a moment and handed it back. The page may have changed — check it before continuing.]"+suffix)})
		}
		rt.publishState(u.ID)
		return nil
	}
	if nd := rt.pendingNeeds(ctx, u.ID); nd != nil {
		answer := "The user took over the browser and has handed control back."
		if nd.Kind == "question" {
			answer = "The user took over the browser to handle this themselves and handed control back."
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
		env, err := rt.envs.Ensure(u.ID)
		if err != nil {
			return err
		}
		rt.launch(u, env, false)
		return nil
	}
	if had {
		_ = rt.st.AddInbox(ctx, u.ID, store.InboxMessage{Kind: "system", Text: stamp(loc, "", "[While you were idle, the user used the shared browser themselves and handed it back.]"+suffix)})
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
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	rt.mu.Lock()
	r := rt.runs[u.ID]
	delete(rt.control, u.ID)
	rt.mu.Unlock()
	if r != nil {
		r.cancel() // the loop records the stop
		return nil
	}
	if nd := rt.pendingNeeds(ctx, u.ID); nd != nil {
		_ = rt.resolveNeeds(ctx, u.ID, nd, "The user dismissed this. Don't continue that work unless they ask again.", nil)
		_ = rt.st.SetSessionStatus(ctx, u.ID, "idle", "")
		_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "note", Text: "Stopped"})
	}
	rt.publishState(u.ID)
	return nil
}

// View returns the session as clients see it.
func (rt *Runtime) View(ctx context.Context, userID int64) View {
	v := View{Status: "idle"}
	ss, err := rt.st.GetSession(ctx, userID)
	if err != nil {
		return v
	}
	v.Status, v.Activity, v.BrowserURL = ss.Status, ss.Activity, ss.BrowserURL
	v.ContextTokens, v.Compactions = ss.ContextTokens, ss.Compactions
	var nd Needs
	if len(ss.Needs) > 0 && json.Unmarshal(ss.Needs, &nd) == nil && nd.Kind != "" {
		v.Needs = &nd
	}
	_ = json.Unmarshal(ss.Surface, &v.Surface)
	rt.mu.Lock()
	v.Control = rt.control[userID]
	rt.mu.Unlock()
	return v
}

// SetSurface changes what the user's screen shows: the browser, a report,
// or just the conversation.
func (rt *Runtime) SetSurface(ctx context.Context, u *store.User, s Surface) error {
	switch s.Kind {
	case "", "browser":
		s.ID = ""
	case "report":
		if _, err := rt.st.GetReport(ctx, u.ID, s.ID); err != nil {
			return ErrNotFound
		}
	default:
		return errors.New("unknown surface")
	}
	if err := rt.st.SetSessionSurface(ctx, u.ID, marshal(s)); err != nil {
		return err
	}
	if r := rt.running(u.ID); r != nil {
		r.mu.Lock()
		r.held = s.Kind != "browser"
		r.mu.Unlock()
	}
	rt.publishState(u.ID)
	return nil
}

// Compact summarises the older conversation now instead of waiting for the
// context to fill up.
func (rt *Runtime) Compact(ctx context.Context, u *store.User) error {
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	if rt.running(u.ID) != nil {
		return ErrBusy
	}
	err := rt.compact(ctx, u, true)
	rt.publishState(u.ID)
	return err
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
	rt.mu.Unlock()
	if err := rt.st.ResetSession(ctx, u.ID); err != nil {
		return err
	}
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
// stopped.
func (rt *Runtime) ResumeInterrupted(ctx context.Context) {
	ids, err := rt.st.SessionsWithStatus(ctx, "working")
	if err != nil {
		slog.Error("resume sessions", "err", err)
		return
	}
	for _, id := range ids {
		u, err := rt.st.GetUser(ctx, id)
		if err != nil {
			continue
		}
		env, err := rt.envs.Ensure(u.ID)
		if err != nil {
			continue
		}
		_ = rt.st.SetEntriesStatus(ctx, id, "step", "running", "failed")
		_ = rt.st.AddInbox(ctx, id, store.InboxMessage{Kind: "system", Text: stamp(userLoc(u), "", "[The server restarted while you were working. The browser may have restarted too — re-open pages as needed and carry on.]")})
		slog.Info("resuming session", "user", u.Username)
		l := rt.userLock(id)
		l.Lock()
		rt.launch(u, env, true)
		l.Unlock()
	}
}

// Shutdown cancels running loops; their sessions stay "working" and resume
// on the next start.
func (rt *Runtime) Shutdown() {
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

// ---------- internals ----------

// launch starts the agent loop for a user. The caller holds the user lock.
func (rt *Runtime) launch(u *store.User, env *userenv.Env, awaiting bool) {
	rt.mu.Lock()
	if rt.runs[u.ID] != nil {
		rt.mu.Unlock()
		return
	}
	// Most requests touch the web; start Chrome while the model thinks.
	rt.browsers.Prewarm(u.ID)
	ctx, cancel := context.WithCancel(rt.baseCtx)
	r := &run{user: u, env: env, cancel: cancel, wake: make(chan struct{}, 1), awaiting: awaiting}
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
	content := []map[string]any{{"type": "input_text", "text": text}}
	item := marshal(map[string]any{"role": "user", "content": content})
	return rt.st.AppendItems(ctx, userID, []json.RawMessage{marshal(storedItem{Item: item, Kind: kind, Attach: attach})})
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
	}
	return rt.st.SetSessionNeeds(ctx, userID, nil)
}

func (rt *Runtime) publishState(userID int64) {
	rt.hub.Publish(userID, hub.Event{Type: "state", Data: rt.View(context.Background(), userID)})
}

// showSurface puts something on the user's screen (if it isn't already).
func (rt *Runtime) showSurface(userID int64, s Surface) {
	ctx := context.Background()
	if ss, err := rt.st.GetSession(ctx, userID); err == nil {
		var cur Surface
		_ = json.Unmarshal(ss.Surface, &cur)
		if cur == s {
			return
		}
	}
	_ = rt.st.SetSessionSurface(ctx, userID, marshal(s))
	rt.publishState(userID)
}

// showReport puts a report on the user's screen and in their feed.
func (rt *Runtime) showReport(ctx context.Context, u *store.User, rep *store.Report, updated bool) {
	rt.hub.Publish(u.ID, hub.Event{Type: "report", Data: rep})
	_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "report", Text: rep.Title, Data: marshal(map[string]any{"id": rep.ID, "updated": updated})})
	_ = rt.st.SetSessionSurface(ctx, u.ID, marshal(Surface{Kind: "report", ID: rep.ID}))
	rt.publishState(u.ID)
	body := "Alexa wrote you a report."
	if rep.Format == "markdown" {
		body = trim(plain(rep.Content), 160)
	}
	_ = rt.notify(ctx, u, "report:"+rep.ID, rep.Title, body, false, false)
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

func (rt *Runtime) generateImage(ctx context.Context, prompt string) ([]byte, error) {
	body := marshal(map[string]any{"model": rt.cfg.ImageModel, "prompt": prompt, "n": 1})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, rt.cfg.BaseURL+"/images/generations", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+rt.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("image API %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil || len(out.Data) == 0 {
		return nil, fmt.Errorf("unexpected image API response")
	}
	if out.Data[0].B64 != "" {
		return base64.StdEncoding.DecodeString(out.Data[0].B64)
	}
	r2, err := http.Get(out.Data[0].URL)
	if err != nil {
		return nil, err
	}
	defer r2.Body.Close()
	return io.ReadAll(io.LimitReader(r2.Body, 64<<20))
}

func (rt *Runtime) describeSchedules(ctx context.Context, u *store.User) string {
	list, err := rt.st.ListSchedules(ctx, u.ID)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(list) == 0 {
		return "No schedules."
	}
	var b strings.Builder
	loc := userLoc(u)
	for _, sc := range list {
		when := sc.Cron
		if sc.RunAt > 0 {
			when = "once at " + time.UnixMilli(sc.RunAt).In(loc).Format("2006-01-02 15:04")
		}
		state := "enabled"
		if !sc.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(&b, "- %s %q (%s, %s): %s\n", sc.ID, sc.Name, when, state, sc.Prompt)
	}
	return b.String()
}
