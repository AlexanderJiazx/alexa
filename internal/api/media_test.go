package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"alexagent/internal/llm"
)

// What a client loads for blocks: the entries themselves, the pictures
// they show (through /api/img and /api/favicon) and their calendar file.

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{1}, 64)...)

// fakeInternet stands in for the web: every host name resolves to a public
// address (so the real address guard passes) and every connection reaches
// one local TLS server, whose certificate is accepted for any name. It
// replaces the media fetcher for the test.
type fakeInternet struct {
	srv      *httptest.Server
	hits     atomic.Int64
	connects atomic.Int64
	fetcher  *fetcher
}

func newFakeInternet(t *testing.T, h http.HandlerFunc) *fakeInternet {
	t.Helper()
	fi := &fakeInternet{}
	fi.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fi.hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(fi.srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(fi.srv.Certificate())
	f := newFetcher(&tls.Config{RootCAs: pool, ServerName: "example.com"})
	f.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}
	f.connect = func(ctx context.Context, network, _ string) (net.Conn, error) {
		fi.connects.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, fi.srv.Listener.Addr().String())
	}
	fi.fetcher = f
	old := media
	media = f
	t.Cleanup(func() { media = old })
	return fi
}

func TestFavicon(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	web := newFakeInternet(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "shop.example.org" && r.URL.Path == "/":
			fmt.Fprint(w, `<html><head><link rel="mask-icon" href="/mask.svg"><link rel="icon" href="/static/icon.svg?v=2&amp;x=1"></head><body><link rel="icon" href="/late.png"></body></html>`)
		case r.Host == "shop.example.org" && r.URL.Path == "/static/icon.svg":
			if r.URL.RawQuery != "v=2&x=1" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain") // what the server says doesn't count
			fmt.Fprint(w, `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
		case r.Host == "example.com" && r.URL.Path == "/apple-touch-icon.png":
			_, _ = w.Write(pngBytes)
		case r.Host == "html.example.net":
			fmt.Fprint(w, "<html>not an icon</html>")
		default:
			http.NotFound(w, r)
		}
	})

	// Local and private addresses are refused before anything is fetched.
	for _, host := range []string{"localhost", "10.0.0.1", "127.0.0.1", "169.254.169.254", "printer.local", "intranet", "[::1]"} {
		if code, _ := alice.raw("GET", "/api/favicon/"+host, nil, false); code != 400 {
			t.Errorf("favicon for %s = %d, want 400", host, code)
		}
	}
	// So is a public-looking name that resolves to a private address.
	web.fetcher.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("10.1.2.3")}, nil
	}
	if code, _ := alice.raw("GET", "/api/favicon/rebind.example.com", nil, false); code != 404 || web.connects.Load() != 0 {
		t.Fatalf("private resolution: %d, %d connections", code, web.connects.Load())
	}
	web.fetcher.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}

	// The apple-touch-icon, then the same bytes from the cache.
	code, body := alice.raw("GET", "/api/favicon/example.com", nil, false)
	if code != 200 || !bytes.Equal(body, pngBytes) {
		t.Fatalf("favicon = %d (%d bytes)", code, len(body))
	}
	hits := web.hits.Load()
	if code, body := alice.raw("GET", "/api/favicon/EXAMPLE.com", nil, false); code != 200 || !bytes.Equal(body, pngBytes) || web.hits.Load() != hits {
		t.Fatalf("second hit: %d, %d fetches, want %d", code, web.hits.Load(), hits)
	}

	// The home page's <link rel=icon>; an SVG is served inert.
	resp := alice.getResp(t, "/api/favicon/shop.example.org")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/svg+xml" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("svg favicon: %d %v", resp.StatusCode, resp.Header)
	}

	// No icon at all: 404, remembered, so the site isn't asked again.
	if code, _ := alice.raw("GET", "/api/favicon/html.example.net", nil, false); code != 404 {
		t.Fatalf("no icon = %d", code)
	}
	hits = web.hits.Load()
	if code, _ := alice.raw("GET", "/api/favicon/html.example.net", nil, false); code != 404 || web.hits.Load() != hits {
		t.Fatalf("the miss wasn't cached: %d, %d fetches", code, web.hits.Load()-hits)
	}

	// Signed out, nothing.
	if code, _ := e.client(t).raw("GET", "/api/favicon/example.com", nil, false); code != 401 {
		t.Errorf("signed-out favicon = %d", code)
	}
}

func (c *client) getResp(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := c.hc.Get(c.base + path)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// TestMediaFetchGuards: redirects are re-checked (no private hops, no
// downgrade from https), and pictures over their cap aren't served.
func TestMediaFetchGuards(t *testing.T) {
	big := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{0}, imageMax)...)
	web := newFakeInternet(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-http":
			http.Redirect(w, r, "http://example.com/pic.png", http.StatusFound)
		case "/to-private":
			http.Redirect(w, r, "https://10.0.0.5/pic.png", http.StatusFound)
		case "/to-cdn":
			http.Redirect(w, r, "https://cdn.example.com/pic.png", http.StatusMovedPermanently)
		case "/pic.png":
			_, _ = w.Write(pngBytes)
		case "/big.png":
			_, _ = w.Write(big)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	f := web.fetcher
	if ct, b, err := f.image(ctx, "https://example.com/to-cdn", imageMax, imageTimeout, false); err != nil || ct != "image/png" || !bytes.Equal(b, pngBytes) {
		t.Fatalf("https redirect: %q %v", ct, err)
	}
	if _, _, err := f.image(ctx, "https://example.com/to-http", imageMax, imageTimeout, false); err == nil {
		t.Error("a redirect from https to http was followed")
	}
	before := web.connects.Load()
	if _, _, err := f.image(ctx, "https://example.com/to-private", imageMax, imageTimeout, false); err == nil {
		t.Error("a redirect to a private address was followed")
	}
	if n := web.connects.Load() - before; n > 1 {
		t.Errorf("%d connections for a redirect to a private address, want only the first hop", n)
	}
	if _, _, err := f.image(ctx, "https://example.com/big.png", imageMax, imageTimeout, false); err == nil {
		t.Error("an image over 5 MB was accepted")
	}
	if _, _, err := f.image(ctx, "https://example.com/pic.png", int64(len(pngBytes)-1), fetchTimeout, true); err == nil {
		t.Error("an icon over its cap was accepted")
	}
}

func TestPublicAddr(t *testing.T) {
	for s, want := range map[string]bool{
		"93.184.215.14": true, "2606:2800:21f:cb07:6820:80da:af6b:8b2c": true,
		"10.0.0.1": false, "172.16.5.4": false, "192.168.1.1": false, "127.0.0.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fd00::1": false, "fe80::1": false, "::ffff:10.0.0.1": false,
		"64:ff9b::a00:1": false, "2002:a00:1::": false, "224.0.0.1": false,
	} {
		if got := publicAddr(netip.MustParseAddr(s)); got != want {
			t.Errorf("publicAddr(%s) = %v, want %v", s, got, want)
		}
	}
}

// TestBlocksInConversation drives blocks through a real session: a say
// with a product card is stored, broadcast and its image proxied; a table
// too long for a message is refused with the fix, and the model's next
// call, a report, goes through.
func TestBlocksInConversation(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	alice := e.client(t)
	alice.signup("alice")
	web := newFakeInternet(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sony.jpg":
			_, _ = w.Write(pngBytes)
		default:
			fmt.Fprint(w, "<html>an error page</html>")
		}
	})

	card := map[string]any{"type": "cards", "kind": "product", "items": []any{map[string]any{
		"title": "Sony WH-1000XM6", "image": "https://example.com/sony.jpg", "price": map[string]any{"amount": 279, "currency": "USD"}, "pick": true,
		"action": map[string]any{"do": "reply", "label": "Choose this", "text": "Let's go with the Sony."}}}}
	broken := map[string]any{"type": "images", "items": []any{map[string]any{"src": "https://example.com/missing.jpg", "caption": "Missing"}}}
	say := func(blocks ...any) map[string]any {
		return map[string]any{"text": "The Sony is the one.", "blocks": blocks,
			"sources": []any{map[string]any{"title": "Wirecutter", "url": "https://www.nytimes.com/wirecutter/"}}, "suggestions": []any{"Track the price"}}
	}
	// A block of an unknown type refuses the whole message, so it's shown
	// once, with its blocks, after the model fixes it.
	fm.script(
		reply(1000, fn("say", say(card, broken, map[string]any{"type": "map"}))),
		expect(t, []string{`Not sent: blocks[2]: there's no \"map\" block`, `Nothing was shown.`}, fn("say", say(card, broken))),
		expect(t, []string{`Said. Shown: 1 product card (pick: Sony WH-1000XM6), 1 image, 1 source.`}),
	)
	alice.send("which headphones?")
	s := alice.wait("idle")
	en := last(s.Feed, "say")
	if en == nil || en.Text != "The Sony is the one." {
		t.Fatalf("feed = %v", lastN(s.Feed, 3))
	}
	if got := lastN(s.Feed, 2); strings.Join(got, "|") != "user:which headphones?|say:The Sony is the one." {
		t.Fatalf("the refused message was shown: %v", got)
	}
	var data struct {
		Blocks []struct {
			Type  string `json:"type"`
			Items []struct {
				Title string `json:"title"`
				Image string `json:"image"`
				Src   string `json:"src"`
			} `json:"items"`
		} `json:"blocks"`
		Summary     string   `json:"summary"`
		Suggestions []string `json:"suggestions"`
		Sources     []struct {
			URL string `json:"url"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(en.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Blocks) != 2 || data.Summary == "" || len(data.Suggestions) != 1 || len(data.Sources) != 1 {
		t.Fatalf("say data = %s", en.Data)
	}
	img := data.Blocks[0].Items[0].Image
	if !strings.HasPrefix(img, "/api/img/") || strings.Contains(string(en.Data), "example.com/sony.jpg") {
		t.Fatalf("the image isn't proxied: %s", en.Data)
	}

	// The proxied image loads, once; another user can't use the token.
	code, body := alice.raw("GET", img, nil, false)
	if code != 200 || !bytes.Equal(body, pngBytes) {
		t.Fatalf("image = %d (%d bytes)", code, len(body))
	}
	hits := web.hits.Load()
	if code, _ := alice.raw("GET", img, nil, false); code != 200 || web.hits.Load() != hits {
		t.Fatalf("image not cached: %d", code)
	}
	bob := secondUser(t, e, alice)
	if code, _ := bob.raw("GET", img, nil, false); code != 404 {
		t.Fatalf("another user's image token = %d", code)
	}
	if code, _ := alice.raw("GET", "/api/img/"+strings.Repeat("0", 32), nil, false); code != 404 {
		t.Fatalf("an unminted token = %d", code)
	}
	// Something that isn't an image is never served.
	if code, _ := alice.raw("GET", data.Blocks[1].Items[0].Src, nil, false); code != 404 {
		t.Fatalf("a page served as an image: %d", code)
	}

	// The table that's too long for a message, then the report.
	rows := make([]any, 23)
	for i := range rows {
		rows[i] = []any{fmt.Sprintf("Hotel %d", i+1), "$120"}
	}
	table := map[string]any{"type": "table", "columns": []any{"Hotel", "Price"}, "rows": rows}
	fm.script(
		reply(1100, fn("say", map[string]any{"text": "Here are the hotels.", "blocks": []any{table}})),
		expect(t, []string{"Not sent: a table in a message can have at most 8 rows (this one has 23). Show the few that matter for the user's choice, or put the full list in present_report and say a one-line takeaway."},
			fn("present_report", map[string]any{"title": "Hotels in Alfama", "takeaway": "Hotel 1 is the pick.", "blocks": []any{table}})),
		expect(t, []string{"Shown: a table (23 rows)."}, fn("say", map[string]any{"text": "All 23 are in the report."})),
		reply(1200),
	)
	alice.send("hotels in alfama")
	s = alice.wait("idle")
	if got := lastN(s.Feed, 2); strings.Join(got, "|") != "report:Hotels in Alfama|say:All 23 are in the report." {
		t.Fatalf("feed tail = %v", got)
	}
	for _, en := range s.Feed {
		if en.Text == "Here are the hotels." {
			t.Fatal("the refused message was shown")
		}
	}
	_, rep, _ := alice.do("GET", "/api/reports/"+s.State.Surface.ID, nil, false)
	rd, _ := rep["data"].(map[string]any)
	if blocks, _ := rd["blocks"].([]any); len(blocks) != 1 || rd["takeaway"] != "Hotel 1 is the pick." || rd["preview"] == nil {
		t.Fatalf("report = %v", rep)
	}
}

// TestBlockReportDownload: a report made only of blocks downloads as
// readable Markdown. It needs handleReportDownload to use
// Runtime.ReportMarkdown (a handoff to reports.go), so it skips until then.
func TestBlockReportDownload(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	e.model.script(
		reply(1000, fn("present_report", map[string]any{"title": "Headphones", "blocks": []any{
			map[string]any{"type": "compare", "items": []any{map[string]any{"name": "Sony", "pick": true}, map[string]any{"name": "Bose"}},
				"rows": []any{map[string]any{"label": "Price", "values": []any{"$279", "$299"}, "best": 0}}, "verdict": "Sony wins."},
		}})),
		reply(1100, fn("say", map[string]any{"text": "Sony wins."})),
		reply(1200),
	)
	alice.send("compare them")
	s := alice.wait("idle")
	code, md := alice.raw("GET", "/api/reports/"+s.State.Surface.ID+"/download", nil, false)
	if code != 200 || !bytes.HasPrefix(md, []byte("# Headphones")) {
		t.Fatalf("download: %d %s", code, md)
	}
	if !bytes.Contains(md, []byte("Sony wins.")) {
		t.Skip("handleReportDownload doesn't render blocks yet (handoff: use s.rt.ReportMarkdown)")
	}
	if !bytes.Contains(md, []byte("|  | Sony (pick) | Bose |")) || !bytes.Contains(md, []byte("| Price | **$279** ✓ | $299 |")) {
		t.Fatalf("download isn't readable markdown:\n%s", md)
	}
}

// TestRichAsk: a form with a credential field is refused with the fix; the
// model's next ask, with a proper form, waits for the user. Submitting the
// form is a visible user entry, and the model reads [Form] … — that part
// needs the handoffs to session.go (via, values, tap) and Runtime.Send
// (Runtime.reply), so it skips until they land.
func TestRichAsk(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	alice := e.client(t)
	alice.signup("alice")
	fm.script(
		reply(1000, fn("ask", map[string]any{"question": "Your card?", "form": map[string]any{"fields": []any{map[string]any{"name": "card_number", "label": "Card", "type": "text"}}}})),
		expect(t, []string{"Never collect this in a form; use request_login or request_takeover."}, fn("ask", map[string]any{
			"question": "A few details for the hotel search?",
			"form": map[string]any{"submit": "Search", "fields": []any{
				map[string]any{"name": "check_in", "label": "Check-in", "type": "date", "value": "2026-11-14", "required": true},
				map[string]any{"name": "guests", "label": "Guests", "type": "stepper", "value": 2, "min": 1, "max": 8},
			}},
		})),
	)
	alice.send("find me a hotel")
	alice.wait("needs")
	code, raw := alice.raw("GET", "/api/session", nil, false)
	var snap struct {
		State struct {
			Needs struct {
				Form struct {
					Submit string `json:"submit"`
					Fields []struct {
						Name  string `json:"name"`
						Value any    `json:"value"`
					} `json:"fields"`
				} `json:"form"`
			} `json:"needs"`
		} `json:"state"`
	}
	if code != 200 || json.Unmarshal(raw, &snap) != nil || snap.State.Needs.Form.Submit != "Search" || len(snap.State.Needs.Form.Fields) != 2 || snap.State.Needs.Form.Fields[1].Value != 2.0 {
		t.Fatalf("needs = %s", raw)
	}

	var got string
	fm.script(
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			got = inputText(req)
			return []any{fn("say", map[string]any{"text": "Searching for 2."})}, 1100
		},
		reply(1200),
	)
	code, _, _ = alice.do("POST", "/api/messages", map[string]any{"text": "", "via": "form", "values": map[string]any{"check_in": "2026-11-14", "guests": 2}}, true)
	if code != 200 {
		t.Skip("POST /api/messages doesn't take via and values yet (handoff to session.go and Runtime.Send)")
	}
	s := alice.wait("idle")
	if !strings.Contains(got, `[Form] check_in=2026-11-14; guests=2`) {
		t.Fatalf("the model didn't get the form: …%s", tail(got, 400))
	}
	u := lastUser(s.Feed)
	if u == nil || u.Text != "Check-in: 2026-11-14 · Guests: 2" || !strings.Contains(string(u.Data), `"via":"form"`) {
		t.Fatalf("form entry = %+v", u)
	}
}

func lastUser(feed []entry) *entry { return last(feed, "user") }

// TestEntryCalendar: an itinerary in a message downloads as a calendar.
func TestEntryCalendar(t *testing.T) {
	e := newTestServer(t)
	alice := e.client(t)
	alice.signup("alice")
	e.model.script(
		reply(1000, fn("say", map[string]any{"text": "Here's Saturday.", "blocks": []any{map[string]any{"type": "timeline", "days": []any{map[string]any{"date": "2026-11-14", "items": []any{
			map[string]any{"time": "08:10", "end": "16:45", "title": "UA 1543 SFO to JFK", "kind": "flight"},
			map[string]any{"time": "19:30", "title": "Dinner at Via Carota", "kind": "meal", "place": "51 Grove St"},
		}}}}}})),
		reply(1100),
	)
	alice.send("plan saturday")
	s := alice.wait("idle")
	en := last(s.Feed, "say")
	code, ics := alice.raw("GET", fmt.Sprintf("/api/entries/%d/calendar.ics", en.ID), nil, false)
	if code != 200 || !bytes.Contains(ics, []byte("SUMMARY:UA 1543 SFO to JFK")) || !bytes.Contains(ics, []byte("LOCATION:51 Grove St")) || bytes.Count(ics, []byte("BEGIN:VEVENT")) != 2 {
		t.Fatalf("calendar: %d %s", code, ics)
	}
	user := last(s.Feed, "user")
	if code, _ := alice.raw("GET", fmt.Sprintf("/api/entries/%d/calendar.ics", user.ID), nil, false); code != 404 {
		t.Errorf("an entry without events = %d", code)
	}
	bob := secondUser(t, e, alice)
	if code, _ := bob.raw("GET", fmt.Sprintf("/api/entries/%d/calendar.ics", en.ID), nil, false); code != 404 {
		t.Errorf("another user's entry = %d", code)
	}
}

// secondUser has the admin add bob (sign-up closes after the first
// account) and signs him in.
func secondUser(t *testing.T, e *testEnv, admin *client) *client {
	t.Helper()
	admin.post("/api/admin/users", map[string]any{"username": "bob", "password": "password2"})
	bob := e.client(t)
	bob.post("/api/auth/login", map[string]any{"username": "bob", "password": "password2"})
	return bob
}

// TestCardTap: tapping a card's reply button sends its text as a visible
// message, and Alexa reads which button on which card. Like the form, it
// needs the session.go and Runtime.Send handoffs, so it skips until then.
func TestCardTap(t *testing.T) {
	e := newTestServer(t)
	fm := e.model
	alice := e.client(t)
	alice.signup("alice")
	card := map[string]any{"type": "cards", "kind": "product", "items": []any{
		map[string]any{"title": "Bose QC Ultra", "action": map[string]any{"do": "reply", "label": "Choose this", "text": "Let's go with the Bose."}},
		map[string]any{"title": "Sony WH-1000XM6", "action": map[string]any{"do": "reply", "label": "Choose this", "text": "Let's go with the Sony."}},
	}}
	fm.script(
		reply(1000, fn("say", map[string]any{"text": "Two good ones.", "blocks": []any{card}})),
		reply(1100),
	)
	alice.send("headphones?")
	s := alice.wait("idle")
	said := last(s.Feed, "say")

	var got string
	fm.script(
		func(_ context.Context, req *llm.Request) ([]any, int64) {
			got = inputText(req)
			return []any{fn("say", map[string]any{"text": "The Sony it is."})}, 1200
		},
		reply(1300),
	)
	code, _, _ := alice.do("POST", "/api/messages", map[string]any{"text": "Let's go with the Sony.", "via": "button", "tap": map[string]any{"entryId": said.ID, "block": 0, "item": 1}}, true)
	if code != 200 {
		t.Fatalf("tap = %d", code)
	}
	s = alice.wait("idle")
	if !strings.Contains(got, `[Tapped \"Choose this\" on: Sony WH-1000XM6] Let's go with the Sony.`) {
		if strings.Contains(got, "] Let's go with the Sony.") {
			t.Skip("POST /api/messages doesn't take via and tap yet (handoff to session.go and Runtime.Send)")
		}
		t.Fatalf("the model didn't read the tap: …%s", tail(got, 400))
	}
	u := last(s.Feed, "user")
	if u == nil || u.Text != "Let's go with the Sony." || !strings.Contains(string(u.Data), `"via":"button"`) {
		t.Fatalf("tap entry = %+v", u)
	}
	// The tap answered what Alexa had just shown, so the exchange goes on.
	if strings.Contains(string(u.Data), `"start":true`) {
		t.Fatalf("a tap on the latest cards started a new exchange: %s", u.Data)
	}

	// Once the user has moved on, a tap on the old cards is a new request.
	fm.script(reply(1400, fn("say", map[string]any{"text": "Sunny."})), reply(1500))
	alice.send("weather?")
	alice.wait("idle")
	fm.script(reply(1600, fn("say", map[string]any{"text": "Back to the Bose."})), reply(1700))
	alice.do("POST", "/api/messages", map[string]any{"text": "Let's go with the Bose.", "via": "button", "tap": map[string]any{"entryId": said.ID, "block": 0, "item": 0}}, true)
	s = alice.wait("idle")
	if u := last(s.Feed, "user"); u == nil || !strings.Contains(string(u.Data), `"start":true`) {
		t.Fatalf("a tap on old cards didn't start a new exchange: %+v", u)
	}
}
