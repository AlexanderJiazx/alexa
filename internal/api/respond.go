package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"alexagent/internal/agent"
)

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
