package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"alexagent/internal/browser"
	"alexagent/internal/hub"
	"alexagent/internal/llm"
	"alexagent/internal/notify"
	"alexagent/internal/userenv"
	"alexagent/internal/vault"
)

// Tests of how a run ends (WP-13): Stop, a shutdown and its resume, a
// failure, and what the user sent meanwhile.

// blockingTool replaces a tool with one that signals when it starts and
// then runs until release is closed, ignoring its context, like a tool
// stuck on a call that can't be cancelled.
func (e *progressEnv) blockingTool(name string) (started, release chan struct{}) {
	started, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.fakeTool(name, func(map[string]any) (*toolResult, error) {
		once.Do(func() { close(started) })
		<-release
		return &toolResult{Text: "The 8:15 runs on time."}, nil
	})
	e.t.Cleanup(func() { close(release) })
	return started, release
}

// ctxTool replaces a tool with one that sees its context.
func (e *progressEnv) ctxTool(name string, run func(ctx context.Context, a map[string]any) (*toolResult, error)) {
	old := e.rt.toolMap[name]
	e.rt.toolMap[name] = &tool{Name: name, NoStatus: old != nil && old.NoStatus, Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) { return run(tc.ctx, a) }}
}

// conversation is everything the session's conversation holds, as stored.
func (e *progressEnv) conversation() string {
	e.t.Helper()
	rows, err := e.st.Items(e.ctx, e.user.ID, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	var b strings.Builder
	for _, r := range rows {
		b.Write(r.JSON)
		b.WriteByte('\n')
	}
	return b.String()
}

// ended is the outcome of the user's latest run, from its events.
func (e *progressEnv) ended() string {
	out := ""
	for _, ev := range e.runEvents() {
		if ev.Outcome != "" {
			out = ev.Outcome
		}
	}
	return out
}

func noContextCanceled(t *testing.T, e *progressEnv) {
	t.Helper()
	for _, x := range e.feed() {
		if strings.Contains(x.Text, "context canceled") || strings.Contains(string(x.Data), "context canceled") {
			t.Errorf("the feed shows %q: %s", x.Kind, x.Text)
		}
	}
	if c := e.conversation(); strings.Contains(c, "context canceled") {
		t.Errorf("the conversation says context canceled:\n%s", tailOf(c, 800))
	}
}

// Acceptance 1 (L1, L22): Stop during a long tool that ignores its context
// ends the run as stopped within 500ms, says so, and never shows
// "context canceled".
func TestStopDuringLongTool(t *testing.T) {
	e := newProgressEnv(t)
	started, _ := e.blockingTool("fetch_url")
	e.model.script(does(fc("fetch_url", map[string]any{"url": "https://trains.example/timetable", "status": "Reading the timetable"})))
	e.send("When does the 8:15 leave?")
	<-started
	t0 := time.Now()
	if err := e.rt.Stop(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	if !subsequence(phasesOf(e.runEvents()), []string{"started", "stopping"}) {
		t.Errorf("Stop didn't show stopping at once: %v", phasesOf(e.runEvents()))
	}
	v := e.settle()
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Errorf("the run took %s to stop", d)
	}
	if v.Status != "idle" {
		t.Errorf("status = %q, want idle", v.Status)
	}
	feed := e.feed()
	if errs := entriesOf(feed, "error"); len(errs) > 0 {
		t.Errorf("Stop showed an error: %q", errs[0].Text)
	}
	if notes := entriesOf(feed, "note"); len(notes) != 1 || notes[0].Text != "Stopped" {
		t.Errorf("notes = %v, want one Stopped", notes)
	}
	if x, _ := stepNamed(t, feed, "Reading the timetable"); x.Status != "interrupted" {
		t.Errorf("the step is %q, want interrupted", x.Status)
	}
	if got := e.ended(); got != "stopped" {
		t.Errorf("the run ended %q, want stopped", got)
	}
	noContextCanceled(t, e)
	if !strings.Contains(e.conversation(), jsonText(stopNote)) {
		t.Error("the conversation doesn't say the user stopped Alexa")
	}
}

// L1: Stop while the model is thinking ends the run the same way, and Stop
// pressed again does nothing more.
func TestStopDuringModelCall(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	e.model.script(g.turn(fc("say", map[string]any{"text": "Booked.", "final": true})))
	e.send("Book the table for eight")
	g.wait()
	_ = e.rt.Stop(e.ctx, e.user)
	_ = e.rt.Stop(e.ctx, e.user)
	e.settle()
	g.open()
	feed := e.feed()
	if len(entriesOf(feed, "error")) > 0 || len(entriesOf(feed, "note")) != 1 {
		t.Errorf("feed = %v", feed)
	}
	if n := strings.Count(e.conversation(), jsonText(stopNote)); n != 1 {
		t.Errorf("the stop note is in the conversation %d times", n)
	}
	noContextCanceled(t, e)
}

// StaleNoteOrder (L13): Stop's note comes before the next request, never
// after it.
func TestStopNoteComesBeforeTheNextRequest(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	e.model.script(g.turn())
	e.send("Book the table")
	g.wait()
	_ = e.rt.Stop(e.ctx, e.user)
	e.settle()
	g.open()
	var input string
	e.model.script(func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "Booked for eight.", "final": true})}
	}, does())
	e.send("Actually, go ahead and book it")
	e.settle()
	stop, next := strings.Index(input, "The user stopped you"), strings.Index(input, "Actually, go ahead and book it")
	if stop < 0 || next < 0 || stop > next {
		t.Errorf("stop note at %d, new request at %d:\n%s", stop, next, tailOf(input, 800))
	}
}

// StrandedAfterStop (L2): a message sent after Stop, while the stopped run
// is still winding down, is answered.
func TestMessageAfterStopIsAnswered(t *testing.T) {
	e := newProgressEnv(t)
	started, _ := e.blockingTool("fetch_url")
	var input string
	e.model.script(
		does(fc("fetch_url", map[string]any{"url": "https://trains.example/timetable", "status": "Reading the timetable"})),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "It’s 4:20.", "final": true})}
		},
		does(),
	)
	e.send("When does the 8:15 leave?")
	<-started
	_ = e.rt.Stop(e.ctx, e.user)
	e.send("No wait, just tell me the time")
	v := e.settle()
	if v.Status != "idle" {
		t.Errorf("status = %q", v.Status)
	}
	if !strings.Contains(input, "No wait, just tell me the time") {
		t.Fatalf("the message after Stop wasn't answered; the model saw:\n%s", tailOf(input, 600))
	}
	if stop, msg := strings.Index(input, "The user stopped you"), strings.Index(input, "No wait"); stop > msg {
		t.Error("the stop note came after the message sent after it")
	}
	if says := entriesOf(e.feed(), "say"); len(says) != 1 {
		t.Errorf("says = %v", says)
	}
	if n, _ := e.st.InboxCount(e.ctx, e.user.ID); n != 0 {
		t.Errorf("%d messages left in the inbox", n)
	}
}

// A message sent before Stop was part of what Stop stopped: it joins the
// conversation, before Stop's note, and starts nothing.
func TestMessageBeforeStopIsKeptNotRun(t *testing.T) {
	e := newProgressEnv(t)
	started, _ := e.blockingTool("fetch_url")
	e.model.script(does(fc("fetch_url", map[string]any{"url": "https://trains.example/timetable", "status": "Reading the timetable"})))
	e.send("When does the 8:15 leave?")
	<-started
	e.send("And the 9:15")
	_ = e.rt.Stop(e.ctx, e.user)
	e.settle()
	c := e.conversation()
	msg, stop := strings.Index(c, "And the 9:15"), strings.Index(c, "The user stopped you")
	if msg < 0 || stop < 0 || msg > stop {
		t.Errorf("message at %d, stop note at %d", msg, stop)
	}
	if n, _ := e.st.InboxCount(e.ctx, e.user.ID); n != 0 {
		t.Errorf("%d messages left in the inbox", n)
	}
	if n := len(e.model.requests()); n != 1 {
		t.Errorf("%d model calls, want 1", n)
	}
}

// StrandedAfterFail (L2, acceptance 3): a message sent during a run that
// then fails is answered by a new run, which knows why the last one ended.
func TestMessageDuringFailedRunIsAnswered(t *testing.T) {
	e := newProgressEnv(t)
	g := newGate()
	var input string
	e.model.script(
		g.turn(sse{"type": "error", "code": "invalid_prompt", "message": "The model couldn’t read that."}),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "The weekend one leaves at 9.", "final": true})}
		},
		does(),
	)
	e.send("Check the timetable")
	g.wait()
	e.send("And the weekend one too")
	g.open()
	v := e.settle()
	if v.Status != "idle" {
		t.Errorf("status = %q", v.Status)
	}
	if n := len(entriesOf(e.feed(), "error")); n != 1 {
		t.Errorf("%d error entries, want 1", n)
	}
	if !strings.Contains(input, "And the weekend one too") || !strings.Contains(input, "Your previous turn ended with an error") {
		t.Fatalf("the follow-up wasn't answered after the error; the model saw:\n%s", tailOf(input, 800))
	}
	if errAt, msg := strings.Index(input, "Your previous turn ended"), strings.Index(input, "And the weekend one too"); errAt > msg {
		t.Error("the error note came after the follow-up")
	}
	if n, _ := e.st.InboxCount(e.ctx, e.user.ID); n != 0 {
		t.Errorf("%d messages left in the inbox", n)
	}
}

// L23: Stop brings back a session that says it's working with nothing
// running.
func TestStopClearsAStuckSession(t *testing.T) {
	e := newProgressEnv(t)
	if err := e.st.SetSessionStatus(e.ctx, e.user.ID, "working", "Thinking"); err != nil {
		t.Fatal(err)
	}
	_ = e.rt.Stop(e.ctx, e.user)
	if ss, _ := e.st.GetSession(e.ctx, e.user.ID); ss.Status != "idle" {
		t.Errorf("status = %q, want idle", ss.Status)
	}
}

// restart starts a second Runtime on the same store and data directory,
// as the server does when it starts again, with a model of its own.
func (e *progressEnv) restart() (*Runtime, *scriptModel) {
	t := e.t
	dir := e.cfg.DataDir
	ctx, cancel := context.WithCancel(context.Background())
	h := hub.New()
	n, err := notify.New(ctx, e.st, h, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	envs := userenv.New(dir, false, 0)
	br := browser.NewManager(browser.Options{ChromePath: filepath.Join(dir, "no-chrome"), Headless: true, Width: 1280, Height: 800}, func(id int64) browser.UserEnv {
		x := envs.For(id)
		return browser.UserEnv{UserID: id, ProfileDir: x.Profile, DownloadDir: x.Downloads, HomeDir: x.Home}
	}, nil)
	v, err := vault.Open(filepath.Join(dir, "vault.key"), e.st)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptModel{t: t}
	srv := httptest.NewServer(model)
	rt := New(ctx, e.cfg, e.st, llm.New(srv.URL, "test"), br, envs, h, n, v)
	if rt.toolMap["plan"] == nil {
		pt := planTool()
		rt.tools, rt.toolMap["plan"], rt.toolDefs = append(rt.tools, pt), pt, append(rt.toolDefs, pt.schema())
	}
	t.Cleanup(func() { rt.Shutdown(); br.CloseAll(); srv.Close(); cancel() })
	return rt, model
}

func settleOn(t *testing.T, rt *Runtime, userID int64) View {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v := rt.View(context.Background(), userID)
		if v.Status != "working" && rt.running(userID) == nil {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("still working (%s)", v.Activity)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ShutdownDuringTool (L1, acceptance 2): a shutdown during a tool leaves the
// session working, and the next start resumes it once, however often
// ResumeInterrupted is called.
func TestShutdownDuringToolResumesOnce(t *testing.T) {
	e := newProgressEnv(t)
	started, _ := e.blockingTool("fetch_url")
	e.model.script(does(fc("fetch_url", map[string]any{"url": "https://trains.example/timetable", "status": "Reading the timetable"})))
	e.send("When does the 8:15 leave?")
	<-started
	t0 := time.Now()
	e.rt.Shutdown()
	if d := time.Since(t0); d > time.Second {
		t.Errorf("shutdown took %s", d)
	}
	ss, _ := e.st.GetSession(e.ctx, e.user.ID)
	if ss.Status != "working" {
		t.Fatalf("after the shutdown the session is %q, want working so it resumes", ss.Status)
	}
	if x, _ := stepNamed(t, e.feed(), "Reading the timetable"); x.Status != "interrupted" {
		t.Errorf("the step is %q, want interrupted", x.Status)
	}
	if len(entriesOf(e.feed(), "error")) > 0 || len(entriesOf(e.feed(), "note")) > 0 {
		t.Errorf("a shutdown showed %v", e.feed())
	}

	rt, model := e.restart()
	var input string
	model.script(func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "The 8:15 runs on time.", "final": true})}
	}, does())
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); rt.ResumeInterrupted(e.ctx) }()
	}
	wg.Wait()
	settleOn(t, rt, e.user.ID)
	rt.ResumeInterrupted(e.ctx) // idle now: nothing to resume
	settleOn(t, rt, e.user.ID)
	if n := strings.Count(e.conversation(), "The server restarted while you were working"); n != 1 {
		t.Errorf("the restart note is in the conversation %d times, want 1", n)
	}
	if !strings.Contains(input, "When does the 8:15 leave?") || !strings.Contains(input, "The server restarted") {
		t.Errorf("the resumed run saw:\n%s", tailOf(input, 600))
	}
	if says := entriesOf(e.feed(), "say"); len(says) != 1 {
		t.Errorf("says = %v, want one", says)
	}
	noContextCanceled(t, e)
}

// A scheduled job interrupted by a shutdown resumes in the background, as
// it was; a user's request resumes owing them an answer.
func TestResumeKeepsTheKindOfRun(t *testing.T) {
	e := newProgressEnv(t)
	started, _ := e.blockingTool("fetch_url")
	e.model.script(does(fc("fetch_url", map[string]any{"url": "https://flights.example/ua1180", "status": "Checking UA 1180"})))
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Check UA 1180's status", Source: "schedule", Label: "Flight watch"}); err != nil {
		t.Fatal(err)
	}
	<-started
	e.rt.Shutdown()
	flags := resumeFlags{}
	if v, err := e.st.GetKV(e.ctx, resumeKey(e.user.ID)); err != nil || json.Unmarshal([]byte(v), &flags) != nil {
		t.Fatalf("no resume flags saved: %q %v", v, err)
	}
	if flags.Awaiting || !flags.Held {
		t.Errorf("flags = %+v, want a held background run", flags)
	}
	rt, model := e.restart()
	model.script(does())
	rt.ResumeInterrupted(e.ctx)
	settleOn(t, rt, e.user.ID)
	if got := rt.takeResume(e.ctx, e.user.ID); !got.Awaiting || got.Held {
		t.Errorf("the flags weren't cleared: %+v", got)
	}
}

// A user's request whose screen they held when the server went down
// resumes as their request, not as an unattended scheduled job; the hold
// itself stays with the session's surface.
func TestResumeKeepsAHeldRequestInTheForeground(t *testing.T) {
	e := newProgressEnv(t)
	started, _ := e.blockingTool("fetch_url")
	e.model.script(does(fc("say", map[string]any{"text": "On it."}), fc("fetch_url", map[string]any{"url": "https://flights.example/ua1180", "status": "Checking UA 1180"})))
	e.send("Research flights to Porto")
	<-started
	if err := e.rt.SetSurface(e.ctx, e.user, Surface{}); err != nil {
		t.Fatal(err)
	}
	e.rt.Shutdown()
	flags := resumeFlags{}
	if v, err := e.st.GetKV(e.ctx, resumeKey(e.user.ID)); err != nil || json.Unmarshal([]byte(v), &flags) != nil {
		t.Fatalf("no resume flags saved: %q %v", v, err)
	}
	if flags.Held {
		t.Errorf("flags = %+v, want a foreground run", flags)
	}
	if v := e.rt.View(e.ctx, e.user.ID); !v.Surface.Held {
		t.Errorf("the user's hold is gone: %+v", v.Surface)
	}
}

// B06: taking over the browser ends the agent's browser action in flight,
// and Alexa hears why.
func TestTakeOverCutsTheBrowserAction(t *testing.T) {
	e := newProgressEnv(t)
	started := make(chan struct{})
	e.ctxTool("browser_click", func(ctx context.Context, _ map[string]any) (*toolResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	var input string
	e.model.script(
		does(fc("browser_click", map[string]any{"index": 4, "status": "Clicking Buy"})),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "Over to you.", "final": true})}
		},
		does(),
	)
	e.send("Buy the headphones")
	<-started
	if err := e.rt.TakeOver(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	e.settle()
	_ = e.rt.HandBack(e.ctx, e.user, "")
	if !strings.Contains(input, "the user took control of the browser while this was running") {
		t.Errorf("Alexa wasn't told why the click ended:\n%s", tailOf(input, 600))
	}
	if x, _ := stepNamed(t, e.feed(), "Clicking Buy"); x.Status != "interrupted" {
		t.Errorf("the step is %q, want interrupted", x.Status)
	}
}

// Acceptance 4 (B07), with a real browser: Stop during a navigation that
// takes seconds returns within 300ms.
func TestStopDuringSlowNavigation(t *testing.T) {
	if os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}
	e := newProgressEnv(t)
	dir := t.TempDir()
	br := browser.NewManager(browser.Options{Headless: true, Width: 1280, Height: 800, IdleAfter: time.Hour}, func(id int64) browser.UserEnv {
		return browser.UserEnv{UserID: id, ProfileDir: dir + "/profile", DownloadDir: dir + "/dl", HomeDir: dir}
	}, nil)
	t.Cleanup(br.CloseAll)
	if _, err := br.Get(e.user.ID); err != nil {
		t.Skipf("no Chrome to run: %v", err)
	}
	e.rt.browsers = br
	asked, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slow" {
			http.NotFound(w, r)
			return
		}
		once.Do(func() { close(asked) })
		select {
		case <-time.After(6 * time.Second):
		case <-done:
		}
		_, _ = w.Write([]byte("<html><body>slow page</body></html>"))
	}))
	t.Cleanup(func() { close(done); slow.Close() })
	e.model.script(does(fc("browser_open", map[string]any{"url": slow.URL + "/slow", "status": "Opening the slow page"})))
	e.send("Open the slow page")
	select {
	case <-asked:
	case <-time.After(20 * time.Second):
		t.Fatal("the page was never asked for")
	}
	t0 := time.Now()
	_ = e.rt.Stop(e.ctx, e.user)
	e.settle()
	d := time.Since(t0)
	if d > 300*time.Millisecond {
		t.Errorf("Stop took %s during a navigation", d)
	}
	t.Logf("stopped %s after Stop", d.Round(time.Millisecond))
	if x, _ := stepNamed(t, e.feed(), "Opening the slow page"); x.Status != "interrupted" {
		t.Errorf("the step is %q, want interrupted", x.Status)
	}
	if errs := entriesOf(e.feed(), "error"); len(errs) > 0 {
		t.Errorf("Stop showed an error: %q", errs[0].Text)
	}
	noContextCanceled(t, e)
}
