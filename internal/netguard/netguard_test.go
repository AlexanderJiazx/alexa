package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "0.0.0.0": false, "100.64.0.1": false, "::1": false,
		"fe80::1": false, "fc00::1": false, "::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false,
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
}

// The dialer resolves the name itself and connects only if every address
// it has is allowed, to that address: a name can't point it somewhere
// private, nor change its answer between the check and the connection.
func TestDialer(t *testing.T) {
	var dialed []string
	d := &Dialer{
		Ports: []string{"443"},
		Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			switch host {
			case "mixed.example":
				return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")}, nil
			case "public.example":
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}
			return nil, errors.New("no such host")
		},
		Connect: func(_ context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			c, _ := net.Pipe()
			return c, nil
		},
	}
	ctx := context.Background()
	for _, addr := range []string{"mixed.example:443", "127.0.0.1:443", "[::1]:443", "public.example:80"} {
		if _, err := d.DialContext(ctx, "tcp", addr); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: %v, want ErrBlocked", addr, err)
		}
	}
	if len(dialed) != 0 {
		t.Fatalf("connected to %v", dialed)
	}
	c, err := d.DialContext(ctx, "tcp", "public.example:443")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if len(dialed) != 1 || dialed[0] != "93.184.216.34:443" {
		t.Errorf("connected to %v, want the checked address", dialed)
	}
}
