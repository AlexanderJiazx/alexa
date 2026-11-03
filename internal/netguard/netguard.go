// Package netguard keeps the server's own requests to addresses it was
// handed (a picture a block shows, a page Alexa fetches) on the public
// internet, so neither can probe the server's own network: loopback, the
// LAN, or a cloud's metadata service at 169.254.169.254.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
)

// ErrBlocked is returned for a connection to an address that isn't public.
var ErrBlocked = errors.New("not a public address")

// Dialer connects only to public addresses. It resolves the host itself and
// connects to the address it checked, so a name can't resolve to a public
// address for the check and a private one for the connection (DNS
// rebinding). An http.Transport dials every request through it, redirects
// included, so the check holds for them too. Give that transport no proxy:
// through one, the dialer would only ever see the proxy's address.
type Dialer struct {
	// Ports are the ports that may be reached; none means any.
	Ports []string
	// Lookup resolves a host name; Allowed decides which addresses may be
	// reached; Connect opens the connection. Nil means the system
	// resolver, Public and a plain net.Dialer. Tests replace them.
	Lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
	Allowed func(netip.Addr) bool
	Connect func(ctx context.Context, network, addr string) (net.Conn, error)
}

// DialContext is a net.Dialer's DialContext for public addresses only.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if len(d.Ports) > 0 && !slices.Contains(d.Ports, port) {
		return nil, ErrBlocked
	}
	allowed := d.Allowed
	if allowed == nil {
		allowed = Public
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else if d.Lookup != nil {
		if ips, err = d.Lookup(ctx, host); err != nil {
			return nil, err
		}
	} else if ips, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no address for %s", host)
	}
	// A name with any private address is refused outright, rather than
	// trying its public ones.
	for _, ip := range ips {
		if !allowed(ip.Unmap()) {
			return nil, ErrBlocked
		}
	}
	connect := d.Connect
	if connect == nil {
		connect = (&net.Dialer{}).DialContext
	}
	var last error
	for _, ip := range ips {
		c, err := connect(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// blockedPrefixes are non-public ranges that netip's predicates don't
// cover: shared and reserved space, documentation ranges, and IPv6
// transition prefixes that can carry a private IPv4 address.
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "2001::/32", "2001:db8::/32", "2002::/16", "fec0::/10"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// Public reports whether an address is on the public internet.
func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
