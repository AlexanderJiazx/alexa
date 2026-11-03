package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"alexagent/internal/config"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
)

// The file tools can't reach through a link to the server's data
// directory: absolute, relative, or a file that doesn't exist yet
// (acceptance 1 of WP-S1, for the agent's callers).
func TestFileToolsRefuseLinksOut(t *testing.T) {
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "vault.key"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := userenv.New(data, false, 0).Ensure(3)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(data, filepath.Join(env.Workspace, "d"))
	_ = os.Symlink(filepath.Join(data, "planted.webp"), filepath.Join(env.Workspace, "pic.webp"))
	// The image API answers with a WebP, so generate_image renames pic.png
	// to pic.webp: the dangling link that leads out.
	var calls atomic.Int32
	webp := base64.StdEncoding.EncodeToString([]byte("RIFF\x00\x00\x00\x00WEBPVP8 fake"))
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": webp}}})
	}))
	defer api.Close()
	rt := &Runtime{cfg: &config.Config{BaseURL: api.URL, ImageModel: "test"}}
	tc := &toolCtx{ctx: context.Background(), rt: rt, run: &run{env: env}}

	for _, p := range []string{"d/vault.key", "/workspace/d/vault.key", filepath.Join(env.Workspace, "d", "vault.key"), "d/new.txt", filepath.Join(env.Workspace, "d", "new.txt")} {
		if res, err := readFile(tc, map[string]any{"path": p}); err == nil {
			t.Errorf("read_file %q = %q", p, res.Text)
		}
		if res, err := writeFile(tc, map[string]any{"path": p, "content": "x"}); err == nil {
			t.Errorf("write_file %q = %q", p, res.Text)
		}
		if res, err := writeFile(tc, map[string]any{"path": p, "content": "x", "append": true}); err == nil {
			t.Errorf("write_file append %q = %q", p, res.Text)
		}
		if res, err := listFiles(tc, map[string]any{"path": filepath.Dir(p)}); err == nil && strings.Contains(res.Text, "vault.key") {
			t.Errorf("list_files %q = %q", filepath.Dir(p), res.Text)
		}
		if res, err := generateImage(tc, map[string]any{"path": filepath.Join(filepath.Dir(p), "img.png"), "prompt": "a cat"}); err == nil {
			t.Errorf("generate_image %q = %q", p, res.Text)
		}
	}
	for _, p := range []string{"", "/workspace/", "."} {
		if res, err := generateImage(tc, map[string]any{"path": p, "prompt": "a cat"}); err == nil {
			t.Errorf("generate_image with no file name %q = %q", p, res.Text)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("the image API was called %d times for paths that were refused", n)
	}
	if res, err := generateImage(tc, map[string]any{"path": "pic.png", "prompt": "a cat"}); err == nil {
		t.Errorf("generate_image wrote through the link after renaming: %q", res.Text)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "vault.key")); string(b) != "SECRET" {
		t.Fatalf("vault key = %q", b)
	}
	for _, f := range []string{"new.txt", "img.png", "planted.webp"} {
		if _, err := os.Lstat(filepath.Join(data, f)); err == nil {
			t.Errorf("%s was written into the data dir", f)
		}
	}
	// The whole workspace listing shows the link, not what's behind it.
	res, err := listFiles(tc, map[string]any{})
	if err != nil || strings.Contains(res.Text, "vault.key") {
		t.Errorf("list_files = %q, %v", res.Text, err)
	}

	// Inside the workspace everything still works.
	if _, err := writeFile(tc, map[string]any{"path": "notes/a.md", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := writeFile(tc, map[string]any{"path": "notes/a.md", "content": " there", "append": true}); err != nil {
		t.Fatal(err)
	}
	if res, err := readFile(tc, map[string]any{"path": "/workspace/notes/a.md"}); err != nil || res.Text != "hi there" {
		t.Errorf("read back %q, %v", res.Text, err)
	}
	res, err = generateImage(tc, map[string]any{"path": "art/cat.png", "prompt": "a cat"})
	if err != nil || !strings.Contains(res.Text, "art/cat.webp") {
		t.Fatalf("generate_image = %v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(env.Workspace, "art", "cat.webp")); err != nil {
		t.Error("the image wasn't saved under its final name")
	}
	if res, err := listFiles(tc, map[string]any{"path": "notes"}); err != nil || res.Text != "notes/a.md (8 bytes)" {
		t.Errorf("list_files notes = %q, %v", res.Text, err)
	}
}

// fetch_url's save_as can't write through a link out either (it checks
// with Resolve; see the handoff to move it onto env.Create).
func TestFetchSaveAsRefusesLinksOut(t *testing.T) {
	data := t.TempDir()
	env, err := userenv.New(data, false, 0).Ensure(3)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(data, filepath.Join(env.Workspace, "d"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("BODY"))
	}))
	defer srv.Close()
	allowFetchFrom(t, "127.0.0.1")
	tc := &toolCtx{ctx: context.Background(), run: &run{env: env}}
	for _, p := range []string{"d/planted.bin", "/workspace/d/planted.bin", filepath.Join(env.Workspace, "d", "planted.bin")} {
		if res, err := fetchURL(tc, map[string]any{"url": srv.URL, "save_as": p}); err == nil {
			t.Errorf("fetch_url save_as %q = %q", p, res.Text)
		}
	}
	if _, err := os.Lstat(filepath.Join(data, "planted.bin")); err == nil {
		t.Error("fetch_url wrote into the data dir")
	}
	if _, err := fetchURL(tc, map[string]any{"url": srv.URL, "save_as": "dl/page.bin"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(env.Workspace, "dl", "page.bin")); string(b) != "BODY" {
		t.Errorf("saved %q", b)
	}
}

// shellEnv is a toolCtx for run_shell in a fresh workspace.
func shellEnv(t *testing.T) *toolCtx {
	t.Helper()
	env, err := userenv.New(t.TempDir(), false, 0).Ensure(7)
	if err != nil {
		t.Fatal(err)
	}
	return &toolCtx{ctx: context.Background(), rt: &Runtime{}, run: &run{env: env, user: &store.User{ID: 7, Timezone: "UTC"}}}
}

// A command that leaves something running ("start a server &") returns
// once it's done, not when that process lets go of its output, and what it
// left in its process group is killed (BH-1).
func TestShellBackgroundJobDoesNotHoldTheCall(t *testing.T) {
	tc := shellEnv(t)
	before := runtime.NumGoroutine()
	t0 := time.Now()
	res, err := runShell(tc, map[string]any{"command": "sleep 30 & echo $! > bg.pid; echo started", "timeout_seconds": float64(20)})
	took := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	if took > shellWaitDelay+2*time.Second {
		t.Errorf("run_shell took %s for a command that finished at once", took.Round(time.Millisecond))
	}
	if !strings.HasPrefix(res.Text, "exit code 0\n") || !strings.Contains(res.Text, "started") || strings.Contains(res.Text, "timed out") {
		t.Errorf("result = %q", res.Text)
	}
	b, err := os.ReadFile(filepath.Join(tc.run.env.Workspace, "bg.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		t.Fatalf("bad pid %q", b)
	}
	waitFor(t, 3*time.Second, "the background job to be killed", func() bool { return syscall.Kill(pid, 0) != nil })
	waitFor(t, 3*time.Second, "run_shell's goroutines to end", func() bool { return runtime.NumGoroutine() <= before })
}

// A process that left the command's group (setsid) can't be killed with
// it, but it doesn't keep the call past its timeout either (BH-1).
func TestShellTimeoutWithEscapedChild(t *testing.T) {
	if findTool("perl") == "" {
		t.Skip("no perl")
	}
	tc := shellEnv(t)
	before := runtime.NumGoroutine()
	t0 := time.Now()
	res, err := runShell(tc, map[string]any{"command": `perl -e 'use POSIX; POSIX::setsid(); sleep 6' & echo started; sleep 30`, "timeout_seconds": float64(2)})
	took := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	if took > 2*time.Second+shellWaitDelay+2*time.Second {
		t.Errorf("run_shell with timeout_seconds=2 returned after %s", took.Round(time.Millisecond))
	}
	if !strings.HasPrefix(res.Text, "exit code -1\n") || !strings.Contains(res.Text, "started") || !strings.Contains(res.Text, "[timed out after 2s]") {
		t.Errorf("result = %q", res.Text)
	}
	waitFor(t, 3*time.Second, "run_shell's goroutines to end", func() bool { return runtime.NumGoroutine() <= before })
}

// An ordinary command still reports its own exit code and output.
func TestShellExitCode(t *testing.T) {
	tc := shellEnv(t)
	res, err := runShell(tc, map[string]any{"command": "echo out; echo err >&2; exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "exit code 3\nout\nerr\n" {
		t.Errorf("result = %q", res.Text)
	}
}

func waitFor(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Errorf("timed out waiting for %s", what)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// read_file clamps the offset the model gives it, and pages long text on
// whole characters, so the pages put back together are the file (BH-9).
func TestReadFileOffsets(t *testing.T) {
	tc := shellEnv(t)
	if err := os.WriteFile(filepath.Join(tc.run.env.Workspace, "a.txt"), []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, off := range []float64{-5, -1 << 40, 3, 11, 1 << 40} {
		res, err := readFile(tc, map[string]any{"path": "a.txt", "offset": off})
		if err != nil {
			t.Fatalf("offset %v: %v", off, err)
		}
		want := "hello world"[min(max(int(off), 0), 11):]
		if res.Text != want {
			t.Errorf("offset %v = %q, want %q", off, res.Text, want)
		}
	}
	// 3-byte characters, so a page of readPage bytes ends inside one.
	text := strings.Repeat("€", readPage/3*2+7) + "end"
	if err := os.WriteFile(filepath.Join(tc.run.env.Workspace, "b.txt"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	off := 0
	for page := 0; ; page++ {
		if page > 5 {
			t.Fatal("never reached the end")
		}
		res, err := readFile(tc, map[string]any{"path": "b.txt", "offset": float64(off)})
		if err != nil {
			t.Fatal(err)
		}
		s, next, more := strings.Cut(res.Text, "\n[…truncated; continue with offset=")
		if !utf8.ValidString(s) {
			t.Fatalf("page at %d isn't whole characters", off)
		}
		got.WriteString(s)
		if !more {
			break
		}
		n, err := strconv.Atoi(strings.TrimSuffix(next, "]"))
		if err != nil || n <= off {
			t.Fatalf("next offset %q after %d", next, off)
		}
		off = n
	}
	if got.String() != text {
		t.Errorf("the pages put together are %d bytes, want %d", got.Len(), len(text))
	}
	// An offset inside a character starts at that character.
	res, err := readFile(tc, map[string]any{"path": "b.txt", "offset": float64(4)})
	if err != nil || !strings.HasPrefix(res.Text, "€€") || !utf8.ValidString(res.Text) {
		t.Errorf("offset inside a character = %q…, %v", res.Text[:min(len(res.Text), 12)], err)
	}
}

func TestCutText(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 2, "ab"},
		{"a€b", 2, "a"},
		{"a€b", 3, "a"},
		{"a€b", 4, "a€"},
	} {
		if got := cutText(c.in, c.n); got != c.want {
			t.Errorf("cutText(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
