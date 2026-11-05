package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The summariser is told today's date, writes "Open threads" rather than
// "In progress", dates finished work and quotes standing limits; the
// summary comes back framed as background.
func TestCompactionHandoff(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	m.onCreate(summarises("## Open threads\nNone.\n\n## Done\nBooked a table at Nora, on Mon Nov 2."))
	uid := e.user.ID
	loc := userLoc(e.user)
	items := []json.RawMessage{storedMsg("user", stamp(loc, "", "Book a table at Nora for Friday, never above 60 a head"))}
	items = append(items, storedCall("say", map[string]any{"text": "Booked for Friday 7pm."}))
	items = append(items, storedOutput(lastCallID(), "Sent.", ""), storedMsg("user", stamp(loc, "", "thanks")))
	if err := e.st.AppendItems(e.ctx, uid, items); err != nil {
		t.Fatal(err)
	}
	if err := e.rt.Compact(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	in := m.createInputs()
	today := time.Now().In(loc).Format("Mon Jan 2 2006")
	if len(in) != 1 || !strings.HasPrefix(in[0], "Today is "+today+".\n\nPrevious summary:") {
		t.Fatalf("the summariser's input doesn't start with today's date:\n%v", in)
	}
	for _, want := range []string{"## Open threads", "dated past fact", "Quote word for word", "never invent one"} {
		if !strings.Contains(compactPrompt, want) {
			t.Errorf("the summary prompt lacks %q", want)
		}
	}
	for _, gone := range []string{"## In progress", "forgotten for good", "about to be deleted"} {
		if strings.Contains(compactPrompt, gone) {
			t.Errorf("the summary prompt still says %q", gone)
		}
	}
	input, err := e.rt.buildInput(e.ctx, uid)
	if err != nil || len(input) == 0 {
		t.Fatal(err)
	}
	var first struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(input[0], &first)
	if len(first.Content) == 0 || !strings.HasPrefix(first.Content[0].Text, summaryHeader+"\n\n## Open threads") {
		t.Fatalf("the summary isn't introduced as background: %s", input[0])
	}
	for _, want := range []string{"It's background", "Don't redo anything it lists as done", `"stop"`, "memory of the user still holds", "tools work as usual", "recall"} {
		if !strings.Contains(summaryHeader, want) {
			t.Errorf("the summary header lacks %q", want)
		}
	}
}

// A tool call the output limit cut off doesn't run; the model is told why
// and writes it again.
func TestCutOffCallNotRun(t *testing.T) {
	e := newProgressEnv(t)
	m := withFakeModel(e)
	ran := false
	e.fakeTool("write_file", func(map[string]any) (*toolResult, error) { ran = true; return &toolResult{Text: "Wrote a.md"}, nil })
	cut := map[string]any{"type": "function_call", "call_id": "call_cut", "name": "write_file", "arguments": `{"path":"a.md","content":"# Plan\nDay 1`}
	m.onStream(func(w http.ResponseWriter, r *http.Request) {
		respond(w, "incomplete", 4000, fc("say", map[string]any{"text": "Writing it now."}), cut)
	}, says(fc("say", map[string]any{"text": "Done.", "final": true})))
	e.send("write the plan to a.md")
	e.settle()
	if ran {
		t.Fatal("the cut-off call ran")
	}
	calls := m.streamCalls()
	if len(calls) != 2 {
		t.Fatalf("%d model calls, want 2", len(calls))
	}
	if in := inputOf(&calls[1]); !strings.Contains(in, `"call_id":"call_cut","output":"Not run: your reply hit the output limit`) {
		t.Fatalf("the model isn't told the call was cut off:\n%s", tailOf(in, 800))
	}
	// A whole call in a cut-off response still runs.
	if says := entriesOf(e.feed(), "say"); len(says) != 2 || says[0].Text != "Writing it now." {
		t.Fatalf("says = %v", says)
	}
}
