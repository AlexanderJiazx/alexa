package agent

import (
	"strings"
	"testing"
	"time"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

func TestCleanText(t *testing.T) {
	in := "Price: $$ / $11-30 ($$)【768110726327115208†L5-L8】, rating 4.1【1†L1】."
	want := "Price: $$ / $11-30 ($$), rating 4.1."
	if got := cleanText(in); got != want {
		t.Fatalf("cleanText = %q, want %q", got, want)
	}
}

func TestDropTitle(t *testing.T) {
	for in, want := range map[string]string{
		"# Québec City, Nov 13–16\n\n## Weather\nCold.": "## Weather\nCold.",
		"## Weather\nCold.":            "## Weather\nCold.",
		"Intro line.\n# Heading later": "Intro line.\n# Heading later",
		"#hashtag first":               "#hashtag first",
	} {
		if got := dropTitle(in); got != want {
			t.Errorf("dropTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClip(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"Short and sweet.", 300, "Short and sweet."},
		{"Lots of\n\n  space   here.", 300, "Lots of space here."},
		{"First sentence here. Second sentence is longer and goes on. Third.", 40, "First sentence here."},
		{"One clause runs on, then another clause keeps going without a stop", 40, "One clause runs on…"},
		{"word word word word word word word word", 20, "word word word word…"},
	} {
		if got := clip(c.in, c.n); got != c.want {
			t.Errorf("clip(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
		if got := clip(c.in, c.n); len([]rune(got)) > c.n {
			t.Errorf("clip(%q, %d) is %d runes long", c.in, c.n, len([]rune(got)))
		}
	}
}

// Conversation items for the compaction tests.
func userMsg() storedItem {
	return storedItem{Kind: "user", Item: marshal(map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": strings.Repeat("u", 400)}}})}
}
func modelMsg() storedItem {
	return storedItem{Item: marshal(map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": strings.Repeat("m", 400)}}})}
}
func reasoning() storedItem {
	return storedItem{Item: marshal(map[string]any{"type": "reasoning", "encrypted_content": strings.Repeat("r", 400)})}
}
func call(id string) storedItem {
	return storedItem{Item: marshal(map[string]any{"type": "function_call", "call_id": id, "name": "browser_open", "arguments": `{"url":"` + strings.Repeat("a", 380) + `"}`})}
}
func output(id string) storedItem {
	return storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": id, "output": strings.Repeat("o", 400)})}
}

func TestChooseCut(t *testing.T) {
	for _, c := range []struct {
		name  string
		items []storedItem
		keep  int
		want  int
	}{
		{"everything answered folds up to the last safe point", []storedItem{userMsg(), call("1"), output("1"), userMsg(), modelMsg()}, 0, 4},
		{"never folds the message being worked on", []storedItem{userMsg(), modelMsg(), userMsg()}, 0, 2},
		{"nothing but unanswered messages", []storedItem{userMsg(), userMsg()}, 0, 0},
		{"a lone new message stays", []storedItem{userMsg()}, 0, 0},
		{"keeps a call with its output", []storedItem{userMsg(), call("1"), output("1"), call("2"), output("2")}, 0, 3},
		{"keeps reasoning with its call", []storedItem{userMsg(), reasoning(), call("1"), output("1")}, 0, 1},
		{"keeps the tail that fits", []storedItem{userMsg(), modelMsg(), userMsg(), modelMsg(), userMsg(), modelMsg()}, 250, 4},
		{"keeps everything that fits", []storedItem{userMsg(), call("1"), output("1")}, 100000, 1},
	} {
		if got := chooseCut(c.items, c.keep, 0); got != c.want {
			t.Errorf("%s: chooseCut = %d, want %d", c.name, got, c.want)
		}
	}
	// Real token counts calibrate the estimates: when the prompt was really
	// four times bigger than estimated, the kept tail shrinks to match.
	items := []storedItem{userMsg(), modelMsg(), userMsg(), modelMsg(), userMsg(), modelMsg()}
	total := 0
	for _, si := range items {
		total += estimate(si)
	}
	if got := chooseCut(items, 250, int64(total*4)); got != 5 {
		t.Errorf("calibrated chooseCut = %d, want 5", got)
	}
}

func TestRenderItem(t *testing.T) {
	say := storedItem{Item: marshal(map[string]any{"type": "function_call", "call_id": "1", "name": "say", "arguments": `{"text":"Booked for 7pm."}`})}
	if got := renderItem(say); got != "ALEXA said: Booked for 7pm." {
		t.Errorf("render say = %q", got)
	}
	u := storedItem{Kind: "user", Item: marshal(map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "[Mon] book it"}}}),
		Attach: []Attachment{{Name: "a.pdf", Path: "uploads/a.pdf"}}}
	if got := renderItem(u); got != "USER: [Mon] book it [attached a.pdf at uploads/a.pdf]" {
		t.Errorf("render user = %q", got)
	}
	if got := renderItem(reasoning()); got != "" {
		t.Errorf("reasoning should not be rendered, got %q", got)
	}
}

func TestIsContextOverflow(t *testing.T) {
	for msg, want := range map[string]bool{
		"This model's maximum context length is 128000 tokens": true,
		"Input exceeds the context window":                     true,
		"prompt is too long":                                   true,
		"rate limit exceeded":                                  false,
		"invalid tool schema":                                  false,
	} {
		if got := isContextOverflow(&llm.APIError{Status: 400, Message: msg}); got != want {
			t.Errorf("isContextOverflow(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestCronDays(t *testing.T) {
	for spec, warn := range map[string]bool{
		"0 18 8-14 * 2": true,
		"0 18 * * 2":    false,
		"0 9 1 * *":     false,
		"0 9 * * 1-5":   false,
		"@daily":        false,
	} {
		if got := cronDayWarning(spec) != ""; got != warn {
			t.Errorf("cronDayWarning(%q) warns = %v, want %v", spec, got, warn)
		}
	}
	// What the warning is about: with both set, cron also runs on days
	// that aren't Tuesdays.
	loc, _ := time.LoadLocation("America/Toronto")
	runs := (&Scheduler{}).NextRuns(&store.Schedule{Cron: "0 18 8-14 * 2", Enabled: true}, loc, 10)
	if len(runs) != 10 {
		t.Fatalf("NextRuns gave %d runs, want 10", len(runs))
	}
	other := false
	for i, r := range runs {
		if i > 0 && !r.After(runs[i-1]) {
			t.Fatalf("runs out of order: %v", runs)
		}
		if r.In(loc).Weekday() != time.Tuesday {
			other = true
		}
	}
	if !other {
		t.Errorf("expected some runs on days other than Tuesday: %v", runs)
	}
	once := (&Scheduler{}).NextRuns(&store.Schedule{RunAt: time.Now().Add(time.Hour).UnixMilli(), Enabled: true}, loc, 4)
	if len(once) != 1 {
		t.Errorf("a one-off schedule should have one run, got %d", len(once))
	}
}
