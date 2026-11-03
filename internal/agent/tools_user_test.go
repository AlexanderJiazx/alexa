package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"alexagent/internal/store"
)

// A choice sent from the report Alexa showed in this exchange carries the
// exchange on, as a tap on her latest cards does. One sent from a report of
// an earlier exchange starts a new one.
func TestReportSendContinues(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(report("Lisbon hotels", nil)))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	book := Message{Text: "Book Casa Alfama", Via: "report"}
	continues := func() bool {
		_, _, via := e.rt.reply(e.ctx, e.user.ID, book)
		return e.rt.continues(e.ctx, e.user.ID, book, via)
	}
	if !continues() {
		t.Errorf("a choice sent from the report Alexa just showed starts a new exchange")
	}
	// Closed and opened again, it's still this exchange's report.
	e.setSurface(Surface{Kind: "report", ID: id})
	if !continues() {
		t.Errorf("a choice sent from the report, opened again, starts a new exchange")
	}
	if e.rt.continues(e.ctx, e.user.ID, Message{Text: "Thanks"}, nil) {
		t.Errorf("a typed message carries the exchange on")
	}

	e.model.script(does(fc("say", map[string]any{"text": "Sunny all week.", "final": true})))
	e.send("What's the weather there?")
	e.settle()
	e.setSurface(Surface{Kind: "report", ID: id}) // opened from Reflect
	if continues() {
		t.Errorf("a choice sent from a report of an earlier exchange carries on the current one")
	}
	e.setSurface(Surface{})
	if continues() {
		t.Errorf("a message marked as from a report, with none on screen, carries the exchange on")
	}
}

// The panel shows an html report as its own page and nothing else, so one
// without content, or with blocks, is refused instead of saved blank or
// said to show what it doesn't.
func TestHTMLReportNeedsItsPage(t *testing.T) {
	e := newProgressEnv(t)
	cards := []any{map[string]any{"type": "cards", "kind": "stay", "items": []any{map[string]any{"title": "Casa Alfama"}, map[string]any{"title": "Lisboa Prata"}}}}
	page := "<!doctype html><h1>Lisbon stays</h1><div class=card>Casa Alfama</div>"
	e.model.script(
		does(fc("present_report", map[string]any{"title": "Lisbon stays", "format": "html", "blocks": cards})),
		does(fc("present_report", map[string]any{"title": "Lisbon stays", "format": "html", "content": page, "blocks": cards})),
		does(fc("present_report", map[string]any{"title": "Lisbon stays", "format": "html", "content": page, "final": true})),
	)
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	out := e.conversation()
	for _, want := range []string{"Not saved: an html report needs content", "Not saved: an html report shows only its own page"} {
		if !strings.Contains(out, want) {
			t.Errorf("the model wasn't told %q: …%s", want, tailOf(out, 800))
		}
	}
	reps := entriesOf(e.feed(), "report")
	if len(reps) != 1 {
		t.Fatalf("%d reports saved, want only the one with its page", len(reps))
	}
	id, _ := e.lastReport()
	rep, err := e.st.GetReport(e.ctx, e.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Format != "html" || rep.Content != page {
		t.Errorf("saved %s report %q", rep.Format, rep.Content)
	}
}

// A report's sources are checked as a message's are: http(s) only, once
// each, titles cleaned. The panel makes each one a link.
func TestReportSources(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(report("Lisbon hotels", map[string]any{"sources": []any{
		map[string]any{"title": "Open in VS Code", "url": "vscode://file/etc/passwd"},
		map[string]any{"title": "Run", "url": "ms-officecmd:launch"},
		map[string]any{"title": "  Alfama   Stay\n", "url": "https://alfama.example/"},
		map[string]any{"title": "Again", "url": "https://alfama.example/"},
		"https://loft.example/rooms",
		map[string]any{"title": "Card 4111 1111 1111 1111", "url": "https://pay.example/"},
	}})))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	rep, err := e.st.GetReport(e.ctx, e.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var data ReportData
	if err := json.Unmarshal(rep.Data, &data); err != nil {
		t.Fatal(err)
	}
	want := []Link{{Title: "Alfama Stay", URL: "https://alfama.example/"}, {URL: "https://loft.example/rooms"}, {URL: "https://pay.example/"}}
	if string(marshal(data.Sources)) != string(marshal(want)) {
		t.Errorf("sources = %+v, want %+v", data.Sources, want)
	}
	if out := e.conversation(); !strings.Contains(out, "Left out sources[0]: it needs an http(s) url.") {
		t.Errorf("the model wasn't told about the left-out sources: …%s", tailOf(out, 600))
	}
}

// A form question the user skipped and answered later is read against its
// own form, not the one open now, and Alexa reads which question it
// answers. answersEarlier tells it apart from an answer to the question
// open now (FE-1).
func TestLateFormAnswer(t *testing.T) {
	tc, _ := newToolTest(t)
	rt, uid := tc.rt, tc.run.user.ID
	when, _ := normalizeForm(json.RawMessage(`{"fields":[{"name":"date","label":"Date","type":"date"},{"name":"guests","label":"Guests","type":"stepper","min":1,"max":8}]}`), time.UTC)
	skipped := &store.Entry{UserID: uid, Kind: "ask", Text: "When?", Status: "answered", Data: askEntryData(&Needs{Kind: "question", Message: "When?", Form: when})}
	if err := rt.st.AddEntry(tc.ctx, skipped); err != nil {
		t.Fatal(err)
	}
	budget, _ := normalizeForm(json.RawMessage(`{"fields":[{"name":"budget","label":"Budget","type":"number"}]}`), time.UTC)
	open := &store.Entry{UserID: uid, Kind: "ask", Text: "Budget?", Status: "open", Data: askEntryData(&Needs{Kind: "question", Message: "Budget?", Form: budget})}
	if err := rt.st.AddEntry(tc.ctx, open); err != nil {
		t.Fatal(err)
	}
	nd := &Needs{Kind: "question", Message: "Budget?", Form: budget, CallID: "call_budget", EntryID: open.ID}
	if err := rt.st.SetSessionNeeds(tc.ctx, uid, marshal(nd)); err != nil {
		t.Fatal(err)
	}
	if err := rt.st.SetSessionStatus(tc.ctx, uid, "needs", ""); err != nil {
		t.Fatal(err)
	}

	late := Message{Text: "About “When?”: Sat 14 Nov · 2 guests", Via: "form", Values: map[string]any{"date": "2026-11-14", "guests": 2.0}, AskEntry: skipped.ID}
	shown, said, data := rt.reply(tc.ctx, uid, late)
	if said != "About “When?”: [Form] date=2026-11-14; guests=2" || shown != late.Text || data["via"] != "form" {
		t.Fatalf("late answer: %q / %q / %v", shown, said, data)
	}
	// Sent without the app's summary, the entry says which question too.
	late.Text = ""
	if shown, _, _ := rt.reply(tc.ctx, uid, late); shown != "About “When?”: Date: 2026-11-14 · Guests: 2" {
		t.Errorf("late answer without text shows %q", shown)
	}
	pending := rt.pendingNeeds(tc.ctx, uid)
	if pending == nil || pending.CallID != "call_budget" {
		t.Fatalf("the Budget question is no longer pending: %+v", pending)
	}
	if !late.answersEarlier(pending) {
		t.Error("a late answer is taken as the answer to the question open now")
	}

	// The question open now is answered against its own form, as before,
	// with or without its entry.
	for _, m := range []Message{
		{Via: "form", Values: map[string]any{"budget": 300.0, "date": "2026-11-14"}},
		{Via: "form", Values: map[string]any{"budget": 300.0, "date": "2026-11-14"}, AskEntry: open.ID},
	} {
		if m.answersEarlier(pending) {
			t.Errorf("an answer to the open question (askEntry %d) is taken as a late one", m.AskEntry)
		}
		if _, said, _ := rt.reply(tc.ctx, uid, m); said != "[Form] budget=300" {
			t.Errorf("answer to the open question (askEntry %d) reads %q", m.AskEntry, said)
		}
	}
	// One that points at no question keeps the plain values it sent, and
	// the question the app said it answers.
	if _, said, _ := rt.reply(tc.ctx, uid, Message{Via: "form", Values: map[string]any{"date": "2026-11-14"}, AskEntry: open.ID + 50}); said != "[Form] date=2026-11-14" {
		t.Errorf("answer to a missing question reads %q", said)
	}
	gone := Message{Text: "About “When?”: Sat 14 Nov", Via: "form", Values: map[string]any{"date": "2026-11-14"}, AskEntry: open.ID + 50}
	if _, said, _ := rt.reply(tc.ctx, uid, gone); said != "About “When?”: [Form] date=2026-11-14" {
		t.Errorf("answer to a missing question with the app's summary reads %q", said)
	}
}

// A question reaches the user as a notification only while they aren't
// looking at Alexa: the question is its title, and it opens the entry
// that shows it (D1).
func TestQuestionNotifiesWhenAway(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("ask", map[string]any{"question": "Which date works?", "options": []any{"Friday", "Saturday"}})))
	e.send("Book me a table")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q after the ask", v.Status)
	}
	asks := entriesOf(e.feed(), "ask")
	notes, err := e.st.Notifications(e.ctx, e.user.ID, 10)
	if err != nil || len(notes) != 1 || len(asks) != 1 {
		t.Fatalf("notifications = %+v, %v (asks %d)", notes, err, len(asks))
	}
	if n := notes[0]; n.Ref != fmt.Sprintf("entry:%d", asks[0].ID) || n.Title != "Which date works?" || n.Body != "Alexa needs you" {
		t.Errorf("notification = %+v", n)
	}

	// With Alexa open in front of them, the screen shows the question.
	sub := e.hub.Subscribe(e.user.ID, "phone", "")
	defer sub.Close()
	e.model.script(does(fc("ask", map[string]any{"question": "How many people?", "options": []any{"2", "4"}})))
	e.send("Saturday")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q after the second ask", v.Status)
	}
	if notes, _ := e.st.Notifications(e.ctx, e.user.ID, 10); len(notes) != 1 {
		t.Errorf("a question was notified while the user was looking: %d notifications", len(notes))
	}

	// A take-over is notified even so, and one with no reason still has a
	// title.
	e.model.script(does(fc("request_takeover", map[string]any{"reason": ""})))
	e.send("2")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("status = %q after the take-over", v.Status)
	}
	notes, _ = e.st.Notifications(e.ctx, e.user.ID, 10)
	if len(notes) != 2 {
		t.Fatalf("%d notifications after the take-over, want 2", len(notes))
	}
	if n := notes[0]; n.Title != "Alexa needs you in the browser" || !strings.HasPrefix(n.Ref, "entry:") {
		t.Errorf("take-over notification = %+v", n)
	}
}
