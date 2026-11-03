package agent

// Blocks are the native, declarative UI Alexa can attach to say, ask and
// present_report (SYNTHESIS Appendix A): cards, comparisons, tables, stats,
// charts, itineraries, checklists, receipts, links, images, files, callouts
// and markdown. The model supplies data and the app supplies the design, so
// a block carries no colours, fonts or layout. normalizeBlocks is the only
// way in: it checks every block against the catalog, clamps what can be
// clamped and refuses the rest with a message the model can act on. The Go
// types mirror web/src/api/blocks.ts field for field (snake_case, as the
// model writes them). Summaries, Markdown and report previews are in
// blocks_text.go; the checks for each type are in blocks_check.go.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"alexagent/internal/userenv"
)

// Placement is where a list of blocks is shown, which sets its limits.
type Placement string

const (
	PlaceInline Placement = "inline" // under a say, in the exchange
	PlaceAsk    Placement = "ask"    // context on an ask card
	PlaceReport Placement = "report" // in a report document
)

// placeLimit is how many blocks, and how many bytes of them, a placement
// takes, and the words its refusals use.
type placeLimit struct {
	count int
	bytes int
	verb  string // starts every refusal: "Not sent: …"
	where string // "a table in a message …"
	move  string // what to do with something too big for it
}

var placeLimits = map[Placement]placeLimit{
	PlaceInline: {3, 16 << 10, "Not sent", "a message", "Show the few that matter for the user's choice, or put the full list in present_report and say a one-line takeaway."},
	PlaceAsk:    {2, 8 << 10, "Not asked", "a question", "Put it in present_report first, then ask."},
	// "Not saved" is a prefix the step outcome check reads as failed, so a
	// refused report doesn't count as one shown.
	PlaceReport: {40, 128 << 10, "Not saved", "a report", "Split it into two blocks."},
}

// blockTypes lists the catalog in the order the model is told about it.
var blockTypes = []string{"cards", "compare", "table", "stats", "chart", "timeline", "checklist", "receipt", "links", "images", "files", "callout", "markdown"}

// ---------- tolerant scalars ----------

// The model writes numbers as strings ("279", "1,299.00") and strings as
// numbers ("value": 412) often enough that refusing them would only cost a
// turn, so these types accept either and store the intended form.

var (
	typeFloat  = reflect.TypeOf(0.0)
	typeString = reflect.TypeOf("")
)

// flexNum is a number that may arrive as a numeric string.
type flexNum float64

func (n *flexNum) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = flexNum(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return &json.UnmarshalTypeError{Value: string(b), Type: typeFloat}
	}
	clean := strings.NewReplacer(",", "", " ", "", "$", "", "€", "", "£", "", "¥", "").Replace(strings.TrimSpace(s))
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return &json.UnmarshalTypeError{Value: "string " + strconv.Quote(s), Type: typeFloat}
	}
	*n = flexNum(f)
	return nil
}

func (n *flexNum) int() int { return int(math.Round(float64(*n))) }

// txt is a string that may arrive as a number or a boolean.
type txt string

func (t *txt) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = txt(s)
		return nil
	}
	b = bytes.TrimSpace(b)
	switch {
	case string(b) == "null":
		*t = ""
	case len(b) > 0 && b[0] != '{' && b[0] != '[':
		*t = txt(b)
	default:
		return &json.UnmarshalTypeError{Value: "object or list", Type: typeString}
	}
	return nil
}

// flexBool is a boolean that may arrive as "true" or "false".
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	switch strings.Trim(strings.ToLower(string(bytes.TrimSpace(b))), `"`) {
	case "true", "1", "yes":
		*f = true
	default:
		*f = false
	}
	return nil
}

// ---------- shared types (Appendix A.2) ----------

type Money struct {
	Amount   flexNum `json:"amount"`
	Currency string  `json:"currency"`
}

// Price is a card's price: money, what it was, and what it's per.
type Price struct {
	Amount   flexNum  `json:"amount"`
	Currency string   `json:"currency"`
	Was      *flexNum `json:"was,omitempty"`
	Per      string   `json:"per,omitempty"` // night | person | month | total
}

type Fact struct {
	Label string `json:"label"`
	Value txt    `json:"value"`
}

type Rating struct {
	Value flexNum  `json:"value"`
	OutOf *flexNum `json:"out_of,omitempty"` // 5 when unset
	Count *flexNum `json:"count,omitempty"`
}

// Source is a page Alexa relied on, shown as a favicon stack.
type Source struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
}

// Action is what a card's button does: send a visible reply, open a link
// or open a report. Nothing else: buttons never call tools.
type Action struct {
	Do    string `json:"do"` // reply | open | report
	Label string `json:"label,omitempty"`
	Text  string `json:"text,omitempty"` // reply: sent as the user's message
	URL   string `json:"url,omitempty"`  // open
	ID    txt    `json:"id,omitempty"`   // report: the report's id
}

type Segment struct {
	Carrier string `json:"carrier"`
	Number  txt    `json:"number,omitempty"`
	From    string `json:"from"`
	To      string `json:"to"`
	Depart  string `json:"depart"`
	Arrive  string `json:"arrive"`
}

// Card is one thing in a cards block. The place, stay, event and flight
// fields only matter for that kind.
type Card struct {
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	URL      string   `json:"url,omitempty"`
	Image    string   `json:"image,omitempty"`
	Price    *Price   `json:"price,omitempty"`
	Rating   *Rating  `json:"rating,omitempty"`
	Badges   []string `json:"badges,omitempty"`
	Facts    []Fact   `json:"facts,omitempty"`
	Note     string   `json:"note,omitempty"`
	Pick     flexBool `json:"pick,omitempty"`
	Action   *Action  `json:"action,omitempty"`
	// place
	Address  string    `json:"address,omitempty"`
	Hours    string    `json:"hours,omitempty"`
	OpenNow  *flexBool `json:"open_now,omitempty"`
	Distance txt       `json:"distance,omitempty"`
	Phone    txt       `json:"phone,omitempty"`
	Lat      *flexNum  `json:"lat,omitempty"`
	Lng      *flexNum  `json:"lng,omitempty"`
	// stay
	CheckIn  string `json:"check_in,omitempty"`
	CheckOut string `json:"check_out,omitempty"`
	// event
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	Venue string `json:"venue,omitempty"`
	// flight
	Segments []Segment `json:"segments,omitempty"`
	Duration txt       `json:"duration,omitempty"`
	Stops    *flexNum  `json:"stops,omitempty"`
	Cabin    string    `json:"cabin,omitempty"`
}

// ---------- block types ----------

// blockBase is what every block has.
type blockBase struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`    // ≤32, [a-z0-9-]
	Title string `json:"title,omitempty"` // ≤60
}

func (b *blockBase) base() *blockBase { return b }

type block interface {
	base() *blockBase
	check(c *checker)
}

type cardsBlock struct {
	blockBase
	Kind  string `json:"kind,omitempty"` // product | place | stay | flight | event | item
	Items []Card `json:"items"`
}

type compareItem struct {
	Name  string   `json:"name"`
	Price *Money   `json:"price,omitempty"`
	Pick  flexBool `json:"pick,omitempty"`
}

type compareRow struct {
	Label  string   `json:"label"`
	Values []txt    `json:"values"`
	Best   *flexNum `json:"best,omitempty"`
}

type compareBlock struct {
	blockBase
	Items   []compareItem `json:"items"`
	Rows    []compareRow  `json:"rows"`
	Verdict string        `json:"verdict,omitempty"`
}

type tableBlock struct {
	blockBase
	Columns []txt    `json:"columns"`
	Align   []string `json:"align,omitempty"`
	Rows    [][]txt  `json:"rows"`
}

type stat struct {
	Label string `json:"label"`
	Value txt    `json:"value"`
	Delta txt    `json:"delta,omitempty"`
	Trend string `json:"trend,omitempty"` // up | down | flat
	Good  string `json:"good,omitempty"`  // up | down
	Note  string `json:"note,omitempty"`
}

type statsBlock struct {
	blockBase
	Items []stat `json:"items"`
}

// A chart's x values are dates (x: time) or categories, which may be
// numbers, so they keep the JSON they came in.
type point struct {
	X json.RawMessage `json:"x"`
	Y flexNum         `json:"y"`
}

type series struct {
	Name   string  `json:"name"`
	Points []point `json:"points"`
}

type mark struct {
	X     json.RawMessage `json:"x"`
	Label string          `json:"label"`
}

type chartBlock struct {
	blockBase
	Style    string   `json:"style"` // line | bar
	X        string   `json:"x"`     // time | category
	Currency string   `json:"currency,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Series   []series `json:"series"`
	Marks    []mark   `json:"marks,omitempty"`
}

type timelineItem struct {
	Time  txt    `json:"time,omitempty"`
	End   txt    `json:"end,omitempty"`
	Title string `json:"title"`
	Kind  string `json:"kind,omitempty"` // flight | stay | meal | event | travel | task
	Place string `json:"place,omitempty"`
	Note  string `json:"note,omitempty"`
}

type timelineDay struct {
	Date  string         `json:"date"`
	Items []timelineItem `json:"items"`
}

type timelineBlock struct {
	blockBase
	Days []timelineDay `json:"days"`
}

type checkItem struct {
	Text   string `json:"text"`
	Status string `json:"status"` // done | failed | todo | skipped
	Note   string `json:"note,omitempty"`
}

type checklistBlock struct {
	blockBase
	Items []checkItem `json:"items"`
}

type receiptLine struct {
	Label string `json:"label"`
	Value txt    `json:"value"`
}

type receiptBlock struct {
	blockBase
	Status   string        `json:"status"` // draft | pending | confirmed | cancelled | failed
	Merchant string        `json:"merchant,omitempty"`
	Lines    []receiptLine `json:"lines"`
	Total    *Money        `json:"total"`
	Ref      txt           `json:"ref,omitempty"`
}

type linkItem struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Note  string `json:"note,omitempty"`
}

type linksBlock struct {
	blockBase
	Items []linkItem `json:"items"`
}

type imageItem struct {
	Src     string `json:"src"`
	Caption string `json:"caption"` // also the alt text
	URL     string `json:"url,omitempty"`
}

type imagesBlock struct {
	blockBase
	Items []imageItem `json:"items"`
}

// fileRef is a workspace path. The tool schema describes block items as
// objects, so {"path": "…"} is accepted as well as a plain string; it is
// stored as the string.
type fileRef string

func (f *fileRef) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = fileRef(s)
		return nil
	}
	var o struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return &json.UnmarshalTypeError{Value: string(b), Type: typeString}
	}
	*f = fileRef(o.Path)
	if o.Path == "" {
		*f = fileRef(o.Name)
	}
	return nil
}

type filesBlock struct {
	blockBase
	Items []fileRef `json:"items"`
}

type calloutBlock struct {
	blockBase
	Tone string `json:"tone"` // info | tip | warning | success
	Text string `json:"text"`
}

type markdownBlock struct {
	blockBase
	Text string `json:"text"`
}

// newBlock returns an empty block of a type, or nil for one the catalog
// doesn't have.
func newBlock(typ string) block {
	switch typ {
	case "cards":
		return &cardsBlock{}
	case "compare":
		return &compareBlock{}
	case "table":
		return &tableBlock{}
	case "stats":
		return &statsBlock{}
	case "chart":
		return &chartBlock{}
	case "timeline":
		return &timelineBlock{}
	case "checklist":
		return &checklistBlock{}
	case "receipt":
		return &receiptBlock{}
	case "links":
		return &linksBlock{}
	case "images":
		return &imagesBlock{}
	case "files":
		return &filesBlock{}
	case "callout":
		return &calloutBlock{}
	case "markdown":
		return &markdownBlock{}
	}
	return nil
}

// parseBlock decodes a stored block. Stored blocks were normalized when
// they were shown, so errors here mean the data is from somewhere else.
func parseBlock(raw json.RawMessage) (block, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	b := newBlock(head.Type)
	if b == nil {
		return nil, fmt.Errorf("unknown block type %q", head.Type)
	}
	if err := json.Unmarshal(raw, b); err != nil {
		return nil, err
	}
	return b, nil
}

// parseBlocks decodes stored blocks, skipping any it can't read.
func parseBlocks(raws []json.RawMessage) []block {
	var out []block
	for _, raw := range raws {
		if b, err := parseBlock(raw); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// ---------- normalizeBlocks ----------

// blockEnv is what checking blocks needs from the user's session: their
// time zone, their workspace, their reports and the image proxy. A nil
// func means the thing isn't available, so blocks that need it are left
// out.
type blockEnv struct {
	loc    *time.Location
	file   func(path string) (rel string, ok bool) // an existing workspace file
	image  func(src string) (proxied string, ok bool)
	report func(id string) bool
}

// normalizeBlocks checks the blocks a tool was given against the catalog
// and the placement's limits (Appendix A.3). It returns the blocks as they
// will be stored and shown, notes for the tool result (what was dropped or
// shortened), and an error whose message tells the model exactly how to fix
// the call when something can't be shown as given.
func normalizeBlocks(raw json.RawMessage, place Placement, env blockEnv) ([]json.RawMessage, []string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return nil, nil, nil
	}
	lim := placeLimits[place]
	refuse := func(format string, a ...any) error { return errors.New(lim.verb + ": " + fmt.Sprintf(format, a...)) }
	if len(raw) > 4*lim.bytes {
		return nil, nil, refuse("the blocks are %d KB, and %s can carry %d KB. %s", len(raw)>>10, lim.where, lim.bytes>>10, lim.move)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		// Some models send the array as a JSON string.
		var s string
		if json.Unmarshal(raw, &s) != nil || json.Unmarshal([]byte(s), &list) != nil {
			return nil, nil, refuse("blocks must be a list of block objects, each with a \"type\".")
		}
	}
	if len(list) == 0 {
		return nil, nil, nil
	}
	if len(list) > lim.count {
		hint := "Combine them, or put the details in present_report."
		if place == PlaceReport {
			hint = "Combine some of them."
		}
		return nil, nil, refuse("%s can carry at most %d blocks (this one has %d). %s", lim.where, lim.count, len(list), hint)
	}
	c := &checker{env: env, place: place, lim: lim}
	var out []json.RawMessage
	seen := map[string]int{}
	for i, rb := range list {
		c.path = fmt.Sprintf("blocks[%d]", i)
		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(rb, &head)
		typ := strings.ToLower(strings.TrimSpace(head.Type))
		b := newBlock(typ)
		if b == nil {
			why := fmt.Sprintf("there's no %q block", head.Type)
			if typ == "" {
				why = `it has no "type"`
			}
			// A message or question shown without the block it was written
			// around reads wrong, and the model sends it again with the block
			// fixed, so the user sees it twice. Refuse it whole instead; a
			// report is long enough to keep the rest.
			if place != PlaceReport {
				return nil, nil, refuse("%s: %s. Set \"type\" to one of: %s. Nothing was shown.", c.path, why, strings.Join(blockTypes, ", "))
			}
			c.note("Left out %s: %s. Use one of: %s.", c.path, why, strings.Join(blockTypes, ", "))
			continue
		}
		if err := json.Unmarshal(rb, b); err != nil {
			var te *json.UnmarshalTypeError
			if errors.As(err, &te) && te.Field != "" {
				return nil, nil, refuse("%s.%s has the wrong shape (got %s).", c.path, te.Field, te.Value)
			}
			return nil, nil, refuse("%s isn't a valid %s block: %v.", c.path, typ, err)
		}
		bb := b.base()
		bb.Type = typ
		c.base(bb)
		b.check(c)
		if c.err != "" {
			return nil, nil, errors.New(lim.verb + ": " + c.err)
		}
		seen[typ]++
		out = append(out, marshal(b))
	}
	if place != PlaceReport {
		if n := seen["receipt"]; n > 1 {
			return nil, nil, refuse("%s can show one receipt (this one has %d). Put the others in present_report.", lim.where, n)
		}
		if n := seen["callout"]; n > 1 {
			return nil, nil, refuse("%s can show one callout (this one has %d). Keep the one that matters most.", lim.where, n)
		}
	}
	size := 0
	for _, b := range out {
		size += len(b)
	}
	if size > lim.bytes {
		return nil, nil, refuse("the blocks are %d KB, and %s can carry %d KB. %s", (size+1023)>>10, lim.where, lim.bytes>>10, lim.move)
	}
	notes := c.notes
	if len(c.cut) > 0 {
		notes = append(notes, "Shortened to fit: "+strings.Join(c.cut, ", ")+".")
	}
	return out, notes, nil
}

// checker carries the state of one normalizeBlocks call: where it is, what
// it noted and the first refusal.
type checker struct {
	env   blockEnv
	place Placement
	lim   placeLimit
	path  string   // the block being checked, e.g. "blocks[1]"
	notes []string // for the tool result
	cut   []string // paths of strings shortened to fit
	err   string   // the first refusal, without the verb
}

func (c *checker) fail(format string, a ...any) {
	if c.err == "" {
		c.err = fmt.Sprintf(format, a...)
	}
}

func (c *checker) note(format string, a ...any) { c.notes = append(c.notes, fmt.Sprintf(format, a...)) }

// at names a field of the current block for messages.
func (c *checker) at(field string) string { return c.path + "." + field }

func (c *checker) inline() bool { return c.place != PlaceReport }

// limit refuses a list that's longer than this placement allows.
func (c *checker) limit(subject, unit string, n, max int) bool {
	if n <= max {
		return true
	}
	c.fail("%s in %s can have at most %d %s (this one has %d). %s", subject, c.lim.where, max, unit, n, c.lim.move)
	return false
}

// least refuses a list that's too short to show.
func (c *checker) least(field string, n, min int, what string) bool {
	if n >= min {
		return true
	}
	c.fail("%s needs %s.", c.at(field), what)
	return false
}

// line cleans a one-line string in place: citation marks and extra space
// go, and it's shortened to max characters with an ellipsis.
func (c *checker) line(field string, s *string, max int) {
	*s = c.clean(field, strings.Join(strings.Fields(cleanText(*s)), " "), max)
}

func (c *checker) linet(field string, s *txt, max int) {
	v := string(*s)
	c.line(field, &v, max)
	*s = txt(v)
}

// prose is line for text that keeps its line breaks (markdown).
func (c *checker) prose(field string, s *string, max int) {
	*s = c.clean(field, cleanText(*s), max)
}

func (c *checker) clean(field, v string, max int) string {
	if v != "" && looksLikeCardNumber(v) {
		c.fail("%s has a long run of digits that looks like a card or account number. Mask it (···· 4242) or leave it out.", c.at(field))
	}
	if max > 0 && utf8.RuneCountInString(v) > max {
		v = trim(v, max)
		c.cut = append(c.cut, c.at(field))
	}
	return v
}

// need refuses a missing required string.
func (c *checker) need(field, v, why string) bool {
	if v != "" {
		return true
	}
	c.fail("%s is missing: %s.", c.at(field), why)
	return false
}

// enum lowercases v and checks it against the allowed values. A missing or
// unknown optional value is cleared; a required one is refused.
func (c *checker) enum(field string, v *string, required bool, allowed ...string) {
	*v = strings.ToLower(strings.TrimSpace(*v))
	for _, a := range allowed {
		if *v == a {
			return
		}
	}
	if required {
		got := "nothing"
		if *v != "" {
			got = strconv.Quote(*v)
		}
		c.fail("%s must be one of %s (got %s).", c.at(field), strings.Join(allowed, ", "), got)
		return
	}
	if *v != "" {
		c.note("Left out %s: %q isn't one of %s.", c.at(field), *v, strings.Join(allowed, ", "))
	}
	*v = ""
}

var blockIDRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func (c *checker) base(b *blockBase) {
	b.ID = strings.ToLower(strings.TrimSpace(b.ID))
	if !blockIDRe.MatchString(b.ID) || looksLikeCardNumber(b.ID) {
		b.ID = ""
	}
	c.line("title", &b.Title, 60)
}

// currency checks an ISO 4217 code, accepting lower case.
func (c *checker) currency(field string, v *string) {
	*v = strings.ToUpper(strings.TrimSpace(*v))
	if !currencyRe.MatchString(*v) {
		got := "nothing"
		if *v != "" {
			got = strconv.Quote(*v)
		}
		c.fail("%s must be a 3-letter currency code like USD or EUR (got %s).", c.at(field), got)
	}
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (c *checker) money(field string, m *Money) {
	c.currency(field+".currency", &m.Currency)
}

// link cleans an optional http(s) URL, leaving it out with a note if it
// isn't one.
func (c *checker) link(field string, v *string) {
	*v = strings.TrimSpace(*v)
	if *v == "" {
		return
	}
	if u, ok := webURL(*v); ok {
		*v = u
		return
	}
	c.note("Left out %s: %q isn't an http(s) URL.", c.at(field), trim(*v, 60))
	*v = ""
}

// needLink refuses a missing or non-http(s) URL.
func (c *checker) needLink(field string, v *string) bool {
	*v = strings.TrimSpace(*v)
	if u, ok := webURL(*v); ok {
		*v = u
		return true
	}
	if *v == "" {
		c.fail("%s is missing: it needs an http(s) URL.", c.at(field))
	} else {
		c.fail("%s must be an http(s) URL (got %q).", c.at(field), trim(*v, 60))
	}
	return false
}

// image turns an image reference into what the app loads: a remote https
// image becomes a proxied /api/img URL, and a workspace file its
// workspace-relative path. Anything else is left out with a note.
func (c *checker) image(field string, v *string) bool {
	src := strings.TrimSpace(*v)
	*v = ""
	if src == "" {
		return false
	}
	if imgTokenPath.MatchString(src) {
		*v = src // already proxied, e.g. a block copied from an earlier one
		return true
	}
	if u, err := url.Parse(src); err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "data") {
		if u.Scheme != "https" || u.Host == "" || u.User != nil {
			c.note("Left out %s: images must be https URLs or workspace files.", c.at(field))
			return false
		}
		if c.env.image != nil {
			if p, ok := c.env.image(u.String()); ok {
				*v = p
				return true
			}
		}
		c.note("Left out %s: the image couldn't be prepared.", c.at(field))
		return false
	}
	if c.env.file != nil {
		if rel, ok := c.env.file(src); ok && isImageFile(rel) {
			*v = rel
			return true
		}
	}
	c.note("Left out %s: %q isn't an https URL or an image in the workspace.", c.at(field), trim(src, 60))
	return false
}

var imgTokenPath = regexp.MustCompile(`^/api/img/[0-9a-f]{32}$`)

func isImageFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif":
		return true
	}
	return false
}

// date checks and normalizes a required date or date-time.
func (c *checker) date(field string, v *string, required bool) {
	*v = strings.TrimSpace(*v)
	if *v == "" {
		if required {
			c.fail("%s is missing: it needs a date like 2026-11-14.", c.at(field))
		}
		return
	}
	if d, ok := normDate(*v, c.env.loc); ok {
		*v = d
		return
	}
	c.fail("%s must be a date like 2026-11-14 or 2026-11-14T19:30 (got %q).", c.at(field), trim(*v, 40))
}

// when checks a time of day ("19:30", "7:30 PM") or a date-time.
func (c *checker) when(field string, v *txt, required bool) {
	s := strings.TrimSpace(string(*v))
	*v = txt(s)
	if s == "" {
		if required {
			c.fail("%s is missing: it needs a time like 19:30 or a date-time like 2026-11-14T19:30.", c.at(field))
		}
		return
	}
	if t, ok := normClock(s); ok {
		*v = txt(t)
		return
	}
	if d, ok := normDate(s, c.env.loc); ok && strings.Contains(d, "T") {
		*v = txt(d)
		return
	}
	c.fail("%s must be a time like 19:30 or a date-time like 2026-11-14T19:30 (got %q).", c.at(field), trim(s, 40))
}

// pickOne keeps the first pick in a block and clears the others.
func (c *checker) pickOne(picks []*flexBool) {
	seen := false
	for i, p := range picks {
		if !*p {
			continue
		}
		if seen {
			*p = false
			c.note("Cleared the pick on %s: a block can have one pick.", c.at(fmt.Sprintf("items[%d]", i)))
		}
		seen = true
	}
}

// ---------- values ----------

// webURL returns s if it's an absolute http(s) URL with a host and no
// credentials in it.
func webURL(s string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", false
	}
	return u.String(), true
}

var (
	longDigits  = regexp.MustCompile(`\d{12,}`)
	groupedCard = regexp.MustCompile(`\b(?:\d{4}[ -]){2,3}\d{4}(?:\d{1,3})?\b|\b\d{4}[ -]\d{6}[ -]\d{4,5}\b`)
)

// looksLikeCardNumber is the guard against card and account numbers in
// blocks: any run of 12 or more digits, or digits grouped like a card
// (4242 4242 4242 4242) that pass the Luhn check. Masked numbers
// (···· 4242) pass.
func looksLikeCardNumber(s string) bool {
	if longDigits.MatchString(s) {
		return true
	}
	for _, m := range groupedCard.FindAllString(s, -1) {
		d := strings.NewReplacer(" ", "", "-", "").Replace(m)
		if len(d) >= 13 && len(d) <= 19 && luhn(d) {
			return true
		}
	}
	return false
}

func luhn(d string) bool {
	sum := 0
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if (len(d)-1-i)%2 == 1 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	return sum%10 == 0
}

var (
	localLayouts = []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02 15:04:05"}
	zonedLayouts = []string{time.RFC3339, "2006-01-02T15:04Z07:00", time.RFC3339Nano}
)

// normDate accepts a date, a local date-time (in the user's zone) or a
// date-time with an offset, and returns it in a canonical form:
// "2026-11-14", "2026-11-14T19:30" or RFC 3339.
func normDate(s string, loc *time.Location) (string, bool) {
	if loc == nil {
		loc = time.UTC
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t.Format("2006-01-02"), true
	}
	for _, l := range localLayouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t.Format("2006-01-02T15:04"), true
		}
	}
	for _, l := range zonedLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format(time.RFC3339), true
		}
	}
	return "", false
}

var clockRe = regexp.MustCompile(`(?i)^(\d{1,2})(?:[:.](\d{2}))?(?::\d{2})?\s*([ap]\.?m\.?)?$`)

// normClock accepts a time of day ("19:30", "7:30 PM", "7pm") and returns
// it as 24-hour "19:30".
func normClock(s string) (string, bool) {
	m := clockRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil || (m[2] == "" && m[3] == "") {
		return "", false
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	if ap := strings.ToLower(strings.ReplaceAll(m[3], ".", "")); ap != "" {
		if h < 1 || h > 12 {
			return "", false
		}
		if ap == "pm" && h != 12 {
			h += 12
		}
		if ap == "am" && h == 12 {
			h = 0
		}
	}
	if h > 23 || mi > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", h, mi), true
}

// ---------- the session side: files and images ----------

// blockEnv is the environment blocks are checked in for this tool call.
func (tc *toolCtx) blockEnv() blockEnv {
	r := tc.run
	return blockEnv{
		loc:   r.loc(),
		file:  func(p string) (string, bool) { return workspaceFile(r.env, p) },
		image: func(src string) (string, bool) { return tc.rt.mintImage(r.user.ID, src) },
		report: func(id string) bool {
			_, err := tc.rt.st.GetReport(tc.ctx, r.user.ID, id)
			return err == nil
		},
	}
}

// workspaceFile returns the workspace-relative path of an existing regular
// file. The check goes through the workspace's os.Root (Env.Stat), so a
// symlink can't point a block at something outside the workspace.
func workspaceFile(env *userenv.Env, p string) (string, bool) {
	if env == nil || strings.TrimSpace(p) == "" {
		return "", false
	}
	rel := env.Clean(p)
	if rel == "." {
		return "", false
	}
	fi, err := env.Stat(rel)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// imageToken is the record behind an /api/img token.
type imageToken struct {
	User int64  `json:"user"`
	URL  string `json:"url"`
}

func (rt *Runtime) imageTokenPath(token string) string {
	return filepath.Join(rt.cfg.DataDir, "media", "img", token+".json")
}

// mintImage registers a remote image a block shows and returns the
// same-origin URL the app loads it from, so the user's browser never
// contacts the image's host and /api/img only fetches what Alexa showed.
// The token comes from the user and the URL, so showing the same picture
// again reuses it, and it only resolves for that user.
func (rt *Runtime) mintImage(userID int64, src string) (string, bool) {
	sum := sha256.Sum256([]byte(strconv.FormatInt(userID, 10) + "\n" + src))
	token := hex.EncodeToString(sum[:16])
	path := rt.imageTokenPath(token)
	if _, err := os.Stat(path); err != nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", false
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, marshal(imageToken{User: userID, URL: src}), 0o600); err != nil {
			return "", false
		}
		if err := os.Rename(tmp, path); err != nil {
			return "", false
		}
	} else {
		// Shown again: the sweep keeps it as one just minted, until the
		// entry that shows it is written (imageTokenGrace).
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
	return "/api/img/" + token, true
}

var imageTokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ImageSource returns the remote URL behind an image token minted for this
// user, or ErrNotFound.
func (rt *Runtime) ImageSource(userID int64, token string) (string, error) {
	if !imageTokenRe.MatchString(token) {
		return "", ErrNotFound
	}
	b, err := os.ReadFile(rt.imageTokenPath(token))
	if err != nil {
		return "", ErrNotFound
	}
	var it imageToken
	if json.Unmarshal(b, &it) != nil || it.User != userID || it.URL == "" {
		return "", ErrNotFound
	}
	return it.URL, nil
}

// imageOrigin maps a proxied image back to where it came from, for
// documents that leave the app (the .md download). Other references pass
// through.
func (rt *Runtime) imageOrigin(userID int64) func(string) string {
	return func(src string) string {
		if imgTokenPath.MatchString(src) {
			if u, err := rt.ImageSource(userID, strings.TrimPrefix(src, "/api/img/")); err == nil {
				return u
			}
		}
		return src
	}
}

// promptBlocks tells Alexa when to show results as blocks, which block
// fits, and the rules around them (Appendix C.4). It's static, like every
// section, so the cached prefix stays the same.
const promptBlocks = `# Showing results
say, ask and present_report can carry blocks: native cards, comparisons, tables and more, drawn by the app in its own style in light and dark mode. The text stays the headline (it's what gets pushed and read aloud) and the blocks hold the details, so don't repeat in the text what the blocks show.
- Choose the place by what you're showing:
  - a task done, one answer or a fact: say's text alone;
  - 2 to 8 things to choose between or act on (products, places, stays, flights, events): say with a cards block, plus ask if they must decide now;
  - a head-to-head between 2 to 4 options: compare;
  - a report or document the user asked for, or a long result worth keeping (more than 8 rows, more than 3 blocks of content): present_report with blocks (markdown blocks for the prose between them), then say the takeaway;
  - partial results, or a limit you hit: a checklist of what's done, failed and still to do.
- The blocks, each {"type": …, "title": optional heading}:
  - cards: {kind: product|place|stay|flight|event|item, items: [{title, subtitle, url, image, price: {amount, currency, was, per}, rating: {value, count}, badges, facts: [{label, value}] (up to 4), note, pick, action}]}. Places add address, hours, open_now, phone, distance; stays check_in and check_out; events start, end and venue; flights segments [{carrier, number, from, to, depart, arrive}], duration, stops and cabin.
  - compare: {items: [{name, price, pick}], rows: [{label, values (one per item, in order), best (index of the winner)}], verdict}. Up to 4 items and 8 rows in a message.
  - table: {columns (up to 6), align, rows: [[cells]]}. Up to 8 rows in a message, 40 in a report.
  - stats: {items: [{label, value, delta, trend: up|down|flat, good: up|down, note}]}, up to 6.
  - chart: {style: line|bar, x: time|category, currency or unit, series: [{name, points: [{x, y}]}] (up to 3 series of 60 points), marks: [{x, label}]}.
  - timeline: {days: [{date, items: [{time, end, title, kind: flight|stay|meal|event|travel|task, place, note}]}]}.
  - checklist: {items: [{text, status: done|failed|todo|skipped, note}]}.
  - receipt: {status: draft|pending|confirmed|cancelled|failed, merchant, lines: [{label, value}], total: {amount, currency}, ref}.
  - links: {items: [{title, url, note}]}; images: {items: [{src, caption, url}]}; files: {items: [{path}]}; callout: {tone: info|tip|warning|success, text}; markdown: {text}, in reports only.
- Give money as {amount, currency} with an ISO code, dates as 2026-11-14 or 2026-11-14T19:30 in the user's time, and images as https URLs or files in the workspace. Keep cards to what matters for the choice: the price, the rating and two or three facts.
- Mark at most one pick per block: the one you'd choose, with the reason in its note. In an ask, put the option you recommend in recommended.
- A card's action is {do: "reply", label, text} (a tap sends text as the user's message), {do: "open", url} or {do: "report", id}. A tap, a suggestion or an option is never permission to buy, book, send or delete: before you do any of those, ask with confirm: true and a draft receipt in blocks that shows exactly what will happen and what it costs. Afterwards, say the outcome with the confirmed receipt and its reference. Never give prices or totals only in text when a receipt fits.
- ask can also take multi: true to let the user choose several options, or a small form (up to 6 fields) when you need a few values at once. Never collect passwords, codes or card numbers in a form; use request_login or request_takeover.
- When you used web pages for a message, pass them in say's sources. Only when there's an obvious next step, offer up to 3 suggestions the user can tap, written as they'd say it ("Track the price"). Set final: true on the say or present_report that ends your work on a request.
- Write the text, the blocks and the suggestions in the user's language.`
