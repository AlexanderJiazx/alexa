package userenv

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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
		"/workspaces/x":           "workspaces/x",
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

// dataLink sets up the attack from the review: the user's shell links the
// server's data directory into their workspace as d/, next to a vault key.
func dataLink(t *testing.T) (env *Env, data string) {
	t.Helper()
	data = t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "vault.key"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := New(data, false, 0).Ensure(3)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(data, filepath.Join(env.Workspace, "d")); err != nil {
		t.Fatal(err)
	}
	// A dangling link to a file that doesn't exist yet, outside.
	if err := os.Symlink(filepath.Join(data, "planted.txt"), filepath.Join(env.Workspace, "new.txt")); err != nil {
		t.Fatal(err)
	}
	return env, data
}

// Every way in refuses d/vault.key: absolute, relative, and a file that
// doesn't exist yet, through a linked folder or a dangling link.
func TestSymlinkToDataDirRefused(t *testing.T) {
	env, data := dataLink(t)
	paths := []string{
		"d/vault.key",
		"./d/vault.key",
		"/workspace/d/vault.key",
		filepath.Join(env.Workspace, "d", "vault.key"),
		filepath.Join(env.Workspace, "d", "vault.key") + "/",
		"d/not-yet.txt",
		filepath.Join(env.Workspace, "d", "not-yet.txt"),
		"new.txt",
		filepath.Join(env.Workspace, "new.txt"),
	}
	for _, p := range paths {
		if _, err := env.Resolve(p); !errors.Is(err, ErrOutside) {
			t.Errorf("Resolve(%q) = %v, want outside", p, err)
		}
		if _, err := env.Stat(p); err == nil {
			t.Errorf("Stat(%q) worked", p)
		}
		if f, err := env.Open(p); err == nil {
			b, _ := io.ReadAll(f)
			f.Close()
			t.Errorf("Open(%q) read %q", p, b)
		}
		if _, err := env.ReadFile(p, 1<<20); err == nil {
			t.Errorf("ReadFile(%q) worked", p)
		}
		for _, flag := range []int{os.O_EXCL, os.O_TRUNC, os.O_APPEND} {
			if f, err := env.Create(p, flag); err == nil {
				f.Close()
				t.Errorf("Create(%q, %x) worked", p, flag)
			}
		}
		if err := env.MkdirAll(filepath.Join(env.Workspace, env.Clean(p), "sub")); err == nil {
			t.Errorf("MkdirAll under %q worked", p)
		}
		// Removing a path through the linked folder is refused; removing
		// the dangling link itself is fine, and is checked below.
		if strings.Contains(p, "d/") {
			if err := env.Remove(p); err == nil {
				t.Errorf("Remove(%q) worked", p)
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(data, "vault.key")); err != nil || string(b) != "SECRET" {
		t.Fatalf("vault key changed: %q %v", b, err)
	}
	for _, f := range []string{"planted.txt", "not-yet.txt", "sub"} {
		if _, err := os.Lstat(filepath.Join(data, f)); err == nil {
			t.Errorf("%s was created in the data dir", f)
		}
	}
	// Removing a link removes the link, never what it points to.
	for _, link := range []string{"d", "new.txt"} {
		if err := env.Remove(link); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(env.Workspace, link)); err == nil {
			t.Errorf("%s is still there", link)
		}
	}
	if _, err := os.Stat(filepath.Join(data, "vault.key")); err != nil {
		t.Fatal("removing the link removed its target")
	}
}

func TestWorkspaceIO(t *testing.T) {
	env, err := New(t.TempDir(), false, 0).Ensure(1)
	if err != nil {
		t.Fatal(err)
	}
	f, err := env.Create("a/b/租房合同.txt", os.O_EXCL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("hello")
	f.Close()
	if _, err := env.Create("a/b/租房合同.txt", os.O_EXCL); !errors.Is(err, fs.ErrExist) {
		t.Errorf("O_EXCL over an existing file = %v", err)
	}
	f, _ = env.Create("/workspace/a/b/租房合同.txt", os.O_APPEND)
	_, _ = f.WriteString(" world")
	f.Close()
	if b, err := env.ReadFile("a/b/租房合同.txt", 100); err != nil || string(b) != "hello world" {
		t.Fatalf("read back %q %v", b, err)
	}
	if _, err := env.ReadFile("a/b/租房合同.txt", 5); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("over the limit = %v", err)
	}
	f, _ = env.Create("a/b/租房合同.txt", os.O_TRUNC)
	_, _ = f.WriteString("new")
	f.Close()
	if b, _ := env.ReadFile("a/b/租房合同.txt", 100); string(b) != "new" {
		t.Errorf("after O_TRUNC: %q", b)
	}
	// Links that stay inside the workspace still work.
	_ = os.Symlink("a", filepath.Join(env.Workspace, "inside"))
	if b, err := env.ReadFile("inside/b/租房合同.txt", 100); err != nil || string(b) != "new" {
		t.Errorf("link inside the workspace: %q %v", b, err)
	}
	for _, p := range []string{"", ".", "/", "/workspace", "/workspace/", env.Workspace, env.Workspace + "/", "uploads", "uploads/", "/workspace/downloads/"} {
		if err := env.Remove(p); err == nil {
			t.Errorf("Remove(%q) worked", p)
		}
	}
	if _, err := os.Stat(env.Uploads); err != nil {
		t.Fatal("uploads is gone")
	}
	if err := env.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Stat("a/b/租房合同.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("after Remove: %v", err)
	}
	if err := env.Remove("a"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Remove of a missing file = %v", err)
	}
}

// A folder swapped for a link is never used: file I/O refuses to go through
// it, and Ensure moves the link aside (the link, not its target) and makes
// a real folder, so the user isn't locked out of every message.
func TestEnsureMovesLinkedFoldersAside(t *testing.T) {
	for _, name := range []string{"workspace", "workspace/uploads", "workspace/downloads", "browser", "home"} {
		t.Run(name, func(t *testing.T) {
			data := t.TempDir()
			envs := New(data, false, 0)
			env, err := envs.Ensure(1)
			if err != nil {
				t.Fatal(err)
			}
			other := t.TempDir()
			if err := os.WriteFile(filepath.Join(other, "keep"), []byte("theirs"), 0o600); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(env.Root, name)
			if err := os.RemoveAll(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, p); err != nil {
				t.Fatal(err)
			}
			// Before Ensure runs again, nothing goes through the link.
			if name == "workspace" {
				// The error says what's wrong without the server's paths.
				if _, err := env.OpenRoot(); err == nil || !strings.Contains(err.Error(), "isn't a plain folder") || strings.Contains(err.Error(), data) {
					t.Errorf("OpenRoot with a linked workspace = %v", err)
				}
			}
			if name == "workspace/uploads" {
				if f, err := env.Create("uploads/2026-10-30/x.txt", os.O_EXCL); err == nil {
					f.Close()
					t.Error("an upload went through a linked uploads/")
				}
			}
			if _, err := envs.Ensure(1); err != nil {
				t.Fatalf("Ensure with %s linked = %v", name, err)
			}
			if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
				t.Fatalf("%s isn't a real folder after Ensure: %v", name, err)
			}
			moved, _ := filepath.Glob(p + ".moved-*")
			if len(moved) != 1 {
				t.Fatalf("moved aside: %v", moved)
			}
			if to, err := os.Readlink(moved[0]); err != nil || to != other {
				t.Errorf("the link wasn't kept as it was: %q %v", to, err)
			}
			if entries, _ := os.ReadDir(other); len(entries) != 1 || entries[0].Name() != "keep" {
				t.Errorf("the link's target changed: %v", entries)
			}
			if name == "workspace" {
				if _, err := env.OpenRoot(); err != nil {
					t.Errorf("OpenRoot after Ensure = %v", err)
				}
			}
		})
	}
	// A file in place of a folder is moved aside the same way.
	data := t.TempDir()
	envs := New(data, false, 0)
	env, _ := envs.Ensure(1)
	_ = os.RemoveAll(env.Home)
	_ = os.WriteFile(env.Home, []byte("not a folder"), 0o600)
	if _, err := envs.Ensure(1); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(env.Home); err != nil || !fi.IsDir() {
		t.Fatalf("home after Ensure: %v", err)
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
	// Outside the workspace (the shell's temp folder) works as before.
	if err := env.MkdirAll(filepath.Join(env.Home, "tmp")); err != nil {
		t.Fatal(err)
	}
}

// In the sandbox a hard link to someone else's file (the database, say) is
// refused for reads and writes; os.Root can't see hard links.
func TestHardLinkRefused(t *testing.T) {
	data := t.TempDir()
	env, err := New(data, false, 0).Ensure(1)
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(data, "alexa.db")
	if err := os.WriteFile(db, []byte("DB"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(db, filepath.Join(env.Workspace, "db")); err != nil {
		t.Skip("can't make hard links here:", err)
	}
	_ = os.WriteFile(filepath.Join(env.Workspace, "old.txt"), []byte("old"), 0o600)
	// Pretend the user runs as another UID; the files above are the test's.
	env.UID, env.GID = os.Getuid()+12345, os.Getgid()
	if _, err := env.ReadFile("db", 100); err == nil || !strings.Contains(err.Error(), "isn't yours") {
		t.Errorf("read through a hard link = %v", err)
	}
	if f, err := env.Create("db", os.O_TRUNC); err == nil {
		f.Close()
		t.Error("opened a hard link for writing")
	}
	if b, _ := os.ReadFile(db); string(b) != "DB" {
		t.Errorf("database changed: %q", b)
	}
	// A file of someone else's with one name (made before the sandbox, say)
	// is still readable.
	if b, err := env.ReadFile("old.txt", 100); err != nil || string(b) != "old" {
		t.Errorf("old file: %q %v", b, err)
	}
}

// A FIFO where a file should be (the user's shell can make one) is refused
// at once, for reading and for writing, instead of blocking the caller
// until something opens its other end.
func TestFifoRefused(t *testing.T) {
	env, err := New(t.TempDir(), false, 0).Ensure(1)
	if err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(env.Workspace, "notes.txt")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("can't make a FIFO here:", err)
	}
	try := func(name string, op func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- op() }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "isn't a regular file") {
				t.Errorf("%s on a FIFO = %v, want refused", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s on a FIFO is still blocked after 2s", name)
		}
	}
	try("ReadFile", func() error { _, err := env.ReadFile("notes.txt", 1<<20); return err })
	try("Create", func() error {
		f, err := env.Create("notes.txt", os.O_TRUNC)
		if f != nil {
			f.Close()
		}
		return err
	})
	// With a reader at the other end, opening it to write wouldn't block,
	// but it's still no file to write to.
	r, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	try("Create with a reader", func() error {
		f, err := env.Create("notes.txt", os.O_APPEND)
		if f != nil {
			f.Close()
		}
		return err
	})
	// Plain files and folders still open as before.
	if err := os.WriteFile(filepath.Join(env.Workspace, "a.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := env.ReadFile("a.txt", 100); err != nil || string(b) != "hi" {
		t.Errorf("ReadFile(a.txt) = %q, %v", b, err)
	}
	if _, err := env.ReadFile("uploads", 100); err == nil || !strings.Contains(err.Error(), "is a folder") {
		t.Errorf("ReadFile(uploads) = %v, want the folder message", err)
	}
	f, err := env.Create("b.txt", os.O_TRUNC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("written"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if b, _ := os.ReadFile(filepath.Join(env.Workspace, "b.txt")); string(b) != "written" {
		t.Errorf("b.txt = %q", b)
	}
}

// The shell's home/tmp is made through the user's folder: a home swapped
// for a link mid-run is moved aside, never followed to make (and, in the
// sandbox, give the user) a folder somewhere else.
func TestMkdirAllHomeLink(t *testing.T) {
	data := t.TempDir()
	outside := filepath.Join(data, "server-only")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	env, err := New(data, false, 0).Ensure(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(env.Home); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, env.Home); err != nil {
		t.Fatal(err)
	}
	if err := env.MkdirAll(filepath.Join(env.Home, "tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "tmp")); err == nil {
		t.Errorf("MkdirAll(home/tmp) made %s, outside the user's folder", filepath.Join(outside, "tmp"))
	}
	if fi, err := os.Lstat(filepath.Join(env.Home, "tmp")); err != nil || !fi.IsDir() {
		t.Errorf("home/tmp isn't a plain folder in a plain home: %v", err)
	}
	if err := env.MkdirAll(filepath.Join(data, "elsewhere")); err == nil {
		t.Error("MkdirAll made a folder outside the user's folder")
	}
}

// Removing a user deletes their folders, and they aren't made again: a
// late run or a browser that starts for them gets ErrRemoved (BH-5).
func TestRemove(t *testing.T) {
	data := t.TempDir()
	e := New(data, false, 0)
	env, err := e.Ensure(7)
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.Ensure(8)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Home, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.Remove(7); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{env.Root, env.Shots, env.Cache} {
		if _, err := os.Stat(d); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is still there: %v", d, err)
		}
	}
	if _, err := e.Ensure(7); !errors.Is(err, ErrRemoved) {
		t.Errorf("Ensure after Remove = %v, want ErrRemoved", err)
	}
	if _, err := os.Stat(env.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Error("Ensure made the removed user's folder again")
	}
	if _, err := e.Ensure(8); err != nil {
		t.Errorf("another user: %v", err)
	}
	if _, err := os.Stat(other.Root); err != nil {
		t.Errorf("another user's folder: %v", err)
	}
}
