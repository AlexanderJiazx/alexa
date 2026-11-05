package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"alexagent/internal/store"
)

// The plan is Alexa's checklist for a job of several steps, shown to the user
// as a card with their progress (SYNTHESIS Appendix B.1). Each plan call
// replaces the whole plan. It belongs to the current exchange: it is shown by
// one "plan" entry, updated in place, and kept with the session so the view,
// the steps (which point at the plan step they serve) and compaction can read
// it. A new exchange starts without one.

// Plan is the checklist as the model wrote it and the user sees it.
type Plan struct {
	Title string     `json:"title,omitempty"`
	Steps []PlanStep `json:"steps"`
	Note  string     `json:"note,omitempty"`
}

// PlanStep is one line of the plan.
type PlanStep struct {
	Text   string `json:"text"`
	Status string `json:"status"` // pending | active | done | failed | skipped
	Note   string `json:"note,omitempty"`
}

const (
	planMinSteps = 2
	planMaxSteps = 8
)

// promptPlan tells Alexa when to plan and how to keep the plan honest.
const promptPlan = `# Plan and progress
For a job with 3 or more steps, call plan before you start, with 2 to 8 short steps, and keep it current: exactly one step active (the one you're on), and update the plan when a step finishes or the plan changes. The user sees it as a checklist with their progress, so it must be true: mark a step done only when it really is, failed when you couldn't do it, and skipped when it's no longer needed. Skip the plan for quick requests.`

func planTool() *tool {
	return &tool{
		Name: "plan",
		Description: "Show the user your plan for a job of 3 or more steps, as a checklist they can follow, and keep it current. Each call replaces the whole plan. " +
			"Exactly one step is active while you work; call plan again when a step finishes, fails or the plan changes.",
		NoStatus: true,
		Params: map[string]any{
			"title": p("string", "Short title for the job, e.g. \"Book a table for Friday\" (under 60 characters)"),
			"steps": map[string]any{"type": "array", "description": "2 to 8 steps, in order", "items": map[string]any{"type": "object", "properties": map[string]any{
				"text":   p("string", "The step in a few words, e.g. \"Compare prices\" (under 60 characters)"),
				"status": map[string]any{"type": "string", "enum": []string{"pending", "active", "done", "failed", "skipped"}},
				"note":   p("string", "Optional short note, e.g. what you found or why it failed (under 80 characters)"),
			}, "required": []string{"text", "status"}}},
			"note": p("string", "Optional note on the whole plan (under 140 characters)"),
		},
		Required: []string{"steps"},
		Run:      runPlan,
	}
}

func runPlan(tc *toolCtx, a map[string]any) (*toolResult, error) {
	pl, fix := parsePlan(a)
	if fix != "" {
		return &toolResult{Text: "Not updated: " + fix}, nil
	}
	uid := tc.run.user.ID
	rec := tc.rt.loadPlan(tc.ctx, uid)
	if rec == nil {
		rec = &planRecord{}
	}
	rec.Plan = pl
	if err := tc.rt.showPlan(tc.ctx, uid, rec); err != nil {
		return nil, err
	}
	tc.rt.publishState(uid)
	return &toolResult{Text: planSummary(pl)}, nil
}

// planStatuses maps what the model may write to a step status; anything else
// is a fixable error.
var planStatuses = map[string]string{
	"pending": "pending", "todo": "pending", "to do": "pending", "next": "pending",
	"active": "active", "in_progress": "active", "in progress": "active", "current": "active", "doing": "active", "running": "active",
	"done": "done", "complete": "done", "completed": "done",
	"failed": "failed", "error": "failed",
	"skipped": "skipped", "skip": "skipped", "cancelled": "skipped", "canceled": "skipped",
}

// parsePlan reads and checks a plan call. A problem the model can fix comes
// back as fix, worded for it; small slips (a step given as a plain string, no
// active step while some are pending) are put right instead.
func parsePlan(a map[string]any) (*Plan, string) {
	arr, ok := a["steps"].([]any)
	if !ok {
		return nil, "give steps as a list of {text, status}."
	}
	pl := &Plan{Title: trim(cleanText(str(a, "title")), 60), Note: trim(cleanText(str(a, "note")), 140)}
	for i, v := range arr {
		st := PlanStep{Status: "pending"}
		switch v := v.(type) {
		case string:
			st.Text = v
		case map[string]any:
			st.Text, st.Note = str(v, "text"), str(v, "note")
			if s := strings.ToLower(strings.TrimSpace(str(v, "status"))); s != "" {
				if st.Status, ok = planStatuses[s]; !ok {
					return nil, fmt.Sprintf("step %d has status %q; use pending, active, done, failed or skipped.", i+1, s)
				}
			}
		}
		st.Text, st.Note = trim(cleanText(st.Text), 60), trim(cleanText(st.Note), 80)
		if st.Text == "" {
			return nil, fmt.Sprintf("step %d has no text.", i+1)
		}
		pl.Steps = append(pl.Steps, st)
	}
	if n := len(pl.Steps); n < planMinSteps || n > planMaxSteps {
		hint := "For a quick job, skip the plan."
		if n > planMaxSteps {
			hint = "Merge some steps."
		}
		return nil, fmt.Sprintf("a plan has %d to %d steps (this one has %d). %s", planMinSteps, planMaxSteps, n, hint)
	}
	var active []string
	firstOpen := -1
	for i, st := range pl.Steps {
		switch st.Status {
		case "active":
			active = append(active, fmt.Sprint(i+1))
		case "pending":
			if firstOpen < 0 {
				firstOpen = i
			}
		}
	}
	if len(active) > 1 {
		return nil, fmt.Sprintf("steps %s are all active. Keep exactly one step active, the one you're on, and mark the others pending or done.", strings.Join(active, " and "))
	}
	if len(active) == 0 && firstOpen >= 0 {
		// Between steps the next one is the one about to start.
		pl.Steps[firstOpen].Status = "active"
	}
	return pl, ""
}

// planSummary is the plan tool's answer: "Plan updated (1 of 4 done).
// Current: Compare prices."
func planSummary(pl *Plan) string {
	n := map[string]int{}
	cur := ""
	for _, st := range pl.Steps {
		n[st.Status]++
		if st.Status == "active" {
			cur = st.Text
		}
	}
	extra := ""
	for _, s := range []string{"failed", "skipped"} {
		if n[s] > 0 {
			extra += fmt.Sprintf(", %d %s", n[s], s)
		}
	}
	out := fmt.Sprintf("Plan updated (%d of %d done%s).", n["done"], len(pl.Steps), extra)
	if cur != "" {
		out += " Current: " + strings.TrimSuffix(cur, ".") + "."
	}
	return out
}

func stepClosed(status string) bool {
	return status == "done" || status == "failed" || status == "skipped"
}

// openSteps lists the steps that aren't finished.
func (pl *Plan) openSteps() []string {
	var out []string
	for _, st := range pl.Steps {
		if !stepClosed(st.Status) {
			out = append(out, st.Text)
		}
	}
	return out
}

// active returns the index of the active step, or -1.
func (pl *Plan) active() int {
	for i, st := range pl.Steps {
		if st.Status == "active" {
			return i
		}
	}
	return -1
}

// text renders the plan for the model: in the compaction summary and when
// it's recited after compaction. Every field is kept, word for word.
func (pl *Plan) text() string {
	var b strings.Builder
	if pl.Title != "" {
		b.WriteString(pl.Title + "\n")
	}
	for i, st := range pl.Steps {
		fmt.Fprintf(&b, "%d. [%s] %s", i+1, st.Status, st.Text)
		if st.Note != "" {
			b.WriteString(" (" + st.Note + ")")
		}
		b.WriteString("\n")
	}
	if pl.Note != "" {
		b.WriteString("Note: " + pl.Note + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// planRecord is the current exchange's plan as kept with the session.
type planRecord struct {
	Plan      *Plan `json:"plan"`
	EntryID   int64 `json:"entryId"`
	CreatedAt int64 `json:"createdAt"`
	// Checked: the one completion self-check of this exchange was given.
	Checked bool `json:"checked,omitempty"`
}

// The plan is kept in the store's key-value table; the session table has no
// place for it yet.
func planKey(userID int64) string { return fmt.Sprintf("plan:%d", userID) }

func (rt *Runtime) loadPlan(ctx context.Context, userID int64) *planRecord {
	v, err := rt.st.GetKV(ctx, planKey(userID))
	if err != nil || v == "" {
		return nil
	}
	var rec planRecord
	if json.Unmarshal([]byte(v), &rec) != nil || rec.Plan == nil {
		return nil
	}
	return &rec
}

func (rt *Runtime) savePlan(ctx context.Context, userID int64, rec *planRecord) error {
	return rt.st.SetKV(ctx, planKey(userID), string(marshal(rec)))
}

// clearPlan forgets the plan: a new exchange starts without one.
func (rt *Runtime) clearPlan(ctx context.Context, userID int64) {
	if rt.loadPlan(ctx, userID) != nil {
		_ = rt.st.SetKV(ctx, planKey(userID), "")
	}
}

// showPlan saves the plan and shows it in its entry: the exchange's first
// plan adds the entry, later ones update it in place.
func (rt *Runtime) showPlan(ctx context.Context, userID int64, rec *planRecord) error {
	pl := rec.Plan
	e := &store.Entry{ID: rec.EntryID, UserID: userID, Kind: "plan", Text: pl.Title, Status: "running",
		CreatedAt: rec.CreatedAt, Data: marshal(map[string]any{"plan": pl})}
	if e.Text == "" {
		e.Text = "Plan"
	}
	if len(pl.openSteps()) == 0 {
		e.Status = "done"
	}
	if e.ID == 0 {
		if err := rt.addEntry(ctx, e); err != nil {
			return err
		}
		rec.EntryID, rec.CreatedAt = e.ID, e.CreatedAt
	} else {
		rt.updateEntry(ctx, e)
	}
	return rt.savePlan(ctx, userID, rec)
}

// viewPlan is the plan the view shows. While the session is idle it checks
// that the plan's entry is still there: resetting the conversation deletes
// the entries but not the plan.
func (rt *Runtime) viewPlan(ctx context.Context, userID int64, status string) *Plan {
	rec := rt.loadPlan(ctx, userID)
	if rec == nil {
		return nil
	}
	if status == "idle" {
		if e, err := rt.st.Feed(ctx, userID, rec.EntryID+1, 1); err == nil && (len(e) == 0 || e[0].ID != rec.EntryID) {
			rt.clearPlan(ctx, userID)
			return nil
		}
	}
	return rec.Plan
}

// currentPlan is the current exchange's plan, or nil.
func (rt *Runtime) currentPlan(ctx context.Context, userID int64) *Plan {
	if rec := rt.loadPlan(ctx, userID); rec != nil {
		return rec.Plan
	}
	return nil
}

// planReminderLead starts the completion self-check. A final say whose
// result carries it must not end the run there: the model needs one more
// turn to read it and put the plan right.
const planReminderLead = "[Alexa system note] Your plan still has open steps"

// planReminder is the exchange's one completion self-check: when Alexa
// finishes while plan steps are still open, it's reminded once to finish
// them or to say why not. It returns "" when there's nothing to remind.
func (rt *Runtime) planReminder(ctx context.Context, userID int64) string {
	rec := rt.loadPlan(ctx, userID)
	if rec == nil || rec.Checked {
		return ""
	}
	open := rec.Plan.openSteps()
	if len(open) == 0 {
		return ""
	}
	rec.Checked = true
	if err := rt.savePlan(ctx, userID, rec); err != nil {
		return ""
	}
	return fmt.Sprintf(planReminderLead+": “%s”. If they're done or no longer needed, update the plan (done, failed or skipped) so the user's checklist is true; otherwise carry on with them before you finish.",
		strings.Join(open, "”, “"))
}

// withPlan puts the plan into a compaction summary word for word, under
// "## Plan", replacing whatever the summary said under that heading: the
// summariser may paraphrase it, and an older plan is obsolete.
func withPlan(summary string, pl *Plan) string {
	summary = dropSection(summary, "## Plan")
	if pl == nil {
		return summary
	}
	return strings.TrimSpace(summary) + "\n\n## Plan\n" + pl.text()
}

// dropSection removes a markdown section, from its heading line to the next
// heading of the same level.
func dropSection(md, heading string) string {
	lines := strings.Split(md, "\n")
	var out []string
	in := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == heading:
			in = true
			continue
		case in && strings.HasPrefix(t, "## "):
			in = false
		}
		if !in {
			out = append(out, l)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// recitePlan reminds Alexa of an unfinished plan right after compaction, at
// the end of the conversation where it gets the most attention. While a
// question waits, nothing may come between it and its answer: the reminder
// waits in the inbox and follows the answer.
func (rt *Runtime) recitePlan(ctx context.Context, userID int64, pl *Plan) error {
	if pl == nil || len(pl.openSteps()) == 0 {
		return nil
	}
	note := "[Alexa system note] The conversation was just summarised. Your plan, as you last set it (keep it current with plan):\n" + pl.text()
	if rt.pendingNeeds(ctx, userID) != nil {
		return rt.st.AddInbox(ctx, userID, store.InboxMessage{Kind: "system", Text: note})
	}
	return rt.appendMessage(ctx, userID, "system", note, nil)
}
