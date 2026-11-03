package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func contents(ms []*Memory) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Content)
	}
	return out
}

// Memories saved before the migration keep their content and their ids,
// count as the user's, and all stay live.
func TestMigrationKeepsMemories(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + sessionSchema + loginSchema); err != nil {
		t.Fatal(err)
	}
	if err := setUserVersion(ctx, db, schemaBaseline); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id, username, password_hash, created_at) VALUES(1, 'old', 'x', 1)`); err != nil {
		t.Fatal(err)
	}
	long := string(make([]byte, 5000)) // over any budget: still kept whole
	for i, c := range []string{"Lives in Lyon", "Prefers trains", long} {
		if _, err := db.Exec(`INSERT INTO memories(user_id, content, created_at) VALUES(1, ?, ?)`, c, 100+i); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ms, err := s.Memories(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 || ms[0].Content != "Lives in Lyon" || ms[1].Content != "Prefers trains" || ms[2].Content != long {
		t.Fatalf("memories after the migration = %q", contents(ms))
	}
	for i, m := range ms {
		if m.ID != int64(i+1) || m.Source != "user" || m.CreatedAt != int64(100+i) || m.UpdatedAt != m.CreatedAt {
			t.Errorf("memory %d after the migration = %+v", i, m)
		}
	}
}

// A vetoed edit changes nothing; one that goes through deletes and adds
// together.
func TestEditMemoriesAtomic(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "u", "x", "U", false)
	a, _ := s.AddMemory(ctx, u.ID, "Lives in Lyon")
	b, _ := s.AddMemory(ctx, u.ID, "Has a dog")

	veto := errors.New("over budget")
	_, err := s.EditMemories(ctx, u.ID, func(live []*Memory) ([]int64, []NewMemory, error) {
		if len(live) != 2 {
			t.Errorf("edit saw %d live entries, want 2", len(live))
		}
		return []int64{a.ID}, []NewMemory{{Content: "Lives in Paris", Source: "alexa"}}, veto
	})
	if !errors.Is(err, veto) {
		t.Fatalf("EditMemories = %v, want the veto", err)
	}
	if ms, _ := s.Memories(ctx, u.ID); len(ms) != 2 || ms[0].Content != "Lives in Lyon" {
		t.Fatalf("a vetoed edit changed memory: %q", contents(ms))
	}

	// An id that isn't live fails the whole edit.
	_, err = s.EditMemories(ctx, u.ID, func([]*Memory) ([]int64, []NewMemory, error) {
		return []int64{a.ID, 999}, []NewMemory{{Content: "Lives in Paris", Source: "alexa"}}, nil
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("EditMemories with an unknown id = %v, want ErrNotFound", err)
	}
	if ms, _ := s.Memories(ctx, u.ID); len(ms) != 2 {
		t.Fatalf("a failed edit changed memory: %q", contents(ms))
	}

	added, err := s.EditMemories(ctx, u.ID, func([]*Memory) ([]int64, []NewMemory, error) {
		return []int64{a.ID}, []NewMemory{{Content: "Lives in Paris", Source: "alexa"}}, nil
	})
	if err != nil || len(added) != 1 || added[0].ID == 0 || added[0].Source != "alexa" {
		t.Fatalf("EditMemories = %+v, %v", added, err)
	}
	ms, _ := s.Memories(ctx, u.ID)
	if len(ms) != 2 || ms[0].ID != b.ID || ms[1].Content != "Lives in Paris" || ms[1].Source != "alexa" {
		t.Fatalf("memory after the edit = %+v", ms)
	}
}

// Deleted entries leave the list and can be restored; another user can do
// neither.
func TestDeleteAndRestoreMemory(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "u", "x", "U", false)
	other, _ := s.CreateUser(ctx, "o", "x", "O", false)
	m, _ := s.AddMemory(ctx, u.ID, "Has a dog")
	if ok, _ := s.DeleteMemory(ctx, u.ID, m.ID); !ok {
		t.Fatal("delete found nothing")
	}
	if ms, _ := s.Memories(ctx, u.ID); len(ms) != 0 {
		t.Fatalf("a deleted entry is still listed: %q", contents(ms))
	}
	if ok, _ := s.DeleteMemory(ctx, u.ID, m.ID); ok {
		t.Fatal("deleting it twice found it again")
	}
	if ok, _ := s.RestoreMemory(ctx, other.ID, m.ID); ok {
		t.Fatal("another user restored the entry")
	}
	if ok, _ := s.RestoreMemory(ctx, u.ID, m.ID); !ok {
		t.Fatal("restore found nothing")
	}
	if ms, _ := s.Memories(ctx, u.ID); len(ms) != 1 || ms[0].Content != "Has a dog" {
		t.Fatalf("after restore = %q", contents(ms))
	}
}

// An entry the user edits becomes theirs.
func TestUpdateMemoryMakesItTheUsers(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "u", "x", "U", false)
	added, err := s.EditMemories(ctx, u.ID, func([]*Memory) ([]int64, []NewMemory, error) {
		return nil, []NewMemory{{Content: "Likes jazz", Source: "review"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMemory(ctx, u.ID, added[0].ID, "Likes jazz and blues"); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.Memories(ctx, u.ID)
	if len(ms) != 1 || ms[0].Content != "Likes jazz and blues" || ms[0].Source != "user" || ms[0].UpdatedAt < ms[0].CreatedAt {
		t.Fatalf("after the user's edit = %+v", ms[0])
	}
}
