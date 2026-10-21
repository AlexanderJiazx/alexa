package api

import (
	"errors"
	"net/http"
	"strconv"

	"alexagent/internal/store"
)

// ---------- saved logins ----------

// Passwords go in through these handlers and the sign-in form, and never
// come back out: the list has sites and usernames only.

type loginReq struct {
	Site     string `json:"site"`
	Username string `json:"username"`
	Password string `json:"password"`
	Save     bool   `json:"save"`
}

func (s *Server) handleListLogins(w http.ResponseWriter, r *http.Request) {
	list, err := s.vault.List(r.Context(), userFrom(r).ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Login{}
	}
	writeJSON(w, list)
}

func (s *Server) handleAddLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !readJSON(w, r, &req) {
		return
	}
	l, err := s.vault.Save(r.Context(), userFrom(r).ID, req.Site, req.Username, req.Password)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, l)
}

func (s *Server) handleUpdateLogin(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req loginReq
	if !readJSON(w, r, &req) {
		return
	}
	l, err := s.vault.Update(r.Context(), userFrom(r).ID, id, req.Username, req.Password)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "login not found")
	case errors.Is(err, store.ErrExists):
		writeErr(w, 409, "you already have a login with that username for this site")
	case err != nil:
		writeErr(w, 400, err.Error())
	default:
		writeJSON(w, l)
	}
}

func (s *Server) handleDeleteLogin(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ok, err := s.vault.Delete(r.Context(), userFrom(r).ID, id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 404, "login not found")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSignIn takes the login the user typed into the secure form Alexa
// asked them to fill in.
func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.rt.SignIn(r.Context(), userFrom(r), req.Username, req.Password, req.Save); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
