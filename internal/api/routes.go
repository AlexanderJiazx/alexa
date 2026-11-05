// Package api exposes the HTTP and WebSocket API and serves the frontend.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
	mux.HandleFunc("POST /api/interrupt", s.auth(s.handleInterrupt))
	mux.HandleFunc("POST /api/takeover", s.auth(s.handleTakeOver))
	mux.HandleFunc("POST /api/handback", s.auth(s.handleHandBack))
	mux.HandleFunc("POST /api/stop", s.auth(s.handleStop))
	mux.HandleFunc("POST /api/retry", s.auth(s.handleRetry))
	mux.HandleFunc("POST /api/repair", s.auth(s.handleRepair))
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
	mux.HandleFunc("GET /api/entries/{id}/calendar.ics", s.auth(s.handleEntryICS))
	mux.HandleFunc("GET /api/favicon/{host}", s.auth(s.handleFavicon))
	mux.HandleFunc("GET /api/img/{token}", s.auth(s.handleImage))
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
	mux.HandleFunc("POST /api/memories/{id}/restore", s.auth(s.handleRestoreMemory))
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
