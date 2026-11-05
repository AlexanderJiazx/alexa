package api

// The pictures blocks show: site icons and remote images, plus the
// calendar file for an entry's itinerary. The server fetches the pictures
// itself, so the user's browser never contacts third parties (no favicon
// services that would learn what they browse, no merchants seeing their
// address), and every fetch goes through one guarded client that only
// reaches public addresses, so neither endpoint can probe the server's own
// network. Both are cached on disk under DATA_DIR/cache.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"alexagent/internal/agent"
	"alexagent/internal/netguard"
)

const (
	faviconMax    = 256 << 10 // icon bytes, and how much of a home page is read for its <link rel=icon>
	faviconTTL    = 7 * 24 * time.Hour
	faviconMiss   = 24 * time.Hour // how long a site without an icon isn't asked again
	faviconBudget = 9 * time.Second
	imageMax      = 5 << 20
	imageTTL      = 7 * 24 * time.Hour
	imageMiss     = time.Hour
	imageBudget   = 15 * time.Second
	fetchTimeout  = 3 * time.Second // one request for an icon
	imageTimeout  = 10 * time.Second
	userAgent     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36"
)

var (
	errBlocked = netguard.ErrBlocked
	errNoImage = errors.New("no image")
)

// fetcher is the guarded HTTP client for media, with the in-flight fetches
// it shares between requests for the same thing.
type fetcher struct {
	client *http.Client
	// lookup resolves a host name; allowed decides which addresses may be
	// reached; connect opens the connection. Tests replace them.
	lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
	allowed func(netip.Addr) bool
	connect func(ctx context.Context, network, addr string) (net.Conn, error)

	mu       sync.Mutex
	inflight map[string]*flight
}

type flight struct {
	done chan struct{}
	ct   string
	body []byte
	err  error
}

var media = newFetcher(nil)

func newFetcher(tlsConf *tls.Config) *fetcher {
	d := &net.Dialer{Timeout: fetchTimeout}
	f := &fetcher{
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		allowed:  publicAddr,
		connect:  d.DialContext,
		inflight: map[string]*flight{},
	}
	f.client = &http.Client{
		Transport: &http.Transport{
			Proxy:                  nil, // a proxy would make the address check meaningless
			DialContext:            f.dial,
			TLSClientConfig:        tlsConf,
			TLSHandshakeTimeout:    fetchTimeout,
			ResponseHeaderTimeout:  fetchTimeout,
			MaxResponseHeaderBytes: 64 << 10,
			MaxIdleConns:           16,
			IdleConnTimeout:        30 * time.Second,
			ForceAttemptHTTP2:      true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return errors.New("too many redirects")
			}
			// Every hop is dialled through f.dial, so the address check
			// holds for redirects too. A redirect may not leave https:
			// block images must be https, and a downgrade would let
			// anyone on the path swap the picture.
			if req.URL.Scheme != "https" && (req.URL.Scheme != "http" || via[0].URL.Scheme == "https") {
				return errBlocked
			}
			return nil
		},
	}
	return f
}

// dial connects only to public addresses, on the standard ports, through
// the shared guard (netguard.Dialer): it resolves the host itself and
// connects to the address it checked, so a name can't resolve to a public
// address for the check and a private one for the connection. Redirects
// come through here too.
func (f *fetcher) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := netguard.Dialer{Ports: []string{"443", "80"}, Lookup: f.lookup, Allowed: f.allowed, Connect: f.connect}
	return d.DialContext(ctx, network, addr)
}

// publicAddr reports whether an address is on the public internet.
var publicAddr = netguard.Public

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// mediaHost checks a host name from a request: a public IP, or a dotted
// name that isn't a local one.
func mediaHost(h string) (string, bool) {
	h = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
	if ip, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		return ip.String(), publicAddr(ip)
	}
	if len(h) > 253 || !hostRe.MatchString(h) {
		return "", false
	}
	tld := h[strings.LastIndex(h, ".")+1:]
	if strings.Trim(tld, "0123456789") == "" {
		return "", false
	}
	for _, s := range []string{".localhost", ".local", ".internal", ".lan", ".home.arpa", ".intranet", ".corp", ".test", ".invalid"} {
		if strings.HasSuffix(h, s) {
			return "", false
		}
	}
	return h, true
}

// get fetches a URL, reading at most max bytes. Past max, an image is
// refused and a page is cut (its head is what matters).
func (f *fetcher) get(ctx context.Context, u, accept string, max int64, timeout time.Duration, cut bool) ([]byte, *url.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, errBlocked) {
			return nil, nil, errBlocked
		}
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, errNoImage
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(body)) > max {
		if !cut {
			return nil, nil, fmt.Errorf("larger than %d KB", max>>10)
		}
		body = body[:max]
	}
	return body, resp.Request.URL, nil
}

// image fetches an image and works out its type from its bytes, never
// from what the server says.
func (f *fetcher) image(ctx context.Context, u string, max int64, timeout time.Duration, svg bool) (string, []byte, error) {
	body, _, err := f.get(ctx, u, "image/avif,image/webp,image/png,image/svg+xml,image/*;q=0.8", max, timeout, false)
	if err != nil {
		return "", nil, err
	}
	ct := imageType(body, svg)
	if ct == "" {
		return "", nil, errNoImage
	}
	return ct, body, nil
}

func imageType(b []byte, svg bool) string {
	switch ct := http.DetectContentType(b); ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon", "image/bmp", "image/avif":
		return ct
	}
	if len(b) > 12 && (string(b[4:12]) == "ftypavif" || string(b[4:12]) == "ftypavis") {
		return "image/avif"
	}
	if svg && looksSVG(b) {
		return "image/svg+xml"
	}
	return ""
}

func looksSVG(b []byte) bool {
	head := bytes.ToLower(b[:min(len(b), 2048)])
	head = bytes.TrimSpace(bytes.TrimPrefix(head, []byte("\xef\xbb\xbf")))
	if bytes.HasPrefix(head, []byte("<svg")) {
		return true
	}
	return (bytes.HasPrefix(head, []byte("<?xml")) || bytes.HasPrefix(head, []byte("<!--")) || bytes.HasPrefix(head, []byte("<!doctype svg"))) && bytes.Contains(head, []byte("<svg"))
}

// favicon finds a site's icon: apple-touch-icon.png (the sharpest), then
// /favicon.ico, then the icons its home page links to.
func (f *fetcher) favicon(ctx context.Context, host string) (string, []byte, error) {
	base := "https://" + host
	for _, p := range []string{"/apple-touch-icon.png", "/favicon.ico"} {
		ct, b, err := f.image(ctx, base+p, faviconMax, fetchTimeout, true)
		if err == nil {
			return ct, b, nil
		}
		var dnsErr *net.DNSError
		if errors.Is(err, errBlocked) || errors.As(err, &dnsErr) {
			return "", nil, err
		}
	}
	page, final, err := f.get(ctx, base+"/", "text/html", faviconMax, fetchTimeout, true)
	if err != nil {
		return "", nil, err
	}
	for _, href := range iconLinks(page, final) {
		if ct, b, err := f.image(ctx, href, faviconMax, fetchTimeout, true); err == nil {
			return ct, b, nil
		}
	}
	return "", nil, errNoImage
}

var (
	linkTagRe = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRe    = regexp.MustCompile(`(?is)\b(rel|href|sizes)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// iconLinks lists the icons a page's head links to, best first: Apple
// touch icons, then large icons, then any icon. Mask icons are skipped
// (they're single-colour stencils).
func iconLinks(page []byte, base *url.URL) []string {
	if i := bytes.Index(bytes.ToLower(page), []byte("</head>")); i >= 0 {
		page = page[:i]
	}
	type cand struct {
		href  string
		score int
	}
	var cands []cand
	for _, tag := range linkTagRe.FindAll(page, 40) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllSubmatch(tag, -1) {
			attrs[strings.ToLower(string(m[1]))] = html.UnescapeString(string(m[2]) + string(m[3]) + string(m[4]))
		}
		rel := " " + strings.ToLower(attrs["rel"]) + " "
		if !strings.Contains(rel, "icon") || strings.Contains(rel, "mask-icon") || attrs["href"] == "" {
			continue
		}
		ref, err := url.Parse(strings.TrimSpace(attrs["href"]))
		if err != nil || base == nil {
			continue
		}
		u := base.ResolveReference(ref)
		if u.Scheme != "https" && u.Scheme != "http" {
			continue
		}
		score := 1
		switch {
		case strings.Contains(rel, "apple-touch-icon"):
			score = 3
		case strings.Contains(attrs["sizes"], "192") || strings.Contains(attrs["sizes"], "180") || strings.Contains(attrs["sizes"], "128") || strings.Contains(attrs["sizes"], "96"):
			score = 2
		}
		cands = append(cands, cand{u.String(), score})
	}
	var out []string
	for s := 3; s >= 1; s-- {
		for _, c := range cands {
			if c.score == s && len(out) < 3 {
				out = append(out, c.href)
			}
		}
	}
	return out
}

// cached serves key from the disk cache in dir, or fetches it once (even
// when several requests ask at the same time) and caches the result. A
// failed fetch is remembered for miss, so a site without an icon isn't
// asked on every page view. The fetch runs on its own deadline, so a
// browser that gives up doesn't waste it.
func (f *fetcher) cached(dir, key string, ttl, miss, budget time.Duration, fetch func(context.Context) (string, []byte, error)) (string, []byte, error) {
	path := filepath.Join(dir, key)
	if ct, body, ok := readCached(path, ttl); ok {
		return ct, body, nil
	}
	if fi, err := os.Stat(path + ".miss"); err == nil && time.Since(fi.ModTime()) < miss {
		return "", nil, errNoImage
	}
	f.mu.Lock()
	if fl := f.inflight[path]; fl != nil {
		f.mu.Unlock()
		<-fl.done
		return fl.ct, fl.body, fl.err
	}
	fl := &flight{done: make(chan struct{})}
	f.inflight[path] = fl
	f.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	fl.ct, fl.body, fl.err = fetch(ctx)
	cancel()
	if err := os.MkdirAll(dir, 0o700); err == nil {
		if fl.err != nil {
			_ = os.WriteFile(path+".miss", nil, 0o600)
		} else {
			writeCached(path, fl.ct, fl.body)
			_ = os.Remove(path + ".miss")
		}
	}
	f.mu.Lock()
	delete(f.inflight, path)
	f.mu.Unlock()
	close(fl.done)
	return fl.ct, fl.body, fl.err
}

// A cache file holds the content type, a newline, then the bytes.
func readCached(path string, ttl time.Duration) (string, []byte, bool) {
	fi, err := os.Stat(path)
	if err != nil || time.Since(fi.ModTime()) > ttl {
		return "", nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, false
	}
	ct, body, ok := bytes.Cut(b, []byte("\n"))
	if !ok || !bytes.HasPrefix(ct, []byte("image/")) {
		return "", nil, false
	}
	return string(ct), body, true
}

func writeCached(path, ct string, body []byte) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(ct+"\n"), body...), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// serveImage writes a fetched picture. Its type was sniffed from its
// bytes; the CSP keeps an SVG opened on its own inert.
func serveImage(w http.ResponseWriter, ct string, body []byte, maxAge time.Duration) {
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(maxAge.Seconds())))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = w.Write(body)
}

// handleFavicon serves a site's icon, for link rows, sources and steps:
// GET /api/favicon/{host}. A site without one answers 404, and the app
// draws a monogram.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	host, ok := mediaHost(r.PathValue("host"))
	if !ok {
		writeErr(w, 400, "not a public host")
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "cache", "favicons")
	ct, body, err := media.cached(dir, host, faviconTTL, faviconMiss, faviconBudget, func(ctx context.Context) (string, []byte, error) {
		return media.favicon(ctx, host)
	})
	if err != nil {
		writeErr(w, 404, "no icon")
		return
	}
	serveImage(w, ct, body, faviconTTL)
}

// handleImage serves a remote image a block showed: GET /api/img/{token}.
// Tokens are minted when blocks are checked (agent.mintImage), so this
// only fetches https images Alexa showed this user; it isn't an open
// proxy.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	src, err := s.rt.ImageSource(userFrom(r).ID, token)
	if err != nil || !strings.HasPrefix(src, "https://") {
		writeErr(w, 404, "image not found")
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "cache", "img")
	ct, body, err := media.cached(dir, token, imageTTL, imageMiss, imageBudget, func(ctx context.Context) (string, []byte, error) {
		return media.image(ctx, src, imageMax, imageTimeout, false)
	})
	if err != nil {
		writeErr(w, 404, "image unavailable")
		return
	}
	serveImage(w, ct, body, imageTTL)
}

// handleEntryICS turns the itinerary, events and stays in an entry's
// blocks into a calendar file: GET /api/entries/{id}/calendar.ics.
func (s *Server) handleEntryICS(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 404, "entry not found")
		return
	}
	feed, err := s.st.Feed(r.Context(), u.ID, id+1, 1)
	if err != nil || len(feed) != 1 || feed[0].ID != id {
		writeErr(w, 404, "entry not found")
		return
	}
	var data struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	_ = json.Unmarshal(feed[0].Data, &data)
	events := agent.BlockEvents(data.Blocks)
	if len(events) == 0 {
		writeErr(w, 404, "no events")
		return
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		loc = time.UTC
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(firstTitle(events))+`.ics"`)
	_, _ = io.WriteString(w, buildICS(events, "entry-"+strconv.FormatInt(id, 10), loc))
}

func firstTitle(events []agent.CalEvent) string {
	if len(events) == 1 {
		return events[0].Title
	}
	return "Itinerary"
}
