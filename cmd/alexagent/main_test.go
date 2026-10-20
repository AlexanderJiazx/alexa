package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdoptOldFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"paper.db", "paper.db-wal", "paper.log"} {
		write(n, n)
	}
	if err := adoptOldFiles(dir); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"alexa.db", "alexa.db-wal", "alexa.log"} {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if want := strings.Replace(n, "alexa", "paper", 1); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want the old %s", n, b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "paper.db")); err == nil {
		t.Error("paper.db is still there")
	}

	// Once renamed, an old file that turns up again is left alone.
	write("paper.db", "stray")
	if err := adoptOldFiles(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "alexa.db")); string(b) != "paper.db" {
		t.Errorf("alexa.db was overwritten: %q", b)
	}
}
