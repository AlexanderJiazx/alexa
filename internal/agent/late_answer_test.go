package agent

import (
	"strings"
	"testing"

	"alexagent/internal/llm"
)

// A late answer to a skipped form question, sent while another question
// waits, doesn't answer that one: it waits in the inbox, and Alexa reads it
// after the user's answer to the open question (FE-1).
func TestLateAnswerLeavesTheOpenQuestion(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("ask", map[string]any{"question": "When?", "form": map[string]any{"fields": []any{
		map[string]any{"name": "date", "label": "Date", "type": "date"},
		map[string]any{"name": "guests", "label": "Guests", "type": "stepper", "min": 1, "max": 8},
	}}})))
	e.send("Book me a table")
	e.settle()
	when := entriesOf(e.feed(), "ask")[0]
	e.model.script(does(fc("ask", map[string]any{"question": "Budget?", "options": []any{"€50", "€100"}})))
	e.send("Skip this one for now. Use your best judgement.")
	if v := e.settle(); v.Status != "needs" || v.Needs.Message != "Budget?" {
		t.Fatalf("view = %+v", v)
	}
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "About “When?”: Sat 14 Nov · 2 guests", Via: "form", Values: map[string]any{"date": "2026-11-14", "guests": 2.0}, AskEntry: when.ID}); err != nil {
		t.Fatal(err)
	}
	if nd := e.rt.pendingNeeds(e.ctx, e.user.ID); nd == nil || nd.Message != "Budget?" {
		t.Fatalf("the Budget question was answered by the late answer: %+v", nd)
	}
	var input string
	e.model.script(func(req *llm.Request) []any {
		input = string(marshal(req.Input))
		return []any{fc("say", map[string]any{"text": "Booked for 2 on the 14th, under €100.", "final": true})}
	})
	e.send("€100")
	e.settle()
	budget := strings.Index(input, "The user replied: €100")
	late := strings.Index(input, "About “When?”: [Form] date=2026-11-14; guests=2")
	if budget < 0 || late < budget {
		t.Fatalf("the model read the answer at %d and the late answer at %d: %s", budget, late, input)
	}
}
