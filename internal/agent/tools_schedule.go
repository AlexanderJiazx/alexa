package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"alexagent/internal/hub"
	"alexagent/internal/store"
)

// scheduleTools create and manage schedules, whose prompts arrive in the
// conversation when they fire.
func scheduleTools() []*tool {
	return []*tool{
		{
			Name: "schedule_create",
			Description: "Create a schedule: recurring (cron, in the user's timezone) or once at a time. When it fires, its prompt arrives in this conversation as a scheduled message for you to act on. " +
				"Use for reminders, daily briefings and monitoring (\"check X and tell me if Y\"). Write the prompt as a self-contained instruction to yourself.",
			Params: map[string]any{
				"name":   p("string", "Short name, e.g. \"Morning briefing\""),
				"prompt": p("string", "What to do each time it fires"),
				"cron":   p("string", "Standard 5-field cron expression (minute hour day month weekday) for recurring runs, e.g. \"0 8 * * 1-5\". As in any cron, if both day and weekday are set it runs on days matching either one"),
				"run_at": p("string", "For a one-off run: local date-time like 2026-10-21T09:00 or RFC3339"),
			},
			Required: []string{"name", "prompt"},
			Run:      scheduleCreate,
		},
		{
			Name:        "schedule_list",
			Description: "List the user's schedules.",
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				return &toolResult{Text: tc.rt.describeSchedules(tc.ctx, tc.run.user)}, nil
			},
		},
		{
			Name:        "schedule_delete",
			Description: "Delete a schedule by id.",
			Params:      map[string]any{"id": p("string", "Schedule id")},
			Required:    []string{"id"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				ok, err := tc.rt.st.DeleteSchedule(tc.ctx, tc.run.user.ID, str(a, "id"))
				if err != nil {
					return nil, err
				}
				tc.rt.sched.Reload(tc.ctx)
				tc.rt.hub.Publish(tc.run.user.ID, hub.Changed("schedules"))
				if !ok {
					return &toolResult{Text: "No such schedule."}, nil
				}
				return &toolResult{Text: "Deleted."}, nil
			},
		},
	}
}

func scheduleCreate(tc *toolCtx, a map[string]any) (*toolResult, error) {
	sc := &store.Schedule{
		ID:      uuid.NewString()[:8],
		UserID:  tc.run.user.ID,
		Name:    str(a, "name"),
		Prompt:  str(a, "prompt"),
		Cron:    strings.TrimSpace(str(a, "cron")),
		Enabled: true,
	}
	if ra := strings.TrimSpace(str(a, "run_at")); ra != "" {
		t, err := parseLocalTime(ra, tc.run.loc())
		if err != nil {
			return nil, err
		}
		if t.Before(time.Now()) {
			return nil, fmt.Errorf("run_at %s is in the past", t.Format(time.RFC3339))
		}
		sc.RunAt = t.UnixMilli()
	}
	if sc.Cron == "" && sc.RunAt == 0 {
		return nil, errors.New("give either cron or run_at")
	}
	if sc.Cron != "" {
		if _, err := cronParser.Parse(sc.Cron); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
	}
	if err := tc.rt.st.CreateSchedule(tc.ctx, sc); err != nil {
		return nil, err
	}
	tc.rt.sched.Reload(tc.ctx)
	tc.rt.hub.Publish(tc.run.user.ID, hub.Changed("schedules"))
	// Listing the next few runs lets the model check the schedule does what
	// was asked.
	var when []string
	for _, t := range tc.rt.sched.NextRuns(sc, tc.run.loc(), 4) {
		when = append(when, t.In(tc.run.loc()).Format("Mon Jan 2 2006 15:04 MST"))
	}
	msg := fmt.Sprintf("Created schedule %s (%q). Next run: %s.", sc.ID, sc.Name, strings.Join(when, ""))
	if len(when) > 1 {
		msg = fmt.Sprintf("Created schedule %s (%q). Next runs: %s.", sc.ID, sc.Name, strings.Join(when, "; "))
	}
	if w := cronDayWarning(sc.Cron); w != "" {
		msg += " " + w
	}
	return &toolResult{Text: msg}, nil
}

func parseLocalTime(s string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse time %q (use 2006-01-02T15:04)", s)
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
