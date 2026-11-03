package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"alexagent/internal/browser"
	"alexagent/internal/hub"
	"alexagent/internal/store"
)

// pingEvery and pongWait pace the liveness check on every socket. A peer
// that doesn't answer a ping in time is gone (a phone that slept or changed
// networks), so it stops counting as online and Alexa's messages reach it
// as push notifications instead. Each ping also checks that the socket's
// session still exists. Tests shorten them.
var (
	pingEvery = 25 * time.Second
	pongWait  = 10 * time.Second
)

// Close codes the client acts on.
const (
	// closeRevoked: the session was signed out, its password changed or
	// its user deleted. The client goes back to sign-in.
	closeRevoked websocket.StatusCode = 4401
	// closeLagged: the client fell too far behind. It reconnects at once
	// with its cursor and gets what it missed replayed.
	closeLagged websocket.StatusCode = 4000
)

func (s *Server) acceptOpts() *websocket.AcceptOptions {
	o := &websocket.AcceptOptions{}
	if s.cfg.DevCORSOrigin != "" {
		o.OriginPatterns = []string{strings.TrimPrefix(strings.TrimPrefix(s.cfg.DevCORSOrigin, "http://"), "https://")}
	}
	return o
}

// alive pings the peer and checks the socket's session is still signed in,
// returning false if either fails. A signed-out session is closed with
// closeRevoked, so the client goes back to sign-in instead of retrying.
func (s *Server) alive(ctx context.Context, c *websocket.Conn, session string, wait time.Duration) bool {
	pctx, done := context.WithTimeout(ctx, wait)
	err := c.Ping(pctx)
	done()
	if err != nil {
		return false
	}
	if session == "" {
		return true
	}
	if _, err := s.st.SessionUser(ctx, session); errors.Is(err, store.ErrNotFound) {
		_ = c.Close(closeRevoked, "signed out")
		return false
	}
	return true
}

// closeFor closes a socket whose registration in the hub ended.
func closeFor(c *websocket.Conn, conn *hub.Conn) {
	switch conn.Err() {
	case hub.ErrRevoked:
		_ = c.Close(closeRevoked, "signed out")
	case hub.ErrLagged:
		_ = c.Close(closeLagged, "resync")
	}
}

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
	// Signing out closes the live view too: take-over input from a revoked
	// session must never reach the user's browser.
	session := sessionHash(r)
	conn := s.hub.Attach(u.ID, session)
	defer conn.Close()
	frames, stop := tab.Subscribe()
	defer stop()
	url, title := tab.URL()
	cx, cy := tab.Cursor()
	if err := c.Write(ctx, websocket.MessageText, liveHello(s.cfg.ViewportWidth, s.cfg.ViewportHeight, url, title, cx, cy)); err != nil {
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
			select {
			case <-conn.Done():
				return // revoked: drop input that raced the close
			default:
			}
			if err := tab.Human(ev); err != nil {
				slog.Debug("human input failed", "err", err)
				// A bad address typed in the URL bar is the person's to fix,
				// so say why it didn't open. Writes may run alongside the
				// frame writer.
				if ev.Kind == "navigate" {
					notice, _ := json.Marshal(map[string]string{"type": "notice", "text": err.Error()})
					_ = c.Write(ctx, websocket.MessageText, notice)
				}
			}
		}
	}()
	every, wait := pingEvery, pongWait
	ping := time.NewTicker(every)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-conn.Done():
			closeFor(c, conn)
			return
		case f, ok := <-frames:
			if !ok {
				// The browser ended (it crashed or was closed): the client
				// reconnects at once, which starts it again.
				_ = c.Close(websocket.StatusServiceRestart, "browser restarted")
				return
			}
			wctx, wc := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageBinary, f)
			wc()
			if err != nil {
				return
			}
		case <-ping.C:
			if !s.alive(ctx, c, session, wait) {
				return
			}
		}
	}
}

// liveHello is the live view's first message: the page's size, where it
// is, and where Alexa's pointer was last.
func liveHello(width, height int, url, title string, x, y float64) []byte {
	b, _ := json.Marshal(map[string]any{"type": "hello", "width": width, "height": height, "url": url, "title": title, "cursor": map[string]float64{"x": x, "y": y}})
	return b
}

// clientMsg is what clients send on the event socket: attention (whether
// the user is looking: page visible and focused) and, from older clients,
// visibility.
type clientMsg struct {
	Type    string `json:"type"`
	Visible bool   `json:"visible"`
	Focused bool   `json:"focused"`
}

// handleWS streams the user's events (protocol v2, SYNTHESIS B.5). The
// socket opens with {type:"hello", seq, epoch}: seq is the user's latest
// event when this client subscribed. A client that reconnects with
// ?after=<epoch>:<seq> then gets every event it missed, in order, or
// {type:"resync"} when they're no longer kept (or the server restarted)
// and it must reload the session. Live events follow, each with its seq.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	c, err := websocket.Accept(w, r, s.acceptOpts())
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	session := sessionHash(r)
	sub := s.hub.Subscribe(u.ID, session, r.URL.Query().Get("after"))
	defer sub.Close()
	// Clients say whether the user is looking, so Alexa's messages are
	// pushed to their phone while they're not.
	go func() {
		defer cancel()
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m clientMsg
			if json.Unmarshal(msg, &m) != nil {
				continue
			}
			switch m.Type {
			case "attention":
				sub.SetAttention(m.Visible, m.Focused)
			case "visibility":
				sub.SetVisible(m.Visible)
			}
		}
	}()
	write := func(b []byte) bool {
		wctx, wc := context.WithTimeout(ctx, 10*time.Second)
		defer wc()
		return c.Write(wctx, websocket.MessageText, b) == nil
	}
	hello, _ := json.Marshal(map[string]any{"type": "hello", "seq": sub.Seq, "epoch": sub.Epoch, "data": map[string]any{"seq": sub.Seq, "epoch": sub.Epoch}})
	if !write(hello) {
		return
	}
	if sub.Resync {
		resync, _ := json.Marshal(map[string]any{"type": "resync", "epoch": sub.Epoch})
		if !write(resync) {
			return
		}
	}
	for _, ev := range sub.Missed {
		if !write(ev) {
			return
		}
	}
	every, wait := pingEvery, pongWait
	ping := time.NewTicker(every)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done():
			closeFor(c, sub.Conn)
			return
		case ev := <-sub.Events():
			if !write(ev) {
				return
			}
		case <-ping.C:
			if !s.alive(ctx, c, session, wait) {
				return
			}
		}
	}
}

// changed tells the user's other tabs and devices that something they may
// have open (a sheet, a list) has changed, so they fetch it again.
func (s *Server) changed(r *http.Request, what string) {
	s.hub.Publish(userFrom(r).ID, hub.Changed(what))
}
