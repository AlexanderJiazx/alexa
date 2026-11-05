package agent

import (
	"strconv"
	"strings"
	"testing"
)

// A question Alexa asks while the user is away sends a note that opens it.
// Once the question is answered, the note is read on every device (D1).
func TestAnsweredQuestionReadsItsNote(t *testing.T) {
	e := newProgressEnv(t)
	e.model.script(does(fc("ask", map[string]any{"question": "Which dates?", "options": []any{"May", "June"}})))
	e.send("Plan a trip to Lisbon")
	if v := e.settle(); v.Status != "needs" {
		t.Fatalf("view = %+v", v)
	}
	asked := entriesOf(e.feed(), "ask")[0]
	ref := "entry:" + strconv.FormatInt(asked.ID, 10)
	note := func() (read, found bool) {
		list, err := e.rt.st.Notifications(e.ctx, e.user.ID, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range list {
			if n.Ref == ref {
				return n.Read, true
			}
		}
		return false, false
	}
	if read, found := note(); !found || read {
		t.Fatalf("before the answer, the note about %s: found %v, read %v", ref, found, read)
	}
	before := len(e.events("changed"))
	e.model.script(does(fc("say", map[string]any{"text": "May it is.", "final": true})))
	e.send("May")
	e.settle()
	if read, _ := note(); !read {
		t.Fatal("the note about the answered question is still unread")
	}
	var told bool
	for _, ev := range e.events("changed")[before:] {
		told = told || strings.Contains(string(ev.Data), `"notifications"`)
	}
	if !told {
		t.Fatal("other devices weren't told the notifications changed")
	}
}
