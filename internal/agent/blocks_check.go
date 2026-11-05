package agent

// The checks for each block type (Appendix A.3). Each one cleans its block
// in place through the checker: strings are cleaned and shortened, optional
// values that can't be shown are left out with a note, and anything the
// block can't be shown without is refused with a fixable message.

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// cardNouns names a cards block's items by kind, for messages and
// summaries.
var cardNouns = map[string]string{"product": "option", "place": "place", "stay": "stay", "flight": "flight", "event": "event", "item": "option", "": "option"}

func (b *cardsBlock) check(c *checker) {
	c.enum("kind", &b.Kind, false, "product", "place", "stay", "flight", "event", "item")
	if !c.least("items", len(b.Items), 1, "at least one card") || !c.limit("a cards block", "cards", len(b.Items), 8) {
		return
	}
	var picks []*flexBool
	for i := range b.Items {
		it := &b.Items[i]
		f := func(name string) string { return fmt.Sprintf("items[%d].%s", i, name) }
		c.line(f("title"), &it.Title, 80)
		if !c.need(f("title"), it.Title, "every card needs a title") {
			return
		}
		c.line(f("subtitle"), &it.Subtitle, 80)
		c.link(f("url"), &it.URL)
		c.image(f("image"), &it.Image)
		if it.Price != nil {
			c.currency(f("price.currency"), &it.Price.Currency)
			c.enum(f("price.per"), &it.Price.Per, false, "night", "person", "month", "total")
			if it.Price.Was != nil && *it.Price.Was <= it.Price.Amount {
				it.Price.Was = nil
			}
		}
		if it.Rating != nil {
			out := flexNum(5)
			if it.Rating.OutOf != nil && *it.Rating.OutOf > 0 {
				out = *it.Rating.OutOf
			}
			if it.Rating.Value < 0 || it.Rating.Value > out {
				c.note("Left out %s: %g isn't a rating out of %g.", c.at(f("rating")), float64(it.Rating.Value), float64(out))
				it.Rating = nil
			} else if it.Rating.Count != nil && *it.Rating.Count < 0 {
				it.Rating.Count = nil
			}
		}
		if len(it.Badges) > 3 {
			c.note("Kept the first 3 badges of %s.", c.at(fmt.Sprintf("items[%d]", i)))
			it.Badges = it.Badges[:3]
		}
		for j := range it.Badges {
			c.line(f(fmt.Sprintf("badges[%d]", j)), &it.Badges[j], 24)
		}
		it.Badges = compact(it.Badges)
		if len(it.Facts) > 4 {
			c.note("Kept the first 4 facts of %s.", c.at(fmt.Sprintf("items[%d]", i)))
			it.Facts = it.Facts[:4]
		}
		facts := it.Facts[:0]
		for j := range it.Facts {
			fc := it.Facts[j]
			c.line(f(fmt.Sprintf("facts[%d].label", j)), &fc.Label, 24)
			c.linet(f(fmt.Sprintf("facts[%d].value", j)), &fc.Value, 60)
			if fc.Label != "" && fc.Value != "" {
				facts = append(facts, fc)
			}
		}
		it.Facts = facts
		c.line(f("note"), &it.Note, 140)
		picks = append(picks, &it.Pick)
		if it.Action != nil {
			c.action(f("action"), &it.Action)
		}
		// place
		c.line(f("address"), &it.Address, 120)
		c.line(f("hours"), &it.Hours, 60)
		c.linet(f("distance"), &it.Distance, 24)
		c.phone(f("phone"), &it.Phone)
		if it.Lat != nil && it.Lng != nil && (math.Abs(float64(*it.Lat)) > 90 || math.Abs(float64(*it.Lng)) > 180) || (it.Lat == nil) != (it.Lng == nil) {
			it.Lat, it.Lng = nil, nil
		}
		// stay and event
		c.date(f("check_in"), &it.CheckIn, false)
		c.date(f("check_out"), &it.CheckOut, false)
		c.date(f("start"), &it.Start, false)
		c.date(f("end"), &it.End, false)
		c.line(f("venue"), &it.Venue, 80)
		// flight
		if len(it.Segments) > 4 {
			c.fail("%s can have at most 4 flight segments (this one has %d).", c.at(fmt.Sprintf("items[%d]", i)), len(it.Segments))
			return
		}
		for j := range it.Segments {
			s := &it.Segments[j]
			g := func(name string) string { return f(fmt.Sprintf("segments[%d].%s", j, name)) }
			c.line(g("carrier"), &s.Carrier, 40)
			c.linet(g("number"), &s.Number, 12)
			c.line(g("from"), &s.From, 40)
			c.line(g("to"), &s.To, 40)
			if !c.need(g("carrier"), s.Carrier, "every segment needs its carrier") || !c.need(g("from"), s.From, "every segment needs where it leaves from") || !c.need(g("to"), s.To, "every segment needs where it arrives") {
				return
			}
			dep, arr := txt(s.Depart), txt(s.Arrive)
			c.when(g("depart"), &dep, true)
			c.when(g("arrive"), &arr, true)
			s.Depart, s.Arrive = string(dep), string(arr)
		}
		c.linet(f("duration"), &it.Duration, 24)
		if it.Stops != nil && (*it.Stops < 0 || *it.Stops > 9) {
			it.Stops = nil
		}
		c.line(f("cabin"), &it.Cabin, 24)
		if c.err != "" {
			return
		}
	}
	c.pickOne(picks)
}

// phone allows a phone number's digits (up to 15, the E.164 maximum)
// through the card-number guard.
func (c *checker) phone(field string, v *txt) {
	s := strings.Join(strings.Fields(cleanText(string(*v))), " ")
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits > 15 {
		c.note("Left out %s: it doesn't look like a phone number.", c.at(field))
		s = ""
	}
	*v = txt(trim(s, 32))
}

// action checks a card's button. One that can't work is left out.
func (c *checker) action(field string, ap **Action) {
	a := *ap
	a.Do = strings.ToLower(strings.TrimSpace(a.Do))
	c.line(field+".label", &a.Label, 24)
	switch a.Do {
	case "reply":
		c.line(field+".text", &a.Text, 200)
		if a.Text == "" {
			a.Text = a.Label
		}
		if a.Label == "" || a.Text == "" {
			c.note("Left out %s: a reply needs a label and the text it sends.", c.at(field))
			*ap = nil
			return
		}
		a.URL, a.ID = "", ""
	case "open":
		a.URL = strings.TrimSpace(a.URL)
		u, ok := webURL(a.URL)
		if !ok {
			c.note("Left out %s: it needs an http(s) url.", c.at(field))
			*ap = nil
			return
		}
		a.URL, a.Text, a.ID = u, "", ""
	case "report":
		a.ID = txt(strings.TrimSpace(string(a.ID)))
		if a.ID == "" || c.env.report == nil || !c.env.report(string(a.ID)) {
			c.note("Left out %s: there's no report with id %q.", c.at(field), string(a.ID))
			*ap = nil
			return
		}
		a.URL, a.Text = "", ""
	default:
		c.note("Left out %s: do must be reply, open or report.", c.at(field))
		*ap = nil
	}
}

func (b *compareBlock) check(c *checker) {
	maxItems, maxRows := 5, 12
	if c.inline() {
		maxItems, maxRows = 4, 8
	}
	if !c.least("items", len(b.Items), 2, "at least 2 items to compare") || !c.limit("a comparison", "items", len(b.Items), maxItems) {
		return
	}
	if !c.least("rows", len(b.Rows), 1, "at least one row") || !c.limit("a comparison", "rows", len(b.Rows), maxRows) {
		return
	}
	var picks []*flexBool
	for i := range b.Items {
		it := &b.Items[i]
		c.line(fmt.Sprintf("items[%d].name", i), &it.Name, 40)
		if !c.need(fmt.Sprintf("items[%d].name", i), it.Name, "every item needs a name") {
			return
		}
		if it.Price != nil {
			c.money(fmt.Sprintf("items[%d].price", i), it.Price)
		}
		picks = append(picks, &it.Pick)
	}
	c.pickOne(picks)
	for i := range b.Rows {
		row := &b.Rows[i]
		c.line(fmt.Sprintf("rows[%d].label", i), &row.Label, 40)
		if !c.need(fmt.Sprintf("rows[%d].label", i), row.Label, "every row needs a label") {
			return
		}
		if len(row.Values) != len(b.Items) {
			c.fail("%s has %d values for %d items. Give one value per item, in the same order as items.", c.at(fmt.Sprintf("rows[%d].values", i)), len(row.Values), len(b.Items))
			return
		}
		for j := range row.Values {
			c.linet(fmt.Sprintf("rows[%d].values[%d]", i, j), &row.Values[j], 60)
		}
		if row.Best != nil && (row.Best.int() < 0 || row.Best.int() >= len(b.Items)) {
			c.note("Cleared %s: %g isn't the index of an item (0 to %d).", c.at(fmt.Sprintf("rows[%d].best", i)), float64(*row.Best), len(b.Items)-1)
			row.Best = nil
		} else if row.Best != nil {
			*row.Best = flexNum(row.Best.int())
		}
	}
	c.line("verdict", &b.Verdict, 200)
}

func (b *tableBlock) check(c *checker) {
	if !c.least("columns", len(b.Columns), 1, "at least one column") {
		return
	}
	if len(b.Columns) > 6 {
		c.fail("a table can have at most 6 columns (this one has %d). Drop the least useful ones, or split it into two tables.", len(b.Columns))
		return
	}
	if !c.least("rows", len(b.Rows), 1, "at least one row") {
		return
	}
	max := 40
	if c.inline() {
		max = 8
	}
	if !c.limit("a table", "rows", len(b.Rows), max) {
		return
	}
	for i := range b.Columns {
		c.linet(fmt.Sprintf("columns[%d]", i), &b.Columns[i], 40)
	}
	if len(b.Align) > len(b.Columns) {
		b.Align = b.Align[:len(b.Columns)]
	}
	for i := range b.Align {
		c.enum(fmt.Sprintf("align[%d]", i), &b.Align[i], false, "left", "right", "center")
		if b.Align[i] == "" {
			b.Align[i] = "left"
		}
	}
	for i, row := range b.Rows {
		if len(row) > len(b.Columns) {
			c.fail("%s has %d cells for %d columns. Give one cell per column.", c.at(fmt.Sprintf("rows[%d]", i)), len(row), len(b.Columns))
			return
		}
		for len(row) < len(b.Columns) {
			row = append(row, "")
		}
		for j := range row {
			c.linet(fmt.Sprintf("rows[%d][%d]", i, j), &row[j], 120)
		}
		b.Rows[i] = row
	}
}

func (b *statsBlock) check(c *checker) {
	if !c.least("items", len(b.Items), 1, "at least one stat") || !c.limit("a stats block", "stats", len(b.Items), 6) {
		return
	}
	for i := range b.Items {
		s := &b.Items[i]
		f := func(name string) string { return fmt.Sprintf("items[%d].%s", i, name) }
		c.line(f("label"), &s.Label, 24)
		c.linet(f("value"), &s.Value, 24)
		if !c.need(f("label"), s.Label, "every stat needs a label") || !c.need(f("value"), string(s.Value), "every stat needs a value") {
			return
		}
		c.linet(f("delta"), &s.Delta, 16)
		c.enum(f("trend"), &s.Trend, false, "up", "down", "flat")
		c.enum(f("good"), &s.Good, false, "up", "down")
		c.line(f("note"), &s.Note, 60)
	}
}

func (b *chartBlock) check(c *checker) {
	c.enum("style", &b.Style, true, "line", "bar")
	c.enum("x", &b.X, true, "time", "category")
	if b.Currency != "" {
		c.currency("currency", &b.Currency)
	}
	c.line("unit", &b.Unit, 12)
	if c.err != "" || !c.least("series", len(b.Series), 1, "at least one series") || !c.limit("a chart", "series", len(b.Series), 3) {
		return
	}
	for i := range b.Series {
		s := &b.Series[i]
		c.line(fmt.Sprintf("series[%d].name", i), &s.Name, 32)
		if !c.need(fmt.Sprintf("series[%d].name", i), s.Name, "every series needs a name") ||
			!c.least(fmt.Sprintf("series[%d].points", i), len(s.Points), 1, "at least one point") ||
			!c.limit("a chart series", "points", len(s.Points), 60) {
			return
		}
		for j := range s.Points {
			c.chartX(fmt.Sprintf("series[%d].points[%d].x", i, j), &s.Points[j].X, b.X)
		}
	}
	if len(b.Marks) > 5 {
		c.note("Kept the first 5 marks of %s.", c.path)
		b.Marks = b.Marks[:5]
	}
	for i := range b.Marks {
		c.chartX(fmt.Sprintf("marks[%d].x", i), &b.Marks[i].X, b.X)
		c.line(fmt.Sprintf("marks[%d].label", i), &b.Marks[i].Label, 40)
	}
}

// chartX checks an x value: a date on a time axis, a short label or a
// number on a category axis.
func (c *checker) chartX(field string, x *json.RawMessage, axis string) {
	var s string
	var n float64
	switch {
	case json.Unmarshal(*x, &s) == nil:
		if axis == "time" {
			c.date(field, &s, true)
		} else {
			c.line(field, &s, 24)
			if s == "" {
				c.fail("%s is missing: every point needs an x.", c.at(field))
			}
		}
		*x = marshal(s)
	case json.Unmarshal(*x, &n) == nil && axis != "time":
		*x = marshal(n)
	case axis == "time":
		c.fail("%s must be a date like 2026-11-14 on a time axis.", c.at(field))
	default:
		c.fail("%s must be a label or a number.", c.at(field))
	}
}

func (b *timelineBlock) check(c *checker) {
	if !c.least("days", len(b.Days), 1, "at least one day") {
		return
	}
	total := 0
	for _, d := range b.Days {
		total += len(d.Items)
	}
	max := 60
	if c.inline() {
		max = 10
	}
	if !c.limit("a timeline", "items", total, max) {
		return
	}
	for i := range b.Days {
		d := &b.Days[i]
		c.date(fmt.Sprintf("days[%d].date", i), &d.Date, true)
		if !c.least(fmt.Sprintf("days[%d].items", i), len(d.Items), 1, "at least one item") {
			return
		}
		for j := range d.Items {
			it := &d.Items[j]
			f := func(name string) string { return fmt.Sprintf("days[%d].items[%d].%s", i, j, name) }
			c.when(f("time"), &it.Time, false)
			c.when(f("end"), &it.End, false)
			c.line(f("title"), &it.Title, 80)
			if !c.need(f("title"), it.Title, "every item needs a title") {
				return
			}
			c.enum(f("kind"), &it.Kind, false, "flight", "stay", "meal", "event", "travel", "task")
			c.line(f("place"), &it.Place, 80)
			c.line(f("note"), &it.Note, 140)
		}
		if c.err != "" {
			return
		}
	}
}

func (b *checklistBlock) check(c *checker) {
	max := 20
	if c.inline() {
		max = 12
	}
	if !c.least("items", len(b.Items), 1, "at least one item") || !c.limit("a checklist", "items", len(b.Items), max) {
		return
	}
	for i := range b.Items {
		it := &b.Items[i]
		c.line(fmt.Sprintf("items[%d].text", i), &it.Text, 120)
		if !c.need(fmt.Sprintf("items[%d].text", i), it.Text, "every item needs its text") {
			return
		}
		c.enum(fmt.Sprintf("items[%d].status", i), &it.Status, true, "done", "failed", "todo", "skipped")
		c.line(fmt.Sprintf("items[%d].note", i), &it.Note, 80)
	}
}

func (b *receiptBlock) check(c *checker) {
	c.enum("status", &b.Status, true, "draft", "pending", "confirmed", "cancelled", "failed")
	c.line("merchant", &b.Merchant, 60)
	if c.err != "" || !c.least("lines", len(b.Lines), 1, "at least one line") || !c.limit("a receipt", "lines", len(b.Lines), 12) {
		return
	}
	for i := range b.Lines {
		l := &b.Lines[i]
		c.line(fmt.Sprintf("lines[%d].label", i), &l.Label, 60)
		c.linet(fmt.Sprintf("lines[%d].value", i), &l.Value, 60)
		if !c.need(fmt.Sprintf("lines[%d].label", i), l.Label, "every line needs a label") {
			return
		}
	}
	if b.Total == nil {
		c.fail("%s is missing: a receipt needs its total as {amount, currency}.", c.at("total"))
		return
	}
	c.money("total", b.Total)
	c.linet("ref", &b.Ref, 40)
}

func (b *linksBlock) check(c *checker) {
	max := 12
	if c.inline() {
		max = 6
	}
	if !c.least("items", len(b.Items), 1, "at least one link") || !c.limit("a links block", "links", len(b.Items), max) {
		return
	}
	for i := range b.Items {
		it := &b.Items[i]
		if !c.needLink(fmt.Sprintf("items[%d].url", i), &it.URL) {
			return
		}
		c.line(fmt.Sprintf("items[%d].title", i), &it.Title, 80)
		if it.Title == "" {
			it.Title = hostOf(it.URL)
		}
		c.line(fmt.Sprintf("items[%d].note", i), &it.Note, 80)
	}
}

func (b *imagesBlock) check(c *checker) {
	max := 12
	if c.inline() {
		max = 4
	}
	if !c.least("items", len(b.Items), 1, "at least one image") || !c.limit("an images block", "images", len(b.Items), max) {
		return
	}
	kept := b.Items[:0]
	for i := range b.Items {
		it := b.Items[i]
		c.line(fmt.Sprintf("items[%d].caption", i), &it.Caption, 80)
		if !c.need(fmt.Sprintf("items[%d].caption", i), it.Caption, "every image needs a caption (it's also the alt text)") {
			return
		}
		c.link(fmt.Sprintf("items[%d].url", i), &it.URL)
		if c.image(fmt.Sprintf("items[%d].src", i), &it.Src) {
			kept = append(kept, it)
		}
	}
	if len(kept) == 0 {
		c.fail("none of the images in %s could be shown. Use https image URLs or images saved in the workspace.", c.path)
		return
	}
	b.Items = kept
}

func (b *filesBlock) check(c *checker) {
	max := 12
	if c.inline() {
		max = 6
	}
	if !c.least("items", len(b.Items), 1, "at least one file") || !c.limit("a files block", "files", len(b.Items), max) {
		return
	}
	var kept []fileRef
	var missing []string
	for _, f := range b.Items {
		p := strings.TrimSpace(string(f))
		if c.env.file != nil {
			if rel, ok := c.env.file(p); ok {
				kept = append(kept, fileRef(rel))
				continue
			}
		}
		missing = append(missing, p)
	}
	if len(kept) == 0 {
		c.fail("none of the files in %s exist in the workspace: %s.", c.path, strings.Join(missing, ", "))
		return
	}
	if len(missing) > 0 {
		c.note("Left out of %s, as they don't exist: %s.", c.path, strings.Join(missing, ", "))
	}
	b.Items = kept
}

func (b *calloutBlock) check(c *checker) {
	if strings.TrimSpace(b.Tone) == "" {
		b.Tone = "info"
	}
	c.enum("tone", &b.Tone, true, "info", "tip", "warning", "success")
	c.line("text", &b.Text, 280)
	c.need("text", b.Text, "a callout needs its text")
}

func (b *markdownBlock) check(c *checker) {
	if c.inline() {
		c.fail("markdown blocks only go in reports. Keep the message text plain, and put longer writing in present_report.")
		return
	}
	c.prose("text", &b.Text, 0)
	c.need("text", b.Text, "a markdown block needs its text")
}

// compact drops empty strings.
func compact(list []string) []string {
	out := list[:0]
	for _, s := range list {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// hostOf is a URL's host without "www.", for labels.
func hostOf(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return strings.TrimPrefix(strings.ToLower(u), "www.")
}
