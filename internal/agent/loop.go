package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

const (
	keepShots  = 2 // screenshots kept in context
	keepStates = 4 // full browser states kept in context
	keepLong   = 8 // full long tool outputs kept in context
)

// nudge is added when Alexa ends its turn without answering the user in a
// way they can see.
const nudge = "[Alexa system note] The user can't see plain text or reasoning — only say, ask and present_report reach them. Answer their last message now with say (one or two short sentences; put anything longer in a report), then end your turn."

func (rt *Runtime) loop(ctx context.Context, r *run) {
	uid := r.user.ID
	defer func() {
		if p := recover(); p != nil {
			slog.Error("agent loop panic", "user", uid, "panic", p, "stack", string(debug.Stack()))
			rt.fail(r, fmt.Errorf("internal error: %v", p))
		}
		l := rt.userLock(uid)
		l.Lock()
		rt.removeRun(r)
		l.Unlock()
		rt.publishState(uid)
	}()

	retriedOverflow := false
	for turn := 0; turn < rt.cfg.MaxTurns; turn++ {
		// Alexa keeps thinking while the user controls the browser; only
		// browser actions wait for them to hand it back.
		if ctx.Err() != nil {
			rt.stopped(r)
			return
		}
		if err := rt.drainInbox(ctx, r); err != nil {
			rt.fail(r, err)
			return
		}
		if err := rt.compact(ctx, r.user, false); err != nil {
			slog.Warn("compaction failed", "user", uid, "err", err)
		}
		input, err := rt.buildInput(ctx, uid)
		if err != nil {
			rt.fail(r, err)
			return
		}
		mems := rt.memories(ctx, uid)
		if u, err := rt.st.GetUser(ctx, uid); err == nil {
			r.user = u
		}
		req := &llm.Request{
			Model:        rt.cfg.Model,
			Instructions: buildInstructions(r.user, mems, rt.cfg.ShellEnabled, r.loc()),
			Input:        input,
			Tools:        rt.toolDefs,
			Reasoning:    &llm.Reasoning{Effort: rt.cfg.ReasoningEffort, Summary: "auto"},
			Include:      []string{"reasoning.encrypted_content"},
			Store:        false,
		}
		t0 := time.Now()
		var firstEvent, firstOut time.Duration
		resp, err := rt.llm.Stream(ctx, req, func(ev llm.Event) {
			if firstEvent == 0 {
				firstEvent = time.Since(t0)
			}
			if firstOut == 0 && strings.HasSuffix(ev.Type, ".delta") {
				firstOut = time.Since(t0)
			}
			// Web searches run inside the model's turn; show them as they go.
			if ev.Type == "response.output_item.added" || ev.Type == "response.output_item.done" {
				if it := llm.ParseItem(ev.Item); it.Type == "web_search_call" {
					if ev.Type == "response.output_item.added" {
						rt.setActivity(r, "Searching the web")
					} else {
						rt.searchStep(r, it)
					}
				}
			}
		})
		if err != nil {
			if ctx.Err() != nil {
				rt.stopped(r)
				return
			}
			if isContextOverflow(err) && !retriedOverflow {
				retriedOverflow = true
				slog.Warn("context too long, compacting", "user", uid, "err", err)
				if cerr := rt.compact(ctx, r.user, true); cerr == nil {
					continue
				}
			}
			rt.fail(r, err)
			return
		}
		retriedOverflow = false
		slog.Info("model turn", "user", uid, "turn", turn,
			"first_event", firstEvent.Round(time.Millisecond), "first_output", firstOut.Round(time.Millisecond),
			"total", time.Since(t0).Round(time.Millisecond), "in", resp.Usage.InputTokens,
			"cached", resp.Usage.InputTokensDetails.CachedTokens, "out", resp.Usage.OutputTokens,
			"reasoning", resp.Usage.OutputTokensDetails.ReasoningTokens, "items", len(input))
		_ = rt.st.AddSessionUsage(ctx, uid, resp.Usage.InputTokens, resp.Usage.OutputTokens)

		// Keep the model's output for replay. Its plain text is never shown:
		// the user only sees what it says through the say tool.
		var stored []json.RawMessage
		var calls []llm.Item
		var text strings.Builder
		for _, raw := range resp.Output {
			it := llm.ParseItem(raw)
			stored = append(stored, marshal(storedItem{Item: raw}))
			switch it.Type {
			case "message":
				text.WriteString(it.Text())
			case "function_call":
				calls = append(calls, it)
			}
		}
		if err := rt.st.AppendItems(ctx, uid, stored); err != nil {
			rt.fail(r, err)
			return
		}

		if len(calls) == 0 {
			if resp.Status == "incomplete" && strings.TrimSpace(text.String()) == "" {
				continue // ran out of output tokens mid-thought; keep going
			}
			if r.shouldNudge() {
				if err := rt.appendMessage(ctx, uid, "system", nudge, nil); err != nil {
					rt.fail(r, err)
					return
				}
				continue
			}
			if t := cleanText(text.String()); t != "" && r.isAwaiting() {
				rt.post(ctx, r, clip(t, 300))
			}
			if rt.finish(r) {
				return
			}
			continue // a message arrived meanwhile
		}

		var suspend *Needs
		for _, call := range calls {
			out := rt.execute(ctx, r, call)
			if out.Suspend != nil {
				if suspend == nil {
					suspend = out.Suspend
					suspend.CallID = call.CallID
					suspend.ToolName = call.Name
					continue // its output is written when the user responds
				}
				out = &toolResult{Text: "Only one request to the user at a time; ask again after the first is answered."}
			}
			si := storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": call.CallID, "output": out.Text}), Kind: out.Kind, Short: out.Short}
			if len(out.Image) > 0 {
				if name, err := rt.saveShot(uid, out.Image); err == nil {
					si.Shot = name
				}
			}
			if err := rt.st.AppendItems(ctx, uid, []json.RawMessage{marshal(si)}); err != nil {
				rt.fail(r, err)
				return
			}
			if ctx.Err() != nil {
				break
			}
		}
		if ctx.Err() != nil {
			if suspend != nil {
				_ = rt.resolveNeeds(context.Background(), uid, suspend, "Stopped before the user answered.", nil)
			}
			rt.stopped(r)
			return
		}
		if suspend != nil && rt.suspend(r, suspend) {
			return
		}
	}
	rt.fail(r, fmt.Errorf("stopped after %d steps without finishing", rt.cfg.MaxTurns))
}

func (r *run) shouldNudge() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.awaiting || r.nudged {
		return false
	}
	r.nudged = true
	return true
}

func (r *run) isHeld() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.held
}

func (r *run) isAwaiting() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.awaiting
}

// replied records that the user has been answered.
func (r *run) replied() {
	r.mu.Lock()
	r.awaiting = false
	r.mu.Unlock()
}

// post shows a message from Alexa to the user.
func (rt *Runtime) post(ctx context.Context, r *run, text string) {
	_ = rt.addEntry(ctx, &store.Entry{UserID: r.user.ID, Kind: "say", Text: text})
	r.replied()
	_ = rt.notify(ctx, r.user, "", "Alexa", text, false, false)
}

// execute runs one tool call, never failing the loop.
func (rt *Runtime) execute(ctx context.Context, r *run, call llm.Item) (out *toolResult) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("tool panic", "tool", call.Name, "panic", p, "stack", string(debug.Stack()))
			out = &toolResult{Text: fmt.Sprintf("Error: the tool crashed (%v).", p)}
		}
	}()
	tl := rt.toolMap[call.Name]
	if tl == nil {
		return &toolResult{Text: fmt.Sprintf("Error: unknown tool %q.", call.Name)}
	}
	args := map[string]any{}
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return &toolResult{Text: "Error: arguments were not valid JSON: " + err.Error()}
		}
	}
	if s := strings.TrimSpace(str(args, "status")); s != "" {
		rt.setStep(r, s, strings.TrimSpace(str(args, "done")))
	} else if !tl.NoStatus && !r.hasStep() {
		rt.setStep(r, "Working on it", "")
	}
	if strings.HasPrefix(call.Name, "browser_") {
		if !rt.waitControl(ctx, r) {
			return &toolResult{Text: "Stopped."}
		}
		// The user's screen follows the agent into the browser, unless they
		// chose to look at something else during this request.
		if !r.isHeld() {
			rt.showSurface(r.user.ID, Surface{Kind: "browser"})
		}
	}
	slog.Debug("tool call", "user", r.user.ID, "tool", call.Name, "args", call.Arguments)
	t0 := time.Now()
	res, err := tl.Run(&toolCtx{ctx: ctx, rt: rt, run: r}, args)
	slog.Info("tool", "user", r.user.ID, "name", call.Name, "took", time.Since(t0).Round(time.Millisecond), "err", err)
	if err != nil {
		return &toolResult{Text: "Error: " + err.Error()}
	}
	if res == nil {
		res = &toolResult{Text: "OK"}
	}
	return res
}

// waitControl blocks while the user controls the browser. Returns false if
// the run was cancelled meanwhile.
func (rt *Runtime) waitControl(ctx context.Context, r *run) bool {
	waited := false
	for {
		r.mu.Lock()
		ch := r.released
		r.mu.Unlock()
		if ch == nil {
			if waited && ctx.Err() == nil {
				_ = rt.st.SetSessionActivity(ctx, r.user.ID, "Picking up where you left off")
				rt.publishState(r.user.ID)
			}
			return ctx.Err() == nil
		}
		waited = true
		_ = rt.st.SetSessionActivity(ctx, r.user.ID, "You’re in control")
		rt.publishState(r.user.ID)
		select {
		case <-ch:
		case <-ctx.Done():
			return false
		}
	}
}

func (rt *Runtime) drainInbox(ctx context.Context, r *run) error {
	select { // a wake-up for messages about to be read is stale
	case <-r.wake:
	default:
	}
	msgs, err := rt.st.TakeInbox(ctx, r.user.ID)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		var attach []Attachment
		_ = json.Unmarshal(m.Attachments, &attach)
		if err := rt.appendMessage(ctx, r.user.ID, m.Kind, m.Text, attach); err != nil {
			return err
		}
		if m.Kind == "user" {
			r.mu.Lock()
			r.awaiting, r.nudged, r.held = true, false, false
			r.mu.Unlock()
		}
	}
	return nil
}

// ---------- steps ----------

func (r *run) hasStep() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.step != nil
}

func (rt *Runtime) setStep(r *run, label, done string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.step != nil && strings.EqualFold(r.step.Text, label) {
		if done != "" && r.step.DoneText == "" {
			r.step.DoneText = done
		}
		return
	}
	rt.finishStepLocked(r, "done")
	e := &store.Entry{UserID: r.user.ID, Kind: "step", Text: label, DoneText: done, Status: "running"}
	if err := rt.addEntry(context.Background(), e); err != nil {
		return
	}
	r.step = e
	_ = rt.st.SetSessionActivity(context.Background(), r.user.ID, label)
	rt.publishState(r.user.ID)
}

// setActivity changes what the user sees Alexa doing, without a new step.
func (rt *Runtime) setActivity(r *run, activity string) {
	_ = rt.st.SetSessionActivity(context.Background(), r.user.ID, activity)
	rt.publishState(r.user.ID)
}

func (rt *Runtime) finishStepLocked(r *run, status string) {
	if r.step == nil {
		return
	}
	r.step.Status = status
	rt.updateEntry(context.Background(), r.step)
	r.step = nil
}

func (rt *Runtime) finishStep(r *run, status string) {
	r.mu.Lock()
	rt.finishStepLocked(r, status)
	r.mu.Unlock()
}

func (rt *Runtime) searchStep(r *run, it llm.Item) {
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
		}
	case "open_page":
		host := it.Action.URL
		if u, err := url.Parse(it.Action.URL); err == nil && u.Host != "" {
			host = strings.TrimPrefix(u.Host, "www.")
		}
		if host != "" {
			rt.setStep(r, "Reading "+host, "Read "+host)
		}
	}
}

// ---------- how a run ends ----------

// finish ends the run unless a message arrived meanwhile (then the loop
// goes on with it).
func (rt *Runtime) finish(r *run) bool {
	ctx := context.Background()
	uid := r.user.ID
	l := rt.userLock(uid)
	l.Lock()
	defer l.Unlock()
	if n, err := rt.st.InboxCount(ctx, uid); err == nil && n > 0 {
		return false
	}
	rt.finishStep(r, "done")
	rt.removeRun(r)
	_ = rt.st.SetSessionStatus(ctx, uid, "idle", "")
	rt.publishState(uid)
	return true
}

// suspend makes the session wait for the user. If they already wrote
// something meanwhile, that is taken as the answer and the run goes on.
func (rt *Runtime) suspend(r *run, nd *Needs) bool {
	ctx := context.Background()
	uid := r.user.ID
	l := rt.userLock(uid)
	l.Lock()
	defer l.Unlock()
	if msgs, err := rt.st.TakeInbox(ctx, uid); err == nil && len(msgs) > 0 {
		var texts []string
		var attach []Attachment
		for _, m := range msgs {
			texts = append(texts, m.Text)
			var a []Attachment
			_ = json.Unmarshal(m.Attachments, &a)
			attach = append(attach, a...)
		}
		_ = rt.resolveNeeds(ctx, uid, nd, "Before seeing this, the user wrote:\n"+strings.Join(texts, "\n"), nil)
		if len(attach) > 0 {
			_ = rt.appendMessage(ctx, uid, "user", stamp(r.loc(), "", "(Attached files)"), attach)
		}
		r.mu.Lock()
		r.awaiting, r.nudged = true, false
		r.mu.Unlock()
		return false
	}
	rt.finishStep(r, "done")
	e := &store.Entry{UserID: uid, Kind: "ask", Text: nd.Message, Status: "open"}
	if len(nd.Options) > 0 {
		e.Data = marshal(map[string]any{"options": nd.Options})
	}
	activity := "Waiting for your answer"
	switch nd.Kind {
	case "takeover":
		e.Kind = "takeover"
		activity = nd.Message
		_ = rt.st.SetSessionSurface(ctx, uid, marshal(Surface{Kind: "browser"}))
	case "login":
		e.Kind = "login"
		e.Data = marshal(map[string]any{"site": nd.Site, "username": nd.Username})
		activity = "Waiting for you to sign in to " + nd.Site
		_ = rt.st.SetSessionSurface(ctx, uid, marshal(Surface{Kind: "browser"}))
	}
	_ = rt.addEntry(ctx, e)
	nd.EntryID = e.ID
	_ = rt.st.SetSessionNeeds(ctx, uid, marshal(nd))
	_ = rt.st.SetSessionStatus(ctx, uid, "needs", activity)
	rt.removeRun(r)
	rt.publishState(uid)
	_ = rt.notify(ctx, r.user, "", "Alexa needs you", nd.Message, true, false)
	return true
}

func (rt *Runtime) fail(r *run, err error) {
	ctx := context.Background()
	uid := r.user.ID
	slog.Error("agent run failed", "user", uid, "err", err)
	msg := err.Error()
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		msg = "The model service returned an error: " + apiErr.Message
	}
	l := rt.userLock(uid)
	l.Lock()
	defer l.Unlock()
	rt.finishStep(r, "failed")
	_ = rt.st.SetEntriesStatus(ctx, uid, "step", "running", "failed")
	_ = rt.addEntry(ctx, &store.Entry{UserID: uid, Kind: "error", Text: msg})
	_ = rt.st.AddInbox(ctx, uid, store.InboxMessage{Kind: "system", Text: stamp(r.loc(), "", "[Your previous turn ended with an error: "+msg+"]")})
	_ = rt.st.SetSessionStatus(ctx, uid, "idle", "")
	rt.removeRun(r)
	rt.publishState(uid)
	_ = rt.notify(ctx, r.user, "", "Alexa ran into a problem", trim(msg, 180), false, false)
}

func (rt *Runtime) stopped(r *run) {
	ctx := context.Background()
	uid := r.user.ID
	l := rt.userLock(uid)
	l.Lock()
	defer l.Unlock()
	rt.finishStep(r, "failed")
	_ = rt.st.SetEntriesStatus(ctx, uid, "step", "running", "failed")
	rt.removeRun(r)
	// A shutdown (not the user) leaves the session "working" so it resumes.
	if rt.baseCtx.Err() != nil {
		return
	}
	_ = rt.addEntry(ctx, &store.Entry{UserID: uid, Kind: "note", Text: "Stopped"})
	_ = rt.st.AddInbox(ctx, uid, store.InboxMessage{Kind: "system", Text: stamp(r.loc(), "", "[The user stopped you. Don't resume that work unless they ask.]")})
	_ = rt.st.SetSessionStatus(ctx, uid, "idle", "")
	rt.publishState(uid)
}

// ---------- context building ----------

// buildInput replays the conversation since the last compaction (after its
// summary), with old screenshots and page dumps pruned.
func (rt *Runtime) buildInput(ctx context.Context, userID int64) ([]json.RawMessage, error) {
	ss, err := rt.st.GetSession(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := rt.st.Items(ctx, userID, ss.CompactedThrough)
	if err != nil {
		return nil, err
	}
	items := make([]storedItem, 0, len(rows))
	for _, r := range rows {
		var si storedItem
		if err := json.Unmarshal(r.JSON, &si); err != nil || len(si.Item) == 0 {
			continue
		}
		items = append(items, si)
	}
	// Which function calls have outputs (others get a synthetic one).
	answered := map[string]bool{}
	for _, si := range items {
		it := llm.ParseItem(si.Item)
		if it.Type == "function_call_output" {
			answered[it.CallID] = true
		}
	}
	env := rt.envs.For(userID)
	shots, states, longs := 0, 0, 0
	out := make([]json.RawMessage, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		si := items[i]
		it := llm.ParseItem(si.Item)
		switch {
		case it.Type == "function_call_output":
			var o struct {
				CallID string `json:"call_id"`
				Output string `json:"output"`
			}
			_ = json.Unmarshal(si.Item, &o)
			text := o.Output
			switch si.Kind {
			case "browser_state":
				states++
				if states > keepStates && si.Short != "" {
					text = si.Short
				}
			case "long":
				longs++
				if longs > keepLong && len(text) > 1500 {
					text = text[:1500] + "\n[…the rest of this older output was dropped to save space; run the tool again if you need it]"
				}
			}
			var output any = text
			if si.Shot != "" && shots < keepShots && states <= keepStates {
				if img, err := os.ReadFile(filepath.Join(env.Shots, si.Shot)); err == nil {
					shots++
					output = []map[string]any{
						{"type": "input_text", "text": text},
						{"type": "input_image", "image_url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)},
					}
				}
			}
			out[i] = marshal(map[string]any{"type": "function_call_output", "call_id": o.CallID, "output": output})
		case isMessage(si.Kind):
			out[i] = rt.userItem(env.Workspace, si)
		default:
			out[i] = si.Item
		}
	}
	final := make([]json.RawMessage, 0, len(out)+3)
	if ss.Summary != "" {
		final = append(final, marshal(map[string]any{"role": "user", "content": []map[string]any{{"type": "input_text",
			"text": "[Summary of the earlier part of our conversation, written when it was compacted]\n\n" + ss.Summary}}}))
	}
	// Insert synthetic outputs for unanswered calls (e.g. after a crash).
	for i, raw := range out {
		final = append(final, raw)
		it := llm.ParseItem(items[i].Item)
		if it.Type == "function_call" && !answered[it.CallID] {
			final = append(final, marshal(map[string]any{"type": "function_call_output", "call_id": it.CallID, "output": "Interrupted before this tool call completed; check the current state and retry if needed."}))
		}
	}
	return final, nil
}

func (rt *Runtime) userItem(workspace string, si storedItem) json.RawMessage {
	var msg struct {
		Role    string           `json:"role"`
		Content []map[string]any `json:"content"`
	}
	_ = json.Unmarshal(si.Item, &msg)
	content := msg.Content
	for _, a := range si.Attach {
		full := filepath.Join(workspace, a.Path)
		switch {
		case strings.HasPrefix(a.Mime, "image/") && a.Size < 15<<20:
			if b, err := os.ReadFile(full); err == nil {
				content = append(content, map[string]any{"type": "input_text", "text": fmt.Sprintf("[Attached image %q, saved at %s]", a.Name, a.Path)})
				content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + a.Mime + ";base64," + base64.StdEncoding.EncodeToString(b)})
				continue
			}
		case a.Mime == "application/pdf" && a.Size < 20<<20:
			if b, err := os.ReadFile(full); err == nil {
				content = append(content, map[string]any{"type": "input_text", "text": fmt.Sprintf("[Attached PDF %q, saved at %s]", a.Name, a.Path)})
				content = append(content, map[string]any{"type": "input_file", "filename": a.Name, "file_data": "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(b)})
				continue
			}
		}
		content = append(content, map[string]any{"type": "input_text", "text": fmt.Sprintf("[Attached file %q (%s, %d bytes), saved in the workspace at %s]", a.Name, a.Mime, a.Size, a.Path)})
	}
	return marshal(map[string]any{"role": "user", "content": content})
}

// ---------- small helpers ----------

// citationRe matches raw search citation markers such as 【123†L4-L9】 that
// the model sometimes copies from web results.
var citationRe = regexp.MustCompile(`\s?【[^】]*】`)

func cleanText(s string) string { return strings.TrimSpace(citationRe.ReplaceAllString(s, "")) }

func trim(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// clip shortens text to at most n characters, ending at a sentence, clause
// or word boundary.
func clip(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= n {
		return text
	}
	head := string([]rune(text)[:n])
	for _, sep := range []string{". ", "! ", "? "} {
		if i := strings.LastIndex(head, sep); i >= n/3 {
			return head[:i+1]
		}
	}
	for _, sep := range []string{"; ", " — ", ", "} {
		if i := strings.LastIndex(head, sep); i >= n/3 {
			return head[:i] + "…"
		}
	}
	if i := strings.LastIndex(head, " "); i >= n/3 {
		return head[:i] + "…"
	}
	return trim(text, n)
}

func plain(md string) string {
	r := strings.NewReplacer("**", "", "__", "", "#", "", "`", "", "*", "")
	return strings.Join(strings.Fields(r.Replace(md)), " ")
}

func appendLink(list []Link, l Link) []Link {
	for _, x := range list {
		if x.URL == l.URL {
			return list
		}
	}
	return append(list, l)
}
