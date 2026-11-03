package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"alexagent/internal/contract"
	"alexagent/internal/hub"
	"alexagent/internal/store"
)

// ---------- the frontend contract (web/src/api/*.ts) ----------
//
// These tests read the client's API types and check that the server sends
// every field the client reads, under the same name, all the way down: a
// field renamed on either side fails here instead of rendering as nothing.

// contractExempt names the client fields the server deliberately doesn't
// send where the client looks for them, and why. An exemption the server
// has caught up with fails the test, so the list stays true.
var contractExempt = map[string]string{
	"SessionState.seq":   "sent on the snapshot (SessionData) and on every event's envelope, not in the view",
	"SessionState.epoch": "sent on the snapshot (SessionData) and on every event's envelope, not in the view",
	"RunEvent.type":      "the envelope's: a run event's data travels as {type:\"run\", seq, epoch, data}",
	"RunEvent.seq":       "the envelope's: a run event's data travels as {type:\"run\", seq, epoch, data}",
	"Entry.rev":          "deferred: entries have no revision column yet; the reducer falls back to updatedAt",
}

type contractCheck struct {
	t    *testing.T
	ts   contract.Types
	seen map[string]bool
	used map[string]bool
}

func loadContract(t *testing.T) *contractCheck {
	t.Helper()
	ts, err := contract.Load(contract.APIDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatalf("no types read from %s", contract.APIDir())
	}
	return &contractCheck{t: t, ts: ts, seen: map[string]bool{}, used: map[string]bool{}}
}

var (
	rawJSONType   = reflect.TypeOf(json.RawMessage(nil))
	marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
)

// jsonFields maps the JSON names a Go struct marshals under to their
// types, with embedded structs' fields promoted as encoding/json does.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			for k, v := range jsonFields(f.Type) {
				if _, ok := out[k]; !ok {
					out[k] = v
				}
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
}

// elemOf is what a field holds once pointers, slices and arrays are looked
// through: Card for []Card.
func elemOf(t reflect.Type) reflect.Type {
	for t != rawJSONType && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		t = t.Elem()
	}
	return t
}

// object resolves a TypeScript field type to the fields of the object it
// holds: "Card[]", "{ x: number }[]", "Money & { was?: number }", "Plan |
// null". It reports false for anything else (strings, unions of objects,
// records), which the check doesn't look into.
func (c *contractCheck) object(expr string) ([]contract.Field, bool) {
	expr = strings.TrimSpace(expr)
	for strings.HasSuffix(expr, "[]") {
		expr = strings.TrimSpace(strings.TrimSuffix(expr, "[]"))
	}
	if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		expr = expr[1 : len(expr)-1]
	}
	parts := splitTop(expr, '|')
	var kept []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "null" && p != "undefined" {
			kept = append(kept, p)
		}
	}
	if len(kept) != 1 {
		return nil, false
	}
	var out []contract.Field
	for _, p := range splitTop(kept[0], '&') {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}"):
			out = append(out, contract.Fields(p[1:len(p)-1])...)
		case c.ts[p] != nil:
			out = append(out, c.ts[p]...)
		default:
			return nil, false
		}
	}
	return out, len(out) > 0
}

// splitTop splits s at sep where it isn't nested in brackets or a string.
func splitTop(s string, sep byte) []string {
	var out []string
	depth, from := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '(', '[', '<':
			depth++
		case '}', ')', ']', '>':
			depth--
		case '\'':
			for i++; i < len(s) && s[i] != '\''; i++ {
			}
		case sep:
			if depth == 0 {
				out = append(out, s[from:i])
				from = i + 1
			}
		}
	}
	return append(out, s[from:])
}

// check compares the client's fields at path with what Go type g sends,
// and goes on into every field that holds an object on both sides.
func (c *contractCheck) check(path string, fields []contract.Field, g reflect.Type) {
	c.t.Helper()
	gf := jsonFields(g)
	if len(gf) == 0 {
		c.t.Errorf("%s: %s has no JSON fields to check against", path, g)
		return
	}
	for _, f := range fields {
		p := path + "." + f.Name
		gt, ok := gf[f.Name]
		if why, exempt := contractExempt[p]; exempt {
			c.used[p] = true
			if ok {
				c.t.Errorf("%s is exempt (%s), but %s sends it now: drop the exemption", p, why, g)
			}
			continue
		}
		if !ok {
			hint := ""
			for k := range gf {
				if strings.EqualFold(k, f.Name) {
					hint = " (it has " + k + ")"
				}
			}
			c.t.Errorf("%s: the client reads it, but %s sends no such field%s", p, g, hint)
			continue
		}
		sub, ok := c.object(f.Type)
		if !ok {
			continue
		}
		ge := elemOf(gt)
		if ge.Kind() != reflect.Struct || ge.Implements(marshalerType) || reflect.PointerTo(ge).Implements(marshalerType) {
			c.t.Logf("%s: not checked deeper, the server sends %s", p, gt)
			continue
		}
		key := f.Type + "→" + ge.String()
		if c.seen[key] {
			continue
		}
		c.seen[key] = true
		c.check(p, sub, ge)
	}
	var extra []string
	for k := range gf {
		found := false
		for _, f := range fields {
			found = found || f.Name == k
		}
		if !found {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		c.t.Logf("%s: the server also sends %v, which the client doesn't declare", path, extra)
	}
}

func (c *contractCheck) root(tsName string, g any) {
	c.t.Helper()
	fields := c.ts[tsName]
	if len(fields) == 0 {
		c.t.Errorf("web/src/api has no object type %s", tsName)
		return
	}
	c.check(tsName, fields, reflect.TypeOf(g))
}

func (c *contractCheck) source(file string) string {
	c.t.Helper()
	b, err := os.ReadFile(filepath.Join(contract.APIDir(), file))
	if err != nil {
		c.t.Fatal(err)
	}
	return string(b)
}

// Every object the server sends, against the client type that reads it.
func TestContractTypes(t *testing.T) {
	c := loadContract(t)
	c.root("SessionState", View{}) // with Needs (form, fields), Surface and Plan
	c.root("Entry", store.Entry{})
	c.root("Report", store.Report{})
	c.root("ReportData", ReportData{}) // with ReportPreview and its stat
	c.root("User", store.User{})
	c.root("Memory", store.Memory{})
	c.root("Schedule", store.Schedule{})
	c.root("AppNotification", store.Notification{})
	c.root("Login", store.Login{})
	c.root("Attachment", Attachment{})
	c.root("StepData", StepData{})
	c.root("Failure", Failure{})
	c.root("RunEvent", runEvent{}) // with RunTool and RunRetry
	c.root("Changed", hub.Change{})
	c.root("Tap", Tap{})
	c.root("Card", Card{}) // with Money, Rating, Fact, segments

	// Every block, and the fields they share.
	blocks := contract.Members(c.source("blocks.ts"), "Block", "type")
	var names []string
	for typ, fields := range blocks {
		names = append(names, typ)
		b := newBlock(typ)
		if b == nil {
			t.Errorf("the client renders a %q block the server doesn't have", typ)
			continue
		}
		c.check("Block<"+typ+">", append(append([]contract.Field{}, c.ts["Base"]...), fields...), reflect.TypeOf(b).Elem())
	}
	sort.Strings(names)
	want := append([]string{}, blockTypes...)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Errorf("block catalogs differ: client %v, server %v", names, want)
	}
	// A card's action, one member per do.
	acts := contract.Members(c.source("blocks.ts"), "Action", "do")
	if len(acts) != 3 {
		t.Errorf("Action members = %v, want reply, open and report", acts)
	}
	for do, fields := range acts {
		c.check("Action<"+do+">", fields, reflect.TypeOf(Action{}))
	}

	for p, why := range contractExempt {
		if !c.used[p] {
			t.Errorf("exemption %s (%s) matches no client field: fix or drop it", p, why)
		}
	}
}

// The data the server keeps on each kind of entry, from a real exchange:
// every key the client's EntryData declares turns up on some entry, and no
// entry carries a key the client doesn't declare.
func TestContractEntryData(t *testing.T) {
	c := loadContract(t)
	e := newProgressEnv(t)
	e.fakeTool("web_search", func(map[string]any) (*toolResult, error) {
		return &toolResult{Text: "1. Alfama Stay — https://alfama.example/"}, nil
	})
	cards := []any{map[string]any{"type": "cards", "kind": "stay", "items": []any{
		map[string]any{"title": "Casa Alfama", "url": "https://alfama.example/", "action": map[string]any{"do": "reply", "label": "Book", "text": "Book Casa Alfama"}},
		map[string]any{"title": "Lisbon Loft", "url": "https://loft.example/"},
	}}}
	stats := []any{map[string]any{"type": "stats", "items": []any{map[string]any{"label": "Best rate", "value": "€128"}}}}
	src := []any{map[string]any{"title": "Alfama Stay", "url": "https://alfama.example/"}}
	e.model.script(
		does(fc("plan", planArgs("Hotels in Lisbon", "done:Search", "done:Compare")), fc("web_search", map[string]any{"query": "hotels alfama"})),
		does(fc("say", map[string]any{"text": "Two good ones.", "blocks": cards, "sources": src, "suggestions": []any{"Cheaper ones"}})),
		does(report("Lisbon hotels", map[string]any{"final": false, "takeaway": "Casa Alfama is the pick.", "blocks": stats})),
		does(fc("ask", map[string]any{"question": "Book Casa Alfama?", "options": []any{"Book it", "Not yet"}, "recommended": 0, "confirm": true,
			"blocks": []any{map[string]any{"type": "receipt", "status": "draft", "lines": []any{map[string]any{"label": "2 nights", "value": "€256"}}, "total": map[string]any{"amount": 256, "currency": "EUR"}}}})),
	)
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Find me a hotel in Lisbon", Voice: true, ClientID: "c-first"}); err != nil {
		t.Fatal(err)
	}
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q after the confirm ask", v.Status)
	}
	// A form, answered by the form.
	e.model.script(does(fc("ask", map[string]any{"question": "How many guests?", "form": map[string]any{"submit": "Go", "fields": []any{
		map[string]any{"name": "guests", "label": "Guests", "type": "stepper", "value": 2, "min": 1, "max": 6},
	}}})))
	e.send("Not yet")
	e.settle()
	e.model.script(does(fc("say", map[string]any{"text": "Noted, 3 guests.", "blocks": cards, "final": true})))
	if err := e.rt.Send(e.ctx, e.user, Message{Via: "form", Values: map[string]any{"guests": 3}}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	// A tapped card, then a multi-select.
	var said *store.Entry
	for _, x := range entriesOf(e.feed(), "say") {
		said = x
	}
	e.model.script(does(fc("ask", map[string]any{"question": "Which extras?", "options": []any{"Breakfast", "Parking", "Late checkout"}, "multi": true})))
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Book Casa Alfama", Via: "button", Tap: &Tap{EntryID: said.ID, Block: 0, Item: 0}}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	// A sign-in, then a failed turn and Try again.
	e.model.script(does(fc("request_login", map[string]any{"reason": "Sign in to book", "site": "https://alfama.example/"})))
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Breakfast, Parking", Via: "multi"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	e.model.script(does(sse{"type": "error", "code": "invalid_prompt", "message": "The model couldn’t read that."}))
	e.send("Skip the sign-in")
	e.settle()
	e.model.script(does(fc("say", map[string]any{"text": "Booked.", "final": true})))
	if err := e.rt.Retry(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	e.settle()
	// From the report, then a scheduled job.
	e.model.script(does(fc("say", map[string]any{"text": "Sure.", "final": true})), does(fc("say", map[string]any{"text": "Prices checked.", "final": true})))
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Compare these two", Via: "report"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Check hotel prices", Source: "schedule", Label: "Hotel prices"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	// A follow-up, read while the topic call is out and once it's back.
	release := make(chan struct{})
	e.judgeTopics(func(string) (string, error) { <-release; return "same", nil })
	e.model.script(does(fc("say", map[string]any{"text": "Done.", "final": true})))
	e.send("And for next weekend?")
	e.settle()
	entries := e.feed()
	close(release)
	e.lastUser()
	entries = append(entries, e.feed()...)

	got := map[string][]string{} // key → the kinds of entry that carry it
	for _, x := range entries {
		if len(x.Data) == 0 {
			continue
		}
		var d map[string]json.RawMessage
		if err := json.Unmarshal(x.Data, &d); err != nil {
			t.Fatalf("%s entry %d has data %s: %v", x.Kind, x.ID, x.Data, err)
		}
		for k := range d {
			got[k] = append(got[k], x.Kind)
		}
	}
	declared := map[string]bool{}
	for _, f := range c.ts["EntryData"] {
		declared[f.Name] = true
		if len(got[f.Name]) == 0 {
			t.Errorf("EntryData.%s: the client reads it, but no entry of the exchange carries it", f.Name)
		}
	}
	for k, kinds := range got {
		if !declared[k] {
			t.Errorf("%s entries carry %q, which the client's EntryData doesn't declare", kinds[0], k)
		}
	}
	// The nested ones, as the entries carry them.
	c.check("EntryData", c.ts["EntryData"], reflect.TypeOf(entryDataShape{}))
}

// entryDataShape is the union of what entries carry where it nests: the
// typed parts the client reads below EntryData.
type entryDataShape struct {
	Attachments []Attachment      `json:"attachments"`
	Voice       bool              `json:"voice"`
	Options     []string          `json:"options"`
	ID          string            `json:"id"`
	Updated     bool              `json:"updated"`
	Start       bool              `json:"start"`
	FollowUp    bool              `json:"followUp"`
	Topic       string            `json:"topic"`
	Scheduled   bool              `json:"scheduled"`
	Site        string            `json:"site"`
	Username    string            `json:"username"`
	Blocks      []json.RawMessage `json:"blocks"`
	Sources     []Source          `json:"sources"`
	Suggestions []string          `json:"suggestions"`
	Final       bool              `json:"final"`
	Preview     *ReportPreview    `json:"preview"`
	Takeaway    string            `json:"takeaway"`
	Summary     string            `json:"summary"`
	Form        *AskForm          `json:"form"`
	Multi       bool              `json:"multi"`
	Recommended int               `json:"recommended"`
	Confirm     bool              `json:"confirm"`
	Via         string            `json:"via"`
	ClientID    string            `json:"clientId"`
	Tap         *Tap              `json:"tap"`
	Values      map[string]any    `json:"values"`
	Retry       int64             `json:"retry"`
	Plan        *Plan             `json:"plan"`
	Step        *StepData         `json:"step"`
	Failure     *Failure          `json:"failure"`
}
