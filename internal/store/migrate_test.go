package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func versionOf(t *testing.T, s *Store) int {
	t.Helper()
	v, err := userVersion(context.Background(), s.db)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// withMigrations swaps in test migrations for the length of a test.
func withMigrations(t *testing.T, ms ...migration) {
	t.Helper()
	old := migrations
	migrations = ms
	t.Cleanup(func() { migrations = old })
}

func TestFreshDatabaseIsAtBaseline(t *testing.T) {
	s := openTest(t)
	if got := versionOf(t, s); got != schemaVersion() {
		t.Fatalf("user_version = %d, want %d", got, schemaVersion())
	}
}

// A database made before versioning has the baseline tables and
// user_version 0. Opening it keeps its data, marks it as the baseline and
// migrates it from there.
func TestExistingDatabaseIsBaselined(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + sessionSchema + loginSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(username, password_hash, created_at) VALUES('old', 'x', 1)`); err != nil {
		t.Fatal(err)
	}
	if v, _ := userVersion(ctx, db); v != 0 {
		t.Fatalf("setup: user_version = %d, want 0", v)
	}
	db.Close()

	for range 2 { // and opening it again changes nothing
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := versionOf(t, s); got != schemaVersion() {
			t.Fatalf("user_version = %d, want %d", got, schemaVersion())
		}
		if _, err := s.GetUserByName(ctx, "old"); err != nil {
			t.Fatalf("existing user lost: %v", err)
		}
		s.Close()
	}
}

func TestMigrationsRunOnceInOrder(t *testing.T) {
	withMigrations(t,
		migration{name: "add a table", sql: `CREATE TABLE things (n INTEGER NOT NULL)`},
		migration{name: "fill it", sql: `INSERT INTO things(n) VALUES (1)`},
	)
	path := filepath.Join(t.TempDir(), "m.db")
	for range 3 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := versionOf(t, s); got != schemaBaseline+2 {
			t.Fatalf("user_version = %d, want %d", got, schemaBaseline+2)
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM things`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("the backfill ran %d times, want once", n)
		}
		s.Close()
	}
}

// A failing migration rolls back with its version bump, so the next start
// tries it again from the same place.
func TestFailedMigrationLeavesVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.db")
	withMigrations(t) // start at the baseline
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	withMigrations(t, migration{name: "broken", sql: `CREATE TABLE half (n INTEGER); INSERT INTO nowhere VALUES (1)`})
	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded despite a broken migration")
	}
	migrations = nil
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := versionOf(t, s); got != schemaBaseline {
		t.Fatalf("user_version = %d after a failed migration, want %d", got, schemaBaseline)
	}
	var name string
	if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE name = 'half'`).Scan(&name); err != sql.ErrNoRows {
		t.Fatalf("the failed migration left a table behind (err %v)", err)
	}
}

// A database from a newer build still opens: a rollback must not become an
// outage.
func TestNewerDatabaseStillOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := setUserVersion(context.Background(), s.db, schemaVersion()+5); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("Open = %v, want it to open a newer database", err)
	}
	defer s.Close()
	if got := versionOf(t, s); got != schemaVersion()+5 {
		t.Fatalf("user_version = %d, want it left at %d", got, schemaVersion()+5)
	}
}
