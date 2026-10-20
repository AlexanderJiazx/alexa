package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Cross-origin frames (embedded forms, booking widgets, consent banners) run
// in their own renderer, out of reach of the page's scripts. Chrome exposes
// each as an "iframe" target, so they're indexed and read through a chromedp
// context attached to that target, while clicks and typing still go through
// the page: Chrome routes input to whichever frame is under the pointer or
// has focus.

// frameCtx is a cross-origin frame we've attached to.
type frameCtx struct {
	id  target.ID
	ctx context.Context
}

// maxFrames bounds how many frames one page attaches to.
const maxFrames = 24

// frame returns a context attached to a cross-origin frame of page p. The
// context lives as long as the page: cancelling chromedp's context for a
// frame closes its target, which takes the page down with it.
func (t *Tab) frame(p *pageCtx, id target.ID) *frameCtx {
	t.mu.Lock()
	if f := p.frames[id]; f != nil {
		t.mu.Unlock()
		return f
	}
	full := len(p.frames) >= maxFrames
	t.mu.Unlock()
	if full {
		return nil
	}
	ctx, _ := chromedp.NewContext(p.ctx, chromedp.WithTargetID(id))
	if err := runWithTimeout(ctx, 10*time.Second); err != nil {
		slog.Debug("attach frame failed", "target", id, "err", err)
		return nil
	}
	// Attached with its page domain on, the frame's dialogs wait for us too.
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			t.onDialog(ctx, e)
		}
	})
	f := &frameCtx{id: id, ctx: ctx}
	t.mu.Lock()
	if p.frames == nil {
		p.frames = map[target.ID]*frameCtx{}
	}
	p.frames[id] = f
	t.mu.Unlock()
	return f
}

// crossFrames attaches to the cross-origin frames the page's last index (or
// read) listed in window[list], keyed by their position in that list.
func (t *Tab) crossFrames(p *pageCtx, list string, n int) map[int]*frameCtx {
	if n == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	var infos []*target.Info
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		infos, err = target.GetTargets().Do(c)
		return err
	})); err != nil {
		return nil
	}
	live := map[target.ID]bool{}
	out := map[int]*frameCtx{}
	for _, in := range infos {
		if in.Type != "iframe" {
			continue
		}
		live[in.TargetID] = true
		k := -1
		// The frame's owner element is in this page only if it's ours.
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			backend, _, err := dom.GetFrameOwner(cdp.FrameID(in.TargetID)).Do(c)
			if err != nil {
				return err
			}
			obj, err := dom.ResolveNode().WithBackendNodeID(backend).Do(c)
			if err != nil {
				return err
			}
			res, exc, err := runtime.CallFunctionOn(fmt.Sprintf(`function() { return (top[%q] || []).indexOf(this); }`, list)).
				WithObjectID(obj.ObjectID).WithReturnByValue(true).Do(c)
			if err != nil {
				return err
			}
			if exc != nil {
				return fmt.Errorf("%s", exc.Text)
			}
			return json.Unmarshal(res.Value, &k)
		}))
		if err != nil || k < 0 || k >= n {
			continue
		}
		if f := t.frame(p, in.TargetID); f != nil {
			out[k] = f
		}
	}
	// Forget frames that are gone (their contexts end with the page).
	t.mu.Lock()
	for id := range p.frames {
		if !live[id] {
			delete(p.frames, id)
		}
	}
	t.mu.Unlock()
	return out
}

// evalIn evaluates JS in a frame (retrying across navigations).
func evalIn(ctx context.Context, js string, out any) error {
	var err error
	for i := 0; i < 3; i++ {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		err = chromedp.Run(c, chromedp.Evaluate(js, out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}))
		cancel()
		if err == nil {
			return nil
		}
		msg := err.Error()
		if strings.Contains(msg, "context was destroyed") || strings.Contains(msg, "Cannot find context") || strings.Contains(msg, "uniqueContextId") {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		return err
	}
	return err
}

// indexFrames indexes the elements and text of the cross-origin frames in
// view, adding them after the page's own.
func (t *Tab) indexFrames(p *pageCtx, ix *indexResult, refs []elemRef) []elemRef {
	frames := t.crossFrames(p, "__aaXFrames", len(ix.XFrames))
	var texts []string
	for k, fb := range ix.XFrames {
		f := frames[k]
		if f == nil {
			// Couldn't look inside: offer the frame itself as a target.
			if len(ix.Els) < 250 {
				ix.Els = append(ix.Els, element{D: fmt.Sprintf("<iframe src=%q> (embedded frame %q; its contents couldn't be read: click inside it by coordinates, then type with index 0)", fb.Src, fb.Name),
					X: fb.Clip.L, Y: fb.Clip.T, W: fb.Clip.R - fb.Clip.L, H: fb.Clip.B - fb.Clip.T})
				refs = append(refs, elemRef{local: -1, x: (fb.Clip.L + fb.Clip.R) / 2, y: (fb.Clip.T + fb.Clip.B) / 2})
			}
			continue
		}
		opts := map[string]any{"frame": true, "maxEl": max(250-len(ix.Els), 0), "maxText": 1200,
			"clip": map[string]float64{"l": fb.Clip.L - fb.X, "t": fb.Clip.T - fb.Y, "r": fb.Clip.R - fb.X, "b": fb.Clip.B - fb.Y}}
		o, _ := json.Marshal(opts)
		var fr indexResult
		if err := evalIn(f.ctx, fmt.Sprintf("(%s)(%s)", indexFn, o), &fr); err != nil {
			slog.Debug("index frame failed", "frame", f.id, "err", err)
			continue
		}
		label := fmt.Sprintf(" (in frame %q)", fb.Name)
		for i, e := range fr.Els {
			e.D += label
			e.X += fb.X
			e.Y += fb.Y
			ix.Els = append(ix.Els, e)
			refs = append(refs, elemRef{frame: f, xf: k, local: i + 1, x: fb.X, y: fb.Y})
		}
		if fr.Text != "" {
			texts = append(texts, fmt.Sprintf("[In frame %q:] %s", fb.Name, fr.Text))
		}
	}
	if len(texts) > 0 {
		ix.Text = strings.TrimSpace(ix.Text + " " + strings.Join(texts, " "))
	}
	return refs
}
