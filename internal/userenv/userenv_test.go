package userenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStaysInWorkspace(t *testing.T) {
	envs := New(t.TempDir(), false, 0)
	env, err := envs.Ensure(1)
	if err != nil {
		t.Fatal(err)
	}
	ok := map[string]string{
		"notes.md":                "notes.md",
		"/workspace/a/b.txt":      "a/b.txt",
		"./x/../y.txt":            "y.txt",
		"../../2/workspace/s.txt": "2/workspace/s.txt", // clamped inside, not escaped
		"/etc/passwd":             "etc/passwd",
		env.Workspace + "/in.txt": "in.txt",
	}
	for in, want := range ok {
		got, err := env.Resolve(in)
		if err != nil {
			t.Errorf("Resolve(%q) error: %v", in, err)
			continue
		}
		if !strings.HasPrefix(got, env.Workspace+string(filepath.Separator)) {
			t.Errorf("Resolve(%q) = %q escapes the workspace", in, got)
		}
		if rel := env.Rel(got); rel != want {
			t.Errorf("Resolve(%q) rel = %q, want %q", in, rel, want)
		}
	}
	// A symlink pointing out of the workspace must not be followed.
	outside := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(outside, []byte("x"), 0o600)
	_ = os.Symlink(filepath.Dir(outside), filepath.Join(env.Workspace, "link"))
	if _, err := env.Resolve("link/secret"); err == nil {
		t.Error("Resolve followed a symlink out of the workspace")
	}
}

func TestMkdirAll(t *testing.T) {
	envs := New(t.TempDir(), false, 0)
	env, _ := envs.Ensure(1)
	d := filepath.Join(env.Workspace, "a", "b", "c")
	if err := env.MkdirAll(d); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
		t.Fatal("directory not created")
	}
	if err := env.MkdirAll(d); err != nil {
		t.Fatal("MkdirAll should be idempotent:", err)
	}
}
