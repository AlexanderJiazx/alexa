package browser

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/url"
	"strconv"
	"strings"
	"time"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
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

type indexResult struct {
	Els []struct {
		D string  `json:"d"`
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"els"`
	Above   int    `json:"above"`
	Below   int    `json:"below"`
	ScrollY int    `json:"scrollY"`
	ScrollH int    `json:"scrollH"`
	VH      int    `json:"vh"`
	VW      int    `json:"vw"`
	Text    string `json:"text"`
	Title   string `json:"title"`
	URL     string `json:"url"`
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
	var ix indexResult
	if err := t.eval(indexJS, &ix); err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}
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
	return &State{URL: ix.URL, Title: ix.Title, Text: b.String(), Screenshot: annotated}, nil
}

// Navigate opens a URL.
func (t *Tab) Navigate(raw string) error {
	defer t.begin()()
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
	x, y, tag, err := t.elementCenter(idx)
	if err != nil {
		return "", err
	}
	if tag == "select" {
		return "", fmt.Errorf("element [%d] is a dropdown <select>; use browser_select with the option text instead", idx)
	}
	time.Sleep(120 * time.Millisecond)
	t.setCursor(x, y, true)
	if err := t.mouseClick(x, y, input.Left, 1); err != nil {
		return "", err
	}
	t.settle()
	return tag, nil
}

func (t *Tab) ClickXY(x, y float64, double bool) error {
	defer t.begin()()
	t.setCursor(x, y, true)
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

// Type focuses element idx (if >0), optionally clears it, types text and
// optionally presses Enter.
func (t *Tab) Type(idx int, text string, clear, submit bool) error {
	defer t.begin()()
	if idx > 0 {
		x, y, _, err := t.elementCenter(idx)
		if err != nil {
			return err
		}
		t.setCursor(x, y, true)
		if err := t.mouseClick(x, y, input.Left, 1); err != nil {
			return err
		}
		time.Sleep(150 * time.Millisecond)
		if clear {
			var ok bool
			_ = t.eval(fmt.Sprintf(`(() => { const el = (window.__aaEls||[])[%d]; const a = document.activeElement || el;
				if (a && (a.tagName === 'INPUT' || a.tagName === 'TEXTAREA')) { a.select(); return true; }
				if (a && a.isContentEditable) { document.execCommand('selectAll'); return true; } return false; })()`, idx), &ok)
			if ok {
				_ = t.pressKey("Backspace", 0)
			}
		}
	}
	if err := t.run(20*time.Second, input.InsertText(text)); err != nil {
		return err
	}
	if submit {
		time.Sleep(150 * time.Millisecond)
		if err := t.pressKey("Enter", 0); err != nil {
			return err
		}
	}
	t.settle()
	return nil
}

// Keys presses a key combination like "Enter", "Escape", "Control+a".
func (t *Tab) Keys(combo string) error {
	defer t.begin()()
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
	if err := t.pressKey(strings.TrimSpace(parts[len(parts)-1]), mods); err != nil {
		return err
	}
	t.settle()
	return nil
}

func (t *Tab) pressKey(key string, mods int64) error {
	k := lookupKey(key)
	return t.run(10*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		down := input.DispatchKeyEvent(input.KeyRawDown).WithKey(k.key).WithCode(k.code).WithWindowsVirtualKeyCode(k.vk).WithNativeVirtualKeyCode(k.vk).WithModifiers(input.Modifier(mods))
		if k.text != "" && mods&^8 == 0 {
			down = input.DispatchKeyEvent(input.KeyDown).WithKey(k.key).WithCode(k.code).WithWindowsVirtualKeyCode(k.vk).WithNativeVirtualKeyCode(k.vk).WithText(k.text).WithUnmodifiedText(k.text).WithModifiers(input.Modifier(mods))
		}
		if err := down.Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey(k.key).WithCode(k.code).WithWindowsVirtualKeyCode(k.vk).WithNativeVirtualKeyCode(k.vk).WithModifiers(input.Modifier(mods)).Do(ctx)
	}))
}

// Scroll scrolls the page (or the scrollable area under element idx).
func (t *Tab) Scroll(direction string, pages float64, idx int) error {
	defer t.begin()()
	if pages <= 0 {
		pages = 0.8
	}
	w, h := float64(t.b.mgr.opts.Width), float64(t.b.mgr.opts.Height)
	x, y := w/2, h/2
	if idx > 0 {
		var r struct {
			OK bool    `json:"ok"`
			X  float64 `json:"x"`
			Y  float64 `json:"y"`
		}
		_ = t.eval(fmt.Sprintf(`(() => { const el = (window.__aaEls||[])[%d]; if (!el) return {ok:false}; const r = el.getBoundingClientRect(); return {ok:true, x: r.left + r.width/2, y: r.top + r.height/2}; })()`, idx), &r)
		if r.OK {
			x, y = r.X, r.Y
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
	if err := t.run(10*time.Second, input.DispatchMouseEvent(input.MouseWheel, x, y).WithDeltaX(dx).WithDeltaY(dy)); err != nil {
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
	var res struct {
		OK  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	js := fmt.Sprintf(`(() => {
		const el = (window.__aaEls||[])[%d];
		if (!el) return {ok:false, msg:'element not found'};
		if (el.tagName !== 'SELECT') return {ok:false, msg:'element is not a <select>; click it and pick the option instead'};
		const want = %s.toLowerCase().trim();
		let opt = Array.from(el.options).find(o => o.text.trim().toLowerCase() === want || o.value.toLowerCase() === want)
			|| Array.from(el.options).find(o => o.text.toLowerCase().includes(want));
		if (!opt) return {ok:false, msg:'no option matching; options are: ' + Array.from(el.options).map(o => o.text.trim()).join(' | ')};
		el.focus();
		const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set;
		setter.call(el, opt.value);
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return {ok:true, msg: opt.text.trim()};
	})()`, idx, jsString(option))
	if err := t.eval(js, &res); err != nil {
		return "", err
	}
	if !res.OK {
		return "", fmt.Errorf("%s", res.Msg)
	}
	t.settle()
	return res.Msg, nil
}

func (t *Tab) Upload(idx int, paths []string) error {
	defer t.begin()()
	node, err := t.fileInputNode(idx)
	if err != nil {
		return err
	}
	if err := t.run(15*time.Second, dom.SetFileInputFiles(paths).WithNodeID(node)); err != nil {
		return err
	}
	t.settle()
	return nil
}

func (t *Tab) History(delta int) error {
	defer t.begin()()
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
	if err := t.run(30*time.Second, chromedp.Reload()); err != nil {
		return err
	}
	t.settle()
	return nil
}

// Read returns the page content as markdown (paged by character offset).
func (t *Tab) Read(offset, limit int) (string, int, error) {
	defer t.begin()()
	var html string
	if err := t.eval(`(() => { const c = document.body ? document.body.cloneNode(true) : document.documentElement.cloneNode(true);
		c.querySelectorAll('script,style,noscript,svg,iframe,template,link,meta').forEach(e => e.remove());
		return c.innerHTML; })()`, &html); err != nil {
		return "", 0, err
	}
	md, err := htmltomd.ConvertString(html)
	if err != nil {
		md = html
	}
	md = collapseBlank(md)
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
