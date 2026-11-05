package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		credsReq
		IsAdmin bool `json:"isAdmin"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if err := validateCreds(req.Username, req.Password); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	hash, _ := hashPassword(req.Password)
	u, err := s.st.CreateUser(r.Context(), req.Username, hash, strings.TrimSpace(req.DisplayName), req.IsAdmin)
	if err != nil {
		writeErr(w, 409, "that username is taken")
		return
	}
	if _, err := s.envs.Ensure(u.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, u)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id == userFrom(r).ID {
		writeErr(w, 400, "you can't delete yourself")
		return
	}
	// Once begun, it's finished even if the request goes away.
	ctx := context.WithoutCancel(r.Context())
	if _, err := s.st.GetUser(ctx, id); err != nil {
		writeErr(w, 404, "no such user")
		return
	}
	// Their open sockets close now (4401), not at the next ping.
	_ = s.revokeUser(ctx, id, "")
	// Their run stops, their browser closes and nothing starts for them
	// again before their rows and folders go: a run or a relaunched Chrome
	// would make the folders again (BH-5).
	s.rt.RemoveUser(ctx, id)
	if err := s.st.DeleteUser(ctx, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.envs.Remove(id) // their files, screenshots and the pictures made from their photos
	s.rt.Scheduler().Reload(ctx)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Password) < 8 {
		writeErr(w, 400, "password must be at least 8 characters")
		return
	}
	hash, _ := hashPassword(req.Password)
	if err := s.st.UpdateUserPassword(r.Context(), id, hash); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.revokeUser(r.Context(), id, "")
	writeJSON(w, map[string]bool{"ok": true})
}
