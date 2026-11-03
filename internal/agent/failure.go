package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"strings"

	"alexagent/internal/hub"
	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// Failure is why a run ended badly (SYNTHESIS Appendix B.4). It's stored on
// the error entry (entry.data.failure) and carried by the run's "ended"
// event, and the screen draws its card from it: what went wrong in plain
// words, Try again when trying again can work, a countdown to RetryAt. The
// raw error never reaches the user or the model: it's in the log.
type Failure struct {
	Reason    string `json:"reason"` // model_unavailable | rate_limited | stalled | context_overflow | empty_response | loop_blocked | invalid_input | tool_crashed | internal
	Retryable bool   `json:"retryable"`
	RetryAt   int64  `json:"retryAt,omitempty"` // unix ms: when the service said to come back
	Partial   bool   `json:"partial,omitempty"` // some work was done before it failed
	Detail    string `json:"detail,omitempty"`  // ≤200, fit to show the user

	note string // what the model is told, beyond Detail
}

// The agent's own reasons; the model service's come from llm.Classify.
const (
	reasonEmptyResponse = "empty_response"
	reasonLoopBlocked   = "loop_blocked"
	reasonToolCrashed   = "tool_crashed"
	reasonInternal      = "internal"
)

// runError is a run ending for a reason of the agent's own: the model kept
// answering with nothing, or a guard stopped a scheduled job going nowhere.
type runError struct {
	reason string
	detail string // for the user
}

func (e *runError) Error() string { return e.reason + ": " + e.detail }

var (
	errEmptyResponse = &runError{reasonEmptyResponse, "Alexa couldn’t come up with an answer for that. Try asking another way."}
	errTruncated     = &runError{reasonEmptyResponse, "Alexa’s answer kept getting cut off before it was done. Try asking for less at once."}
)

func loopBlocked(detail string) error { return &runError{reasonLoopBlocked, detail} }

// errOutOfSteps is a stretch of work that used all its turns without
// Alexa telling the user anything.
func errOutOfSteps(n int) error { return &runError{reasonLoopBlocked, outOfStepsText(n)} }

func outOfStepsText(n int) string {
	return fmt.Sprintf("Alexa stopped after %d steps without finishing. Say “carry on” to let it continue.", n)
}

// buildFailure says how err ended the run, in words fit for the user.
// partial says the run had done some work first.
func buildFailure(err error, partial bool) Failure {
	f := Failure{Partial: partial}
	var re *runError
	var me *llm.Error
	var apiErr *llm.APIError
	switch {
	case errors.As(err, &re):
		f.Reason, f.Retryable, f.Detail = re.reason, true, re.detail
	case errors.As(err, &me), errors.As(err, &apiErr):
		f.Reason, f.Retryable = llm.Classify(err)
		if me != nil && !me.RetryAt.IsZero() && f.Retryable {
			f.RetryAt = me.RetryAt.UnixMilli()
		}
		if f.Reason == llm.ReasonContextOverflow {
			// The same request can't be sent again, but a run that tries
			// again compacts again, and folds what couldn't be kept.
			f.Retryable = true
		}
		f.Detail = modelTrouble(f.Reason, f.Retryable)
		if f.Reason == llm.ReasonAuth {
			f.Reason = llm.ReasonUnavailable // the screen has no reason of its own for it
		}
		if errors.As(err, &apiErr) && (f.Reason == llm.ReasonInvalidInput || f.Reason == llm.ReasonContextOverflow) {
			// Alexa may be able to avoid it, so it hears what was refused.
			f.note = sanitize(apiErr.Message, 200)
		}
	default:
		f.Reason, f.Retryable = reasonInternal, true
		f.Detail = "Something went wrong inside Alexa. Nothing was lost."
	}
	f.Detail = trim(f.Detail, 200)
	return f
}

// modelTrouble is what the user is told about a model call that failed.
func modelTrouble(reason string, retryable bool) string {
	switch reason {
	case llm.ReasonRateLimited:
		if !retryable {
			return "Alexa has used up its model allowance for now. Ask whoever runs Alexa to check its plan."
		}
		return "Alexa’s model is busy right now. Nothing was lost."
	case llm.ReasonStalled:
		return "Alexa’s model stopped responding. Nothing was lost."
	case llm.ReasonContextOverflow:
		return "This was too much for Alexa’s model to read, even after tidying up. Try again to tidy up more."
	case llm.ReasonInvalidInput:
		return "Alexa’s model couldn’t read something in the conversation, such as an attached file. Fix conversation sets it aside."
	case llm.ReasonAuth:
		return "Alexa’s model turned down its key. Ask whoever runs Alexa to check it."
	}
	if !retryable {
		return "Alexa’s model isn’t available. Ask whoever runs Alexa to check its settings."
	}
	return "Alexa couldn’t reach its model. Nothing was lost."
}

// modelNote is what Alexa is told on its next turn about the failure.
func (f Failure) modelNote() string {
	s := f.Detail + " (" + f.Reason + ")"
	if f.note != "" {
		s += " The model service said: " + f.note
	}
	return s
}

// sanitize keeps an error message short and free of what the user (or the
// model) has no business seeing: server paths, stack traces.
func sanitize(s string, n int) string {
	if i := strings.Index(s, "goroutine "); i >= 0 {
		s = s[:i]
	}
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); strings.Contains(t, ".go:") || strings.HasPrefix(t, "panic:") {
			continue
		}
		keep = append(keep, l)
	}
	s = pathRe.ReplaceAllStringFunc(strings.Join(keep, " "), filepath.Base)
	return trim(strings.Join(strings.Fields(s), " "), n)
}

// failure is what fail records for err: the error entry (the user-safe
// text, with data.failure), the note that tells Alexa on its next turn,
// and the Failure itself for the run's "ended" event. The raw error is
// logged here and goes nowhere else.
func (rt *Runtime) failure(r *run, err error) (*store.Entry, string, Failure) {
	uid := r.user.ID
	f := buildFailure(err, rt.guardOf(r).worked() || rt.exchangeDidWork(context.Background(), uid))
	slog.Warn("run failure", "user", uid, "reason", f.Reason, "retryable", f.Retryable, "err", err)
	data := map[string]any{"failure": f}
	if AttachmentRejected(err) {
		// For Fix conversation: the model named a picture or a file, so one
		// older than the messages it was working on may be to blame.
		data[refusedAttachment] = true
	}
	e := &store.Entry{UserID: uid, Kind: "error", Text: f.Detail, Data: marshal(data)}
	note := stamp(r.loc(), "", "[Your previous turn ended with an error: "+f.modelNote()+"]")
	return e, note, f
}

// exchangeDidWork says whether the current exchange shows any work: a step,
// a message from Alexa or a report since the user's last message.
func (rt *Runtime) exchangeDidWork(ctx context.Context, userID int64) bool {
	feed, err := rt.st.Feed(ctx, userID, 0, 60)
	if err != nil {
		return false
	}
	for i := len(feed) - 1; i >= 0; i-- {
		switch feed[i].Kind {
		case "user":
			return false
		case "step", "say", "report", "ask":
			return true
		}
	}
	return false
}

// streamModel is the loop's model call: rt.llm.Stream with what keeps a run
// steady around it. Before the call it adds the guards' notes (budget,
// searches) to the conversation, or ends the run a guard stopped. During
// it, each retry shows on screen as the "retrying" phase, and a web search
// an attempt that broke off had already shown isn't shown again. A model
// that refuses the replayed reasoning as not its own is asked once more
// without it. After it, an empty response (Hermes's ladder) can end the
// run with empty_response.
// turn is the loop's turn number in the current stretch of work.
func (rt *Runtime) streamModel(ctx context.Context, r *run, turn int, req *llm.Request, fn func(llm.Event)) (*llm.Response, error) {
	uid := r.user.ID
	g := rt.guardOf(r)
	notes, stop := g.streamStarted(turn, rt.cfg.MaxTurns)
	if stop != nil {
		return nil, stop
	}
	env := rt.envs.For(uid)
	for _, n := range notes {
		if err := rt.appendMessage(ctx, uid, "system", n, nil); err != nil {
			return nil, err
		}
		// The same bytes buildInput will replay next turn, so the cached
		// prefix still matches.
		si := storedItem{Kind: "system", Item: marshal(map[string]any{"role": "user", "content": []map[string]any{{"type": "input_text", "text": n}}})}
		req.Input = append(req.Input, rt.userItem(ctx, env, si))
	}

	attempt, resumed := 1, true
	shown := map[string]bool{}   // web searches this call has shown
	earlier := map[string]bool{} // shown by attempts that broke off
	req.OnRetry = func(rr llm.Retry) {
		maps.Copy(earlier, shown)
		attempt, resumed = rr.Attempt, false
		rt.runRetrying(r, rr.Attempt, rr.Of, rr.Reason, rr.At.UnixMilli())
		rt.setActivity(r, fmt.Sprintf("Retrying (%d of %d)", rr.Attempt, rr.Of))
	}
	stream := func() (*llm.Response, error) {
		return rt.llm.Stream(ctx, req, func(ev llm.Event) {
			if !resumed {
				resumed = true
				rt.setActivity(r, "Thinking")
				rt.runPhase(r, "thinking")
			}
			if ev.Type == "response.output_item.done" {
				if it := llm.ParseItem(ev.Item); it.Type == "web_search_call" {
					key := searchKey(it)
					if attempt > 1 && key != "" && earlier[key] {
						return
					}
					if key != "" {
						shown[key] = true
					}
					g.searched()
				}
			}
			fn(ev)
		})
	}
	resp, err := stream()
	if err != nil && reasoningRefused(err) {
		if in := withoutReasoning(req.Input); len(in) < len(req.Input) {
			// Reasoning from another model (or another key) can't be
			// opened: this one would refuse it on every turn (L12).
			slog.Warn("the model refused the replayed reasoning; leaving it out", "user", uid, "err", err)
			if ferr := rt.forgetReasoning(ctx, uid); ferr != nil {
				slog.Warn("forgetting reasoning", "user", uid, "err", ferr)
			}
			req.Input = in
			resp, err = stream()
		}
	}
	if err != nil {
		return nil, err
	}
	if err := g.checkResponse(resp); err != nil {
		slog.Warn("model gave nothing", "user", uid, "status", resp.Status, "out", resp.Usage.OutputTokens, "err", err)
		return nil, err
	}
	return resp, nil
}

// reasoningRefused recognises the model refusing replayed encrypted
// reasoning it can't open.
func reasoningRefused(err error) bool {
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status < 400 || apiErr.Status >= 500 {
		return false
	}
	s := strings.ToLower(apiErr.Code + " " + apiErr.Message)
	return strings.Contains(s, "encrypted_content") || strings.Contains(s, "encrypted content")
}

// searchKey names a web search action: what it searched for or read.
func searchKey(it llm.Item) string {
	if it.Action == nil {
		return ""
	}
	q := it.Action.Query
	if q == "" && len(it.Action.Queries) > 0 {
		q = strings.Join(it.Action.Queries, "|")
	}
	if q == "" {
		q = it.Action.URL
	}
	if q == "" {
		return ""
	}
	return it.Action.Type + " " + q
}

// ErrNothingToRetry is returned by Retry when the conversation doesn't end
// in a failed run.
var ErrNothingToRetry = errors.New("There’s nothing to try again.")

// Retry is the error card's Try again: Alexa picks the failed run up from
// where the conversation stands, which is its last good item, since a
// failed model call stores nothing. The tap shows as the user's message.
func (rt *Runtime) Retry(ctx context.Context, u *store.User) error {
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	return rt.relaunch(ctx, u)
}

// Repair is "Fix conversation": it sets aside the pictures and documents
// the model can't read (attach.go) and, when the run failed, tries again.
// It returns the names of what was set aside.
func (rt *Runtime) Repair(ctx context.Context, u *store.User) ([]string, error) {
	l := rt.userLock(u.ID)
	l.Lock()
	defer l.Unlock()
	if rt.running(u.ID) != nil {
		return nil, ErrBusy
	}
	failed := rt.lastFailure(ctx, u.ID)
	names, err := rt.RepairAttachments(ctx, u.ID, failed != nil && namedAttachment(failed))
	if err != nil || len(names) == 0 {
		return names, err
	}
	_ = rt.addEntry(ctx, &store.Entry{UserID: u.ID, Kind: "note", Text: trim("Set aside "+strings.Join(names, ", ")+": Alexa couldn’t read it", 200)})
	if failed != nil {
		if err := rt.relaunch(ctx, u); err != nil && !errors.Is(err, ErrNothingToRetry) {
			return names, err
		}
	}
	rt.publishState(u.ID)
	return names, nil
}

// refusedAttachment marks, in an error entry's data, a failure where the
// model named a picture or a file it refused.
const refusedAttachment = "refusedAttachment"

// namedAttachment says whether the failure e records was the model refusing
// a picture or a file, rather than the request (a policy refusal, a bad
// tool schema), which setting aside older attachments can't fix.
func namedAttachment(e *store.Entry) bool {
	var d map[string]any
	_ = json.Unmarshal(e.Data, &d)
	refused, _ := d[refusedAttachment].(bool)
	return refused
}

// relaunch starts the failed run again. The caller holds the user lock.
func (rt *Runtime) relaunch(ctx context.Context, u *store.User) error {
	if rt.running(u.ID) != nil {
		return ErrBusy
	}
	if rt.pendingNeeds(ctx, u.ID) != nil {
		return ErrNothingToRetry // Alexa is waiting for an answer, not stuck
	}
	failed := rt.lastFailure(ctx, u.ID)
	if failed == nil {
		return ErrNothingToRetry
	}
	env, err := rt.envs.Ensure(u.ID)
	if err != nil {
		return err
	}
	if err := rt.st.SetEntryStatus(ctx, u.ID, failed.ID, "retried"); err == nil {
		rt.hub.Publish(u.ID, hub.Event{Type: "entryStatus", Data: map[string]any{"id": failed.ID, "status": "retried"}})
	}
	// Anything still waiting goes in first, so the conversation reads in
	// the order things happened.
	if _, err := rt.moveInbox(ctx, u.ID, nil); err != nil {
		return err
	}
	e := &store.Entry{UserID: u.ID, Kind: "user", Text: "Try again", Data: marshal(map[string]any{"via": "retry", "retry": failed.ID})}
	if err := rt.addEntry(ctx, e); err != nil {
		return err
	}
	said := `[Tapped "Try again" on the error] Try again: pick up where you left off.`
	if err := rt.appendMessage(ctx, u.ID, "user", stamp(userLoc(u), "", said), nil); err != nil {
		return err
	}
	rt.launch(u, env, true, false)
	return nil
}

// lastFailure is the error entry the conversation ends with, if it does
// and it hasn't been retried. Notes after it don't count.
func (rt *Runtime) lastFailure(ctx context.Context, userID int64) *store.Entry {
	feed, err := rt.st.Feed(ctx, userID, 0, 20)
	if err != nil {
		return nil
	}
	for i := len(feed) - 1; i >= 0; i-- {
		switch e := feed[i]; e.Kind {
		case "note", "plan":
			continue
		case "error":
			if e.Status == "retried" {
				return nil
			}
			return e
		default:
			return nil
		}
	}
	return nil
}
