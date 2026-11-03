package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func appendN(t *testing.T, s *Store, userID int64, n int) []int64 {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		item, _ := json.Marshal(map[string]any{"i": i})
		if err := s.AppendItems(ctx, userID, []json.RawMessage{item}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Items(ctx, userID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// A compaction deletes the items its summary covers, and writes only if
// the conversation is still as the summary was made from (BH-2, BH-3).
func TestCompactSessionDeletesWhatItFolds(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", true)
	ids := appendN(t, s, u.ID, 5)

	// Based on an older read: a new item came since, or another
	// compaction, or a reset.
	// What's kept of the folded items is written with them, or not at all.
	kept := Kept{Recall: []RecallDoc{{Ref: "item:1", Kind: "user", ItemID: ids[0], Text: "My sister lives in Porto"}}, KV: map[string]string{"recall:1": "3"}}
	if err := s.CompactSession(ctx, u.ID, "stale", 0, ids[2], ids[3], kept); !errors.Is(err, ErrChanged) {
		t.Fatalf("compaction missing the newest item = %v, want ErrChanged", err)
	}
	if err := s.CompactSession(ctx, u.ID, "stale", ids[0], ids[2], ids[4], kept); !errors.Is(err, ErrChanged) {
		t.Fatalf("compaction from another summary = %v, want ErrChanged", err)
	}
	if rows, _ := s.Items(ctx, u.ID, 0); len(rows) != 5 {
		t.Fatalf("a refused compaction deleted items: %d left", len(rows))
	}
	if ss, _ := s.GetSession(ctx, u.ID); ss.Summary != "" || ss.CompactedThrough != 0 {
		t.Fatalf("a refused compaction wrote %+v", ss)
	}
	if hits, _ := s.SearchRecall(ctx, u.ID, "sister", 0, 0, 5); len(hits) != 0 {
		t.Fatalf("a refused compaction indexed %+v", hits)
	}
	if v, err := s.GetKV(ctx, "recall:1"); err == nil {
		t.Fatalf("a refused compaction wrote kv %q", v)
	}

	if err := s.CompactSession(ctx, u.ID, "first", 0, ids[2], ids[4], kept); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchRecall(ctx, u.ID, "sister", 0, 0, 5); len(hits) != 1 || hits[0].Ref != "item:1" {
		t.Fatalf("the compaction didn't index what it kept: %+v", hits)
	}
	if v, _ := s.GetKV(ctx, "recall:1"); v != "3" {
		t.Fatalf("the compaction didn't write its kv: %q", v)
	}
	rows, _ := s.Items(ctx, u.ID, 0)
	if len(rows) != 2 || rows[0].ID != ids[3] {
		t.Fatalf("after compaction the store holds %v, want only what's after %d", rows, ids[2])
	}
	// What replay reads is unchanged by the delete: the summary, then the
	// items after it.
	ss, conv, err := s.Conversation(ctx, u.ID)
	if err != nil || ss.Summary != "first" || ss.CompactedThrough != ids[2] || len(conv) != 2 || conv[0].ID != ids[3] || conv[1].ID != ids[4] {
		t.Fatalf("Conversation = %+v %v %v", ss, conv, err)
	}

	// The same compaction again finds the conversation changed.
	if err := s.CompactSession(ctx, u.ID, "again", 0, ids[2], ids[4], Kept{}); !errors.Is(err, ErrChanged) {
		t.Fatalf("a second commit of the same compaction = %v, want ErrChanged", err)
	}
	// So does one planned before a reset.
	more := appendN(t, s, u.ID, 1)
	if err := s.ResetSession(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CompactSession(ctx, u.ID, "after reset", ids[2], ids[4], more[len(more)-1], Kept{}); !errors.Is(err, ErrChanged) {
		t.Fatalf("a compaction planned before a reset = %v, want ErrChanged", err)
	}
}

// Reading the session writes nothing, so it never waits for the write
// lock, and a user without a session row reads as idle (BH-6).
func TestGetSessionIsARead(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", false)
	ss, err := s.GetSession(ctx, u.ID)
	if err != nil || ss.Status != "idle" || ss.UserID != u.ID {
		t.Fatalf("new session = %+v, %v", ss, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM session_state WHERE user_id=?`, u.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("reading the session made %d rows (%v)", n, err)
	}
	if err := s.SetSessionActivity(ctx, u.ID, "Thinking"); err != nil {
		t.Fatal(err)
	}

	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE: holds the write lock
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kv(key, value) VALUES('x','y')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan time.Duration, 1)
	go func() {
		t0 := time.Now()
		if ss, err := s.GetSession(ctx, u.ID); err != nil || ss.Activity != "Thinking" {
			t.Errorf("GetSession = %+v, %v", ss, err)
		}
		if _, _, err := s.Conversation(ctx, u.ID); err != nil {
			t.Errorf("Conversation: %v", err)
		}
		done <- time.Since(t0)
	}()
	select {
	case d := <-done:
		if d > time.Second {
			t.Errorf("reading the session took %s while a write was open", d)
		}
	case <-time.After(3 * time.Second):
		t.Error("reading the session waited for the write lock")
	}
	_ = tx.Rollback()
}

// The sweep deletes notifications read over a month ago and expired
// sign-ins, and leaves the rest and the feed alone (BH-3).
func TestPrune(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", false)
	old := time.Now().Add(-NotificationKeep - time.Hour).UnixMilli()
	for _, n := range []struct {
		title string
		read  int
		at    int64
	}{{"old read", 1, old}, {"old unread", 0, old}, {"new read", 1, now()}} {
		if _, err := s.db.Exec(`INSERT INTO notifications(user_id, title, read, created_at) VALUES(?,?,?,?)`, u.ID, n.title, n.read, n.at); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateSession(ctx, "expired", u.ID, -time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "valid", u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	_ = s.AddEntry(ctx, &Entry{UserID: u.ID, Kind: "say", Text: "kept"})

	p, err := s.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Notifications != 1 || p.AuthSessions != 1 {
		t.Errorf("pruned %+v, want one notification and one sign-in", p)
	}
	list, _ := s.Notifications(ctx, u.ID, 10)
	var titles []string
	for _, n := range list {
		titles = append(titles, n.Title)
	}
	if strings.Join(titles, ",") != "new read,old unread" {
		t.Errorf("notifications left: %v", titles)
	}
	if _, err := s.SessionUser(ctx, "valid"); err != nil {
		t.Errorf("a valid sign-in was pruned: %v", err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE token_hash='expired'`).Scan(&n)
	if n != 0 {
		t.Error("an expired sign-in was kept")
	}
	if f, _ := s.Feed(ctx, u.ID, 0, 10); len(f) != 1 {
		t.Errorf("the feed lost entries: %d left", len(f))
	}
}

func TestMentions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, "a", "x", "A", false)
	b, _ := s.CreateUser(ctx, "b", "x", "B", false)
	_ = s.AddEntry(ctx, &Entry{UserID: a.ID, Kind: "say", Text: "x", Data: json.RawMessage(`{"src":"/api/img/feed"}`)})
	_ = s.SaveReport(ctx, &Report{ID: "r1", UserID: b.ID, Title: "t", Content: "![](/api/img/report)"})
	item, _ := json.Marshal(map[string]any{"text": "/api/img/item"})
	_ = s.AppendItems(ctx, a.ID, []json.RawMessage{item})
	_ = s.AddEntry(ctx, &Entry{UserID: a.ID, Kind: "say", Text: "nothing to see"})
	got := map[string]bool{}
	if err := s.Mentions(ctx, "/api/img/", func(id int64, text string) {
		got[fmt.Sprintf("%d:%s", id, text[strings.Index(text, "/api/img/")+9:][:4])] = true
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{fmt.Sprintf("%d:feed", a.ID), fmt.Sprintf("%d:repo", b.ID), fmt.Sprintf("%d:item", a.ID)} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("mentions = %v, want 3", got)
	}
}

// The per-user queries use an index rather than reading every user's
// rows (BH-7).
func TestQueriesUseIndexes(t *testing.T) {
	s := openTest(t)
	for _, q := range []struct {
		table, sql string
	}{
		{"notifications", `SELECT id FROM notifications WHERE user_id=1 ORDER BY id DESC LIMIT 50`},
		{"notifications", `UPDATE notifications SET read=1 WHERE user_id=1 AND task_id='x' AND read=0`},
		{"inbox", `SELECT id FROM inbox WHERE user_id=1 ORDER BY id`},
		{"inbox", `SELECT COUNT(*) FROM inbox WHERE user_id=1 AND kind='user'`},
		{"auth_sessions", `DELETE FROM auth_sessions WHERE user_id=1 AND token_hash<>'x'`},
		{"auth_sessions", `DELETE FROM auth_sessions WHERE expires_at<=1`},
		{"memories", `SELECT id FROM memories WHERE user_id=1 AND deleted_at=0 ORDER BY id`},
		{"schedules", `SELECT id FROM schedules WHERE user_id=1 ORDER BY created_at`},
		{"feed", `UPDATE feed SET status='failed' WHERE user_id=1 AND kind='step' AND status='running'`},
		{"session_items", `DELETE FROM session_items WHERE user_id=1 AND id<=10`},
	} {
		rows, err := s.db.Query(`EXPLAIN QUERY PLAN ` + q.sql)
		if err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		p := strings.Join(plan, "; ")
		if !strings.Contains(p, "SEARCH "+q.table) || strings.Contains(p, "SCAN "+q.table) {
			t.Errorf("%s\n  plan: %s", q.sql, p)
		}
	}
}

// Of people signing up to a new server at once, exactly one becomes the
// admin, and with sign-up closed nobody gets in after them (BH-13).
func TestSignUpFirstIsAdminOnce(t *testing.T) {
	for _, open := range []bool{false, true} {
		s := openTest(t)
		ctx := context.Background()
		const n = 8
		type result struct {
			u   *User
			err error
		}
		res := make(chan result, n)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			go func() {
				<-start
				u, err := s.SignUp(ctx, fmt.Sprintf("user%d", i), "x", "", open)
				res <- result{u, err}
			}()
		}
		close(start)
		admins, made := 0, 0
		for i := 0; i < n; i++ {
			r := <-res
			switch {
			case r.err == nil:
				made++
				if r.u.IsAdmin {
					admins++
				}
			case errors.Is(r.err, ErrSignupClosed) && !open:
			default:
				t.Errorf("open=%v: SignUp: %v", open, r.err)
			}
		}
		want := 1
		if open {
			want = n
		}
		if admins != 1 || made != want {
			t.Errorf("open=%v: %d accounts, %d admins; want %d and 1", open, made, admins, want)
		}
		if c, _ := s.CountUsers(ctx); c != want {
			t.Errorf("open=%v: %d users stored, want %d", open, c, want)
		}
	}
}

// The last user id counts deleted users, and none made after it's read.
func TestLastUserID(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if id, err := s.LastUserID(ctx); err != nil || id != 0 {
		t.Fatalf("no users: %d, %v", id, err)
	}
	a, _ := s.CreateUser(ctx, "a", "x", "A", false)
	b, _ := s.CreateUser(ctx, "b", "x", "B", false)
	if err := s.DeleteUser(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if id, _ := s.LastUserID(ctx); id != b.ID || id <= a.ID {
		t.Errorf("last user id = %d, want %d (deleted, still counted)", id, b.ID)
	}
	c, _ := s.CreateUser(ctx, "c", "x", "C", false)
	if c.ID <= b.ID {
		t.Errorf("a deleted user's id was given again: %d", c.ID)
	}
}
