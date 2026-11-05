package browser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestNavTarget(t *testing.T) {
	ok := map[string]string{
		"example.com":              "https://example.com",
		"  https://example.com/a ": "https://example.com/a",
		"http://127.0.0.1:8091/x":  "http://127.0.0.1:8091/x",
		"about:blank":              "about:blank",
		"ABOUT:BLANK":              "about:blank",
		"cheap flights to Lisbon":  "https://duckduckgo.com/?q=cheap+flights+to+Lisbon",
	}
	for in, want := range ok {
		if got, err := navTarget(in); err != nil || got != want {
			t.Errorf("navTarget(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"file:///etc/passwd", "FILE:///etc/passwd", "file://localhost/etc/hosts",
		"chrome://settings", "chrome://version", "devtools://devtools/bundled/inspector.html",
		"chrome-extension://abc/x.html", "view-source:https://example.com",
		"about:settings", "javascript://%0Aalert(1)", "data://text/html,hi", "http:///nohost",
	} {
		if got, err := navTarget(in); err == nil {
			t.Errorf("navTarget(%q) = %q, want refused", in, got)
		}
	}
}

// Chrome starts with chromeEnv's short list and nothing else of the
// server's environment.
func TestChromeCmdDropsTheServersEnvironment(t *testing.T) {
	printenv, err := exec.LookPath("printenv")
	if err != nil {
		t.Skip("no printenv")
	}
	t.Setenv("META_MODEL_API_KEY", "leaked-model-key")
	t.Setenv("RESEND_API_KEY", "leaked-mail-key")
	t.Setenv("TZ", "Europe/Lisbon")
	out, err := chromeCmd(printenv, nil, UserEnv{HomeDir: "/home/alexa-test"}).Output()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("leaked")) {
		t.Fatalf("the server's secrets reached the command:\n%s", out)
	}
	for _, want := range []string{"PATH=", "HOME=", "TZ=Europe/Lisbon"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if n := len(strings.Split(strings.TrimSpace(string(out)), "\n")); n > 20 {
		t.Errorf("%d variables, want a short list:\n%s", n, out)
	}
}

// Chrome itself starts without the server's secrets (acceptance 3 of
// WP-S1). Linux shows a process's environment in /proc; macOS doesn't show
// it at all, so the test above covers the mechanism there.
func TestChromeHasNoSecrets(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc/<pid>/environ")
	}
	t.Setenv("META_MODEL_API_KEY", "leaked-model-key")
	t.Setenv("RESEND_API_KEY", "leaked-mail-key")
	m, _ := newTestBrowser(t)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	pid := b.chrome.cmd.Process.Pid
	env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		t.Skip(err)
	}
	if bytes.Contains(env, []byte("leaked")) {
		t.Fatalf("Chrome's environment has the server's secrets: %q", env)
	}
}

// Chrome speaks DevTools over a pipe, never a port: no local process
// (another user's shell, a fetch the server makes) can drive it (SEC-1).
// With a debugging port Chrome writes DevToolsActivePort in its profile.
func TestChromeHasNoDebuggingPort(t *testing.T) {
	m, _ := newTestBrowser(t)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range b.chrome.cmd.Args {
		if strings.HasPrefix(a, "--remote-debugging-port") {
			t.Errorf("Chrome started with %s", a)
		}
	}
	if _, err := os.Stat(filepath.Join(b.env.ProfileDir, "DevToolsActivePort")); !os.IsNotExist(err) {
		t.Errorf("Chrome listens on a debugging port (DevToolsActivePort: %v)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if c, _, err := websocket.Dial(ctx, b.chrome.url, nil); err == nil {
		c.CloseNow()
		t.Error("a second client connected to the bridge chromedp uses")
	}
	if _, err := b.Tab("main"); err != nil {
		t.Fatalf("the browser stopped working: %v", err)
	}
}

// The bridge takes one connection, at its own path: nothing that gets to
// its port first, or after, can use it.
func TestBridgeTakesOneClient(t *testing.T) {
	toR, toW := io.Pipe()
	fromR, fromW := io.Pipe()
	exited := make(chan struct{})
	defer close(exited)
	url, err := bridge(toW, fromR, exited)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + strings.TrimPrefix(url[:strings.Index(url, "/devtools")], "ws://")
	for _, path := range []string{"/json/version", "/json/list", "/devtools/browser/guess"} {
		if resp, err := http.Get(base + path); err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	// Messages go to Chrome NUL-terminated, and come back without the NUL.
	go func() { _, _ = fromW.Write([]byte(`{"id":1}` + "\x00")) }()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"id":1,"method":"Browser.getVersion"}`)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 64)
	n, _ := toR.Read(got)
	if string(got[:n]) != `{"id":1,"method":"Browser.getVersion"}`+"\x00" {
		t.Errorf("Chrome got %q", got[:n])
	}
	if _, msg, err := c.Read(ctx); err != nil || string(msg) != `{"id":1}` {
		t.Errorf("chromedp got %q, %v", msg, err)
	}
	if c2, _, err := websocket.Dial(ctx, url, nil); err == nil {
		c2.CloseNow()
		t.Error("a second client connected to the bridge")
	}
}

// The idle reaper leaves a browser up while someone watches its live view
// or KeepAlive says the user needs it (B10): closing it would lose their
// page, and an open view would only start it again, reloaded.
func TestReaperSparesABrowserInUse(t *testing.T) {
	if os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}
	dir := chromeDir(t)
	var keep sync.Map
	m := NewManager(Options{Headless: true, Width: 800, Height: 600, IdleAfter: time.Millisecond, ChromePath: os.Getenv("CHROME_PATH"),
		KeepAlive: func(id int64) bool { _, ok := keep.Load(id); return ok }},
		func(id int64) UserEnv {
			return UserEnv{UserID: id, ProfileDir: dir + "/profile", DownloadDir: dir + "/dl", HomeDir: dir}
		}, nil)
	t.Cleanup(m.CloseAll)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := b.Tab("main")
	if err != nil {
		t.Fatal(err)
	}
	_, stop := tab.Subscribe()
	time.Sleep(10 * time.Millisecond)
	m.reap()
	if !b.alive() || m.Existing(1) != b {
		t.Fatal("the reaper closed a browser with its live view open")
	}
	stop()
	keep.Store(int64(1), true)
	time.Sleep(10 * time.Millisecond)
	m.reap()
	if !b.alive() || m.Existing(1) != b {
		t.Fatal("the reaper closed a browser KeepAlive asked to keep")
	}
	keep.Delete(int64(1))
	m.reap()
	if m.Existing(1) != nil {
		t.Error("an idle browser no one needs wasn't closed")
	}
}

// Neither the agent nor a person in take-over can open file:// or chrome://.
func TestBrowserRefusesLocalSchemes(t *testing.T) {
	m, srv := newTestBrowser(t)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := b.Tab("main")
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(srv.URL); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"file:///etc/passwd", "chrome://settings", "view-source:" + srv.URL} {
		if err := tab.Navigate(u); err == nil || !strings.Contains(err.Error(), "only web pages") {
			t.Errorf("Navigate(%q) = %v, want refused", u, err)
		}
		if err := tab.Human(HumanInput{Kind: "navigate", URL: u}); err == nil {
			t.Errorf("take-over URL bar opened %q", u)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if cur, _ := tab.URL(); !strings.HasPrefix(cur, srv.URL) {
		t.Fatalf("the tab moved to %q", cur)
	}
}

// Dragging in take-over selects text and pulls a slider: moves carry the
// held button, so the page doesn't think it was let go.
func TestHumanDrag(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>Drag</title><body style="margin:0;font:20px sans-serif">
		<p id=p style="position:absolute;left:20px;top:20px;margin:0;white-space:nowrap">Select this whole sentence please</p>
		<input id=r type=range min=0 max=100 value=0 style="position:absolute;left:20px;top:100px;width:400px">
		</body>`})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	var box struct{ L, R, Y, RL, RR, RY float64 }
	if err := tab.eval(`(() => { const p = document.getElementById('p').getBoundingClientRect(), r = document.getElementById('r').getBoundingClientRect(); return {L: p.left + 1, R: p.right - 1, Y: p.top + p.height/2, RL: r.left + 4, RR: r.right - 2, RY: r.top + r.height/2} })()`, &box); err != nil {
		t.Fatal(err)
	}
	drag := func(x0, x1, y float64) {
		t.Helper()
		evs := []HumanInput{{Kind: "mousemove", X: x0, Y: y}, {Kind: "mousedown", X: x0, Y: y, Button: "left", Buttons: 1, Clicks: 1}}
		for i := 1; i <= 8; i++ {
			evs = append(evs, HumanInput{Kind: "mousemove", X: x0 + (x1-x0)*float64(i)/8, Y: y, Buttons: 1})
		}
		evs = append(evs, HumanInput{Kind: "mouseup", X: x1, Y: y, Button: "left", Buttons: 0, Clicks: 1})
		for _, ev := range evs {
			if err := tab.Human(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	drag(box.L, box.R, box.Y)
	var sel string
	if err := tab.eval(`window.getSelection().toString()`, &sel); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sel, "whole sentence") {
		t.Errorf("drag selected %q", sel)
	}
	drag(box.RL, box.RR, box.RY)
	var val string
	if err := tab.eval(`document.getElementById('r').value`, &val); err != nil {
		t.Fatal(err)
	}
	if val == "0" {
		t.Error("the slider didn't move")
	}
}

// When Chrome dies, its live views end (so they reconnect) and the next
// Get launches a new browser that works.
func TestCrashedBrowserRelaunches(t *testing.T) {
	if os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}
	dir := chromeDir(t)
	var mu sync.Mutex
	var events []string
	m := NewManager(Options{Headless: true, Width: 800, Height: 600, IdleAfter: time.Hour, ChromePath: os.Getenv("CHROME_PATH")},
		func(id int64) UserEnv {
			return UserEnv{UserID: id, ProfileDir: dir + "/profile", DownloadDir: dir + "/dl", HomeDir: dir}
		}, func(_ int64, key string, ev TabEvent) {
			mu.Lock()
			events = append(events, key+":"+ev.Type)
			mu.Unlock()
		})
	t.Cleanup(m.CloseAll)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := b.Tab("main")
	if err != nil {
		t.Fatal(err)
	}
	frames, stop := tab.Subscribe()
	defer stop()
	if err := b.chrome.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(15 * time.Second)
	for open := true; open; {
		select {
		case _, open = <-frames:
		case <-deadline:
			t.Fatal("the live view wasn't ended after Chrome died")
		}
	}
	mu.Lock()
	got := strings.Join(events, ",")
	mu.Unlock()
	if !strings.Contains(got, "main:crashed") {
		t.Errorf("events = %q, want main:crashed", got)
	}
	b2, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if b2 == b || !b2.alive() {
		t.Fatal("Get didn't launch a new browser")
	}
	tab2, err := b2.Tab("main")
	if err != nil {
		t.Fatal(err)
	}
	if err := tab2.Navigate("about:blank"); err != nil {
		t.Fatal(err)
	}
	// A view subscribing to the dead tab is ended at once.
	if f, ok := <-func() <-chan []byte { ch, _ := tab.Subscribe(); return ch }(); ok {
		t.Errorf("subscribed to a dead tab and got %d bytes", len(f))
	}
}
