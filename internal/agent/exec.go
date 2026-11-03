package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"alexagent/internal/browser"
	"alexagent/internal/hub"
	"alexagent/internal/llm"
)

// What a call returns when it doesn't run, or runs only partly. Each says
// why and what to do instead; "Not run" and "Skipped" show as skipped steps.
const (
	notRunAsked    = "Not run: you asked the user first; call it again after they answer."
	notRunAsking   = "Not run: you're asking the user in this turn, so this waits for their answer; call it again after they answer."
	notRunUnasked  = "Not run: it came after a question in this turn, which wasn't asked (its result says why). Ask again, and call this after the user answers."
	skippedMessage = "Skipped: the user sent a new message."
	skippedPage    = "Skipped: the page changed after the previous action. Use the indexes in the latest state."
	skippedControl = "Skipped: the user used the browser after you wrote this, so the page may have changed. Use the indexes in the latest state."
	notRunStopped  = "Not run: the user stopped you before this step."
	notRunRestart  = "Not run: the server restarted before this step."
)

// Why a call was cut short (context.Cause of its context), besides Stop
// and a shutdown.
var (
	errTookOver   = errors.New("the user took control of the browser")
	errNewMessage = errors.New("the user sent a new message")
)

// stopGrace is how long a stopped run waits for a tool to notice before
// leaving it to finish on its own.
const stopGrace = 200 * time.Millisecond

// guard is the loop guards' hook (guards.go, WP-16), which sets it from its
// init. It sees every call that's about to run: it may block it, returning
// its own result without calling run, or call run and add a warning to the
// result. Until guards.go sets it, nothing is guarded.
var guard = func(rt *Runtime, r *run, name string, args map[string]any, run func() *toolResult) *toolResult {
	return run()
}

// execute runs one tool call, never failing the loop. Nothing runs once a
// call in the batch has asked the user something: Alexa waits for the
// answer.
func (rt *Runtime) execute(ctx context.Context, r *run, call llm.Item) (out *toolResult) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("tool panic", "tool", call.Name, "panic", p, "stack", string(debug.Stack()))
			out = rt.toolEnded(ctx, r, &toolResult{Text: fmt.Sprintf("Error: the tool crashed (%v).", p)}, errToolCrashed)
		}
	}()
	tl := rt.toolMap[call.Name]
	if tl == nil {
		return &toolResult{Text: fmt.Sprintf("Error: unknown tool %q.", call.Name)}
	}
	args := map[string]any{}
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return &toolResult{Text: "Error: arguments were not valid JSON: " + err.Error()}
		}
	}
	if r.hasAsked() {
		return &toolResult{Text: notRunAsked}
	}
	if s := strings.TrimSpace(str(args, "status")); s != "" {
		rt.setStep(r, s, strings.TrimSpace(str(args, "done")))
	} else if tl.NoStatus {
		// Alexa says something, asks or shows a report once the step
		// before it is over.
		rt.finishStep(r, "done")
	} else if !r.hasStep() {
		rt.setStep(r, "Working on it", "")
	}
	if strings.HasPrefix(call.Name, "browser_") {
		// It has waited for the user to hand the browser back (runBatch).
		// The user's screen follows the agent into the browser, unless they
		// chose to look at something else during this request. Results said
		// before this are no longer the last word (R5).
		r.mu.Lock()
		r.results = false
		r.mu.Unlock()
		if !r.isHeld() {
			rt.showSurface(r.user.ID, Surface{Kind: "browser"})
		}
	}
	slog.Debug("tool call", "user", r.user.ID, "tool", call.Name, "args", call.Arguments)
	ran := false
	res := guard(rt, r, call.Name, args, func() *toolResult {
		ran = true
		return rt.runCall(ctx, r, tl, args)
	})
	if !ran {
		// Blocked by a guard: the step shows how, as for any call.
		rt.toolStarted(r, call.Name, args)
		res = rt.toolEnded(ctx, r, res, nil)
	}
	return res
}

// runCall runs the tool in a context of its own, which Stop, a shutdown, a
// new message (Interrupt) or the user taking over the browser cuts short.
// The step records what really happened (steps.go): where, on what, how
// long, and whether it worked.
func (rt *Runtime) runCall(ctx context.Context, r *run, tl *tool, args map[string]any) *toolResult {
	callCtx, cut := context.WithCancelCause(ctx)
	defer cut(nil)
	r.setCall(cut, tl.Name)
	defer r.setCall(nil, "")
	rt.toolStarted(r, tl.Name, args)
	t0 := time.Now()
	res, err := rt.runTool(callCtx, r, tl, args)
	slog.Info("tool", "user", r.user.ID, "name", tl.Name, "took", time.Since(t0).Round(time.Millisecond), "err", err)
	switch {
	case errors.Is(err, errToolCrashed) && res != nil:
		// The crash text is for the model; the step shows errToolCrashed.
	case err != nil:
		res = &toolResult{Text: "Error: " + err.Error()}
	case res == nil:
		res = &toolResult{Text: "OK"}
	}
	if callCtx.Err() != nil {
		res = rt.cutShort(r, callCtx, res)
	}
	if res.Suspend != nil {
		r.mu.Lock()
		r.asked = true
		r.mu.Unlock()
	}
	res = rt.toolEnded(callCtx, r, res, err)
	if writesFiles(tl.Name, args) || strings.Contains(res.Text, "Note: downloaded ") {
		// The workspace may have changed: a Files tab open on another
		// screen fetches it again. A browser download shows in the page
		// state that follows it (browser.downloadNote).
		rt.hub.Publish(r.user.ID, hub.Changed("files"))
	}
	if r.isStopping() {
		// The step's end says Alexa is thinking again; she's stopping.
		rt.runPhase(r, "stopping")
		rt.setActivity(r, "Stopping")
	}
	return res
}

// writesFiles says whether a tool call may have written workspace files.
func writesFiles(name string, args map[string]any) bool {
	switch name {
	case "write_file", "run_shell", "generate_image":
		return true
	case "fetch_url":
		return str(args, "save_as") != ""
	}
	return false
}

// runTool runs the tool on a goroutine of its own, so a tool that doesn't
// watch its context can't hold up a Stop (L22): once the run is stopped,
// the tool has stopGrace to finish and is then left to end by itself. A
// browser tool's tab is bound to the call, so its actions end as soon as
// the call does (B07), and stay bound until the tool is done.
func (rt *Runtime) runTool(ctx context.Context, r *run, tl *tool, args map[string]any) (*toolResult, error) {
	uid := r.user.ID
	type result struct {
		res *toolResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		var out result
		defer func() {
			if p := recover(); p != nil {
				slog.Error("tool panic", "tool", tl.Name, "panic", p, "stack", string(debug.Stack()))
				out = result{&toolResult{Text: fmt.Sprintf("Error: the tool crashed (%v).", p)}, errToolCrashed}
			}
			done <- out
		}()
		if strings.HasPrefix(tl.Name, "browser_") {
			// Here, not before: starting the browser can take a while too.
			if tab := rt.callTab(uid); tab != nil {
				defer tab.Bind(ctx)()
			}
		}
		res, err := tl.Run(&toolCtx{ctx: ctx, rt: rt, run: r}, args)
		out = result{res, err}
	}()
	var stopped <-chan struct{} // a new message or a take-over waits for the tool
	if r.ctx != nil {
		stopped = r.ctx.Done()
	}
	select {
	case d := <-done:
		return d.res, d.err
	case <-stopped:
	}
	select {
	case d := <-done:
		return d.res, d.err
	case <-time.After(stopGrace):
		slog.Warn("tool still running after the run stopped; leaving it", "user", uid, "tool", tl.Name)
		return nil, context.Cause(ctx)
	}
}

// callTab is the tab a browser tool is about to use, starting the browser
// if it isn't up yet (as the tool would), or nil if it can't be had.
func (rt *Runtime) callTab(userID int64) *browser.Tab {
	if t := rt.peekTab(userID); t != nil {
		return t
	}
	b, err := rt.browsers.Get(userID)
	if err != nil {
		return nil
	}
	t, _ := b.Tab(tabKey)
	return t
}

// cutShort is what the model reads of a call that was cut short: why, that
// it may not have finished, and what the tool had to say, if anything.
func (rt *Runtime) cutShort(r *run, ctx context.Context, res *toolResult) *toolResult {
	cause := context.Cause(ctx)
	var line string
	switch {
	case errors.Is(cause, errTookOver):
		line = "Interrupted: the user took control of the browser while this was running, so it may not have finished. Check the page before doing it again."
	case errors.Is(cause, errNewMessage):
		line = "Interrupted: the user sent a new message while this was running, so it may not have finished. Read it before going on."
	case !r.isStopping() && rt.shuttingDown():
		line = "Interrupted by a server restart while this was running; it may or may not have finished. Check before doing it again."
	default:
		line = "Stopped: the user stopped you while this was running, so it may not have finished. Check before doing it again."
	}
	if res == nil || strings.HasPrefix(res.Text, "Error:") || res.Text == "OK" || res.Text == "Stopped." {
		return &toolResult{Text: line}
	}
	out := *res
	out.Text = line + "\n\n" + res.Text
	if out.Short != "" {
		out.Short = line + "\n" + out.Short
	}
	return &out
}

// steered reports whether the user has written since the message with
// inbox id mark, the last one before the batch started: the batch was
// written without it, so what hasn't started yet waits for Alexa to read
// it. A message that came while the model was writing the batch doesn't
// stop it; Alexa reads it next turn.
func (rt *Runtime) steered(ctx context.Context, userID, mark int64) bool {
	has, err := rt.st.InboxHas(ctx, userID, "user", mark)
	return err == nil && has
}

// skipStep shows a call that won't run as a skipped step, under its label,
// with the step's record like any call's (steps.go reads a "Not run" result
// as skipped). It's a step of its own even under the label of the step
// before: that step's calls ran, and keep how they went.
func (rt *Runtime) skipStep(ctx context.Context, r *run, name string, args map[string]any) {
	s := strings.TrimSpace(str(args, "status"))
	if s == "" {
		return
	}
	rt.finishStep(r, "done")
	rt.setStep(r, s, "")
	rt.toolStarted(r, name, args)
	rt.toolEnded(ctx, r, &toolResult{Text: "Not run: the user sent a new message."}, nil)
	rt.finishStep(r, "skipped")
}

// takeSteer returns and forgets the inbox id an interrupt was sent after
// (Runtime.Interrupt), if there was one.
func (r *run) takeSteer() (int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.steerAt, r.steer
	r.steer, r.steerAt = false, 0
	return at, ok
}

// markTurn notes, before a model turn reads the conversation, which of the
// user's take-overs it will know of: HandBack queues its note before it
// lets go, so each one finished by now is in what the turn reads.
func (r *run) markTurn() {
	r.mu.Lock()
	r.turnSeen, r.turnHeld = r.takeovers, r.released != nil
	r.mu.Unlock()
}

// tookOver reports whether the user has had the browser since the model's
// turn began: its batch was aimed at a page they may have changed.
func (r *run) tookOver() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.turnHeld || r.takeovers != r.turnSeen
}

func (r *run) hasAsked() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked
}

// setCall records the call in flight and how to cut it short. A browser
// action that starts once the user has taken the browser (they took over
// just after waitControl let it through) is cut at once, as TakeOver cuts
// one already going, so it can't act on the page they now control (B06).
func (r *run) setCall(cut context.CancelCauseFunc, tool string) {
	r.mu.Lock()
	r.cut, r.cutTool = cut, tool
	taken := cut != nil && r.released != nil && strings.HasPrefix(tool, "browser_")
	r.mu.Unlock()
	if taken {
		cut(errTookOver)
	}
}

// cutCall cuts short the call in flight, if there is one (and, with
// browserOnly, only if it's a browser action), giving the reason.
func (r *run) cutCall(cause error, browserOnly bool) {
	r.mu.Lock()
	cut, tool := r.cut, r.cutTool
	r.mu.Unlock()
	if cut != nil && (!browserOnly || strings.HasPrefix(tool, "browser_")) {
		cut(cause)
	}
}

// waitControl blocks while the user controls the browser. Returns false if
// the run was cancelled meanwhile. It also stops waiting once the user has
// written since mark (the inbox id before the batch): the call was chosen
// without their message, so the caller skips it (steer-skip) rather than
// run it on a page they may have changed.
func (rt *Runtime) waitControl(ctx context.Context, r *run, mark int64) bool {
	waited := false
	for {
		r.mu.Lock()
		ch := r.released
		r.mu.Unlock()
		if ch == nil {
			if waited && ctx.Err() == nil {
				_ = rt.st.SetSessionActivity(ctx, r.user.ID, "Picking up where you left off")
				rt.publishState(r.user.ID)
			}
			return ctx.Err() == nil
		}
		if rt.steered(ctx, r.user.ID, mark) {
			return ctx.Err() == nil
		}
		if !waited {
			waited = true
			rt.runPhase(r, "waiting")
			_ = rt.st.SetSessionActivity(ctx, r.user.ID, "You’re in control")
			rt.publishState(r.user.ID)
		}
		select {
		case <-ch:
		case <-r.wake: // something was sent: the user's own message ends the wait
		case <-ctx.Done():
			return false
		}
	}
}

// batchEnd is how a batch of tool calls left the run.
type batchEnd struct {
	suspend *Needs // a call waits for the user; its output comes with the answer
	told    bool   // in the wrap-up turn, Alexa told the user where things stand
	final   bool   // the batch ended on a final say or report
	err     error  // an output couldn't be saved
}

// runBatch runs the tool calls of one model response in order and saves
// each output as soon as its call ends. Outputs are saved without the run's
// context, so a Stop or a shutdown mid-batch can neither lose them nor turn
// into an error (L1), and a call the stop kept from starting gets an output
// that says so. Besides what execute enforces for every call:
//   - an action batched before an ask waits for the answer too, so nothing
//     is done before the user confirms (H1), and nothing batched after a
//     question runs, even when the question was refused;
//   - a browser action waits while the user has the browser;
//   - once the user writes, what hasn't started is skipped, and its step
//     shows skipped: Alexa reads the message first (steer-skip); after an
//     interrupt she hadn't read when she wrote the batch, all of it is;
//   - once a browser action has changed the page, later calls aimed at an
//     index or a point on it are skipped: they were chosen on a page that's
//     gone (H2), and so are they once the user has had the browser since
//     the batch was written;
//   - a final say or report that ends the batch lets the run end without
//     another model turn (H3).
func (rt *Runtime) runBatch(ctx context.Context, r *run, calls []llm.Item, wrapUp bool) batchEnd {
	uid := r.user.ID
	save := context.WithoutCancel(ctx)
	var b batchEnd
	mark, err := rt.st.InboxLast(save, uid)
	if err != nil {
		mark = 1<<62 - 1 // can't tell what's new: don't skip for it
	}
	if at, ok := r.takeSteer(); ok {
		// An interrupt the model hasn't read yet: what it wrote without it
		// doesn't run.
		mark = min(mark, at)
	}
	// One that comes during the batch skips its rest, and Alexa reads it
	// before her next turn.
	defer r.takeSteer()
	askAt := slices.IndexFunc(calls, func(c llm.Item) bool { return c.Name == "ask" })
	watch := pageWatch{needed: needsWatch(calls)}
	if watch.needed {
		watch.seen = pageSignature(rt.seenPage(save, r))
	}
	questioned := false // a question in this batch ran, whether or not it was asked
	for i, call := range calls {
		args := callArgs(call)
		ran := false
		var out *toolResult
		switch {
		case ctx.Err() != nil:
			out = &toolResult{Text: rt.notRunText(r)}
		case b.suspend != nil:
			out = &toolResult{Text: notRunAsked}
		case questioned:
			// What the model batched after a question was meant to wait for
			// the answer; a refused question (too long, a bad card) mustn't
			// let it run unconfirmed (H1).
			out = &toolResult{Text: notRunUnasked}
		case i < askAt && !harmless[call.Name]:
			out = &toolResult{Text: notRunAsking}
		case strings.HasPrefix(call.Name, "browser_") && !wrapUp && !rt.waitControl(ctx, r, mark):
			// Stopped, or the server went down, while the user had the
			// browser: say which, as for a call the stop kept from starting.
			out = &toolResult{Text: rt.notRunText(r)}
		case rt.steered(save, uid, mark):
			rt.skipStep(ctx, r, call.Name, args)
			out = &toolResult{Text: skippedMessage}
		case wrapUp && !talks(call.Name):
			out = &toolResult{Text: "Not done: no steps left for now. Tell the user where things stand."}
		case aimed(call.Name, args) && r.tookOver():
			out = &toolResult{Text: skippedControl}
		case watch.stale(call.Name, args):
			out = &toolResult{Text: skippedPage}
		default:
			out = rt.execute(ctx, r, call)
			watch.after(call.Name, out)
			ran = true
			b.told = b.told || wrapUp
		}
		questioned = questioned || ran && asksUser[call.Name]
		if out.Suspend != nil {
			if b.suspend == nil {
				b.suspend = out.Suspend
				b.suspend.CallID = call.CallID
				b.suspend.ToolName = call.Name
				continue // its output is written when the user responds
			}
			out = &toolResult{Text: "Only one request to the user at a time; ask again after the first is answered."}
		}
		if err := rt.saveOutput(save, uid, call.CallID, out); err != nil {
			b.err = err
			return b
		}
		b.final = ran && i == len(calls)-1 && isFinal(call.Name, args, out)
	}
	if ctx.Err() != nil && b.suspend != nil {
		// Stopped before the question was shown: it's withdrawn.
		_ = rt.resolveNeeds(save, uid, b.suspend, rt.withdrawn(r), nil)
		b.suspend = nil
	}
	return b
}

// saveOutput stores a call's output for replay, with its screenshot.
func (rt *Runtime) saveOutput(ctx context.Context, userID int64, callID string, out *toolResult) error {
	si := storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": callID, "output": out.Text}), Kind: out.Kind, Short: out.Short}
	if len(out.Image) > 0 {
		if name, err := rt.saveShot(userID, out.Image); err == nil {
			si.Shot = name
		}
	}
	return rt.st.AppendItems(ctx, userID, []json.RawMessage{marshal(si)})
}

// withdrawn is the answer to a question a stop kept from being shown.
func (rt *Runtime) withdrawn(r *run) string {
	if !r.isStopping() && rt.shuttingDown() {
		return "Not asked: the server restarted before the user saw this. Ask again if you still need to."
	}
	return "Stopped before the user answered."
}

// notRunText is the output of a call a stop kept from starting.
func (rt *Runtime) notRunText(r *run) string {
	if !r.isStopping() && rt.shuttingDown() {
		return notRunRestart
	}
	return notRunStopped
}

// harmless are the calls that may run before an ask in the same batch: they
// read, show or navigate, and change nothing the user would want to
// approve first.
var harmless = map[string]bool{
	"say": true, "plan": true, "present_report": true, "memory_save": true,
	"browser_open": true, "browser_state": true, "browser_read": true, "browser_scroll": true, "browser_hover": true,
	"read_file": true, "list_files": true, "fetch_url": true, "schedule_list": true, "recall": true,
}

// asksUser are the calls that wait for the user: nothing batched after one
// runs before they respond.
var asksUser = map[string]bool{"ask": true, "request_takeover": true, "request_login": true}

// talks reports whether the tool speaks to the user.
func talks(name string) bool { return name == "say" || name == "ask" || name == "present_report" }

// isFinal reports whether a call ends the work on the request: a say or a
// report sent with final that went out, and whose result doesn't carry the
// plan's completion check (the model must read that first).
func isFinal(name string, args map[string]any, out *toolResult) bool {
	if (name != "say" && name != "present_report") || !boolean(args, "final", false) {
		return false
	}
	if status, _ := callOutcome(context.Background(), name, out, nil); status != "done" || strings.HasPrefix(out.Text, "Skipped:") {
		return false
	}
	return !strings.Contains(out.Text, planReminderLead)
}

func callArgs(c llm.Item) map[string]any {
	args := map[string]any{}
	_ = json.Unmarshal([]byte(c.Arguments), &args)
	return args
}

// pageWatch is the page-change barrier (H2). Indexes and points refer to
// the page as the model last saw it; once an action in the batch has
// changed what's on it, later calls aimed at it are skipped.
type pageWatch struct {
	needed  bool   // the batch has a call aimed at the page after an action on it
	seen    string // the signature of the page the model saw ("" if unknown)
	changed bool
}

func (w *pageWatch) stale(name string, args map[string]any) bool {
	return w.changed && aimed(name, args)
}

// after records whether the action changed the page. Its result shows the
// page now; one without a page (the action failed before reading it)
// counts as a change, and so does any action when the page the model saw
// is unknown.
func (w *pageWatch) after(name string, out *toolResult) {
	if !w.needed || w.changed || !changesIndexes(name) {
		return
	}
	now := ""
	if out.Kind == "browser_state" {
		now = pageSignature(out.Text)
	}
	w.changed = now == "" || now != w.seen
}

// needsWatch reports whether a call aimed at the page comes after an
// action on it, the only case the barrier is for.
func needsWatch(calls []llm.Item) bool {
	acted := false
	for _, c := range calls {
		if acted && aimed(c.Name, callArgs(c)) {
			return true
		}
		acted = acted || changesIndexes(c.Name)
	}
	return false
}

// changesIndexes reports whether a browser tool may change the page or the
// indexes on it: an action, or browser_state, whose fresh indexes replace
// the ones the model saw (Tab.State).
func changesIndexes(name string) bool { return changesPage(name) || name == "browser_state" }

// changesPage reports whether a browser tool may change the page: all but
// the two that only read it.
func changesPage(name string) bool {
	return strings.HasPrefix(name, "browser_") && name != "browser_state" && name != "browser_read"
}

// aimed reports whether a browser call acts on an element index or a point
// of the latest state.
func aimed(name string, args map[string]any) bool {
	if !strings.HasPrefix(name, "browser_") {
		return false
	}
	for k := range args {
		switch {
		case k == "index" || strings.HasSuffix(k, "_index"):
			if num(args, k, 0) > 0 {
				return true
			}
		case k == "x" || k == "y" || strings.HasSuffix(k, "_x") || strings.HasSuffix(k, "_y"):
			return true
		}
	}
	return false
}

// seenPage is the latest browser state the model has seen when the batch
// starts: the last one this run returned, or else the last one in the
// conversation.
func (rt *Runtime) seenPage(ctx context.Context, r *run) string {
	if p := peekProgress(r); p != nil {
		p.mu.Lock()
		text := p.page.text
		p.mu.Unlock()
		if text != "" {
			return text
		}
	}
	_, rows, err := rt.st.Conversation(ctx, r.user.ID)
	if err != nil {
		return ""
	}
	for i := len(rows) - 1; i >= 0; i-- {
		var si storedItem
		if json.Unmarshal(rows[i].JSON, &si) != nil || si.Kind != "browser_state" {
			continue
		}
		var o struct {
			Output string `json:"output"`
		}
		if json.Unmarshal(si.Item, &o) == nil {
			return o.Output
		}
	}
	return ""
}

var stateAttrRe = regexp.MustCompile(` (checked|unchecked|selected|disabled|expanded=\w+)\b`)

// pageSignature is what identifies the elements of a browser state: the
// page, then each indexed element in order, without what was typed into it
// and without its checked, selected, expanded or disabled state. So typing
// into fields and ticking boxes keep it, and a form can be filled in one
// batch; an element appearing, going or moving (a suggestion list, another
// page) changes it. A text without an element list has none ("").
func pageSignature(text string) string {
	var b strings.Builder
	url, in, found := "", false, false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case url == "" && strings.HasPrefix(l, "URL: "):
			url = cleanURL(strings.TrimPrefix(l, "URL: "))
		case strings.HasPrefix(l, "Interactive elements"):
			in, found = true, true
		case in && strings.HasPrefix(l, "["):
			_, d, _ := strings.Cut(l, "] ")
			b.WriteString(elementKey(d) + "\n")
		case in:
			in = false
		}
	}
	if !found {
		return ""
	}
	return url + "\n" + b.String()
}

// elementKey is an element's description without its value or state.
func elementKey(d string) string {
	head, text := d, ""
	if i := strings.Index(d, ">"); strings.HasPrefix(d, "<") && i > 0 {
		head, text = d[:i+1], strings.TrimSpace(d[i+1:])
	}
	if strings.HasPrefix(text, "value=") || strings.HasPrefix(text, "options:") {
		text = ""
	}
	return stateAttrRe.ReplaceAllString(head, "") + " " + text
}
