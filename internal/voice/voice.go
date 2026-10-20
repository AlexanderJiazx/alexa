// Package voice proxies live speech-to-text to Meta's realtime ASR and
// offers a batch transcription fallback.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type Service struct {
	BaseURL string // https://api.meta.ai/v1
	APIKey  string
	Model   string
}

func (s *Service) realtimeURL() string {
	u := strings.Replace(s.BaseURL, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	return u + "/asr/realtime"
}

type clientConfig struct {
	Type     string   `json:"type"`
	Mode     string   `json:"mode"`
	Keywords []string `json:"keywords"`
	Language []string `json:"languages"`
}

// ServeRealtime bridges a client websocket to the upstream ASR socket.
// Client protocol: first a JSON text frame {"type":"start","mode":"ENDPOINTING"},
// then binary frames of 16-bit LE mono PCM at 24 kHz, then {"type":"end"}.
// Server events from upstream are forwarded verbatim as JSON text frames,
// plus {"type":"ready"} after the handshake and {"type":"closed"} at the end.
func (s *Service) ServeRealtime(w http.ResponseWriter, r *http.Request) {
	client, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer client.CloseNow()
	client.SetReadLimit(1 << 20)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	typ, data, err := client.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		return
	}
	var cfg clientConfig
	_ = json.Unmarshal(data, &cfg)
	mode := cfg.Mode
	if mode != "PUSH_TO_TALK" && mode != "ENDPOINTING" {
		mode = "ENDPOINTING"
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	up, _, err := websocket.Dial(dialCtx, s.realtimeURL(), nil)
	dialCancel()
	if err != nil {
		slog.Warn("asr dial failed", "err", err)
		writeJSON(ctx, client, map[string]any{"type": "error", "message": "speech service unavailable"})
		return
	}
	defer up.CloseNow()
	up.SetReadLimit(1 << 20)
	hs := map[string]any{
		"authorization":     map[string]string{"accessToken": "Bearer " + s.APIKey},
		"audioEncoding":     "PCM_24KHZ",
		"model":             s.Model,
		"mode":              mode,
		"partialMode":       "CUMULATIVE",
		"emitAudioProgress": false,
	}
	if len(cfg.Keywords) > 0 {
		hs["keywords"] = cfg.Keywords
	}
	if len(cfg.Language) > 0 {
		hs["languageBias"] = cfg.Language
	}
	if err := writeJSON(ctx, up, hs); err != nil {
		writeJSON(ctx, client, map[string]any{"type": "error", "message": "speech handshake failed"})
		return
	}
	_, ack, err := up.Read(ctx)
	if err != nil {
		writeJSON(ctx, client, map[string]any{"type": "error", "message": "speech handshake failed: " + err.Error()})
		return
	}
	var ackMsg map[string]any
	_ = json.Unmarshal(ack, &ackMsg)
	if _, ok := ackMsg["sessionId"]; !ok {
		writeJSON(ctx, client, map[string]any{"type": "error", "message": fmt.Sprintf("speech handshake rejected: %s", string(ack))})
		return
	}
	writeJSON(ctx, client, map[string]any{"type": "ready", "sessionId": ackMsg["sessionId"]})

	// upstream -> client
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			typ, msg, err := up.Read(ctx)
			if err != nil {
				code := websocket.CloseStatus(err)
				writeJSON(ctx, client, map[string]any{"type": "closed", "code": int(code)})
				return
			}
			if typ == websocket.MessageText {
				_ = client.Write(ctx, websocket.MessageText, msg)
			}
		}
	}()

	// client -> upstream
	ended := false
	for {
		typ, msg, err := client.Read(ctx)
		if err != nil {
			if !ended {
				_ = writeJSON(ctx, up, map[string]string{"type": "endStream"})
			}
			break
		}
		if typ == websocket.MessageBinary {
			if ended {
				continue
			}
			if err := up.Write(ctx, websocket.MessageBinary, msg); err != nil {
				break
			}
			continue
		}
		var m struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(msg, &m)
		if m.Type == "end" && !ended {
			ended = true
			_ = writeJSON(ctx, up, map[string]string{"type": "endStream"})
		}
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
	client.Close(websocket.StatusNormalClosure, "")
}

func writeJSON(ctx context.Context, c *websocket.Conn, v any) error {
	b, _ := json.Marshal(v)
	return c.Write(ctx, websocket.MessageText, b)
}

// Transcribe sends a WAV recording to the batch endpoint and returns the text.
func (s *Service) Transcribe(ctx context.Context, wav []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="request"`)
	h.Set("Content-Type", "application/json")
	part, _ := mw.CreatePart(h)
	cfg, _ := json.Marshal(map[string]string{"model": s.Model, "audioEncoding": "WAV", "mode": "PUSH_TO_TALK"})
	part.Write(cfg)
	fh := textproto.MIMEHeader{}
	fh.Set("Content-Disposition", `form-data; name="audio"; filename="audio.wav"`)
	fh.Set("Content-Type", "audio/wav")
	fp, _ := mw.CreatePart(fh)
	fp.Write(wav)
	mw.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/asr/transcribe", &body)
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("transcribe: %s %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var out struct {
		Transcript string `json:"transcript"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Transcript), nil
}
