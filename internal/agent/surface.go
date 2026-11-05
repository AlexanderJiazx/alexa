package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"alexagent/internal/hub"
	"alexagent/internal/store"
)

// What the screen shows next to the exchange follows these rules
// (SYNTHESIS Appendix C.1):
//
//	R1 The surface is nothing, the browser, a report or one entry's blocks.
//	   The user's own choice holds it until the next new exchange.
//	R2 A new exchange closes the panel, unless the user holds it.
//	R3 Follow mode: Alexa's browsing opens the browser, unless the screen is
//	   held or the run is in the background.
//	R4 A new report opens, unless the screen is held, the run is in the
//	   background, or it revises a report the user isn't looking at; its
//	   card shows it instead.
//	R5 Results take the stage: a final say or an ask with blocks, or a
//	   final report, closes the browser unless the screen is held.
//	R6 Background (scheduled) runs never change the surface.
//	R7 Expanding a block shows that entry and holds it.
//	R8 Take-over and sign-in show the browser whatever is held: the user
//	   must act there (TakeOver and suspend do this).

// View returns the session as clients see it. It takes r.mu (through
// isHeld), so the state must never be published while holding it.
func (rt *Runtime) View(ctx context.Context, userID int64) View {
	v := View{Status: "idle", Follow: true}
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
	r := rt.running(userID)
	if p := rt.syncRuns(ctx, userID, r, v.Status); p != nil && v.Status == "working" {
		v.Phase, v.RunStartedAt = p.view()
	}
	if v.Status == "needs" {
		v.Phase = "waiting"
	}
	v.Plan = rt.viewPlan(ctx, userID, v.Status)
	v.Follow = !v.Surface.Held && (r == nil || !r.isHeld())
	if n, err := rt.st.InboxUserCount(ctx, userID); err == nil {
		v.Queued = n
	}
	return v
}

// SetSurface changes what the user's screen shows: the browser, a report,
// one entry's blocks, or just the conversation. It's the user's choice, so
// it holds the screen (R1, R7), unless they asked it to follow Alexa again.
func (rt *Runtime) SetSurface(ctx context.Context, u *store.User, s Surface) error {
	switch s.Kind {
	case "", "browser":
		s.ID = ""
	case "report":
		if _, err := rt.st.GetReport(ctx, u.ID, s.ID); err != nil {
			return ErrNotFound
		}
	case "entry":
		if !rt.hasEntry(ctx, u.ID, s.ID) {
			return ErrNotFound
		}
	default:
		return errors.New("unknown surface")
	}
	follow := s.Follow
	s.Held, s.Follow = !follow, false
	if err := rt.writeSurface(ctx, u.ID, s); err != nil {
		return err
	}
	if r := rt.running(u.ID); r != nil {
		r.mu.Lock()
		r.held = !follow
		r.mu.Unlock()
	}
	rt.publishState(u.ID)
	return nil
}

// hasEntry says whether the user's conversation has entry id.
func (rt *Runtime) hasEntry(ctx context.Context, userID int64, id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return false
	}
	e, err := rt.st.Feed(ctx, userID, n+1, 1)
	return err == nil && len(e) == 1 && e[0].ID == n
}

// publishState sends the session to the user's screens. The view is read and
// published as one step per user, so of two concurrent calls the later read
// lands last and a stale view never overwrites a fresh one. View publishes
// run events itself, but never through publishState, so this can't deadlock.
func (rt *Runtime) publishState(userID int64) {
	rt.hub.PublishOrdered(userID, "state", func() any { return rt.View(context.Background(), userID) })
}

// surface is what the user's screen shows now.
func (rt *Runtime) surface(ctx context.Context, userID int64) Surface {
	var cur Surface
	if ss, err := rt.st.GetSession(ctx, userID); err == nil {
		_ = json.Unmarshal(ss.Surface, &cur)
	}
	return cur
}

func (rt *Runtime) setSurface(ctx context.Context, userID int64, s Surface) {
	_ = rt.writeSurface(ctx, userID, s)
	rt.publishState(userID)
}

// writeSurface stores what the user's screen shows, and counts the move.
// Every change to the surface goes through it.
func (rt *Runtime) writeSurface(ctx context.Context, userID int64, s Surface) error {
	err := rt.st.SetSessionSurface(ctx, userID, marshal(s))
	rt.movedSurface(userID)
	return err
}

// movedSurface counts a change to the user's screen, or Alexa using what's
// on it already, so settleTopic can tell whether the screen has been put
// to use since a message came in (comparing what it shows can't: the
// browser left open and the browser Alexa is now using look the same).
func (rt *Runtime) movedSurface(userID int64) {
	rt.mu.Lock()
	if rt.moves == nil {
		rt.moves = map[int64]uint64{}
	}
	rt.moves[userID]++
	rt.mu.Unlock()
}

func (rt *Runtime) surfaceMoves(userID int64) uint64 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.moves[userID]
}

// endHold ends the user's hold on the screen, leaving it as it is, and says
// whether there was one.
func (rt *Runtime) endHold(ctx context.Context, userID int64) bool {
	cur := rt.surface(ctx, userID)
	if !cur.Held {
		return false
	}
	rt.setSurface(ctx, userID, Surface{Kind: cur.Kind, ID: cur.ID})
	return true
}

// showSurface puts something on the user's screen (if it isn't already),
// unless they hold what's there: follow mode (R3).
func (rt *Runtime) showSurface(userID int64, s Surface) {
	ctx := context.Background()
	cur := rt.surface(ctx, userID)
	if cur.Held {
		return
	}
	if cur == s {
		rt.movedSurface(userID)
		return
	}
	rt.setSurface(ctx, userID, s)
}

// showReport puts a report in the user's feed and, when it should take the
// screen, on it (R4). Otherwise its card says it's new or updated.
func (rt *Runtime) showReport(ctx context.Context, u *store.User, rep *store.Report, updated bool) {
	rt.hub.Publish(u.ID, hub.Event{Type: "report", Data: rep})
	_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "report", Text: rep.Title, Data: reportEntryData(rep, updated)})
	cur := rt.surface(ctx, u.ID)
	viewing := cur.Kind == "report" && cur.ID == rep.ID
	if viewing {
		rt.movedSurface(u.ID)
	}
	r := rt.running(u.ID)
	if !viewing && !cur.Held && (r == nil || !r.isHeld()) && !updated {
		_ = rt.writeSurface(ctx, u.ID, Surface{Kind: "report", ID: rep.ID})
	}
	rt.publishState(u.ID)
	_ = rt.notify(ctx, u, "report:"+rep.ID, rep.Title, reportNotifyBody(rep), false, false)
}

// onNewExchange is called when a message sets Alexa to work while it's
// idle: the start of a new exchange. The last exchange's plan is done with,
// and the screen starts afresh.
func (rt *Runtime) onNewExchange(ctx context.Context, userID int64, background bool) {
	rt.clearPlan(ctx, userID)
	rt.surfaceOnNewExchange(ctx, userID, background)
}

// surfaceOnNewExchange closes the panel the last exchange left open: it's
// about something else (R2). A panel the user chose stays, and their hold
// ends here. A scheduled job leaves the screen as it is (R6).
func (rt *Runtime) surfaceOnNewExchange(ctx context.Context, userID int64, background bool) {
	if background {
		return
	}
	cur := rt.surface(ctx, userID)
	next := Surface{}
	if cur.Held {
		next = Surface{Kind: cur.Kind, ID: cur.ID}
	}
	if next != cur {
		rt.setSurface(ctx, userID, next)
	}
}

// resultsTakeStage closes the browser when Alexa's answer brings its own
// results: a final say or an ask with blocks, or a final report. The page it
// worked on is no longer the point (R5). A held screen and background runs
// are left alone.
func (rt *Runtime) resultsTakeStage(ctx context.Context, r *run, tool string, args map[string]any) {
	blocks, _ := args["blocks"].([]any)
	final := boolean(args, "final", false)
	if tool == "say" {
		// The model often leaves final off its last say; finish checks this.
		r.mu.Lock()
		r.results = len(blocks) > 0
		r.mu.Unlock()
	}
	switch {
	case tool == "say" && final && len(blocks) > 0:
	case tool == "ask" && len(blocks) > 0:
	case tool == "present_report" && final:
	default:
		return
	}
	if rt.closeBrowserForResults(ctx, r) {
		rt.publishState(r.user.ID)
	}
}

// closeBrowserForResults closes the browser panel unless the screen is held,
// and says whether it did. The caller publishes the state.
func (rt *Runtime) closeBrowserForResults(ctx context.Context, r *run) bool {
	if r.isHeld() {
		return false
	}
	if cur := rt.surface(ctx, r.user.ID); cur.Kind == "browser" && !cur.Held {
		_ = rt.writeSurface(ctx, r.user.ID, Surface{})
		return true
	}
	return false
}
