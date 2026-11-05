package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"

	"github.com/google/uuid"

	"alexagent/internal/agent"
	"alexagent/internal/store"
)

const feedPage = 80

// handleSession returns everything the app needs to draw the session: its
// state, the latest feed and the report on screen, if any. seq and epoch
// say which event it is current to: the client applies the events it
// buffered meanwhile only if their seq is greater.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	// The cursor is read first: every event up to it was published after
	// its change was stored, so the reads below include it.
	seq, epoch := s.hub.Cursor(u.ID)
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
	out := map[string]any{"state": view, "feed": feed, "hasMore": more, "seq": seq, "epoch": epoch}
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
	// ClientID is the id the client made for this message. It comes back in
	// the entry's data, so the client can match the echo to the bubble it
	// already shows, and sending it again (a double tap, a retry after a
	// dropped connection) returns the first entry instead of sending twice.
	ClientID string `json:"clientId"`
	// Via, Tap and Values say how the app made the message when the user
	// didn't type it: a tapped card button, a submitted form (WP-03, see
	// Runtime.reply).
	Via    string         `json:"via"`
	Tap    *agent.Tap     `json:"tap"`
	Values map[string]any `json:"values"`
	// AskEntry is the entry of a skipped question this answers late.
	AskEntry int64 `json:"askEntry"`
}

// message is what the request sends into the session.
func (req *messageReq) message(att []agent.Attachment, clientID string) agent.Message {
	return agent.Message{Text: req.Text, Attach: att, Voice: req.Voice, Source: "user", ClientID: clientID, Via: req.Via, Tap: req.Tap, Values: req.Values, AskEntry: req.AskEntry}
}

// handleMessage sends the user's message and answers with its entry.
func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	var req messageReq
	if !readJSON(w, r, &req) {
		return
	}
	u := userFrom(r)
	id := req.ClientID
	if id == "" {
		// Older clients send none; one made here still finds the entry.
		id = uuid.NewString()
	} else if !clientIDRe.MatchString(id) {
		writeErr(w, 400, "bad clientId")
		return
	}
	att, err := s.checkAttachments(u, req.Attachments)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	e, err := s.sendOnce(r.Context(), u.ID, id, func() error {
		return s.rt.Send(r.Context(), u, req.message(att, id))
	})
	if err != nil {
		sessionErr(w, err)
		return
	}
	out := map[string]any{"ok": true}
	if e != nil {
		out["entry"] = e
	}
	writeJSON(w, out)
}

// handleInterrupt is "send now" while Alexa works: the message is sent as
// handleMessage sends it, and the step she's on is cut short, so she turns
// to it at once instead of after her batch (Runtime.Interrupt). It takes
// the same body and gives the same answer.
func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	var req messageReq
	if !readJSON(w, r, &req) {
		return
	}
	u := userFrom(r)
	id := req.ClientID
	if id == "" {
		id = uuid.NewString()
	} else if !clientIDRe.MatchString(id) {
		writeErr(w, 400, "bad clientId")
		return
	}
	att, err := s.checkAttachments(u, req.Attachments)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	e, err := s.sendOnce(r.Context(), u.ID, id, func() error {
		return s.rt.Interrupt(r.Context(), u, req.message(att, id))
	})
	if err != nil {
		sessionErr(w, err)
		return
	}
	out := map[string]any{"ok": true}
	if e != nil {
		out["entry"] = e
	}
	writeJSON(w, out)
}

var clientIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// sentWindow is how many of the latest entries are searched for a message
// sent before with the same clientId. A retry comes within seconds; one
// after this many newer entries is sent again.
const sentWindow = 200

// sending holds the messages being sent right now, so a second request
// with the same clientId waits for the first and then finds its entry
// instead of sending again. The key includes the server because tests run
// several, each with its own user ids.
var sending = struct {
	sync.Mutex
	m map[sendKey]chan struct{}
}{m: map[sendKey]chan struct{}{}}

type sendKey struct {
	srv    *Server
	userID int64
	id     string
}

// sendOnce runs send at most once per clientId and returns the entry of
// the message sent with it.
func (s *Server) sendOnce(ctx context.Context, userID int64, clientID string, send func() error) (*store.Entry, error) {
	k := sendKey{s, userID, clientID}
	for {
		sending.Lock()
		busy, ok := sending.m[k]
		if !ok {
			sending.m[k] = make(chan struct{})
			sending.Unlock()
			break
		}
		sending.Unlock()
		select {
		case <-busy:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() {
		sending.Lock()
		close(sending.m[k])
		delete(sending.m, k)
		sending.Unlock()
	}()
	// Runtime.Send goes on if the client goes away (a reload, a closed
	// tab), so the lookup that keeps a retry from sending twice must not
	// fail for it either.
	ctx = context.WithoutCancel(ctx)
	if e := s.sentEntry(ctx, userID, clientID); e != nil {
		return e, nil
	}
	if err := send(); err != nil {
		return nil, err
	}
	return s.sentEntry(ctx, userID, clientID), nil
}

// sentEntry finds the user's message sent with clientID among the latest
// entries.
func (s *Server) sentEntry(ctx context.Context, userID int64, clientID string) *store.Entry {
	feed, err := s.st.Feed(ctx, userID, 0, sentWindow)
	if err != nil {
		return nil
	}
	for i := len(feed) - 1; i >= 0; i-- {
		e := feed[i]
		if e.Kind != "user" || !bytes.Contains(e.Data, []byte(clientID)) {
			continue
		}
		var d struct {
			ClientID string `json:"clientId"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.ClientID == clientID {
			return e
		}
	}
	return nil
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

// handleRetry is the error card's Try again: Alexa picks the failed run up
// where it stopped (WP-15). The tap shows in the conversation as the
// user's message, like any other.
func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	if err := s.rt.Retry(r.Context(), userFrom(r)); err != nil {
		if errors.Is(err, agent.ErrNothingToRetry) {
			writeErr(w, 409, err.Error())
			return
		}
		sessionErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleRepair is "Fix conversation": the pictures and documents the model
// couldn't read are set aside, and a run that failed over them goes on
// (WP-17).
func (s *Server) handleRepair(w http.ResponseWriter, r *http.Request) {
	names, err := s.rt.Repair(r.Context(), userFrom(r))
	if err != nil {
		sessionErr(w, err)
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, map[string]any{"ok": true, "setAside": names})
}
