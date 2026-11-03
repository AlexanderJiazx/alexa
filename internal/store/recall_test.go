package store

import (
	"context"
	"encoding/json"
	"testing"
)

func refs(docs []RecallDoc) []string {
	var out []string
	for _, d := range docs {
		out = append(out, d.Ref)
	}
	return out
}

func TestRecallSearch(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, "a", "x", "A", false)
	b, _ := s.CreateUser(ctx, "b", "x", "B", false)
	if err := s.IndexRecall(ctx, a.ID, []RecallDoc{
		{Ref: "item:10", Kind: "user", ItemID: 10, At: 1000, Text: "Find me a hotel in Lisbon for the 12th"},
		{Ref: "item:12", Kind: "said", ItemID: 12, At: 2000, Text: "Booked the Memmo Alfama, three nights"},
		{Ref: "report:r1", Kind: "report", At: 3000, Text: "Lisbon options\nHotels near the river"},
	}); err != nil {
		t.Fatal(err)
	}
	_ = s.IndexRecall(ctx, b.ID, []RecallDoc{{Ref: "item:11", Kind: "user", ItemID: 11, At: 1500, Text: "Hotel in Lisbon, please"}})

	hits, err := s.SearchRecall(ctx, a.ID, `"lisbon"`, 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want a's two (never b's)", refs(hits))
	}
	for _, h := range hits {
		if h.Ref == "item:11" {
			t.Fatal("a found b's message")
		}
	}
	if hits, _ := s.SearchRecall(ctx, a.ID, `"hotel"* "lisbon"`, 1500, 0, 10); len(hits) != 1 || hits[0].Ref != "report:r1" {
		t.Fatalf("after 1500: %v", refs(hits))
	}
	if hits, _ := s.SearchRecall(ctx, a.ID, `"lisbon"`, 0, 1500, 10); len(hits) != 1 || hits[0].Ref != "item:10" || hits[0].Text == "" {
		t.Fatalf("before 1500: %+v", hits)
	}
	// Diacritics don't matter.
	if hits, _ := s.SearchRecall(ctx, a.ID, `"álfama"`, 0, 0, 10); len(hits) != 1 {
		t.Fatalf("álfama: %v", refs(hits))
	}

	// Re-indexing a ref replaces it, in the index too.
	_ = s.IndexRecall(ctx, a.ID, []RecallDoc{{Ref: "item:12", Kind: "said", ItemID: 12, At: 2000, Text: "Booked a flat in Porto"}})
	if hits, _ := s.SearchRecall(ctx, a.ID, `"memmo"`, 0, 0, 10); len(hits) != 0 {
		t.Fatalf("the old text is still found: %v", refs(hits))
	}
	if hits, _ := s.SearchRecall(ctx, a.ID, `"porto"`, 0, 0, 10); len(hits) != 1 {
		t.Fatalf("the new text isn't found: %v", refs(hits))
	}

	around, err := s.RecallAround(ctx, a.ID, 12, 3)
	if err != nil || len(around) != 2 || around[0].Ref != "item:10" || around[1].Ref != "item:12" {
		t.Fatalf("around item 12 = %v, %v", refs(around), err)
	}
	if d, err := s.GetRecall(ctx, a.ID, "report:r1"); err != nil || d.Text != "Lisbon options\nHotels near the river" {
		t.Fatalf("GetRecall = %+v, %v", d, err)
	}
	if _, err := s.GetRecall(ctx, b.ID, "report:r1"); err != ErrNotFound {
		t.Fatalf("b read a's report doc: %v", err)
	}
}

// A reset forgets the conversation's documents but keeps the reports'; a
// deleted report and a deleted user take theirs with them.
func TestRecallCleanup(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "u", "x", "U", false)
	_ = s.SaveReport(ctx, &Report{ID: "r1", UserID: u.ID, Title: "Lisbon", Format: "markdown", Content: "x"})
	_ = s.IndexRecall(ctx, u.ID, []RecallDoc{
		{Ref: "item:1", Kind: "user", ItemID: 1, Text: "lisbon trip"},
		{Ref: "report:r1", Kind: "report", Text: "lisbon report"},
		{Ref: "report:r2", Kind: "report", Text: "lisbon flights"},
	})
	if err := s.ResetSession(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchRecall(ctx, u.ID, `"lisbon"`, 0, 0, 10); len(hits) != 2 || hits[0].Kind != "report" || hits[1].Kind != "report" {
		t.Fatalf("after reset: %v", refs(hits))
	}
	if _, err := s.DeleteReport(ctx, u.ID, "r1"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchRecall(ctx, u.ID, `"lisbon"`, 0, 0, 10); len(hits) != 1 || hits[0].Ref != "report:r2" {
		t.Fatalf("after deleting the report: %v", refs(hits))
	}
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	var docs, indexed int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM recall_docs`).Scan(&docs)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM recall WHERE recall MATCH '"lisbon"'`).Scan(&indexed)
	if docs != 0 || indexed != 0 {
		t.Fatalf("after deleting the user: %d documents, %d indexed", docs, indexed)
	}
}

func TestItemTimes(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "u", "x", "U", false)
	if err := s.AppendItems(ctx, u.ID, []json.RawMessage{json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":2}`)}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Items(ctx, u.ID, 0)
	times, err := s.ItemTimes(ctx, u.ID, 0, rows[1].ID)
	if err != nil || len(times) != 2 || times[rows[0].ID] == 0 {
		t.Fatalf("ItemTimes = %v, %v", times, err)
	}
	if got, _ := s.ItemsThrough(ctx, u.ID, 0, rows[0].ID); len(got) != 1 || got[0].ID != rows[0].ID {
		t.Fatalf("ItemsThrough = %v", got)
	}
}
