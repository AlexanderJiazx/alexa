package browser

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/chromedp/cdproto/input"
)

// Signing in with a saved login. The agent says which fields to fill, but
// the values come from the vault and never pass through the model: each
// goes in only if the field's own page (an embedded frame's, for a field in
// one) is one the login may go into, a password only goes into a password
// field, and from then on it's kept out of everything the model is shown,
// even if the page reveals it.

// FieldURL returns the address of the page element idx is on.
func (t *Tab) FieldURL(idx int) (string, error) {
	defer t.begin()()
	var res struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := t.evalAt(idx, `return {ok: true, url: el.ownerDocument.location.href};`, &res); err != nil {
		return "", err
	}
	if !res.OK {
		return "", errNoElement
	}
	return res.URL, nil
}

// The input types a username can go into.
var usernameTypes = map[string]bool{"text": true, "email": true, "tel": true, "number": true}

type okResult struct {
	OK bool `json:"ok"`
}

// Fill types a login's username, or with password set its password, into
// input element idx. allow vets the address of the page the field is on,
// both before and right before typing.
func (t *Tab) Fill(idx int, value string, password bool, allow func(pageURL string) error) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	var info struct {
		OK   bool   `json:"ok"`
		URL  string `json:"url"`
		Tag  string `json:"tag"`
		Type string `json:"type"`
	}
	if err := t.evalAt(idx, `return {ok: true, url: el.ownerDocument.location.href, tag: el.tagName.toLowerCase(), type: (el.type || '').toLowerCase()};`, &info); err != nil {
		return err
	}
	if !info.OK {
		return errNoElement
	}
	if err := allow(info.URL); err != nil {
		return err
	}
	switch {
	case info.Tag != "input":
		return fmt.Errorf("element [%d] isn't a text field", idx)
	case password && info.Type != "password":
		return fmt.Errorf("element [%d] isn't a password field, and a password only goes into one", idx)
	case !password && !usernameTypes[info.Type]:
		return fmt.Errorf("element [%d] isn't a username or email field", idx)
	}
	x, y, _, err := t.elementCenter(idx)
	if err != nil {
		return err
	}
	t.setCursor(x, y, true, true)
	if err := t.mouseClick(x, y, input.Left, 1); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	// Typing goes wherever the focus is: make sure that's this field, still
	// on a page the login may go into, with its old text selected.
	var ready struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := t.evalAt(idx, `const doc = el.ownerDocument;
		if (doc.activeElement !== el) el.focus();
		if (doc.activeElement !== el || !doc.hasFocus()) return {ok: false};
		el.select();
		return {ok: true, url: doc.location.href};`, &ready); err != nil {
		return err
	}
	if !ready.OK {
		return fmt.Errorf("couldn't put the cursor in element [%d]", idx)
	}
	if err := allow(ready.URL); err != nil {
		return err
	}
	if password {
		t.addSecret(value)
	}
	if err := t.input(10*time.Second, input.InsertText(value)); err != nil {
		return err
	}
	// Some fields only take a value set the way autofill does it.
	if !t.holds(idx, value, password) {
		var res okResult
		_ = t.evalAt(idx, fmt.Sprintf(`const set = Object.getOwnPropertyDescriptor(el.ownerDocument.defaultView.HTMLInputElement.prototype, 'value').set;
			set.call(el, %s);
			el.dispatchEvent(new Event('input', {bubbles: true}));
			el.dispatchEvent(new Event('change', {bubbles: true}));
			return {ok: true};`, jsString(value)), &res)
		if !t.holds(idx, value, password) {
			what := "username"
			if password {
				what = "password"
			}
			return fmt.Errorf("element [%d] didn't take the %s", idx, what)
		}
	}
	if password {
		// Keep it masked if the page turns the field into a text field
		// ("show password"): on screen, and in the element list.
		var res okResult
		_ = t.evalAt(idx, `el.__aaSecret = true; el.style.setProperty('-webkit-text-security', 'disc', 'important'); return {ok: true};`, &res)
	}
	return nil
}

// holds reports whether input element idx now holds value. For a password
// only its length is compared, so it never leaves the page.
func (t *Tab) holds(idx int, value string, password bool) bool {
	var got struct {
		OK    bool   `json:"ok"`
		N     int    `json:"n"`
		Value string `json:"value"`
	}
	js := `return {ok: true, n: el.value.length};`
	if !password {
		js = `return {ok: true, n: el.value.length, value: el.value};`
	}
	if t.evalAt(idx, js, &got) != nil || !got.OK {
		return false
	}
	if password {
		return got.N == len(utf16.Encode([]rune(value)))
	}
	// Fields may format or lowercase what's typed.
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }
	return norm(got.Value) == norm(value) || (got.Value != "" && strings.Contains(norm(got.Value), norm(value)))
}

// addSecret remembers a password typed into the page, to keep it out of
// what the model is shown. Very short ones would blank out ordinary words.
func (t *Tab) addSecret(s string) {
	if len([]rune(s)) < 4 {
		return
	}
	t.mu.Lock()
	if !slices.Contains(t.secrets, s) {
		t.secrets = append(t.secrets, s)
	}
	t.mu.Unlock()
}

// Redact hides the passwords typed into the tab in text meant for the
// model.
func (t *Tab) Redact(s string) string {
	t.mu.Lock()
	secrets := t.secrets
	t.mu.Unlock()
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec, "••••")
	}
	return s
}
