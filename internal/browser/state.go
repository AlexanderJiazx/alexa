package browser

import (
	"fmt"
	"strings"
	"time"
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
	// With no frames in view it only lets go of frames that are gone.
	refs = t.indexFrames(p, &ix, refs)
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
	b.WriteString(downloadNote(t.b.downloads.take(8*time.Second, t.pause)))
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
		// A frame has a context of its own, so tie it to the bound call:
		// a Stop or take-over then ends this read at once.
		if err := t.cut(); err != nil {
			return err
		}
		ctx, done := t.withBound(r.frame.ctx)
		defer done()
		if err := evalIn(ctx, js, out); err != nil {
			if cerr := t.cut(); cerr != nil {
				return cerr
			}
			return err
		}
		return nil
	}
	return t.eval(js, out)
}
