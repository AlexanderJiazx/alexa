package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"alexagent/internal/hub"
	"alexagent/internal/llm"
)

// A run's lifecycle reaches the user's screens as hub events of type "run"
// (SYNTHESIS Appendix B.3): it started, Alexa is thinking (with a headline
// from its reasoning summary), writing a message, using a tool, retrying,
// compacting, waiting, stopping, and how it ended. The RunCard draws its
// timer, working line and summary from them.

// runEvent is the data of a "run" event. Its type and seq travel on the
// hub's envelope.
type runEvent struct {
	RunID    string    `json:"runId"`
	At       int64     `json:"at"` // unix ms, never earlier than the run's previous event
	Phase    string    `json:"phase"`
	Headline string    `json:"headline,omitempty"` // thinking: ≤60
	Target   string    `json:"target,omitempty"`   // writing: say | ask | report
	Tool     *runTool  `json:"tool,omitempty"`
	Retry    *runRetry `json:"retry,omitempty"`
	Outcome  string    `json:"outcome,omitempty"` // ended: done | stopped | failed | suspended
	Failure  any       `json:"failure,omitempty"`
}

type runTool struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Host  string `json:"host,omitempty"`
}

type runRetry struct {
	Attempt int    `json:"attempt"`
	Of      int    `json:"of"`
	Reason  string `json:"reason"`
	At      int64  `json:"at"` // unix ms of the next attempt
}

// progress is what the screen shows of one run: its id and start, its phase,
// the current step's details and the sources it read. Runtime and run belong
// to the loop, so this lives beside them, keyed by the run.
type progress struct {
	rt   *Runtime
	uid  int64
	run  *run
	dead bool // the run is over: announce nothing

	mu      sync.Mutex
	id      string
	started int64
	at      int64
	phase   string
	last    runEvent // the latest event, to skip repeats
	outcome string   // how the run ended, when the loop said so
	failure any

	// The current step (r.step) and what's known about it.
	step     *StepData
	stepID   int64
	stepLast string // how its latest call went: done | failed | interrupted | skipped
	call     *inflight
	idle     bool // the activity was reset to "Thinking" after the last tool

	page    pageState // the browser page as the latest state showed it
	sources []Source  // pages the run read, newest last, for its say and reports
	search  *searchRef

	thought     strings.Builder // the reasoning summary part streaming in
	thoughtID   string
	thoughtDone bool
	searchStart time.Time
}

// inflight is the tool call that's running.
type inflight struct {
	name   string
	args   map[string]any
	start  time.Time
	stepID int64
}

var runProgress = struct {
	sync.Mutex
	m map[*run]*progress
}{m: map[*run]*progress{}}

// runSync lets one view at a time reconcile the runs it shows (syncRuns).
var runSync sync.Mutex

// progress returns the run's progress, creating it (and announcing the start
// of the run) the first time. Only the run's own goroutine may call it: the
// loop replaces r.user each turn without a lock. Others use progressOf.
func (rt *Runtime) progress(r *run) *progress {
	return rt.progressOf(r.user.ID, r)
}

// progressOf is progress for the user's run r, without reading r.user. A
// run that is over gets a detached progress that announces nothing, so a
// late call can't start it again.
func (rt *Runtime) progressOf(userID int64, r *run) *progress {
	live := rt.running(userID) == r
	runProgress.Lock()
	p := runProgress.m[r]
	if p != nil || !live {
		runProgress.Unlock()
		if p == nil {
			p = &progress{rt: rt, uid: userID, run: r, dead: true}
		}
		return p
	}
	now := time.Now().UnixMilli()
	p = &progress{rt: rt, uid: userID, run: r, id: uuid.NewString(), started: now}
	runProgress.m[r] = p
	runProgress.Unlock()
	p.emit(runEvent{Phase: "started", At: now})
	p.emit(runEvent{Phase: "thinking"})
	return p
}

// peekProgress returns the run's progress if it has any.
func peekProgress(r *run) *progress {
	runProgress.Lock()
	defer runProgress.Unlock()
	return runProgress.m[r]
}

// syncRuns brings the lifecycle in line with the session View is about to
// show. Every start of a run (launch) and every end (finish, suspend, fail,
// stopped and the loop's own exit) publishes the state, so this is where
// runs are seen to start and to end; it returns the progress of the run
// that's going on, if any.
//
// Views run one at a time through here, so one that comes second never
// misses an end the first is still announcing. A run that has left but
// hasn't set the session's new status yet is ended by the view read after
// that change: only at shutdown does a session stay "working" without a run,
// and reading the outcome any earlier would call a finished run stopped.
func (rt *Runtime) syncRuns(ctx context.Context, userID int64, cur *run, status string) *progress {
	runSync.Lock()
	defer runSync.Unlock()
	if cur == nil && status == "working" && rt.baseCtx.Err() == nil {
		return nil
	}
	var ended []*progress
	runProgress.Lock()
	for r, p := range runProgress.m {
		if p.rt == rt && p.uid == userID && r != cur {
			ended = append(ended, p)
			delete(runProgress.m, r)
		}
	}
	runProgress.Unlock()
	for _, p := range ended {
		p.end(ctx, status)
	}
	if cur == nil {
		return nil
	}
	return rt.progressOf(userID, cur)
}

// end announces how the run ended: what the loop said, or else what the
// session shows. A run that left the session waiting on the user was
// suspended; one whose last word is an error failed, and one that ends on
// "Stopped" was stopped.
func (p *progress) end(ctx context.Context, status string) {
	p.mu.Lock()
	outcome, failure := p.outcome, p.failure
	p.mu.Unlock()
	if outcome == "" {
		outcome = "done"
		switch {
		case status == "needs":
			outcome = "suspended"
		case status == "working":
			outcome = "stopped" // shutting down; it resumes on the next start
		default:
			if feed, err := p.rt.st.Feed(ctx, p.uid, 0, 1); err == nil && len(feed) == 1 {
				switch e := feed[0]; {
				case e.Kind == "error":
					outcome = "failed"
				case e.Kind == "note" && e.Text == "Stopped":
					outcome = "stopped"
				}
			}
		}
	}
	p.emit(runEvent{Phase: "ended", Outcome: outcome, Failure: failure})
}

// emit publishes a run event, unless it only repeats the latest one.
func (p *progress) emit(ev runEvent) {
	p.mu.Lock()
	if p.dead || ev.Phase == p.last.Phase && ev.Headline == p.last.Headline && ev.Target == p.last.Target && sameTool(ev.Tool, p.last.Tool) && ev.Retry == nil && ev.Phase != "ended" {
		p.mu.Unlock()
		return
	}
	ev.RunID = p.id
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	if ev.At <= p.at {
		ev.At = p.at + 1
	}
	p.at = ev.At
	p.phase = ev.Phase
	p.last = ev
	p.mu.Unlock()
	p.rt.hub.Publish(p.uid, hub.Event{Type: "run", Data: ev})
}

func sameTool(a, b *runTool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// view is what the session view shows of the run.
func (p *progress) view() (phase string, startedAt int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phase, p.started
}

// runPhase moves the run to a phase that needs no details: thinking (a model
// turn starts), compacting or waiting. It's for the run's own goroutine.
func (rt *Runtime) runPhase(r *run, phase string) {
	rt.progress(r).emit(runEvent{Phase: phase})
}

// runPhaseFor does the same for whatever run the user has, if any, from
// any goroutine: Stop's stopping, or compaction, which only knows the user.
func (rt *Runtime) runPhaseFor(userID int64, phase string) {
	if r := rt.running(userID); r != nil {
		rt.progressOf(userID, r).emit(runEvent{Phase: phase})
	}
}

// runCompacting says the user's run is folding the older conversation into
// its summary, and returns the call that says it's thinking again, for a
// defer: compact() knows only the user, and a compaction the user asked for
// while idle has no run to announce. A run stopped meanwhile isn't thinking
// again: it's stopping.
func (rt *Runtime) runCompacting(userID int64) (done func()) {
	rt.runPhaseFor(userID, "compacting")
	return func() {
		if r := rt.running(userID); r != nil && !r.cancelled() {
			rt.progressOf(userID, r).emit(runEvent{Phase: "thinking"})
		}
	}
}

// runRetrying says the model call failed and will be tried again at the
// given time (unix ms), so the screen can count down.
func (rt *Runtime) runRetrying(r *run, attempt, of int, reason string, at int64) {
	rt.progress(r).emit(runEvent{Phase: "retrying", Retry: &runRetry{Attempt: attempt, Of: of, Reason: reason, At: at}})
}

// runEnded records how the run ended (done, stopped, failed or suspended,
// with the failure for a failed run), for when the session is next shown.
// Without it, the outcome is read from the session.
func (rt *Runtime) runEnded(r *run, outcome string, failure any) {
	p := rt.progress(r)
	p.mu.Lock()
	p.outcome, p.failure = outcome, failure
	p.mu.Unlock()
}

// writingTarget says which message a tool writes, for the writing phase.
func writingTarget(tool string) string {
	switch tool {
	case "say", "ask":
		return tool
	case "present_report":
		return "report"
	}
	return ""
}

// onStreamEvent follows the model's output as it streams: the headline of
// its reasoning summary (the thinking phase), the message it starts writing
// (the writing phase, while a long report is still coming), and the web
// searches it runs. Nothing here is stored: the summary is only a label.
func (rt *Runtime) onStreamEvent(r *run, ev llm.Event) {
	switch ev.Type {
	case "response.output_item.added":
		it := llm.ParseItem(ev.Item)
		switch it.Type {
		case "function_call":
			if target := writingTarget(it.Name); target != "" {
				rt.progress(r).emit(runEvent{Phase: "writing", Target: target})
				if target == "report" {
					rt.setActivity(r, "Writing the report")
				}
			}
		case "reasoning":
			rt.progress(r).newThought(it.ID)
		case "web_search_call":
			p := rt.progress(r)
			p.mu.Lock()
			p.searchStart = time.Now()
			p.mu.Unlock()
			rt.setActivity(r, "Searching the web")
		}
	case "response.output_item.done":
		switch it := llm.ParseItem(ev.Item); it.Type {
		case "web_search_call":
			rt.searchItem(r, it, searchSources(ev.Item))
		case "reasoning":
			if len(it.Summary) > 0 {
				rt.progress(r).think(it.ID, "", it.Summary[0].Text)
			}
		}
	case "response.reasoning_summary_part.added":
		rt.progress(r).newThought(ev.ItemID)
	case "response.reasoning_summary_text.delta":
		rt.progress(r).think(ev.ItemID, ev.Delta, "")
	}
}

// newThought starts a new part of the reasoning summary.
func (p *progress) newThought(id string) {
	p.mu.Lock()
	p.thought.Reset()
	p.thoughtID, p.thoughtDone = id, false
	p.mu.Unlock()
}

// think adds to the reasoning summary part streaming in (or takes a whole
// one) and announces its headline once there is one.
func (p *progress) think(id, delta, whole string) {
	p.mu.Lock()
	if id != p.thoughtID {
		p.thought.Reset()
		p.thoughtID, p.thoughtDone = id, false
	}
	if p.thoughtDone {
		p.mu.Unlock()
		return
	}
	if whole != "" {
		p.thought.Reset()
		p.thought.WriteString(whole)
	} else {
		p.thought.WriteString(delta)
	}
	h, ok := thoughtHeadline(p.thought.String(), whole != "")
	if ok {
		p.thoughtDone = true
	}
	p.mu.Unlock()
	if ok && h != "" {
		p.emit(runEvent{Phase: "thinking", Headline: h})
	}
}

// thoughtHeadline picks a short title from a reasoning summary: its bold lead
// ("**Comparing fares**") or its first sentence, at most 60 characters. ok
// is false while not enough has streamed in to tell; complete says the
// summary is all there.
func thoughtHeadline(s string, complete bool) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", complete
	}
	if rest, bold := strings.CutPrefix(s, "**"); bold {
		if i := strings.Index(rest, "**"); i >= 0 {
			return trim(strings.Join(strings.Fields(rest[:i]), " "), 60), true
		}
		if !complete && len(rest) < 120 {
			return "", false
		}
		s = strings.ReplaceAll(rest, "**", "")
	}
	end := -1
	if i := strings.Index(s, "\n"); i >= 0 {
		end = i
	}
	for _, sep := range []string{". ", "! ", "? "} {
		if i := strings.Index(s, sep); i >= 0 && (end < 0 || i+1 < end) {
			end = i + 1
		}
	}
	switch {
	case end >= 0:
		s = s[:end]
	case !complete && len([]rune(s)) <= 60:
		return "", false
	}
	return clip(strings.TrimSuffix(strings.TrimSpace(s), "."), 60), true
}

// runSources lists the pages the run has read so far (at most 10, most
// recent first), for a say's or a report's sources.
func (rt *Runtime) runSources(r *run) []Source {
	p := peekProgress(r)
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Source
	for i := len(p.sources) - 1; i >= 0 && len(out) < 10; i-- {
		out = append(out, p.sources[i])
	}
	return out
}

// runLinks is runSources as a report's links, for a report the model gave
// no sources of its own: research reports otherwise ship without any.
func (rt *Runtime) runLinks(r *run) []Link {
	var out []Link
	for _, s := range rt.runSources(r) {
		out = append(out, Link{Title: s.Title, URL: s.URL})
	}
	return out
}

// addSource remembers a page the run read.
func (p *progress) addSource(s Source) {
	for i, x := range p.sources {
		if x.URL == s.URL {
			if x.Title == "" {
				p.sources[i].Title = s.Title
			}
			return
		}
	}
	p.sources = append(p.sources, s)
	if len(p.sources) > 50 {
		p.sources = p.sources[len(p.sources)-50:]
	}
}
