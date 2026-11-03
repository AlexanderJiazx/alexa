package api

import (
	"fmt"
	"testing"
	"time"
)

// The limiter forgets addresses that stopped trying, so its map doesn't
// keep every address that ever tried to sign in (BH-13), and still holds
// back one that keeps trying.
func TestLimiterForgetsQuietAddresses(t *testing.T) {
	var l limiter
	old := time.Now().Add(-limitWindow - time.Minute)
	l.hits = map[string][]time.Time{}
	for i := 0; i < 1000; i++ {
		l.hits[fmt.Sprintf("10.0.%d.%d", i/256, i%256)] = []time.Time{old}
	}
	for i := 0; i < 20; i++ {
		if !l.allow("192.0.2.1") {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if l.allow("192.0.2.1") {
		t.Error("the 21st attempt in the window was allowed")
	}
	if len(l.hits) != 1 {
		t.Errorf("the limiter keeps %d addresses, want only the one still trying", len(l.hits))
	}
	// Until the next sweep, new addresses are just added.
	l.allow("192.0.2.2")
	if len(l.hits) != 2 {
		t.Errorf("%d addresses after a second one tried", len(l.hits))
	}
}
