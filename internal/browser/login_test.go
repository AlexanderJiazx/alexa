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
	const secret = "hunter2-Correct-Horse"

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

	// When the page reveals the password, the model still doesn't see it.
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
		if strings.Contains(text, secret) {
			t.Errorf("%s shows the password:\n%s", what, text)
		}
	}
	if !strings.Contains(st.Text, `<input type=text placeholder="Password"> value=••••`) || !strings.Contains(st.Text, "You typed ••••") {
		t.Errorf("the revealed password should read as ••••:\n%s", st.Text)
	}
	if got, err := tab.Eval(`getComputedStyle(document.getElementById('pass')).webkitTextSecurity`); err != nil || got != "disc" {
		t.Errorf("the revealed field isn't masked on screen: %q, %v", got, err)
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
	if _, title := tab.URL(); title != "clicked" {
		if st := mustState(t, tab); st.Title != "clicked" {
			t.Errorf("the click didn't land: title %q", st.Title)
		}
	}
}
