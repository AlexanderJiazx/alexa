package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"alexagent/internal/llm"
)

// A message sent while Alexa works reaches her even when its request goes
// away (a dropped phone connection): its entry is on the screen, and a
// retry with the same clientId isn't sent again.
func TestSendSurvivesTheRequest(t *testing.T) {
	e := newProgressEnv(t)
	started, release := e.blockingTool("fetch_url")
	var input string
	e.model.script(
		does(fc("fetch_url", map[string]any{"url": "https://hotels.example", "status": "Reading the reviews"})),
		func(req *llm.Request) []any {
			input = inputOf(req)
			return []any{fc("say", map[string]any{"text": "Only 4 stars and up, then.", "final": true})}
		},
		does(),
	)
	e.send("Find a hotel in Porto")
	<-started
	gone, cancel := context.WithCancel(e.ctx)
	cancel()
	if err := e.rt.Send(gone, e.user, Message{Text: "only 4 stars or more", ClientID: "c-42"}); err != nil {
		t.Fatalf("Send after the request went away: %v", err)
	}
	release <- struct{}{}
	e.settle()
	if users := entriesOf(e.feed(), "user"); len(users) != 2 {
		t.Errorf("user entries = %d, want 2", len(users))
	}
	if !strings.Contains(input, "only 4 stars or more") {
		t.Errorf("Alexa never got the message:\n%s", tailOf(input, 600))
	}
}

// A note handed back after the browser was already back (another screen
// pressed Done first) is still the user's message: Alexa reads what the
// conversation shows them sending.
func TestHandBackNoteAfterControlIsBack(t *testing.T) {
	e := newProgressEnv(t)
	var input string
	e.model.script(func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "The 7pm slot it is.", "final": true})}
	}, does())
	if err := e.rt.TakeOver(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	if err := e.rt.HandBack(e.ctx, e.user, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.rt.HandBack(e.ctx, e.user, "use the 7pm slot"); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if !strings.Contains(input, "use the 7pm slot") {
		t.Errorf("the note shown as sent never reached Alexa:\n%s", tailOf(input, 600))
	}
	if users := entriesOf(e.feed(), "user"); len(users) != 1 || users[0].Text != "use the 7pm slot" {
		t.Errorf("user entries = %v", users)
	}
}

// A choice sent from the report Alexa just showed answers it, as a tap on
// her cards does: it doesn't start a new exchange, so the report stays on
// screen instead of closing with the old exchange.
func TestReportSendKeepsTheExchange(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(report("Lisbon hotels", nil)))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	e.model.script(does(fc("say", map[string]any{"text": "Booking Casa Alfama.", "final": true})))
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Book Casa Alfama", Via: "report"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	users := entriesOf(e.feed(), "user")
	var d map[string]any
	_ = json.Unmarshal(users[len(users)-1].Data, &d)
	if _, ok := d["start"]; ok {
		t.Errorf("a choice sent from the report started a new exchange: %s", users[len(users)-1].Data)
	}
	if s := e.surface(); s != (Surface{Kind: "report", ID: id}) {
		t.Errorf("a choice sent from the report closed it: surface = %+v", s)
	}
}
