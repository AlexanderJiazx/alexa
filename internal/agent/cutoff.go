package agent

import (
	"context"
	"encoding/json"
	"strings"

	"alexagent/internal/llm"
)

// notRunCutOffText is the output of a tool call whose arguments the output
// limit cut off: run with what arrived, it would act on half an argument
// (a report without its end, a file cut short).
const notRunCutOffText = "Not run: your reply hit the output limit while writing this call, so its arguments were cut off. Write it again shorter, or split it (a long report or file in parts)."

// splitCutOff takes out of calls the ones a response cut off by the output
// limit left unfinished: those whose arguments aren't a whole JSON object.
func splitCutOff(resp *llm.Response, calls []llm.Item) (whole, cut []llm.Item) {
	if resp.Status != "incomplete" || resp.IncompleteDetails == nil || resp.IncompleteDetails.Reason != "max_output_tokens" {
		return calls, nil
	}
	for _, c := range calls {
		var args map[string]any
		if strings.TrimSpace(c.Arguments) == "" || json.Unmarshal([]byte(c.Arguments), &args) != nil {
			cut = append(cut, c)
		} else {
			whole = append(whole, c)
		}
	}
	return whole, cut
}

// notRunCutOff saves the outputs of the calls splitCutOff took out, after
// the batch's, as they came last.
func (rt *Runtime) notRunCutOff(ctx context.Context, userID int64, cut []llm.Item) error {
	for _, c := range cut {
		if err := rt.saveOutput(context.WithoutCancel(ctx), userID, c.CallID, &toolResult{Text: notRunCutOffText}); err != nil {
			return err
		}
	}
	return nil
}
