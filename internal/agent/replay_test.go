package agent

import (
	"encoding/json"
	"testing"
)

// A user's message stored between a call and its output (a note added
// while a question waited) is replayed after the output: the model service
// refuses a message between them.
func TestReplayPutsTheOutputBeforeAMessage(t *testing.T) {
	e := newProgressEnv(t)
	items := []storedItem{userMsg(), call("plan"), call("ask"), output("plan"), {Item: json.RawMessage(`{"role":"user","content":[{"type":"input_text","text":"[Alexa system note] The conversation was just summarised."}]}`), Kind: "system"}, output("ask")}
	var raws []json.RawMessage
	for _, si := range items {
		raws = append(raws, marshal(si))
	}
	if err := e.st.AppendItems(e.ctx, e.user.ID, raws); err != nil {
		t.Fatal(err)
	}
	input, err := e.rt.buildInput(e.ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bad := callsInterrupted(input); bad != "" {
		t.Errorf("replayed %s", bad)
	}
	if len(input) != len(items) {
		t.Errorf("replayed %d items, want %d", len(input), len(items))
	}
}
