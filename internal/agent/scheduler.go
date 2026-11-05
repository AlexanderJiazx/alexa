package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"alexagent/internal/hub"
	"alexagent/internal/store"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Scheduler runs users' schedules: cron specs evaluated in each user's
// timezone, and one-off runs. Its ticker also runs the storage sweep
// (Runtime.Sweep): soon after start, then every sweepEvery.
type Scheduler struct {
	rt     *Runtime
	mu     sync.Mutex
	c      *cron.Cron
	tries  map[string]int // one-offs that failed to start, and how often
	reload sync.Mutex     // one Reload at a time: an older table never replaces a newer one
}

func newScheduler(rt *Runtime) *Scheduler { return &Scheduler{rt: rt, tries: map[string]int{}} }

func (s *Scheduler) Start(ctx context.Context) {
	s.Reload(ctx)
	go func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		var swept time.Time // zero: the first tick sweeps
		for {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				if s.c != nil {
					s.c.Stop()
				}
				s.mu.Unlock()
				return
			case <-tick.C:
				s.runDueOneOffs(ctx)
				if time.Since(swept) >= sweepEvery {
					swept = time.Now()
					go s.rt.Sweep(ctx) // not in the way of the next one-off
				}
			}
		}
	}()
}

func (s *Scheduler) spec(sc *store.Schedule, loc *time.Location) string {
	return "CRON_TZ=" + loc.String() + " " + sc.Cron
}

// Reload rebuilds the cron table from the store. It doesn't end with ctx:
// the tool call or request that changed a schedule may be cut short (a
// Stop), and the table must still follow the change (BH-8).
func (s *Scheduler) Reload(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	s.reload.Lock()
	defer s.reload.Unlock()
	list, err := s.rt.st.ListSchedules(ctx, 0)
	if err != nil {
		slog.Error("load schedules", "err", err)
		return
	}
	c := cron.New(cron.WithParser(cronParser))
	users := map[int64]*store.User{}
	for _, sc := range list {
		if !sc.Enabled || sc.Cron == "" {
			continue
		}
		u := users[sc.UserID]
		if u == nil {
			if u, err = s.rt.st.GetUser(ctx, sc.UserID); err != nil {
				continue
			}
			users[sc.UserID] = u
		}
		id := sc.ID
		if _, err := c.AddFunc(s.spec(sc, userLoc(u)), func() { s.fire(id, false) }); err != nil {
			slog.Warn("bad schedule", "id", sc.ID, "err", err)
		}
	}
	c.Start()
	s.mu.Lock()
	old := s.c
	s.c = c
	s.mu.Unlock()
	if old != nil {
		old.Stop()
	}
}

func (s *Scheduler) runDueOneOffs(ctx context.Context) {
	list, err := s.rt.st.ListSchedules(ctx, 0)
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	for _, sc := range list {
		if sc.Enabled && sc.RunAt > 0 && sc.RunAt <= now && sc.LastRun == 0 {
			// Disabled first, so it can't fire twice; one that fails to
			// start is enabled again for another try (failed).
			_ = s.rt.st.SetScheduleEnabled(ctx, sc.UserID, sc.ID, false)
			s.fire(sc.ID, true)
		}
	}
}

// fire starts a schedule's job. due says the one-off's time came, rather
// than the user asking for it now.
func (s *Scheduler) fire(id string, due bool) {
	ctx := context.Background()
	sc, err := s.rt.st.GetSchedule(ctx, id)
	if err != nil {
		return
	}
	u, err := s.rt.st.GetUser(ctx, sc.UserID)
	if err != nil {
		return
	}
	slog.Info("running schedule", "id", sc.ID, "name", sc.Name, "user", u.Username)
	if err := s.rt.Send(ctx, u, Message{Text: sc.Prompt, Source: "schedule", Label: sc.Name}); err != nil {
		s.failed(ctx, u, sc, due, err)
		return
	}
	s.mu.Lock()
	delete(s.tries, sc.ID)
	s.mu.Unlock()
	_ = s.rt.st.MarkScheduleRun(ctx, sc.ID)
}

// oneOffTries is how often a one-off whose time came is tried, a tick
// apart, before it's left for the user to run.
const oneOffTries = 3

// failed handles a job that couldn't start (BH-8). A one-off whose time
// came is enabled again, so the next tick tries it, up to oneOffTries
// times; after that it stays disabled and not run, for the user to run
// from their schedules. The user hears of a job that didn't run, then or
// at once: a recurring one runs again at its next time.
func (s *Scheduler) failed(ctx context.Context, u *store.User, sc *store.Schedule, due bool, err error) {
	slog.Error("schedule run failed", "id", sc.ID, "err", err)
	oneOff := sc.RunAt > 0 && sc.LastRun == 0
	if due && oneOff {
		s.mu.Lock()
		s.tries[sc.ID]++
		again := s.tries[sc.ID] < oneOffTries
		if !again {
			delete(s.tries, sc.ID)
		}
		s.mu.Unlock()
		if again {
			_ = s.rt.st.SetScheduleEnabled(ctx, u.ID, sc.ID, true)
			return
		}
	}
	body := fmt.Sprintf("“%s” couldn't start (%s).", sc.Name, sanitize(err.Error(), 160))
	switch {
	case sc.Cron != "":
		body += " It will run again at its next time."
	case oneOff:
		body += " Run it from your schedules when you're ready."
	}
	_ = s.rt.notify(ctx, u, "", "A scheduled job didn't run", body, true, false)
	s.rt.hub.Publish(u.ID, hub.Changed("schedules"))
}

// RunNow fires a schedule immediately.
func (s *Scheduler) RunNow(id string) { go s.fire(id, false) }

// NextRun computes when a schedule will next run.
func (s *Scheduler) NextRun(sc *store.Schedule, loc *time.Location) time.Time {
	if !sc.Enabled {
		return time.Time{}
	}
	if sc.RunAt > 0 {
		if sc.LastRun > 0 {
			return time.Time{}
		}
		return time.UnixMilli(sc.RunAt)
	}
	sched, err := cronParser.Parse(s.spec(sc, loc))
	if err != nil {
		return time.Time{}
	}
	return sched.Next(time.Now())
}

// NextRuns lists up to n upcoming runs of a schedule.
func (s *Scheduler) NextRuns(sc *store.Schedule, loc *time.Location, n int) []time.Time {
	next := s.NextRun(sc, loc)
	if next.IsZero() {
		return nil
	}
	runs := []time.Time{next}
	if sc.RunAt > 0 {
		return runs
	}
	sched, err := cronParser.Parse(s.spec(sc, loc))
	if err != nil {
		return runs
	}
	for len(runs) < n {
		runs = append(runs, sched.Next(runs[len(runs)-1]))
	}
	return runs
}

// cronDayWarning flags a spec that restricts both the day of the month and
// the weekday: cron then runs on days matching either one, which is rarely
// what was meant ("the second Tuesday" is not "0 18 8-14 * 2").
func cronDayWarning(spec string) string {
	f := strings.Fields(spec)
	if len(f) != 5 || f[2] == "*" || f[2] == "?" || f[4] == "*" || f[4] == "?" {
		return ""
	}
	return "Warning: this cron sets both a day of the month and a weekday, so it runs on every day that matches EITHER of them, not only days that match both. " +
		"For something like \"the second Tuesday\", delete it (schedule_delete) and schedule the weekday alone with a prompt that first checks the date."
}

// ParseCron validates a 5-field cron expression.
func ParseCron(spec string) (cron.Schedule, error) { return cronParser.Parse(spec) }
