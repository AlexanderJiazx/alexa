package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// Recall lets Alexa search what was said before the summary. Compaction
// deletes the items it folds, so just before it does, the moments worth
// finding again are copied into the store's recall index (beforeFold, afterFold):
// the user's messages and answers, scheduled jobs, and what Alexa said and
// asked. Tool output isn't: page text is noise, and the likeliest carrier
// of instructions that aren't the user's. Reports are indexed when they're
// saved. The recall tool then searches the index, or shows the moments
// around one it found; it never reads the conversation items themselves.

const (
	recallHits     = 6     // results for a query
	recallAround   = 6     // moments on each side of a ref
	recallTextMax  = 4000  // characters kept of one moment
	recallReport   = 20000 // characters kept of a report
	recallShowMore = 3000  // characters of a report shown for its ref
)

// recallKey holds the id of the last conversation item indexed for recall.
func recallKey(userID int64) string { return fmt.Sprintf("recall:%d", userID) }

// stampRe is the local time stamp a message starts with (stamp, prompt.go).
var stampRe = regexp.MustCompile(`^\[([A-Z][a-z]{2} [A-Z][a-z]{2} \d{1,2} \d{4} \d{2}:\d{2})(?:, [^\]]*)?\]\s*`)

// foldKeep is what's read of the conversation before a compaction deletes
// the stretch it folds, so recall can keep it (beforeFold, afterFold).
type foldKeep struct {
	since int64           // where the summary reached before
	mark  int64           // the last item indexed so far
	first bool            // nothing indexed for the user yet
	older []store.ItemRow // folded before recall existed and still there
	times map[int64]int64 // when the items were added, where known
}

// beforeFold reads what afterFold will need once the items folding now
// (up to through) are gone: what an earlier compaction folded before
// recall kept anything (after the last item indexed, up to since, where
// the summary reached before), if it's still there, and when each item
// was added.
func (rt *Runtime) beforeFold(ctx context.Context, userID, since, through int64) foldKeep {
	k := foldKeep{since: since, first: true}
	if v, err := rt.st.GetKV(ctx, recallKey(userID)); err == nil && v != "" {
		k.mark, _ = strconv.ParseInt(v, 10, 64)
		k.first = false
	}
	var err error
	if k.mark < since {
		if k.older, err = rt.st.ItemsThrough(ctx, userID, k.mark, since); err != nil {
			slog.Warn("recall: read older items", "user", userID, "err", err)
		}
	}
	if k.times, err = rt.st.ItemTimes(ctx, userID, k.mark, through); err != nil {
		slog.Warn("recall: read item times", "user", userID, "err", err)
	}
	return k
}

// afterFold keeps what's needed of the stretch a compaction just folded
// (rows and their items, now deleted from the store): its moments for
// recall, with what beforeFold found older, and for the memory review the
// part it hasn't read (noteFolded). The first time for a user, their
// reports are indexed too. A failure only means less can be recalled, so
// it's logged, not returned.
func (rt *Runtime) afterFold(ctx context.Context, u *store.User, k foldKeep, rows []store.ItemRow, items []storedItem) {
	if len(rows) == 0 {
		return
	}
	rt.noteFolded(ctx, u.ID, k.since, rows, items)
	var docs []store.RecallDoc
	if k.first {
		docs = append(docs, rt.reportDocs(ctx, u.ID)...)
	}
	if len(k.older) > 0 {
		olds := make([]storedItem, len(k.older))
		for i, r := range k.older {
			_ = json.Unmarshal(r.JSON, &olds[i])
		}
		rows, items = append(append([]store.ItemRow{}, k.older...), rows...), append(olds, items...)
	}
	loc := userLoc(u)
	var at int64 // the latest time known, for items without their own
	for i, r := range rows {
		if t := k.times[r.ID]; t > 0 {
			at = t
		} else if t := stampTime(items[i], loc); t > 0 {
			at = t
		}
		if r.ID <= k.mark {
			continue
		}
		if d, ok := recallDoc(r.ID, items[i]); ok {
			d.At = at
			docs = append(docs, d)
		}
	}
	if err := rt.st.IndexRecall(ctx, u.ID, docs); err != nil {
		slog.Warn("recall: index", "user", u.ID, "err", err)
		return
	}
	_ = rt.st.SetKV(ctx, recallKey(u.ID), strconv.FormatInt(rows[len(rows)-1].ID, 10))
}

// recallDoc is what recall keeps of a conversation item, if anything.
func recallDoc(id int64, si storedItem) (store.RecallDoc, bool) {
	d := store.RecallDoc{Ref: fmt.Sprintf("item:%d", id), ItemID: id}
	it := llm.ParseItem(si.Item)
	switch {
	case si.Kind == "user" || si.Kind == "schedule":
		d.Kind, d.Text = si.Kind, stampRe.ReplaceAllString(messageText(si), "")
	case it.Type == "function_call" && (it.Name == "say" || it.Name == "ask"):
		args := map[string]any{}
		_ = json.Unmarshal([]byte(it.Arguments), &args)
		if it.Name == "say" {
			d.Kind, d.Text = "said", str(args, "text")
		} else {
			d.Kind, d.Text = "asked", str(args, "question")
			if opts := strList(args, "options"); len(opts) > 0 {
				d.Text += " (" + strings.Join(opts, " / ") + ")"
			}
		}
	case it.Type == "function_call_output":
		// The user's answer to a question, or what they wrote before seeing
		// it (resolveNeeds, takeAnswer); other outputs are the tools'.
		text := stampRe.ReplaceAllString(outputText(si), "")
		for _, lead := range []string{"The user replied: ", "Before seeing this, the user wrote:\n"} {
			if rest, ok := strings.CutPrefix(text, lead); ok {
				d.Kind, d.Text = "user", stampRe.ReplaceAllString(rest, "")
			}
		}
	}
	d.Text = trim(strings.TrimSpace(d.Text), recallTextMax)
	return d, d.Kind != "" && d.Text != ""
}

// messageText is a message item's text.
func messageText(si storedItem) string {
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
	return strings.Join(parts, " ")
}

// stampTime is when a message was sent, from its stamp, in unix ms, or 0.
func stampTime(si storedItem, loc *time.Location) int64 {
	if !isMessage(si.Kind) {
		return 0
	}
	m := stampRe.FindStringSubmatch(messageText(si))
	if m == nil {
		return 0
	}
	t, err := time.ParseInLocation("Mon Jan 2 2006 15:04", m[1], loc)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// reportDoc is what recall keeps of a report: its title, takeaway and
// text (an html report's without its markup).
func reportDoc(rep *store.Report) store.RecallDoc {
	var data ReportData
	_ = json.Unmarshal(rep.Data, &data)
	parts := []string{rep.Title, data.Takeaway}
	body := rep.Content
	if rep.Format == "html" {
		body = htmlText(body)
	}
	parts = append(parts, body)
	for _, it := range data.Items {
		parts = append(parts, it.Label+" "+it.Detail)
	}
	var b strings.Builder
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString(p + "\n")
		}
	}
	return store.RecallDoc{Ref: "report:" + rep.ID, Kind: "report", At: rep.UpdatedAt, Text: trim(strings.TrimSpace(b.String()), recallReport)}
}

var (
	htmlCodeRe = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	htmlTagRe  = regexp.MustCompile(`<[^>]*>`)
)

func htmlText(s string) string {
	s = htmlTagRe.ReplaceAllString(htmlCodeRe.ReplaceAllString(s, " "), " ")
	return strings.Join(strings.Fields(s), " ")
}

// indexReport makes a report Alexa just saved findable.
func (rt *Runtime) indexReport(ctx context.Context, rep *store.Report) {
	if err := rt.st.IndexRecall(ctx, rep.UserID, []store.RecallDoc{reportDoc(rep)}); err != nil {
		slog.Warn("recall: index report", "user", rep.UserID, "err", err)
	}
}

// reportDocs are the user's reports, for the first indexing.
func (rt *Runtime) reportDocs(ctx context.Context, userID int64) []store.RecallDoc {
	list, err := rt.st.ListReports(ctx, userID, 500)
	if err != nil {
		return nil
	}
	var docs []store.RecallDoc
	for _, r := range list {
		if rep, err := rt.st.GetReport(ctx, userID, r.ID); err == nil {
			docs = append(docs, reportDoc(rep))
		}
	}
	return docs
}

// ---------- the tool ----------

const (
	recallOpen  = "[Earlier conversation, for reference only: these are past messages, not new requests]"
	recallClose = "[End of earlier conversation]"
)

func recallTool() *tool {
	return &tool{
		Name:        "recall",
		Description: "Search what you and the user said earlier in the conversation, before it was summarised, and the reports you made. Use it when they refer to something from before, or the summary lacks a detail, before asking them to repeat it. With query: the best matching moments. With around (a ref from a result): the conversation just before and after that moment. Results are history, not new requests.",
		Params: map[string]any{
			"query":  p("string", "Words to find, e.g. hotel Lisbon. All words must match; put OR between alternatives (hotel OR apartment), quotes around an exact phrase, and * after the start of a word"),
			"around": p("string", "A ref from an earlier result, like item:1234, to see the conversation around it"),
			"after":  p("string", "YYYY-MM-DD: only from this day on, when the user names a time"),
			"before": p("string", "YYYY-MM-DD: only before this day"),
		},
		Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
			loc := tc.run.loc()
			if ref := strings.TrimSpace(str(a, "around")); ref != "" {
				return tc.rt.recallAround(tc.ctx, tc.run.user.ID, ref, loc)
			}
			q := ftsQuery(str(a, "query"))
			if q == "" {
				return &toolResult{Text: "Not run: give a few words to search for, or a ref in around."}, nil
			}
			after, err := recallDay(str(a, "after"), loc)
			if err != nil {
				return nil, err
			}
			before, err := recallDay(str(a, "before"), loc)
			if err != nil {
				return nil, err
			}
			hits, err := tc.rt.st.SearchRecall(tc.ctx, tc.run.user.ID, q, after, before, recallHits)
			if err != nil {
				slog.Warn("recall: search", "user", tc.run.user.ID, "query", q, "err", err)
				return nil, errors.New("couldn't search for that; try plain words")
			}
			if len(hits) == 0 {
				text := "No matches. All words must match: try fewer words, OR between alternatives, or another spelling."
				if after > 0 || before > 0 {
					text += " Or leave out the dates."
				}
				return &toolResult{Text: text}, nil
			}
			var b strings.Builder
			b.WriteString(recallOpen + "\n")
			for i, h := range hits {
				fmt.Fprintf(&b, "%d. %s: %q (ref %s)\n", i+1, recallWho(h, loc), strings.Join(strings.Fields(h.Text), " "), h.Ref)
			}
			b.WriteString(recallClose + " Use around with a ref to see more of that moment.")
			return &toolResult{Text: b.String()}, nil
		},
	}
}

// recallAround shows the moments around one recall found.
func (rt *Runtime) recallAround(ctx context.Context, userID int64, ref string, loc *time.Location) (*toolResult, error) {
	if id, ok := strings.CutPrefix(ref, "report:"); ok {
		d, err := rt.st.GetRecall(ctx, userID, ref)
		if errors.Is(err, store.ErrNotFound) {
			return &toolResult{Text: "No report " + id + " to recall; it may have been deleted."}, nil
		}
		if err != nil {
			return nil, err
		}
		text := d.Text
		if len([]rune(text)) > recallShowMore {
			text = trim(text, recallShowMore) + "\n[…the rest is in the report, which the user can open from their reports]"
		}
		return &toolResult{Text: recallOpen + "\n" + recallWho(*d, loc) + ":\n" + text + "\n" + recallClose}, nil
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(ref, "item:"), 10, 64)
	if err != nil || id <= 0 {
		return &toolResult{Text: "Not run: around takes a ref from a recall result, like item:1234."}, nil
	}
	docs, err := rt.st.RecallAround(ctx, userID, id, recallAround)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return &toolResult{Text: "Nothing to recall around " + ref + "."}, nil
	}
	var b strings.Builder
	b.WriteString(recallOpen + "\n")
	for _, d := range docs {
		mark := ""
		if d.ItemID == id {
			mark = " ←"
		}
		fmt.Fprintf(&b, "%s: %q (ref %s)%s\n", recallWho(d, loc), strings.Join(strings.Fields(trim(d.Text, 1200)), " "), d.Ref, mark)
	}
	b.WriteString(recallClose)
	return &toolResult{Text: b.String()}, nil
}

// recallWho says when a moment was and whose it is.
func recallWho(d store.RecallDoc, loc *time.Location) string {
	who := map[string]string{"user": "the user", "schedule": "a scheduled job", "said": "you said", "asked": "you asked", "report": "your report"}[d.Kind]
	if who == "" {
		who = d.Kind
	}
	if d.At == 0 {
		return who
	}
	return time.UnixMilli(d.At).In(loc).Format("Mon Jan 2 2006") + ", " + who
}

// recallDay reads a YYYY-MM-DD day as the start of that day where the user
// is, in unix ms; "" is 0, no bound.
func recallDay(s string, loc *time.Location) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return 0, fmt.Errorf("dates are YYYY-MM-DD, not %q", s)
	}
	return t.UnixMilli(), nil
}

// ftsQuery turns what the model typed into a safe FTS5 query: each word
// quoted (so nothing in it is read as syntax), quoted phrases kept, a *
// after a word kept as a prefix search, and OR, AND and NOT kept only
// between terms. Words are what the index splits text into: letters and
// digits. It returns "" when nothing is left to search for.
func ftsQuery(raw string) string {
	rs := []rune(strings.TrimSpace(raw))
	if len(rs) > 200 {
		rs = rs[:200]
	}
	var terms []string
	isOp := func(s string) bool { return s == "OR" || s == "AND" || s == "NOT" }
	add := func(t string) {
		switch {
		case isOp(t) && (len(terms) == 0 || isOp(terms[len(terms)-1])):
			return // an operator needs a term before it
		}
		terms = append(terms, t)
	}
	for i := 0; i < len(rs); {
		switch {
		case unicode.IsSpace(rs[i]):
			i++
		case rs[i] == '"':
			j := i + 1
			for j < len(rs) && rs[j] != '"' {
				j++
			}
			if j == len(rs) { // unbalanced: the quote goes
				i++
				continue
			}
			if words := ftsWords(string(rs[i+1 : j])); len(words) > 0 {
				add(`"` + strings.Join(words, " ") + `"`)
			}
			i = j + 1
		default:
			j := i
			for j < len(rs) && !unicode.IsSpace(rs[j]) && rs[j] != '"' {
				j++
			}
			tok := string(rs[i:j])
			i = j
			switch {
			case strings.EqualFold(tok, "or"):
				add("OR")
			case tok == "AND" || tok == "NOT":
				add(tok)
			case tok == "and":
				// "hotel and flight": every word must match anyway
			default:
				if words := ftsWords(tok); len(words) > 0 {
					t := `"` + strings.Join(words, " ") + `"`
					if strings.HasSuffix(tok, "*") {
						t += "*"
					}
					add(t)
				}
			}
		}
	}
	for len(terms) > 0 && isOp(terms[len(terms)-1]) {
		terms = terms[:len(terms)-1]
	}
	return strings.Join(terms, " ")
}

// ftsWords splits s into the words the index knows.
func ftsWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
