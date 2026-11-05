package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"alexagent/internal/browser"
	"alexagent/internal/contract"
)

// ---------- the frontend contract, on the sockets ----------
//
// The parts of the wire the client declares outside web/src/api: the live
// browser socket's messages (panels/browser/source.ts) and the close codes
// both sockets end with. A field or a code changed on one side fails here
// instead of a drag that never holds its button or a client that retries a
// revoked session forever.

// webSource reads a file under web/src.
func webSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(contract.APIDir(), "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tags lists the JSON names a struct marshals and unmarshals under.
func tags(v any) []string {
	var out []string
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Take-over input: every field the client sends is one the server reads,
// and the other way round (buttons is what makes a drag hold its button).
func TestContractHumanInput(t *testing.T) {
	ts := contract.Parse(webSource(t, "panels/browser/source.ts"))
	client := ts.Names("HumanInput")
	if len(client) == 0 {
		t.Fatal("panels/browser/source.ts has no HumanInput type")
	}
	if server := tags(browser.HumanInput{}); !reflect.DeepEqual(client, server) {
		t.Errorf("take-over input fields differ:\n client %v\n server %v", client, server)
	}
}

// The live view's hello carries what the client's LiveHello reads, and
// every browser event the hub relays is a BrowserEvent.
func TestContractLiveMessages(t *testing.T) {
	ts := contract.Parse(webSource(t, "panels/browser/source.ts"))
	var hello map[string]any
	if err := json.Unmarshal(liveHello(1280, 800, "https://example.com/", "Example", 10, 20), &hello); err != nil {
		t.Fatal(err)
	}
	shape(t, ts, "LiveHello", hello)
	if hello["type"] != "hello" {
		t.Errorf("hello type = %v", hello["type"])
	}

	api, err := contract.Load(contract.APIDir())
	if err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	b, _ := json.Marshal(browser.TabEvent{Type: "cursor", URL: "https://example.com/", Title: "Example", X: 1, Y: 2, Click: true})
	if err := json.Unmarshal(b, &ev); err != nil {
		t.Fatal(err)
	}
	shape(t, api, "BrowserEvent", ev)
}

// What the client sends on the event socket is what the server reads.
func TestContractClientMessages(t *testing.T) {
	members := contract.Members(webSource(t, "api/realtime.ts"), "ClientMessage", "type")
	if len(members) != 2 {
		t.Fatalf("ClientMessage members = %v, want attention and visibility", members)
	}
	server := map[string]bool{}
	for _, n := range tags(clientMsg{}) {
		server[n] = true
	}
	for typ, fields := range members {
		for _, f := range fields {
			if !server[f.Name] {
				t.Errorf("ClientMessage<%s>.%s: the client sends it, but the server doesn't read it", typ, f.Name)
			}
		}
	}
}

// The close codes the client acts on are the ones the server closes with.
func TestContractCloseCodes(t *testing.T) {
	codes := map[string]websocket.StatusCode{}
	re := regexp.MustCompile(`export const (close[A-Za-z]+) = (\d+)`)
	for _, f := range []string{"api/realtime.ts", "panels/browser/status.ts"} {
		for _, m := range re.FindAllStringSubmatch(webSource(t, f), -1) {
			n, _ := strconv.Atoi(m[2])
			codes[m[1]] = websocket.StatusCode(n)
		}
	}
	want := map[string]websocket.StatusCode{
		"closeRevoked":   closeRevoked,
		"closeLagged":    closeLagged,
		"closeRestarted": websocket.StatusServiceRestart, // the live view's browser restarted
	}
	for name, code := range want {
		if got, ok := codes[name]; !ok {
			t.Errorf("the client has no %s (want %d)", name, code)
		} else if got != code {
			t.Errorf("%s: the client has %d, the server closes with %d", name, got, code)
		}
	}
}
