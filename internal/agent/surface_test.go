package agent

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"alexagent/internal/llm"
)

// The surface rules R1-R8 (surface.go), against the scripted model in
// plan_test.go.

func (e *progressEnv) surface() Surface {
	e.t.Helper()
	return e.rt.View(e.ctx, e.user.ID).Surface
}

func (e *progressEnv) setSurface(s Surface) {
	e.t.Helper()
	if err := e.rt.SetSurface(e.ctx, e.user, s); err != nil {
		e.t.Fatal(err)
	}
}

// lastReport is the id of the newest report entry, and whether it said it
// replaced an earlier version.
func (e *progressEnv) lastReport() (id string, updated bool) {
	e.t.Helper()
	reps := entriesOf(e.feed(), "report")
	if len(reps) == 0 {
		e.t.Fatal("no report in the feed")
	}
	var d struct {
		ID      string `json:"id"`
		Updated bool   `json:"updated"`
	}
	_ = json.Unmarshal(reps[len(reps)-1].Data, &d)
	return d.ID, d.Updated
}

func report(title string, extra map[string]any) any {
	a := map[string]any{"title": title, "content": "## " + title + "\nTwo quiet picks in Alfama, both under €150 a night.", "final": true}
	for k, v := range extra {
		a[k] = v
	}
	return fc("present_report", a)
}

var tipBlocks = []any{map[string]any{"type": "callout", "tone": "tip", "text": "Gate B14 is a 10-minute walk."}}

func (e *progressEnv) fakeBrowser() {
	e.fakeTool("browser_open", func(a map[string]any) (*toolResult, error) {
		return page("Opened "+str(a, "url"), "https://"+str(a, "url")+"/", "Flight status", `<a href="/status"> Flight status`), nil
	})
}

func openShop() any {
	return fc("browser_open", map[string]any{"url": "united.com", "status": "Opening United", "done": "Opened United"})
}

// Acceptance 1 and 2: a report Alexa opened closes when an unrelated
// request starts; one the user chose stays, and their hold ends there.
func TestNewExchangeClosesThePanel(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(report("Lisbon hotels", nil)))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	if s := e.surface(); s != (Surface{Kind: "report", ID: id}) {
		t.Fatalf("after the report, surface = %+v", s)
	}

	e.model.script(does(fc("say", map[string]any{"text": "It's 18°C and sunny."})), does())
	e.send("What's the weather in Porto?")
	e.settle()
	if s := e.surface(); s != (Surface{}) {
		t.Errorf("a new exchange kept the last one's panel: %+v", s)
	}

	// The user opens the report again: it holds the screen.
	e.setSurface(Surface{Kind: "report", ID: id})
	if v := e.rt.View(e.ctx, e.user.ID); !v.Surface.Held || v.Follow {
		t.Errorf("a chosen report: held %v, follow %v", v.Surface.Held, v.Follow)
	}
	e.model.script(does(fc("say", map[string]any{"text": "Booked for 7pm."})), does())
	e.send("Book a table at Taberna da Rua for tonight")
	v := e.settle()
	if v.Surface != (Surface{Kind: "report", ID: id}) || !v.Follow {
		t.Errorf("a held report after a new exchange: %+v, follow %v; want it shown and no longer held", v.Surface, v.Follow)
	}
}

// Acceptance 3 (R6, R3, R4): a scheduled run that browses and presents a
// report leaves the report the user is reading on screen.
func TestScheduledRunLeavesTheScreen(t *testing.T) {
	e := newProgressEnv(t)
	e.fakeBrowser()
	e.model.script(does(report("Lisbon hotels", nil)))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	reading, _ := e.lastReport()

	reached, release := make(chan View, 1), make(chan struct{})
	e.model.script(
		does(openShop()),
		func(*llm.Request) []any {
			reached <- e.rt.View(e.ctx, e.user.ID)
			<-release
			return []any{report("UA 1180 status", nil)}
		},
		does(fc("say", map[string]any{"text": "UA 1180 is on time.", "final": true, "blocks": tipBlocks})),
		does(),
	)
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Check UA 1180's status", Source: "schedule", Label: "Flight watch"}); err != nil {
		t.Fatal(err)
	}
	during := <-reached // the run has browsed
	close(release)
	v := e.settle()
	if during.Follow || during.Surface != (Surface{Kind: "report", ID: reading}) {
		t.Errorf("while the scheduled run browsed: surface %+v, follow %v", during.Surface, during.Follow)
	}
	if v.Surface != (Surface{Kind: "report", ID: reading}) {
		t.Errorf("after the scheduled run, surface = %+v, want the report the user was reading", v.Surface)
	}
	if id, updated := e.lastReport(); id == reading || updated {
		t.Errorf("the scheduled report's card = %s (updated %v)", id, updated)
	}
}

// Acceptance 4 (R5): a final say with blocks after browsing closes the
// browser; when the user pinned the browser mid-run, it stays.
func TestResultsTakeTheStage(t *testing.T) {
	e := newProgressEnv(t)
	e.fakeBrowser()
	var browsing Surface
	e.model.script(
		does(openShop()),
		func(*llm.Request) []any {
			browsing = e.surface()
			return []any{fc("say", map[string]any{"text": "UA 1180 is on time, boarding 7:40.", "final": true, "blocks": tipBlocks})}
		},
		does(),
	)
	e.send("Is my United flight on time?")
	v := e.settle()
	if browsing != (Surface{Kind: "browser"}) {
		t.Errorf("while browsing, surface = %+v; want the browser (follow mode)", browsing)
	}
	if v.Surface != (Surface{}) {
		t.Errorf("after a final say with blocks, surface = %+v; want the browser closed", v.Surface)
	}

	// A plain final say leaves the page where it is.
	e.model.script(does(openShop()), does(fc("say", map[string]any{"text": "It's on time.", "final": true})), does())
	e.send("And UA 1181?")
	if v := e.settle(); v.Surface != (Surface{Kind: "browser"}) {
		t.Errorf("after a final say without blocks, surface = %+v", v.Surface)
	}

	// The model often leaves final off: a say with blocks that turns out to
	// be the last word still takes the stage when the run ends.
	e.model.script(does(openShop()), does(fc("say", map[string]any{"text": "Two fares under $300.", "blocks": tipBlocks})), does())
	e.send("Any cheaper fares?")
	if v := e.settle(); v.Surface != (Surface{}) {
		t.Errorf("after a say with blocks that ended the run, surface = %+v; want the browser closed", v.Surface)
	}
	// Browsing after it means the page is the point again.
	e.model.script(does(fc("say", map[string]any{"text": "Checking the second one.", "blocks": tipBlocks})), does(openShop()), does())
	e.send("Open the second fare")
	if v := e.settle(); v.Surface != (Surface{Kind: "browser"}) {
		t.Errorf("after browsing that followed a say with blocks, surface = %+v; want the browser", v.Surface)
	}

	// The user pins the browser mid-run.
	e.model.script(
		does(openShop()),
		func(*llm.Request) []any {
			e.setSurface(Surface{Kind: "browser"})
			return []any{report("Fare history", nil)}
		},
		does(fc("say", map[string]any{"text": "UA 1182 is on time.", "final": true, "blocks": tipBlocks})),
		does(),
	)
	e.send("And UA 1182?")
	v = e.settle()
	if v.Surface != (Surface{Kind: "browser", Held: true}) || v.Follow {
		t.Errorf("with the browser held, surface = %+v, follow %v; want it kept", v.Surface, v.Follow)
	}
	if _, updated := e.lastReport(); updated {
		t.Error("a new report says it was updated")
	}
}

// R4: a revision of a report the user isn't looking at doesn't take the
// screen; its card says it's updated.
func TestRevisedReportStaysOnItsCard(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(report("Lisbon hotels", nil)), does())
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()

	e.model.script(does(fc("say", map[string]any{"text": "Sure."})), does())
	e.send("Thanks")
	e.settle() // a new exchange: the panel closed

	e.model.script(does(report("Lisbon hotels", map[string]any{"id": id})), does())
	e.send("Add prices for May to the hotel report")
	v := e.settle()
	if v.Surface != (Surface{}) {
		t.Errorf("a revision took the screen: %+v", v.Surface)
	}
	if got, updated := e.lastReport(); got != id || !updated {
		t.Errorf("the revision's card = %s (updated %v), want %s updated", got, updated, id)
	}

	// While the user looks at it, the revision shows in place.
	e.setSurface(Surface{Kind: "report", ID: id})
	e.model.script(does(report("Lisbon hotels", map[string]any{"id": id})), does())
	e.send("And June")
	if v := e.settle(); v.Surface != (Surface{Kind: "report", ID: id}) {
		t.Errorf("viewing the revised report, surface = %+v", v.Surface)
	}
}

// R7 and R1: expanding a block shows its entry and holds it; follow
// switches the hold off again.
func TestExpandAndFollow(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("say", map[string]any{"text": "Gate B14.", "blocks": tipBlocks})), does())
	e.send("Which gate?")
	e.settle()
	says := entriesOf(e.feed(), "say")
	if len(says) == 0 {
		t.Fatal("no say")
	}
	id := strconv.FormatInt(says[len(says)-1].ID, 10)
	e.setSurface(Surface{Kind: "entry", ID: id})
	if v := e.rt.View(e.ctx, e.user.ID); v.Surface != (Surface{Kind: "entry", ID: id, Held: true}) || v.Follow {
		t.Errorf("expanded: surface %+v, follow %v", v.Surface, v.Follow)
	}
	for _, bad := range []string{"999999", "abc", "", "-3"} {
		if err := e.rt.SetSurface(e.ctx, e.user, Surface{Kind: "entry", ID: bad}); !errors.Is(err, ErrNotFound) {
			t.Errorf("entry %q: err = %v, want ErrNotFound", bad, err)
		}
	}
	if err := e.rt.SetSurface(e.ctx, e.user, Surface{Kind: "dashboard"}); err == nil {
		t.Error("an unknown surface was accepted")
	}
	if s := e.surface(); s.Kind != "entry" || s.ID != id {
		t.Errorf("a refused change changed the surface: %+v", s)
	}

	e.setSurface(Surface{Kind: "browser"})
	if v := e.rt.View(e.ctx, e.user.ID); v.Surface != (Surface{Kind: "browser", Held: true}) || v.Follow {
		t.Errorf("chosen browser: %+v, follow %v", v.Surface, v.Follow)
	}
	e.setSurface(Surface{Kind: "browser", Follow: true})
	if v := e.rt.View(e.ctx, e.user.ID); v.Surface != (Surface{Kind: "browser"}) || !v.Follow {
		t.Errorf("following again: %+v, follow %v", v.Surface, v.Follow)
	}
	var stored struct{ Follow *bool }
	ss, _ := e.st.GetSession(e.ctx, e.user.ID)
	if _ = json.Unmarshal(ss.Surface, &stored); stored.Follow != nil {
		t.Errorf("follow was stored: %s", ss.Surface)
	}

	// Following mid-run lets Alexa's browsing show again.
	e.fakeBrowser()
	views := make(chan [2]View, 1)
	e.model.script(
		func(*llm.Request) []any {
			e.setSurface(Surface{Kind: "entry", ID: id})
			held := e.rt.View(e.ctx, e.user.ID)
			e.setSurface(Surface{Kind: "entry", ID: id, Follow: true})
			views <- [2]View{held, e.rt.View(e.ctx, e.user.ID)}
			return []any{openShop()}
		},
		does(fc("say", map[string]any{"text": "The map is on screen."})),
		does(),
	)
	e.send("Show me the gate map")
	v := e.settle()
	got := <-views
	held, following := got[0], got[1]
	if held.Follow || !following.Follow {
		t.Errorf("mid-run: follow %v when held, %v after following", held.Follow, following.Follow)
	}
	if v.Surface != (Surface{Kind: "browser"}) {
		t.Errorf("following, Alexa's browsing showed %+v", v.Surface)
	}
}

// R8: taking over shows the browser, whatever the user held.
func TestTakeOverShowsTheBrowser(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(report("Lisbon hotels", nil)), does())
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	e.setSurface(Surface{Kind: "report", ID: id})
	if err := e.rt.TakeOver(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	if s := e.surface(); s.Kind != "browser" {
		t.Errorf("taking over shows %+v", s)
	}
	if err := e.rt.HandBack(e.ctx, e.user, ""); err != nil {
		t.Fatal(err)
	}
}

// A scheduled job that starts while the user holds a panel keeps the hold:
// it isn't a new exchange of theirs (R6).
func TestScheduledRunKeepsTheHold(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(report("Lisbon hotels", nil)), does())
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	e.setSurface(Surface{Kind: "report", ID: id})
	e.model.script(does())
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Check the price", Source: "schedule", Label: "Price watch"}); err != nil {
		t.Fatal(err)
	}
	if v := e.settle(); v.Surface != (Surface{Kind: "report", ID: id, Held: true}) {
		t.Errorf("after a scheduled run, surface = %+v; want the held report", v.Surface)
	}
}
