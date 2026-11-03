package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"sort"
	"testing"

	"alexagent/internal/contract"
)

// ---------- the frontend contract, at the endpoints ----------
//
// What the endpoints and the event socket answer, against the client types
// in web/src/api that read them (the agent package checks the Go types
// themselves): every field the client requires is there, and nothing comes
// back that the client doesn't declare.

// shape checks one JSON object against the client type typ. also names
// fields the server may send beyond the type: the parts of an intersection
// written inline (Attachment & { note?: string }), or fields the client
// knowingly ignores.
func shape(t *testing.T, ts contract.Types, typ string, got map[string]any, also ...string) {
	t.Helper()
	fields := ts[typ]
	if len(fields) == 0 {
		t.Fatalf("web/src/api has no object type %s", typ)
	}
	if got == nil {
		t.Fatalf("%s: no object to check", typ)
	}
	known := map[string]bool{}
	for _, a := range also {
		known[a] = true
	}
	for _, f := range fields {
		known[f.Name] = true
		if _, ok := got[f.Name]; !ok && !f.Optional {
			t.Errorf("%s.%s: the client requires it, but the answer has none: %v", typ, f.Name, keysOf(got))
		}
	}
	for k := range got {
		if !known[k] {
			t.Errorf("%s: the server sends %q, which the client doesn't declare", typ, k)
		}
	}
}

func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func object(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want an object, got %T %v", v, v)
	}
	return m
}

func TestContractEndpoints(t *testing.T) {
	ts, err := contract.Load(contract.APIDir())
	if err != nil {
		t.Fatal(err)
	}
	e := newTestServer(t)
	c := e.client(t)
	_, me, _ := c.do("POST", "/api/auth/signup", map[string]any{"username": "alice", "password": "password1", "timezone": "Europe/Lisbon"}, true)
	shape(t, ts, "User", me)
	_, me, _ = c.do("GET", "/api/me", nil, false)
	shape(t, ts, "User", me)
	_, cfg, _ := c.do("GET", "/api/config", nil, false)
	shape(t, ts, "ServerConfig", cfg)
	if vp := object(t, cfg["viewport"]); vp["width"] == nil || vp["height"] == nil {
		t.Errorf("config viewport = %v", vp)
	}

	ws := c.dial("")
	hello := readEv(t, ws)
	if hello.Type != "hello" || hello.Epoch == "" {
		t.Errorf("hello = %+v", hello)
	}
	var hd map[string]any
	if err := json.Unmarshal(hello.Data, &hd); err != nil {
		t.Fatal(err)
	}
	shape(t, ts, "Hello", hd)

	// A message with its client id, answered with its entry.
	e.model.script(reply(1000, fn("say", map[string]any{"text": "Hello Alice.", "final": true})))
	_, sent, _ := c.do("POST", "/api/messages", map[string]any{"text": "Hi", "clientId": "c-contract-1"}, true)
	shape(t, ts, "SendResult", sent)
	entry := object(t, sent["entry"])
	shape(t, ts, "Entry", entry)
	if d := object(t, entry["data"]); d["clientId"] != "c-contract-1" {
		t.Errorf("the echoed entry's clientId = %v", d["clientId"])
	}
	c.wait("idle")

	_, snap, _ := c.do("GET", "/api/session", nil, false)
	shape(t, ts, "SessionData", snap)
	state := object(t, snap["state"])
	shape(t, ts, "SessionState", state, "callId", "toolName")
	shape(t, ts, "Surface", object(t, state["surface"]))
	if q, ok := state["queued"].(float64); !ok || q != 0 {
		t.Errorf("state.queued = %v, want 0 (sent even at zero)", state["queued"])
	}
	feed, _ := snap["feed"].([]any)
	if len(feed) < 2 {
		t.Fatalf("feed = %v", feed)
	}
	for _, x := range feed {
		shape(t, ts, "Entry", object(t, x))
	}
	last := object(t, feed[len(feed)-1])
	_, page, _ := c.do("GET", "/api/feed?before="+jsonNum(last["id"]), nil, false)
	shape(t, ts, "FeedPage", page)

	// An upload answers with the attachment and, maybe, a note.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "notes.txt")
	_, _ = fw.Write([]byte("Check in at 3pm."))
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.base+"/api/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Alexa", "1")
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var up map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	shape(t, ts, "Attachment", up, "note")

	_, _, files := c.do("GET", "/api/files", nil, false)
	if len(files) == 0 {
		t.Fatal("no files after an upload")
	}
	for _, f := range files {
		shape(t, ts, "FileInfo", object(t, f))
	}

	// Sheets: what they list, and the changed event that refetches them.
	_, mem, _ := c.do("POST", "/api/memories", map[string]any{"content": "Prefers quiet hotels"}, true)
	shape(t, ts, "Memory", mem, "source", "updatedAt") // the client doesn't show them yet
	// The upload said files changed; this one says memory did.
	var ch map[string]any
	ev := readUntil(t, ws, func(ev wsEvent) bool {
		ch = nil
		_ = json.Unmarshal(ev.Data, &ch)
		return ev.Type == "changed" && ch["what"] == "memory"
	})
	shape(t, ts, "Changed", ch)
	if ev.Seq == 0 || ev.Epoch == "" {
		t.Errorf("changed event without seq or epoch: %+v", ev)
	}
	_, sch, _ := c.do("POST", "/api/schedules", map[string]any{"name": "Prices", "prompt": "Check hotel prices", "cron": "0 9 * * 1"}, true)
	shape(t, ts, "Schedule", sch, "userId")
	_, _, notes := c.do("GET", "/api/notifications", nil, false)
	for _, n := range notes {
		shape(t, ts, "AppNotification", object(t, n))
	}

	// Repair with nothing to set aside.
	if code, rep, _ := c.do("POST", "/api/repair", nil, true); code != 200 || rep["ok"] != true {
		t.Errorf("repair = %d %v", code, rep)
	}
	// Try again with nothing that failed: refused, in words the card shows.
	if code, rep, _ := c.do("POST", "/api/retry", nil, true); code != 409 || rep["error"] != "There’s nothing to try again." {
		t.Errorf("retry = %d %v", code, rep)
	}
	// Media the client builds URLs for: a host that isn't public is refused
	// before anything is fetched, and an image token not made for this user
	// is not found.
	if code, _, _ := c.do("GET", "/api/favicon/localhost", nil, false); code != 400 {
		t.Errorf("favicon for localhost = %d, want 400", code)
	}
	if code, _, _ := c.do("GET", "/api/img/0123456789abcdef0123456789abcdef", nil, false); code != 404 {
		t.Errorf("unknown image token = %d, want 404", code)
	}
}

// jsonNum writes a JSON number (decoded as float64) as an integer.
func jsonNum(v any) string {
	b, _ := json.Marshal(int64(v.(float64)))
	return string(b)
}
