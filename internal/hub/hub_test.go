package hub

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type got struct {
	Seq   int64           `json:"seq"`
	Epoch string          `json:"epoch"`
	Type  string          `json:"type"`
	Data  json.RawMessage `json:"data"`
}

func decode(t *testing.T, b []byte) got {
	t.Helper()
	var g got
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("bad envelope %s: %v", b, err)
	}
	return g
}

// next reads the subscriber's next event.
func next(t *testing.T, s *Sub) got {
	t.Helper()
	select {
	case b := <-s.Events():
		return decode(t, b)
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	return got{}
}

func publishN(h *Hub, userID int64, from, to int) {
	for i := from; i <= to; i++ {
		h.Publish(userID, Event{Type: "entry", Data: map[string]int{"n": i}})
	}
}

func TestEnvelopeAndSeq(t *testing.T) {
	h := New()
	s := h.Subscribe(1, "tok", "")
	defer s.Close()
	other := h.Subscribe(2, "tok2", "")
	defer other.Close()

	h.Publish(1, Event{Type: "reset"})
	h.Publish(2, Event{Type: "entry", Data: map[string]string{"text": "bob's"}})
	h.Publish(1, Event{Type: "entry", Data: map[string]string{"text": `say "hi"`}})

	a, b := next(t, s), next(t, s)
	if a.Seq != 1 || a.Type != "reset" || a.Data != nil || a.Epoch != h.Epoch() || a.Epoch == "" {
		t.Errorf("first event = %+v", a)
	}
	if b.Seq != 2 || b.Type != "entry" || string(b.Data) != `{"text":"say \"hi\""}` {
		t.Errorf("second event = %+v (%s)", b, b.Data)
	}
	// Each user has their own counter.
	if g := next(t, other); g.Seq != 1 {
		t.Errorf("bob's first seq = %d", g.Seq)
	}
	if seq, epoch := h.Cursor(1); seq != 2 || epoch != h.Epoch() {
		t.Errorf("cursor = %d %q", seq, epoch)
	}
	if seq, _ := h.Cursor(99); seq != 0 {
		t.Errorf("cursor of a quiet user = %d", seq)
	}
}

// A client that reconnects with its cursor gets exactly what it missed,
// then the live events, with no gap and nothing twice.
func TestReplayAfterReconnect(t *testing.T) {
	h := New()
	s := h.Subscribe(1, "tok", "")
	publishN(h, 1, 1, 3)
	var last got
	for range 3 {
		last = next(t, s)
	}
	s.Close()

	publishN(h, 1, 4, 10) // while the phone was away
	s = h.Subscribe(1, "tok", fmt.Sprintf("%s:%d", last.Epoch, last.Seq))
	defer s.Close()
	if s.Resync || s.Seq != 10 || len(s.Missed) != 7 {
		t.Fatalf("resync=%v seq=%d missed=%d", s.Resync, s.Seq, len(s.Missed))
	}
	for i, b := range s.Missed {
		if g := decode(t, b); g.Seq != int64(4+i) {
			t.Errorf("missed[%d] has seq %d", i, g.Seq)
		}
	}
	publishN(h, 1, 11, 11)
	if g := next(t, s); g.Seq != 11 {
		t.Errorf("live event after replay has seq %d", g.Seq)
	}

	// Up to date: nothing to replay.
	up := h.Subscribe(1, "tok", fmt.Sprintf("%s:%d", h.Epoch(), 11))
	defer up.Close()
	if up.Resync || len(up.Missed) != 0 || up.Seq != 11 {
		t.Errorf("up to date: resync=%v missed=%d seq=%d", up.Resync, len(up.Missed), up.Seq)
	}
	// A brand-new user at seq 0.
	fresh := h.Subscribe(7, "tok7", h.Epoch()+":0")
	defer fresh.Close()
	if fresh.Resync || len(fresh.Missed) != 0 {
		t.Errorf("new user: resync=%v missed=%d", fresh.Resync, len(fresh.Missed))
	}
}

func TestResync(t *testing.T) {
	h := New()
	publishN(h, 1, 1, 5)
	restarted := New() // a server restart: a new epoch, seq from zero
	if restarted.Epoch() == h.Epoch() {
		t.Fatal("two hubs share an epoch")
	}
	publishN(restarted, 1, 1, 2)

	cases := map[string]string{
		"another epoch":   h.Epoch() + ":5",
		"seq ahead":       restarted.Epoch() + ":9",
		"garbage":         "nonsense",
		"negative":        restarted.Epoch() + ":-1",
		"empty epoch":     ":1",
		"seq not numeric": restarted.Epoch() + ":x",
	}
	for name, after := range cases {
		s := restarted.Subscribe(1, "tok", after)
		if !s.Resync || len(s.Missed) != 0 {
			t.Errorf("%s: resync=%v missed=%d", name, s.Resync, len(s.Missed))
		}
		s.Close()
	}
	// No cursor: the client loads a snapshot itself, so no resync.
	if s := restarted.Subscribe(1, "tok", ""); s.Resync {
		t.Error("resync without a cursor")
	}
}

// Events beyond the ring (by count or by size) can't be replayed.
func TestRingLimits(t *testing.T) {
	h := New()
	publishN(h, 1, 1, ringSize+10)
	old := h.Subscribe(1, "tok", h.Epoch()+":5")
	if !old.Resync {
		t.Error("resumed from an event the ring no longer holds")
	}
	edge := h.Subscribe(1, "tok", fmt.Sprintf("%s:%d", h.Epoch(), 10)) // 11 is the oldest kept
	if edge.Resync || len(edge.Missed) != ringSize {
		t.Errorf("edge: resync=%v missed=%d", edge.Resync, len(edge.Missed))
	}

	big := strings.Repeat("x", ringBytes/4)
	for range 6 {
		h.Publish(2, Event{Type: "report", Data: map[string]string{"content": big}})
	}
	h.mu.Lock()
	r := h.users[2].ring
	h.mu.Unlock()
	if r.bytes > ringBytes || r.n >= 6 || r.n < 1 {
		t.Errorf("ring holds %d events, %d bytes", r.n, r.bytes)
	}
	if s := h.Subscribe(2, "tok", h.Epoch()+":0"); !s.Resync {
		t.Error("resumed past events dropped for size")
	}
	// A single event over the cap is still kept, alone.
	h.Publish(3, Event{Type: "report", Data: map[string]string{"content": strings.Repeat("y", ringBytes+1)}})
	if s := h.Subscribe(3, "tok", h.Epoch()+":0"); s.Resync || len(s.Missed) != 1 {
		t.Errorf("huge event: resync=%v missed=%d", s.Resync, len(s.Missed))
	}
}

// A client that stops reading is cut off (and can resume from the ring)
// instead of silently missing events.
func TestSlowClientIsDisconnected(t *testing.T) {
	h := New()
	slow := h.Subscribe(1, "tok", "")
	ok := h.Subscribe(1, "tok", "")
	defer ok.Close()
	// The reading client empties its buffer before the event that overflows
	// the other's. Read concurrently, a busy machine could let it fall
	// behind too, and the read would then wait forever.
	publishN(h, 1, 1, subBuffer)
	for range subBuffer {
		<-ok.Events()
	}
	publishN(h, 1, subBuffer+1, subBuffer+1)
	<-ok.Events()
	select {
	case <-slow.Done():
	default:
		t.Fatal("slow client still subscribed")
	}
	if slow.Err() != ErrLagged {
		t.Errorf("err = %v", slow.Err())
	}
	select {
	case <-ok.Done():
		t.Fatal("reading client was cut off")
	default:
	}
	// It resumes from what it last got.
	r := h.Subscribe(1, "tok", fmt.Sprintf("%s:%d", h.Epoch(), subBuffer))
	if r.Resync || len(r.Missed) != 1 {
		t.Errorf("resume: resync=%v missed=%d", r.Resync, len(r.Missed))
	}
	slow.Close() // closing after the hub ended it is harmless
}

func closed(c *Conn) bool {
	select {
	case <-c.Done():
		return true
	default:
		return false
	}
}

func TestRevoke(t *testing.T) {
	h := New()
	phone := h.Subscribe(1, "a", "")
	laptop := h.Subscribe(1, "a", "") // the same session (a copied cookie)
	live := h.Attach(1, "a")
	tablet := h.Subscribe(1, "b", "")
	bob := h.Subscribe(2, "c", "")

	h.RevokeSession("a")
	for name, c := range map[string]*Conn{"phone": phone.Conn, "laptop": laptop.Conn, "live": live} {
		if !closed(c) || c.Err() != ErrRevoked {
			t.Errorf("%s: closed=%v err=%v", name, closed(c), c.Err())
		}
	}
	if closed(tablet.Conn) || closed(bob.Conn) {
		t.Fatal("revoking one session closed another")
	}
	h.Publish(1, Event{Type: "entry"}) // no send to a revoked subscriber
	if g := next(t, tablet); g.Seq != 1 {
		t.Errorf("tablet got seq %d", g.Seq)
	}
	if len(phone.Events()) != 0 {
		t.Error("revoked subscriber still gets events")
	}

	// A password change keeps the current session and closes the rest.
	keep := h.Subscribe(1, "d", "")
	h.RevokeUser(1, "d")
	if !closed(tablet.Conn) || closed(keep.Conn) || closed(bob.Conn) {
		t.Errorf("RevokeUser keep: tablet=%v keep=%v bob=%v", closed(tablet.Conn), closed(keep.Conn), closed(bob.Conn))
	}
	h.RevokeUser(1, "")
	if !closed(keep.Conn) || closed(bob.Conn) {
		t.Errorf("RevokeUser all: keep=%v bob=%v", closed(keep.Conn), closed(bob.Conn))
	}
	h.mu.Lock()
	n, subs := len(h.conns), len(h.users[1].subs)
	h.mu.Unlock()
	if n != 1 || subs != 0 {
		t.Errorf("registry keeps %d sessions and %d of the user's subscribers", n, subs)
	}
	if h.Online(1) {
		t.Error("revoked clients count as online")
	}
	bob.Close()
	h.RevokeSession("") // no session: nothing to do
}

func TestOnline(t *testing.T) {
	h := New()
	if h.Online(1) {
		t.Fatal("online with no clients")
	}
	s := h.Subscribe(1, "tok", "")
	if !h.Online(1) {
		t.Fatal("a new client starts out on screen")
	}
	// Older clients only say whether the page is visible.
	s.SetVisible(false)
	if h.Online(1) {
		t.Error("online with the page hidden")
	}
	s.SetVisible(true)
	if !h.Online(1) {
		t.Error("offline with the page visible")
	}
	// attention also needs focus…
	s.SetAttention(true, false)
	if h.Online(1) {
		t.Error("online while the window isn't focused")
	}
	s.SetAttention(true, true)
	if !h.Online(1) {
		t.Error("offline while visible and focused")
	}
	// …and counts for attentionTTL: a tab left open in front of nobody
	// stops holding back pushes.
	h.mu.Lock()
	s.heard = time.Now().Add(-attentionTTL - time.Second)
	h.mu.Unlock()
	if h.Online(1) {
		t.Error("stale attention still counts")
	}
	s.SetAttention(true, true)
	other := h.Subscribe(1, "tok", "")
	other.SetAttention(false, false)
	if !h.Online(1) {
		t.Error("one attentive client of two should be enough")
	}
	s.Close()
	if h.Online(1) {
		t.Error("online after the attentive client left")
	}
	other.Close()
}

// PublishOrdered publishes snapshots in the order they were read, so a
// stale one never lands last.
func TestPublishOrdered(t *testing.T) {
	h := New()
	s := h.Subscribe(1, "tok", "")
	defer s.Close()
	var mu sync.Mutex
	reads := 0
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.PublishOrdered(1, "state", func() any {
				mu.Lock()
				defer mu.Unlock()
				reads++
				return map[string]int{"read": reads}
			})
		}()
	}
	wg.Wait()
	prev := 0
	for range 50 {
		g := next(t, s)
		var d struct{ Read int }
		_ = json.Unmarshal(g.Data, &d)
		if d.Read <= prev {
			t.Fatalf("snapshot read #%d published after #%d", d.Read, prev)
		}
		prev = d.Read
	}
}

func TestChanged(t *testing.T) {
	h := New()
	s := h.Subscribe(1, "tok", "")
	defer s.Close()
	h.Publish(1, Changed("memory"))
	h.Publish(1, ChangedItem("reportDeleted", "r1"))
	if g := next(t, s); g.Type != "changed" || string(g.Data) != `{"what":"memory"}` {
		t.Errorf("changed = %s %s", g.Type, g.Data)
	}
	if g := next(t, s); string(g.Data) != `{"what":"reportDeleted","id":"r1"}` {
		t.Errorf("reportDeleted = %s", g.Data)
	}
}

// Publishers, subscribers and revocations at once; run with -race.
func TestConcurrent(t *testing.T) {
	h := New()
	var wg sync.WaitGroup
	for u := range int64(3) {
		wg.Add(2)
		go func() {
			defer wg.Done()
			publishN(h, u, 1, 300)
		}()
		go func() {
			defer wg.Done()
			for i := range 30 {
				s := h.Subscribe(u, fmt.Sprintf("tok%d", i%3), "")
				s.SetAttention(true, i%2 == 0)
				_ = h.Online(u)
				for len(s.Events()) > 0 {
					<-s.Events()
				}
				if i%5 == 0 {
					h.RevokeSession(fmt.Sprintf("tok%d", i%3))
				}
				s.Close()
			}
		}()
	}
	wg.Wait()
	for u := range int64(3) {
		if seq, _ := h.Cursor(u); seq != 300 {
			t.Errorf("user %d at seq %d", u, seq)
		}
	}
}

// Forgetting a deleted user closes their sockets, live views included, and
// drops the events kept for them; other users are untouched (BH-5).
func TestForget(t *testing.T) {
	h := New()
	a := h.Subscribe(1, "a", "")
	view := h.Attach(1, "a")
	b := h.Subscribe(2, "b", "")
	h.Publish(1, Event{Type: "x"})
	h.Forget(1)
	if !closed(a.Conn) || !closed(view) || closed(b.Conn) {
		t.Errorf("after Forget: a=%v view=%v b=%v", closed(a.Conn), closed(view), closed(b.Conn))
	}
	if a.Err() != ErrRevoked {
		t.Errorf("a's socket ended with %v", a.Err())
	}
	h.mu.Lock()
	_, kept := h.users[1]
	h.mu.Unlock()
	if kept || h.Online(1) {
		t.Errorf("the user is still kept (%v) or online", kept)
	}
	b.Close()
}
