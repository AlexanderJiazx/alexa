package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"alexagent/internal/config"
	"alexagent/internal/hub"
	"alexagent/internal/notify"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
)

// The fixtures of Appendix A.6, as the model would write them.
var blockFixtures = []string{
	`{"type":"cards","kind":"product","items":[{"title":"Sony WH-1000XM6","price":{"amount":279,"currency":"USD","was":329},"rating":{"value":4.7,"count":3120},"badges":["Best noise cancelling"],"facts":[{"label":"Battery","value":"30 h"}],"note":"Quietest on planes","pick":true,"action":{"do":"reply","label":"Choose this","text":"Let's go with the Sony WH-1000XM6 ($279)."}}]}`,
	`{"type":"compare","title":"Top 3","items":[{"name":"Sony XM6","pick":true},{"name":"Bose QC Ultra"},{"name":"AirPods Max"}],"rows":[{"label":"Price","values":["$279","$299","$449"],"best":0},{"label":"Battery","values":["30 h","24 h","20 h"],"best":0}],"verdict":"Sony wins on price and battery."}`,
	`{"type":"stats","items":[{"label":"Lowest fare","value":"$412","delta":"-$38","trend":"down","good":"down"}]}`,
	`{"type":"receipt","status":"draft","merchant":"Best Buy","lines":[{"label":"Sony WH-1000XM6","value":"$279.00"},{"label":"Tax","value":"$24.76"}],"total":{"amount":303.76,"currency":"USD"}}`,
	`{"type":"timeline","days":[{"date":"2026-11-14","items":[{"time":"08:10","title":"UA 1543 SFO to JFK","kind":"flight"},{"time":"19:30","title":"Dinner at Via Carota","kind":"meal","place":"51 Grove St"}]}]}`,
}

// testEnv is a blockEnv with a workspace in dir and a fake image proxy.
func testBlockEnv(t *testing.T) (blockEnv, *userenv.Env) {
	t.Helper()
	env, err := userenv.New(t.TempDir(), false, 0).Ensure(1)
	if err != nil {
		t.Fatal(err)
	}
	return blockEnv{
		loc:    time.UTC,
		file:   func(p string) (string, bool) { return workspaceFile(env, p) },
		image:  func(src string) (string, bool) { return "/api/img/" + strings.Repeat("a", 32), true },
		report: func(id string) bool { return id == "r1" },
	}, env
}

func list(blocks ...string) json.RawMessage {
	return json.RawMessage("[" + strings.Join(blocks, ",") + "]")
}

func mustNormalize(t *testing.T, raw json.RawMessage, place Placement, env blockEnv) ([]json.RawMessage, []string) {
	t.Helper()
	out, notes, err := normalizeBlocks(raw, place, env)
	if err != nil {
		t.Fatalf("normalizeBlocks: %v", err)
	}
	return out, notes
}

func TestBlockFixtures(t *testing.T) {
	env, _ := testBlockEnv(t)
	for _, f := range blockFixtures {
		for _, place := range []Placement{PlaceInline, PlaceReport} {
			out, notes := mustNormalize(t, list(f), place, env)
			if len(out) != 1 || len(notes) != 0 {
				t.Fatalf("%s at %s: %d blocks, notes %v", f[:30], place, len(out), notes)
			}
			// Normalizing is stable: a stored block passes through unchanged.
			again, _ := mustNormalize(t, list(string(out[0])), place, env)
			if string(again[0]) != string(out[0]) {
				t.Errorf("not idempotent:\n%s\n%s", out[0], again[0])
			}
			if s := summarizeBlocks(out); s == "" || utf8.RuneCountInString(s) > summaryMax {
				t.Errorf("summary of %s is %d characters: %q", f[:30], utf8.RuneCountInString(s), s)
			}
			if md := blocksToMarkdown(out); strings.TrimSpace(md) == "" {
				t.Errorf("no markdown for %s", f[:30])
			}
		}
	}
	// All five together still summarize within the limit.
	all, _ := mustNormalize(t, list(blockFixtures...), PlaceReport, env)
	if s := summarizeBlocks(all); utf8.RuneCountInString(s) > summaryMax {
		t.Errorf("summary of all fixtures is %d characters", utf8.RuneCountInString(s))
	}
	// The card keeps every field the client renders.
	out, _ := mustNormalize(t, list(blockFixtures[0]), PlaceInline, env)
	var cb cardsBlock
	if err := json.Unmarshal(out[0], &cb); err != nil {
		t.Fatal(err)
	}
	c := cb.Items[0]
	if cb.Type != "cards" || cb.Kind != "product" || c.Price.Amount != 279 || c.Price.Currency != "USD" || c.Price.Was == nil || *c.Price.Was != 329 ||
		c.Rating.Value != 4.7 || !bool(c.Pick) || c.Action.Do != "reply" || c.Action.Label != "Choose this" || len(c.Facts) != 1 {
		t.Errorf("card lost fields: %s", out[0])
	}
	if got := summarizeBlocks(out); got != "Sony WH-1000XM6 $279 (pick)" {
		t.Errorf("card summary = %q", got)
	}
	if got := echoBlocks(out); got != "1 product card (pick: Sony WH-1000XM6)" {
		t.Errorf("echo = %q", got)
	}
}

func TestBlockRefusals(t *testing.T) {
	env, _ := testBlockEnv(t)
	rows := make([]string, 23)
	for i := range rows {
		rows[i] = `["a","b"]`
	}
	table := `{"type":"table","columns":["Hotel","Price"],"rows":[` + strings.Join(rows, ",") + `]}`
	for _, c := range []struct {
		name  string
		raw   json.RawMessage
		place Placement
		want  string
	}{
		{"inline table too long", list(table), PlaceInline, "Not sent: a table in a message can have at most 8 rows (this one has 23). Show the few that matter for the user's choice, or put the full list in present_report and say a one-line takeaway."},
		{"table in a question", list(table), PlaceAsk, "Not asked: a table in a question can have at most 8 rows (this one has 23)."},
		{"too many blocks", list(blockFixtures[2], blockFixtures[2], blockFixtures[2], blockFixtures[2]), PlaceInline, "a message can carry at most 3 blocks (this one has 4)"},
		{"card number", list(`{"type":"receipt","status":"confirmed","lines":[{"label":"Card","value":"4242424242424242"}],"total":{"amount":1,"currency":"USD"}}`), PlaceInline, "blocks[0].lines[0].value has a long run of digits that looks like a card or account number"},
		{"grouped card number", list(`{"type":"receipt","status":"draft","lines":[{"label":"Card","value":"4242 4242 4242 4242"}],"total":{"amount":1,"currency":"USD"}}`), PlaceInline, "looks like a card or account number"},
		{"digits in a card note", list(`{"type":"cards","items":[{"title":"X","note":"Account 1234567890123"}]}`), PlaceReport, "blocks[0].items[0].note has a long run of digits"},
		{"values per item", list(`{"type":"compare","items":[{"name":"A"},{"name":"B"},{"name":"C"}],"rows":[{"label":"Price","values":["$1","$2"]}]}`), PlaceInline, "blocks[0].rows[0].values has 2 values for 3 items. Give one value per item, in the same order as items."},
		{"currency", list(`{"type":"receipt","status":"draft","lines":[{"label":"A","value":"1"}],"total":{"amount":1,"currency":"$"}}`), PlaceInline, "blocks[0].total.currency must be a 3-letter currency code like USD or EUR (got \"$\")"},
		{"date", list(`{"type":"timeline","days":[{"date":"Nov 14","items":[{"title":"Fly"}]}]}`), PlaceInline, "blocks[0].days[0].date must be a date like 2026-11-14"},
		{"markdown inline", list(`{"type":"markdown","text":"## Hi"}`), PlaceInline, "markdown blocks only go in reports"},
		{"missing title", list(`{"type":"cards","items":[{"subtitle":"no title"}]}`), PlaceInline, "blocks[0].items[0].title is missing: every card needs a title."},
		{"empty cards", list(`{"type":"cards","items":[]}`), PlaceInline, "blocks[0].items needs at least one card."},
		{"two receipts", list(blockFixtures[3], blockFixtures[3]), PlaceInline, "a message can show one receipt (this one has 2)"},
		{"checklist status", list(`{"type":"checklist","items":[{"text":"Book","status":"maybe"}]}`), PlaceInline, "must be one of done, failed, todo, skipped"},
		{"wrong shape", list(`{"type":"cards","items":{"title":"x"}}`), PlaceInline, "blocks[0].items has the wrong shape"},
		{"not a list", json.RawMessage(`{"type":"cards"}`), PlaceInline, "blocks must be a list of block objects"},
		// A refused report reads as a failed step ("Not saved:" is one of the
		// prefixes the step outcome check knows).
		{"report too long", list(`{"type":"checklist","items":[` + strings.Repeat(`{"text":"x","status":"todo"},`, 20) + `{"text":"x","status":"todo"}]}`), PlaceReport, "Not saved: a checklist in a report can have at most 20 items (this one has 21)."},
	} {
		_, _, err := normalizeBlocks(c.raw, c.place, env)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	// The same table fits a report.
	if out, _ := mustNormalize(t, list(table), PlaceReport, env); len(out) != 1 {
		t.Fatal("the table should fit in a report")
	}
	// A message's blocks are capped at 16 KB.
	cell := `"` + strings.Repeat("x", 119) + `"`
	row := "[" + strings.Repeat(cell+",", 5) + cell + "]"
	huge := `{"type":"table","columns":["a","b","c","d","e","f"],"rows":[` + strings.Repeat(row+",", 7) + row + `]}`
	if _, _, err := normalizeBlocks(list(huge, huge), PlaceAsk, env); err == nil || !strings.Contains(err.Error(), "Not asked: the blocks are 12 KB, and a question can carry 8 KB.") {
		t.Errorf("oversized blocks: %v", err)
	}
}

func TestBlockNotes(t *testing.T) {
	env, envDir := testBlockEnv(t)
	if err := os.WriteFile(filepath.Join(envDir.Workspace, "room.jpg"), []byte("jpg"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A message or question with an unknown or missing type is refused
	// whole, so it isn't shown once without the block and again with it.
	for _, bad := range []string{`{"type":"map","pins":[]}`, `{"items":[{"title":"Lisbon 5"}]}`} {
		for _, place := range []Placement{PlaceInline, PlaceAsk} {
			if _, _, err := normalizeBlocks(list(bad, blockFixtures[2]), place, env); err == nil || !strings.Contains(err.Error(), "Nothing was shown") {
				t.Fatalf("%s in %v: want a refusal, got %v", bad, place, err)
			}
		}
	}
	// A report drops the block with a note; the rest still shows.
	out, notes := mustNormalize(t, list(`{"type":"map","pins":[]}`, blockFixtures[2]), PlaceReport, env)
	if len(out) != 1 || len(notes) != 1 || !strings.Contains(notes[0], `there's no "map" block`) {
		t.Fatalf("unknown type: %d blocks, notes %v", len(out), notes)
	}
	// A second pick is cleared with a note.
	out, notes = mustNormalize(t, list(`{"type":"cards","items":[{"title":"A","pick":true},{"title":"B","pick":"true"}]}`), PlaceInline, env)
	var cb cardsBlock
	_ = json.Unmarshal(out[0], &cb)
	if !bool(cb.Items[0].Pick) || bool(cb.Items[1].Pick) || len(notes) != 1 || !strings.Contains(notes[0], "Cleared the pick on blocks[0].items[1]") {
		t.Fatalf("picks: %s, notes %v", out[0], notes)
	}
	// Long strings are shortened, and the result says where.
	out, notes = mustNormalize(t, list(`{"type":"cards","items":[{"title":"`+strings.Repeat("Long ", 30)+`"}]}`), PlaceInline, env)
	_ = json.Unmarshal(out[0], &cb)
	if utf8.RuneCountInString(cb.Items[0].Title) != 80 || !strings.HasSuffix(cb.Items[0].Title, "…") || len(notes) != 1 || notes[0] != "Shortened to fit: blocks[0].items[0].title." {
		t.Fatalf("clamp: %q, notes %v", cb.Items[0].Title, notes)
	}
	// Images: https is proxied, http is left out, workspace files stay.
	out, notes = mustNormalize(t, list(`{"type":"images","items":[{"src":"https://img.example.com/a.jpg","caption":"A"},{"src":"http://img.example.com/b.jpg","caption":"B"},{"src":"room.jpg","caption":"Room"}]}`), PlaceInline, env)
	var ib imagesBlock
	_ = json.Unmarshal(out[0], &ib)
	if len(ib.Items) != 2 || !strings.HasPrefix(ib.Items[0].Src, "/api/img/") || ib.Items[1].Src != "room.jpg" || len(notes) != 1 {
		t.Fatalf("images: %s, notes %v", out[0], notes)
	}
	// Files that don't exist are left out; none at all is refused.
	out, notes = mustNormalize(t, list(`{"type":"files","items":["room.jpg",{"path":"nope.pdf"}]}`), PlaceInline, env)
	if !strings.Contains(string(out[0]), `"items":["room.jpg"]`) || len(notes) != 1 {
		t.Fatalf("files: %s, notes %v", out[0], notes)
	}
	if _, _, err := normalizeBlocks(list(`{"type":"files","items":["../../etc/passwd"]}`), PlaceInline, env); err == nil {
		t.Fatal("a path outside the workspace was accepted")
	}
	// Tolerant input: numbers as strings, strings as numbers, lower-case
	// currencies, local date-times with a space, 12-hour times.
	out, notes = mustNormalize(t, list(
		`{"type":"cards","kind":"stay","items":[{"title":"Hotel","price":{"amount":"1,299.50","currency":"eur","per":"night"},"check_in":"2026-11-14 15:00","phone":"+442079460958"}]}`,
		`{"type":"timeline","days":[{"date":"2026-11-14","items":[{"time":"7:30 PM","title":"Dinner"}]}]}`,
		`{"type":"stats","items":[{"label":"Fare","value":412}]}`), PlaceInline, env)
	if len(notes) != 0 {
		t.Fatalf("notes %v", notes)
	}
	joined := string(out[0]) + string(out[1]) + string(out[2])
	for _, want := range []string{`"amount":1299.5`, `"currency":"EUR"`, `"check_in":"2026-11-14T15:00"`, `"phone":"+442079460958"`, `"time":"19:30"`, `"value":"412"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("normalized blocks lack %s: %s", want, joined)
		}
	}
	// Ids are kept when they're valid, and never carry a card number.
	out, _ = mustNormalize(t, list(`{"type":"stats","id":"Fares-1","items":[{"label":"Fare","value":"$412"}]}`, `{"type":"stats","id":"4242424242424242","items":[{"label":"Fare","value":"$412"}]}`), PlaceInline, env)
	if !strings.Contains(string(out[0]), `"id":"fares-1"`) || strings.Contains(string(out[1]), `"id"`) {
		t.Fatalf("ids: %s %s", out[0], out[1])
	}
	if src, _ := normalizeSources([]any{map[string]any{"title": "Order 4242424242424242", "url": "https://shop.example.com/o"}}, 6); len(src) != 1 || src[0].Title != "" {
		t.Fatalf("source with a card number in its title: %+v", src)
	}
	// An action that can't work is left out; the card stays.
	out, notes = mustNormalize(t, list(`{"type":"cards","items":[{"title":"A","action":{"do":"report","id":"nope"}},{"title":"B","action":{"do":"report","id":"r1"}}]}`), PlaceInline, env)
	_ = json.Unmarshal(out[0], &cb)
	if cb.Items[0].Action != nil || cb.Items[1].Action == nil || len(notes) != 1 {
		t.Fatalf("actions: %s, notes %v", out[0], notes)
	}
}

func TestCardNumberGuard(t *testing.T) {
	for s, want := range map[string]bool{
		"4242424242424242":          true,
		"4242 4242 4242 4242":       true,
		"4242-4242-4242-4242":       true,
		"Visa ···· 4242":            false,
		"Order 112-4471234-5678901": false,
		"Call +1 415 555 0100":      false,
		"Total $1,299.50":           false,
		"Ticket 016 2123 4567 8901": false, // grouped, but not a card (fails Luhn)
		"Account 1234567890123":     true,
	} {
		if got := looksLikeCardNumber(s); got != want {
			t.Errorf("looksLikeCardNumber(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestNormalizeForm(t *testing.T) {
	form, err := normalizeForm(json.RawMessage(`{"submit":"Search","fields":[
		{"name":"check_in","label":"Check-in","type":"date","value":"2026-11-14","required":true},
		{"name":"guests","label":"Guests","type":"stepper","value":"2","min":1,"max":8},
		{"name":"area","label":"Area","type":"select","options":["Alfama","Chiado"],"value":"Baixa"},
		{"label":"Breakfast included","type":"toggle","value":true}]}`), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	f := form.Fields
	if f[0].Value != "2026-11-14" || f[1].Value != 2.0 || f[2].Value != nil || f[3].Name != "breakfast_included" || f[3].Value != true {
		t.Fatalf("fields: %+v", f)
	}
	for _, c := range []struct{ form, want string }{
		{`{"fields":[{"name":"card_number","label":"Card","type":"text"}]}`, "Never collect this in a form; use request_login or request_takeover."},
		{`{"fields":[{"name":"pw","label":"Password","type":"text"}]}`, "Never collect this in a form"},
		{`{"fields":[{"name":"code","label":"PIN","type":"number"}]}`, "Never collect this in a form"},
		{`{"fields":[{"name":"code","label":"Verification code","type":"text"}]}`, "Never collect this in a form"},
		{`{"fields":[{"name":"ssn","label":"Your number","type":"text"}]}`, "Never collect this in a form"},
		{`{"fields":[{"name":"a","label":"A","type":"slider"}]}`, "form.fields[0].type must be one of"},
		{`{"fields":[{"name":"a","label":"A","type":"select"}]}`, "a select needs choices"},
		{`{"fields":[]}`, "a form needs at least one field"},
		{`{"fields":[{"name":"a","label":"A","type":"text"},{"name":"a","label":"B","type":"text"}]}`, "is used twice"},
	} {
		if _, err := normalizeForm(json.RawMessage(c.form), time.UTC); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("form %s: err = %v, want %q", c.form, err, c.want)
		}
	}
	// Words that only contain a credential word are fine.
	if _, err := normalizeForm(json.RawMessage(`{"fields":[{"name":"shipping","label":"Shipping","type":"text"},{"name":"boarding_pass","label":"Boarding pass","type":"toggle"}]}`), time.UTC); err != nil {
		t.Errorf("harmless fields refused: %v", err)
	}
}

func TestReportMarkdown(t *testing.T) {
	rt := &Runtime{cfg: &config.Config{DataDir: t.TempDir()}}
	env, _ := testBlockEnv(t)
	env.image = func(src string) (string, bool) { return rt.mintImage(7, src) }
	blocks, _ := mustNormalize(t, list(
		`{"type":"cards","kind":"product","items":[{"title":"Sony WH-1000XM6","image":"https://img.example.com/sony.jpg","price":{"amount":279,"currency":"USD"},"pick":true}]}`,
		blockFixtures[1], blockFixtures[3], blockFixtures[4],
		`{"type":"markdown","text":"## Why the Sony\nIt cancels the most noise."}`), PlaceReport, env)
	rep := &store.Report{ID: "r1", Title: "Headphones", Format: "markdown", Data: marshal(ReportData{Blocks: blocks, Takeaway: "Get the Sony."})}
	md := rt.ReportMarkdown(7, rep)
	for _, want := range []string{"# Headphones\n", "_Get the Sony._", "**Sony WH-1000XM6** — Alexa’s pick", "$279", "![Sony WH-1000XM6](https://img.example.com/sony.jpg)",
		"|  | Sony XM6 (pick) | Bose QC Ultra | AirPods Max |", "**$279** ✓", "| **Total** | **$303.76** |", "**Sat, Nov 14**", "- 08:10 UA 1543 SFO to JFK", "## Why the Sony"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	// The proxied image resolves for its user only.
	var cb cardsBlock
	_ = json.Unmarshal(blocks[0], &cb)
	token := strings.TrimPrefix(cb.Items[0].Image, "/api/img/")
	if src, err := rt.ImageSource(7, token); err != nil || src != "https://img.example.com/sony.jpg" {
		t.Errorf("ImageSource = %q, %v", src, err)
	}
	if _, err := rt.ImageSource(8, token); err == nil {
		t.Error("another user resolved the image token")
	}
	if _, err := rt.ImageSource(7, "../../etc/passwd"); err == nil {
		t.Error("a bad token resolved")
	}
	// The preview and the entry's data.
	p := reportPreview(blocks)
	if p == nil || len(p.Thumbs) != 1 || p.Kinds != "1 option, comparison, receipt, itinerary" {
		t.Errorf("preview = %+v", p)
	}
	rep.Data = marshal(ReportData{Blocks: blocks, Takeaway: "Get the Sony.", Preview: p})
	if d := string(reportEntryData(rep, false)); !strings.Contains(d, `"preview":{`) || !strings.Contains(d, `"takeaway":"Get the Sony."`) {
		t.Errorf("entry data = %s", d)
	}
	if got := reportNotifyBody(rep); got != "Get the Sony." {
		t.Errorf("notify body = %q", got)
	}
	// Timeline items become calendar events.
	ev := BlockEvents(blocks)
	if len(ev) != 2 || ev[0].Start != "2026-11-14T08:10" || ev[1].Location != "51 Grove St" {
		t.Errorf("events = %+v", ev)
	}
}

// ---------- the tools ----------

// newToolTest gives say, ask and present_report a session to work in: a
// store, the hub, a workspace and a signed-up user.
func newToolTest(t *testing.T) (*toolCtx, *hub.Sub) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "alexa.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := hub.New()
	n, err := notify.New(ctx, st, h, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	envs := userenv.New(dir, false, 0)
	u, err := st.CreateUser(ctx, "alice", "x", "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	env, err := envs.Ensure(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{cfg: &config.Config{DataDir: dir}, st: st, hub: h, notifier: n, envs: envs, baseCtx: ctx,
		runs: map[int64]*run{}, control: map[int64]bool{}, locks: map[int64]*sync.Mutex{}, mems: map[int64][]*store.Memory{}}
	return &toolCtx{ctx: ctx, rt: rt, run: &run{user: u, env: env}}, h.Subscribe(u.ID, "", "")
}

// nextEntry waits for the next entry event on the hub.
func nextEntry(t *testing.T, sub *hub.Sub) map[string]any {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case b := <-sub.Events():
			var ev struct {
				Type string         `json:"type"`
				Data map[string]any `json:"data"`
			}
			if json.Unmarshal(b, &ev) == nil && ev.Type == "entry" {
				return ev.Data
			}
		case <-timeout:
			t.Fatal("no entry event")
			return nil
		}
	}
}

func TestSayWithBlocks(t *testing.T) {
	tc, sub := newToolTest(t)
	res, err := say(tc, map[string]any{
		"text":        "The Sony is the one to get.",
		"blocks":      []any{mustJSON(blockFixtures[0])},
		"sources":     []any{map[string]any{"title": "Wirecutter", "url": "https://www.nytimes.com/wirecutter/"}, map[string]any{"url": "javascript:alert(1)"}},
		"suggestions": []any{"Track the price", "Track the price", "Compare with Bose"},
		"final":       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Text, "Said. Shown: 1 product card (pick: Sony WH-1000XM6), 1 source.") || !strings.Contains(res.Text, "Left out sources[1]") {
		t.Fatalf("result = %q", res.Text)
	}
	// Broadcast as it's stored, and stored as the client reads it.
	ev := nextEntry(t, sub)
	data, _ := ev["data"].(map[string]any)
	if ev["kind"] != "say" || data == nil || data["final"] != true || data["summary"] != "Sony WH-1000XM6 $279 (pick)" {
		t.Fatalf("event = %v", ev)
	}
	feed, err := tc.rt.st.Feed(tc.ctx, tc.run.user.ID, 0, 10)
	if err != nil || len(feed) != 1 {
		t.Fatalf("feed: %v %v", feed, err)
	}
	var stored sayData
	if err := json.Unmarshal(feed[0].Data, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Blocks) != 1 || len(stored.Sources) != 1 || len(stored.Suggestions) != 2 || !stored.Final {
		t.Fatalf("stored = %s", feed[0].Data)
	}
	var cb cardsBlock
	if err := json.Unmarshal(stored.Blocks[0], &cb); err != nil || cb.Items[0].Action.Text != "Let's go with the Sony WH-1000XM6 ($279)." {
		t.Fatalf("stored block = %s (%v)", stored.Blocks[0], err)
	}

	// A table too long for a message is refused with the fix, and nothing
	// is shown; the model's next call, with a report, goes through.
	rows := make([]any, 23)
	for i := range rows {
		rows[i] = []any{"Hotel", "$1"}
	}
	res, _ = say(tc, map[string]any{"text": "Here are the hotels.", "blocks": []any{map[string]any{"type": "table", "columns": []any{"Hotel", "Price"}, "rows": rows}}})
	if res.Text != "Not sent: a table in a message can have at most 8 rows (this one has 23). Show the few that matter for the user's choice, or put the full list in present_report and say a one-line takeaway." {
		t.Fatalf("refusal = %q", res.Text)
	}
	if feed, _ := tc.rt.st.Feed(tc.ctx, tc.run.user.ID, 0, 10); len(feed) != 1 {
		t.Fatal("a refused message was shown")
	}
	res, err = presentReport(tc, map[string]any{"title": "Hotels in Alfama", "takeaway": "Memmo is the pick.", "blocks": []any{map[string]any{"type": "table", "columns": []any{"Hotel", "Price"}, "rows": rows}}})
	if err != nil || !strings.Contains(res.Text, "Shown: a table (23 rows).") {
		t.Fatalf("report: %v %v", res, err)
	}
	all, _ := tc.rt.st.ListReports(tc.ctx, tc.run.user.ID, 5)
	if len(all) != 1 {
		t.Fatalf("reports = %+v", all)
	}
	rep, err := tc.rt.st.GetReport(tc.ctx, tc.run.user.ID, all[0].ID)
	if err != nil || rep.Content != "" {
		t.Fatalf("report = %+v, %v", rep, err)
	}
	reps := []*store.Report{rep}
	var rd ReportData
	_ = json.Unmarshal(reps[0].Data, &rd)
	if len(rd.Blocks) != 1 || rd.Takeaway != "Memmo is the pick." || rd.Preview == nil || rd.Preview.Kinds != "table" {
		t.Fatalf("report data = %s", reps[0].Data)
	}
	md := tc.rt.ReportMarkdown(tc.run.user.ID, reps[0])
	if !strings.HasPrefix(md, "# Hotels in Alfama\n\n_Memmo is the pick._\n\n| Hotel | Price |") || strings.Count(md, "| Hotel | $1 |") != 23 {
		t.Fatalf("report markdown:\n%s", md)
	}
}

func TestAskRich(t *testing.T) {
	tc, _ := newToolTest(t)
	res, err := ask(tc, map[string]any{
		"question": "Place this order for $303.76?", "options": []any{"Place order", "Change something"}, "recommended": 0.0,
		"confirm": true, "blocks": []any{mustJSON(blockFixtures[3])},
	})
	if err != nil || res.Suspend == nil {
		t.Fatalf("ask: %+v %v", res, err)
	}
	nd := res.Suspend
	if !nd.Confirm || len(nd.Blocks) != 1 || nd.Recommended == nil || *nd.Recommended != 0 {
		t.Fatalf("needs = %+v", nd)
	}
	d := string(askEntryData(nd))
	for _, want := range []string{`"confirm":true`, `"recommended":0`, `"blocks":[{"type":"receipt"`, `"options":["Place order","Change something"]`} {
		if !strings.Contains(d, want) {
			t.Errorf("ask entry data lacks %s: %s", want, d)
		}
	}
	// A confirm card can't show a confirmed receipt: nothing has happened yet.
	confirmed := strings.Replace(blockFixtures[3], `"draft"`, `"confirmed"`, 1)
	res, _ = ask(tc, map[string]any{"question": "Order?", "confirm": true, "blocks": []any{mustJSON(confirmed)}})
	if res.Suspend != nil || !strings.Contains(res.Text, "should be draft") {
		t.Fatalf("confirmed receipt on a confirm card: %+v", res)
	}
	// A form with a credential field is refused.
	res, _ = ask(tc, map[string]any{"question": "Card details?", "form": map[string]any{"fields": []any{map[string]any{"name": "card_number", "label": "Card number", "type": "text"}}}})
	if res.Suspend != nil || !strings.Contains(res.Text, "Never collect this in a form; use request_login or request_takeover.") {
		t.Fatalf("credential form: %+v", res)
	}
	// multi needs options.
	res, _ = ask(tc, map[string]any{"question": "Which stores?", "multi": true, "options": []any{"Amazon"}})
	if res.Suspend != nil {
		t.Fatal("multi with one option was asked")
	}
	res, _ = ask(tc, map[string]any{"question": "Which stores?", "multi": true, "options": []any{"Amazon", "Best Buy", "B&H"}})
	if res.Suspend == nil || !res.Suspend.Multi {
		t.Fatalf("multi: %+v", res)
	}
}

func TestReplies(t *testing.T) {
	tc, _ := newToolTest(t)
	rt, uid := tc.rt, tc.run.user.ID
	if _, err := say(tc, map[string]any{"text": "Two good ones.", "blocks": []any{mustJSON(blockFixtures[0])}}); err != nil {
		t.Fatal(err)
	}
	feed, _ := rt.st.Feed(tc.ctx, uid, 0, 1)
	sayID := feed[0].ID

	// A tapped card button: the user's entry shows the button's text, and
	// Alexa reads what was tapped on what.
	shown, said, data := rt.reply(tc.ctx, uid, Message{Text: "anything", Via: "button", Tap: &Tap{EntryID: sayID, Block: 0, Item: 0}})
	if shown != "Let's go with the Sony WH-1000XM6 ($279)." || said != `[Tapped "Choose this" on: Sony WH-1000XM6] Let's go with the Sony WH-1000XM6 ($279).` || data["via"] != "button" {
		t.Fatalf("tap: %q / %q / %v", shown, said, data)
	}
	// A tap that points nowhere stays a plain message.
	if shown, said, _ := rt.reply(tc.ctx, uid, Message{Text: "Choose this", Via: "button", Tap: &Tap{EntryID: sayID, Block: 3}}); shown != "Choose this" || said != "Choose this" {
		t.Fatalf("bad tap: %q / %q", shown, said)
	}

	// A form answer, against the open question's form.
	form, _ := normalizeForm(json.RawMessage(`{"fields":[{"name":"date","label":"Date","type":"date"},{"name":"guests","label":"Guests","type":"stepper","min":1,"max":8},{"name":"notes","label":"Notes","type":"text"}]}`), time.UTC)
	nd := &Needs{Kind: "question", Message: "When?", Form: form, CallID: "call_1", EntryID: 99}
	if err := rt.st.SetSessionNeeds(tc.ctx, uid, marshal(nd)); err != nil {
		t.Fatal(err)
	}
	if err := rt.st.SetSessionStatus(tc.ctx, uid, "needs", ""); err != nil {
		t.Fatal(err)
	}
	shown, said, data = rt.reply(tc.ctx, uid, Message{Via: "form", Values: map[string]any{"guests": "2", "date": "2026-11-14", "extra": "x", "notes": ""}})
	if said != "[Form] date=2026-11-14; guests=2" || shown != "Date: 2026-11-14 · Guests: 2" || data["via"] != "form" {
		t.Fatalf("form: %q / %q / %v", shown, said, data)
	}
	// The app's own summary wins for the entry; Alexa still gets the values.
	shown, said, _ = rt.reply(tc.ctx, uid, Message{Text: "Sat 14 Nov · 2 guests", Via: "form", Values: map[string]any{"guests": 2.0, "date": "2026-11-14"}})
	if shown != "Sat 14 Nov · 2 guests" || said != "[Form] date=2026-11-14; guests=2" {
		t.Fatalf("form with text: %q / %q", shown, said)
	}
	// Multi-select and typed messages pass through.
	if shown, said, data := rt.reply(tc.ctx, uid, Message{Text: "Amazon, B&H", Via: "multi"}); shown != "Amazon, B&H" || said != "Amazon, B&H" || data["via"] != "multi" {
		t.Fatalf("multi: %q / %q / %v", shown, said, data)
	}
	if shown, said, data := rt.reply(tc.ctx, uid, Message{Text: "hello"}); shown != "hello" || said != "hello" || data != nil {
		t.Fatalf("typed: %q / %q / %v", shown, said, data)
	}
}

func mustJSON(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return v
}
