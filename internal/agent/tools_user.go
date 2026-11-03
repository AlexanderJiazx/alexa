package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"alexagent/internal/store"
)

// userTools reach the user: a take-over request, messages, questions and
// reports. Alexa's plain text is hidden, so these are all the user sees of it.
func userTools() []*tool {
	return []*tool{
		{
			Name:        "request_takeover",
			Description: "Pause and ask the user to take over the shared browser — for 2FA codes, CAPTCHAs, payment details, or anything else only they can do on the page (for signing in, use browser_login and request_login). Their screen switches to the browser. You resume when they hand back control. Calls after this one in the same turn don't run; call them again after the user answers.",
			Params:      map[string]any{"reason": p("string", "Short instruction shown to the user, e.g. \"Sign in to continue\"")},
			Required:    []string{"reason"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				tc.run.replied()
				return &toolResult{Suspend: &Needs{Kind: "takeover", Message: trim(str(a, "reason"), 200)}}, nil
			},
		},
		{
			Name:        "say",
			Description: "Send the user a short message. This is the only way the user sees anything you write: your plain text output is hidden. For a task you've done, a few words: \"Done.\" or \"Done. <the state they need>\". Otherwise one or two plain sentences, under 300 characters, no headings, lists or tables. Attach blocks only for things to choose between, compare or check (see Showing results). Messages are pushed to the user's phone automatically when they're away.",
			NoStatus:    true,
			Params: map[string]any{
				"text":        p("string", "The message, as you'd say it aloud"),
				"push":        p("boolean", "Also push it to their phone even if they have Alexa open (alerts, reminders)"),
				"email":       p("boolean", "Also email it (only when they asked for email)"),
				"blocks":      blocksParam("Up to 3 blocks shown under the message"),
				"sources":     sourcesParam("Web pages you relied on for this message, up to 6"),
				"suggestions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Up to 3 short follow-ups the user might tap, in their words, under 40 characters each (e.g. \"Track the price\")"},
				"final":       p("boolean", "True when this message ends your work on the request"),
			},
			Required: []string{"text"},
			Run:      say,
		},
		{
			Name:        "ask",
			Description: "Ask the user a question and wait for the answer — to confirm consequential actions (purchases, sending, booking, deleting) or to resolve real ambiguity. Short and self-contained, under 300 characters. Offer options when there are clear choices, and mark the one you'd pick with recommended. Before buying, booking or sending, set confirm and put a draft receipt in blocks. Calls after this one in the same turn don't run; call them again after the user answers.",
			NoStatus:    true,
			Params: map[string]any{
				"question":    p("string", "The question"),
				"options":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional short answers to choose from (2–4, a few words each)"},
				"recommended": p("integer", "Index into options of the answer you recommend; it's drawn as the main button"),
				"multi":       p("boolean", "Let the user choose several options, then Done; the answer lists them"),
				"confirm":     p("boolean", "Show a confirm card before you buy, book, send or delete, with a draft receipt in blocks"),
				"form":        formParam(),
				"blocks":      blocksParam("Up to 2 blocks of context shown with the question: a draft receipt, or the cards being chosen between"),
			},
			Required: []string{"question"},
			Run:      ask,
		},
		{
			Name: "present_report",
			Description: "Show the user a report: a document that opens on their screen and stays in their reports. Use it only when the user asks for a report, document or write-up, or when the result is long and worth keeping (in-depth research, a multi-day itinerary, a draft). Never use it to sum up work you did: say the outcome instead. " +
				"Write well-structured markdown (headings, short paragraphs, lists, tables, links), add blocks for options, comparisons, numbers and itineraries, or build it from blocks alone. Afterwards, say a one-line takeaway. To revise a report you showed earlier, pass its id.",
			NoStatus: true,
			Params: map[string]any{
				"title":   p("string", "Short title that states what it is or the outcome, e.g. \"Flights to Lisbon under $400\""),
				"content": p("string", "The report in markdown (or a full HTML document when format is html). Optional when you give blocks. Don't repeat the title as a heading; it's shown above the content."),
				"format": map[string]any{"type": "string", "enum": []string{"markdown", "html"}, "description": "markdown (default), or html for a self-contained page with its own charts or interactivity. It runs sandboxed: scripts may load libraries from cdn.jsdelivr.net, cdnjs.cloudflare.com or unpkg.com and images from https URLs, but can't fetch data, so embed the data in the page. " +
					"Style it with the app's CSS variables so it matches light and dark mode: var(--ink) for text, var(--text-2) and var(--text-3) for secondary text, var(--surface) and var(--tile) for panels, var(--line) for borders, var(--ok), var(--warn) and var(--bad) only for status, var(--series-1) to var(--series-3) for chart series. Keep it flat and monochrome: no shadows, gradients or accent colours. Don't set fonts or a page background. " +
					"Classes .card, .grid, .stat, .label, .muted and button.primary are ready to use. A script can read a colour with getComputedStyle(document.documentElement).getPropertyValue('--ink') and redraw on the window 'alexa:theme' event. " +
					"A button may call alexa.send(text) to send the user's choice as their own visible message (only from a tap, at most 300 characters, one every 2 seconds). It never answers a confirm card: confirm with ask. Never name a top-level script variable status, name or top."},
				"id": p("string", "Id of an earlier report to replace with this version"),
				"items": map[string]any{"type": "array", "description": "Optional key rows shown at the top, one per thing handled", "items": map[string]any{"type": "object", "properties": map[string]any{
					"label": p("string", "Item"), "detail": p("string", "Short detail like a date, price or status")}, "required": []string{"label"}}},
				"events": map[string]any{"type": "array", "description": "Calendar events the user may want to add", "items": map[string]any{"type": "object", "properties": map[string]any{
					"title": p("string", "Event title"), "start": p("string", "Start, RFC3339 with offset, or YYYY-MM-DD for all-day"), "end": p("string", "End (optional)"),
					"all_day": p("boolean", "All-day event"), "location": p("string", "Location"), "description": p("string", "Notes")}, "required": []string{"title", "start"}}},
				"files":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Workspace files to attach (images are shown inline)"},
				"sources":  sourcesParam("Sources and important links"),
				"blocks":   blocksParam("Up to 40 blocks shown after the content; markdown blocks hold the prose between them"),
				"takeaway": p("string", "The one-line takeaway shown on the report's card, under 200 characters"),
				"final":    p("boolean", "True when this report ends your work on the request"),
			},
			Required: []string{"title"},
			Run:      presentReport,
		},
	}
}

// ---------- schemas ----------

// blocksParam is the schema of a blocks parameter. It's typed loosely on
// purpose: an object with a type and the fields the types use, with no
// oneOf (the model endpoint's support for it is unverified). The exact
// shapes are in the Showing results section of the instructions, and
// normalizeBlocks enforces them.
func blocksParam(desc string) map[string]any {
	list := func(d string) map[string]any {
		return map[string]any{"type": "array", "description": d, "items": map[string]any{"type": "object"}}
	}
	enum := func(d string, vals ...string) map[string]any {
		return map[string]any{"type": "string", "enum": vals, "description": d}
	}
	return map[string]any{
		"type":        "array",
		"description": desc + " (see Showing results)",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":     enum("The block type", blockTypes...),
				"title":    p("string", "Optional heading, under 60 characters"),
				"kind":     enum("cards: what the cards are", "product", "place", "stay", "flight", "event", "item"),
				"items":    map[string]any{"type": "array", "items": map[string]any{}, "description": "cards: the cards; compare: [{name, price, pick}]; stats, checklist, links and images: their entries; files: [{path}]"},
				"rows":     map[string]any{"type": "array", "items": map[string]any{}, "description": "table: one list of cells per row; compare: [{label, values, best}]"},
				"columns":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "table: the column headings (up to 6)"},
				"align":    map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"left", "right", "center"}}, "description": "table: how each column is aligned"},
				"verdict":  p("string", "compare: the conclusion in one line"),
				"style":    enum("chart", "line", "bar"),
				"x":        enum("chart: what the x axis shows", "time", "category"),
				"currency": p("string", "chart: the ISO currency of the values"),
				"unit":     p("string", "chart: the unit of the values"),
				"series":   list("chart: up to 3 of {name, points: [{x, y}]}"),
				"marks":    list("chart: up to 5 of {x, label}"),
				"days":     list("timeline: [{date, items: [{time, end, title, kind, place, note}]}]"),
				"status":   enum("receipt", "draft", "pending", "confirmed", "cancelled", "failed"),
				"merchant": p("string", "receipt: who it's from"),
				"lines":    list("receipt: [{label, value}]"),
				"total": map[string]any{"type": "object", "description": "receipt: the total", "properties": map[string]any{
					"amount": p("number", "Amount"), "currency": p("string", "ISO currency code, e.g. USD")}},
				"ref":  p("string", "receipt: the order or booking reference"),
				"tone": enum("callout", "info", "tip", "warning", "success"),
				"text": p("string", "callout: under 280 characters; markdown (reports only): the prose"),
			},
			"required": []string{"type"},
		},
	}
}

func sourcesParam(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "object", "properties": map[string]any{
		"title": p("string", "Title"), "url": p("string", "URL")}, "required": []string{"url"}}}
}

func formParam() map[string]any {
	num := func(d string) map[string]any { return p("number", d) }
	return map[string]any{
		"type":        "object",
		"description": "A small form (up to 6 fields) when you need a few values at once. The answer arrives as [Form] name=value; …. Never ask for passwords, codes or card numbers in a form.",
		"properties": map[string]any{
			"submit": p("string", "The submit button's label, e.g. \"Search\""),
			"fields": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"name":        p("string", "The key its value is sent under, lowercase_with_underscores"),
				"label":       p("string", "The label, under 24 characters"),
				"type":        map[string]any{"type": "string", "enum": formTypes},
				"value":       p("string", "The default value"),
				"required":    p("boolean", "It must be filled in"),
				"min":         num("number and stepper: the smallest value"),
				"max":         num("number and stepper: the largest value"),
				"step":        num("number and stepper: the step"),
				"options":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "select and multiselect: the choices (up to 8)"},
				"placeholder": p("string", "A hint shown while the field is empty"),
			}, "required": []string{"name", "label", "type"}}},
		},
		"required": []string{"fields"},
	}
}

// rawArg is a tool argument as JSON, for the parts checked by type.
func rawArg(a map[string]any, k string) json.RawMessage {
	v, ok := a[k]
	if !ok || v == nil {
		return nil
	}
	return marshal(v)
}

// ---------- say ----------

// Messages must stay glanceable; longer content belongs in a report.
const (
	sayHint = 300 // what the model is told
	sayMax  = 400 // what is refused
)

// sayData is what a say entry carries besides its text.
type sayData struct {
	Blocks      []json.RawMessage `json:"blocks,omitempty"`
	Summary     string            `json:"summary,omitempty"` // of the blocks, ≤160
	Sources     []Source          `json:"sources,omitempty"`
	Suggestions []string          `json:"suggestions,omitempty"`
	Final       bool              `json:"final,omitempty"`
}

func say(tc *toolCtx, a map[string]any) (*toolResult, error) {
	text := cleanText(str(a, "text"))
	if text == "" {
		return nil, errors.New("empty message")
	}
	if n := utf8.RuneCountInString(text); n > sayMax {
		return &toolResult{Text: fmt.Sprintf("Not sent: %d characters is too long for a message. Say it in under %d characters: the outcome or the answer, without how you got there. Only if the user needs the details, put them in a report (present_report) and say a one-line takeaway.", n, sayHint)}, nil
	}
	blocks, notes, err := normalizeBlocks(rawArg(a, "blocks"), PlaceInline, tc.blockEnv())
	if err != nil {
		return &toolResult{Text: err.Error()}, nil
	}
	sources, more := normalizeSources(a["sources"], 6)
	notes = append(notes, more...)
	suggestions, more, err := normalizeSuggestions(strList(a, "suggestions"))
	if err != nil {
		return &toolResult{Text: err.Error()}, nil
	}
	notes = append(notes, more...)
	d := sayData{Blocks: blocks, Sources: sources, Suggestions: suggestions, Final: boolean(a, "final", false)}
	if len(blocks) > 0 {
		d.Summary = summarizeBlocks(blocks)
	}
	r := tc.run
	e := &store.Entry{UserID: r.user.ID, Kind: "say", Text: text}
	if len(blocks) > 0 || len(sources) > 0 || len(suggestions) > 0 || d.Final {
		e.Data = marshal(d)
	}
	if err := tc.rt.addEntry(tc.ctx, e); err != nil {
		return nil, err
	}
	r.replied()
	body := text
	if d.Summary != "" {
		body += "\n" + d.Summary
	}
	result := shownText("Said.", blocks, len(sources), notes)
	if err := tc.rt.notify(tc.ctx, r.user, "", "Alexa", body, boolean(a, "push", false), boolean(a, "email", false)); err != nil {
		return &toolResult{Text: result + " The notification failed: " + err.Error()}, nil
	}
	return &toolResult{Text: result}, nil
}

// shownText is a tool result that echoes what the user now sees: "Said.
// Shown: 4 product cards (pick: Sony WH-1000XM6), 1 source." plus notes.
func shownText(lead string, blocks []json.RawMessage, sources int, notes []string) string {
	var shown []string
	if s := echoBlocks(blocks); s != "" {
		shown = append(shown, s)
	}
	if sources > 0 {
		shown = append(shown, fmt.Sprintf("%d %s", sources, plural("source", sources)))
	}
	out := lead
	if len(shown) > 0 {
		out += " Shown: " + strings.Join(shown, ", ") + "."
	}
	for _, n := range notes {
		out += " " + n
	}
	return out
}

// normalizeSources keeps up to max http(s) sources, once each.
func normalizeSources(v any, max int) ([]Source, []string) {
	arr, _ := v.([]any)
	var out []Source
	var notes []string
	seen := map[string]bool{}
	for i, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			if s, isStr := x.(string); isStr {
				m = map[string]any{"url": s}
			} else {
				continue
			}
		}
		u, ok := webURL(str(m, "url"))
		if !ok {
			notes = append(notes, fmt.Sprintf("Left out sources[%d]: it needs an http(s) url.", i))
			continue
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		if len(out) == max {
			notes = append(notes, fmt.Sprintf("Kept the first %d sources.", max))
			break
		}
		title := trim(strings.Join(strings.Fields(cleanText(str(m, "title"))), " "), 80)
		if looksLikeCardNumber(title) {
			title = "" // the app shows the host instead
		}
		out = append(out, Source{Title: title, URL: u})
	}
	return out, notes
}

// normalizeSuggestions keeps up to 3 short, distinct follow-ups.
func normalizeSuggestions(list []string) ([]string, []string, error) {
	var out, notes []string
	seen := map[string]bool{}
	for i, s := range list {
		s = strings.Join(strings.Fields(cleanText(s)), " ")
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		if looksLikeCardNumber(s) {
			return nil, nil, fmt.Errorf("Not sent: suggestions[%d] has a long run of digits that looks like a card or account number. Leave it out.", i)
		}
		if len(out) == 3 {
			notes = append(notes, "Kept the first 3 suggestions.")
			break
		}
		if utf8.RuneCountInString(s) > 40 {
			s = trim(s, 40)
			notes = append(notes, fmt.Sprintf("Shortened suggestions[%d] to 40 characters.", i))
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out, notes, nil
}

// ---------- ask ----------

func ask(tc *toolCtx, a map[string]any) (*toolResult, error) {
	q := cleanText(str(a, "question"))
	if q == "" {
		return nil, errors.New("empty question")
	}
	if n := utf8.RuneCountInString(q); n > sayMax {
		return &toolResult{Text: fmt.Sprintf("Not asked: %d characters is too long. Ask in under %d characters; put background in a report first if needed.", n, sayHint)}, nil
	}
	var opts []string
	for _, o := range strList(a, "options") {
		if len(opts) < 4 {
			opts = append(opts, trim(strings.TrimSpace(o), 60))
		}
	}
	env := tc.blockEnv()
	// Notes about clamped blocks can't reach the model here: an ask's result
	// is the user's answer. Anything that matters is refused instead.
	blocks, _, err := normalizeBlocks(rawArg(a, "blocks"), PlaceAsk, env)
	if err != nil {
		return &toolResult{Text: err.Error()}, nil
	}
	form, err := normalizeForm(rawArg(a, "form"), env.loc)
	if err != nil {
		return &toolResult{Text: err.Error()}, nil
	}
	nd := &Needs{Kind: "question", Message: q, Options: opts, Blocks: blocks, Form: form, Confirm: boolean(a, "confirm", false)}
	if boolean(a, "multi", false) && form == nil {
		if len(opts) < 2 {
			return &toolResult{Text: "Not asked: multi needs 2 to 4 options to choose from."}, nil
		}
		nd.Multi = true
	}
	if _, ok := a["recommended"]; ok && !nd.Multi {
		if i := int(num(a, "recommended", -1)); i >= 0 && i < len(opts) {
			nd.Recommended = &i
		}
	}
	if nd.Confirm {
		for _, b := range parseBlocks(blocks) {
			if rc, ok := b.(*receiptBlock); ok && rc.Status != "draft" && rc.Status != "pending" {
				return &toolResult{Text: fmt.Sprintf("Not asked: the receipt on a confirm card shows what will happen, so its status should be draft (it's %s). Say the confirmed receipt after you've done it.", rc.Status)}, nil
			}
		}
		if len(opts) == 0 && form == nil {
			nd.Options = []string{"Confirm", "Cancel"}
		}
	}
	tc.run.replied()
	return &toolResult{Suspend: nd}, nil
}

// askEntryData is the data of an ask's entry: its options and its rich
// parts, so the conversation and Reflect can show the card as it was asked.
func askEntryData(nd *Needs) json.RawMessage {
	d := map[string]any{}
	if len(nd.Options) > 0 {
		d["options"] = nd.Options
	}
	if len(nd.Blocks) > 0 {
		d["blocks"] = nd.Blocks
		d["summary"] = summarizeBlocks(nd.Blocks)
	}
	if nd.Form != nil {
		d["form"] = nd.Form
	}
	if nd.Multi {
		d["multi"] = true
	}
	if nd.Recommended != nil {
		d["recommended"] = *nd.Recommended
	}
	if nd.Confirm {
		d["confirm"] = true
	}
	if len(d) == 0 {
		return nil
	}
	return marshal(d)
}

// AskForm is a small form on an ask card (Appendix A.4).
type AskForm struct {
	Submit string      `json:"submit,omitempty"`
	Fields []FormField `json:"fields"`
}

type FormField struct {
	Name        string   `json:"name"` // [a-z_]{1,24}
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Value       any      `json:"value,omitempty"` // string, number, bool or []string, by type
	Required    flexBool `json:"required,omitempty"`
	Min         *flexNum `json:"min,omitempty"`
	Max         *flexNum `json:"max,omitempty"`
	Step        *flexNum `json:"step,omitempty"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

var (
	formTypes   = []string{"text", "number", "date", "time", "datetime", "select", "multiselect", "toggle", "stepper"}
	fieldNameRe = regexp.MustCompile(`^[a-z_]{1,24}$`)
	// credentialRe matches what a form must never collect: passwords, PINs,
	// card details, SSNs and one-time codes. Those go through request_login
	// (the vault) or request_takeover (the user, on the site itself).
	credentialRe = regexp.MustCompile(`(?i)\b(pass ?(word|code|phrase)|passwd|pwd|pin|pin ?code|cvv2?|cvc2?|csc|security ?code|card ?(number|no|num)|credit ?card|debit ?card|ssn|social ?security|2fa|two ?factor|mfa|otp|one ?time ?(code|password|passcode)|verification ?code|auth(entication)? ?code|login ?code)\b`)
)

// normalizeForm checks an ask's form: up to 6 typed fields with short
// labels, defaults that fit their type, and nothing that collects
// credentials.
func normalizeForm(raw json.RawMessage, loc *time.Location) (*AskForm, error) {
	if s := strings.TrimSpace(string(raw)); s == "" || s == "null" {
		return nil, nil
	}
	refuse := func(format string, a ...any) error { return errors.New("Not asked: " + fmt.Sprintf(format, a...)) }
	var f AskForm
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, refuse("form must be {submit, fields: [{name, label, type, …}]}.")
	}
	if len(f.Fields) == 0 {
		return nil, refuse("form.fields is missing: a form needs at least one field.")
	}
	if len(f.Fields) > 6 {
		return nil, refuse("a form can have at most 6 fields (this one has %d). Ask for the rest afterwards.", len(f.Fields))
	}
	c := &checker{env: blockEnv{loc: loc}, place: PlaceAsk, lim: placeLimits[PlaceAsk], path: "form"}
	c.line("submit", &f.Submit, 24)
	names := map[string]bool{}
	for i := range f.Fields {
		fd := &f.Fields[i]
		at := fmt.Sprintf("fields[%d]", i)
		fd.Name = fieldName(fd.Name)
		c.line(at+".label", &fd.Label, 24)
		c.line(at+".placeholder", &fd.Placeholder, 40)
		if fd.Name == "" {
			fd.Name = fieldName(fd.Label)
		}
		if fd.Label == "" {
			fd.Label = labelFor(fd.Name)
		}
		for _, s := range []string{fd.Name, fd.Label, fd.Placeholder} {
			if credentialRe.MatchString(strings.NewReplacer("_", " ", "-", " ").Replace(s)) {
				return nil, refuse("form.%s (%s) asks for a credential. Never collect this in a form; use request_login or request_takeover.", at, fd.Name)
			}
		}
		if !fieldNameRe.MatchString(fd.Name) {
			return nil, refuse("form.%s.name must be lowercase letters and underscores, up to 24 (got %q).", at, fd.Name)
		}
		if names[fd.Name] {
			return nil, refuse("form.%s.name %q is used twice; every field needs its own name.", at, fd.Name)
		}
		names[fd.Name] = true
		if strings.TrimSpace(fd.Type) == "" {
			fd.Type = "text"
		}
		c.enum(at+".type", &fd.Type, true, formTypes...)
		switch fd.Type {
		case "select", "multiselect":
			if len(fd.Options) == 0 {
				return nil, refuse("form.%s.options is missing: a %s needs choices.", at, fd.Type)
			}
			if len(fd.Options) > 8 {
				return nil, refuse("form.%s can offer at most 8 options (this one has %d).", at, len(fd.Options))
			}
			for j := range fd.Options {
				c.line(fmt.Sprintf("%s.options[%d]", at, j), &fd.Options[j], 40)
			}
			fd.Options = compact(fd.Options)
		default:
			fd.Options = nil
		}
		if fd.Type != "number" && fd.Type != "stepper" {
			fd.Min, fd.Max, fd.Step = nil, nil, nil
		} else if fd.Min != nil && fd.Max != nil && *fd.Min > *fd.Max {
			fd.Min, fd.Max = fd.Max, fd.Min
		}
		if c.err != "" {
			return nil, errors.New("Not asked: " + c.err)
		}
		fd.Value = fieldValue(fd, fd.Value, loc)
		if s, ok := fd.Value.(string); ok && looksLikeCardNumber(s) {
			return nil, refuse("form.%s.value has a long run of digits that looks like a card or account number. Leave it out.", at)
		}
	}
	return &f, nil
}

// fieldName turns "Check-in date" or "checkIn" into check_in_date-style
// names.
func fieldName(s string) string {
	var b strings.Builder
	prevLower := false
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLower {
				b.WriteByte('_')
			}
			b.WriteRune(r + 'a' - 'A')
			prevLower = false
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevLower = true
		case r == ' ' || r == '-' || r == '_':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
			prevLower = false
		}
	}
	return strings.Trim(b.String(), "_")
}

// labelFor turns a field name into a label: check_in becomes "Check in".
func labelFor(name string) string {
	if name == "" {
		return ""
	}
	return trim(strings.ToUpper(name[:1])+strings.ReplaceAll(name[1:], "_", " "), 24)
}

// fieldValue coerces a field's value (a default, or what the user
// submitted) to its type, or returns nil when it doesn't fit.
func fieldValue(fd *FormField, v any, loc *time.Location) any {
	switch fd.Type {
	case "number", "stepper":
		var f float64
		switch x := v.(type) {
		case float64:
			f = x
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return nil
			}
			f = n
		default:
			return nil
		}
		if fd.Min != nil && f < float64(*fd.Min) || fd.Max != nil && f > float64(*fd.Max) {
			return nil
		}
		return f
	case "toggle":
		switch x := v.(type) {
		case bool:
			return x
		case string:
			return x == "true" || x == "yes" || x == "on"
		}
		return nil
	case "multiselect":
		var picked []string
		var list []any
		switch x := v.(type) {
		case []any:
			list = x
		case []string:
			for _, s := range x {
				list = append(list, s)
			}
		case string:
			for _, s := range strings.Split(x, ",") {
				list = append(list, s)
			}
		}
		for _, x := range list {
			if s, ok := x.(string); ok && hasOption(fd.Options, strings.TrimSpace(s)) {
				picked = append(picked, strings.TrimSpace(s))
			}
		}
		if len(picked) == 0 {
			return nil
		}
		return picked
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if v == nil || s == "" {
		return nil
	}
	switch fd.Type {
	case "select":
		if !hasOption(fd.Options, s) {
			return nil
		}
	case "date":
		d, ok := normDate(s, loc)
		if !ok || strings.Contains(d, "T") {
			return nil
		}
		s = d
	case "time":
		t, ok := normClock(s)
		if !ok {
			return nil
		}
		s = t
	case "datetime":
		d, ok := normDate(s, loc)
		if !ok {
			return nil
		}
		s = d
	default:
		s = trim(strings.Join(strings.Fields(s), " "), 200)
	}
	return s
}

func hasOption(opts []string, s string) bool {
	for _, o := range opts {
		if o == s {
			return true
		}
	}
	return false
}

// ---------- replies from the app ----------

// Tap says which card button the user tapped: the entry, the block in it
// and the card in the block.
type Tap struct {
	EntryID int64 `json:"entryId"`
	Block   int   `json:"block"`
	Item    int   `json:"item"`
}

// reply works out how a message the app made for the user reads: shown is
// the text of their entry, said is what Alexa reads (Appendix A.4), and
// data is what the entry records about how it was made. A typed message
// comes back unchanged. Taps and forms are always visible messages, never
// hidden turns, and the instructions tell Alexa a tap is not permission to
// buy, book or send.
func (rt *Runtime) reply(ctx context.Context, userID int64, m Message) (shown, said string, data map[string]any) {
	shown = strings.TrimSpace(m.Text)
	said = shown
	switch m.Via {
	case "button":
		data = map[string]any{"via": "button"}
		if m.Tap == nil {
			return
		}
		if act, on, ok := rt.tapped(ctx, userID, *m.Tap); ok {
			shown = act.Text
			said = `[Tapped "` + act.Label + `" on: ` + on + `] ` + act.Text
			data["tap"] = m.Tap
		}
	case "form":
		var fields []FormField
		about := ""
		nd := rt.pendingNeeds(ctx, userID)
		switch {
		case m.answersEarlier(nd):
			// A late answer reads against the skipped question's own form,
			// whatever is open now, and says which question it answers,
			// since the run has moved on.
			var q string
			if q, fields = rt.askedForm(ctx, userID, m.AskEntry); q != "" {
				about = "About “" + trim(q, 64) + "”: "
			} else if i := strings.Index(shown, "”: "); strings.HasPrefix(shown, "About “") && i > 0 {
				// The question is gone; the app's summary still says
				// which one it was (FE-1).
				about = shown[:i+len("”: ")]
			}
		case nd != nil && nd.Form != nil:
			fields = nd.Form.Fields
		}
		values := formValues(fields, m.Values, rt.userLocation(ctx, userID))
		said = about + "[Form] " + formLine(fields, values)
		if shown == "" {
			shown = about + formSummary(fields, values)
		}
		data = map[string]any{"via": "form", "values": values}
	case "multi":
		data = map[string]any{"via": "multi"}
	case "report":
		shown = trim(shown, 300)
		said = "[Sent from the report] " + shown
		data = map[string]any{"via": "report"}
	}
	return
}

// answersEarlier says whether m answers a question the user skipped
// earlier rather than nd, the one open now (nil when none is). It's read
// against that question's form, never nd's.
func (m Message) answersEarlier(nd *Needs) bool {
	return m.AskEntry > 0 && (nd == nil || nd.EntryID != m.AskEntry)
}

// askedForm reads back question entry id: what it asked, and the fields of
// its form, if it had one.
func (rt *Runtime) askedForm(ctx context.Context, userID, id int64) (string, []FormField) {
	feed, err := rt.st.Feed(ctx, userID, id+1, 1)
	if err != nil || len(feed) != 1 || feed[0].ID != id || feed[0].Kind != "ask" {
		return "", nil
	}
	var d struct {
		Form *AskForm `json:"form"`
	}
	if json.Unmarshal(feed[0].Data, &d) != nil || d.Form == nil {
		return feed[0].Text, nil
	}
	return feed[0].Text, d.Form.Fields
}

// continues says whether a message the app made, while Alexa is idle,
// carries on the exchange on screen instead of starting a new one: a tap
// on her latest cards, or a choice sent from the report she showed in this
// exchange. Either answers what she showed, so the exchange, its plan and
// its panel stay. via is what reply made of the message.
func (rt *Runtime) continues(ctx context.Context, userID int64, m Message, via map[string]any) bool {
	switch {
	case via["tap"] != nil && m.Tap != nil:
		return rt.unanswered(ctx, userID, m.Tap.EntryID)
	case m.Via == "report":
		// The message doesn't say which report; it came from the one on
		// screen.
		cur := rt.surface(ctx, userID)
		return cur.Kind == "report" && rt.shownInExchange(ctx, userID, cur.ID)
	}
	return false
}

// shownInExchange says whether report id was shown, or revised, since the
// message that started the current topic: back through the exchanges that
// carried it on (or may, while their call is out). One the user opened
// from an earlier topic wasn't: a choice sent from it starts a new
// exchange.
func (rt *Runtime) shownInExchange(ctx context.Context, userID int64, id string) bool {
	feed, err := rt.st.Feed(ctx, userID, 0, 200)
	if err != nil {
		return false
	}
	for i := len(feed) - 1; i >= 0; i-- {
		var d struct {
			ID string `json:"id"`
			entryFlags
		}
		_ = json.Unmarshal(feed[i].Data, &d)
		switch {
		case feed[i].Kind == "report" && d.ID == id:
			return true
		case d.Start && !d.FollowUp && d.Topic != "pending":
			return false
		}
	}
	return false
}

// unanswered says whether entry id is still Alexa's last word: nothing the
// user sent, and no scheduled job, has come after it.
func (rt *Runtime) unanswered(ctx context.Context, userID, id int64) bool {
	feed, err := rt.st.Feed(ctx, userID, 0, 200)
	if err != nil || len(feed) == 0 || feed[0].ID > id {
		return false // further back than the last 200 entries: long answered
	}
	for _, e := range feed {
		if e.ID > id && (e.Kind == "user" || e.Kind == "note" && strings.HasPrefix(e.Text, "Scheduled: ")) {
			return false
		}
	}
	return true
}

// userLocation is the user's time zone, for reading dates they submitted.
func (rt *Runtime) userLocation(ctx context.Context, userID int64) *time.Location {
	if u, err := rt.st.GetUser(ctx, userID); err == nil {
		return userLoc(u)
	}
	return time.UTC
}

// tapped finds the reply button a tap points at, in the open question or
// in the entry's stored blocks, and the title of its card.
func (rt *Runtime) tapped(ctx context.Context, userID int64, t Tap) (Action, string, bool) {
	var raws []json.RawMessage
	if nd := rt.pendingNeeds(ctx, userID); nd != nil && nd.EntryID == t.EntryID && len(nd.Blocks) > 0 {
		raws = nd.Blocks
	} else {
		feed, err := rt.st.Feed(ctx, userID, t.EntryID+1, 1)
		if err != nil || len(feed) != 1 || feed[0].ID != t.EntryID {
			return Action{}, "", false
		}
		var d struct {
			Blocks []json.RawMessage `json:"blocks"`
		}
		if json.Unmarshal(feed[0].Data, &d) != nil {
			return Action{}, "", false
		}
		raws = d.Blocks
	}
	if t.Block < 0 || t.Block >= len(raws) {
		return Action{}, "", false
	}
	b, err := parseBlock(raws[t.Block])
	if err != nil {
		return Action{}, "", false
	}
	cb, ok := b.(*cardsBlock)
	if !ok || t.Item < 0 || t.Item >= len(cb.Items) {
		return Action{}, "", false
	}
	card := cb.Items[t.Item]
	if card.Action == nil || card.Action.Do != "reply" {
		return Action{}, "", false
	}
	return *card.Action, card.Title, true
}

// formValues keeps the submitted values that fit the form's fields. Without
// the form (it was answered meanwhile), plain values under valid names are
// kept as they are.
func formValues(fields []FormField, in map[string]any, loc *time.Location) map[string]any {
	out := map[string]any{}
	if len(fields) == 0 {
		for k, v := range in {
			if !fieldNameRe.MatchString(k) {
				continue
			}
			switch x := v.(type) {
			case string:
				if s := trim(strings.TrimSpace(x), 200); s != "" {
					out[k] = s
				}
			case float64, bool:
				out[k] = x
			case []any:
				var list []string
				for _, e := range x {
					if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
						list = append(list, trim(strings.TrimSpace(s), 40))
					}
				}
				if len(list) > 0 {
					out[k] = list
				}
			}
		}
		return out
	}
	for i := range fields {
		fd := &fields[i]
		if v, ok := in[fd.Name]; ok {
			if val := fieldValue(fd, v, loc); val != nil {
				out[fd.Name] = val
			}
		}
	}
	return out
}

// formOrder lists the submitted fields in the form's order, then any
// others by name.
func formOrder(fields []FormField, values map[string]any) []string {
	var keys []string
	seen := map[string]bool{}
	for _, f := range fields {
		if _, ok := values[f.Name]; ok {
			keys = append(keys, f.Name)
			seen[f.Name] = true
		}
	}
	var rest []string
	for k := range values {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func formText(v any, yes, no string) string {
	switch x := v.(type) {
	case bool:
		if x {
			return yes
		}
		return no
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []string:
		return strings.Join(x, ", ")
	}
	return fmt.Sprint(v)
}

// formLine is a submitted form as Alexa reads it: "date=2026-11-14;
// guests=2".
func formLine(fields []FormField, values map[string]any) string {
	var parts []string
	for _, k := range formOrder(fields, values) {
		parts = append(parts, k+"="+formText(values[k], "true", "false"))
	}
	if len(parts) == 0 {
		return "(nothing filled in)"
	}
	return strings.Join(parts, "; ")
}

// formSummary is a submitted form as the user's entry shows it, when the
// app didn't send its own: "Check-in: 2026-11-14 · Guests: 2".
func formSummary(fields []FormField, values map[string]any) string {
	labels := map[string]string{}
	for _, f := range fields {
		labels[f.Name] = f.Label
	}
	var parts []string
	for _, k := range formOrder(fields, values) {
		label := labels[k]
		if label == "" {
			label = k
		}
		parts = append(parts, label+": "+formText(values[k], "yes", "no"))
	}
	if len(parts) == 0 {
		return "Sent the form"
	}
	return strings.Join(parts, " · ")
}

// ---------- present_report ----------

func presentReport(tc *toolCtx, a map[string]any) (*toolResult, error) {
	r := tc.run
	rep := &store.Report{UserID: r.user.ID, Title: cleanText(str(a, "title")), Format: "markdown", Content: strings.TrimSpace(str(a, "content"))}
	if str(a, "format") == "html" {
		rep.Format = "html"
	} else {
		rep.Content = dropTitle(cleanText(rep.Content))
	}
	var data ReportData
	if arr, ok := a["items"].([]any); ok {
		for _, v := range arr {
			if m, ok := v.(map[string]any); ok && str(m, "label") != "" {
				data.Items = append(data.Items, ReportItem{Label: cleanText(str(m, "label")), Detail: cleanText(str(m, "detail"))})
			}
		}
	}
	if arr, ok := a["events"].([]any); ok {
		for _, v := range arr {
			if m, ok := v.(map[string]any); ok {
				data.Events = append(data.Events, CalEvent{Title: str(m, "title"), Start: str(m, "start"), End: str(m, "end"), AllDay: boolean(m, "all_day", false), Location: str(m, "location"), Description: str(m, "description")})
			}
		}
	}
	// Checked as say's are: the panel makes each one a link (24 at most).
	sources, notes := normalizeSources(a["sources"], 24)
	for _, s := range sources {
		data.Sources = append(data.Sources, Link{Title: s.Title, URL: s.URL})
	}
	if len(data.Sources) == 0 {
		// The model named no sources: list the pages this run read (M8).
		data.Sources = tc.rt.runLinks(r)
	}
	var missing []string
	for _, f := range strList(a, "files") {
		// Checked through the workspace root, so a link can't point the
		// report at a file outside it.
		fi, err := r.env.Stat(f)
		if err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, f)
			continue
		}
		data.Files = append(data.Files, r.env.Clean(f))
	}
	if rep.Title == "" {
		return nil, errors.New("give the report a title")
	}
	blocks, more, err := normalizeBlocks(rawArg(a, "blocks"), PlaceReport, tc.blockEnv())
	if err != nil {
		return &toolResult{Text: err.Error()}, nil
	}
	notes = append(notes, more...)
	if rep.Format == "html" {
		// The panel shows an html report as its own page and nothing else:
		// blocks wouldn't appear, and without content it's blank.
		switch {
		case rep.Content == "":
			return &toolResult{Text: "Not saved: an html report needs content, a full HTML document. Give content, or leave format out and build the report from markdown and blocks."}, nil
		case len(blocks) > 0:
			return &toolResult{Text: "Not saved: an html report shows only its own page, so blocks wouldn't appear. Draw them in the HTML, or leave format out to use blocks."}, nil
		}
	}
	data.Blocks = blocks
	data.Preview = reportPreview(blocks)
	if t := strings.Join(strings.Fields(cleanText(str(a, "takeaway"))), " "); t != "" {
		if looksLikeCardNumber(t) {
			return &toolResult{Text: "Not saved: the takeaway has a long run of digits that looks like a card or account number. Leave it out."}, nil
		}
		if utf8.RuneCountInString(t) > 200 {
			t = clip(t, 200)
			notes = append(notes, "Shortened the takeaway to 200 characters.")
		}
		data.Takeaway = t
	}
	if rep.Content == "" && len(data.Items) == 0 && len(blocks) == 0 {
		return nil, errors.New("the report has no content")
	}
	rep.Data = marshal(data)
	updated := false
	if id := strings.TrimSpace(str(a, "id")); id != "" {
		old, err := tc.rt.st.GetReport(tc.ctx, r.user.ID, id)
		if err != nil {
			return nil, fmt.Errorf("no report with id %q", id)
		}
		rep.ID, rep.CreatedAt, updated = old.ID, old.CreatedAt, true
	} else {
		rep.ID = uuid.NewString()[:8]
	}
	if err := tc.rt.st.SaveReport(tc.ctx, rep); err != nil {
		return nil, err
	}
	tc.rt.indexReport(tc.ctx, rep) // recall.go
	tc.rt.showReport(tc.ctx, r.user, rep, updated)
	r.replied()
	msg := fmt.Sprintf("Report %s (%q) is open on the user's screen.", rep.ID, rep.Title)
	if len(missing) > 0 {
		msg += " These files don't exist and were left out: " + strings.Join(missing, ", ") + "."
	}
	return &toolResult{Text: shownText(msg, blocks, 0, notes)}, nil
}

// dropTitle removes a heading the model put at the top of a report: the
// report's title is already shown above its content.
func dropTitle(md string) string {
	first, rest, _ := strings.Cut(md, "\n")
	if strings.HasPrefix(first, "# ") {
		return strings.TrimSpace(rest)
	}
	return md
}
