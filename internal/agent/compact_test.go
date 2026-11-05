package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// tooLongOver answers with the API's "context too long" while the input is
// over limit bytes, and with a final say once it fits.
func tooLongOver(limit int) modelTurn {
	return func(req *llm.Request) []any {
		if len(inputOf(req)) > limit {
			return []any{sse{"type": "error", "code": "context_length_exceeded", "message": "Your input exceeds the context window of this model."}}
		}
		return []any{fc("say", map[string]any{"text": "Done.", "final": true})}
	}
}

func failuresOf(feed []*store.Entry) []Failure {
	var out []Failure
	for _, x := range entriesOf(feed, "error") {
		var d struct{ Failure Failure }
		_ = json.Unmarshal(x.Data, &d)
		out = append(out, d.Failure)
	}
	return out
}

// A message that alone is too long for the model (a huge paste) is folded
// into the summary once nothing else is left to fold: kept, it overflows
// every call from then on, since only an answer lets it be folded (LLM-1).
func TestOverflowFromUnansweredMessage(t *testing.T) {
	t.Run("over the budget", func(t *testing.T) {
		e := newProgressEnv(t)
		e.cfg.CompactTokens = 4000
		e.model.script(tooLongOver(12_000), tooLongOver(12_000), tooLongOver(12_000))
		e.send("hi")
		e.settle()
		e.send("Look at this log: " + strings.Repeat("lorem ipsum ", 5000))
		e.settle()
		if fs := failuresOf(e.feed()); len(fs) != 0 {
			t.Fatalf("failed: %+v", fs)
		}
		if says := entriesOf(e.feed(), "say"); len(says) != 2 {
			t.Fatalf("%d answers, want 2", len(says))
		}
		in := inputOf(e.model.requests()[2])
		if !strings.Contains(in, "folded into the summary") || !strings.Contains(in, "Look at this log: lorem ipsum") {
			t.Errorf("Alexa isn't told the message was folded, or its words aren't quoted:\n%s", tailOf(in, 1500))
		}
	})
	t.Run("under the budget", func(t *testing.T) {
		// It fits the budget, so the first overflow folds only what's
		// older. That isn't enough: the run fails, Try again is offered,
		// and it folds the rest.
		e := newProgressEnv(t)
		e.model.script(tooLongOver(6000), tooLongOver(6000), tooLongOver(6000), tooLongOver(6000), tooLongOver(6000))
		e.send("hi")
		e.settle()
		e.send("Look at this log: " + strings.Repeat("lorem ipsum ", 700))
		e.settle()
		fs := failuresOf(e.feed())
		if len(fs) != 1 || fs[0].Reason != llm.ReasonContextOverflow || !fs[0].Retryable {
			t.Fatalf("failures = %+v, want one context_overflow that can be tried again", fs)
		}
		if err := e.rt.Retry(e.ctx, e.user); err != nil {
			t.Fatal(err)
		}
		e.settle()
		if fs := failuresOf(e.feed()); len(fs) != 1 {
			t.Errorf("Try again failed too: %+v", fs)
		}
		if says := entriesOf(e.feed(), "say"); len(says) != 2 {
			t.Errorf("%d answers, want 2", len(says))
		}
	})
}

// The loop compacts for exactly what llm.Classify calls too long: a 413,
// but not a gateway's "context deadline exceeded" (LLM-3).
func TestIsContextOverflowMatchesClassify(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"window", &llm.Error{Reason: llm.ReasonContextOverflow, Err: &llm.APIError{Status: 400, Code: "context_length_exceeded"}}, true},
		{"413", &llm.APIError{Status: 413, Message: "Request Entity Too Large"}, true},
		{"gateway timeout", &llm.Error{Reason: llm.ReasonUnavailable, Retryable: true, Err: &llm.APIError{Status: 504, Message: "upstream: context deadline exceeded"}}, false},
		{"streamed timeout", &llm.APIError{Status: 500, Code: "server_error", Message: "upstream: context deadline exceeded"}, false},
	} {
		if got := isContextOverflow(c.err); got != c.want {
			t.Errorf("%s: isContextOverflow = %v, want %v", c.name, got, c.want)
		}
	}
}

// A server timeout fails the run without folding the conversation away.
func TestTimeoutDoesNotCompact(t *testing.T) {
	e := newProgressEnv(t)
	e.rt.llm.Attempts = 1
	timeout := sse{"type": "error", "code": "server_error", "message": "upstream: context deadline exceeded"}
	e.model.script(does(fc("say", map[string]any{"text": "Hello.", "final": true})), does(timeout), does(timeout))
	e.send("hi")
	e.settle()
	e.send("and again")
	e.settle()
	ss, _ := e.st.GetSession(e.ctx, e.user.ID)
	if ss.Summary != "" || ss.CompactedThrough != 0 {
		t.Errorf("a server timeout compacted the conversation (through %d)", ss.CompactedThrough)
	}
	if fs := failuresOf(e.feed()); len(fs) != 1 || fs[0].Reason != llm.ReasonUnavailable {
		t.Errorf("failures = %+v, want model_unavailable", fs)
	}
}
