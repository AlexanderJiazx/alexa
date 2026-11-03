package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Once loaded, the secrets are gone from the environment, so Chrome and the
// tools can't inherit them; the rest stays.
func TestDropSecrets(t *testing.T) {
	t.Setenv("META_MODEL_API_KEY", "model-key")
	t.Setenv("RESEND_API_KEY", "mail-key")
	t.Setenv("SOME_SERVICE_TOKEN", "token")
	t.Setenv("MAIL_FROM", "alexa@example.com")
	dropSecrets()
	for _, k := range []string{"META_MODEL_API_KEY", "RESEND_API_KEY", "SOME_SERVICE_TOKEN"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s is still set (%q)", k, v)
		}
	}
	if os.Getenv("MAIL_FROM") != "alexa@example.com" {
		t.Error("MAIL_FROM was dropped")
	}
}

// Unsetenv leaves the environment the server started with in
// /proc/<pid>/environ; once hidden, a process of the same UID (the shell
// tool without SANDBOX_UIDS) can't read it there (B01).
func TestHideEnviron(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux shows a process's environment in /proc")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads every process's /proc")
	}
	environ := fmt.Sprintf("/proc/%d/environ", os.Getpid())
	if err := exec.Command("cat", environ).Run(); err != nil {
		t.Skipf("a child can't read our environment to begin with: %v", err)
	}
	if err := hideEnviron(); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cat", environ).Output(); err == nil {
		t.Errorf("a process of the same UID still reads the server's environment (%d bytes)", len(out))
	}
}
