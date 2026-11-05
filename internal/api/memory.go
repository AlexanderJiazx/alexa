package api

import (
	"net/http"
	"strconv"
	"strings"

	"alexagent/internal/store"
)

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

// handleRestoreMemory brings back an entry deleted in the last 30 days.
func (s *Server) handleRestoreMemory(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ok, err := s.st.RestoreMemory(r.Context(), userFrom(r).ID, id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 404, "no deleted memory with that id")
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
