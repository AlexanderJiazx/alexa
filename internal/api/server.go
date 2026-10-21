// Package api exposes the HTTP and WebSocket API and serves the frontend.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"alexagent/internal/agent"
	"alexagent/internal/browser"
	"alexagent/internal/config"
	"alexagent/internal/hub"
	"alexagent/internal/notify"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
	"alexagent/internal/vault"
	"alexagent/internal/voice"
)

type Server struct {
	cfg      *config.Config
	st       *store.Store
	rt       *agent.Runtime
	hub      *hub.Hub
	envs     *userenv.Envs
	notifier *notify.Notifier
	voice    *voice.Service
	browsers *browser.Manager
	vault    *vault.Vault
	static   fs.FS
	lim      limiter
}

func New(cfg *config.Config, st *store.Store, rt *agent.Runtime, h *hub.Hub, envs *userenv.Envs, n *notify.Notifier, v *voice.Service, br *browser.Manager, logins *vault.Vault, static fs.FS) *Server {
	return &Server{cfg: cfg, st: st, rt: rt, hub: h, envs: envs, notifier: n, voice: v, browsers: br, vault: logins, static: static}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// auth
	mux.HandleFunc("GET /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/auth/signup", csrf(s.handleSignup))
	mux.HandleFunc("POST /api/auth/login", csrf(s.handleLogin))
	mux.HandleFunc("POST /api/auth/logout", csrf(s.handleLogout))
	mux.HandleFunc("GET /api/me", s.auth(s.handleMe))
	mux.HandleFunc("PUT /api/me", s.auth(s.handleUpdateMe))
	mux.HandleFunc("PUT /api/me/password", s.auth(s.handlePassword))
	mux.HandleFunc("GET /api/config", s.auth(s.handleConfig))
	// admin
	mux.HandleFunc("GET /api/admin/users", s.admin(s.handleListUsers))
	mux.HandleFunc("POST /api/admin/users", s.admin(s.handleCreateUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", s.admin(s.handleDeleteUser))
	mux.HandleFunc("PUT /api/admin/users/{id}/password", s.admin(s.handleResetPassword))
	// the session (one per user, never ending)
	mux.HandleFunc("GET /api/session", s.auth(s.handleSession))
	mux.HandleFunc("GET /api/feed", s.auth(s.handleFeed))
	mux.HandleFunc("POST /api/messages", s.auth(s.handleMessage))
	mux.HandleFunc("POST /api/takeover", s.auth(s.handleTakeOver))
	mux.HandleFunc("POST /api/handback", s.auth(s.handleHandBack))
	mux.HandleFunc("POST /api/stop", s.auth(s.handleStop))
	mux.HandleFunc("PUT /api/surface", s.auth(s.handleSurface))
	mux.HandleFunc("POST /api/session/compact", s.auth(s.handleCompact))
	mux.HandleFunc("POST /api/session/reset", s.auth(s.handleReset))
	mux.HandleFunc("GET /api/live", s.auth(s.handleLive))
	mux.HandleFunc("POST /api/signin", s.auth(s.handleSignIn))
	// saved logins
	mux.HandleFunc("GET /api/logins", s.auth(s.handleListLogins))
	mux.HandleFunc("POST /api/logins", s.auth(s.handleAddLogin))
	mux.HandleFunc("PUT /api/logins/{id}", s.auth(s.handleUpdateLogin))
	mux.HandleFunc("DELETE /api/logins/{id}", s.auth(s.handleDeleteLogin))
	// reports
	mux.HandleFunc("GET /api/reports", s.auth(s.handleListReports))
	mux.HandleFunc("GET /api/reports/{id}", s.auth(s.handleGetReport))
	mux.HandleFunc("DELETE /api/reports/{id}", s.auth(s.handleDeleteReport))
	mux.HandleFunc("GET /api/reports/{id}/download", s.auth(s.handleReportDownload))
	mux.HandleFunc("GET /api/reports/{id}/calendar.ics", s.auth(s.handleICS))
	// files
	mux.HandleFunc("POST /api/uploads", s.auth(s.handleUpload))
	mux.HandleFunc("GET /api/files", s.auth(s.handleFiles))
	mux.HandleFunc("GET /api/files/download", s.auth(s.handleDownload))
	mux.HandleFunc("DELETE /api/files", s.auth(s.handleDeleteFile))
	// memory
	mux.HandleFunc("GET /api/memories", s.auth(s.handleListMemories))
	mux.HandleFunc("POST /api/memories", s.auth(s.handleAddMemory))
	mux.HandleFunc("PUT /api/memories/{id}", s.auth(s.handleUpdateMemory))
	mux.HandleFunc("DELETE /api/memories/{id}", s.auth(s.handleDeleteMemory))
	// schedules
	mux.HandleFunc("GET /api/schedules", s.auth(s.handleListSchedules))
	mux.HandleFunc("POST /api/schedules", s.auth(s.handleCreateSchedule))
	mux.HandleFunc("PATCH /api/schedules/{id}", s.auth(s.handlePatchSchedule))
	mux.HandleFunc("DELETE /api/schedules/{id}", s.auth(s.handleDeleteSchedule))
	mux.HandleFunc("POST /api/schedules/{id}/run", s.auth(s.handleRunSchedule))
	// notifications
	mux.HandleFunc("GET /api/notifications", s.auth(s.handleNotifications))
	mux.HandleFunc("POST /api/notifications/read", s.auth(s.handleNotificationsRead))
	mux.HandleFunc("GET /api/push/key", s.auth(s.handlePushKey))
	mux.HandleFunc("POST /api/push/subscribe", s.auth(s.handlePushSubscribe))
	// realtime
	mux.HandleFunc("GET /api/ws", s.auth(s.handleWS))
	mux.HandleFunc("GET /api/voice", s.auth(s.handleVoice))
	mux.HandleFunc("POST /api/transcribe", s.auth(s.handleTranscribe))
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeErr(w, 404, "not found") })
	mux.HandleFunc("/", s.handleStatic)
	return logRequests(securityHeaders(mux))
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func sessionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrNotFound):
		writeErr(w, 404, "not found")
	case errors.Is(err, agent.ErrBusy), errors.Is(err, agent.ErrNotWaiting):
		writeErr(w, 409, err.Error())
	default:
		writeErr(w, 400, err.Error())
	}
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int)           { w.code = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		h.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && !isWebSocket(r) {
			slog.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.code, "dur", time.Since(start))
		}
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) acceptOpts() *websocket.AcceptOptions {
	o := &websocket.AcceptOptions{}
	if s.cfg.DevCORSOrigin != "" {
		o.OriginPatterns = []string{strings.TrimPrefix(strings.TrimPrefix(s.cfg.DevCORSOrigin, "http://"), "https://")}
	}
	return o
}

// ---------- me ----------

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
	_ = s.st.UpdateUserPassword(r.Context(), u.ID, hash)
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

// ---------- admin ----------

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
	s.browsers.CloseUser(id)
	if err := s.st.DeleteUser(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	env := s.envs.For(id)
	_ = os.RemoveAll(env.Root)
	_ = os.RemoveAll(env.Shots)
	s.rt.Scheduler().Reload(r.Context())
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
	_ = s.st.DeleteUserSessions(r.Context(), id)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------- session ----------

const feedPage = 80

// handleSession returns everything the app needs to draw the session: its
// state, the latest feed and the report on screen, if any.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	view := s.rt.View(r.Context(), u.ID)
	feed, err := s.st.Feed(r.Context(), u.ID, 0, feedPage+1)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	more := len(feed) > feedPage
	if more {
		feed = feed[1:]
	}
	if feed == nil {
		feed = []*store.Entry{}
	}
	out := map[string]any{"state": view, "feed": feed, "hasMore": more}
	if view.Surface.Kind == "report" {
		if rep, err := s.st.GetReport(r.Context(), u.ID, view.Surface.ID); err == nil {
			out["report"] = rep
		}
	}
	writeJSON(w, out)
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	feed, err := s.st.Feed(r.Context(), userFrom(r).ID, before, feedPage+1)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	more := len(feed) > feedPage
	if more {
		feed = feed[1:]
	}
	if feed == nil {
		feed = []*store.Entry{}
	}
	writeJSON(w, map[string]any{"feed": feed, "hasMore": more})
}

type messageReq struct {
	Text        string             `json:"text"`
	Attachments []agent.Attachment `json:"attachments"`
	Voice       bool               `json:"voice"`
}

func (s *Server) checkAttachments(u *store.User, list []agent.Attachment) ([]agent.Attachment, error) {
	env := s.envs.For(u.ID)
	var out []agent.Attachment
	for _, a := range list {
		full, err := env.Resolve(a.Path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(full)
		if err != nil {
			return nil, fmt.Errorf("attachment %s not found", a.Path)
		}
		a.Path = env.Rel(full)
		a.Size = info.Size()
		if a.Mime == "" {
			a.Mime = mime.TypeByExtension(filepath.Ext(full))
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	var req messageReq
	if !readJSON(w, r, &req) {
		return
	}
	u := userFrom(r)
	att, err := s.checkAttachments(u, req.Attachments)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.rt.Send(r.Context(), u, agent.Message{Text: req.Text, Attach: att, Voice: req.Voice, Source: "user"}); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleTakeOver(w http.ResponseWriter, r *http.Request) {
	if err := s.rt.TakeOver(r.Context(), userFrom(r)); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleHandBack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	if err := s.rt.HandBack(r.Context(), userFrom(r), req.Note); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if err := s.rt.Stop(r.Context(), userFrom(r)); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleSurface(w http.ResponseWriter, r *http.Request) {
	var req agent.Surface
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.rt.SetSurface(r.Context(), userFrom(r), req); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
	if err := s.rt.Compact(r.Context(), userFrom(r)); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, s.rt.View(r.Context(), userFrom(r).ID))
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if err := s.rt.Reset(r.Context(), userFrom(r)); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------- reports ----------

func (s *Server) handleListReports(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListReports(r.Context(), userFrom(r).ID, 200)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Report{}
	}
	writeJSON(w, list)
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	rep, err := s.st.GetReport(r.Context(), userFrom(r).ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "report not found")
		return
	}
	writeJSON(w, rep)
}

func (s *Server) handleDeleteReport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	ok, err := s.st.DeleteReport(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 404, "report not found")
		return
	}
	if v := s.rt.View(r.Context(), u.ID); v.Surface.Kind == "report" && v.Surface.ID == r.PathValue("id") {
		_ = s.rt.SetSurface(r.Context(), u, agent.Surface{})
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleReportDownload saves a report as a markdown or HTML file.
func (s *Server) handleReportDownload(w http.ResponseWriter, r *http.Request) {
	rep, err := s.st.GetReport(r.Context(), userFrom(r).ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "report not found")
		return
	}
	ext, ct, body := ".md", "text/markdown; charset=utf-8", "# "+rep.Title+"\n\n"+rep.Content+"\n"
	if rep.Format == "html" {
		ext, ct, body = ".html", "text/html; charset=utf-8", rep.Content
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(rep.Title)+ext+`"`)
	_, _ = io.WriteString(w, body)
}

func (s *Server) handleICS(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rep, err := s.st.GetReport(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "report not found")
		return
	}
	var data agent.ReportData
	_ = json.Unmarshal(rep.Data, &data)
	if len(data.Events) == 0 {
		writeErr(w, 404, "no events")
		return
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		loc = time.UTC
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(rep.Title)+`.ics"`)
	_, _ = io.WriteString(w, buildICS(data.Events, rep.ID, loc))
}

// ---------- live browser view / takeover ----------

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	tab, err := s.rt.Tab(r.Context(), u, true)
	if err != nil || tab == nil {
		writeErr(w, 404, "browser unavailable")
		return
	}
	c, err := websocket.Accept(w, r, s.acceptOpts())
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	frames, stop := tab.Subscribe()
	defer stop()
	url, title := tab.URL()
	cx, cy := tab.Cursor()
	hello, _ := json.Marshal(map[string]any{"type": "hello", "width": s.cfg.ViewportWidth, "height": s.cfg.ViewportHeight, "url": url, "title": title, "cursor": map[string]float64{"x": cx, "y": cy}})
	if err := c.Write(ctx, websocket.MessageText, hello); err != nil {
		return
	}
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var ev browser.HumanInput
			if json.Unmarshal(data, &ev) != nil {
				continue
			}
			if !s.rt.InControl(u.ID) {
				continue // the agent is driving; ignore stray input
			}
			if err := tab.Human(ev); err != nil {
				slog.Debug("human input failed", "err", err)
			}
		}
	}()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case f, ok := <-frames:
			if !ok {
				return
			}
			wctx, wc := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageBinary, f)
			wc()
			if err != nil {
				return
			}
		case <-ping.C:
			if err := c.Ping(ctx); err != nil {
				return
			}
		}
	}
}

// ---------- events websocket ----------

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	c, err := websocket.Accept(w, r, s.acceptOpts())
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events, setVisible, unsub := s.hub.Subscribe(u.ID)
	defer unsub()
	// Clients say when they go to the background, so Alexa's messages are
	// pushed to the user's phone while they're not looking.
	go func() {
		defer cancel()
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m struct {
				Type    string `json:"type"`
				Visible bool   `json:"visible"`
			}
			if json.Unmarshal(msg, &m) == nil && m.Type == "visibility" {
				setVisible(m.Visible)
			}
		}
	}()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello"}`))
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			wctx, wc := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageText, ev)
			wc()
			if err != nil {
				return
			}
		case <-ping.C:
			if err := c.Ping(ctx); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleVoice(w http.ResponseWriter, r *http.Request) {
	s.voice.ServeRealtime(w, r)
}

func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil || len(data) < 44 {
		writeErr(w, 400, "send a WAV file as the request body")
		return
	}
	text, err := s.voice.Transcribe(r.Context(), data)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, map[string]string{"text": text})
}

// ---------- files ----------

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	env, err := s.envs.Ensure(u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "missing file")
		return
	}
	defer f.Close()
	base, ext := safeName(strings.TrimSuffix(hdr.Filename, filepath.Ext(hdr.Filename))), strings.ToLower(filepath.Ext(hdr.Filename))
	dir := filepath.Join(env.Uploads, time.Now().Format("2006-01-02"))
	_ = env.MkdirAll(dir)
	// A file keeps its name, which a site Alexa uploads it to sees too; a
	// number is added when that day already has a file by that name.
	var full string
	var out *os.File
	for i := 1; ; i++ {
		full = filepath.Join(dir, base+ext)
		if i > 1 {
			full = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		}
		out, err = os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if !errors.Is(err, fs.ErrExist) || i == 1000 {
			break
		}
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	n, err := io.Copy(out, f)
	out.Close()
	if err != nil {
		os.Remove(full)
		writeErr(w, 400, "upload failed (max 100 MB)")
		return
	}
	env.Chown(full)
	ct := hdr.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		if m := mime.TypeByExtension(filepath.Ext(full)); m != "" {
			ct = m
		}
	}
	writeJSON(w, agent.Attachment{Name: hdr.Filename, Path: env.Rel(full), Mime: ct, Size: n})
}

type fileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	env, err := s.envs.Ensure(userFrom(r).ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var out []fileInfo
	_ = filepath.WalkDir(env.Workspace, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == env.Workspace {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) > 2000 {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, fileInfo{Path: env.Rel(p), Size: info.Size(), ModTime: info.ModTime().UnixMilli(), IsDir: d.IsDir()})
		return nil
	})
	if out == nil {
		out = []fileInfo{}
	}
	writeJSON(w, out)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	env := s.envs.For(userFrom(r).ID)
	full, err := env.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		writeErr(w, 404, "file not found")
		return
	}
	disp := "attachment"
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("inline") == "1" {
		disp = "inline"
		// Never render user content as active HTML on our origin.
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.Header().Set("Content-Disposition", disp+`; filename="`+strings.ReplaceAll(filepath.Base(full), `"`, "")+`"`)
	http.ServeFile(w, r, full)
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	env := s.envs.For(userFrom(r).ID)
	full, err := env.Resolve(r.URL.Query().Get("path"))
	if err != nil || full == env.Workspace {
		writeErr(w, 400, "invalid path")
		return
	}
	if err := os.RemoveAll(full); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------- memories ----------

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	m, err := s.st.Memories(r.Context(), userFrom(r).ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if m == nil {
		m = []*store.Memory{}
	}
	writeJSON(w, m)
}

func (s *Server) handleAddMemory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if !readJSON(w, r, &req) || strings.TrimSpace(req.Content) == "" {
		if strings.TrimSpace(req.Content) == "" {
			writeErr(w, 400, "empty memory")
		}
		return
	}
	m, err := s.st.AddMemory(r.Context(), userFrom(r).ID, strings.TrimSpace(req.Content))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rt.MemoryChanged(userFrom(r).ID)
	writeJSON(w, m)
}

func (s *Server) handleUpdateMemory(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Content string `json:"content"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.st.UpdateMemory(r.Context(), userFrom(r).ID, id, strings.TrimSpace(req.Content)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rt.MemoryChanged(userFrom(r).ID)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := s.st.DeleteMemory(r.Context(), userFrom(r).ID, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rt.MemoryChanged(userFrom(r).ID)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------- schedules ----------

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
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if _, err := s.st.DeleteSchedule(r.Context(), userFrom(r).ID, r.PathValue("id")); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rt.Scheduler().Reload(r.Context())
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	sc, err := s.st.GetSchedule(r.Context(), r.PathValue("id"))
	if err != nil || sc.UserID != userFrom(r).ID {
		writeErr(w, 404, "schedule not found")
		return
	}
	s.rt.Scheduler().RunNow(sc.ID)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------- notifications ----------

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.Notifications(r.Context(), userFrom(r).ID, 50)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Notification{}
	}
	writeJSON(w, list)
}

func (s *Server) handleNotificationsRead(w http.ResponseWriter, r *http.Request) {
	_ = s.st.MarkNotificationsRead(r.Context(), userFrom(r).ID)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"key": s.notifier.VAPIDPublicKey()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil {
		writeErr(w, 400, "bad body")
		return
	}
	var sub struct {
		Endpoint string `json:"endpoint"`
	}
	if json.Unmarshal(body, &sub) != nil || !strings.HasPrefix(sub.Endpoint, "https://") {
		writeErr(w, 400, "invalid subscription")
		return
	}
	if err := s.st.SavePushSub(r.Context(), userFrom(r).ID, sub.Endpoint, string(body)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------- static SPA ----------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if f, err := s.static.Open(p); err == nil {
		info, _ := f.Stat()
		f.Close()
		if info != nil && !info.IsDir() {
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, s.static, p)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, s.static, "index.html")
}
