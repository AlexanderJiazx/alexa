package api

import (
	"io"
	"net/http"
)

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
