package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// The session never ends, so its context can't grow forever. When a prompt
// gets bigger than cfg.CompactTokens, the older part of the conversation is
// folded into a running summary and dropped from what's replayed; the most
// recent stretch is kept verbatim.

const compactPrompt = `You keep the working memory of Alexa, a personal agent that has one continuous, never-ending conversation with its user. The older part of that conversation is about to leave Alexa's view and be replaced by your summary. She can search old messages, but only when she knows something is there to look for, so what you leave out is as good as forgotten. (Alexa's separate long-term memory list survives on its own.)

Write the new summary by merging the previous summary, if there is one, with the conversation below. Use these sections:

## Open threads
Requests not finished yet, promises Alexa made, follow-ups due (with dates and times) and questions waiting for an answer, each with when it was asked. Leave out what's finished, and what the user dropped or told Alexa to stop.

## Done
What was completed, with the concrete results: numbers, names, dates, prices, confirmation codes, links, file paths, report ids and titles. Write each finished action as a dated past fact ("Booked a table at Nora for Fri Nov 6, 19:00, on Mon Nov 2"), never as something still to do.

## Context
Details needed to carry on: sites and accounts involved, where things stand in the browser, decisions and preferences the user expressed, things not to repeat. Quote word for word, in quotation marks, any standing instruction or limit the user gave: budgets, things never to do, people not to contact.

Be specific and factual. Take dates from the conversation's time stamps, with today's date (given below) for working out "yesterday" or "on Friday"; never invent one. Skip pleasantries and dead ends that no longer matter, drop what's obsolete, and keep recent things in the most detail. Write at most about 1,200 words. Reply with the summary only.`

// summaryHeader introduces the summary where it's replayed (buildInput):
// it's background to act from, not work to take up again.
const summaryHeader = `[Summary of the earlier part of our conversation, written when it was compacted. It's background: act on the messages after it, and when a note after it says unanswered messages were folded in, answer those from the quotes. Don't redo anything it lists as done, and don't take up older work unless asked; a "stop" or "never mind" ends it. Your memory of the user still holds, your tools work as usual, and what was said before can still be searched with recall.]`

// A compaction has three parts: planCompaction works out what to fold from
// the session as it is, foldSummary asks the model for the new summary
// (the part that takes a while, and touches nothing of the session but
// its activity), and commitCompaction stores it in place of the folded
// items. The commit goes ahead only if the conversation is still as it was
// planned from (store.CompactSession checks, in one transaction), so a
// summary written while something else changed the conversation is
// dropped instead of folding away what it never saw. The loop runs the
// three in a row (compact): it's the only writer of the conversation while
// it runs. Compact, the one the user asks for, plans and commits under the
// user lock and lets go of it for the model call (BH-2).

// errCompactChanged is a summary dropped because the conversation changed
// while it was written: a message came, a run started, a reset.
var errCompactChanged = errors.New("the conversation changed while it was being tidied up")

// compaction is a fold worked out from the session as it was read.
type compaction struct {
	ss    *store.Session
	rows  []store.ItemRow
	items []storedItem
	cut   int // the leading items folded
	from  int // where the messages not answered yet start
	t0    time.Time
}

// compact folds old conversation into the summary when the context is over
// budget (or always, with force). It returns nil if there was nothing to
// do. It's the loop's.
func (rt *Runtime) compact(ctx context.Context, u *store.User, force bool) error {
	c, err := rt.planCompaction(ctx, u, force)
	if err != nil || c == nil {
		return err
	}
	_ = rt.st.SetSessionActivity(ctx, u.ID, "Tidying up my notes")
	rt.publishState(u.ID)
	defer rt.runCompacting(u.ID)()
	summary, err := rt.foldSummary(ctx, u, c)
	if err != nil {
		return err
	}
	return rt.commitCompaction(ctx, u, c, summary)
}

// planCompaction works out what compact would fold now, or nil when
// there's nothing to.
func (rt *Runtime) planCompaction(ctx context.Context, u *store.User, force bool) (*compaction, error) {
	ss, rows, err := rt.st.Conversation(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	budget := int64(rt.cfg.CompactTokens)
	if !force && ss.ContextTokens < budget {
		return nil, nil
	}
	if !force && rt.compactCooling(u.ID) {
		return nil, nil // it failed a moment ago; don't pay for it again every turn
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
	from := unansweredFrom(items)
	if force && from < len(items) && (from == 0 || replaySize(items[from:]) > int(budget)) {
		// The messages Alexa hasn't answered yet are what doesn't fit:
		// nothing older is left to fold, or they alone are over the budget
		// (a huge paste, big PDFs). Kept, they would overflow every call
		// from now on, since only an answer lets them be folded.
		cut = len(items)
	}
	if nd := rt.pendingNeeds(ctx, u.ID); nd != nil {
		cut = keepPending(items, cut, nd.CallID)
	}
	if cut == 0 {
		return nil, nil
	}
	return &compaction{ss: ss, rows: rows, items: items, cut: cut, from: from, t0: time.Now()}, nil
}

// foldSummary writes the summary with the folded items merged in.
func (rt *Runtime) foldSummary(ctx context.Context, u *store.User, c *compaction) (string, error) {
	summary := c.ss.Summary
	for _, chunk := range transcriptChunks(c.items[:c.cut], 150_000) {
		s, err := rt.summarise(ctx, summary, chunk, time.Now().In(userLoc(u)).Format("Mon Jan 2 2006"))
		if err != nil {
			if ctx.Err() != nil {
				// A Stop or a shutdown isn't the summariser failing: no
				// cooldown, and the run says it's stopping, not thinking.
				return "", err
			}
			rt.compactFailed(u.ID)
			if rt.running(u.ID) != nil {
				_ = rt.st.SetSessionActivity(ctx, u.ID, "Thinking")
			}
			return "", err
		}
		summary = s
	}
	// The user's own words and the details that must never be paraphrased
	// survive outside what the summariser writes.
	return withAnchors(summary, c.ss.Summary, c.items[:c.cut]), nil
}

// commitCompaction stores the summary in place of the folded items, or
// returns errCompactChanged if the conversation isn't as it was planned
// from any more.
func (rt *Runtime) commitCompaction(ctx context.Context, u *store.User, c *compaction, summary string) error {
	// The plan survives word for word, in the summary and recited after it.
	plan := rt.currentPlan(ctx, u.ID)
	summary = withPlan(summary, plan)
	// The folded items are deleted with the commit; what recall and the
	// memory review keep of them is written in the same transaction
	// (recall.go).
	since, through := c.ss.CompactedThrough, c.rows[c.cut-1].ID
	kept := rt.keepFolded(ctx, u, rt.beforeFold(ctx, u.ID, since, through), c.rows[:c.cut], c.items[:c.cut])
	err := rt.st.CompactSession(ctx, u.ID, summary, since, through, c.rows[len(c.rows)-1].ID, kept)
	if errors.Is(err, store.ErrChanged) {
		return errCompactChanged
	}
	if err != nil {
		return err
	}
	_ = rt.recitePlan(ctx, u.ID, plan)
	if c.cut > c.from {
		if note := foldedNote(c.items[c.from:c.cut]); note != "" {
			_ = rt.appendMessage(ctx, u.ID, "system", note, nil)
		}
	}
	rt.compactOK(u.ID)
	if rt.running(u.ID) != nil {
		_ = rt.st.SetSessionActivity(ctx, u.ID, "Thinking")
	}
	// The cached prompt is gone anyway; catch up on memories saved since.
	rt.MemoryChanged(u.ID)
	// The folded items are gone, and the screenshots only they showed with
	// them (BH-3).
	rt.pruneShots(ctx, u.ID, shotGrace)
	slog.Info("compacted session", "user", u.ID, "folded", c.cut, "kept", len(c.items)-c.cut, "context_was", c.ss.ContextTokens,
		"summary_chars", len(summary), "took", time.Since(c.t0).Round(time.Millisecond))
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
	limit := unansweredFrom(items)
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

// unansweredFrom is where the messages after the model's last output
// begin: the ones being worked on.
func unansweredFrom(items []storedItem) int {
	from := len(items)
	for i := len(items) - 1; i >= 0 && isMessage(items[i].Kind); i-- {
		from = i
	}
	return from
}

// pictureTokens is about what a picture costs the model, at most
// maxViewSide a side.
const pictureTokens = 1600

// replaySize guesses the tokens items take replayed with their pictures and
// PDFs. A PDF's pages aren't known until the model reads them, so it counts
// as its base64 would as text: too much rather than too little, for
// deciding what can't be kept.
func replaySize(items []storedItem) int {
	n := 0
	for _, si := range items {
		n += estimate(si)
		for _, a := range si.Attach {
			switch {
			case strings.HasPrefix(a.Mime, "image/"):
				n += pictureTokens
			case a.Mime == "application/pdf" && a.Size < maxPDFInput:
				n += int(a.Size / 3)
			}
		}
	}
	return n
}

// foldedNote tells Alexa that messages she hadn't answered yet were folded
// into the summary, and where the files they came with still are. It's
// empty when none of them came from the user or a schedule.
func foldedNote(msgs []storedItem) string {
	asked := false
	var files []string
	for _, si := range msgs {
		asked = asked || si.Kind == "user" || si.Kind == "schedule"
		for _, a := range si.Attach {
			files = append(files, fmt.Sprintf("%s (%s)", attachName(a), a.Path))
		}
	}
	if !asked {
		return ""
	}
	note := "[Alexa system note] To make room, the messages you hadn't answered yet were folded into the summary above, where the user's words are quoted. Answer from there, and tell the user if you couldn't see all of what they sent."
	if len(files) > 0 {
		note += " The files that came with them are still in the workspace, for read_file: " + strings.Join(files, ", ") + "."
	}
	return note
}

// keepPending moves the cut back so the call the user hasn't answered yet
// stays in the conversation, with the model output it came in. Folding it
// away would leave its answer an output with no call, which the model
// service refuses (L5).
func keepPending(items []storedItem, cut int, callID string) int {
	at := -1
	for i, si := range items[:cut] {
		if it := llm.ParseItem(si.Item); it.Type == "function_call" && it.CallID == callID {
			at = i
		}
	}
	if at < 0 {
		return cut
	}
	for i := at; i >= 1; i-- {
		if safeCut(items, i) {
			return i
		}
	}
	return 0
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
		case "request_login":
			return "ALEXA asked the user to sign in: " + str(args, "reason")
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

// isContextOverflow recognises the API's "prompt too long" errors, a 413
// among them, as llm.Classify does: the loop compacts for exactly what the
// card will call too long, and never for a gateway's "context deadline
// exceeded".
func isContextOverflow(err error) bool {
	reason, _ := llm.Classify(err)
	return reason == llm.ReasonContextOverflow
}

// ---------- quality gates ----------

// summarise folds one chunk of the conversation into the summary so far,
// through quality gates. A summary that was cut off, came back empty, or
// lost an identifier the chunk shows the user (an order number, a link, an
// email, a price) is sent back once with what's wrong. A second cut-off or
// empty one fails the compaction; a second one that still lost
// identifiers is let through, since the anchors (withAnchors) keep them
// word for word anyway. today is the user's date ("Mon Nov 2 2026"), so
// finished work can be dated.
func (rt *Runtime) summarise(ctx context.Context, prev, chunk, today string) (string, error) {
	base := "Today is " + today + ".\n\nPrevious summary:\n" + orNone(prev) + "\n\nConversation to fold in:\n" + chunk
	want := identifiers(chunk)
	input := base
	for try := 1; ; try++ {
		s, status, err := rt.summaryCall(ctx, input)
		if err != nil {
			return "", fmt.Errorf("summarise: %w", err)
		}
		s = dropSection(s, anchorsHeading)
		problem := ""
		switch {
		case s == "":
			problem = "empty"
		case status == "incomplete":
			problem = "cut off"
		}
		missing := missingFrom(s, want)
		if problem == "" && len(missing) == 0 {
			return s, nil
		}
		if try == 2 {
			if problem != "" {
				return "", fmt.Errorf("summarise: the summary came back %s twice", problem)
			}
			slog.Warn("summary lost identifiers; the anchors keep them", "missing", missing)
			return s, nil
		}
		slog.Info("summary rejected, asking again", "problem", problem, "missing", len(missing))
		if problem != "" {
			input = base + "\n\nYour last summary came back " + problem + ". Write it again, complete and a little shorter: at most about 1,000 words."
		} else {
			input = base + "\n\nYour last summary left out details that must be kept exactly as written. Write it again and include each of these word for word: " + strings.Join(missing, "; ")
		}
	}
}

// summaryCall asks the model for a summary and returns it with the
// response's status ("incomplete" when it was cut off).
func (rt *Runtime) summaryCall(ctx context.Context, input string) (string, string, error) {
	msg, _ := json.Marshal(map[string]any{"role": "user", "content": input})
	resp, err := rt.llm.Create(ctx, &llm.Request{
		Model:        rt.cfg.Model,
		Instructions: compactPrompt,
		Input:        []json.RawMessage{msg},
		Reasoning:    &llm.Reasoning{Effort: "low"},
	})
	if err != nil {
		return "", "", err
	}
	var b strings.Builder
	for _, raw := range resp.Output {
		if it := llm.ParseItem(raw); it.Type == "message" {
			b.WriteString(it.Text())
		}
	}
	return strings.TrimSpace(b.String()), resp.Status, nil
}

const maxIdentifiers = 40 // checked and anchored, newest first

var (
	urlRe   = regexp.MustCompile(`https?://[^\s<>"'()\[\]{}]+`)
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	priceRe = regexp.MustCompile(`[$€£¥]\s?\d[\d,]*(?:\.\d+)?|\d[\d,]*(?:\.\d+)?\s?(?:USD|EUR|GBP|CAD|AUD|CHF)\b|\d[\d,]*(?:\.\d+)?\s?[€£]`)
	// An order, confirmation or booking number, with the word that names it.
	refRe = regexp.MustCompile(`\b(?i:order|confirmation|booking|reservation|reference|ref|ticket|tracking|invoice|case|pnr)(?:\s+(?i:number|no\.?|code|id))?\s*(?:#|:|(?i:is))?\s*#?\s*([A-Z0-9][A-Z0-9-]{3,29})\b`)
)

// identifiers finds what a summary must not lose in a transcript (as
// transcriptChunks writes it): order and confirmation numbers anywhere,
// and the links, emails and prices in what the user and Alexa said to
// each other. Pages Alexa read are full of links and prices nobody asked
// about, so those aren't checked. Newest last, at most maxIdentifiers.
func identifiers(transcript string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimRight(s, ".,;:!?'\"")
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	spoken := false
	for _, l := range strings.Split(transcript, "\n") {
		switch {
		case strings.HasPrefix(l, "USER: "), strings.HasPrefix(l, "SCHEDULED: "), strings.HasPrefix(l, "ALEXA said: "),
			strings.HasPrefix(l, "ALEXA asked: "), strings.HasPrefix(l, "ALEXA showed a report"):
			spoken = true
		case strings.HasPrefix(l, "ALEXA "), strings.HasPrefix(l, "NOTE: "), strings.HasPrefix(l, "  → "):
			spoken = false
		}
		for _, m := range refRe.FindAllStringSubmatch(l, -1) {
			if ref := m[1]; strings.ContainsAny(ref, "0123456789") || len(ref) >= 5 && strings.ToUpper(ref) == ref {
				add(m[0])
			}
		}
		if !spoken {
			continue
		}
		for _, re := range []*regexp.Regexp{urlRe, emailRe, priceRe} {
			for _, m := range re.FindAllString(l, -1) {
				add(m)
			}
		}
	}
	if len(out) > maxIdentifiers {
		out = out[len(out)-maxIdentifiers:]
	}
	return out
}

// missingFrom lists the identifiers summary doesn't contain word for word.
// An order number counts when its number is there.
func missingFrom(summary string, want []string) []string {
	var out []string
	for _, id := range want {
		if m := refRe.FindStringSubmatch(id); m != nil {
			id = m[1]
		}
		if !strings.Contains(summary, strings.TrimSuffix(id, "/")) {
			out = append(out, id)
		}
	}
	return out
}

// ---------- anchors ----------

const (
	anchorsHeading = "## Kept word for word"
	maxQuotes      = 6
)

// withAnchors ends the summary with what must survive every compaction
// unparaphrased: the user's latest requests, quoted, and the identifiers
// from the folded conversation, merged with the anchors the previous
// summary had. The summariser doesn't write this part, so it can't lose
// it.
func withAnchors(summary, prev string, cut []storedItem) string {
	oldQuotes, oldIDs := parseAnchors(prev)
	var quotes []string
	for i := len(cut) - 1; i >= 0; i-- {
		if cut[i].Kind == "user" {
			if q := userWords(cut[i]); q != "" {
				quotes = append(quotes, q)
			}
		}
	}
	ids := identifiers(strings.Join(transcriptChunks(cut, 1<<30), "\n"))
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i] // newest first
	}
	quotes = firstUnique(append(quotes, oldQuotes...), maxQuotes)
	ids = firstUnique(append(ids, oldIDs...), maxIdentifiers)
	summary = dropSection(summary, anchorsHeading)
	if len(quotes) == 0 && len(ids) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary) + "\n\n" + anchorsHeading + "\n")
	if len(quotes) > 0 {
		b.WriteString("The user's latest requests, newest first:\n")
		for _, q := range quotes {
			b.WriteString("> " + q + "\n")
		}
	}
	if len(ids) > 0 {
		b.WriteString("Details to keep exactly:\n")
		for _, id := range ids {
			b.WriteString("- " + id + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// parseAnchors reads the anchors section of a summary.
func parseAnchors(summary string) (quotes, ids []string) {
	in := false
	for _, l := range strings.Split(summary, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == anchorsHeading:
			in = true
		case in && strings.HasPrefix(t, "## "):
			in = false
		case in && strings.HasPrefix(t, "> "):
			quotes = append(quotes, strings.TrimPrefix(t, "> "))
		case in && strings.HasPrefix(t, "- "):
			ids = append(ids, strings.TrimPrefix(t, "- "))
		}
	}
	return quotes, ids
}

// userWords is a user message on one line, trimmed for quoting.
func userWords(si storedItem) string {
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
	return trim(strings.Join(strings.Fields(strings.Join(parts, " ")), " "), 300)
}

func firstUnique(list []string, n int) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range list {
		if s != "" && !seen[s] && len(out) < n {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ---------- cooldown ----------

// A compaction that failed isn't tried again on every turn (L19): each one
// costs a full summarising call and its retries. It waits 1, then 5, then
// 15 minutes. A compaction the user asks for, or one the model's "too
// long" forces, goes ahead anyway.
var compactCooldowns = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

type coolKey struct {
	rt  *Runtime
	uid int64
}

var compactFails = struct {
	sync.Mutex
	m map[coolKey]coolState
}{m: map[coolKey]coolState{}}

type coolState struct {
	fails int
	until time.Time
}

// compactsRunning are the users whose Compact is under way: a second one
// is told Alexa is busy rather than paying for the same summary again.
var compactsRunning = struct {
	sync.Mutex
	m map[coolKey]bool
}{m: map[coolKey]bool{}}

func (rt *Runtime) startCompacting(userID int64) bool {
	compactsRunning.Lock()
	defer compactsRunning.Unlock()
	k := coolKey{rt, userID}
	if compactsRunning.m[k] {
		return false
	}
	compactsRunning.m[k] = true
	return true
}

func (rt *Runtime) doneCompacting(userID int64) {
	compactsRunning.Lock()
	delete(compactsRunning.m, coolKey{rt, userID})
	compactsRunning.Unlock()
}

func (rt *Runtime) compactCooling(userID int64) bool {
	compactFails.Lock()
	defer compactFails.Unlock()
	return time.Now().Before(compactFails.m[coolKey{rt, userID}].until)
}

func (rt *Runtime) compactFailed(userID int64) {
	compactFails.Lock()
	defer compactFails.Unlock()
	k := coolKey{rt, userID}
	st := compactFails.m[k]
	st.fails++
	st.until = time.Now().Add(compactCooldowns[min(st.fails, len(compactCooldowns))-1])
	compactFails.m[k] = st
	slog.Warn("compaction failed; waiting before the next try", "user", userID, "fails", st.fails, "until", st.until.Format(time.TimeOnly))
}

func (rt *Runtime) compactOK(userID int64) {
	compactFails.Lock()
	delete(compactFails.m, coolKey{rt, userID})
	compactFails.Unlock()
}
