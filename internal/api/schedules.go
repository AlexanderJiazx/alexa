package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"alexagent/internal/agent"
	"alexagent/internal/store"
)

func (s *Server) scheduleView(u *store.User, list []*store.Schedule) []*store.Schedule {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		loc = time.UTC
	}
	for _, sc := range list {
		if n := s.rt.Scheduler().NextRun(sc, loc); !n.IsZero() {
			sc.NextRun = n.UnixMilli()
		}
	}
	return list
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	list, err := s.st.ListSchedules(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Schedule{}
	}
	writeJSON(w, s.scheduleView(u, list))
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
		Cron   string `json:"cron"`
		RunAt  int64  `json:"runAt"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" || (req.Cron == "" && req.RunAt == 0) {
		writeErr(w, 400, "give a prompt and either a cron expression or a time")
		return
	}
	if req.Cron != "" {
		if _, err := agent.ParseCron(req.Cron); err != nil {
			writeErr(w, 400, "invalid cron expression: "+err.Error())
			return
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = shortName(req.Prompt)
	}
	sc := &store.Schedule{ID: uuid.NewString()[:8], UserID: u.ID, Name: name, Prompt: strings.TrimSpace(req.Prompt), Cron: strings.TrimSpace(req.Cron), RunAt: req.RunAt, Enabled: true}
	if err := s.st.CreateSchedule(r.Context(), sc); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rt.Scheduler().Reload(r.Context())
	s.changed(r, "schedules")
	writeJSON(w, s.scheduleView(u, []*store.Schedule{sc})[0])
}

func (s *Server) handlePatchSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Enabled != nil {
		if err := s.st.SetScheduleEnabled(r.Context(), userFrom(r).ID, r.PathValue("id"), *req.Enabled); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	s.rt.Scheduler().Reload(r.Context())
	s.changed(r, "schedules")
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if _, err := s.st.DeleteSchedule(r.Context(), userFrom(r).ID, r.PathValue("id")); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rt.Scheduler().Reload(r.Context())
	s.changed(r, "schedules")
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	sc, err := s.st.GetSchedule(r.Context(), r.PathValue("id"))
	if err != nil || sc.UserID != userFrom(r).ID {
		writeErr(w, 404, "schedule not found")
		return
	}
	s.rt.Scheduler().RunNow(sc.ID)
	s.changed(r, "schedules")
	writeJSON(w, map[string]bool{"ok": true})
}
