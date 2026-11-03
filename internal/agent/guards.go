package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"alexagent/internal/llm"
)

// Loop guards (adapted from Hermes's tool guardrails, SYNTHESIS WP-16). A
// model can get stuck: clicking the same button on a page that never
// changes, going back and forth between two pages, retrying a tool that
// keeps failing, searching forever. MAX_TURNS alone lets that burn 200
// turns behind one spinning step. The guards notice and say so in the
// tool's result ("[Alexa system note] ..."), refuse a call that has clearly
// gone nowhere ("Not run: ..."), and, when nobody is watching (a scheduled
// job), end the run with a loop_blocked failure instead of letting it spin.
// While the user is there, a guard never ends the run: Alexa is told to ask
// them instead.
const (
	sameCallWarn  = 3  // identical (tool, args, page): warn on the 3rd
	sameCallBlock = 5  // and refuse the 5th
	cycleWarn     = 3  // A-B-A-B on unchanged pages: warn after 3 laps
	cycleBlock    = 5  // and refuse the call that would make the 5th
	failWarn      = 3  // the same tool failing, whatever its args: warn on the 3rd
	failHalt      = 8  // and end an unattended run on the 8th
	searchCap     = 40 // web searches per stretch of work before Alexa is told to stop
	searchHalt    = 50 // an unattended run that keeps searching anyway ends
	ringSize      = 16 // recent calls kept for cycle detection

	// A scheduled job gets a wall-clock budget too: told to wrap up after
	// jobWrapUp, ended after jobLimit.
	jobWrapUp = 20 * time.Minute
	jobLimit  = 30 * time.Minute
)

// loopGuard is one run's record of its recent tool calls. Runtime and run
// belong to the loop (runtime.go), so it lives beside them, keyed by the
// run, like the run's progress (events.go).
type loopGuard struct {
	uid int64
	run *run

	mu         sync.Mutex
	start      time.Time
	background bool              // a scheduled job nobody has written to since it started
	page       string            // digest of the browser page the latest state showed
	seen       map[string]int    // call signature -> times made since the last progress
	fails      map[string]int    // tool -> failures in a row
	ring       []string          // the latest call signatures, oldest first
	pending    map[string]string // call in flight: tool -> its signature
	refused    *toolResult       // the latest call guardBefore refused
	calls      int               // tool calls made by this run
	turn       int               // the latest turn number, to see a new stretch begin
	searches   int               // web searches this stretch
	noted      map[string]bool   // notes given this stretch (budget, searches)
	jobNoted   bool              // the wrap-up note of a scheduled job
	empty      int               // empty incomplete responses in a row
	truncated  int               // responses cut off mid-thought in a row
	halt       error             // why an unattended run must end
}

// The executor's guard hook (exec.go) runs every call through the guards.
func init() {
	guard = func(rt *Runtime, r *run, name string, args map[string]any, run func() *toolResult) *toolResult {
		if refused := rt.guardBefore(r, name, args); refused != nil {
			return refused
		}
		return rt.guardAfter(r, name, args, run())
	}
}

var runGuards = struct {
	sync.Mutex
	m map[*run]*loopGuard
}{m: map[*run]*loopGuard{}}

// guardOf returns the run's guard, creating it on the run's first call.
// Only the run's own goroutine calls it (r.user changes under the loop).
// Guards of runs that have ended are dropped on the way.
func (rt *Runtime) guardOf(r *run) *loopGuard {
	runGuards.Lock()
	g := runGuards.m[r]
	runGuards.Unlock()
	if g != nil {
		return g
	}
	// Only a scheduled job's run counts: the user's own request can be
	// held too, with nobody owed an answer yet (they turned Following off
	// after Alexa said "On it"), and it's still theirs.
	background := r.isJob() && r.isHeld() && !r.isAwaiting()
	runGuards.Lock()
	defer runGuards.Unlock()
	for old, g := range runGuards.m {
		if rt.running(g.uid) != old {
			delete(runGuards.m, old)
		}
	}
	g = &loopGuard{uid: r.user.ID, run: r, start: time.Now(), turn: -1, background: background,
		seen: map[string]int{}, fails: map[string]int{}, pending: map[string]string{}, noted: map[string]bool{}}
	runGuards.m[r] = g
	return g
}

// isJob says whether the run is a scheduled job's that the user hasn't
// written to.
func (r *run) isJob() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job
}

// unattended says whether nobody is following the run: a scheduled job
// the user hasn't written to. Once they have, it stays attended.
func (g *loopGuard) unattended() bool {
	if g.background && (g.run.isAwaiting() || !g.run.isHeld()) {
		g.background = false
	}
	return g.background
}

// Tools whose repeats are the conversation itself, not a loop.
var unguarded = map[string]bool{"say": true, "ask": true, "present_report": true, "plan": true, "request_takeover": true, "request_login": true}

// Tools that change something. One that worked is progress: what was tried
// before it may work now.
var progressTools = map[string]bool{
	"browser_open": true, "browser_click": true, "browser_click_at": true, "browser_type": true, "browser_key": true,
	"browser_select": true, "browser_drag": true, "browser_upload": true, "browser_history": true,
	"browser_answer_dialog": true, "browser_login": true, "write_file": true, "run_shell": true,
	"memory_save": true, "memory_delete": true, "schedule_create": true, "schedule_delete": true, "generate_image": true,
}

// Tools whose failures with different arguments are normal (a command, an
// address that doesn't load): they warn but never end a run.
var failureTolerant = map[string]bool{"run_shell": true, "browser_open": true, "fetch_url": true}

// signature names a call: the tool, its arguments without the status
// labels, and for the browser the page it acts on.
func (g *loopGuard) signature(name string, args map[string]any) string {
	clean := map[string]any{}
	for k, v := range args {
		if k != "status" && k != "done" {
			clean[k] = v
		}
	}
	sig := name + " " + string(marshal(clean))
	if strings.HasPrefix(name, "browser_") {
		sig += " @" + g.page
	}
	return sig
}

// pageDigest is a browser state without the line about the action that led
// to it, so the same page reads the same however it was reached.
func pageDigest(text string) string {
	if i := strings.Index(text, "URL: "); i >= 0 {
		text = text[i:]
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// laps counts how many times the latest calls went back and forth between
// the same two, ending with the call sig.
func laps(ring []string, sig string) int {
	r := append(append([]string{}, ring...), sig)
	n := len(r)
	if n < 4 || r[n-1] == r[n-2] {
		return 0
	}
	m := 2
	for i := n - 3; i >= 0 && r[i] == r[i+2]; i-- {
		m++
	}
	return m / 2
}

// guardBefore runs before a tool call. It returns the result to give the
// model instead of running the call, or nil to run it.
func (rt *Runtime) guardBefore(r *run, name string, args map[string]any) *toolResult {
	if unguarded[name] {
		return nil
	}
	g := rt.guardOf(r)
	g.mu.Lock()
	defer g.mu.Unlock()
	sig := g.signature(name, args)
	g.pending[name] = sig
	var why string
	switch {
	case g.seen[sig]+1 >= sameCallBlock:
		why = fmt.Sprintf("Not run: you've made this exact call (%s, same arguments%s) %d times without it changing anything. Do something different: look at the page again, try another way, or ask the user.", name, onPage(name), g.seen[sig]+1)
	case laps(g.ring, sig) >= cycleBlock:
		why = "Not run: you've gone back and forth between the same two actions " + fmt.Sprint(cycleBlock) + " times and the page keeps coming back the same. Stop and try something else, or ask the user."
	case g.unattended() && !failureTolerant[name] && g.fails[name]+1 >= failHalt:
		why = fmt.Sprintf("Not run: %s has failed %d times in a row. Stop using it for this job.", name, g.fails[name])
	}
	if why == "" {
		return nil
	}
	delete(g.pending, name)
	g.record(sig)
	if g.unattended() && g.halt == nil {
		g.halt = loopBlocked("Alexa kept repeating the same step without getting anywhere, so it stopped this scheduled job.")
	}
	g.refused = &toolResult{Text: why}
	return g.refused
}

func onPage(name string) string {
	if strings.HasPrefix(name, "browser_") {
		return ", same page"
	}
	return ""
}

// record counts a call that was made (or refused) under signature sig.
// g.mu must be held.
func (g *loopGuard) record(sig string) {
	g.seen[sig]++
	g.ring = append(g.ring, sig)
	if len(g.ring) > ringSize {
		g.ring = g.ring[len(g.ring)-ringSize:]
	}
}

// guardAfter runs after a tool call with its result for the model (a Go
// error already turned into "Error: ..."), and adds a note when the run
// looks stuck.
func (rt *Runtime) guardAfter(r *run, name string, args map[string]any, res *toolResult) *toolResult {
	if unguarded[name] || res == nil {
		return res
	}
	g := rt.guardOf(r)
	g.mu.Lock()
	defer g.mu.Unlock()
	status, _ := callOutcome(context.Background(), name, res, nil)
	if res == g.refused || status == "interrupted" {
		delete(g.pending, name)
		return res
	}
	sig, ok := g.pending[name]
	if !ok {
		sig = g.signature(name, args)
	}
	delete(g.pending, name)
	g.calls++
	lapCount := laps(g.ring, sig)
	g.record(sig)
	if res.Kind == "browser_state" {
		g.page = pageDigest(res.Text)
	}
	if status == "failed" {
		g.fails[name]++
	} else {
		g.fails[name] = 0
	}
	if progressTools[name] && status == "done" {
		// Something changed: what was tried before may work now.
		for k := range g.seen {
			if k != sig {
				delete(g.seen, k)
			}
		}
	}
	var note string
	switch n := g.seen[sig]; {
	case n >= sameCallWarn:
		note = fmt.Sprintf("That's the %s time you've made this exact call (%s, same arguments%s) and nothing has changed. It isn't working: try a different approach, or ask the user.", ordinal(n), name, onPage(name))
	case lapCount >= cycleWarn:
		note = fmt.Sprintf("You've gone back and forth between the same two actions %d times and the page keeps coming back the same. Try something else, or ask the user.", lapCount)
	case g.fails[name] >= failWarn && g.fails[name]%failWarn == 0:
		note = fmt.Sprintf("%s has failed %d times in a row. Step back and try another way, or tell the user what's blocking you.", name, g.fails[name])
	}
	if note == "" {
		return res
	}
	out := *res
	out.Text = res.Text + "\n\n[Alexa system note] " + note
	return &out
}

func ordinal(n int) string {
	switch n {
	case 3:
		return "3rd"
	}
	return fmt.Sprintf("%dth", n)
}

// streamStarted is told about each model call: turn is the loop's turn
// number in the current stretch of work (it starts again at 0 for a new
// stretch). It returns the notes to add to the conversation before the
// call, and the error that ends the run instead, if it must end.
func (g *loopGuard) streamStarted(turn, maxTurns int) (notes []string, stop error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.halt != nil {
		return nil, g.halt
	}
	if turn <= g.turn {
		// A new stretch of work: its budget starts again.
		g.searches = 0
		g.noted = map[string]bool{}
	}
	g.turn = turn
	if g.unattended() {
		switch elapsed := time.Since(g.start); {
		case elapsed > jobLimit:
			return nil, loopBlocked("This scheduled job ran for too long, so Alexa stopped it.")
		case elapsed > jobWrapUp && !g.jobNoted:
			g.jobNoted = true
			notes = append(notes, fmt.Sprintf("[Alexa system note] This scheduled job has been running for %d minutes. Wrap up now: finish what matters most, then tell the user what you found.", int(jobWrapUp.Minutes())))
		}
	}
	if note := budgetNote(turn, maxTurns, g.noted); note != "" {
		notes = append(notes, note)
	}
	if g.searches >= searchCap && !g.noted["search"] {
		g.noted["search"] = true
		notes = append(notes, fmt.Sprintf("[Alexa system note] You've searched the web %d times in this stretch of work. Stop searching: work with what you've found, or tell the user what's missing.", g.searches))
	}
	if g.searches >= searchHalt && g.unattended() {
		return notes, loopBlocked("Alexa kept searching without finishing, so it stopped this scheduled job.")
	}
	return notes, nil
}

// budgetNote is the note for a turn that has used 60% or 85% of the
// stretch's turns, given once each.
func budgetNote(turn, maxTurns int, noted map[string]bool) string {
	at60, at85 := maxTurns*60/100, maxTurns*85/100
	if at60 < 1 || at85 <= at60 || at85 >= maxTurns {
		return "" // too few turns for notes to help
	}
	switch {
	case turn >= at85 && !noted["85"]:
		noted["60"], noted["85"] = true, true
		return fmt.Sprintf("[Alexa system note] Only %d steps are left for this stretch of work. Start wrapping up: finish the most important part, then tell the user what's done and what's left.", maxTurns-turn)
	case turn >= at60 && !noted["60"]:
		noted["60"] = true
		return fmt.Sprintf("[Alexa system note] You've used %d of the %d steps for this stretch of work. If you're going in circles, change approach, or tell the user where things stand and ask how to go on.", turn, maxTurns)
	}
	return ""
}

// searched counts a web search the model ran.
func (g *loopGuard) searched() {
	g.mu.Lock()
	g.searches++
	g.mu.Unlock()
}

// worked says whether the run has made any tool call yet.
func (g *loopGuard) worked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls > 0 || g.searches > 0
}

// Empty and cut-off responses (Hermes's empty-response ladder). An
// incomplete response with no output at all (a filter, a broken model)
// happens again if asked again: the second in a row ends the run with
// empty_response. One that ran out of output tokens while thinking is
// continued, but only a few times in a row.
const (
	emptyLimit     = 2
	truncatedLimit = 4
)

// checkResponse looks at a model response for the empty-response ladder.
func (g *loopGuard) checkResponse(resp *llm.Response) error {
	calls, said := 0, false
	for _, raw := range resp.Output {
		switch it := llm.ParseItem(raw); it.Type {
		case "function_call":
			calls++
		case "message":
			said = said || strings.TrimSpace(it.Text()) != ""
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if resp.Status != "incomplete" || calls > 0 || said {
		g.empty, g.truncated = 0, 0
		return nil
	}
	reason := ""
	if resp.IncompleteDetails != nil {
		reason = resp.IncompleteDetails.Reason
	}
	if resp.Usage.OutputTokens == 0 || reason == "content_filter" {
		g.empty++
		if g.empty >= emptyLimit {
			return errEmptyResponse
		}
		return nil
	}
	g.truncated++
	if g.truncated > truncatedLimit {
		return errTruncated
	}
	return nil
}
