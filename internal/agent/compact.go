package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// The session never ends, so its context can't grow forever. When a prompt
// gets bigger than cfg.CompactTokens, the older part of the conversation is
// folded into a running summary and dropped from what's replayed; the most
// recent stretch is kept verbatim.

const compactPrompt = `You keep the working memory of Alexa, a personal agent that has one continuous, never-ending conversation with its user. The older part of that conversation is about to be deleted and replaced by your summary, so anything you leave out is forgotten for good. (Alexa's separate long-term memory list survives on its own.)

Write the new summary by merging the previous summary, if there is one, with the conversation below. Use these sections:

## In progress
What the user currently wants, jobs underway, promises Alexa made, follow-ups due (with dates and times), questions waiting for an answer.

## Done
What was completed, with the concrete results: numbers, names, dates, prices, confirmation codes, links, file paths, report ids and titles.

## Context
Details needed to carry on: sites and accounts involved, where things stand in the browser, decisions and preferences the user expressed, things not to repeat.

Be specific and factual. Skip pleasantries and dead ends that no longer matter, drop what's obsolete, and keep recent things in the most detail. Write at most about 1,200 words. Reply with the summary only.`

// compact folds old conversation into the summary when the context is over
// budget (or always, with force). It returns nil if there was nothing to do.
func (rt *Runtime) compact(ctx context.Context, u *store.User, force bool) error {
	ss, err := rt.st.GetSession(ctx, u.ID)
	if err != nil {
		return err
	}
	budget := int64(rt.cfg.CompactTokens)
	if !force && ss.ContextTokens < budget {
		return nil
	}
	rows, err := rt.st.Items(ctx, u.ID, ss.CompactedThrough)
	if err != nil {
		return err
	}
	items := make([]storedItem, len(rows))
	for i, r := range rows {
		_ = json.Unmarshal(r.JSON, &items[i])
	}
	keep := budget / 4
	if force {
		// On demand or after an overflow: fold at least three quarters.
		size := ss.ContextTokens
		if size == 0 {
			// Not measured since the last compaction; estimate it.
			for _, si := range items {
				size += int64(estimate(si))
			}
		}
		keep = min(keep, size/4)
	}
	cut := chooseCut(items, int(keep), ss.ContextTokens)
	if cut == 0 {
		return nil
	}
	_ = rt.st.SetSessionActivity(ctx, u.ID, "Tidying up my notes")
	rt.publishState(u.ID)
	t0 := time.Now()
	summary := ss.Summary
	for _, chunk := range transcriptChunks(items[:cut], 150_000) {
		input := "Previous summary:\n" + orNone(summary) + "\n\nConversation to fold in:\n" + chunk
		msg, _ := json.Marshal(map[string]any{"role": "user", "content": input})
		resp, err := rt.llm.Create(ctx, &llm.Request{
			Model:        rt.cfg.Model,
			Instructions: compactPrompt,
			Input:        []json.RawMessage{msg},
			Reasoning:    &llm.Reasoning{Effort: "low"},
		})
		if err != nil {
			return fmt.Errorf("summarise: %w", err)
		}
		var b strings.Builder
		for _, raw := range resp.Output {
			if it := llm.ParseItem(raw); it.Type == "message" {
				b.WriteString(it.Text())
			}
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			summary = s
		} else {
			return errors.New("summarise: empty summary")
		}
	}
	if err := rt.st.CompactSession(ctx, u.ID, summary, rows[cut-1].ID); err != nil {
		return err
	}
	// The cached prompt is gone anyway; catch up on memories saved since.
	rt.MemoryChanged(u.ID)
	slog.Info("compacted session", "user", u.ID, "folded", cut, "kept", len(items)-cut, "context_was", ss.ContextTokens,
		"summary_chars", len(summary), "took", time.Since(t0).Round(time.Millisecond))
	return nil
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none yet)"
	}
	return s
}

// chooseCut picks how many leading items to fold into the summary: as many
// as needed for the rest to fit in keep tokens, cutting only where no tool
// call is separated from its output, and never folding away the messages
// Alexa hasn't responded to yet. contextTokens (the real size of the last
// prompt, if known) calibrates the size estimates.
func chooseCut(items []storedItem, keep int, contextTokens int64) int {
	n := len(items)
	if n == 0 {
		return 0
	}
	if contextTokens > 0 {
		total := 0
		for _, si := range items {
			total += estimate(si)
		}
		keep = int(float64(keep) * float64(total) / float64(contextTokens))
	}
	// Messages after the last model output are the ones being worked on.
	limit := n
	for i := n - 1; i >= 0 && isMessage(items[i].Kind); i-- {
		limit = i
	}
	cut, tail := 0, 0
	for i := n - 1; i >= 1; i-- {
		tail += estimate(items[i])
		if tail > keep && cut > 0 {
			break
		}
		if i <= limit && safeCut(items, i) {
			cut = i
		}
	}
	if cut == 0 && limit == n {
		cut = n // everything has been answered; fold it all
	}
	return cut
}

// safeCut reports whether the conversation can start at items[i].
func safeCut(items []storedItem, i int) bool {
	if isMessage(items[i].Kind) {
		return true
	}
	cur, prev := llm.ParseItem(items[i].Item), llm.ParseItem(items[i-1].Item)
	return cur.Type != "function_call_output" && (prev.Type == "function_call_output" || isMessage(items[i-1].Kind))
}

// estimate guesses an item's size in tokens as it would be replayed.
func estimate(si storedItem) int {
	n := len(si.Item) / 4
	if si.Kind == "browser_state" || si.Kind == "long" {
		n = min(n, 2000)
	}
	if si.Shot != "" {
		n += 800
	}
	if llm.ParseItem(si.Item).Type == "reasoning" {
		n = min(n, 1500) // encrypted reasoning is mostly ciphertext
	}
	return n
}

// transcriptChunks renders items as a readable transcript for the
// summariser, split into chunks of at most max characters.
func transcriptChunks(items []storedItem, max int) []string {
	var chunks []string
	var b strings.Builder
	for _, si := range items {
		line := renderItem(si)
		if line == "" {
			continue
		}
		if b.Len()+len(line) > max && b.Len() > 0 {
			chunks = append(chunks, b.String())
			b.Reset()
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if b.Len() > 0 {
		chunks = append(chunks, b.String())
	}
	return chunks
}

func renderItem(si storedItem) string {
	it := llm.ParseItem(si.Item)
	switch {
	case isMessage(si.Kind):
		var msg struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		_ = json.Unmarshal(si.Item, &msg)
		var parts []string
		for _, c := range msg.Content {
			parts = append(parts, c.Text)
		}
		for _, a := range si.Attach {
			parts = append(parts, fmt.Sprintf("[attached %s at %s]", a.Name, a.Path))
		}
		who := "USER"
		if si.Kind == "system" {
			who = "NOTE"
		} else if si.Kind == "schedule" {
			who = "SCHEDULED"
		}
		return who + ": " + trim(strings.Join(parts, " "), 4000)
	case it.Type == "message":
		if t := strings.TrimSpace(it.Text()); t != "" {
			return "ALEXA (private note, not shown): " + trim(t, 800)
		}
	case it.Type == "web_search_call" && it.Action != nil:
		q := it.Action.Query
		if q == "" && len(it.Action.Queries) > 0 {
			q = strings.Join(it.Action.Queries, " | ")
		}
		if q == "" {
			q = it.Action.URL
		}
		return "ALEXA searched the web: " + q
	case it.Type == "function_call":
		args := map[string]any{}
		_ = json.Unmarshal([]byte(it.Arguments), &args)
		switch it.Name {
		case "say":
			return "ALEXA said: " + str(args, "text")
		case "ask":
			return fmt.Sprintf("ALEXA asked: %s %v", str(args, "question"), strList(args, "options"))
		case "request_takeover":
			return "ALEXA asked the user to take over the browser: " + str(args, "reason")
		case "present_report":
			return fmt.Sprintf("ALEXA showed a report %q:\n%s", str(args, "title"), trim(str(args, "content"), 3000))
		case "memory_save":
			return "ALEXA saved to memory: " + str(args, "content")
		}
		delete(args, "status")
		delete(args, "done")
		b, _ := json.Marshal(args)
		return fmt.Sprintf("ALEXA called %s %s", it.Name, trim(string(b), 400))
	case it.Type == "function_call_output":
		var o struct {
			Output string `json:"output"`
		}
		_ = json.Unmarshal(si.Item, &o)
		text := o.Output
		if si.Kind == "browser_state" && si.Short != "" {
			text = si.Short
		}
		return "  → " + trim(strings.Join(strings.Fields(text), " "), 600)
	}
	return ""
}

// isContextOverflow recognises the API's "prompt too long" errors.
func isContextOverflow(err error) bool {
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	s := strings.ToLower(apiErr.Code + " " + apiErr.Message)
	if strings.Contains(s, "too many tokens") || strings.Contains(s, "prompt is too long") || strings.Contains(s, "context_length") {
		return true
	}
	return strings.Contains(s, "context") && (strings.Contains(s, "length") || strings.Contains(s, "window") ||
		strings.Contains(s, "exceed") || strings.Contains(s, "too long") || strings.Contains(s, "maximum"))
}
