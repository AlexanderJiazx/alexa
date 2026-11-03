package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
				if _, err := s.MoveInbox(ctx, uid, nil, func(ms []InboxMessage) []json.RawMessage { return nil }); err != nil {
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
	if err := s.CompactSession(ctx, u.ID, "the summary", 0, ids[2], ids[4], Kept{}); err != nil {
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
	if msgs, _ := s.MoveInbox(ctx, b.ID, nil, func(ms []InboxMessage) []json.RawMessage { return nil }); len(msgs) != 0 {
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

// Deleting a user drops their kv entries ("plan:<id>" and the like), and
// only theirs: user 12's go, user 112's and the server's own stay.
func TestDeleteUserDropsTheirKV(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	var ids []int64
	for i := 0; i < 2; i++ {
		u, err := s.CreateUser(ctx, fmt.Sprintf("u%d", i), "x", "U", false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID)
	}
	gone, kept := ids[0], ids[1]
	keys := map[string]bool{
		fmt.Sprintf("plan:%d", gone):  false,
		fmt.Sprintf("prune:%d", gone): false,
		fmt.Sprintf("plan:%d", kept):  true,
		fmt.Sprintf("plan:%d1", gone): true, // another user whose id starts the same
		"vapid_public":                true,
	}
	for k := range keys {
		if err := s.SetKV(ctx, k, "v"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteUser(ctx, gone); err != nil {
		t.Fatal(err)
	}
	for k, stays := range keys {
		v, _ := s.GetKV(ctx, k)
		if (v != "") != stays {
			t.Errorf("after deleting user %d, %s = %q", gone, k, v)
		}
	}
}

// A listing of reports carries what their cards preview (D4): the
// takeaway, the preview and the first item of the data, and the start of
// a markdown report's content. A page's content, the blocks and the other
// items stay with the report itself.
func TestListReportsCarriesTheirPreview(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", false)
	long := "# Lisbon\n\nThree days, two neighbourhoods. " + strings.Repeat("More. ", 200)
	data := `{"takeaway":"Casa Alfama is the pick.","preview":{"kinds":"2 tables"},"items":[{"label":"Casa Alfama"},{"label":"Lisbon Loft"}],"blocks":[{"type":"stats","items":[]}],"sources":[{"title":"x","url":"https://x.example/"}]}`
	if err := s.SaveReport(ctx, &Report{ID: "md", UserID: u.ID, Title: "Lisbon", Format: "markdown", Content: long, Data: json.RawMessage(data)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReport(ctx, &Report{ID: "page", UserID: u.ID, Title: "Map", Format: "html", Content: "<!doctype html><p>A page</p>"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListReports(ctx, u.ID, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	by := map[string]*Report{}
	for _, r := range list {
		by[r.ID] = r
	}
	md := by["md"]
	if !strings.HasPrefix(long, md.Content) || !strings.HasPrefix(md.Content, "# Lisbon\n\nThree days, two neighbourhoods.") || len([]rune(md.Content)) != 600 {
		t.Errorf("the markdown report's content is listed as %d characters: %.60q", len([]rune(md.Content)), md.Content)
	}
	var d map[string]any
	if err := json.Unmarshal(md.Data, &d); err != nil {
		t.Fatalf("data = %s: %v", md.Data, err)
	}
	if d["takeaway"] != "Casa Alfama is the pick." || d["preview"] == nil || d["blocks"] != nil || d["sources"] != nil {
		t.Errorf("listed data = %s", md.Data)
	}
	if items, _ := d["items"].([]any); len(items) != 1 {
		t.Errorf("listed items = %v, want only the first", d["items"])
	}
	if page := by["page"]; page.Content != "" || page.Data != nil {
		t.Errorf("the page is listed with content %q and data %s", page.Content, page.Data)
	}
	// The report itself still has it all.
	if r, _ := s.GetReport(ctx, u.ID, "md"); r.Content != long || !strings.Contains(string(r.Data), `"blocks"`) {
		t.Errorf("the stored report lost its content or data: %d characters, %s", len(r.Content), r.Data)
	}
}

func TestMarkNotificationRead(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", false)
	for _, ref := range []string{"entry:7", "entry:8", "report:3"} {
		if _, err := s.AddNotification(ctx, u.ID, ref, "t", "b"); err != nil {
			t.Fatal(err)
		}
	}
	if read, err := s.MarkNotificationRead(ctx, u.ID, "entry:7"); err != nil || !read {
		t.Fatalf("first mark: %v, %v", read, err)
	}
	if read, err := s.MarkNotificationRead(ctx, u.ID, "entry:7"); err != nil || read {
		t.Fatalf("second mark: %v, %v (there was nothing left to read)", read, err)
	}
	list, _ := s.Notifications(ctx, u.ID, 10)
	for _, n := range list {
		if n.Read != (n.Ref == "entry:7") {
			t.Errorf("%s read = %v", n.Ref, n.Read)
		}
	}
}

// Rewriting an entry that's gone says so, so nothing is published for it.
func TestUpdateEntryGone(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", true)
	e := &Entry{UserID: u.ID, Kind: "user", Text: "hi", Data: json.RawMessage(`{"start":true,"topic":"pending"}`)}
	if err := s.AddEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if left, err := s.PendingTopics(ctx); err != nil || len(left) != 1 || left[0].ID != e.ID {
		t.Fatalf("PendingTopics = %v, %v", left, err)
	}
	if err := s.UpdateEntry(ctx, e); err != nil {
		t.Fatalf("UpdateEntry = %v", err)
	}
	if err := s.ResetSession(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateEntry(ctx, e); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEntry on a cleared feed = %v, want ErrNotFound", err)
	}
}

// The topic call's answer changes only the topic fields of the message's
// entry: what was written to it meanwhile stays. It settles once.
func TestSettleTopic(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a", "x", "A", true)
	e := &Entry{UserID: u.ID, Kind: "user", Text: "and tomorrow?", Data: json.RawMessage(`{"start":true,"topic":"pending","clientId":"c1"}`)}
	if err := s.AddEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.PendingTopics(ctx); len(left) != 1 || left[0].ID != e.ID {
		t.Fatalf("PendingTopics = %+v", left)
	}
	if _, err := s.SettleTopic(ctx, u.ID+1, e.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("settling another user's entry = %v, want ErrNotFound", err)
	}
	if err := s.SetEntryStatus(ctx, u.ID, e.ID, "answered"); err != nil {
		t.Fatal(err)
	}
	got, err := s.SettleTopic(ctx, u.ID, e.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	_ = json.Unmarshal(got.Data, &d)
	if got.Status != "answered" || got.Text != "and tomorrow?" || d["topic"] != nil || d["followUp"] != true || d["start"] != true || d["clientId"] != "c1" {
		t.Fatalf("settled entry = %+v, data %v", got, d)
	}
	if left, _ := s.PendingTopics(ctx); len(left) != 0 {
		t.Fatalf("still pending: %+v", left)
	}
	if _, err := s.SettleTopic(ctx, u.ID, e.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("settling again = %v, want ErrNotFound", err)
	}
}
