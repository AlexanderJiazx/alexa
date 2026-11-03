package browser

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A cross-origin frame that goes away is let go of, without closing the
// page (BH-13): its chromedp context ends rather than living, with its
// goroutines, until the page closes.
func TestGoneFramesAreReleased(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/": `<!doctype html><title>Outer</title><body><h1>Outer</h1>
			<button onclick="document.querySelector('iframe').remove(); this.textContent='Removed'">Remove widget</button>
			<iframe src="http://localhost:PORT/inner" title="Booking widget" style="width:500px;height:150px"></iframe>`,
		"/inner": innerPage,
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	st := mustState(t, tab)
	if !strings.Contains(st.Text, `(in frame "Booking widget")`) {
		t.Fatalf("frame elements missing:\n%s", st.Text)
	}
	p := tab.active()
	tab.mu.Lock()
	var f *frameCtx
	for _, fc := range p.frames {
		f = fc
	}
	tab.mu.Unlock()
	if f == nil {
		t.Fatal("the cross-origin frame wasn't attached")
	}

	if _, err := tab.Click(indexOf(t, st, "Remove widget", 0)); err != nil {
		t.Fatal(err)
	}
	st = mustState(t, tab)
	if strings.Contains(st.Text, "in frame") || !strings.Contains(st.Text, "Removed") {
		t.Fatalf("after removing the frame:\n%s", st.Text)
	}
	tab.mu.Lock()
	n := len(p.frames)
	tab.mu.Unlock()
	if n != 0 {
		t.Errorf("%d frames still kept", n)
	}
	select {
	case <-f.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the gone frame's context is still live")
	}
	// The page is still there.
	if p.ctx.Err() != nil {
		t.Fatal("releasing the frame closed the page")
	}
	st = mustState(t, tab)
	if !strings.Contains(st.Text, "Outer") {
		t.Errorf("the page after the frame went:\n%s", st.Text)
	}
}

// Letting go of a frame that's still there detaches from it without
// closing it, or the page it's in.
func TestReleaseLeavesALiveFrame(t *testing.T) {
	tab, port := newFeatureBrowser(t, map[string]string{
		"/": `<!doctype html><title>Outer</title><body><h1>Outer</h1>
			<iframe src="http://localhost:PORT/inner" title="Booking widget" style="width:500px;height:150px"></iframe>`,
		"/inner": innerPage,
	})
	if err := tab.Navigate(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	mustState(t, tab)
	p := tab.active()
	tab.mu.Lock()
	var f *frameCtx
	for id, fc := range p.frames {
		f = fc
		delete(p.frames, id)
	}
	tab.mu.Unlock()
	if f == nil {
		t.Fatal("the cross-origin frame wasn't attached")
	}
	release(f.ctx, f.cancel)
	time.Sleep(300 * time.Millisecond)
	if p.ctx.Err() != nil {
		t.Fatal("releasing a live frame closed the page")
	}
	// It's attached to afresh, and still works.
	st := mustState(t, tab)
	if _, err := tab.Type(indexOf(t, st, `placeholder="Your name"`, 0), "Alex", true, false); err != nil {
		t.Fatal(err)
	}
	st = mustState(t, tab)
	if _, err := tab.Click(indexOf(t, st, `Book (in frame "Booking widget")`, 0)); err != nil {
		t.Fatal(err)
	}
	if st = mustState(t, tab); !strings.Contains(st.Text, "Booked for Alex") {
		t.Errorf("the frame after a release:\n%s", st.Text)
	}
}
