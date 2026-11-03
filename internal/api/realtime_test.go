package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"alexagent/internal/hub"
	"alexagent/internal/store"
)

// ---------- event socket helpers ----------

type wsEvent struct {
	Type  string          `json:"type"`
	Seq   int64           `json:"seq"`
	Epoch string          `json:"epoch"`
	Data  json.RawMessage `json:"data"`
}

// dial opens the event socket with the client's cookie.
func (c *client) dial(after string) *websocket.Conn {
	c.t.Helper()
	path := "/api/ws"
	if after != "" {
		path += "?after=" + after
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.base, "http")+path, &websocket.DialOptions{HTTPClient: c.hc})
	if err != nil {
		c.t.Fatalf("dial %s: %v", path, err)
	}
	ws.SetReadLimit(8 << 20)
	c.t.Cleanup(func() { ws.CloseNow() })
	return ws
}

func readEv(t *testing.T, ws *websocket.Conn) wsEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	var ev wsEvent
	if err := json.Unmarshal(b, &ev); err != nil {
		t.Fatalf("bad event %s: %v", b, err)
	}
	return ev
}

// readUntil reads events until one matches.
func readUntil(t *testing.T, ws *websocket.Conn, match func(wsEvent) bool) wsEvent {
	t.Helper()
	for {
		if ev := readEv(t, ws); match(ev) {
			return ev
		}
	}
}

func sendJSON(t *testing.T, ws *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// closedWith waits for the socket to close and returns its close code.
func closedWith(t *testing.T, ws *websocket.Conn, within time.Duration) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		_, _, err := ws.Read(ctx)
		if err == nil {
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("socket still open after %v", within)
		}
		return websocket.CloseStatus(err)
	}
}

type snapshotCursor struct {
	Seq   int64  `json:"seq"`
	Epoch string `json:"epoch"`
}

func (c *client) cursor() snapshotCursor {
	c.t.Helper()
	_, raw := c.raw("GET", "/api/session", nil, false)
	var s snapshotCursor
	if err := json.Unmarshal(raw, &s); err != nil {
		c.t.Fatal(err)
	}
	return s
}

func entryOf(t *testing.T, raw json.RawMessage) entry {
	t.Helper()
	var e entry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("bad entry %s: %v", raw, err)
	}
	return e
}

func clientIDOf(e entry) string {
	var d struct {
		ClientID string `json:"clientId"`
	}
	_ = json.Unmarshal(e.Data, &d)
	return d.ClientID
}

// shortPings speeds up the liveness checks for one test.
func shortPings(t *testing.T, every, wait time.Duration) {
	p, w := pingEvery, pongWait
	pingEvery, pongWait = every, wait
	t.Cleanup(func() { pingEvery, pongWait = p, w })
}

// ---------- tests ----------

// A socket that drops during work gets every missed event replayed, in
// order, when it reconnects with its cursor: no snapshot needed.
func TestEventReplay(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")

	ws := alice.dial("")
	hello := readEv(t, ws)
	if hello.Type != "hello" || hello.Epoch == "" {
		t.Fatalf("hello = %+v", hello)
	}
	if cur := alice.cursor(); cur.Epoch != hello.Epoch || cur.Seq != hello.Seq {
		t.Fatalf("snapshot cursor %+v, hello %+v", cur, hello)
	}
	ws.Close(websocket.StatusNormalClosure, "")

	// The phone is away while Alexa works.
	e.model.script(reply(100, fn("say", map[string]any{"text": "Hi Alice"})), reply(100))
	alice.send("hello")
	alice.wait("idle")
	cur := alice.cursor()
	if cur.Seq <= hello.Seq {
		t.Fatalf("snapshot seq %d didn't move past %d", cur.Seq, hello.Seq)
	}

	ws = alice.dial(fmt.Sprintf("%s:%d", hello.Epoch, hello.Seq))
	again := readEv(t, ws)
	if again.Type != "hello" || again.Epoch != hello.Epoch || again.Seq < cur.Seq {
		t.Fatalf("second hello = %+v, snapshot at %d", again, cur.Seq)
	}
	var kinds []string
	for want := hello.Seq + 1; want <= again.Seq; want++ {
		ev := readEv(t, ws)
		if ev.Type == "resync" {
			t.Fatal("resync instead of replay")
		}
		if ev.Seq != want || ev.Epoch != hello.Epoch {
			t.Fatalf("replayed seq %d (%s), want %d", ev.Seq, ev.Epoch, want)
		}
		if ev.Type == "entry" {
			en := entryOf(t, ev.Data)
			kinds = append(kinds, en.Kind+":"+en.Text)
		}
	}
	if got := strings.Join(kinds, "|"); !strings.Contains(got, "user:hello") || !strings.Contains(got, "say:Hi Alice") {
		t.Errorf("replayed entries = %s", got)
	}

	// Live events continue right after the replay.
	if code, _, _ := alice.do("PUT", "/api/surface", map[string]any{"kind": ""}, true); code != 200 {
		t.Fatalf("PUT /api/surface = %d", code)
	}
	if ev := readEv(t, ws); ev.Type != "state" || ev.Seq != again.Seq+1 {
		t.Errorf("live event = %s seq %d, want state %d", ev.Type, ev.Seq, again.Seq+1)
	}
}

// A cursor from another server start (or one the server can't honour)
// gets resync, and the client reloads the snapshot.
func TestEventResync(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	cur := alice.cursor()

	for _, after := range []string{"0123456789ab:3", fmt.Sprintf("%s:%d", cur.Epoch, cur.Seq+50), "garbage"} {
		ws := alice.dial(after)
		if h := readEv(t, ws); h.Type != "hello" {
			t.Fatalf("%s: first message %s", after, h.Type)
		}
		if ev := readEv(t, ws); ev.Type != "resync" || ev.Epoch != cur.Epoch {
			t.Errorf("%s: got %+v, want resync", after, ev)
		}
		ws.CloseNow()
	}

	// Without a cursor the client loads a snapshot anyway: no resync, the
	// next message is a live event.
	ws := alice.dial("")
	readEv(t, ws)
	alice.do("PUT", "/api/surface", map[string]any{"kind": ""}, true)
	if ev := readEv(t, ws); ev.Type != "state" {
		t.Errorf("after hello: %s", ev.Type)
	}
}

// Sending the same clientId twice, even at the same moment, makes one
// entry; the reply and the socket's echo carry it.
func TestSendIdempotent(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	ws := alice.dial("")
	readEv(t, ws)

	e.model.script(reply(100, fn("say", map[string]any{"text": "On it"})), reply(100))
	var wg sync.WaitGroup
	ids := make([]int64, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, raw := alice.raw("POST", "/api/messages", map[string]any{"text": "book the table", "clientId": "c-1"}, true)
			var out struct {
				OK    bool  `json:"ok"`
				Entry entry `json:"entry"`
			}
			if code != 200 || json.Unmarshal(raw, &out) != nil || !out.OK {
				t.Errorf("send #%d: %d %s", i, code, raw)
				return
			}
			if out.Entry.Kind != "user" || out.Entry.Text != "book the table" || clientIDOf(out.Entry) != "c-1" {
				t.Errorf("send #%d entry = %+v", i, out.Entry)
			}
			ids[i] = out.Entry.ID
		}()
	}
	wg.Wait()
	if ids[0] == 0 || ids[0] != ids[1] {
		t.Fatalf("entry ids %v", ids)
	}
	echo := readUntil(t, ws, func(ev wsEvent) bool { return ev.Type == "entry" && entryOf(t, ev.Data).Kind == "user" })
	if en := entryOf(t, echo.Data); en.ID != ids[0] || clientIDOf(en) != "c-1" {
		t.Errorf("echo = %+v", en)
	}
	alice.wait("idle")

	// A retry after the fact: the same entry, and no second model call.
	code, raw := alice.raw("POST", "/api/messages", map[string]any{"text": "book the table", "clientId": "c-1"}, true)
	var out struct {
		Entry entry `json:"entry"`
	}
	_ = json.Unmarshal(raw, &out)
	if code != 200 || out.Entry.ID != ids[0] {
		t.Errorf("retry: %d %s", code, raw)
	}
	n := 0
	for _, en := range alice.session().Feed {
		if en.Kind == "user" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d user entries, want 1", n)
	}

	// Older clients send no clientId and still get their entry back.
	e.model.script(reply(100, fn("say", map[string]any{"text": "Sure"})), reply(100))
	code, raw = alice.raw("POST", "/api/messages", map[string]any{"text": "and a cab"}, true)
	_ = json.Unmarshal(raw, &out)
	if code != 200 || out.Entry.Text != "and a cab" || clientIDOf(out.Entry) == "" {
		t.Errorf("no clientId: %d %s", code, raw)
	}
	alice.wait("idle")

	if code, _ := alice.raw("POST", "/api/messages", map[string]any{"text": "x", "clientId": "has spaces"}, true); code != 400 {
		t.Errorf("bad clientId = %d", code)
	}
}

// A send whose client went away (a reload, a closed tab) still reaches
// Alexa whole: cut off after its entry was stored, it would never reach
// her, and a retry would find the entry and report it sent. A retry whose
// client went away too finds the entry instead of sending it twice.
func TestSendOutlivesItsRequest(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	u, err := e.st.GetUserByName(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	e.model.script(reply(100, fn("say", map[string]any{"text": "Booked for 8.", "final": true})), reply(100))
	for _, path := range []string{"/api/messages", "/api/interrupt"} {
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, u))
		cancel()
		req := httptest.NewRequestWithContext(ctx, "POST", path, strings.NewReader(`{"text":"Book the table","clientId":"c-gone"}`))
		rec := httptest.NewRecorder()
		if path == "/api/messages" {
			e.api.handleMessage(rec, req)
		} else {
			// The retry, sent the other way: it finds the entry.
			e.api.handleInterrupt(rec, req)
		}
		if rec.Code != 200 {
			t.Fatalf("%s with the client gone: %d %s", path, rec.Code, rec.Body)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := alice.session()
		if sy := last(s.Feed, "say"); sy != nil && s.State.Status == "idle" {
			if sy.Text != "Booked for 8." {
				t.Errorf("say = %+v", sy)
			}
			n := 0
			for _, en := range s.Feed {
				if en.Kind == "user" {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d user entries, want 1", n)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Alexa never got the message: %s, feed %+v", s.State.Status, lastN(s.Feed, 4))
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// Signing out closes the session's sockets everywhere (a copied cookie
// included) within a second; other sessions stay. A session removed from
// the store some other way is caught at the next ping.
func TestRevokeClosesSockets(t *testing.T) {
	shortPings(t, 200*time.Millisecond, time.Second)
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	tablet := e.client(t)
	if code, m, _ := tablet.do("POST", "/api/auth/login", map[string]any{"username": "alice", "password": "password1"}, true); code != 200 {
		t.Fatalf("login: %d %v", code, m)
	}
	phone, laptop, other := alice.dial(""), alice.dial(""), tablet.dial("")
	for _, ws := range []*websocket.Conn{phone, laptop, other} {
		readEv(t, ws)
	}

	start := time.Now()
	if code, _, _ := alice.do("POST", "/api/auth/logout", nil, true); code != 200 {
		t.Fatalf("logout = %d", code)
	}
	for name, ws := range map[string]*websocket.Conn{"phone": phone, "laptop": laptop} {
		if code := closedWith(t, ws, time.Second); code != closeRevoked {
			t.Errorf("%s closed with %d, want %d", name, code, closeRevoked)
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("revoking took %v", d)
	}

	// The tablet's own session is untouched…
	tablet.do("PUT", "/api/surface", map[string]any{"kind": ""}, true)
	if ev := readUntil(t, other, func(ev wsEvent) bool { return ev.Type == "state" }); ev.Seq == 0 {
		t.Error("state event without seq")
	}
	// …until it's deleted behind the socket's back (an admin reset, say):
	// the next ping notices.
	if err := e.st.DeleteOtherSessions(context.Background(), 1, ""); err != nil {
		t.Fatal(err)
	}
	if code := closedWith(t, other, 2*time.Second); code != closeRevoked {
		t.Errorf("tablet closed with %d, want %d", code, closeRevoked)
	}
}

// A phone that goes silent (asleep, off the network) is dropped at the
// next ping, so it stops holding back notifications.
func TestSilentClientStopsCountingOnline(t *testing.T) {
	shortPings(t, 100*time.Millisecond, 100*time.Millisecond)
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")

	// The desk computer answers pings; the phone never reads again, so it
	// never answers one.
	desk := alice.dial("")
	readEv(t, desk)
	go func() {
		for {
			if _, _, err := desk.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	sendJSON(t, desk, map[string]any{"type": "attention", "visible": true, "focused": true})
	phone := alice.dial("")
	sendJSON(t, phone, map[string]any{"type": "attention", "visible": true, "focused": true})
	time.Sleep(500 * time.Millisecond)

	notes := func() int {
		_, _, list := alice.do("GET", "/api/notifications", nil, false)
		return len(list)
	}
	// Someone is at the desk: no notification.
	e.model.script(reply(100, fn("say", map[string]any{"text": "First"})), reply(100))
	alice.send("one")
	alice.wait("idle")
	if n := notes(); n != 0 {
		t.Fatalf("%d notifications while the desk was attended", n)
	}

	// Nobody at the desk now. The phone still claims to be in front of the
	// user, but it was dropped, so Alexa's message becomes a notification.
	sendJSON(t, desk, map[string]any{"type": "attention", "visible": false, "focused": false})
	time.Sleep(100 * time.Millisecond)
	e.model.script(reply(100, fn("say", map[string]any{"text": "Second"})), reply(100))
	alice.send("two")
	alice.wait("idle")
	if n := notes(); n != 1 {
		t.Errorf("%d notifications, want 1: the silent phone still counts as online", n)
	}

	// Older clients only send visibility, and that still counts.
	old := alice.dial("")
	readEv(t, old)
	go func() {
		for {
			if _, _, err := old.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	for i, visible := range []bool{false, true} {
		sendJSON(t, old, map[string]any{"type": "visibility", "visible": visible})
		time.Sleep(100 * time.Millisecond)
		e.model.script(reply(100, fn("say", map[string]any{"text": fmt.Sprint("More ", i)})), reply(100))
		alice.send(fmt.Sprint("more ", i))
		alice.wait("idle")
		if n := notes(); n != 2 {
			t.Errorf("visible=%v: %d notifications, want 2", visible, n)
		}
	}
}

// Changing a password signs the user out everywhere else: those sessions
// are gone and their sockets close at once, while the session that made
// the change keeps working. With no session kept (an admin reset, deleting
// the user) every one goes.
func TestRevokeUser(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "alexa.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{st: st, hub: hub.New()}
	alice, err := st.CreateUser(ctx, "alice", "x", "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.CreateUser(ctx, "bob", "x", "Bob", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"here", "phone"} {
		if err := st.CreateSession(ctx, tok, alice.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CreateSession(ctx, "bob", bob.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	here, phone := s.hub.Subscribe(alice.ID, "here", ""), s.hub.Subscribe(alice.ID, "phone", "")
	live, other := s.hub.Attach(alice.ID, "phone"), s.hub.Subscribe(bob.ID, "bob", "")

	if err := s.revokeUser(ctx, alice.ID, "here"); err != nil {
		t.Fatal(err)
	}
	signedIn := func(tok string) bool {
		_, err := st.SessionUser(ctx, tok)
		return err == nil
	}
	if !signedIn("here") || signedIn("phone") || !signedIn("bob") {
		t.Errorf("sessions after a password change: here %v, phone %v, bob %v", signedIn("here"), signedIn("phone"), signedIn("bob"))
	}
	for name, c := range map[string]*hub.Conn{"phone": phone.Conn, "live view": live} {
		select {
		case <-c.Done():
			if c.Err() != hub.ErrRevoked {
				t.Errorf("%s ended with %v", name, c.Err())
			}
		default:
			t.Errorf("%s still open", name)
		}
	}
	for name, c := range map[string]*hub.Conn{"here": here.Conn, "bob": other.Conn} {
		select {
		case <-c.Done():
			t.Errorf("%s closed: %v", name, c.Err())
		default:
		}
	}

	if err := s.revokeUser(ctx, alice.ID, ""); err != nil {
		t.Fatal(err)
	}
	if signedIn("here") {
		t.Error("kept session survived revoking everything")
	}
	select {
	case <-here.Done():
	default:
		t.Error("here still open")
	}
}
