package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// A deleted entry leaves memories, which a build from before soft delete
// reads in full: rolled back to, it can't put the entry back in Alexa's
// instructions. Restored, the entry is as it was, id and all. One deleted
// behind memories.deleted_at, before deleted entries had their own table,
// moves there with the migration.
func TestDeletedMemoryLeavesTheTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := s.CreateUser(ctx, "u", "x", "U", false)
	added, err := s.EditMemories(ctx, u.ID, func([]*Memory) ([]int64, []NewMemory, error) {
		return nil, []NewMemory{{Content: "Likes jazz", Source: "review"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m := added[0]
	// What a build from before soft delete reads (main at 266ab3e).
	oldRead := func(s *Store) int {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE user_id=?`, u.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if ok, err := s.DeleteMemory(ctx, u.ID, m.ID); !ok || err != nil {
		t.Fatalf("delete = %v, %v", ok, err)
	}
	if n := oldRead(s); n != 0 {
		t.Fatalf("an older build would read %d entries after the delete", n)
	}
	if ok, err := s.RestoreMemory(ctx, u.ID, m.ID); !ok || err != nil {
		t.Fatalf("restore = %v, %v", ok, err)
	}
	if ms, _ := s.Memories(ctx, u.ID); len(ms) != 1 || ms[0].ID != m.ID || ms[0].Source != "review" || ms[0].CreatedAt != m.CreatedAt || ms[0].Content != "Likes jazz" {
		t.Fatalf("after restore = %+v", ms)
	}
	if ok, _ := s.RestoreMemory(ctx, u.ID, m.ID); ok {
		t.Fatal("restored twice")
	}

	// As deleted before the table: flagged, at the version before it.
	before := 0
	for i, mg := range migrations {
		if mg.name == "deleted memories apart" {
			before = schemaBaseline + i
		}
	}
	if _, err := s.db.Exec(`UPDATE memories SET deleted_at=? WHERE id=?`, now(), m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, before)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if n := oldRead(s); n != 0 {
		t.Fatalf("after the migration an older build would read %d entries", n)
	}
	if ok, _ := s.RestoreMemory(ctx, u.ID, m.ID); !ok {
		t.Fatal("the entry deleted behind the flag can't be restored")
	}
	if ms, _ := s.Memories(ctx, u.ID); len(ms) != 1 || ms[0].ID != m.ID {
		t.Fatalf("after restore = %+v", ms)
	}
}
