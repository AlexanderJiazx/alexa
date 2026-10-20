package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"alexagent/internal/store"
)

const (
	cookieName = "alexa_session"
	sessionTTL = 90 * 24 * time.Hour
)

type ctxKey struct{}

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxKey{}).(*store.User)
	return u
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, u *store.User) error {
	tok := newToken()
	if err := s.st.CreateSession(r.Context(), hashToken(tok), u.ID, sessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.cfg.SecureCookies || r.TLS != nil, MaxAge: int(sessionTTL.Seconds()),
	})
	return nil
}

func (s *Server) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.st.SessionUser(r.Context(), hashToken(c.Value))
	if err != nil {
		return nil
	}
	return u
}

// csrf guards unauthenticated state-changing endpoints (login, sign-up) with
// the same custom-header check that auth applies.
func csrf(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Alexa") == "" {
			writeErr(w, http.StatusForbidden, "missing X-Alexa header")
			return
		}
		h(w, r)
	}
}

// auth wraps a handler requiring a signed-in user. State-changing requests
// must carry the X-Alexa header (a CSRF guard: browsers can't add custom
// headers cross-site without a CORS preflight we never grant).
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Alexa") == "" && !isWebSocket(r) {
			writeErr(w, http.StatusForbidden, "missing X-Alexa header")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).IsAdmin {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		h(w, r)
	})
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{2,32}$`)

func validateCreds(username, password string) error {
	if !usernameRe.MatchString(username) {
		return errors.New("username must be 2–32 letters, digits, dots, dashes or underscores")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// limiter is a small per-IP login attempt limiter.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	cut := time.Now().Add(-10 * time.Minute)
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 20 {
		l.hits[ip] = keep
		return false
	}
	l.hits[ip] = append(keep, time.Now())
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, _ := s.st.CountUsers(r.Context())
	writeJSON(w, map[string]any{"needsSetup": n == 0, "signupAllowed": n == 0 || s.cfg.AllowSignup})
}

type credsReq struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	Timezone    string `json:"timezone"`
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if !readJSON(w, r, &req) {
		return
	}
	n, _ := s.st.CountUsers(r.Context())
	if n > 0 && !s.cfg.AllowSignup {
		writeErr(w, http.StatusForbidden, "sign-up is closed; ask the admin for an account")
		return
	}
	if !s.lim.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if err := validateCreds(req.Username, req.Password); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u, err := s.st.CreateUser(r.Context(), req.Username, hash, strings.TrimSpace(req.DisplayName), n == 0)
	if err != nil {
		writeErr(w, http.StatusConflict, "that username is taken")
		return
	}
	if req.Timezone != "" {
		if _, err := time.LoadLocation(req.Timezone); err == nil {
			_ = s.st.UpdateUserProfile(r.Context(), u.ID, u.DisplayName, u.Email, req.Timezone, u.About)
		}
	}
	if _, err := s.envs.Ensure(u.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := s.setSession(w, r, u); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u, _ = s.st.GetUser(r.Context(), u.ID)
	writeJSON(w, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if !readJSON(w, r, &req) {
		return
	}
	if !s.lim.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	u, err := s.st.GetUserByName(r.Context(), strings.TrimSpace(req.Username))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash()), []byte(req.Password)) != nil {
		writeErr(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	if req.Timezone != "" && (u.Timezone == "" || u.Timezone == "UTC") {
		if _, err := time.LoadLocation(req.Timezone); err == nil {
			_ = s.st.UpdateUserProfile(r.Context(), u.ID, u.DisplayName, u.Email, req.Timezone, u.About)
			u.Timezone = req.Timezone
		}
	}
	if err := s.setSession(w, r, u); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = s.st.DeleteSession(r.Context(), hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, map[string]bool{"ok": true})
}
