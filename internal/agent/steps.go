package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// StepData is what the executor records on a step entry (entry.data.step,
// SYNTHESIS Appendix B.2): which tool ran, where, on what, for how long and
// how it really went. The model only writes a step's labels; everything here
// is observed, so a failed click shows as failed, never as its predicted
// "done" text.
type StepData struct {
	Tool    string   `json:"tool"`
	Kind    string   `json:"kind"`   // search | read | browse | click | type | file | shell | memory | schedule | image | report | other
	Status  string   `json:"status"` // running | done | failed | skipped | interrupted
	Host    string   `json:"host,omitempty"`
	URL     string   `json:"url,omitempty"`
	Target  string   `json:"target,omitempty"` // the element's label, ≤60
	Query   string   `json:"query,omitempty"`
	MS      int64    `json:"ms,omitempty"`
	OK      *bool    `json:"ok,omitempty"`
	Error   string   `json:"error,omitempty"` // ≤140, fit to show the user
	Shot    string   `json:"shot,omitempty"`
	Plan    *int     `json:"plan,omitempty"` // the plan step that was active
	Attempt int      `json:"attempt,omitempty"`
	Sources []Source `json:"sources,omitempty"` // search: ≤10
	Text    string   `json:"text,omitempty"`    // browser_type: what was typed, ≤60
	Secret  bool     `json:"secret,omitempty"`  // typed a login or something like one: never shown
}

const maxStepSources = 10

func (r *run) hasStep() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.step != nil
}

func (rt *Runtime) setStep(r *run, label, done string) {
	r.mu.Lock()
	if r.step != nil && strings.EqualFold(r.step.Text, label) {
		if done != "" && r.step.DoneText == "" {
			r.step.DoneText = done
		}
		r.mu.Unlock()
		return
	}
	rt.finishStepLocked(r, "done")
	e := &store.Entry{UserID: r.user.ID, Kind: "step", Text: label, DoneText: done, Status: "running"}
	if err := rt.addEntry(context.Background(), e); err != nil {
		r.mu.Unlock()
		return
	}
	r.step = e
	r.mu.Unlock()
	p := rt.progress(r)
	p.mu.Lock()
	p.idle = false
	p.mu.Unlock()
	// The view takes r.mu, so the state is published after letting go.
	_ = rt.st.SetSessionActivity(context.Background(), r.user.ID, label)
	rt.publishState(r.user.ID)
}

// setActivity changes what the user sees Alexa doing, without a new step.
func (rt *Runtime) setActivity(r *run, activity string) {
	_ = rt.st.SetSessionActivity(context.Background(), r.user.ID, activity)
	rt.publishState(r.user.ID)
}

// finishStepLocked closes the current step. status is what the caller
// believes; the step's own record wins when it has one, so a step whose
// last call failed stays failed, and one that finished stays done when the
// run is stopped afterwards. Only a step that went well keeps its "done"
// text.
func (rt *Runtime) finishStepLocked(r *run, status string) {
	if r.step == nil {
		return
	}
	if p := peekProgress(r); p != nil {
		p.mu.Lock()
		if p.stepID == r.step.ID && p.step != nil {
			if p.stepLast != "" {
				status = p.stepLast
			}
			p.step.Status = status
			r.step.Data = marshal(map[string]any{"step": p.step})
		}
		p.step, p.stepID, p.stepLast = nil, 0, ""
		p.mu.Unlock()
	}
	r.step.Status = status
	if status != "done" {
		r.step.DoneText = ""
	}
	rt.updateEntry(context.Background(), r.step)
	r.step = nil
}

func (rt *Runtime) finishStep(r *run, status string) {
	r.mu.Lock()
	rt.finishStepLocked(r, status)
	r.mu.Unlock()
}

// stepFor returns the record of step entry id, starting a new one when the
// step changed. p.mu must be held.
func (p *progress) stepFor(id int64) *StepData {
	if p.stepID != id || p.step == nil {
		p.step, p.stepID, p.stepLast = &StepData{Status: "running"}, id, ""
	}
	return p.step
}

// writeStep shows the step's record on its entry, if it's still the current
// step. p.mu must not be held.
func (rt *Runtime) writeStep(r *run, id int64, status string, data json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.step == nil || r.step.ID != id {
		return
	}
	r.step.Status = status
	if status != "running" && status != "done" {
		r.step.DoneText = ""
	}
	r.step.Data = marshal(map[string]any{"step": data})
	rt.updateEntry(context.Background(), r.step)
}

// toolStarted records a tool call that's about to run on the current step
// and announces it: the tool phase, or the writing phase for a message.
func (rt *Runtime) toolStarted(r *run, name string, args map[string]any) {
	p := rt.progress(r)
	if target := writingTarget(name); target != "" {
		p.mu.Lock()
		p.call = &inflight{name: name, args: args, start: time.Now()}
		p.mu.Unlock()
		p.emit(runEvent{Phase: "writing", Target: target})
		return
	}
	r.mu.Lock()
	var id int64
	label := ""
	if r.step != nil {
		id, label = r.step.ID, r.step.Text
	}
	r.mu.Unlock()
	if id == 0 || name == "plan" {
		p.mu.Lock()
		p.call = &inflight{name: name, args: args, start: time.Now()}
		p.mu.Unlock()
		return
	}
	planIdx := -1
	if pl := rt.currentPlan(context.Background(), r.user.ID); pl != nil {
		planIdx = pl.active()
	}
	kind := toolKind(name)
	p.mu.Lock()
	sd := p.stepFor(id)
	if p.stepLast == "failed" || p.stepLast == "interrupted" {
		sd.Attempt = max(sd.Attempt, 1) + 1
	}
	sd.Tool, sd.Kind, sd.Status, sd.OK, sd.Error = name, kind, "running", nil, ""
	if planIdx >= 0 {
		sd.Plan = &planIdx
	}
	page := p.page
	switch {
	case name == "browser_open" || name == "fetch_url":
		sd.URL, sd.Host = cleanURL(str(args, "url")), siteOf(str(args, "url"))
	case strings.HasPrefix(name, "browser_"):
		sd.URL, sd.Host = page.url, siteOf(page.url)
	}
	if t := page.label(targetIndex(name, args)); t != "" {
		sd.Target = t
	} else if !keepsTarget(name) {
		sd.Target = ""
	}
	sd.Text, sd.Secret = typedText(name, args, page.line(targetIndex(name, args)), sd.Target)
	p.call = &inflight{name: name, args: args, start: time.Now(), stepID: id}
	reset := p.idle
	p.idle = false
	data := marshal(sd)
	host := sd.Host
	p.mu.Unlock()
	rt.writeStep(r, id, "running", data)
	if reset {
		rt.setActivity(r, label)
	}
	p.emit(runEvent{Phase: "tool", Tool: &runTool{Name: name, Label: label, Kind: kind, Host: host}})
}

// toolEnded records how the call went and returns its result for the model,
// with the exchange's one plan reminder added to a final say or report. A
// status tool's end sets the activity back to "Thinking": the next model
// turn is starting, and the step's label would be stale while it writes.
func (rt *Runtime) toolEnded(ctx context.Context, r *run, res *toolResult, err error) *toolResult {
	p := peekProgress(r)
	if p == nil {
		return res
	}
	p.mu.Lock()
	c := p.call
	p.call = nil
	p.mu.Unlock()
	if c == nil {
		return res
	}
	status, msg := callOutcome(ctx, c.name, res, err)
	if target := writingTarget(c.name); target != "" {
		final := boolean(c.args, "final", false)
		if status == "done" {
			rt.resultsTakeStage(ctx, r, c.name, c.args)
			if final && c.name != "ask" {
				if note := rt.planReminder(ctx, r.user.ID); note != "" {
					res.Text += "\n\n" + note
				}
			}
		}
		if c.name != "ask" && !final {
			p.emit(runEvent{Phase: "thinking"})
		}
		return res
	}
	if c.stepID == 0 {
		return res
	}
	p.mu.Lock()
	if res != nil && res.Kind == "browser_state" {
		p.page = parsePage(res.Text)
	}
	sd := p.stepFor(c.stepID)
	ok := status == "done"
	sd.MS += time.Since(c.start).Milliseconds()
	sd.OK, sd.Error = &ok, msg
	sd.Status = "running"
	if !ok {
		sd.Status = status
	}
	if strings.HasPrefix(c.name, "browser_") && p.page.url != "" {
		sd.URL, sd.Host = p.page.url, siteOf(p.page.url)
	}
	if c.name == "fetch_url" && ok {
		p.addSource(Source{URL: sd.URL})
	}
	if c.name == "browser_open" && ok && p.page.url != "" {
		p.addSource(Source{Title: p.page.title, URL: p.page.url})
	}
	p.stepLast = status
	p.idle = true
	data := marshal(sd)
	p.mu.Unlock()
	rt.writeStep(r, c.stepID, sd.Status, data)
	rt.setActivity(r, "Thinking")
	p.emit(runEvent{Phase: "thinking"})
	return res
}

// errToolCrashed is what the user sees of a tool that panicked.
var errToolCrashed = errors.New("something went wrong in this step")

// What a tool's result says about how it went, when it returned no error:
// the browser tools report failed actions in their result, and several
// tools answer the model with a fixable "Not sent" or "Not updated".
var (
	failedPrefixes  = []string{"Error:", "Action failed:", "Not sent:", "Not asked:", "Not updated:", "Not saved:", "Not deleted:", "Couldn't ", "No such "}
	skippedPrefixes = []string{"Not done:", "Not run:", "Not run "}
	exitCodeRe      = regexp.MustCompile(`^exit code (-?\d+)`)
	httpStatusRe    = regexp.MustCompile(`^HTTP (\d{3})`)
)

// callOutcome says how a tool call went: done, failed, skipped or
// interrupted, with a short error the user can be shown.
func callOutcome(ctx context.Context, name string, res *toolResult, err error) (string, string) {
	if ctx.Err() != nil {
		return "interrupted", ""
	}
	if err != nil {
		return "failed", userError(err.Error())
	}
	if res == nil {
		return "done", ""
	}
	t := strings.TrimSpace(res.Text)
	if t == "Stopped." {
		return "interrupted", ""
	}
	for _, pre := range skippedPrefixes {
		if strings.HasPrefix(t, pre) {
			return "skipped", userError(t)
		}
	}
	for _, pre := range failedPrefixes {
		if strings.HasPrefix(t, pre) {
			return "failed", userError(t)
		}
	}
	switch name {
	case "run_shell":
		if m := exitCodeRe.FindStringSubmatch(t); m != nil && m[1] != "0" {
			return "failed", "Exit code " + m[1]
		}
	case "fetch_url":
		if m := httpStatusRe.FindStringSubmatch(t); m != nil {
			if code, _ := strconv.Atoi(m[1]); code >= 400 {
				return "failed", "HTTP " + m[1]
			}
		}
	}
	return "done", ""
}

var (
	pathRe    = regexp.MustCompile(`(?:/[^\s/:;,"']+){2,}`)
	elementRe = regexp.MustCompile(`(?i)\belement \[\d+\]`)
)

// userError turns an error meant for the model into a line for the user:
// the first sentence without the advice to the model, file paths cut to
// their names, wrapped Go errors cut to their outer part, at most 140
// characters.
func userError(s string) string {
	s = strings.TrimSpace(s)
	for _, pre := range []string{"Error: ", "Action failed: ", "Not sent: ", "Not asked: ", "Not updated: ", "Not done: "} {
		s = strings.TrimPrefix(s, pre)
	}
	s, _, _ = strings.Cut(s, "\n")
	for _, sep := range []string{"; ", " — ", ". "} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	s = pathRe.ReplaceAllStringFunc(s, filepath.Base)
	if strings.Count(s, ": ") >= 2 {
		s, _, _ = strings.Cut(s, ": ")
	}
	s = elementRe.ReplaceAllString(s, "the element")
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if s == "" {
		return "It didn't work"
	}
	r, n := utf8.DecodeRuneInString(s)
	return trim(string(unicode.ToUpper(r))+s[n:], 140)
}

// toolKind sorts tools for the screen (a glyph, and how steps group).
func toolKind(name string) string {
	switch name {
	case "web_search", "recall":
		return "search"
	case "fetch_url", "browser_read":
		return "read"
	case "browser_click", "browser_click_at", "browser_drag", "browser_select", "browser_answer_dialog":
		return "click"
	case "browser_type", "browser_key", "browser_login":
		return "type"
	case "list_files", "read_file", "write_file":
		return "file"
	case "run_shell":
		return "shell"
	case "memory_save", "memory_delete":
		return "memory"
	case "schedule_create", "schedule_list", "schedule_delete":
		return "schedule"
	case "generate_image":
		return "image"
	case "present_report":
		return "report"
	}
	if strings.HasPrefix(name, "browser_") || name == "request_takeover" || name == "request_login" {
		return "browse"
	}
	return "other"
}

// targetIndex is the element a browser call acts on, or 0.
func targetIndex(name string, args map[string]any) int {
	switch name {
	case "browser_click", "browser_type", "browser_select", "browser_hover", "browser_upload":
		return int(num(args, "index", 0))
	case "browser_drag":
		return int(num(args, "from_index", 0))
	}
	return 0
}

// keepsTarget: these calls go on acting on the element the step started
// with (pressing Enter in the field just typed into, scrolling the list),
// so the step keeps showing it.
func keepsTarget(name string) bool {
	return name == "browser_key" || name == "browser_scroll" || name == "browser_state"
}

// pageState is the browser page as the latest state the model saw described
// it: where it was and the labels of its indexed elements.
type pageState struct {
	url, title string
	text       string
}

// parsePage reads the URL, title and element list of a browser state.
func parsePage(text string) pageState {
	var ps pageState
	for _, l := range strings.Split(text, "\n") {
		switch {
		case ps.url == "" && strings.HasPrefix(l, "URL: "):
			ps.url = cleanURL(strings.TrimPrefix(l, "URL: "))
		case ps.title == "" && strings.HasPrefix(l, "Title: "):
			ps.title = trim(strings.TrimSpace(strings.TrimPrefix(l, "Title: ")), 80)
		}
	}
	ps.text = text
	return ps
}

var frameSuffixRe = regexp.MustCompile(` \(in frame "[^"]*"\)$`)

// label finds element idx in the page's element list ("[12] <button>
// Add to cart") and returns a short label for the user, or "".
func (ps pageState) label(idx int) string {
	if d := ps.line(idx); d != "" {
		return elementLabel(frameSuffixRe.ReplaceAllString(d, ""))
	}
	return ""
}

// line is the indexed page's line for element idx, without its marker.
func (ps pageState) line(idx int) string {
	if idx <= 0 || ps.text == "" {
		return ""
	}
	marker := fmt.Sprintf("[%d] ", idx)
	for _, l := range strings.Split(ps.text, "\n") {
		if d, ok := strings.CutPrefix(l, marker); ok {
			return d
		}
	}
	return ""
}

// typedText is what the live view may show of a browser_type step: the
// text, up to 60 characters, unless it went into a password or code field
// or looks like a card number. A login is always secret.
func typedText(name string, args map[string]any, element, label string) (text string, secret bool) {
	switch name {
	case "browser_login":
		return "", true
	case "browser_type":
	default:
		return "", false
	}
	t := strings.Join(strings.Fields(str(args, "text")), " ")
	if t == "" {
		return "", false
	}
	el := strings.ToLower(element)
	if strings.Contains(el, `type="password"`) || credentialRe.MatchString(label) || credentialRe.MatchString(el) || looksLikeCardNumber(t) {
		return "", true
	}
	return clip(t, 60), false
}

var attrRe = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)

// elementLabel picks what a person would call an element from its
// description (`<tag attr="…"> text`): its text, or its aria-label,
// placeholder, title or name. A field's value is never used: it's what was
// typed into it.
func elementLabel(desc string) string {
	head, text := desc, ""
	if i := strings.Index(desc, ">"); strings.HasPrefix(desc, "<") && i > 0 {
		head, text = desc[:i], strings.TrimSpace(desc[i+1:])
	}
	if strings.HasPrefix(text, "value=") || strings.HasPrefix(text, "options:") {
		text = ""
	}
	text = strings.TrimPrefix(text, "img: ")
	attrs := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(head, -1) {
		attrs[m[1]] = m[2]
	}
	name := ""
	if i := strings.Index(head, "name="); i >= 0 {
		name, _, _ = strings.Cut(head[i+5:], " ")
	}
	for _, v := range []string{text, attrs["aria-label"], attrs["placeholder"], attrs["title"], name} {
		if v = strings.TrimSpace(v); v != "" {
			return trim(v, 60)
		}
	}
	return ""
}

// cleanURL drops a URL's query and fragment, which can carry tokens; what's
// left says which page it was.
func cleanURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u.String()
}

// siteOf is the site a URL is on, without "www.".
func siteOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// searchRef is the latest web search step, which the pages read after it
// are added to as its sources.
type searchRef struct {
	entry *store.Entry
	data  *StepData
}

// searchItem shows one of the model's web search actions as a step: a
// search with its query and the sources it found, or a page it read (which
// also counts as a source of the search before it).
func (rt *Runtime) searchItem(r *run, it llm.Item, sources []Source) {
	if it.Action == nil {
		return
	}
	switch it.Action.Type {
	case "search":
		q := it.Action.Query
		if q == "" && len(it.Action.Queries) > 0 {
			q = it.Action.Queries[0]
		}
		if q != "" {
			rt.setStep(r, "Searching for “"+trim(q, 48)+"”", "Searched for “"+trim(q, 48)+"”")
			rt.markSearch(r, it.Status, func(sd *StepData) {
				sd.Tool, sd.Kind, sd.Query = "web_search", "search", trim(q, 200)
				for _, s := range sources {
					sd.Sources = appendSource(sd.Sources, s)
				}
			})
		}
	case "open_page":
		host := siteOf(it.Action.URL)
		if host != "" {
			rt.setStep(r, "Reading "+host, "Read "+host)
			rt.markSearch(r, it.Status, func(sd *StepData) {
				sd.Tool, sd.Kind, sd.URL, sd.Host = "web_search", "read", cleanURL(it.Action.URL), host
			})
		}
	}
}

// markSearch fills in the record of the search step just shown, and ends
// it: the search is over when its item is, and the model goes on thinking
// in the same turn, so the step mustn't show as running meanwhile. It
// took from the item's start to its end.
func (rt *Runtime) markSearch(r *run, itemStatus string, fill func(*StepData)) {
	r.mu.Lock()
	e := r.step
	r.mu.Unlock()
	if e == nil {
		return
	}
	planIdx := -1
	if pl := rt.currentPlan(context.Background(), r.user.ID); pl != nil {
		planIdx = pl.active()
	}
	p := rt.progress(r)
	p.mu.Lock()
	sd := p.stepFor(e.ID)
	fill(sd)
	ok := itemStatus != "failed"
	sd.OK = &ok
	if planIdx >= 0 {
		sd.Plan = &planIdx
	}
	if !p.searchStart.IsZero() {
		sd.MS = time.Since(p.searchStart).Milliseconds()
		p.searchStart = time.Time{}
	}
	p.stepLast = "done"
	if !ok {
		p.stepLast, sd.Error = "failed", "The search didn't work"
	}
	p.idle = true
	var prev *searchRef
	switch sd.Kind {
	case "search":
		for _, s := range sd.Sources {
			p.addSource(s)
		}
		p.search = &searchRef{entry: e, data: sd}
	case "read":
		p.addSource(Source{URL: sd.URL})
		if s := p.search; s != nil && s.entry.ID != e.ID && len(s.data.Sources) < maxStepSources {
			s.data.Sources = appendSource(s.data.Sources, Source{URL: sd.URL})
			prev = s
		}
	}
	var prevData json.RawMessage
	if prev != nil {
		prevData = marshal(map[string]any{"step": prev.data})
	}
	tool := &runTool{Name: "web_search", Label: e.Text, Kind: sd.Kind, Host: sd.Host}
	p.mu.Unlock()
	if prev != nil {
		// The search step is over; only its sources change.
		r.mu.Lock()
		prev.entry.Data = prevData
		rt.updateEntry(context.Background(), prev.entry)
		r.mu.Unlock()
	}
	p.emit(runEvent{Phase: "tool", Tool: tool})
	// Its record decides done or failed; the model thinks over what it found.
	rt.finishStep(r, "done")
	rt.setActivity(r, "Thinking")
	p.emit(runEvent{Phase: "thinking"})
}

func appendSource(list []Source, s Source) []Source {
	if s.URL == "" || len(list) >= maxStepSources {
		return list
	}
	for _, x := range list {
		if x.URL == s.URL {
			return list
		}
	}
	return append(list, s)
}

// searchSources reads the sources a web search call lists (action.sources),
// which the parsed item leaves out.
func searchSources(raw json.RawMessage) []Source {
	var it struct {
		Action struct {
			Sources []struct {
				Type  string `json:"type"`
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"sources"`
		} `json:"action"`
	}
	if json.Unmarshal(raw, &it) != nil {
		return nil
	}
	var out []Source
	for _, s := range it.Action.Sources {
		if u := cleanURL(s.URL); u != "" {
			out = appendSource(out, Source{Title: trim(strings.TrimSpace(s.Title), 80), URL: u})
		}
	}
	return out
}
