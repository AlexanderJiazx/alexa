package browser

import (
	"context"
	"sync"
	"testing"
	"time"
)

// bareTab is a tab with no Chrome behind it: what it asks Chrome fails at
// once, which is all the live view's bookkeeping needs.
func bareTab() *Tab {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Browser{mgr: &Manager{opts: Options{Width: 800, Height: 600}}}
	return &Tab{b: b, ctx: ctx, pages: []*pageCtx{{ctx: ctx, cancel: cancel}}, subs: map[chan []byte]struct{}{}}
}

// Subscribing hands the viewer the last frame, and never sends on a
// channel the tab closed meanwhile or blocks on one frames filled (BH-11).
func TestSubscribeRacesClose(t *testing.T) {
	t0 := time.Now()
	for i := 0; i < 300 && time.Since(t0) < 5*time.Second; i++ {
		tab := bareTab()
		tab.lastFrame = []byte("last")
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			ch, stop := tab.Subscribe()
			for range ch { // ends when the tab closes
			}
			stop()
		}()
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 5; j++ {
				tab.publishFrame([]byte("frame"))
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			tab.close()
		}()
		close(start)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Subscribe, a frame or close got stuck")
		}
	}

	tab := bareTab()
	tab.lastFrame = []byte("last")
	ch, stop := tab.Subscribe()
	defer stop()
	select {
	case f := <-ch:
		if string(f) != "last" {
			t.Errorf("first frame = %q, want the last one", f)
		}
	case <-time.After(time.Second):
		t.Error("a new viewer didn't get the last frame")
	}
}
