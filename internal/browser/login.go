package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Signing in with a saved login. The agent says which fields to fill, but
// the values come from the vault and never pass through the model.
//
// A page's scripts can't be trusted to say where a field is: one can dress
// up its own field as another site's. So each field is looked at from an
// isolated world of the frame its document is in, out of the page's reach,
// and a login goes in only if that document is on the login's site. The
// value is set there the way password managers do it, so it lands in that
// field whatever has the focus, and a password only goes into a password
// field. From then on the password is kept out of everything the model is
// shown, even if the page shows it.

// field is an input element seen from an isolated world.
type field struct {
	ctx context.Context // the target its frame is in
	obj runtime.RemoteObjectID
	url string // the address of the document it's in
}

// field finds element idx of the latest state from an isolated world of
// the frame whose document it is in.
func (t *Tab) field(idx int) (*field, error) {
	r, err := t.ref(idx)
	if err != nil {
		return nil, err
	}
	if r.local < 0 {
		return nil, fmt.Errorf("element [%d] is an embedded frame that couldn't be read", idx)
	}
	tctx := t.active().ctx
	if r.frame != nil {
		tctx = r.frame.ctx
	}
	ctx, cancel := context.WithTimeout(tctx, 15*time.Second)
	defer cancel()
	var f *field
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// The page's own list says which node; anything it says about the
		// node is checked below.
		el, exc, err := runtime.Evaluate(fmt.Sprintf(`(window.__aaEls||[])[%d]`, r.local)).Do(ctx)
		if err != nil {
			return err
		}
		if exc != nil || el == nil || el.ObjectID == "" {
			return errNoElement
		}
		node, err := dom.DescribeNode().WithObjectID(el.ObjectID).Do(ctx)
		_ = runtime.ReleaseObject(el.ObjectID).Do(ctx)
		if err != nil {
			return errNoElement
		}
		tree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		for _, fr := range frameList(tree) {
			world, err := page.CreateIsolatedWorld(fr.Frame.ID).WithWorldName("alexa-login").Do(ctx)
			if err != nil {
				continue
			}
			obj, err := dom.ResolveNode().WithBackendNodeID(node.BackendNodeID).WithExecutionContextID(world).Do(ctx)
			if err != nil || obj == nil || obj.ObjectID == "" {
				continue
			}
			var u *string
			if callOn(ctx, obj.ObjectID, `function () { return this.ownerDocument === document ? document.location.href : null; }`, &u) == nil && u != nil {
				f = &field{ctx: tctx, obj: obj.ObjectID, url: *u}
				return nil
			}
			_ = runtime.ReleaseObject(obj.ObjectID).Do(ctx)
		}
		return fmt.Errorf("couldn't tell which page element [%d] is on", idx)
	}))
	return f, err
}

func frameList(tree *page.FrameTree) []*page.FrameTree {
	if tree == nil || tree.Frame == nil {
		return nil
	}
	out := []*page.FrameTree{tree}
	for _, c := range tree.ChildFrames {
		out = append(out, frameList(c)...)
	}
	return out
}

// call runs fn with this bound to the field, in its isolated world. The
// world goes when its document does, so a call can't reach a page the
// field's address was not checked for.
func (f *field) call(fn string, out any, args ...any) error {
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return callOn(ctx, f.obj, fn, out, args...)
	}))
}

func (f *field) release() {
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	_ = chromedp.Run(ctx, runtime.ReleaseObject(f.obj))
}

func callOn(ctx context.Context, obj runtime.RemoteObjectID, fn string, out any, args ...any) error {
	var cargs []*runtime.CallArgument
	for _, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		cargs = append(cargs, &runtime.CallArgument{Value: b})
	}
	res, exc, err := runtime.CallFunctionOn(fn).WithObjectID(obj).WithArguments(cargs).WithReturnByValue(true).Do(ctx)
	if err != nil {
		return err
	}
	if exc != nil {
		return errors.New("the page's script failed")
	}
	if out == nil || res == nil || len(res.Value) == 0 {
		return nil
	}
	return json.Unmarshal(res.Value, out)
}

// FieldURL returns the address of the document element idx is in.
func (t *Tab) FieldURL(idx int) (string, error) {
	defer t.begin()()
	f, err := t.field(idx)
	if err != nil {
		return "", err
	}
	f.release()
	return f.url, nil
}

// The input types a username can go into.
var usernameTypes = map[string]bool{"text": true, "email": true, "tel": true, "number": true}

// Fill puts a login's username, or with password set its password, into
// input element idx, if allow accepts the address of the document the
// field is in.
func (t *Tab) Fill(idx int, value string, password bool, allow func(pageURL string) error) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	f, err := t.field(idx)
	if err != nil {
		return err
	}
	defer f.release()
	if err := allow(f.url); err != nil {
		return err
	}
	var info struct {
		Tag  string `json:"tag"`
		Type string `json:"type"`
	}
	if err := f.call(`function () { return {tag: this.tagName.toLowerCase(), type: (this.type || '').toLowerCase()}; }`, &info); err != nil {
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
	// Click into it as a person would (pages wake their fields on that).
	if x, y, _, err := t.elementCenter(idx); err == nil {
		t.setCursor(x, y, true, true)
		if err := t.mouseClick(x, y, input.Left, 1); err != nil {
			return err
		}
		time.Sleep(150 * time.Millisecond)
	}
	if password {
		t.addSecret(value)
	}
	var got struct {
		N     int    `json:"n"`
		Value string `json:"value"`
	}
	err = f.call(`function (value, secret) {
		this.focus();
		this.value = value;
		this.dispatchEvent(new Event('input', {bubbles: true}));
		this.dispatchEvent(new Event('change', {bubbles: true}));
		if (secret) {
			// Masked even if the page turns it into a text field.
			this.style.setProperty('-webkit-text-security', 'disc', 'important');
			this.setAttribute('data-alexa-secret', '');
		}
		return {n: this.value.length, value: secret ? '' : this.value};
	}`, &got, value, password)
	if err != nil {
		return err
	}
	// Fields may format or lowercase a username; only a password's length
	// is compared, so it never leaves the page.
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }
	if password && got.N != len(utf16.Encode([]rune(value))) || !password && !strings.Contains(norm(got.Value), norm(value)) {
		what := "username"
		if password {
			what = "password"
		}
		return fmt.Errorf("element [%d] didn't take the %s", idx, what)
	}
	return nil
}

// ---------- keeping typed passwords from the model ----------

type secret struct {
	text string
	re   *regexp.Regexp
}

// addSecret remembers a password typed into the page. Very short ones
// would blank out ordinary words, and no site takes them anyway.
func (t *Tab) addSecret(s string) {
	if len([]rune(s)) < 4 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, x := range t.secrets {
		if x.text == s {
			return
		}
	}
	t.secrets = append(t.secrets, secret{s, secretRe(s)})
}

func (t *Tab) secretList() []secret {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.secrets
}

// secretRe matches a password as it can turn up in text taken from a page:
// as it is, with characters escaped for markdown or HTML, with its spaces
// collapsed, cut short with an ellipsis, or URL-encoded.
func secretRe(s string) *regexp.Regexp {
	rs := []rune(s)
	char := func(r rune) string {
		if unicode.IsSpace(r) {
			return `\s+`
		}
		alts := []string{regexp.QuoteMeta(string(r))}
		if e := html.EscapeString(string(r)); e != string(r) {
			alts = append(alts, regexp.QuoteMeta(e), fmt.Sprintf("&#%d;", r), fmt.Sprintf("(?i:&#x%x;)", r))
		}
		return `\\?(?:` + strings.Join(alts, "|") + `)`
	}
	var whole, cut strings.Builder
	for i, r := range rs {
		whole.WriteString(char(r))
		if i >= 4 {
			cut.WriteString("(?:")
		}
		cut.WriteString(char(r))
	}
	cut.WriteString(strings.Repeat(")?", max(len(rs)-4, 0)) + "…")
	alts := []string{whole.String(), cut.String()}
	for _, enc := range []string{url.QueryEscape(s), url.PathEscape(s)} {
		if enc != s {
			alts = append(alts, "(?i:"+regexp.QuoteMeta(enc)+")")
		}
	}
	return regexp.MustCompile(strings.Join(alts, "|"))
}

// Redact hides the passwords typed into the tab in text meant for the
// model.
func (t *Tab) Redact(s string) string {
	for _, x := range t.secretList() {
		s = x.re.ReplaceAllLiteralString(s, "••••")
	}
	return s
}

func (t *Tab) showsSecret(s string) bool {
	for _, x := range t.secretList() {
		if x.re.MatchString(s) {
			return true
		}
	}
	return false
}

// shownFn returns the page's text and the values of fields that don't mask
// what's in them, in the page and its same-origin frames.
const shownFn = `(() => {
  const out = { text: '', values: [] }, seen = new Set()
  const walk = (doc) => {
    if (!doc || seen.has(doc)) return
    seen.add(doc)
    if (doc.body && out.text.length < 500000) out.text += doc.body.innerText + '\n'
    for (const el of doc.querySelectorAll('input, textarea')) {
      if (el.type === 'password' || doc.defaultView.getComputedStyle(el).webkitTextSecurity !== 'none') continue
      if (el.value) out.values.push(el.value.slice(0, 2000))
    }
    for (const f of doc.querySelectorAll('iframe, frame')) {
      let d = null
      try { d = f.contentDocument } catch (e) {}
      walk(d)
    }
  }
  walk(document)
  out.text = out.text.slice(0, 500000)
  return out
})()`

// pageShowsSecret reports whether the page shows a password typed in from
// a saved login where a screenshot would catch it.
func (t *Tab) pageShowsSecret() bool {
	if len(t.secretList()) == 0 {
		return false
	}
	var shown struct {
		Text   string   `json:"text"`
		Values []string `json:"values"`
	}
	if err := t.eval(shownFn, &shown); err != nil {
		return true // can't tell: assume it does
	}
	return t.showsSecret(shown.Text + "\n" + strings.Join(shown.Values, "\n"))
}
