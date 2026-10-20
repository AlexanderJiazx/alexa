package browser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const testPage = `<!doctype html><html><head><title>Test Shop</title></head><body>
<h1>Welcome</h1>
<a href="/next">Next page</a>
<form action="/search"><input name="q" placeholder="Search"><button>Go</button></form>
<select id="s"><option>Red</option><option>Blue</option></select>
<div style="cursor:pointer" onclick="document.title='clicked'">Clickable div</div>
<a href="/popup" target="_blank">Popup link</a>
<div style="height:3000px"></div><button>Bottom button</button>
</body></html>`

func newTestBrowser(t *testing.T) (*Manager, *httptest.Server) {
	t.Helper()
	if os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/next":
			fmt.Fprint(w, `<html><head><title>Next</title></head><body><p>Second page</p></body></html>`)
		case "/search":
			fmt.Fprintf(w, `<html><head><title>Results</title></head><body><p>Results for %s</p></body></html>`, r.URL.Query().Get("q"))
		case "/popup":
			fmt.Fprint(w, `<html><head><title>Popup</title></head><body><p>Popup content</p></body></html>`)
		default:
			fmt.Fprint(w, testPage)
		}
	}))
	dir := t.TempDir()
	m := NewManager(Options{Headless: true, Width: 1280, Height: 800, IdleAfter: time.Hour},
		func(id int64) UserEnv {
			return UserEnv{UserID: id, ProfileDir: dir + "/profile", DownloadDir: dir + "/dl", HomeDir: dir}
		}, nil)
	t.Cleanup(func() { m.CloseAll(); srv.Close() })
	return m, srv
}

func TestBrowserFlow(t *testing.T) {
	m, srv := newTestBrowser(t)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := b.Tab("task1")
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(srv.URL); err != nil {
		t.Fatal(err)
	}
	st, err := tab.State()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(st.Text)
	if !strings.Contains(st.Text, "Next page") || !strings.Contains(st.Text, "placeholder=\"Search\"") || !strings.Contains(st.Text, "Clickable div") {
		t.Fatalf("missing elements in state:\n%s", st.Text)
	}
	if len(st.Screenshot) < 1000 {
		t.Fatal("screenshot too small")
	}
	idx := func(st *State, needle string) int {
		for _, line := range strings.Split(st.Text, "\n") {
			if strings.Contains(line, needle) && strings.HasPrefix(line, "[") {
				var n int
				fmt.Sscanf(line, "[%d]", &n)
				return n
			}
		}
		t.Fatalf("no element %q", needle)
		return 0
	}
	// type + submit
	if err := tab.Type(idx(st, "Search"), "shoes", true, true); err != nil {
		t.Fatal(err)
	}
	if u, _ := tab.URL(); !strings.Contains(u, "q=shoes") {
		t.Fatalf("search did not submit, url=%s", u)
	}
	if err := tab.History(-1); err != nil {
		t.Fatal(err)
	}
	st, _ = tab.State()
	if _, err := tab.Select(idx(st, "options:"), "blue"); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.Click(idx(st, "Clickable div")); err != nil {
		t.Fatal(err)
	}
	st, _ = tab.State()
	if st.Title != "clicked" {
		t.Fatalf("div click failed: title=%q", st.Title)
	}
	if err := tab.Scroll("down", 5, 0); err != nil {
		t.Fatal(err)
	}
	st, _ = tab.State()
	if !strings.Contains(st.Text, "Bottom button") {
		t.Fatalf("scroll failed:\n%s", st.Text)
	}
	md, _, err := tab.Read(0, 5000)
	if err != nil || !strings.Contains(md, "Welcome") {
		t.Fatalf("read failed: %v %q", err, md)
	}
	// popup follow
	_ = tab.Scroll("top", 0, 0)
	st, _ = tab.State()
	if _, err := tab.Click(idx(st, "Popup link")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	st, _ = tab.State()
	if st.Title != "Popup" {
		t.Fatalf("popup not followed: %q", st.Title)
	}
	// screencast
	ch, stop := tab.Subscribe()
	select {
	case f := <-ch:
		if len(f) < 500 {
			t.Fatal("small frame")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no frame")
	}
	stop()
}

// TestHumanInput drives the tab the way the take-over UI does: raw mouse
// events on a link, then back and forward through history.
func TestHumanInput(t *testing.T) {
	m, srv := newTestBrowser(t)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := b.Tab("human")
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(srv.URL); err != nil {
		t.Fatal(err)
	}
	var r struct{ X, Y float64 }
	if err := tab.eval(`(() => { const b = [...document.querySelectorAll('a')].find(a => a.textContent === 'Next page').getBoundingClientRect(); return {X: b.x + b.width/2, Y: b.y + b.height/2} })()`, &r); err != nil {
		t.Fatal(err)
	}
	waitURL := func(suffix string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if u, _ := tab.URL(); strings.HasSuffix(u, suffix) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		u, _ := tab.URL()
		t.Fatalf("url = %q, want suffix %q", u, suffix)
	}
	for _, ev := range []HumanInput{{Kind: "mousemove", X: r.X, Y: r.Y}, {Kind: "mousedown", X: r.X, Y: r.Y, Button: "left", Clicks: 1}, {Kind: "mouseup", X: r.X, Y: r.Y, Button: "left", Clicks: 1}} {
		if err := tab.Human(ev); err != nil {
			t.Fatal(err)
		}
	}
	waitURL("/next")
	start := time.Now()
	if err := tab.History(-1); err != nil {
		t.Fatal(err)
	}
	waitURL("/")
	if err := tab.History(1); err != nil {
		t.Fatal(err)
	}
	waitURL("/next")
	t.Logf("back+forward took %s", time.Since(start))
	if err := tab.History(1); err == nil {
		t.Fatal("expected an error going forward past the end")
	}
}

// The agent and a live view often ask for the session's tab at the same
// moment; they must get the same tab, or the user watches a blank page.
func TestTabConcurrentOpen(t *testing.T) {
	m, srv := newTestBrowser(t)
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	tabs := make(chan *Tab, 4)
	for i := 0; i < 4; i++ {
		go func() {
			tab, err := b.Tab("main")
			if err != nil {
				t.Error(err)
			}
			tabs <- tab
		}()
	}
	first := <-tabs
	for i := 1; i < 4; i++ {
		if tab := <-tabs; tab != first {
			t.Fatal("concurrent Tab calls opened different tabs")
		}
	}
	// Frames from the tab the agent drives reach a viewer subscribed earlier.
	frames, stop := first.Subscribe()
	defer stop()
	if err := first.Navigate(srv.URL); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case f := <-frames:
			if len(f) > 2000 { // a blank page encodes to a tiny JPEG
				return
			}
		case <-deadline:
			t.Fatal("no frame of the loaded page")
		}
	}
}
