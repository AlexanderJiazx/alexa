package browser

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"testing"
	"time"
)

// onSite allows a login into pages on one host.
func onSite(host string) func(string) error {
	return func(page string) error {
		if u, err := url.Parse(page); err != nil || u.Hostname() != host {
			return fmt.Errorf("%s isn't on %s", page, host)
		}
		return nil
	}
}

func TestFillLogin(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/": `<!doctype html><title>Sign in</title><body>
			<form onsubmit="event.preventDefault(); out.textContent = 'Signed in as ' + user.value + ' with a ' + pass.value.length + '-character password'">
			<input id=user type=email placeholder="Email"> <input id=pass type=password placeholder="Password">
			<button type=button onclick="pass.type = pass.type === 'password' ? 'text' : 'password'">Show password</button>
			<button type=button onclick="echo.textContent = 'You typed ' + pass.value">Echo</button>
			<button>Sign in</button></form><p id=out></p><p id=echo></p>
			<iframe src="http://localhost:PORT/frame" title="Other site" style="width:400px;height:80px"></iframe>`,
		"/frame": `<!doctype html><body><input type=password placeholder="Frame password">`,
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	st := mustState(t, tab)
	user, pass := indexOf(t, st, `placeholder="Email"`, 0), indexOf(t, st, `placeholder="Password"`, 0)
	frame := indexOf(t, st, `placeholder="Frame password"`, 0)
	// Markdown and HTML would escape some of these.
	const secret = "hunter2_Correct*Horse<3"

	// Each field is vetted on the page it's on, not the one around it.
	if u, err := tab.FieldURL(frame); err != nil || !strings.HasPrefix(u, "http://localhost:") {
		t.Fatalf("the frame's field is on %q, %v", u, err)
	}
	if err := tab.Fill(frame, secret, true, onSite("127.0.0.1")); err == nil {
		t.Fatal("a password went into a frame from another site")
	}
	if err := tab.Fill(pass, secret, true, onSite("example.com")); err == nil {
		t.Fatal("a login for another site was typed in")
	}
	// A password only goes into a password field, and a username not into one.
	if err := tab.Fill(user, secret, true, onSite("127.0.0.1")); err == nil {
		t.Fatal("a password went into the email field")
	}
	if err := tab.Fill(pass, "alice@example.com", false, onSite("127.0.0.1")); err == nil {
		t.Fatal("a username went into the password field")
	}

	if err := tab.Fill(user, "alice@example.com", false, onSite("127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if err := tab.Fill(pass, secret, true, onSite("127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if err := tab.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	st = mustState(t, tab)
	if want := fmt.Sprintf("Signed in as alice@example.com with a %d-character password", len(secret)); !strings.Contains(st.Text, want) {
		t.Fatalf("the form didn't get the login:\n%s", st.Text)
	}
	if len(st.Screenshot) == 0 {
		t.Error("the screenshot was left out though the password is masked")
	}

	// When the page shows the password, the model still doesn't see it: not
	// in the state, not in the page's text (where markdown and HTML escape
	// it), not in a screenshot.
	if _, err := tab.Click(indexOf(t, st, "Show password", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.Click(indexOf(t, mustState(t, tab), "Echo", 0)); err != nil {
		t.Fatal(err)
	}
	st = mustState(t, tab)
	md, _, err := tab.Read(0, 20000)
	if err != nil {
		t.Fatal(err)
	}
	for what, text := range map[string]string{"the state": st.Text, "the page text": md} {
		if strings.Contains(text, "Correct") || strings.Contains(text, "Horse") {
			t.Errorf("%s shows the password:\n%s", what, text)
		}
	}
	if !strings.Contains(st.Text, `<input type=text placeholder="Password"> value=••••`) || !strings.Contains(st.Text, "You typed ••••") {
		t.Errorf("the revealed password should read as ••••:\n%s", st.Text)
	}
	if len(st.Screenshot) != 0 || !strings.Contains(st.Text, "No screenshot") {
		t.Errorf("a screenshot showing the password was kept (%d bytes)", len(st.Screenshot))
	}
	if got, err := tab.Eval(`getComputedStyle(document.getElementById('pass')).webkitTextSecurity`); err != nil || got != "disc" {
		t.Errorf("the revealed field isn't masked on screen: %q, %v", got, err)
	}
	if got := tab.Redact("pass=hunter2_Correct%2AHorse%3C3, or hunter2\\_Correct\\*Horse&lt;3, or hunter2_Correct*H…"); strings.Contains(got, "Correct") {
		t.Errorf("escaped forms weren't redacted: %s", got)
	}
}

// A page can dress up its own field as another site's; the login still
// only goes where the field really is.
func TestFillSpoofedField(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>Evil</title><body>
		<input id=pw type=password placeholder="Bank password">
		<script>
		  const fake = { location: { href: 'https://example.com/login' }, defaultView: window,
		    get activeElement() { return document.activeElement }, hasFocus: () => document.hasFocus() };
		  Object.defineProperty(pw, 'ownerDocument', { get: () => fake });
		</script>`})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	pw := indexOf(t, mustState(t, tab), `placeholder="Bank password"`, 0)
	if u, err := tab.FieldURL(pw); err != nil || !strings.HasPrefix(u, "http://127.0.0.1:") {
		t.Fatalf("the field is on %q, %v; want the real page", u, err)
	}
	if err := tab.Fill(pw, "bank-secret-1", true, onSite("example.com")); err == nil {
		t.Fatal("a login for example.com went into a page only claiming to be it")
	}
	if got, _ := tab.Eval(`document.getElementById('pw').value`); got != "" {
		t.Fatalf("the field holds %q", got)
	}
}

// Frameworks like React keep their own copy of a field's value and take
// changes only from input events; a filled login has to reach that copy.
func TestFillTrackedField(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>App</title><body>
		<form onsubmit="event.preventDefault(); out.textContent = 'state ' + state.user + ' / ' + state.pass.length">
		<input id=ru type=email placeholder="Email"> <input id=rp type=password placeholder="Password"> <button>Go</button></form><p id=out></p>
		<script>
		  const state = { user: '', pass: '' };
		  const native = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
		  for (const [el, key] of [[ru, 'user'], [rp, 'pass']]) {
		    let tracked = '';
		    Object.defineProperty(el, 'value', { get() { return native.get.call(this) }, set(v) { tracked = v; native.set.call(this, v) } });
		    el.addEventListener('input', () => { const v = native.get.call(el); if (v !== tracked) { tracked = v; state[key] = v } });
		  }
		</script>`})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	st := mustState(t, tab)
	if err := tab.Fill(indexOf(t, st, `placeholder="Email"`, 0), "alice@example.com", false, onSite("127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if err := tab.Fill(indexOf(t, st, `placeholder="Password"`, 0), "tracked-pass", true, onSite("127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if err := tab.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if st := mustState(t, tab); !strings.Contains(st.Text, "state alice@example.com / 12") {
		t.Fatalf("the app's own state didn't get the login:\n%s", st.Text)
	}
}

// With someone watching the live view, an action waits for the pointer to
// glide to its target before it happens.
func TestCursorGlide(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{"/": `<!doctype html><title>Far</title><body style="margin:0">
		<button style="position:absolute; right:20px; bottom:20px" onclick="document.title = 'clicked'">Far away</button>`})
	if x, y := tab.Cursor(); x != 640 || y != 400 {
		t.Fatalf("the pointer starts at %v,%v, want the middle", x, y)
	}
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	btn := indexOf(t, mustState(t, tab), "Far away", 0)
	_, stop := tab.Subscribe()
	defer stop()
	x0, y0 := tab.Cursor()
	t0 := time.Now()
	if _, err := tab.Click(btn); err != nil {
		t.Fatal(err)
	}
	took := time.Since(t0)
	x1, y1 := tab.Cursor()
	if want := glide(math.Hypot(x1-x0, y1-y0)); want < 300*time.Millisecond || took < want {
		t.Errorf("the click took %s, want at least the %s glide", took, want)
	}
	if st := mustState(t, tab); st.Title != "clicked" {
		t.Errorf("the click didn't land: title %q", st.Title)
	}
}
