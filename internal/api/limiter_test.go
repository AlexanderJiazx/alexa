package api

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"alexagent/internal/config"
)

// Behind a trusted proxy, sign-in limits go by the address it reports, so
// one visitor's attempts don't hold back everyone else's; without one, the
// header is anyone's to set and is ignored.
func TestClientIPBehindAProxy(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/login", nil)
	r.RemoteAddr = "172.18.0.1:51234"
	r.Header.Set("X-Real-IP", "203.0.113.7")
	direct := &Server{cfg: &config.Config{}}
	if got := direct.clientIP(r); got != "172.18.0.1" {
		t.Errorf("without TRUST_PROXY: %s, want the peer", got)
	}
	proxied := &Server{cfg: &config.Config{TrustProxy: true}}
	if got := proxied.clientIP(r); got != "203.0.113.7" {
		t.Errorf("with TRUST_PROXY: %s, want X-Real-IP", got)
	}
	r.Header.Set("X-Real-IP", "not an address")
	if got := proxied.clientIP(r); got != "172.18.0.1" {
		t.Errorf("with a bad X-Real-IP: %s, want the peer", got)
	}
}

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
