package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// State is a snapshot of the page for the model.
type State struct {
	URL        string
	Title      string
	Text       string // textual description for the model
	Screenshot []byte // annotated JPEG
}

type element struct {
	D string  `json:"d"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type frameBox struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Clip struct {
		L float64 `json:"l"`
		T float64 `json:"t"`
		R float64 `json:"r"`
		B float64 `json:"b"`
	} `json:"clip"`
	Name string `json:"name"`
	Src  string `json:"src"`
}

type indexResult struct {
	Els     []element  `json:"els"`
	Above   int        `json:"above"`
	Below   int        `json:"below"`
	ScrollY int        `json:"scrollY"`
	ScrollH int        `json:"scrollH"`
	VH      int        `json:"vh"`
	VW      int        `json:"vw"`
	Text    string     `json:"text"`
	Title   string     `json:"title"`
	URL     string     `json:"url"`
	XFrames []frameBox `json:"xframes"` // cross-origin frames in view
}

// elemRef says where element [i] of the latest state lives.
type elemRef struct {
	frame *frameCtx // a cross-origin frame, or nil for the page (same-origin frames included)
	xf    int       // that frame's position in the page's window.__aaXFrames
	local int       // index in the window's __aaEls; -1 for a frame that couldn't be read
	x, y  float64   // where such a frame was
}

func (t *Tab) begin() func() {
	t.busyN.Add(1)
	t.actMu.Lock()
	t.b.touch()
	return func() {
		t.actMu.Unlock()
		t.busyN.Add(-1)
	}
}

// State indexes the page and captures an annotated screenshot.
func (t *Tab) State() (*State, error) {
	defer t.begin()()
	return t.state()
}

func (t *Tab) state() (*State, error) {
	if d := t.pendingDialog(); d != nil {
		// The page is paused: no scripts and no screenshot until it's answered.
		u, title := t.URL()
		return &State{URL: t.Redact(u), Title: t.Redact(title), Text: t.Redact(fmt.Sprintf("URL: %s\nTitle: %s\n\n%s\n", u, title, d.describe()))}, nil
	}
	p := t.active()
	var ix indexResult
	if err := t.eval(fmt.Sprintf("(%s)({})", indexFn), &ix); err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}
	refs := make([]elemRef, len(ix.Els)+1)
	for i := range ix.Els {
		refs[i+1] = elemRef{local: i + 1}
	}
	if len(ix.XFrames) > 0 {
		refs = t.indexFrames(p, &ix, refs)
	}
	t.mu.Lock()
	t.elems, t.elemsPage = refs, p
	t.mu.Unlock()
	shot, err := t.screenshot(75)
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	t.publishFrame(shot)
	boxes := make([]box, len(ix.Els))
	for i, e := range ix.Els {
		boxes[i] = box{i + 1, e.X, e.Y, e.W, e.H}
	}
	annotated, err := annotate(shot, boxes)
	if err != nil {
		annotated = shot
	}

	t.mu.Lock()
	t.url, t.title = ix.URL, ix.Title
	dialogs := t.dialogs
	t.dialogs = nil
	popups := len(t.pages)
	t.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "URL: %s\nTitle: %s\n", ix.URL, ix.Title)
	if popups > 1 {
		fmt.Fprintf(&b, "(You are in a popup window opened by the page; it returns to the main page when it closes.)\n")
	}
	fmt.Fprintf(&b, "Viewport %dx%d, scrolled to y=%d of page height %d.\n", ix.VW, ix.VH, ix.ScrollY, ix.ScrollH)
	for _, d := range dialogs {
		fmt.Fprintf(&b, "Note: the page showed a %s (auto-accepted).\n", d)
	}
	b.WriteString("\nInteractive elements in view ([index] description; boxes with these numbers are drawn on the screenshot):\n")
	if len(ix.Els) == 0 {
		b.WriteString("(none)\n")
	}
	for i, e := range ix.Els {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, e.D)
	}
	if ix.Above > 0 || ix.Below > 0 {
		fmt.Fprintf(&b, "(%d more interactive elements above and %d below the viewport — scroll to reach them.)\n", ix.Above, ix.Below)
	}
	if ix.Text != "" {
		fmt.Fprintf(&b, "\nVisible text:\n%s\n", ix.Text)
	}
	// A screenshot can't be redacted: leave it out while the page shows a
	// password typed in from a saved login.
	if t.showsSecret(b.String()) || t.pageShowsSecret() {
		annotated = nil
		b.WriteString("\n(No screenshot this time: the page is showing one of the user's saved passwords.)\n")
	}
	return &State{URL: t.Redact(ix.URL), Title: t.Redact(ix.Title), Text: t.Redact(b.String()), Screenshot: annotated}, nil
}

// ref finds element idx of the latest state.
func (t *Tab) ref(idx int) (elemRef, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.elemsPage == nil {
		return elemRef{local: idx}, nil // no state read yet: the page's own index
	}
	if t.elemsPage != t.activeLocked() || idx < 1 || idx >= len(t.elems) {
		return elemRef{}, errNoElement
	}
	return t.elems[idx], nil
}

// evalAt runs body with el bound to element idx, in the frame it lives in.
// body returns an object with ok:true, or nothing if el is gone.
func (t *Tab) evalAt(idx int, body string, out any) error {
	r, err := t.ref(idx)
	if err != nil {
		return err
	}
	if r.local < 0 {
		return fmt.Errorf("element [%d] is an embedded frame that couldn't be read; click inside it by coordinates instead", idx)
	}
	js := fmt.Sprintf(`(() => { const el = (window.__aaEls||[])[%d]; if (!el || !el.isConnected) return {ok:false}; %s })()`, r.local, body)
	if r.frame != nil {
		return evalIn(r.frame.ctx, js, out)
	}
	return t.eval(js, out)
}

// Navigate opens a URL.
func (t *Tab) Navigate(raw string) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	u := strings.TrimSpace(raw)
	if !strings.Contains(u, "://") && !strings.HasPrefix(u, "about:") {
		if strings.Contains(u, ".") && !strings.Contains(u, " ") {
			u = "https://" + u
		} else {
			u = "https://duckduckgo.com/?q=" + url.QueryEscape(u)
		}
	}
	err := t.run(45*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, errText, _, err := page.Navigate(u).Do(ctx)
		if err != nil {
			return err
		}
		if errText != "" && errText != "net::ERR_ABORTED" {
			return fmt.Errorf("navigation failed: %s", errText)
		}
		return nil
	}))
	if err != nil {
		return err
	}
	t.settle()
	return nil
}

func (t *Tab) Click(idx int) (string, error) {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return "", err
	}
	x, y, tag, err := t.elementCenter(idx)
	if err != nil {
		return "", err
	}
	if tag == "select" {
		return "", fmt.Errorf("element [%d] is a dropdown <select>; use browser_select with the option text instead", idx)
	}
	time.Sleep(120 * time.Millisecond)
	t.setCursor(x, y, true, true)
	if err := t.mouseClick(x, y, input.Left, 1); err != nil {
		return "", err
	}
	t.settle()
	return tag, nil
}

func (t *Tab) ClickXY(x, y float64, double bool) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	t.setCursor(x, y, true, true)
	n := int64(1)
	if double {
		n = 2
	}
	if err := t.mouseClick(x, y, input.Left, n); err != nil {
		return err
	}
	t.settle()
	return nil
}

// Input types whose value can't be typed as text (Chrome's insertText
// leaves them empty), with the format they take instead.
var valueInputs = map[string]string{
	"date": "YYYY-MM-DD", "time": "HH:MM (24-hour)", "datetime-local": "YYYY-MM-DDTHH:MM",
	"month": "YYYY-MM", "week": "YYYY-Www", "color": "#rrggbb", "range": "a number within its min and max",
}

// Type focuses element idx (if >0), optionally clears it, types text and
// optionally presses Enter. Date, time, slider and similar fields get their
// value set directly. It returns a note when the field doesn't end up
// showing what was typed.
func (t *Tab) Type(idx int, text string, clear, submit bool) (string, error) {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return "", err
	}
	if idx > 0 {
		var info struct {
			OK   bool   `json:"ok"`
			Tag  string `json:"tag"`
			Type string `json:"type"`
		}
		if r, err := t.ref(idx); err == nil && r.local > 0 {
			if err := t.evalAt(idx, `return {ok: true, tag: el.tagName.toLowerCase(), type: (el.type || '').toLowerCase()};`, &info); err != nil {
				return "", err
			}
			if !info.OK {
				return "", errNoElement
			}
		}
		if info.Tag == "select" {
			return "", fmt.Errorf("element [%d] is a dropdown <select>; use browser_select with the option text instead", idx)
		}
		if hint, ok := valueInputs[info.Type]; ok && info.Tag == "input" {
			return t.setValue(idx, info.Type, hint, text, submit)
		}
		x, y, _, err := t.elementCenter(idx)
		if err != nil {
			return "", err
		}
		t.setCursor(x, y, true, true)
		if err := t.mouseClick(x, y, input.Left, 1); err != nil {
			return "", err
		}
		time.Sleep(150 * time.Millisecond)
		if clear {
			var res struct {
				OK bool `json:"ok"`
			}
			_ = t.evalAt(idx, `const doc = el.ownerDocument, a = doc.activeElement && doc.activeElement !== doc.body ? doc.activeElement : el;
				if (a.tagName === 'INPUT' || a.tagName === 'TEXTAREA') { a.select(); return {ok: true}; }
				if (a.isContentEditable) { doc.execCommand('selectAll'); return {ok: true}; } return {ok: false};`, &res)
			if res.OK {
				_ = t.input(10*time.Second, keyPress("Backspace", 0))
			}
		}
	}
	if err := t.input(20*time.Second, input.InsertText(text)); err != nil {
		return "", err
	}
	if submit {
		time.Sleep(150 * time.Millisecond)
		if err := t.input(10*time.Second, keyPress("Enter", 0)); err != nil {
			return "", err
		}
	}
	t.settle()
	if idx <= 0 || submit || t.pendingDialog() != nil {
		return "", nil
	}
	// Masks, autocomplete and length limits can change what was typed.
	var got struct {
		OK    bool   `json:"ok"`
		Value string `json:"value"`
	}
	_ = t.evalAt(idx, `if (el.type === 'password') return {ok: false};
		const v = el.isContentEditable ? el.innerText : el.value;
		return typeof v === 'string' ? {ok: true, value: v} : {ok: false};`, &got)
	if got.OK && !strings.Contains(strings.Join(strings.Fields(got.Value), " "), strings.Join(strings.Fields(text), " ")) {
		return fmt.Sprintf("Note: the field now shows %q, not exactly what was typed.", trimText(got.Value, 120)), nil
	}
	return "", nil
}

// setValue sets a date, time, slider or similar input the way a person's
// picking would, firing the events pages listen for.
func (t *Tab) setValue(idx int, typ, hint, text string, submit bool) (string, error) {
	v := normalizeValue(typ, text)
	var res struct {
		OK    bool   `json:"ok"`
		Value string `json:"value"`
	}
	err := t.evalAt(idx, fmt.Sprintf(`el.scrollIntoView({block: 'center', behavior: 'instant'});
		el.focus();
		const set = Object.getOwnPropertyDescriptor(el.ownerDocument.defaultView.HTMLInputElement.prototype, 'value').set;
		set.call(el, %s);
		el.dispatchEvent(new Event('input', {bubbles: true}));
		el.dispatchEvent(new Event('change', {bubbles: true}));
		return {ok: true, value: el.value};`, jsString(v)), &res)
	if err != nil {
		return "", err
	}
	if !res.OK {
		return "", errNoElement
	}
	if res.Value == "" && v != "" {
		return "", fmt.Errorf("the %s field didn't accept %q; give it as %s", typ, text, hint)
	}
	if submit {
		if err := t.input(10*time.Second, keyPress("Enter", 0)); err != nil {
			return "", err
		}
	}
	t.settle()
	return fmt.Sprintf("The %s field now holds %s.", typ, res.Value), nil
}

// normalizeValue turns common ways of writing a date or time into the form
// the input takes. Anything else is passed on as it is.
func normalizeValue(typ, v string) string {
	v = strings.TrimSpace(v)
	try := func(out string, layouts ...string) string {
		for _, l := range layouts {
			if t, err := time.Parse(l, v); err == nil {
				return t.Format(out)
			}
		}
		return v
	}
	switch typ {
	case "date":
		return try("2006-01-02", "2006-01-02", "2006/01/02", "January 2, 2006", "Jan 2, 2006", "2 January 2006", "2 Jan 2006", "Monday, January 2, 2006", "Mon, Jan 2, 2006")
	case "time":
		return try("15:04", "15:04", "15:04:05", "3:04 PM", "3:04PM", "3:04 pm", "3:04pm", "3 PM", "3PM", "3 pm", "3pm")
	case "datetime-local":
		return try("2006-01-02T15:04", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02 3:04 PM", "2006-01-02 3:04 pm")
	case "month":
		return try("2006-01", "2006-01", "January 2006", "Jan 2006", "2006/01")
	}
	return v
}

// Keys presses a key combination like "Enter", "Escape", "Control+a".
func (t *Tab) Keys(combo string) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	parts := strings.Split(combo, "+")
	var mods int64
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "alt", "option":
			mods |= 1
		case "control", "ctrl":
			mods |= 2
		case "meta", "cmd", "command":
			mods |= 4
		case "shift":
			mods |= 8
		}
	}
	if err := t.input(10*time.Second, keyPress(strings.TrimSpace(parts[len(parts)-1]), mods)); err != nil {
		return err
	}
	t.settle()
	return nil
}

// keyPress presses and releases a key.
func keyPress(key string, mods int64) chromedp.Action {
	k := lookupKey(key)
	return chromedp.ActionFunc(func(ctx context.Context) error {
		down := input.DispatchKeyEvent(input.KeyRawDown).WithKey(k.key).WithCode(k.code).WithWindowsVirtualKeyCode(k.vk).WithNativeVirtualKeyCode(k.vk).WithModifiers(input.Modifier(mods))
		if k.text != "" && mods&^8 == 0 {
			down = input.DispatchKeyEvent(input.KeyDown).WithKey(k.key).WithCode(k.code).WithWindowsVirtualKeyCode(k.vk).WithNativeVirtualKeyCode(k.vk).WithText(k.text).WithUnmodifiedText(k.text).WithModifiers(input.Modifier(mods))
		}
		if err := down.Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey(k.key).WithCode(k.code).WithWindowsVirtualKeyCode(k.vk).WithNativeVirtualKeyCode(k.vk).WithModifiers(input.Modifier(mods)).Do(ctx)
	})
}

// Scroll scrolls the page (or the scrollable area under element idx).
func (t *Tab) Scroll(direction string, pages float64, idx int) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	if pages <= 0 {
		pages = 0.8
	}
	w, h := float64(t.b.mgr.opts.Width), float64(t.b.mgr.opts.Height)
	x, y := w/2, h/2
	if idx > 0 {
		if cx, cy, _, err := t.elementCenter(idx); err == nil {
			x, y = cx, cy
		}
	}
	dx, dy := 0.0, 0.0
	switch strings.ToLower(direction) {
	case "up":
		dy = -h * pages
	case "left":
		dx = -w * pages
	case "right":
		dx = w * pages
	case "top":
		return t.evalSettle(`window.scrollTo(0,0)`)
	case "bottom":
		return t.evalSettle(`window.scrollTo(0, (document.scrollingElement||document.documentElement).scrollHeight)`)
	default:
		dy = h * pages
	}
	if err := t.input(10*time.Second, input.DispatchMouseEvent(input.MouseWheel, x, y).WithDeltaX(dx).WithDeltaY(dy)); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return nil
}

func (t *Tab) evalSettle(js string) error {
	var x any
	if err := t.eval(js, &x); err != nil {
		return err
	}
	time.Sleep(400 * time.Millisecond)
	return nil
}

// Select chooses an option (by visible text or value) in a <select>.
func (t *Tab) Select(idx int, option string) (string, error) {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return "", err
	}
	var res struct {
		OK  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	body := fmt.Sprintf(`if (el.tagName !== 'SELECT') return {ok:false, msg:'element is not a <select>; click it and pick the option instead'};
		const want = %s.toLowerCase().trim();
		let opt = Array.from(el.options).find(o => o.text.trim().toLowerCase() === want || o.value.toLowerCase() === want)
			|| Array.from(el.options).find(o => o.text.toLowerCase().includes(want));
		if (!opt) return {ok:false, msg:'no option matching; options are: ' + Array.from(el.options).map(o => o.text.trim()).join(' | ')};
		el.focus();
		const setter = Object.getOwnPropertyDescriptor(el.ownerDocument.defaultView.HTMLSelectElement.prototype, 'value').set;
		setter.call(el, opt.value);
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return {ok:true, msg: opt.text.trim()};`, jsString(option))
	if err := t.evalAt(idx, body, &res); err != nil {
		return "", err
	}
	if !res.OK {
		if res.Msg == "" {
			return "", errNoElement
		}
		return "", fmt.Errorf("%s", res.Msg)
	}
	t.settle()
	return res.Msg, nil
}

// Upload sets the files of element idx's file input: the element itself, or
// the one its button, label or form belongs to (they're often hidden).
func (t *Tab) Upload(idx int, paths []string) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	r, err := t.ref(idx)
	if err != nil {
		return err
	}
	if r.local < 0 {
		return fmt.Errorf("element [%d] is an embedded frame that couldn't be read", idx)
	}
	ctx := t.active().ctx
	if r.frame != nil {
		ctx = r.frame.ctx
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err = chromedp.Run(c, chromedp.ActionFunc(func(ctx context.Context) error {
		obj, exc, err := runtime.Evaluate(fmt.Sprintf(`(() => {
			const el = (window.__aaEls||[])[%d];
			if (!el) return null;
			const isFile = e => e && e.tagName === 'INPUT' && e.type === 'file';
			if (isFile(el)) return el;
			const lab = el.closest && el.closest('label');
			const form = el.closest && el.closest('form');
			const near = (el.querySelector && el.querySelector('input[type=file]')) || (lab && isFile(lab.control) ? lab.control : null)
				|| (form && form.querySelector('input[type=file]'));
			if (near) return near;
			const all = el.ownerDocument.querySelectorAll('input[type=file]');
			return all.length === 1 ? all[0] : null;
		})()`, r.local)).Do(ctx)
		if err != nil {
			return err
		}
		if exc != nil || obj == nil || obj.ObjectID == "" {
			return errors.New("no file input found there; use the index of the file input or of its upload button")
		}
		if _, err := dom.GetDocument().Do(ctx); err != nil {
			return err
		}
		nodeID, err := dom.RequestNode(obj.ObjectID).Do(ctx)
		if err != nil {
			return err
		}
		return dom.SetFileInputFiles(paths).WithNodeID(nodeID).Do(ctx)
	}))
	if err != nil {
		return err
	}
	t.settle()
	return nil
}

func (t *Tab) History(delta int) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	err := t.run(10*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		cur, entries, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		i := int(cur) + delta
		if i < 0 || i >= len(entries) {
			return fmt.Errorf("no page to go %s to", map[bool]string{true: "forward", false: "back"}[delta > 0])
		}
		return page.NavigateToHistoryEntry(entries[i].ID).Do(ctx)
	}))
	if err != nil {
		return err
	}
	t.settle()
	return nil
}

func (t *Tab) Reload() error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	if err := t.run(30*time.Second, chromedp.Reload()); err != nil {
		return err
	}
	t.settle()
	return nil
}

// Read returns the page content as markdown (paged by character offset),
// including what's in its frames.
func (t *Tab) Read(offset, limit int) (string, int, error) {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return "", 0, err
	}
	p := t.active()
	var res struct {
		HTML string   `json:"html"`
		XSrc []string `json:"xsrc"`
	}
	if err := t.eval(fmt.Sprintf("(%s)({frames: true})", readFn), &res); err != nil {
		return "", 0, err
	}
	doc := res.HTML
	if len(res.XSrc) > 0 {
		frames := t.crossFrames(p, "__aaReadXF", len(res.XSrc))
		for k, src := range res.XSrc {
			repl := "<p>[An embedded frame from " + html.EscapeString(src) + " that couldn't be read]</p>"
			if f := frames[k]; f != nil {
				var fr struct {
					HTML string `json:"html"`
				}
				if err := evalIn(f.ctx, fmt.Sprintf("(%s)({})", readFn), &fr); err == nil {
					repl = "<p><em>[Embedded frame: " + html.EscapeString(src) + "]</em></p>" + fr.HTML
				}
			}
			doc = strings.Replace(doc, fmt.Sprintf("[[AA-XFRAME-%d]]", k), repl, 1)
		}
	}
	md, err := htmltomd.ConvertString(doc)
	if err != nil {
		md = doc
	}
	md = t.Redact(collapseBlank(md))
	total := len(md)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return md[offset:end], total, nil
}

func (t *Tab) Eval(js string) (string, error) {
	defer t.begin()()
	var out any
	if err := t.eval(js, &out); err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", out), nil
}

// Hover moves the mouse over element idx (or to x, y when idx is 0), for
// menus and tooltips that open on hover.
func (t *Tab) Hover(idx int, x, y float64) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	if idx > 0 {
		var err error
		if x, y, _, err = t.elementCenter(idx); err != nil {
			return err
		}
	}
	t.setCursor(x, y, false, true)
	if err := t.input(10*time.Second, input.DispatchMouseEvent(input.MouseMoved, x, y)); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return nil
}

// Drag presses the mouse on one point (element fromIdx, or fx, fy), moves it
// to another in steps and releases it there: for sliders, reordering and
// drag and drop. A native HTML5 drag doesn't start from synthetic mouse
// events alone, so it's intercepted and finished with drag events.
func (t *Tab) Drag(fromIdx int, fx, fy float64, toIdx int, tx, ty float64) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	var err error
	if fromIdx > 0 {
		if fx, fy, _, err = t.elementCenter(fromIdx); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}
	if toIdx > 0 {
		if tx, ty, _, err = t.elementCenter(toIdx); err != nil {
			return fmt.Errorf("end: %w", err)
		}
	}
	w, h := float64(t.b.mgr.opts.Width), float64(t.b.mgr.opts.Height)
	if tx < 0 || ty < 0 || tx > w || ty > h {
		return errors.New("the end point is out of view; scroll so both ends are visible first")
	}
	p := t.active()
	got := make(chan *input.DragData, 1)
	lctx, lcancel := context.WithCancel(p.ctx)
	defer lcancel()
	chromedp.ListenTarget(lctx, func(ev any) {
		if e, ok := ev.(*input.EventDragIntercepted); ok {
			select {
			case got <- e.Data:
			default:
			}
		}
	})
	_ = t.run(5*time.Second, input.SetInterceptDrags(true))
	defer func() { _ = t.run(5*time.Second, input.SetInterceptDrags(false)) }()
	t.setCursor(fx, fy, false, true)
	// The pointer in the live view glides to the drop point while the mouse
	// really moves there, over the same time.
	const steps = 12
	pace := max(glide(math.Hypot(tx-fx, ty-fy))/steps, 20*time.Millisecond)
	err = t.input(20*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchMouseEvent(input.MouseMoved, fx, fy).Do(ctx); err != nil {
			return err
		}
		if err := input.DispatchMouseEvent(input.MousePressed, fx, fy).WithButton(input.Left).WithButtons(1).WithClickCount(1).Do(ctx); err != nil {
			return err
		}
		t.setCursor(tx, ty, false, false)
		for i := 1; i <= steps; i++ {
			x, y := fx+(tx-fx)*float64(i)/steps, fy+(ty-fy)*float64(i)/steps
			if err := input.DispatchMouseEvent(input.MouseMoved, x, y).WithButton(input.Left).WithButtons(1).Do(ctx); err != nil {
				return err
			}
			time.Sleep(pace)
		}
		return nil
	}))
	if err != nil {
		return err
	}
	var data *input.DragData
	select {
	case data = <-got:
	case <-time.After(250 * time.Millisecond):
	}
	t.setCursor(tx, ty, true, false)
	err = t.input(10*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		if data != nil {
			for _, typ := range []input.DispatchDragEventType{input.DragEnter, input.DragOver, input.Drop} {
				if err := input.DispatchDragEvent(typ, tx, ty, data).Do(ctx); err != nil {
					return err
				}
			}
		}
		return input.DispatchMouseEvent(input.MouseReleased, tx, ty).WithButton(input.Left).WithButtons(0).WithClickCount(1).Do(ctx)
	}))
	t.settle()
	return err
}

// AnswerDialog accepts or dismisses the confirm or prompt dialog the page is
// waiting on, typing text into a prompt first.
func (t *Tab) AnswerDialog(accept bool, text string) (string, error) {
	defer t.begin()()
	d := t.pendingDialog()
	if d == nil {
		return "", errors.New("no dialog is open")
	}
	if err := t.answer(d, accept, text); err != nil {
		return "", err
	}
	t.settle()
	msg := "Dismissed the " + d.Type + " dialog."
	if accept {
		msg = "Accepted the " + d.Type + " dialog."
		if d.Type == "prompt" {
			msg = fmt.Sprintf("Answered the prompt with %q.", text)
		}
	}
	return msg, nil
}

func trimText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func collapseBlank(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return strings.Join(out, "\n")
}

// ---------- human takeover input ----------

type HumanInput struct {
	Kind      string  `json:"kind"` // mousedown | mouseup | mousemove | wheel | keydown | keyup | text | navigate | back | forward | reload
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Button    string  `json:"button"`
	Clicks    int64   `json:"clicks"`
	DX        float64 `json:"dx"`
	DY        float64 `json:"dy"`
	Key       string  `json:"key"`
	Code      string  `json:"code"`
	KeyCode   int64   `json:"keyCode"`
	Text      string  `json:"text"`
	Modifiers int64   `json:"modifiers"`
	URL       string  `json:"url"`
}

// Human applies an input event from a person controlling the browser.
func (t *Tab) Human(ev HumanInput) error {
	t.b.touch()
	t.mu.Lock()
	t.humanAt = time.Now()
	t.mu.Unlock()
	// A dialog the agent left open: they can't see it, and the page ignores
	// them until it's answered.
	if d := t.pendingDialog(); d != nil {
		_ = t.answer(d, false, "")
	}
	btn := input.Left
	switch ev.Button {
	case "right":
		btn = input.Right
	case "middle":
		btn = input.Middle
	}
	mods := input.Modifier(ev.Modifiers)
	switch ev.Kind {
	case "mousemove":
		return t.run(5*time.Second, input.DispatchMouseEvent(input.MouseMoved, ev.X, ev.Y).WithModifiers(mods))
	case "mousedown":
		c := ev.Clicks
		if c < 1 {
			c = 1
		}
		return t.run(5*time.Second, input.DispatchMouseEvent(input.MousePressed, ev.X, ev.Y).WithButton(btn).WithButtons(1).WithClickCount(c).WithModifiers(mods))
	case "mouseup":
		c := ev.Clicks
		if c < 1 {
			c = 1
		}
		return t.run(5*time.Second, input.DispatchMouseEvent(input.MouseReleased, ev.X, ev.Y).WithButton(btn).WithButtons(0).WithClickCount(c).WithModifiers(mods))
	case "wheel":
		return t.run(5*time.Second, input.DispatchMouseEvent(input.MouseWheel, ev.X, ev.Y).WithDeltaX(ev.DX).WithDeltaY(ev.DY))
	case "keydown", "keyup":
		typ := input.KeyRawDown
		if ev.Kind == "keyup" {
			typ = input.KeyUp
		}
		k := input.DispatchKeyEvent(typ).WithKey(ev.Key).WithCode(ev.Code).WithWindowsVirtualKeyCode(ev.KeyCode).WithNativeVirtualKeyCode(ev.KeyCode).WithModifiers(mods)
		if ev.Kind == "keydown" && ev.Text != "" {
			k = input.DispatchKeyEvent(input.KeyDown).WithKey(ev.Key).WithCode(ev.Code).WithWindowsVirtualKeyCode(ev.KeyCode).WithNativeVirtualKeyCode(ev.KeyCode).WithText(ev.Text).WithUnmodifiedText(ev.Text).WithModifiers(mods)
		}
		return t.run(5*time.Second, k)
	case "text":
		return t.run(5*time.Second, input.InsertText(ev.Text))
	case "navigate":
		go func() { _ = t.Navigate(ev.URL) }()
		return nil
	case "back":
		go func() { _ = t.History(-1) }()
		return nil
	case "forward":
		go func() { _ = t.History(1) }()
		return nil
	case "reload":
		go func() { _ = t.Reload() }()
		return nil
	}
	return fmt.Errorf("unknown input kind %q", ev.Kind)
}

// ---------- keys ----------

type keyDef struct {
	key, code, text string
	vk              int64
}

var namedKeys = map[string]keyDef{
	"enter":      {"Enter", "Enter", "\r", 13},
	"return":     {"Enter", "Enter", "\r", 13},
	"tab":        {"Tab", "Tab", "", 9},
	"escape":     {"Escape", "Escape", "", 27},
	"esc":        {"Escape", "Escape", "", 27},
	"backspace":  {"Backspace", "Backspace", "", 8},
	"delete":     {"Delete", "Delete", "", 46},
	"arrowup":    {"ArrowUp", "ArrowUp", "", 38},
	"arrowdown":  {"ArrowDown", "ArrowDown", "", 40},
	"arrowleft":  {"ArrowLeft", "ArrowLeft", "", 37},
	"arrowright": {"ArrowRight", "ArrowRight", "", 39},
	"up":         {"ArrowUp", "ArrowUp", "", 38},
	"down":       {"ArrowDown", "ArrowDown", "", 40},
	"left":       {"ArrowLeft", "ArrowLeft", "", 37},
	"right":      {"ArrowRight", "ArrowRight", "", 39},
	"home":       {"Home", "Home", "", 36},
	"end":        {"End", "End", "", 35},
	"pageup":     {"PageUp", "PageUp", "", 33},
	"pagedown":   {"PageDown", "PageDown", "", 34},
	"space":      {" ", "Space", " ", 32},
}

func lookupKey(name string) keyDef {
	if k, ok := namedKeys[strings.ToLower(name)]; ok {
		return k
	}
	if strings.HasPrefix(strings.ToLower(name), "f") {
		if n, err := strconv.Atoi(name[1:]); err == nil && n >= 1 && n <= 12 {
			return keyDef{name, name, "", int64(111 + n)}
		}
	}
	if len([]rune(name)) == 1 {
		r := []rune(name)[0]
		up := strings.ToUpper(name)
		code := "Key" + up
		if r >= '0' && r <= '9' {
			code = "Digit" + name
		}
		return keyDef{name, code, name, int64([]rune(up)[0])}
	}
	return keyDef{name, name, "", 0}
}

// ---------- screenshot annotation ----------

type box struct {
	n          int
	x, y, w, h float64
}

var palette = []color.RGBA{
	{230, 57, 70, 255}, {29, 120, 220, 255}, {20, 150, 90, 255}, {220, 120, 0, 255},
	{140, 60, 200, 255}, {0, 150, 160, 255}, {200, 40, 140, 255}, {90, 90, 90, 255},
}

func annotate(jpg []byte, boxes []box) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Src)
	face := basicfont.Face7x13
	for _, bx := range boxes {
		c := palette[bx.n%len(palette)]
		x0, y0 := int(bx.x), int(bx.y)
		x1, y1 := int(bx.x+bx.w), int(bx.y+bx.h)
		rect(img, x0, y0, x1, y1, c)
		label := strconv.Itoa(bx.n)
		lw := font.MeasureString(face, label).Ceil() + 4
		lx, ly := x0, y0-14
		if ly < 0 {
			ly = y0
		}
		if lx+lw > img.Bounds().Dx() {
			lx = img.Bounds().Dx() - lw
		}
		draw.Draw(img, image.Rect(lx, ly, lx+lw, ly+14), &image.Uniform{c}, image.Point{}, draw.Src)
		d := &font.Drawer{Dst: img, Src: image.White, Face: face, Dot: fixed.P(lx+2, ly+11)}
		d.DrawString(label)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 72}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func rect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	x0, x1 = clamp(x0, 0, b.Dx()-1), clamp(x1, 0, b.Dx()-1)
	y0, y1 = clamp(y0, 0, b.Dy()-1), clamp(y1, 0, b.Dy()-1)
	for t := 0; t < 2; t++ {
		for x := x0; x <= x1; x++ {
			img.SetRGBA(x, clamp(y0+t, 0, b.Dy()-1), c)
			img.SetRGBA(x, clamp(y1-t, 0, b.Dy()-1), c)
		}
		for y := y0; y <= y1; y++ {
			img.SetRGBA(clamp(x0+t, 0, b.Dx()-1), y, c)
			img.SetRGBA(clamp(x1-t, 0, b.Dx()-1), y, c)
		}
	}
}
