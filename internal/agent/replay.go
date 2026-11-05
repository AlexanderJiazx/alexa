package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"alexagent/internal/llm"
	"alexagent/internal/userenv"
)

// Old page states, screenshots and long outputs are pruned from the replay
// with a ratchet (M5): they pile up to a high mark, then all are cut back to
// their low marks in one go, and where the cut fell is kept (pruneMarks). Between
// cuts nothing already replayed changes, so the model service's cache of
// the prompt so far keeps matching; pruning a little every turn would
// rewrite a recent item each time and send the rest uncached. Long outputs
// are measured by size, not count (M6): a few huge pages crowd out more
// than many short ones.
const (
	statesHigh = 10 // full browser states, at most
	statesLow  = 4  // left after a cut
	shotsHigh  = 6  // screenshots
	shotsLow   = 2
	longHigh   = 100_000 // characters of full long outputs
	longLow    = 40_000
	longCut    = 1500 // what's left of a pruned long output
)

// pruneMarks says how far pruning has reached: items with ids up to each
// mark are replayed pruned. Marks only move forward.
type pruneMarks struct {
	States int64 `json:"states"`
	Shots  int64 `json:"shots"`
	Longs  int64 `json:"longs"`
	// Model is the model the replayed reasoning comes from, and Reasoning
	// the last item from before it took over. Encrypted reasoning only
	// opens for the model that wrote it: replayed to another one, every
	// call would be refused (L12), so older reasoning is left out.
	Model     string `json:"model,omitempty"`
	Reasoning int64  `json:"reasoning,omitempty"`
}

func pruneKey(userID int64) string { return fmt.Sprintf("prune:%d", userID) }

func (rt *Runtime) loadMarks(ctx context.Context, userID int64) pruneMarks {
	var m pruneMarks
	if v, err := rt.st.GetKV(ctx, pruneKey(userID)); err == nil {
		_ = json.Unmarshal([]byte(v), &m)
	}
	return m
}

// ratchet moves the marks forward when what's replayed in full has passed
// a high mark. ids are the items' ids, oldest first.
func ratchet(m pruneMarks, items []storedItem, ids []int64) pruneMarks {
	var states, shots []int64
	type long struct {
		id   int64
		size int
	}
	var longs []long
	longTotal := 0
	for i := len(items) - 1; i >= 0; i-- {
		si, id := items[i], ids[i]
		if llm.ParseItem(si.Item).Type != "function_call_output" {
			continue
		}
		if si.Kind == "browser_state" && si.Short != "" && id > m.States {
			states = append(states, id)
		}
		if si.Shot != "" && id > m.Shots && (si.Kind != "browser_state" || si.Short == "" || id > m.States) {
			shots = append(shots, id)
		}
		if si.Kind == "long" && id > m.Longs {
			if n := len(outputText(si)); n > longCut {
				longs = append(longs, long{id, n})
				longTotal += n
			}
		}
	}
	if len(states) <= statesHigh && len(shots) <= shotsHigh && longTotal <= longHigh {
		return m
	}
	// One of them is over its high mark. The cut breaks the cached prefix
	// anyway, so all three are cut back to their low marks at once, and the
	// next cut is as far off as it can be.
	if len(states) > statesLow {
		m.States = states[statesLow]
	}
	if len(shots) > shotsLow {
		m.Shots = shots[shotsLow]
	}
	kept := 0
	for i, l := range longs {
		if i > 0 && kept+l.size > longLow {
			m.Longs = l.id
			break
		}
		kept += l.size
	}
	return m
}

// newModel moves the reasoning mark to the end of the conversation when
// model isn't the one its reasoning came from. A conversation from before
// the model was recorded is taken to be the current model's.
func newModel(m pruneMarks, model string, ids []int64) pruneMarks {
	if m.Model == model {
		return m
	}
	if m.Model != "" && len(ids) > 0 {
		m.Reasoning = ids[len(ids)-1]
	}
	m.Model = model
	return m
}

// forgetReasoning leaves every reasoning item so far out of the replay from
// now on: the model refused it as not its own, whatever the marks said.
func (rt *Runtime) forgetReasoning(ctx context.Context, userID int64) error {
	_, rows, err := rt.st.Conversation(ctx, userID)
	if err != nil || len(rows) == 0 {
		return err
	}
	m := rt.loadMarks(ctx, userID)
	m.Reasoning = max(m.Reasoning, rows[len(rows)-1].ID)
	return rt.st.SetKV(ctx, pruneKey(userID), string(marshal(m)))
}

// withoutReasoning is input with its reasoning items left out.
func withoutReasoning(input []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(input))
	for _, raw := range input {
		if llm.ParseItem(raw).Type != "reasoning" {
			out = append(out, raw)
		}
	}
	return out
}

// outputText is a function call output's text.
func outputText(si storedItem) string {
	var o struct {
		Output string `json:"output"`
	}
	_ = json.Unmarshal(si.Item, &o)
	return o.Output
}

// buildInput replays the conversation since the last compaction (after its
// summary), with old screenshots and page dumps pruned and reasoning from
// another model left out.
func (rt *Runtime) buildInput(ctx context.Context, userID int64) ([]json.RawMessage, error) {
	ss, rows, err := rt.st.Conversation(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]storedItem, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		var si storedItem
		if err := json.Unmarshal(r.JSON, &si); err != nil || len(si.Item) == 0 {
			continue
		}
		items = append(items, si)
		ids = append(ids, r.ID)
	}
	// Where each function call's output is (calls without one get a synthetic one).
	outputAt := map[string]int{}
	for i, si := range items {
		it := llm.ParseItem(si.Item)
		if it.Type == "function_call_output" {
			outputAt[it.CallID] = i
		}
	}
	saved := rt.loadMarks(ctx, userID)
	marks := saved
	if n := len(rows); n > 0 {
		if last := rows[n-1].ID; marks.States > last || marks.Shots > last || marks.Longs > last || marks.Reasoning > last {
			marks = pruneMarks{Model: marks.Model} // from a conversation that's gone
		}
	}
	marks = ratchet(marks, items, ids)
	if rt.cfg != nil {
		marks = newModel(marks, rt.cfg.Model, ids)
	}
	if marks != saved {
		_ = rt.st.SetKV(ctx, pruneKey(userID), string(marshal(marks)))
	}
	env := rt.envs.For(userID)
	out := make([]json.RawMessage, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		si, id := items[i], ids[i]
		it := llm.ParseItem(si.Item)
		switch {
		case it.Type == "reasoning" && id <= marks.Reasoning:
			// Another model's: left out (out[i] stays nil).
		case it.Type == "function_call_output":
			var o struct {
				CallID string `json:"call_id"`
				Output string `json:"output"`
			}
			_ = json.Unmarshal(si.Item, &o)
			text := o.Output
			stateCut := si.Kind == "browser_state" && si.Short != "" && id <= marks.States
			switch {
			case stateCut:
				text = si.Short
			case si.Kind == "long" && id <= marks.Longs && len(text) > longCut:
				text = cutLong(text)
			}
			var output any = text
			if si.Shot != "" && id > marks.Shots && !stateCut {
				if img, err := os.ReadFile(filepath.Join(env.Shots, si.Shot)); err == nil {
					output = []map[string]any{
						{"type": "input_text", "text": text},
						{"type": "input_image", "image_url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)},
					}
				}
			}
			out[i] = marshal(map[string]any{"type": "function_call_output", "call_id": o.CallID, "output": output})
		case isMessage(si.Kind):
			out[i] = rt.userItem(ctx, env, si)
		default:
			out[i] = si.Item
		}
	}
	final := make([]json.RawMessage, 0, len(out)+3)
	if ss.Summary != "" {
		final = append(final, marshal(map[string]any{"role": "user", "content": []map[string]any{{"type": "input_text",
			"text": summaryHeader + "\n\n" + ss.Summary}}})) // compact.go
	}
	// A turn can put a message between its tool calls (call, message,
	// call), but the API refuses a message between a call and its output:
	// those outputs go before the message, as if the calls ran one at a
	// time. So do they before a user's message stored between them (a note
	// added while a question waited). Nothing else moves, so the prompt
	// cache still matches.
	placed := map[int]bool{}
	var waiting []int // outputs, further on, of calls already replayed
	for i, raw := range out {
		if placed[i] || raw == nil {
			continue
		}
		it := llm.ParseItem(items[i].Item)
		if it.Type == "message" || isMessage(items[i].Kind) {
			for _, j := range waiting {
				if !placed[j] {
					final = append(final, out[j])
					placed[j] = true
				}
			}
			waiting = waiting[:0]
		}
		final = append(final, raw)
		placed[i] = true
		if it.Type != "function_call" {
			continue
		}
		if j, ok := outputAt[it.CallID]; !ok {
			// Insert a synthetic output for an unanswered call (e.g. after a crash).
			final = append(final, marshal(map[string]any{"type": "function_call_output", "call_id": it.CallID, "output": "Interrupted before this tool call completed; check the current state and retry if needed."}))
		} else if j > i {
			waiting = append(waiting, j)
		}
	}
	return final, nil
}

// cutLong keeps the start of an older long output, on a rune boundary.
func cutLong(text string) string {
	cut := longCut
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "\n[…the rest of this older output was dropped to save space; run the tool again if you need it]"
}

// userItem is a user message with its attachments: pictures as the JPEG
// made for the model, PDFs as documents, anything else (or a picture that
// couldn't be converted, or one set aside) described in text.
func (rt *Runtime) userItem(ctx context.Context, env *userenv.Env, si storedItem) json.RawMessage {
	var msg struct {
		Role    string           `json:"role"`
		Content []map[string]any `json:"content"`
	}
	_ = json.Unmarshal(si.Item, &msg)
	content := msg.Content
	for _, a := range si.Attach {
		content = append(content, attachmentContent(ctx, env, a)...)
	}
	return marshal(map[string]any{"role": "user", "content": content})
}
