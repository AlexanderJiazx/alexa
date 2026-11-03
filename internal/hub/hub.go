// Package hub fans out per-user realtime events to connected clients.
//
// Every event a user's clients see carries a sequence number (one counter
// per user, across every event type) and the epoch of this server start, so
// a client can tell what it has missed. The hub keeps the user's latest
// events, and a client that reconnects with its last epoch and seq gets
// what it missed replayed instead of reloading the whole session. The hub
// is also the registry of open sockets by session, so signing out closes
// them.
package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is what publishers hand the hub. Clients receive it as
// {seq, epoch, type, data}.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Change is the data of a "changed" event: something outside the
// conversation (memory, schedules, logins, files, the profile) changed, so
// whatever shows it can fetch it again. ID names the one item, when it
// matters (reportDeleted carries the report's id).
type Change struct {
	What string `json:"what"`
	ID   any    `json:"id,omitempty"`
}

// Changed is the event that tells the user's other screens that one kind
// of data changed.
func Changed(what string) Event { return Event{Type: "changed", Data: Change{What: what}} }

// ChangedItem is Changed for one item, such as the report that was
// deleted.
func ChangedItem(what string, id any) Event {
	return Event{Type: "changed", Data: Change{What: what, ID: id}}
}

const (
	// ringSize events are kept per user for replay. A client that was away
	// for longer than that many events reloads the session instead.
	ringSize = 512
	// ringBytes caps the memory the kept events can take per user (reports
	// travel in full), dropping the oldest first.
	ringBytes = 4 << 20
	// subBuffer events can wait for a slow client. A client that falls
	// further behind is disconnected and catches up from the ring when it
	// reconnects, instead of silently missing events.
	subBuffer = 256
	// attentionTTL is how long an attention report counts. Clients that
	// send attention repeat it while someone is there, so a tab left open in
	// front of nobody stops holding back push notifications.
	attentionTTL = 2 * time.Minute
)

// Why a socket's registration ended.
var (
	// ErrRevoked: the session was signed out, its password changed or its
	// user deleted. Clients go back to sign-in.
	ErrRevoked = errors.New("session revoked")
	// ErrLagged: the client fell behind; it should reconnect at once and
	// resume from its last seq.
	ErrLagged = errors.New("client fell behind")
	// ErrClosed: the socket closed itself.
	ErrClosed = errors.New("closed")
)

type Hub struct {
	epoch string

	mu    sync.Mutex
	users map[int64]*user
	conns map[string]map[*Conn]struct{} // open sockets by session token hash
}

// user is one user's event stream.
type user struct {
	seq  int64
	ring ring
	subs map[*Sub]struct{}
	// order serializes PublishOrdered, so a snapshot read later is never
	// published earlier.
	order sync.Mutex
}

func New() *Hub {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return &Hub{epoch: hex.EncodeToString(b), users: map[int64]*user{}, conns: map[string]map[*Conn]struct{}{}}
}

// Epoch names this server start. Sequence numbers restart with it.
func (h *Hub) Epoch() string { return h.epoch }

// Cursor returns the user's latest seq and the epoch. A snapshot read after
// Cursor reflects every event up to seq; events after it may be newer.
func (h *Hub) Cursor(userID int64) (seq int64, epoch string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if u := h.users[userID]; u != nil {
		seq = u.seq
	}
	return seq, h.epoch
}

func (h *Hub) user(userID int64) *user {
	u := h.users[userID]
	if u == nil {
		u = &user{subs: map[*Sub]struct{}{}}
		h.users[userID] = u
	}
	return u
}

// Publish stamps the event with the user's next seq, keeps it for replay
// and sends it to every connected client of the user.
func (h *Hub) Publish(userID int64, ev Event) {
	typ, err := json.Marshal(ev.Type)
	if err != nil {
		return
	}
	var data []byte
	if ev.Data != nil {
		if data, err = json.Marshal(ev.Data); err != nil {
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.user(userID)
	u.seq++
	b := envelope(u.seq, h.epoch, typ, data)
	u.ring.push(u.seq, b)
	for s := range u.subs {
		select {
		case s.ch <- b:
		default:
			h.end(s.Conn, ErrLagged)
		}
	}
}

// PublishOrdered reads the event's data and publishes it as one step per
// user. Of two concurrent calls, the one that reads later publishes later,
// so a stale snapshot (of the session state, say) never lands after a
// fresh one. read must not publish through PublishOrdered itself.
func (h *Hub) PublishOrdered(userID int64, typ string, read func() any) {
	h.mu.Lock()
	u := h.user(userID)
	h.mu.Unlock()
	u.order.Lock()
	defer u.order.Unlock()
	h.Publish(userID, Event{Type: typ, Data: read()})
}

// envelope builds {"seq":…,"epoch":…,"type":…,"data":…} around data that is
// already JSON, without encoding it again under the lock.
func envelope(seq int64, epoch string, typ, data []byte) []byte {
	b := make([]byte, 0, len(data)+len(typ)+len(epoch)+48)
	b = append(b, `{"seq":`...)
	b = strconv.AppendInt(b, seq, 10)
	b = append(b, `,"epoch":"`...)
	b = append(b, epoch...)
	b = append(b, `","type":`...)
	b = append(b, typ...)
	if data != nil {
		b = append(b, `,"data":`...)
		b = append(b, data...)
	}
	return append(b, '}')
}

// ring is a user's latest events, oldest first.
type ring struct {
	buf   [ringSize]record
	start int
	n     int
	bytes int
}

type record struct {
	seq int64
	b   []byte
}

func (r *ring) push(seq int64, b []byte) {
	if r.n == ringSize {
		r.drop()
	}
	r.buf[(r.start+r.n)%ringSize] = record{seq, b}
	r.n++
	r.bytes += len(b)
	for r.bytes > ringBytes && r.n > 1 {
		r.drop()
	}
}

func (r *ring) drop() {
	r.bytes -= len(r.buf[r.start].b)
	r.buf[r.start] = record{}
	r.start = (r.start + 1) % ringSize
	r.n--
}

// since returns the kept events after seq, or false if some of them are no
// longer kept.
func (r *ring) since(seq, latest int64) ([][]byte, bool) {
	if seq == latest {
		return nil, true
	}
	if r.n == 0 || r.buf[r.start].seq > seq+1 {
		return nil, false
	}
	var out [][]byte
	for i := range r.n {
		if rec := r.buf[(r.start+i)%ringSize]; rec.seq > seq {
			out = append(out, rec.b)
		}
	}
	return out, true
}

// ParseCursor reads a client's "<epoch>:<seq>".
func ParseCursor(s string) (epoch string, seq int64, ok bool) {
	e, n, found := strings.Cut(s, ":")
	if !found || e == "" {
		return "", 0, false
	}
	seq, err := strconv.ParseInt(n, 10, 64)
	if err != nil || seq < 0 {
		return "", 0, false
	}
	return e, seq, true
}

// Conn is one open socket, registered under the session it was opened
// with, so revoking the session closes it.
type Conn struct {
	h       *Hub
	userID  int64
	session string
	sub     *Sub // set for event subscriptions
	done    chan struct{}
	err     error // why done was closed; guarded by h.mu
}

// Attach registers a socket that isn't an event subscription (the live
// browser view), so revoking its session closes it too.
func (h *Hub) Attach(userID int64, session string) *Conn {
	c := &Conn{h: h, userID: userID, session: session, done: make(chan struct{})}
	h.mu.Lock()
	h.register(c)
	h.mu.Unlock()
	return c
}

func (h *Hub) register(c *Conn) {
	if h.conns[c.session] == nil {
		h.conns[c.session] = map[*Conn]struct{}{}
	}
	h.conns[c.session][c] = struct{}{}
}

// end closes c's registration with a reason, once. h.mu must be held.
func (h *Hub) end(c *Conn, err error) {
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
	if set := h.conns[c.session]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, c.session)
		}
	}
	if c.sub != nil {
		if u := h.users[c.userID]; u != nil {
			delete(u.subs, c.sub)
		}
	}
}

// Done is closed when the socket should close: its session was revoked or
// the client fell behind. Err says which.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err says why Done was closed: ErrRevoked, ErrLagged or ErrClosed.
func (c *Conn) Err() error {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	return c.err
}

// Close unregisters the socket.
func (c *Conn) Close() {
	c.h.mu.Lock()
	c.h.end(c, ErrClosed)
	c.h.mu.Unlock()
}

// RevokeSession closes every socket opened with the session (a token
// hash). Call it once the session is gone from the store.
func (h *Hub) RevokeSession(session string) {
	if session == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns[session] {
		h.end(c, ErrRevoked)
	}
}

// RevokeUser closes every socket of the user except those opened with the
// session keep ("" closes them all): after a password change, a password
// reset or deleting the user.
func (h *Hub) RevokeUser(userID int64, keep string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for session, set := range h.conns {
		if session == keep && keep != "" {
			continue
		}
		for c := range set {
			if c.userID == userID {
				h.end(c, ErrRevoked)
			}
		}
	}
}

// Forget drops a deleted user's event stream: their open sockets close
// (ErrRevoked) and the events kept for them to catch up on go.
func (h *Hub) Forget(userID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.conns {
		for c := range set {
			if c.userID == userID {
				h.end(c, ErrRevoked)
			}
		}
	}
	delete(h.users, userID)
}

// Sub is a client's event subscription.
type Sub struct {
	*Conn
	// Seq is the user's latest seq when the client subscribed: Missed holds
	// the events up to it, and Events everything after it.
	Seq   int64
	Epoch string
	// Missed is what the client missed since the cursor it gave, oldest
	// first.
	Missed [][]byte
	// Resync is true when the client gave a cursor the hub can't resume
	// from (another epoch, or too long ago): it must reload the session.
	Resync bool

	ch chan []byte
	// What the client last said about the user's attention; guarded by
	// h.mu.
	visible bool
	focused bool
	heard   time.Time // the last attention message; zero for clients that only send visibility
}

// Subscribe registers a client of the user's events, opened with session
// (a token hash). after is the client's cursor, "<epoch>:<seq>" of the last
// event it has, or "" for a client that will load a snapshot. The client
// starts out on screen.
func (h *Hub) Subscribe(userID int64, session, after string) *Sub {
	s := &Sub{Epoch: h.epoch, ch: make(chan []byte, subBuffer), visible: true, focused: true}
	s.Conn = &Conn{h: h, userID: userID, session: session, sub: s, done: make(chan struct{})}
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.user(userID)
	s.Seq = u.seq
	if after != "" {
		epoch, seq, ok := ParseCursor(after)
		if ok && epoch == h.epoch && seq <= u.seq {
			s.Missed, ok = u.ring.since(seq, u.seq)
		} else {
			ok = false
		}
		s.Resync = !ok
	}
	u.subs[s] = struct{}{}
	h.register(s.Conn)
	return s
}

// Events delivers the user's events after Seq.
func (s *Sub) Events() <-chan []byte { return s.ch }

// SetAttention records what the client says about the user: whether the
// page is visible and focused. It counts for attentionTTL, so clients
// repeat it while the user is there.
func (s *Sub) SetAttention(visible, focused bool) {
	s.h.mu.Lock()
	s.visible, s.focused, s.heard = visible, focused, time.Now()
	s.h.mu.Unlock()
}

// SetVisible records whether the page is on screen, for clients that don't
// send attention.
func (s *Sub) SetVisible(visible bool) {
	s.h.mu.Lock()
	s.visible = visible
	s.h.mu.Unlock()
}

// attentive says whether the user is looking at this client. h.mu must be
// held.
func (s *Sub) attentive(now time.Time) bool {
	if s.heard.IsZero() {
		return s.visible
	}
	return s.visible && s.focused && now.Sub(s.heard) < attentionTTL
}

// Online reports whether the user has Alexa open in front of them on some
// device; if not, what Alexa says reaches them as a notification. A client
// whose socket stops answering pings is dropped, so a phone that slept or
// lost its network stops counting.
func (h *Hub) Online(userID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.users[userID]
	if u == nil {
		return false
	}
	now := time.Now()
	for s := range u.subs {
		if s.attentive(now) {
			return true
		}
	}
	return false
}
