package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestConcurrentWrites reproduces the agent loop, the API and the hub all
// writing at once; it must never surface SQLITE_BUSY.
func TestConcurrentWrites(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	var users []*User
	for i := 0; i < 4; i++ {
		u, err := s.CreateUser(ctx, fmt.Sprintf("u%d", i), "x", "U", i == 0)
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, u)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 5000)
	for g := 0; g < 12; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			uid := users[g%len(users)].ID
			for i := 0; i < 40; i++ {
				item, _ := json.Marshal(map[string]any{"g": g, "i": i})
				if err := s.AppendItems(ctx, uid, []json.RawMessage{item}); err != nil {
					errs <- err
				}
				if err := s.AddInbox(ctx, uid, InboxMessage{Kind: "user", Text: "hi"}); err != nil {
					errs <- err
				}
				if _, err := s.TakeInbox(ctx, uid); err != nil {
					errs <- err
				}
				e := &Entry{UserID: uid, Kind: "step", Text: "x", Status: "running"}
				if err := s.AddEntry(ctx, e); err != nil {
					errs <- err
				}
				e.Status = "done"
				if err := s.UpdateEntry(ctx, e); err != nil {
					errs <- err
				}
				if err := s.SetSessionActivity(ctx, uid, "y"); err != nil {
					errs <- err
				}
				if err := s.AddSessionUsage(ctx, uid, 10, 1); err != nil {
					errs <- err
				}
				if _, err := s.Items(ctx, uid, 0); err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// Three goroutines wrote to each user.
	items, _ := s.Items(ctx, users[1].ID, 0)
	if len(items) != 120 {
		t.Fatalf("items = %d, want 120", len(items))
	}
	if n, _ := s.InboxCount(ctx, users[1].ID); n != 0 {
		t.Fatalf("inbox left with %d messages", n)
	}
	ss, _ := s.GetSession(ctx, users[1].ID)
	if ss.InputTokens != 1200 || ss.ContextTokens != 10 {
		t.Fatalf("usage in=%d context=%d, want 1200 and 10", ss.InputTokens, ss.ContextTokens)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", true)
	ss, err := s.GetSession(ctx, u.ID)
	if err != nil || ss.Status != "idle" {
		t.Fatalf("new session: %+v %v", ss, err)
	}
	var ids []int64
	for i := 0; i < 5; i++ {
		item, _ := json.Marshal(map[string]any{"i": i})
		_ = s.AppendItems(ctx, u.ID, []json.RawMessage{item})
	}
	rows, _ := s.Items(ctx, u.ID, 0)
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	// Compaction drops what's folded into the summary from the replay.
	if err := s.CompactSession(ctx, u.ID, "the summary", ids[2]); err != nil {
		t.Fatal(err)
	}
	ss, _ = s.GetSession(ctx, u.ID)
	if ss.Summary != "the summary" || ss.CompactedThrough != ids[2] || ss.Compactions != 1 || ss.ContextTokens != 0 {
		t.Fatalf("after compaction: %+v", ss)
	}
	rest, _ := s.Items(ctx, u.ID, ss.CompactedThrough)
	if len(rest) != 2 {
		t.Fatalf("items after compaction = %d, want 2", len(rest))
	}
	// The feed pages backwards in ascending order.
	for i := 0; i < 10; i++ {
		_ = s.AddEntry(ctx, &Entry{UserID: u.ID, Kind: "say", Text: fmt.Sprint(i)})
	}
	page, _ := s.Feed(ctx, u.ID, 0, 4)
	if len(page) != 4 || page[0].Text != "6" || page[3].Text != "9" {
		t.Fatalf("latest page = %v", texts(page))
	}
	older, _ := s.Feed(ctx, u.ID, page[0].ID, 4)
	if len(older) != 4 || older[0].Text != "2" || older[3].Text != "5" {
		t.Fatalf("older page = %v", texts(older))
	}
	// Reset forgets the conversation but keeps the user.
	_ = s.AddInbox(ctx, u.ID, InboxMessage{Text: "pending"})
	if err := s.ResetSession(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.Items(ctx, u.ID, 0); len(rows) != 0 {
		t.Fatal("items survived reset")
	}
	if f, _ := s.Feed(ctx, u.ID, 0, 10); len(f) != 0 {
		t.Fatal("feed survived reset")
	}
	if n, _ := s.InboxCount(ctx, u.ID); n != 0 {
		t.Fatal("inbox survived reset")
	}
	if ss, _ := s.GetSession(ctx, u.ID); ss.Summary != "" || ss.Compactions != 0 {
		t.Fatalf("session state survived reset: %+v", ss)
	}
}

func texts(es []*Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Text)
	}
	return out
}

func TestUserIsolation(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, "a", "x", "A", true)
	b, _ := s.CreateUser(ctx, "b", "x", "B", false)
	if _, err := s.AddMemory(ctx, a.ID, "a likes tea"); err != nil {
		t.Fatal(err)
	}
	if mb, _ := s.Memories(ctx, b.ID); len(mb) != 0 {
		t.Fatalf("user b sees %d memories of user a", len(mb))
	}
	ma, _ := s.Memories(ctx, a.ID)
	if ok, _ := s.DeleteMemory(ctx, b.ID, ma[0].ID); ok {
		t.Fatal("user b deleted user a's memory")
	}

	// Reports.
	if err := s.SaveReport(ctx, &Report{ID: "rep1", UserID: a.ID, Title: "secret", Format: "markdown", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReport(ctx, b.ID, "rep1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("user b read user a's report: %v", err)
	}
	if list, _ := s.ListReports(ctx, b.ID, 10); len(list) != 0 {
		t.Fatal("user b lists user a's reports")
	}
	if err := s.SaveReport(ctx, &Report{ID: "rep1", UserID: b.ID, Title: "hijack", Format: "markdown", Content: "y"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("user b overwrote user a's report: %v", err)
	}
	if ok, _ := s.DeleteReport(ctx, b.ID, "rep1"); ok {
		t.Fatal("user b deleted user a's report")
	}
	if r, err := s.GetReport(ctx, a.ID, "rep1"); err != nil || r.Title != "secret" {
		t.Fatalf("user a's report changed: %+v %v", r, err)
	}

	// Conversation and feed.
	item, _ := json.Marshal(map[string]any{"role": "user"})
	_ = s.AppendItems(ctx, a.ID, []json.RawMessage{item})
	e := &Entry{UserID: a.ID, Kind: "ask", Text: "secret question", Status: "open"}
	_ = s.AddEntry(ctx, e)
	_ = s.AddInbox(ctx, a.ID, InboxMessage{Text: "for a"})
	if rows, _ := s.Items(ctx, b.ID, 0); len(rows) != 0 {
		t.Fatal("user b sees user a's conversation")
	}
	if f, _ := s.Feed(ctx, b.ID, 0, 10); len(f) != 0 {
		t.Fatal("user b sees user a's feed")
	}
	if msgs, _ := s.TakeInbox(ctx, b.ID); len(msgs) != 0 {
		t.Fatal("user b took user a's inbox")
	}
	_ = s.SetEntryStatus(ctx, b.ID, e.ID, "answered")
	if f, _ := s.Feed(ctx, a.ID, 0, 10); f[0].Status != "open" {
		t.Fatal("user b changed user a's feed entry")
	}
	if err := s.ResetSession(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.Items(ctx, a.ID, 0); len(rows) != 1 {
		t.Fatal("resetting user b's session touched user a's")
	}
}
