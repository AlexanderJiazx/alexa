package agent

// Blocks as text: the one-line summary (push, screen readers, voice and
// search), the echo in tool results ("Shown: 4 product cards…"), Markdown
// for documents that leave the app (the report's .md download, compaction,
// email), a report card's preview, and calendar events.

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"alexagent/internal/store"
)

// summaryMax is how long a block summary may be.
const summaryMax = 160

// summarizeBlocks describes blocks in at most 160 characters, for push
// bodies, screen readers, voice and search: "3 options: Sony WH-1000XM6
// $279 (pick), Bose QC Ultra $299".
func summarizeBlocks(raws []json.RawMessage) string {
	var parts []string
	for _, b := range parseBlocks(raws) {
		if s := strings.TrimSpace(summarize(b)); s != "" {
			parts = append(parts, s)
		}
	}
	return clip(strings.Join(parts, " · "), summaryMax)
}

func summarize(b block) string {
	switch b := b.(type) {
	case *cardsBlock:
		var names []string
		for _, it := range b.Items {
			s := it.Title
			if it.Price != nil {
				s += " " + money(it.Price.Amount, it.Price.Currency)
			}
			if it.Pick {
				s += " (pick)"
			}
			names = append(names, s)
		}
		if len(b.Items) == 1 {
			return names[0]
		}
		return fmt.Sprintf("%d %s: %s", len(b.Items), plural(cardNouns[b.Kind], len(b.Items)), strings.Join(names, ", "))
	case *compareBlock:
		var names []string
		for _, it := range b.Items {
			s := it.Name
			if it.Pick {
				s += " (pick)"
			}
			names = append(names, s)
		}
		s := "Comparison of " + strings.Join(names, ", ")
		if b.Verdict != "" {
			s += ". " + b.Verdict
		}
		return s
	case *tableBlock:
		what := b.Title
		if what == "" {
			var cols []string
			for _, c := range b.Columns {
				cols = append(cols, string(c))
			}
			what = strings.Join(cols, ", ")
		}
		return fmt.Sprintf("Table: %s, %d %s", what, len(b.Rows), plural("row", len(b.Rows)))
	case *statsBlock:
		var parts []string
		for _, s := range b.Items {
			p := s.Label + " " + string(s.Value)
			if s.Delta != "" {
				p += " (" + string(s.Delta) + ")"
			}
			parts = append(parts, p)
		}
		return strings.Join(parts, "; ")
	case *chartBlock:
		var names []string
		for _, s := range b.Series {
			names = append(names, s.Name)
		}
		what := b.Title
		if what == "" {
			what = strings.Join(names, ", ")
		}
		return fmt.Sprintf("%s chart: %s", strings.ToUpper(b.Style[:1])+b.Style[1:], what)
	case *timelineBlock:
		var items []string
		n := 0
		for _, d := range b.Days {
			for _, it := range d.Items {
				n++
				s := it.Title
				if it.Time != "" {
					s = string(it.Time) + " " + s
				}
				items = append(items, s)
			}
		}
		head := "Itinerary"
		if len(b.Days) > 0 {
			head += ", " + dayLabel(b.Days[0].Date)
			if len(b.Days) > 1 {
				head += " to " + dayLabel(b.Days[len(b.Days)-1].Date)
			}
		}
		return head + ": " + strings.Join(items, "; ")
	case *checklistBlock:
		done, failed := 0, 0
		for _, it := range b.Items {
			switch it.Status {
			case "done":
				done++
			case "failed":
				failed++
			}
		}
		s := fmt.Sprintf("Checklist: %d of %d done", done, len(b.Items))
		if failed > 0 {
			s += fmt.Sprintf(", %d failed", failed)
		}
		return s
	case *receiptBlock:
		s := strings.ToUpper(b.Status[:1]) + b.Status[1:] + " receipt"
		if b.Merchant != "" {
			s += " from " + b.Merchant
		}
		if b.Total != nil {
			s += ": " + money(b.Total.Amount, b.Total.Currency)
		}
		if b.Ref != "" {
			s += ", ref " + string(b.Ref)
		}
		return s
	case *linksBlock:
		var t []string
		for _, it := range b.Items {
			t = append(t, it.Title)
		}
		return fmt.Sprintf("%d %s: %s", len(b.Items), plural("link", len(b.Items)), strings.Join(t, ", "))
	case *imagesBlock:
		var t []string
		for _, it := range b.Items {
			t = append(t, it.Caption)
		}
		return fmt.Sprintf("%d %s: %s", len(b.Items), plural("image", len(b.Items)), strings.Join(t, ", "))
	case *filesBlock:
		var t []string
		for _, f := range b.Items {
			p := string(f)
			t = append(t, p[strings.LastIndex(p, "/")+1:])
		}
		return fmt.Sprintf("%d %s: %s", len(b.Items), plural("file", len(b.Items)), strings.Join(t, ", "))
	case *calloutBlock:
		return b.Text
	case *markdownBlock:
		first, _, _ := strings.Cut(strings.TrimSpace(b.Text), "\n")
		return plain(first)
	}
	return ""
}

// echoBlocks says what was shown, for the tool result: "4 product cards
// (pick: Sony WH-1000XM6), a comparison of 3".
func echoBlocks(raws []json.RawMessage) string {
	var parts []string
	for _, b := range parseBlocks(raws) {
		parts = append(parts, echo(b))
	}
	return strings.Join(parts, ", ")
}

func echo(b block) string {
	switch b := b.(type) {
	case *cardsBlock:
		kind := b.Kind
		if kind == "item" {
			kind = ""
		}
		s := strings.TrimSpace(fmt.Sprintf("%d %s %s", len(b.Items), kind, plural("card", len(b.Items))))
		for _, it := range b.Items {
			if it.Pick {
				s += " (pick: " + it.Title + ")"
			}
		}
		return s
	case *compareBlock:
		s := fmt.Sprintf("a comparison of %d", len(b.Items))
		for _, it := range b.Items {
			if it.Pick {
				s += " (pick: " + it.Name + ")"
			}
		}
		return s
	case *tableBlock:
		return fmt.Sprintf("a table (%d %s)", len(b.Rows), plural("row", len(b.Rows)))
	case *statsBlock:
		return fmt.Sprintf("%d stat %s", len(b.Items), plural("tile", len(b.Items)))
	case *chartBlock:
		return fmt.Sprintf("a %s chart (%d %s)", b.Style, len(b.Series), plural("series", 1))
	case *timelineBlock:
		n := 0
		for _, d := range b.Days {
			n += len(d.Items)
		}
		return fmt.Sprintf("a timeline (%d %s, %d %s)", len(b.Days), plural("day", len(b.Days)), n, plural("item", n))
	case *checklistBlock:
		return "a " + strings.ToLower(summarize(b))
	case *receiptBlock:
		s := "a " + b.Status + " receipt"
		if b.Total != nil {
			s += " (total " + money(b.Total.Amount, b.Total.Currency) + ")"
		}
		return s
	case *linksBlock:
		return fmt.Sprintf("%d %s", len(b.Items), plural("link", len(b.Items)))
	case *imagesBlock:
		return fmt.Sprintf("%d %s", len(b.Items), plural("image", len(b.Items)))
	case *filesBlock:
		return fmt.Sprintf("%d %s", len(b.Items), plural("file", len(b.Items)))
	case *calloutBlock:
		return "a " + b.Tone + " callout"
	case *markdownBlock:
		return "a markdown section"
	}
	return "a block"
}

// ---------- Markdown ----------

// blocksToMarkdown writes blocks as Markdown, for the report's .md
// download, compaction summaries and email.
func blocksToMarkdown(raws []json.RawMessage) string { return blocksMarkdown(raws, nil) }

// blocksMarkdown is blocksToMarkdown with a function that maps image
// references to URLs that work outside the app.
func blocksMarkdown(raws []json.RawMessage, img func(string) string) string {
	if img == nil {
		img = func(s string) string { return s }
	}
	var parts []string
	for _, b := range parseBlocks(raws) {
		if s := strings.TrimSpace(markdown(b, img)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func markdown(b block, img func(string) string) string {
	var w strings.Builder
	if t := b.base().Title; t != "" {
		fmt.Fprintf(&w, "### %s\n\n", t)
	}
	switch b := b.(type) {
	case *cardsBlock:
		for _, it := range b.Items {
			title := "**" + mdEscape(it.Title) + "**"
			if it.URL != "" {
				title = "[" + title + "](" + it.URL + ")"
			}
			if it.Pick {
				title += " — Alexa’s pick"
			}
			fmt.Fprintf(&w, "- %s\n", title)
			var meta []string
			if it.Subtitle != "" {
				meta = append(meta, it.Subtitle)
			}
			if it.Price != nil {
				p := money(it.Price.Amount, it.Price.Currency)
				if it.Price.Per != "" && it.Price.Per != "total" {
					p += " per " + it.Price.Per
				}
				if it.Price.Was != nil {
					p += " (was " + money(*it.Price.Was, it.Price.Currency) + ")"
				}
				meta = append(meta, p)
			}
			if it.Rating != nil {
				r := "★ " + number(float64(it.Rating.Value))
				if it.Rating.OutOf != nil && *it.Rating.OutOf != 5 {
					r += "/" + number(float64(*it.Rating.OutOf))
				}
				if it.Rating.Count != nil {
					r += " (" + number(float64(*it.Rating.Count)) + ")"
				}
				meta = append(meta, r)
			}
			meta = append(meta, it.Badges...)
			if len(meta) > 0 {
				fmt.Fprintf(&w, "  %s\n", mdEscape(strings.Join(meta, " · ")))
			}
			var details []string
			for _, f := range it.Facts {
				details = append(details, f.Label+": "+string(f.Value))
			}
			for _, kv := range [][2]string{{"Address", it.Address}, {"Hours", it.Hours}, {"Distance", string(it.Distance)}, {"Phone", string(it.Phone)},
				{"Check-in", it.CheckIn}, {"Check-out", it.CheckOut}, {"Starts", it.Start}, {"Ends", it.End}, {"Venue", it.Venue},
				{"Duration", string(it.Duration)}, {"Cabin", it.Cabin}} {
				if kv[1] != "" {
					details = append(details, kv[0]+": "+kv[1])
				}
			}
			if it.Stops != nil {
				details = append(details, stopsText(it.Stops.int()))
			}
			for _, s := range it.Segments {
				details = append(details, strings.TrimSpace(s.Carrier+" "+string(s.Number))+" "+s.From+" "+s.Depart+" to "+s.To+" "+s.Arrive)
			}
			if len(details) > 0 {
				fmt.Fprintf(&w, "  %s\n", mdEscape(strings.Join(details, " · ")))
			}
			if it.Note != "" {
				fmt.Fprintf(&w, "  %s\n", mdEscape(it.Note))
			}
			if it.Image != "" {
				fmt.Fprintf(&w, "  ![%s](%s)\n", mdEscape(it.Title), img(it.Image))
			}
		}
	case *compareBlock:
		head := []string{""}
		for _, it := range b.Items {
			h := it.Name
			if it.Pick {
				h += " (pick)"
			}
			head = append(head, h)
		}
		var rows [][]string
		if hasPrice(b.Items) {
			row := []string{"Price"}
			for _, it := range b.Items {
				if it.Price != nil {
					row = append(row, money(it.Price.Amount, it.Price.Currency))
				} else {
					row = append(row, "")
				}
			}
			rows = append(rows, row)
		}
		for _, r := range b.Rows {
			row := []string{r.Label}
			for j, v := range r.Values {
				s := string(v)
				if r.Best != nil && r.Best.int() == j {
					s = "**" + s + "** ✓"
				}
				row = append(row, s)
			}
			rows = append(rows, row)
		}
		w.WriteString(mdTable(head, rows))
		if b.Verdict != "" {
			fmt.Fprintf(&w, "\n%s\n", mdEscape(b.Verdict))
		}
	case *tableBlock:
		var head []string
		for _, c := range b.Columns {
			head = append(head, string(c))
		}
		var rows [][]string
		for _, r := range b.Rows {
			var row []string
			for _, c := range r {
				row = append(row, string(c))
			}
			rows = append(rows, row)
		}
		w.WriteString(mdTable(head, rows, b.Align...))
	case *statsBlock:
		for _, s := range b.Items {
			line := "- **" + mdEscape(s.Label) + ":** " + mdEscape(string(s.Value))
			if s.Delta != "" {
				line += " (" + mdEscape(string(s.Delta)) + ")"
			}
			if s.Note != "" {
				line += " — " + mdEscape(s.Note)
			}
			w.WriteString(line + "\n")
		}
	case *chartBlock:
		head := []string{"x"}
		for _, s := range b.Series {
			name := s.Name
			if b.Currency != "" {
				name += " (" + b.Currency + ")"
			} else if b.Unit != "" {
				name += " (" + b.Unit + ")"
			}
			head = append(head, name)
		}
		var xs []string
		vals := map[string][]string{}
		for i, s := range b.Series {
			for _, p := range s.Points {
				x := rawText(p.X)
				if _, ok := vals[x]; !ok {
					xs = append(xs, x)
					vals[x] = make([]string, len(b.Series))
				}
				vals[x][i] = number(float64(p.Y))
			}
		}
		var rows [][]string
		for _, x := range xs {
			rows = append(rows, append([]string{x}, vals[x]...))
		}
		w.WriteString(mdTable(head, rows))
		for _, m := range b.Marks {
			fmt.Fprintf(&w, "\n- %s: %s", rawText(m.X), mdEscape(m.Label))
		}
	case *timelineBlock:
		for i, d := range b.Days {
			if i > 0 {
				w.WriteString("\n")
			}
			fmt.Fprintf(&w, "**%s**\n\n", dayLabel(d.Date))
			for _, it := range d.Items {
				line := "- "
				if it.Time != "" {
					line += string(it.Time)
					if it.End != "" {
						line += "–" + string(it.End)
					}
					line += " "
				}
				line += mdEscape(it.Title)
				if it.Place != "" {
					line += " — " + mdEscape(it.Place)
				}
				if it.Note != "" {
					line += " (" + mdEscape(it.Note) + ")"
				}
				w.WriteString(line + "\n")
			}
		}
	case *checklistBlock:
		for _, it := range b.Items {
			box := map[string]string{"done": "[x]", "failed": "[ ] ✗", "skipped": "[ ] (skipped)", "todo": "[ ]"}[it.Status]
			line := "- " + box + " " + mdEscape(it.Text)
			if it.Note != "" {
				line += " — " + mdEscape(it.Note)
			}
			w.WriteString(line + "\n")
		}
	case *receiptBlock:
		head := strings.ToUpper(b.Status[:1]) + b.Status[1:]
		if b.Merchant != "" {
			head += " · " + b.Merchant
		}
		if b.Ref != "" {
			head += " · Ref " + string(b.Ref)
		}
		fmt.Fprintf(&w, "%s\n\n", mdEscape(head))
		var rows [][]string
		for _, l := range b.Lines {
			rows = append(rows, []string{l.Label, string(l.Value)})
		}
		if b.Total != nil {
			rows = append(rows, []string{"**Total**", "**" + money(b.Total.Amount, b.Total.Currency) + "**"})
		}
		w.WriteString(mdTable([]string{"", ""}, rows, "left", "right"))
	case *linksBlock:
		for _, it := range b.Items {
			line := "- [" + mdEscape(it.Title) + "](" + it.URL + ")"
			if it.Note != "" {
				line += " — " + mdEscape(it.Note)
			}
			w.WriteString(line + "\n")
		}
	case *imagesBlock:
		for _, it := range b.Items {
			fmt.Fprintf(&w, "![%s](%s)\n", mdEscape(it.Caption), img(it.Src))
			if it.URL != "" {
				fmt.Fprintf(&w, "[%s](%s)\n", mdEscape(it.Caption), it.URL)
			}
			w.WriteString("\n")
		}
	case *filesBlock:
		for _, f := range b.Items {
			fmt.Fprintf(&w, "- `%s`\n", string(f))
		}
	case *calloutBlock:
		label := map[string]string{"info": "Note", "tip": "Tip", "warning": "Warning", "success": "Done"}[b.Tone]
		fmt.Fprintf(&w, "> **%s:** %s\n", label, mdEscape(b.Text))
	case *markdownBlock:
		w.WriteString(b.Text + "\n")
	}
	return w.String()
}

// mdTable writes a Markdown table.
func mdTable(head []string, rows [][]string, align ...string) string {
	var w strings.Builder
	cell := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }
	w.WriteString("|")
	for _, h := range head {
		w.WriteString(" " + cell(h) + " |")
	}
	w.WriteString("\n|")
	for i := range head {
		a := ""
		if i < len(align) {
			a = align[i]
		}
		switch a {
		case "right":
			w.WriteString(" ---: |")
		case "center":
			w.WriteString(" :---: |")
		default:
			w.WriteString(" --- |")
		}
	}
	w.WriteString("\n")
	for _, r := range rows {
		w.WriteString("|")
		for i := range head {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			w.WriteString(" " + cell(v) + " |")
		}
		w.WriteString("\n")
	}
	return w.String()
}

// mdEscape keeps model text from turning into Markdown it didn't mean.
func mdEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", "&lt;").Replace(s)
}

func hasPrice(items []compareItem) bool {
	for _, it := range items {
		if it.Price != nil {
			return true
		}
	}
	return false
}

func rawText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func stopsText(n int) string {
	if n == 0 {
		return "Non-stop"
	}
	return fmt.Sprintf("%d %s", n, plural("stop", n))
}

// ---------- formatting ----------

var currencySymbols = map[string]string{"USD": "$", "EUR": "€", "GBP": "£", "JPY": "¥", "INR": "₹", "CAD": "CA$", "AUD": "A$"}

// money formats an amount for text: "$279", "€1,299.50", "450 CHF". The
// app formats money itself with the user's locale; this is for text.
func money(amount flexNum, currency string) string {
	a := float64(amount)
	n := number(a)
	if a != math.Trunc(a) && currency != "JPY" {
		n = groupThousands(strconv.FormatFloat(math.Abs(a), 'f', 2, 64))
		if a < 0 {
			n = "-" + n
		}
	}
	if sym, ok := currencySymbols[currency]; ok {
		if strings.HasPrefix(n, "-") {
			return "-" + sym + n[1:]
		}
		return sym + n
	}
	return n + " " + currency
}

// number formats a number without trailing zeros, grouping thousands.
func number(f float64) string {
	s := strconv.FormatFloat(math.Abs(f), 'f', -1, 64)
	if f != math.Trunc(f) {
		s = strconv.FormatFloat(math.Abs(f), 'f', 2, 64)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	s = groupThousands(s)
	if f < 0 {
		s = "-" + s
	}
	return s
}

func groupThousands(s string) string {
	whole, frac, _ := strings.Cut(s, ".")
	if len(whole) >= 4 {
		var b strings.Builder
		for i, r := range whole {
			if i > 0 && (len(whole)-i)%3 == 0 {
				b.WriteByte(',')
			}
			b.WriteRune(r)
		}
		whole = b.String()
	}
	if frac != "" {
		return whole + "." + frac
	}
	return whole
}

// dayLabel shows a date as "Sat, Nov 14" (or the text as it was).
func dayLabel(d string) string {
	if t, err := time.Parse("2006-01-02", d); err == nil {
		return t.Format("Mon, Jan 2")
	}
	if t, err := time.Parse("2006-01-02T15:04", d); err == nil {
		return t.Format("Mon, Jan 2 15:04")
	}
	if t, err := time.Parse(time.RFC3339, d); err == nil {
		return t.Format("Mon, Jan 2 15:04")
	}
	return d
}

func plural(word string, n int) string {
	if n == 1 || word == "series" {
		return word
	}
	return word + "s"
}

// ---------- reports ----------

// ReportPreview is what a report's card in the exchange shows (WP-03): up
// to 3 thumbnails, the first stat, and the kinds of blocks inside.
type ReportPreview struct {
	Thumbs []string     `json:"thumbs,omitempty"`
	Stat   *previewStat `json:"stat,omitempty"`
	Kinds  string       `json:"kinds,omitempty"` // "3 options, comparison, itinerary"
}

type previewStat struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Delta string `json:"delta,omitempty"`
}

// reportPreview builds a report card's preview from its blocks, or nil
// when there's nothing to preview.
func reportPreview(raws []json.RawMessage) *ReportPreview {
	var p ReportPreview
	var kinds []string
	seen := map[string]bool{}
	addKind := func(k string) {
		if k != "" && !seen[k] && len(kinds) < 4 {
			seen[k] = true
			kinds = append(kinds, k)
		}
	}
	thumb := func(src string) {
		if src != "" && len(p.Thumbs) < 3 {
			p.Thumbs = append(p.Thumbs, src)
		}
	}
	for _, b := range parseBlocks(raws) {
		switch b := b.(type) {
		case *cardsBlock:
			addKind(fmt.Sprintf("%d %s", len(b.Items), plural(cardNouns[b.Kind], len(b.Items))))
			for _, it := range b.Items {
				thumb(it.Image)
			}
		case *imagesBlock:
			addKind(fmt.Sprintf("%d %s", len(b.Items), plural("photo", len(b.Items))))
			for _, it := range b.Items {
				thumb(it.Src)
			}
		case *statsBlock:
			if p.Stat == nil && len(b.Items) > 0 {
				s := b.Items[0]
				p.Stat = &previewStat{Label: s.Label, Value: string(s.Value), Delta: string(s.Delta)}
			}
		case *compareBlock:
			addKind("comparison")
		case *tableBlock:
			addKind("table")
		case *chartBlock:
			addKind("chart")
		case *timelineBlock:
			addKind("itinerary")
		case *checklistBlock:
			addKind("checklist")
		case *receiptBlock:
			addKind("receipt")
		case *linksBlock:
			addKind(fmt.Sprintf("%d %s", len(b.Items), plural("link", len(b.Items))))
		case *filesBlock:
			addKind(fmt.Sprintf("%d %s", len(b.Items), plural("file", len(b.Items))))
		}
	}
	p.Kinds = strings.Join(kinds, ", ")
	if len(p.Thumbs) == 0 && p.Stat == nil && p.Kinds == "" {
		return nil
	}
	return &p
}

// reportEntryData is the data of a report's entry in the conversation: its
// id, whether it replaced an earlier version, and what its card previews.
func reportEntryData(rep *store.Report, updated bool) json.RawMessage {
	d := map[string]any{"id": rep.ID, "updated": updated}
	var data ReportData
	if json.Unmarshal(rep.Data, &data) == nil {
		if data.Preview != nil {
			d["preview"] = data.Preview
		}
		if data.Takeaway != "" {
			d["takeaway"] = data.Takeaway
		}
	}
	return marshal(d)
}

// reportNotifyBody is the push body for a new report: its takeaway, the
// start of its text, or a summary of its blocks.
func reportNotifyBody(rep *store.Report) string {
	var data ReportData
	_ = json.Unmarshal(rep.Data, &data)
	switch {
	case data.Takeaway != "":
		return data.Takeaway
	case rep.Format == "markdown" && strings.TrimSpace(rep.Content) != "":
		return trim(plain(rep.Content), 160)
	case len(data.Blocks) > 0:
		return summarizeBlocks(data.Blocks)
	}
	return "Alexa wrote you a report."
}

// ReportMarkdown is a report as a Markdown document, for the .md download:
// its title, takeaway, key rows, text, blocks, events, files and sources.
// Proxied images point back at where they came from, so the file works
// outside the app.
func (rt *Runtime) ReportMarkdown(userID int64, rep *store.Report) string {
	var data ReportData
	_ = json.Unmarshal(rep.Data, &data)
	var parts []string
	parts = append(parts, "# "+rep.Title)
	if data.Takeaway != "" {
		parts = append(parts, "_"+mdEscape(data.Takeaway)+"_")
	}
	if len(data.Items) > 0 {
		var rows []string
		for _, it := range data.Items {
			line := "- **" + mdEscape(it.Label) + "**"
			if it.Detail != "" {
				line += ": " + mdEscape(it.Detail)
			}
			rows = append(rows, line)
		}
		parts = append(parts, strings.Join(rows, "\n"))
	}
	if c := strings.TrimSpace(rep.Content); c != "" && rep.Format != "html" {
		parts = append(parts, c)
	}
	if len(data.Blocks) > 0 {
		parts = append(parts, blocksMarkdown(data.Blocks, rt.imageOrigin(userID)))
	}
	if len(data.Events) > 0 {
		rows := []string{"## Events"}
		for _, e := range data.Events {
			line := "- **" + mdEscape(e.Title) + "**, " + e.Start
			if e.End != "" {
				line += " to " + e.End
			}
			if e.Location != "" {
				line += ", " + mdEscape(e.Location)
			}
			rows = append(rows, line)
		}
		parts = append(parts, strings.Join(rows, "\n"))
	}
	if len(data.Files) > 0 {
		rows := []string{"## Files"}
		for _, f := range data.Files {
			rows = append(rows, "- `"+f+"`")
		}
		parts = append(parts, strings.Join(rows, "\n"))
	}
	if len(data.Sources) > 0 {
		rows := []string{"## Sources"}
		for i, s := range data.Sources {
			title := s.Title
			if title == "" {
				title = hostOf(s.URL)
			}
			rows = append(rows, fmt.Sprintf("%d. [%s](%s)", i+1, mdEscape(title), s.URL))
		}
		parts = append(parts, strings.Join(rows, "\n"))
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// ---------- calendar ----------

// BlockEvents lists the calendar events in blocks: timeline items, event
// cards and stays, for "Add to calendar".
func BlockEvents(raws []json.RawMessage) []CalEvent {
	var out []CalEvent
	for _, b := range parseBlocks(raws) {
		switch b := b.(type) {
		case *timelineBlock:
			for _, d := range b.Days {
				for _, it := range d.Items {
					e := CalEvent{Title: it.Title, Location: it.Place, Description: it.Note, Start: d.Date, AllDay: it.Time == ""}
					if it.Time != "" {
						e.Start = joinDayTime(d.Date, string(it.Time))
					}
					if it.End != "" {
						e.End = joinDayTime(d.Date, string(it.End))
					}
					out = append(out, e)
				}
			}
		case *cardsBlock:
			for _, it := range b.Items {
				switch {
				case it.Start != "":
					out = append(out, CalEvent{Title: it.Title, Start: it.Start, End: it.End, AllDay: !strings.Contains(it.Start, "T"), Location: firstOf(it.Venue, it.Address)})
				case it.CheckIn != "":
					out = append(out, CalEvent{Title: it.Title, Start: it.CheckIn, End: it.CheckOut, AllDay: !strings.Contains(it.CheckIn, "T"), Location: it.Address})
				}
			}
		}
	}
	return out
}

// joinDayTime puts a time of day on a date; a full date-time stays as is.
func joinDayTime(date, clock string) string {
	if strings.Contains(clock, "T") {
		return clock
	}
	if len(date) >= 10 {
		return date[:10] + "T" + clock
	}
	return date
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
