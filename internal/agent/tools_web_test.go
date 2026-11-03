package agent

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

// allowFetchFrom lets fetch_url reach the given addresses (a test's local
// server) for the length of a test, and resolves every name to the first.
func allowFetchFrom(t *testing.T, addrs ...string) {
	t.Helper()
	old := *webGuard
	ok := map[netip.Addr]bool{}
	for _, a := range addrs {
		ok[netip.MustParseAddr(a)] = true
	}
	webGuard.Allowed = func(ip netip.Addr) bool { return ok[ip] }
	webGuard.Lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr(addrs[0])}, nil
	}
	t.Cleanup(func() { *webGuard = old })
}

// fetch_url only reaches the public internet, a redirect's target and a
// name that resolves to a private address included (BH-4).
func TestFetchURLRefusesPrivateAddresses(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if to := r.URL.Query().Get("to"); to != "" {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()
	tc := shellEnv(t)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	// The real guard: loopback, the metadata service and the LAN.
	for _, u := range []string{srv.URL, "http://localhost:" + port + "/", "http://[::1]:" + port + "/", "http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/", "http://0.0.0.0:" + port + "/"} {
		if res, err := fetchURL(tc, map[string]any{"url": u}); err == nil || !strings.Contains(err.Error(), "public internet") {
			t.Errorf("fetch_url %s = %v, %v; want it refused", u, res, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the local server was reached %d times", n)
	}

	// With the test server counted as public: it's reached, but neither a
	// redirect to a private address nor a name that resolves to one is.
	allowFetchFrom(t, "127.0.0.1")
	res, err := fetchURL(tc, map[string]any{"url": srv.URL})
	if err != nil || !strings.Contains(res.Text, "secret") {
		t.Fatalf("fetch_url of an allowed address = %v, %v", res, err)
	}
	before := hits.Load()
	if res, err := fetchURL(tc, map[string]any{"url": srv.URL + "/?to=http://127.0.0.2:" + port + "/"}); err == nil || !strings.Contains(err.Error(), "public internet") {
		t.Errorf("a redirect to a private address = %v, %v; want it refused", res, err)
	}
	if n := hits.Load() - before; n != 1 {
		t.Errorf("%d requests for a redirect to a private address, want only the first hop", n)
	}
	webGuard.Lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.168.1.1")}, nil
	}
	before = hits.Load()
	if res, err := fetchURL(tc, map[string]any{"url": "http://rebind.example.com:" + port + "/"}); err == nil {
		t.Errorf("a name with a private address = %v; want it refused", res)
	}
	if n := hits.Load() - before; n != 0 {
		t.Errorf("a name with a private address was reached %d times", n)
	}
}
