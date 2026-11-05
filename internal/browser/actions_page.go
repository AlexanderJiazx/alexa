package browser

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"strings"
	"time"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Navigate opens a URL.
func (t *Tab) Navigate(raw string) error {
	defer t.begin()()
	if err := t.blocked(); err != nil {
		return err
	}
	u, err := navTarget(raw)
	if err != nil {
		return err
	}
	err = t.run(45*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
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

// navTarget turns what the agent or a person typed into the URL to open:
// a bare host gets https://, anything else that isn't a URL is searched
// for. Only web pages open: file:// would read the server's disk (other
// users' files, the database), and chrome:// pages change the browser's
// own settings, so every other scheme is refused.
func navTarget(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if !strings.Contains(u, "://") && !strings.HasPrefix(strings.ToLower(u), "about:") {
		if strings.Contains(u, ".") && !strings.Contains(u, " ") {
			u = "https://" + u
		} else {
			u = "https://duckduckgo.com/?q=" + url.QueryEscape(u)
		}
	}
	pu, err := url.Parse(u)
	if err == nil {
		switch strings.ToLower(pu.Scheme) {
		case "http", "https":
			if pu.Host != "" {
				return u, nil
			}
		case "about":
			if strings.EqualFold(u, "about:blank") {
				return "about:blank", nil
			}
		}
	}
	return "", fmt.Errorf("can't open %s: only web pages (http and https) can be opened", u)
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
	if err := t.pause(120 * time.Millisecond); err != nil {
		return "", err
	}
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
		if err := t.pause(150 * time.Millisecond); err != nil {
			return "", err
		}
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
		if err := t.pause(150 * time.Millisecond); err != nil {
			return "", err
		}
		if err := t.input(10*time.Second, keyPress("Enter", 0)); err != nil {
			return "", err
		}
	}
	t.settle()
	if idx <= 0 || submit || t.pendingDialog() != nil || t.cut() != nil {
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
	err := t.evalAt(idx, fmt.Sprintf(`%s(el);
		el.focus();
		const set = Object.getOwnPropertyDescriptor(el.ownerDocument.defaultView.HTMLInputElement.prototype, 'value').set;
		set.call(el, %s);
		el.dispatchEvent(new Event('input', {bubbles: true}));
		el.dispatchEvent(new Event('change', {bubbles: true}));
		return {ok: true, value: el.value};`, intoViewJS, jsString(v)), &res)
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
	return t.pause(500 * time.Millisecond)
}

func (t *Tab) evalSettle(js string) error {
	var x any
	if err := t.eval(js, &x); err != nil {
		return err
	}
	return t.pause(400 * time.Millisecond)
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
	ctx, done := t.withBound(ctx)
	defer done()
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
				fctx, done := t.withBound(f.ctx)
				err := evalIn(fctx, fmt.Sprintf("(%s)({})", readFn), &fr)
				done()
				if err == nil {
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
	return t.pause(500 * time.Millisecond)
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
			select {
			case <-time.After(pace):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}))
	if err != nil {
		// Cut short (or failed) with the button down: let go of it, so the
		// page isn't left mid-drag. This goes past the binding on purpose.
		c, cancel := context.WithTimeout(p.ctx, 2*time.Second)
		_ = chromedp.Run(c, input.DispatchMouseEvent(input.MouseReleased, fx, fy).WithButton(input.Left).WithButtons(0).WithClickCount(1))
		cancel()
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
