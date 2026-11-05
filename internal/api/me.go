package api

import (
	"net/http"
	"strings"
	"time"

	"alexagent/internal/hub"
)

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) { writeJSON(w, userFrom(r)) }

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req struct {
		DisplayName *string `json:"displayName"`
		Email       *string `json:"email"`
		Timezone    *string `json:"timezone"`
		About       *string `json:"about"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		u.Email = strings.TrimSpace(*req.Email)
	}
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			writeErr(w, 400, "unknown timezone")
			return
		}
		u.Timezone = *req.Timezone
	}
	if req.About != nil {
		u.About = *req.About
	}
	if err := s.st.UpdateUserProfile(r.Context(), u.ID, u.DisplayName, u.Email, u.Timezone, u.About); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rt.Scheduler().Reload(r.Context()) // timezone affects cron
	s.hub.Publish(u.ID, hub.Changed("me"))
	writeJSON(w, u)
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	full, err := s.st.GetUser(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := bcryptCompare(full.PasswordHash(), req.Current); err != nil {
		writeErr(w, 403, "current password is wrong")
		return
	}
	if len(req.New) < 8 {
		writeErr(w, 400, "password must be at least 8 characters")
		return
	}
	hash, _ := hashPassword(req.New)
	if err := s.st.UpdateUserPassword(r.Context(), u.ID, hash); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// A new password signs out every other device, and their open sockets
	// close at once; this one stays signed in.
	_ = s.revokeUser(r.Context(), u.ID, sessionHash(r))
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"model":          s.cfg.Model,
		"viewport":       map[string]int{"width": s.cfg.ViewportWidth, "height": s.cfg.ViewportHeight},
		"emailEnabled":   s.notifier.EmailEnabled(),
		"shellEnabled":   s.cfg.ShellEnabled,
		"sandboxed":      s.cfg.SandboxUIDs,
		"vapidPublicKey": s.notifier.VAPIDPublicKey(),
	})
}
