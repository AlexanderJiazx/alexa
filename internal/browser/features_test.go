package browser

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newFeatureBrowser serves pages on 127.0.0.1 and returns a tab plus the
// server's port, so a page can embed a cross-origin frame from localhost.
func newFeatureBrowser(t *testing.T, pages map[string]string) (*Tab, int) {
	t.Helper()
	if os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.HasSuffix(r.URL.Path, ".csv") {
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", "attachment")
		}
		fmt.Fprint(w, strings.ReplaceAll(body, "PORT", fmt.Sprint(port)))
	})}
	go srv.Serve(ln)
	dir := chromeDir(t)
	m := NewManager(Options{Headless: true, Width: 1280, Height: 800, IdleAfter: time.Hour, ChromePath: os.Getenv("CHROME_PATH")},
		func(id int64) UserEnv {
			return UserEnv{UserID: id, ProfileDir: dir + "/profile", DownloadDir: dir + "/dl", HomeDir: dir}
		}, nil)
	t.Cleanup(func() { m.CloseAll(); srv.Close() })
	b, err := m.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := b.Tab("main")
	if err != nil {
		t.Fatal(err)
	}
	return tab, port
}

// indexOf returns the index of the nth element line containing needle.
func indexOf(t *testing.T, st *State, needle string, nth int) int {
	t.Helper()
	seen := 0
	for _, line := range strings.Split(st.Text, "\n") {
		if strings.HasPrefix(line, "[") && strings.Contains(line, needle) {
			if seen == nth {
				var n int
				fmt.Sscanf(line, "[%d]", &n)
				return n
			}
			seen++
		}
	}
	t.Fatalf("no element %q (#%d) in state:\n%s", needle, nth, st.Text)
	return 0
}

func mustState(t *testing.T, tab *Tab) *State {
	t.Helper()
	st, err := tab.State()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestValueInputs(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>Form</title><body>
		<input id=d type=date aria-label="Date"> <input id=tm type=time aria-label="Time">
		<input id=r type=range min=0 max=5 step=0.5 value=0 aria-label="Level">
		<input placeholder="Code" maxlength=4> <p id=ev></p>
		<script>for (const id of ['d','tm','r']) document.getElementById(id).addEventListener('change', e => ev.textContent += id + '=' + e.target.value + ';');</script>`})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, tab)
	for _, c := range []struct{ field, in, want string }{
		{`aria-label="Date"`, "November 15, 2026", "2026-11-15"},
		{`aria-label="Time"`, "7:30 pm", "19:30"},
		{`aria-label="Level"`, "4", "4"},
	} {
		note, err := tab.Type(indexOf(t, st, c.field, 0), c.in, true, false)
		if err != nil {
			t.Fatalf("type %s: %v", c.field, err)
		}
		if !strings.Contains(note, c.want) {
			t.Errorf("type %s: note %q doesn't mention %s", c.field, note, c.want)
		}
	}
	if _, err := tab.Type(indexOf(t, st, `aria-label="Date"`, 0), "someday", true, false); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Errorf("a bad date should say the format, got %v", err)
	}
	note, err := tab.Type(indexOf(t, st, `placeholder="Code"`, 0), "555-0100", true, false)
	if err != nil || !strings.Contains(note, `"555-"`) {
		t.Errorf("a truncated field should be reported, got %q, %v", note, err)
	}
	st = mustState(t, tab)
	if !strings.Contains(st.Text, "d=2026-11-15;tm=19:30;r=4;") {
		t.Errorf("change events didn't fire as expected:\n%s", st.Text)
	}
}

const innerPage = `<!doctype html><title>Inner</title><body><p>Inner text: book a table</p>
	<input placeholder="Your name"> <button onclick="out.textContent = 'Booked for ' + document.querySelector('input').value">Book</button><p id=out></p>`

func TestFrames(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/": `<!doctype html><title>Outer</title><body><h1>Outer</h1>
			<iframe src="/inner" title="Same site" style="width:500px;height:150px"></iframe>
			<iframe src="http://localhost:PORT/inner" title="Booking widget" style="width:500px;height:150px"></iframe>`,
		"/inner": innerPage,
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	st := mustState(t, tab)
	if !strings.Contains(st.Text, `(in frame "Same site")`) || !strings.Contains(st.Text, `(in frame "Booking widget")`) {
		t.Fatalf("frame elements missing:\n%s", st.Text)
	}
	for _, frame := range []string{"Same site", "Booking widget"} {
		st = mustState(t, tab)
		name := fmt.Sprintf(`placeholder="Your name"> value="" (in frame %q)`, frame)
		if _, err := tab.Type(indexOf(t, st, name, 0), "Alex "+frame, true, false); err != nil {
			t.Fatalf("type in %s: %v", frame, err)
		}
		st = mustState(t, tab)
		if _, err := tab.Click(indexOf(t, st, fmt.Sprintf(`Book (in frame %q)`, frame), 0)); err != nil {
			t.Fatalf("click in %s: %v", frame, err)
		}
	}
	st = mustState(t, tab)
	for _, want := range []string{"Booked for Alex Same site", "Booked for Alex Booking widget"} {
		if !strings.Contains(st.Text, want) {
			t.Errorf("missing %q in visible text:\n%s", want, st.Text)
		}
	}
	md, _, err := tab.Read(0, 20000)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(md, "Inner text: book a table") != 2 {
		t.Errorf("read should include both frames:\n%s", md)
	}
}

func TestDialogs(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>Dialogs</title><body>
		<button onclick="c.textContent = confirm('Delete it?') ? 'deleted' : 'kept'">Delete</button>
		<button onclick="p.textContent = 'You entered: ' + prompt('Name?', 'anon')">Ask name</button>
		<button onclick="alert('Saved')">Save</button><p id=c></p><p id=p></p>`})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, tab)
	del, ask, save := indexOf(t, st, "Delete", 0), indexOf(t, st, "Ask name", 0), indexOf(t, st, "Save", 0)
	t0 := time.Now()
	if _, err := tab.Click(del); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("a click that opens a confirm took %s", d)
	}
	st = mustState(t, tab)
	if !strings.Contains(st.Text, "confirm dialog") || !strings.Contains(st.Text, "Delete it?") {
		t.Fatalf("state should show the confirm:\n%s", st.Text)
	}
	if _, err := tab.Click(save); err == nil {
		t.Error("actions should be refused while a dialog is open")
	}
	if _, err := tab.AnswerDialog(false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.Click(ask); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.AnswerDialog(true, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.Click(save); err != nil {
		t.Fatal(err)
	}
	st = mustState(t, tab)
	for _, want := range []string{"kept", "You entered: hello", `alert dialog: "Saved"`} {
		if !strings.Contains(st.Text, want) {
			t.Errorf("missing %q:\n%s", want, st.Text)
		}
	}
}

func TestHoverAndDrag(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>Pointer</title><body style="margin:0">
		<div id=h style="position:absolute;left:0;top:0;width:200px;height:50px;background:#eee">Hover me</div>
		<p id=tip style="position:absolute;left:0;top:60px;display:none">Secret tooltip</p>
		<div id=a draggable=true style="position:absolute;left:0;top:120px;width:100px;height:100px;background:#fcc">A</div>
		<div id=b style="position:absolute;left:0;top:260px;width:100px;height:100px;background:#ccf">B</div>
		<div id=knob style="position:absolute;left:300px;top:120px;width:40px;height:40px;background:#cfc;cursor:pointer">Knob</div>
		<p id=res style="position:absolute;left:400px;top:300px"></p>
		<script>
		h.onmouseover = () => tip.style.display = 'block';
		a.addEventListener('dragstart', e => e.dataTransfer.setData('text/plain', 'A'));
		b.addEventListener('dragover', e => e.preventDefault());
		b.addEventListener('drop', e => { e.preventDefault(); res.textContent += 'dropped ' + e.dataTransfer.getData('text/plain') + ' '; });
		let drag = false; knob.onmousedown = () => drag = true; document.onmouseup = () => drag = false;
		document.onmousemove = e => { if (drag) { knob.style.left = (e.clientX - 20) + 'px'; res.textContent += 'knob ' + e.clientX + ' '; } };
		</script>`})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	if err := tab.Hover(0, 100, 25); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, tab)
	if !strings.Contains(st.Text, "Secret tooltip") {
		t.Errorf("hover didn't show the tooltip:\n%s", st.Text)
	}
	if err := tab.Drag(0, 50, 170, 0, 50, 310); err != nil {
		t.Fatal(err)
	}
	if err := tab.Drag(indexOf(t, st, "Knob", 0), 0, 0, 0, 600, 140); err != nil {
		t.Fatal(err)
	}
	st = mustState(t, tab)
	if !strings.Contains(st.Text, "dropped A") || !strings.Contains(st.Text, "knob 600") {
		t.Errorf("drags didn't land:\n%s", st.Text)
	}
}

func TestFrameAndHumanDialogs(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/": `<!doctype html><title>Outer</title><body style="margin:0">
			<button style="position:absolute;left:10px;top:10px;width:120px;height:40px" onclick="r.textContent = confirm('Sure?') ? 'yes' : 'no'">Mine</button><p id=r style="position:absolute;left:10px;top:60px"></p>
			<iframe src="http://localhost:PORT/inner" title="Widget" style="position:absolute;left:0;top:120px;width:500px;height:150px"></iframe>`,
		"/inner": `<!doctype html><title>Inner</title><body><button onclick="out.textContent = confirm('Cancel booking?') ? 'cancelled' : 'still booked'">Cancel booking</button><p id=out></p>`,
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	st := mustState(t, tab)
	if _, err := tab.Click(indexOf(t, st, `Cancel booking (in frame "Widget")`, 0)); err != nil {
		t.Fatal(err)
	}
	if st = mustState(t, tab); !strings.Contains(st.Text, "Cancel booking?") {
		t.Fatalf("the frame's confirm should be held:\n%s", st.Text)
	}
	if _, err := tab.AnswerDialog(false, ""); err != nil {
		t.Fatal(err)
	}
	// A person in control clicks their own button: their confirm goes ahead.
	for _, kind := range []string{"mousedown", "mouseup"} {
		if err := tab.Human(HumanInput{Kind: kind, X: 70, Y: 30, Button: "left", Clicks: 1}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	st = mustState(t, tab)
	if !strings.Contains(st.Text, "still booked") || !strings.Contains(st.Text, "yes") || tab.pendingDialog() != nil {
		t.Errorf("want the frame's dialog dismissed and the person's accepted:\n%s", st.Text)
	}
}

func TestFramePartlyInView(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/": `<!doctype html><title>Long</title><body style="margin:0">
			<iframe src="http://localhost:PORT/inner" title="Widget" style="position:absolute;left:0;top:700px;width:500px;height:300px;border:0"></iframe>
			<div style="height:3000px"></div>`,
		"/inner": `<!doctype html><title>Inner</title><body style="margin:0"><button style="margin-top:20px" onclick="out.textContent = 'pressed'">Press</button><p id=out></p>`,
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	st := mustState(t, tab)
	if _, err := tab.Click(indexOf(t, st, `Press (in frame "Widget")`, 0)); err != nil {
		t.Fatal(err)
	}
	if md, _, err := tab.Read(0, 10000); err != nil || !strings.Contains(md, "pressed") {
		t.Errorf("the click inside the frame missed: %v\n%s", err, md)
	}
}

func TestScriptLinksAreIndexed(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>Calendar</title><body>
		<div id=cal><a class="next" title="Next"><span>Next</span></a> <span id=m>October</span></div>
		<a name="top"></a>
		<script>document.getElementById('cal').addEventListener('click', e => { if (e.target.closest('.next')) m.textContent = 'November'; });</script>`})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, tab)
	if _, err := tab.Click(indexOf(t, st, `<a title="Next"> Next`, 0)); err != nil {
		t.Fatal(err)
	}
	if st = mustState(t, tab); !strings.Contains(st.Text, "November") {
		t.Errorf("clicking the script link should work:\n%s", st.Text)
	}
	if strings.Count(st.Text, "<a") != 1 {
		t.Errorf("an empty named anchor shouldn't be listed:\n%s", st.Text)
	}
}

// A download a click starts is noted in the next state under the name it
// was saved as, once.
func TestDownloadNoted(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/":          `<!doctype html><title>Files</title><body><a href="/usage.csv">Usage (CSV)</a>`,
		"/usage.csv": "date,kwh\n2026-08-01,21.1\n",
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.Click(indexOf(t, mustState(t, tab), "Usage (CSV)", 0)); err != nil {
		t.Fatal(err)
	}
	if st := mustState(t, tab); !strings.Contains(st.Text, "downloaded usage.csv to downloads/") {
		t.Errorf("the state doesn't say it downloaded usage.csv:\n%s", st.Text)
	}
	if b, err := os.ReadFile(filepath.Join(tab.b.env.DownloadDir, "usage.csv")); err != nil || !strings.Contains(string(b), "2026-08-01,21.1") {
		t.Errorf("usage.csv holds %q, %v", b, err)
	}
	if again := mustState(t, tab); strings.Contains(again.Text, "downloaded") {
		t.Errorf("the download was noted twice:\n%s", again.Text)
	}
}

// A finished download takes its name without replacing a file by that name.
func TestPlaceDownload(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("bill.pdf", "first")
	for i, want := range []string{"bill-2.pdf", "bill-3.pdf"} {
		from := write(fmt.Sprintf("guid-%d", i), "next")
		if got := place(from, dir, "bill.pdf"); got != want {
			t.Errorf("placed as %q, want %q", got, want)
		}
		if _, err := os.Stat(from); !os.IsNotExist(err) {
			t.Errorf("%s is still there", from)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "bill.pdf")); string(b) != "first" {
		t.Errorf("bill.pdf was replaced: %q", b)
	}
}

// A popup that closes itself hands the tab back to the page that opened it.
func TestPopupClosesItself(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/":      `<!doctype html><title>Product</title><body><a href="/guide" target="_blank">Size guide</a>`,
		"/guide": `<!doctype html><title>Guide</title><body><p>M fits 96–104 cm</p><button onclick="window.close()">Close</button>`,
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.Click(indexOf(t, mustState(t, tab), "Size guide", 0)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	st := mustState(t, tab)
	if st.Title != "Guide" {
		t.Fatalf("the popup wasn't followed: %q", st.Title)
	}
	if _, err := tab.Click(indexOf(t, st, "Close", 0)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	t0 := time.Now()
	if st := mustState(t, tab); st.Title != "Product" || strings.Contains(st.Text, "popup window") {
		t.Errorf("after the popup closed, the tab is on %q:\n%s", st.Title, st.Text)
	}
	if time.Since(t0) > 5*time.Second {
		t.Errorf("reading the page took %s", time.Since(t0))
	}
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/guide", port)); err != nil {
		t.Errorf("the tab can't navigate after the popup closed: %v", err)
	}
}
