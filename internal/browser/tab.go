package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type pageCtx struct {
	ctx    context.Context
	cancel context.CancelFunc
	tid    target.ID
	frames map[target.ID]*frameCtx // attached cross-origin frames
}

// Tab is one task's browsing context (plus any popups it opened).
type Tab struct {
	b      *Browser
	taskID string
	ctx    context.Context // lifetime of the tab (first page)

	mu         sync.Mutex
	pages      []*pageCtx // stack: last is active
	subs       map[chan []byte]struct{}
	lastFrame  []byte
	casting    *pageCtx
	url        string
	title      string
	cursorX    float64
	cursorY    float64
	dialogs    []string      // auto-accepted alerts, reported once
	dialog     *dialog       // a confirm/prompt waiting for the agent
	dialogWait chan struct{} // closed when a dialog opens during input
	humanAt    time.Time     // last input from a person in control
	elems      []elemRef     // where each element of the latest state lives
	elemsPage  *pageCtx      // the page that state was read from
	secrets    []secret      // passwords typed in from the vault, kept from the model

	actMu sync.Mutex // serializes actions
	busyN atomic.Int32
}

const stealthJS = `
Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
window.chrome = window.chrome || {runtime: {}};
Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});
`

func newTab(b *Browser, taskID string, ctx context.Context, cancel context.CancelFunc) (*Tab, error) {
	t := &Tab{b: b, taskID: taskID, ctx: ctx, subs: map[chan []byte]struct{}{}}
	// The pointer shown in the live view starts in the middle.
	t.cursorX, t.cursorY = float64(b.mgr.opts.Width)/2, float64(b.mgr.opts.Height)/2
	p := &pageCtx{ctx: ctx, cancel: cancel}
	if err := t.setupPage(p); err != nil {
		return nil, err
	}
	t.pages = []*pageCtx{p}
	return t, nil
}

func (t *Tab) setupPage(p *pageCtx) error {
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	w, h := int64(t.b.mgr.opts.Width), int64(t.b.mgr.opts.Height)
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if c := chromedp.FromContext(p.ctx); c != nil && c.Target != nil {
				p.tid = c.Target.TargetID
			}
			return nil
		}),
		page.Enable(),
		emulation.SetDeviceMetricsOverride(w, h, 1, false),
		emulation.SetFocusEmulationEnabled(true),
		// Blank pages are transparent, which encodes as black JPEG frames.
		emulation.SetDefaultBackgroundColorOverride().WithColor(&cdp.RGBA{R: 255, G: 255, B: 255, A: 1}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if t.b.userAgent != "" {
				return emulation.SetUserAgentOverride(t.b.userAgent).WithAcceptLanguage("en-US,en").Do(ctx)
			}
			return nil
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(stealthJS).Do(ctx)
			return err
		}),
	)
	if err != nil {
		return fmt.Errorf("setup tab: %w", err)
	}
	pctx := p.ctx
	chromedp.ListenTarget(pctx, func(ev any) {
		switch e := ev.(type) {
		case *page.EventScreencastFrame:
			data, err := base64.StdEncoding.DecodeString(e.Data)
			if err == nil {
				t.publishFrame(data)
			}
			sid := e.SessionID
			go func() { _ = chromedp.Run(pctx, page.ScreencastFrameAck(sid)) }()
		case *page.EventFrameNavigated:
			if e.Frame.ParentID == "" {
				t.mu.Lock()
				active := t.activeLocked() == p
				if active {
					t.url = e.Frame.URL
				}
				t.mu.Unlock()
				if active {
					go t.refreshTitle(e.Frame.URL)
				}
			}
		case *page.EventJavascriptDialogOpening:
			t.onDialog(pctx, e)
		case *target.EventTargetDestroyed, *target.EventDetachedFromTarget:
			go t.popClosed()
		}
	})
	return nil
}

func (t *Tab) refreshTitle(url string) {
	time.Sleep(700 * time.Millisecond)
	var title string
	ctx, cancel := context.WithTimeout(t.active().ctx, 3*time.Second)
	defer cancel()
	_ = chromedp.Run(ctx, chromedp.Title(&title))
	t.mu.Lock()
	t.title = title
	t.mu.Unlock()
	t.b.emit(t.taskID, TabEvent{Type: "nav", URL: url, Title: title})
}

func (t *Tab) activeLocked() *pageCtx {
	for i := len(t.pages) - 1; i >= 0; i-- {
		if t.pages[i].ctx.Err() == nil {
			return t.pages[i]
		}
	}
	if len(t.pages) > 0 {
		return t.pages[0]
	}
	return &pageCtx{ctx: t.ctx}
}

func (t *Tab) active() *pageCtx {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeLocked()
}

func (t *Tab) targetID() target.ID {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pages) == 0 {
		return ""
	}
	return t.pages[0].tid
}

func (t *Tab) ownsTarget(id target.ID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range t.pages {
		if p.tid == id {
			return true
		}
	}
	return false
}

func (t *Tab) switchTo(ctx context.Context, cancel context.CancelFunc) error {
	p := &pageCtx{ctx: ctx, cancel: cancel}
	if err := t.setupPage(p); err != nil {
		return err
	}
	t.mu.Lock()
	t.pages = append(t.pages, p)
	watching := len(t.subs) > 0
	t.mu.Unlock()
	if watching {
		t.restartCast()
	}
	var url string
	c, cl := context.WithTimeout(ctx, 3*time.Second)
	_ = chromedp.Run(c, chromedp.Location(&url))
	cl()
	t.mu.Lock()
	t.url = url
	t.mu.Unlock()
	go t.refreshTitle(url)
	return nil
}

// dropPopup ends the popup with target id if it's one of this tab's.
func (t *Tab) dropPopup(id target.ID) {
	t.mu.Lock()
	var gone *pageCtx
	for i, p := range t.pages {
		if i > 0 && p.tid == id {
			gone = p
		}
	}
	t.mu.Unlock()
	if gone != nil {
		gone.cancel()
		t.popClosed()
	}
}

// popClosed drops popups whose target went away and returns to the opener.
func (t *Tab) popClosed() {
	time.Sleep(200 * time.Millisecond)
	t.mu.Lock()
	changed := false
	for len(t.pages) > 1 && t.pages[len(t.pages)-1].ctx.Err() != nil {
		t.pages = t.pages[:len(t.pages)-1]
		changed = true
	}
	watching := len(t.subs) > 0
	t.mu.Unlock()
	if changed {
		if watching {
			t.restartCast()
		}
		var url string
		ctx, cancel := context.WithTimeout(t.active().ctx, 3*time.Second)
		_ = chromedp.Run(ctx, chromedp.Location(&url))
		cancel()
		t.mu.Lock()
		t.url = url
		t.mu.Unlock()
		go t.refreshTitle(url)
	}
}

func (t *Tab) busy() bool { return t.busyN.Load() > 0 }

func (t *Tab) close() {
	t.mu.Lock()
	pages := t.pages
	t.pages = nil
	for ch := range t.subs {
		close(ch)
	}
	t.subs = map[chan []byte]struct{}{}
	t.mu.Unlock()
	for i := len(pages) - 1; i >= 0; i-- {
		ctx, cancel := context.WithTimeout(pages[i].ctx, 3*time.Second)
		_ = chromedp.Run(ctx, page.Close())
		cancel()
		pages[i].cancel()
	}
}

// URL returns the current URL and title.
func (t *Tab) URL() (string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.url, t.title
}

func (t *Tab) Cursor() (float64, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursorX, t.cursorY
}

// ---------- screencast ----------

func (t *Tab) publishFrame(data []byte) {
	t.mu.Lock()
	t.lastFrame = data
	for ch := range t.subs {
		select {
		case ch <- data:
		default: // slow consumer: drop frame
		}
	}
	t.mu.Unlock()
}

// Subscribe starts streaming JPEG frames; call the returned func to stop.
func (t *Tab) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 2)
	t.mu.Lock()
	t.subs[ch] = struct{}{}
	first := len(t.subs) == 1
	last := t.lastFrame
	t.mu.Unlock()
	if last != nil {
		ch <- last
	}
	if first {
		t.restartCast()
	}
	// Headless only emits frames on change; push a fresh one now.
	go func() {
		if img, err := t.screenshot(70); err == nil {
			t.publishFrame(img)
		}
	}()
	return ch, func() {
		t.mu.Lock()
		if _, ok := t.subs[ch]; ok {
			delete(t.subs, ch)
			close(ch)
		}
		none := len(t.subs) == 0
		cast := t.casting
		if none {
			t.casting = nil
		}
		t.mu.Unlock()
		if none && cast != nil {
			ctx, cancel := context.WithTimeout(cast.ctx, 3*time.Second)
			_ = chromedp.Run(ctx, page.StopScreencast())
			cancel()
		}
	}
}

func (t *Tab) restartCast() {
	t.mu.Lock()
	old := t.casting
	p := t.activeLocked()
	t.casting = p
	t.mu.Unlock()
	if old != nil && old != p {
		ctx, cancel := context.WithTimeout(old.ctx, 3*time.Second)
		_ = chromedp.Run(ctx, page.StopScreencast())
		cancel()
	}
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	w, h := int64(t.b.mgr.opts.Width), int64(t.b.mgr.opts.Height)
	if err := chromedp.Run(ctx, page.StartScreencast().WithFormat(page.ScreencastFormatJpeg).WithQuality(60).WithMaxWidth(w).WithMaxHeight(h).WithEveryNthFrame(1)); err != nil {
		slog.Warn("start screencast", "err", err)
	}
}

// ---------- low level helpers ----------

func (t *Tab) run(timeout time.Duration, actions ...chromedp.Action) error {
	ctx, cancel := context.WithTimeout(t.active().ctx, timeout)
	defer cancel()
	return chromedp.Run(ctx, actions...)
}

// eval evaluates JS (retrying across navigations) and decodes into out.
func (t *Tab) eval(js string, out any) error {
	// Scripts don't run while a dialog holds the page.
	if err := t.blocked(); err != nil {
		return err
	}
	var err error
	for i := 0; i < 4; i++ {
		err = t.run(15*time.Second, chromedp.Evaluate(js, out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}))
		if err == nil {
			return nil
		}
		msg := err.Error()
		if strings.Contains(msg, "context was destroyed") || strings.Contains(msg, "Cannot find context") || strings.Contains(msg, "uniqueContextId") {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		return err
	}
	return err
}

func (t *Tab) screenshot(quality int64) ([]byte, error) {
	var buf []byte
	err := t.run(20*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		buf, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(quality).Do(ctx)
		return err
	}))
	return buf, err
}

// settle waits briefly for the page to finish loading after an action.
func (t *Tab) settle() {
	time.Sleep(350 * time.Millisecond)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if t.pendingDialog() != nil {
			return
		}
		var state string
		ctx, cancel := context.WithTimeout(t.active().ctx, 2*time.Second)
		err := chromedp.Run(ctx, chromedp.Evaluate(`document.readyState`, &state))
		cancel()
		if err == nil && state == "complete" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
}

// glide is how long the pointer in the live view takes to travel dist
// pixels; the app animates it over the same time.
func glide(dist float64) time.Duration {
	if dist < 1 {
		return 0
	}
	return time.Duration(min(220+0.3*dist, 700) * float64(time.Millisecond))
}

// setCursor moves the agent's pointer in the live view to x, y, with a
// click there when click is set. With wait, and someone watching, it
// returns once the pointer gets there, so the page reacts after the pointer
// arrives rather than before.
func (t *Tab) setCursor(x, y float64, click, wait bool) {
	t.mu.Lock()
	dist := math.Hypot(x-t.cursorX, y-t.cursorY)
	t.cursorX, t.cursorY = x, y
	watched := len(t.subs) > 0
	t.mu.Unlock()
	t.b.emit(t.taskID, TabEvent{Type: "cursor", X: x, Y: y, Click: click})
	if wait && watched {
		time.Sleep(glide(dist))
	}
}

func (t *Tab) mouseClick(x, y float64, button input.MouseButton, count int64) error {
	return t.input(10*time.Second, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx); err != nil {
			return err
		}
		for i := int64(1); i <= count; i++ {
			if err := input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(button).WithButtons(1).WithClickCount(i).Do(ctx); err != nil {
				return err
			}
			if err := input.DispatchMouseEvent(input.MouseReleased, x, y).WithButton(button).WithButtons(0).WithClickCount(i).Do(ctx); err != nil {
				return err
			}
		}
		return nil
	}))
}

var errNoElement = errors.New("element not found; the page may have changed — get a fresh browser state and use a current index")

// elementCenter scrolls element [idx] into view and returns a click point in
// page (top-level viewport) coordinates, following the element into whatever
// frame it lives in, plus a tag like "input:date".
func (t *Tab) elementCenter(idx int) (float64, float64, string, error) {
	r, err := t.ref(idx)
	if err != nil {
		return 0, 0, "", err
	}
	if r.local < 0 { // a frame we couldn't read: its stored center
		return r.x, r.y, "iframe", nil
	}
	var res struct {
		OK  bool    `json:"ok"`
		X   float64 `json:"x"`
		Y   float64 `json:"y"`
		W   float64 `json:"w"`
		H   float64 `json:"h"`
		Tag string  `json:"tag"`
	}
	js := fmt.Sprintf(`(() => {
		const el = (window.__aaEls||[])[%d];
		if (!el || !el.isConnected) return {ok:false};
		el.scrollIntoView({block:'center', inline:'center', behavior:'instant'});
		const a = (window.__aaAbs || (e => { const r = e.getBoundingClientRect(); return {x:r.left,y:r.top,w:r.width,h:r.height}; }))(el);
		return {ok:true, x:a.x, y:a.y, w:a.w, h:a.h, tag: el.tagName.toLowerCase() + (el.type ? ':' + el.type : '')};
	})()`, r.local)
	if r.frame != nil {
		err = evalIn(r.frame.ctx, js, &res)
	} else {
		err = t.eval(js, &res)
	}
	if err != nil {
		return 0, 0, "", err
	}
	if !res.OK {
		return 0, 0, "", errNoElement
	}
	lx, ly := res.X+min(res.W/2, 120), res.Y+res.H/2
	if r.frame != nil {
		// Where the frame is now (scrolling in it can scroll the page),
		// bringing the point into view if the frame is partly off screen.
		var o struct {
			OK bool    `json:"ok"`
			X  float64 `json:"x"`
			Y  float64 `json:"y"`
		}
		js := fmt.Sprintf(`(() => {
			const fr = (window.__aaXFrames||[])[%d];
			if (!fr || !fr.isConnected || !window.__aaAbs) return {ok:false};
			const origin = () => { const a = window.__aaAbs(fr), st = fr.ownerDocument.defaultView.getComputedStyle(fr);
				return {x: a.x + fr.clientLeft + (parseFloat(st.paddingLeft)||0), y: a.y + fr.clientTop + (parseFloat(st.paddingTop)||0)}; };
			let o = origin();
			const y = o.y + %f;
			if (y < 0 || y > innerHeight) { window.scrollBy(0, y - innerHeight / 2); o = origin(); }
			return {ok:true, x: o.x, y: o.y};
		})()`, r.xf, ly)
		if err := t.eval(js, &o); err == nil && o.OK {
			lx, ly = lx+o.X, ly+o.Y
		} else {
			lx, ly = lx+r.x, ly+r.y // where it was at the last state
		}
	}
	w, h := float64(t.b.mgr.opts.Width), float64(t.b.mgr.opts.Height)
	return min(max(lx, 1), w-1), min(max(ly, 1), h-1), res.Tag, nil
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
