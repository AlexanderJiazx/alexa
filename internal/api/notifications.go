package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"alexagent/internal/store"
)

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
	if err := s.st.MarkNotificationsRead(r.Context(), userFrom(r).ID); err == nil {
		s.changed(r, "notifications")
	}
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
