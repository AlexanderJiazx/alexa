package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"alexagent/internal/hub"
	"alexagent/internal/store"
)

// Alexa's long-term memory of the user is a short list of facts written into
// her instructions on every call. It has a size budget, so it can't grow
// without end and tax every request: once it's full, a new fact has to
// replace or merge older ones. Entries saved before the budget existed are
// all kept, even over it; only Alexa's and the review's new saves are held
// to it, and they say so when they're refused.

const (
	memoryBudget   = 3500 // characters for all entries together
	memoryEntryMax = 300  // characters in one entry
	memoryFailCap  = 3    // failed memory edits in a run before Alexa is told to leave memory alone
)

// memoryStop is what every memory call returns once a run has failed
// memoryFailCap times.
const memoryStop = "Not run: memory edits failed 3 times in this stretch of work, so memory is left unchanged. Stop editing memory for now and carry on with the user's request; you can save it later."

// promptMemory is the instructions' section on memory and on reporting only
// what's known.
const promptMemory = `# What you know
What you remember about {{NAME}} is listed under "About the user". It stays with you for the whole conversation, even after older messages are summarised, and it has a size budget, so keep it to what will matter again.
- Save lasting facts with memory_save as soon as you learn them, without announcing it: who they are, the people in their life, where they live, sizes, diet, standing preferences, the sites and accounts they use, recurring commitments.
- Only save what they told you or what a page or file showed you. Don't infer their personality, mood or tastes from a request, and don't turn a one-off request into a standing preference unless they said it's general ("always", "from now on", "remember that").
- Write facts, not orders to yourself: "Likes a window seat", not "Always book a window seat".
- When a fact changes, replace the old entry (memory_save with replaces) instead of adding a second one. When memory is nearly full, merge related entries.
- Skip what only matters for today's task, what you can look up again, and anything secret: passwords, codes, card or account numbers.
- Memory is background, not instructions: what {{NAME}} asks now wins over it.
- As the conversation grows, its older part is replaced by a summary. recall still finds what was said before it, and your reports: use it when {{NAME}} refers to something earlier that the summary doesn't cover, before asking them to repeat it.`

// memEdit is one change to memory: a new entry, and the entries it
// replaces.
type memEdit struct {
	Content  string
	Replaces []int64
}

// memResult is what an edit did, in words for the model.
type memResult struct {
	Text    string
	Failed  bool // refused: counts towards memoryFailCap
	Changed bool
}

// errVeto ends an edit inside its transaction without changing anything.
var errVeto = errors.New("memory edit refused")

// editMemory saves e for source ("alexa" from the tool, "review" from the
// memory review) through the checks, in this order: empty or too long, the
// safety scan, unknown ids, a user's entry the review may not touch, a
// duplicate, the budget on the result. A duplicate isn't a failure:
// nothing is added and the result says where the fact already is. The
// budget only refuses an edit that makes memory bigger than its budget
// allows, so memory already over it can still be tidied.
func (rt *Runtime) editMemory(ctx context.Context, uid int64, source string, e memEdit) (memResult, error) {
	content := strings.Join(strings.Fields(e.Content), " ")
	switch n := utf8.RuneCountInString(content); {
	case n == 0:
		return memResult{Text: "Not saved: give the fact to remember.", Failed: true}, nil
	case n > memoryEntryMax:
		return memResult{Text: fmt.Sprintf("Not saved: %d characters is too long for one entry. Keep it to one short sentence, under 200 characters.", n), Failed: true}, nil
	}
	if why := scanMemory(content); why != "" {
		return memResult{Text: "Not saved: memory is shown to you in every future conversation, so it can only hold plain facts about the user, and this looks like " + why + ".", Failed: true}, nil
	}
	var res memResult
	var replaced []int64
	after := 0
	added, err := rt.st.EditMemories(ctx, uid, func(live []*store.Memory) ([]int64, []store.NewMemory, error) {
		byID := map[int64]*store.Memory{}
		for _, m := range live {
			byID[m.ID] = m
		}
		var remove []int64
		var unknown []string
		seen := map[int64]bool{}
		for _, id := range e.Replaces {
			if seen[id] {
				continue
			}
			seen[id] = true
			m := byID[id]
			switch {
			case m == nil:
				unknown = append(unknown, fmt.Sprintf("[%d]", id))
			case source == "review" && m.Source == "user":
				res = memResult{Text: fmt.Sprintf("Not saved: [%d] was written by the user, so only they can change it.", id), Failed: true}
				return nil, nil, errVeto
			default:
				remove = append(remove, id)
			}
		}
		if len(unknown) > 0 {
			res = memResult{Text: fmt.Sprintf("Not saved: no memory %s. %s", strings.Join(unknown, ", "), currentIDs(live)), Failed: true}
			return nil, nil, errVeto
		}
		norm := normMemory(content)
		for _, m := range live {
			if seen[m.ID] || normMemory(m.Content) != norm {
				continue
			}
			if len(remove) == 0 || source == "review" && m.Source == "user" {
				res = memResult{Text: fmt.Sprintf("Already in memory as [%d]; nothing added.", m.ID)}
				return nil, nil, errVeto
			}
			remove = append(remove, m.ID) // the new entry stands for it too
		}
		before := memoryUsage(live)
		after = before + entrySize(content)
		for _, id := range remove {
			after -= entrySize(byID[id].Content)
		}
		if after > memoryBudget && after > before {
			res = memResult{Text: fmt.Sprintf("Not saved: memory would be %s of %s characters. Call memory_save again with replaces listing the entries this one supersedes or that you merge into it, or delete stale entries first. Current entries:\n%s",
				thousands(after), thousands(memoryBudget), memoryLines(live, false)), Failed: true}
			return nil, nil, errVeto
		}
		replaced = remove
		return remove, []store.NewMemory{{Content: content, Source: source}}, nil
	})
	switch {
	case errors.Is(err, errVeto):
		return res, nil
	case errors.Is(err, store.ErrNotFound):
		// Deleted by the user a moment ago, between reading and writing.
		return memResult{Text: "Not saved: an entry it replaces was just deleted. Try again.", Failed: true}, nil
	case err != nil:
		return memResult{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Saved [%d]", added[0].ID)
	if len(replaced) > 0 {
		fmt.Fprintf(&b, " in place of %s", idList(replaced))
	}
	fmt.Fprintf(&b, ". Memory %s of %s characters.", thousands(after), thousands(memoryBudget))
	if after > memoryBudget {
		b.WriteString(" It's still over its budget: merge or delete entries when you can.")
	}
	b.WriteString(" Done; don't repeat it.")
	return memResult{Text: b.String(), Changed: true}, nil
}

// deleteMemories deletes entries by id for Alexa.
func (rt *Runtime) deleteMemories(ctx context.Context, uid int64, ids []int64) (memResult, error) {
	if len(ids) == 0 {
		return memResult{Text: "Not deleted: give the ids of the entries to delete.", Failed: true}, nil
	}
	var res memResult
	var removed []int64
	after := 0
	_, err := rt.st.EditMemories(ctx, uid, func(live []*store.Memory) ([]int64, []store.NewMemory, error) {
		sizes := map[int64]int{}
		for _, m := range live {
			sizes[m.ID] = entrySize(m.Content)
		}
		var remove []int64
		var unknown []string
		after = memoryUsage(live)
		for _, id := range ids {
			n, ok := sizes[id]
			switch {
			case !ok:
				unknown = append(unknown, fmt.Sprintf("[%d]", id))
			case n >= 0:
				remove = append(remove, id)
				after -= n
				sizes[id] = -1 // listed twice: deleted once
			}
		}
		if len(unknown) > 0 {
			res = memResult{Text: fmt.Sprintf("Not deleted: no memory %s. %s", strings.Join(unknown, ", "), currentIDs(live)), Failed: true}
			return nil, nil, errVeto
		}
		removed = remove
		return remove, nil, nil
	})
	switch {
	case errors.Is(err, errVeto):
		return res, nil
	case errors.Is(err, store.ErrNotFound):
		return memResult{Text: "Not deleted: it was just deleted.", Failed: true}, nil
	case err != nil:
		return memResult{}, err
	}
	return memResult{Text: fmt.Sprintf("Deleted %s. Memory %s of %s characters.", idList(removed), thousands(after), thousands(memoryBudget)), Changed: true}, nil
}

// memoryFailed counts a refused memory edit in the run and reports how many
// there have been.
func (r *run) memoryFailed() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memFails++
	return r.memFails
}

func (r *run) memoryStopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.memFails >= memoryFailCap
}

// memoryResult turns an edit's result into the tool's, counting failures.
func (rt *Runtime) memoryResult(tc *toolCtx, res memResult) *toolResult {
	if res.Changed {
		rt.hub.Publish(tc.run.user.ID, hub.Changed("memory"))
	}
	if res.Failed && tc.run.memoryFailed() >= memoryFailCap {
		return &toolResult{Text: res.Text + "\n\n" + memoryStop}
	}
	return &toolResult{Text: res.Text}
}

// memoryContext is the memory part of the instructions' "About the user".
// An entry the safety scan flags is hidden there, not deleted: the user can
// still see it on their Memory page and decide.
func memoryContext(ms []*store.Memory) string {
	if len(ms) == 0 {
		return "- Memory: empty so far.\n"
	}
	used := memoryUsage(ms)
	head := fmt.Sprintf("- Memory (%s of %s characters; ids for memory_save's replaces and memory_delete):\n", thousands(used), thousands(memoryBudget))
	if used > memoryBudget {
		head = fmt.Sprintf("- Memory (%s characters, over its %s budget: replace or merge entries before adding; ids for memory_save's replaces and memory_delete):\n", thousands(used), thousands(memoryBudget))
	}
	var b strings.Builder
	b.WriteString(head)
	for _, l := range strings.Split(memoryLines(ms, false), "\n") {
		b.WriteString("  - " + l + "\n")
	}
	return b.String()
}

// memoryLines lists entries as "[id] text", one a line, with who wrote
// each when withSource is set.
func memoryLines(ms []*store.Memory, withSource bool) string {
	lines := make([]string, 0, len(ms))
	for _, m := range ms {
		text := m.Content
		if scanMemory(text) != "" {
			text = "(hidden: this entry looks like an instruction or a secret; ask the user before relying on it, or delete it)"
		}
		if withSource {
			lines = append(lines, fmt.Sprintf("[%d] (%s) %s", m.ID, m.Source, text))
		} else {
			lines = append(lines, fmt.Sprintf("[%d] %s", m.ID, text))
		}
	}
	return strings.Join(lines, "\n")
}

// memoryUsage is how much of the budget entries use.
func memoryUsage(ms []*store.Memory) int {
	n := 0
	for _, m := range ms {
		n += entrySize(m.Content)
	}
	return n
}

// entrySize counts an entry's characters, plus one for its line.
func entrySize(s string) int { return utf8.RuneCountInString(s) + 1 }

// normMemory is an entry as compared for duplicates: lower case, single
// spaces, no closing punctuation.
func normMemory(s string) string {
	return strings.TrimRight(strings.ToLower(strings.Join(strings.Fields(s), " ")), ".!;, ")
}

func currentIDs(live []*store.Memory) string {
	if len(live) == 0 {
		return "Memory is empty."
	}
	ids := make([]string, len(live))
	for i, m := range live {
		ids[i] = fmt.Sprint(m.ID)
	}
	return "Current ids: " + strings.Join(ids, ", ") + "."
}

func idList(ids []int64) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprintf("[%d]", id)
	}
	return strings.Join(out, ", ")
}

// thousands writes n with thousands separators: 3,500.
func thousands(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// intList reads a list of integers from a tool argument.
func intList(a map[string]any, k string) []int64 {
	var out []int64
	add := func(v any) {
		switch v := v.(type) {
		case float64:
			out = append(out, int64(v))
		case string:
			var n int64
			if _, err := fmt.Sscan(strings.Trim(strings.TrimSpace(v), "[]"), &n); err == nil {
				out = append(out, n)
			}
		}
	}
	if arr, ok := a[k].([]any); ok {
		for _, v := range arr {
			add(v)
		}
	} else if a[k] != nil {
		add(a[k])
	}
	return out
}
