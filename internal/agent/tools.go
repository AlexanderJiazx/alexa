package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/google/uuid"

	"alexagent/internal/browser"
	"alexagent/internal/store"
)

type toolCtx struct {
	ctx context.Context
	rt  *Runtime
	run *run
}

type toolResult struct {
	Text    string
	Image   []byte // JPEG screenshot to show the model
	Kind    string // browser_state | long
	Short   string // replacement when pruned from context
	Suspend *Needs
}

type tool struct {
	Name        string
	Description string
	Params      map[string]any // properties
	Required    []string
	NoStatus    bool
	Run         func(tc *toolCtx, args map[string]any) (*toolResult, error)
}

func (t *tool) schema() map[string]any {
	props := map[string]any{}
	for k, v := range t.Params {
		props[k] = v
	}
	req := append([]string{}, t.Required...)
	if !t.NoStatus {
		props["status"] = map[string]any{"type": "string", "description": "Short user-facing step label, present progressive, e.g. \"Opening the library website\"."}
		props["done"] = map[string]any{"type": "string", "description": "The same step in past tense, e.g. \"Opened the library website\"."}
		req = append(req, "status")
	}
	return map[string]any{
		"type":        "function",
		"name":        t.Name,
		"description": t.Description,
		"parameters": map[string]any{
			"type":       "object",
			"properties": props,
			"required":   req,
		},
	}
}

// ---------- arg helpers ----------

func str(a map[string]any, k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func num(a map[string]any, k string, def float64) float64 {
	switch v := a[k].(type) {
	case float64:
		return v
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			return f
		}
	}
	return def
}

func boolean(a map[string]any, k string, def bool) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return def
}

func strList(a map[string]any, k string) []string {
	var out []string
	if arr, ok := a[k].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func p(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

// ---------- browser helpers ----------

func (tc *toolCtx) tab() (*browser.Tab, error) {
	b, err := tc.rt.browsers.Get(tc.run.user.ID)
	if err != nil {
		return nil, err
	}
	return b.Tab(tabKey)
}

// browserState returns the current page state as a tool result, prefixed by msg.
func (tc *toolCtx) browserState(tab *browser.Tab, msg string) (*toolResult, error) {
	st, err := tab.State()
	if err != nil {
		return &toolResult{Text: msg + "\n(Could not read the page state: " + err.Error() + ")"}, nil
	}
	text := st.Text
	if msg != "" {
		text = msg + "\n\n" + text
	}
	return &toolResult{Text: text, Image: st.Screenshot, Kind: "browser_state", Short: fmt.Sprintf("%s\n[Older page state omitted. Page was: %s — %s]", msg, st.Title, st.URL)}, nil
}

func browserTool(name, desc string, params map[string]any, required []string, act func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error)) *tool {
	return &tool{Name: name, Description: desc, Params: params, Required: required,
		Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
			tab, err := tc.tab()
			if err != nil {
				return nil, fmt.Errorf("browser unavailable: %w", err)
			}
			msg, err := act(tc, tab, a)
			if err != nil {
				res, _ := tc.browserState(tab, "Action failed: "+err.Error())
				return res, nil
			}
			return tc.browserState(tab, msg)
		}}
}

func (rt *Runtime) buildTools() []*tool {
	tools := []*tool{
		browserTool("browser_open", "Open a URL in the shared browser (the user sees it live). Returns the page state.",
			map[string]any{"url": p("string", "Absolute URL, or a domain like example.com")}, []string{"url"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				return "Opened " + str(a, "url"), tab.Navigate(str(a, "url"))
			}),
		browserTool("browser_click", "Click the element with the given index from the latest browser state.",
			map[string]any{"index": p("integer", "Element index")}, []string{"index"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				_, err := tab.Click(int(num(a, "index", 0)))
				return fmt.Sprintf("Clicked [%d]", int(num(a, "index", 0))), err
			}),
		browserTool("browser_click_at", "Click at viewport pixel coordinates of the latest screenshot (only when the target has no index).",
			map[string]any{"x": p("number", "X in pixels"), "y": p("number", "Y in pixels"), "double": p("boolean", "Double-click")}, []string{"x", "y"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				return fmt.Sprintf("Clicked at (%.0f, %.0f)", num(a, "x", 0), num(a, "y", 0)), tab.ClickXY(num(a, "x", 0), num(a, "y", 0), boolean(a, "double", false))
			}),
		browserTool("browser_type", "Type text into the element with the given index (focuses it first). Replaces existing text unless clear=false. Set submit=true to press Enter afterwards. "+
			"Date, time and slider inputs get the value set directly: pass e.g. 2026-11-15, 19:30 or 4. The result says if the field ends up showing something else.",
			map[string]any{"index": p("integer", "Element index (0 = currently focused element)"), "text": p("string", "Text to type"), "clear": p("boolean", "Clear existing value first (default true)"), "submit": p("boolean", "Press Enter after typing")}, []string{"index", "text"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				msg := fmt.Sprintf("Typed into [%d]", int(num(a, "index", 0)))
				note, err := tab.Type(int(num(a, "index", 0)), str(a, "text"), boolean(a, "clear", true), boolean(a, "submit", false))
				if note != "" {
					msg += ". " + note
				}
				return msg, err
			}),
		browserTool("browser_hover", "Move the mouse over an element (by index) or a point (x, y), for menus and tooltips that only appear on hover.",
			map[string]any{"index": p("integer", "Element index"), "x": p("number", "X in pixels (when there's no index)"), "y": p("number", "Y in pixels")}, nil,
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				idx := int(num(a, "index", 0))
				if idx > 0 {
					return fmt.Sprintf("Hovering over [%d]", idx), tab.Hover(idx, 0, 0)
				}
				return fmt.Sprintf("Hovering at (%.0f, %.0f)", num(a, "x", 0), num(a, "y", 0)), tab.Hover(0, num(a, "x", 0), num(a, "y", 0))
			}),
		browserTool("browser_drag", "Drag from one element or point to another: drag and drop, reordering, sliders and handles. Give each end as an element index or as x/y coordinates; both ends must be in view.",
			map[string]any{"from_index": p("integer", "Element to drag"), "from_x": p("number", "Start X (when there's no index)"), "from_y": p("number", "Start Y"),
				"to_index": p("integer", "Element to drop on"), "to_x": p("number", "End X (when there's no index)"), "to_y": p("number", "End Y")}, nil,
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				return "Dragged", tab.Drag(int(num(a, "from_index", 0)), num(a, "from_x", 0), num(a, "from_y", 0), int(num(a, "to_index", 0)), num(a, "to_x", 0), num(a, "to_y", 0))
			}),
		browserTool("browser_answer_dialog", "Answer the confirm or prompt dialog the page is showing (the browser state says when one is open; the page is frozen until it's answered). accept=true clicks OK, false clicks Cancel. For a prompt, text is what to type into it. "+
			"A confirm can be consequential (\"Delete this?\"): check with the user first when it is.",
			map[string]any{"accept": p("boolean", "true for OK, false for Cancel"), "text": p("string", "For a prompt: the text to enter")}, []string{"accept"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				return tab.AnswerDialog(boolean(a, "accept", false), str(a, "text"))
			}),
		browserTool("browser_key", "Press a key or key combination in the browser, e.g. Enter, Escape, Tab, ArrowDown, PageDown, Control+a.",
			map[string]any{"keys": p("string", "Key or combination")}, []string{"keys"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				return "Pressed " + str(a, "keys"), tab.Keys(str(a, "keys"))
			}),
		browserTool("browser_scroll", "Scroll the page (or the scrollable area under an element).",
			map[string]any{"direction": map[string]any{"type": "string", "enum": []string{"down", "up", "left", "right", "top", "bottom"}}, "pages": p("number", "How far, in viewport heights (default 0.8)"), "index": p("integer", "Optional element inside the area to scroll")}, []string{"direction"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				return "Scrolled " + str(a, "direction"), tab.Scroll(str(a, "direction"), num(a, "pages", 0.8), int(num(a, "index", 0)))
			}),
		browserTool("browser_select", "Choose an option in a <select> dropdown by its visible text or value.",
			map[string]any{"index": p("integer", "Index of the <select>"), "option": p("string", "Option text or value")}, []string{"index", "option"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				sel, err := tab.Select(int(num(a, "index", 0)), str(a, "option"))
				return "Selected " + sel, err
			}),
		browserTool("browser_history", "Go back or forward in the browser history, or reload.",
			map[string]any{"action": map[string]any{"type": "string", "enum": []string{"back", "forward", "reload"}}}, []string{"action"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				switch str(a, "action") {
				case "forward":
					return "Went forward", tab.History(1)
				case "reload":
					return "Reloaded", tab.Reload()
				}
				return "Went back", tab.History(-1)
			}),
		browserTool("browser_state", "Get the current browser state (after waiting for something, or to re-check the page).",
			map[string]any{"wait_seconds": p("number", "Seconds to wait before reading (max 30)")}, nil,
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				if w := num(a, "wait_seconds", 0); w > 0 {
					select {
					case <-time.After(time.Duration(min(w, 30) * float64(time.Second))):
					case <-tc.ctx.Done():
					case <-tc.run.wake:
						return "Stopped waiting early: the user sent a message.", nil
					}
				}
				return "", nil
			}),
		browserTool("browser_upload", "Attach workspace files to a file input element.",
			map[string]any{"index": p("integer", "Index of the file input (or the element containing it)"), "paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Workspace paths"}}, []string{"index", "paths"},
			func(tc *toolCtx, tab *browser.Tab, a map[string]any) (string, error) {
				var full []string
				for _, pth := range strList(a, "paths") {
					f, err := tc.run.env.Resolve(pth)
					if err != nil {
						return "", err
					}
					full = append(full, f)
				}
				return "Attached files", tab.Upload(int(num(a, "index", 0)), full)
			}),
		{
			Name:        "browser_read",
			Description: "Read the full text of the current page as markdown (for extracting information without scrolling). Long pages are paged; pass offset to continue.",
			Params:      map[string]any{"offset": p("integer", "Character offset to start from (default 0)")},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				tab, err := tc.tab()
				if err != nil {
					return nil, err
				}
				off := int(num(a, "offset", 0))
				md, total, err := tab.Read(off, 24000)
				if err != nil {
					return nil, err
				}
				u, title := tab.URL()
				more := ""
				if off+len(md) < total {
					more = fmt.Sprintf("\n\n[Showing %d–%d of %d characters. Call browser_read with offset=%d for more.]", off, off+len(md), total, off+len(md))
				}
				return &toolResult{Text: fmt.Sprintf("# %s\n%s\n\n%s%s", title, u, md, more), Kind: "long"}, nil
			},
		},
		{
			Name:        "request_takeover",
			Description: "Pause and ask the user to take over the shared browser — for signing in, passwords, 2FA codes, CAPTCHAs, payment details, or anything only they can do on the page. Their screen switches to the browser. You resume when they hand back control.",
			Params:      map[string]any{"reason": p("string", "Short instruction shown to the user, e.g. \"Sign in to continue\"")},
			Required:    []string{"reason"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				tc.run.replied()
				return &toolResult{Suspend: &Needs{Kind: "takeover", Message: trim(str(a, "reason"), 200)}}, nil
			},
		},
		{
			Name:        "say",
			Description: "Send the user a short message. This is the only way the user sees anything you write: your plain text output is hidden. One or two plain sentences, under 300 characters, no headings, lists or tables. Put anything longer in a report (present_report) and say a one-line takeaway. Messages are pushed to the user's phone automatically when they're away.",
			NoStatus:    true,
			Params: map[string]any{
				"text":  p("string", "The message"),
				"push":  p("boolean", "Also push it to their phone even if they have Alexa open (alerts, reminders)"),
				"email": p("boolean", "Also email it (only when they asked for email)"),
			},
			Required: []string{"text"},
			Run:      say,
		},
		{
			Name:        "ask",
			Description: "Ask the user a question and wait for the answer — to confirm consequential actions (purchases, sending, booking, deleting) or to resolve real ambiguity. Short and self-contained, under 300 characters. Offer options when there are clear choices.",
			NoStatus:    true,
			Params: map[string]any{
				"question": p("string", "The question"),
				"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional short answers to choose from (2–4, a few words each)"},
			},
			Required: []string{"question"},
			Run:      ask,
		},
		{
			Name: "present_report",
			Description: "Show the user a report: a document that opens on their screen and stays in their reports. Use it for anything longer than a short message — research findings, comparisons, itineraries, plans, drafts, instructions, detailed results. " +
				"Write well-structured markdown (headings, short paragraphs, lists, tables, links). Afterwards, say a one-line takeaway. To revise a report you showed earlier, pass its id.",
			NoStatus: true,
			Params: map[string]any{
				"title":   p("string", "Short title that states what it is or the outcome, e.g. \"Flights to Lisbon under $400\""),
				"content": p("string", "The report in markdown (or a full HTML document when format is html). Don't repeat the title as a heading; it's shown above the content."),
				"format":  map[string]any{"type": "string", "enum": []string{"markdown", "html"}, "description": "markdown (default), or html for a self-contained page with its own styling, charts or interactivity. It runs sandboxed: scripts may load libraries from cdn.jsdelivr.net, cdnjs.cloudflare.com or unpkg.com and images from https URLs, but can't fetch data, so embed the data in the page"},
				"id":      p("string", "Id of an earlier report to replace with this version"),
				"items": map[string]any{"type": "array", "description": "Optional key rows shown at the top, one per thing handled", "items": map[string]any{"type": "object", "properties": map[string]any{
					"label": p("string", "Item"), "detail": p("string", "Short detail like a date, price or status")}, "required": []string{"label"}}},
				"events": map[string]any{"type": "array", "description": "Calendar events the user may want to add", "items": map[string]any{"type": "object", "properties": map[string]any{
					"title": p("string", "Event title"), "start": p("string", "Start, RFC3339 with offset, or YYYY-MM-DD for all-day"), "end": p("string", "End (optional)"),
					"all_day": p("boolean", "All-day event"), "location": p("string", "Location"), "description": p("string", "Notes")}, "required": []string{"title", "start"}}},
				"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Workspace files to attach (images are shown inline)"},
				"sources": map[string]any{"type": "array", "description": "Sources and important links", "items": map[string]any{"type": "object", "properties": map[string]any{
					"title": p("string", "Title"), "url": p("string", "URL")}, "required": []string{"url"}}},
			},
			Required: []string{"title"},
			Run:      presentReport,
		},
		{
			Name:        "fetch_url",
			Description: "Fetch a URL directly over HTTP (no JavaScript, no cookies) and return its content — HTML as markdown, JSON/text as-is. Good for APIs, feeds and simple pages. Set save_as to download the file into the workspace instead.",
			Params:      map[string]any{"url": p("string", "URL"), "save_as": p("string", "Optional workspace path to save the response body to")},
			Required:    []string{"url"},
			Run:         fetchURL,
		},
		{
			Name:        "list_files",
			Description: "List files in the workspace (or a sub-directory).",
			Params:      map[string]any{"path": p("string", "Directory (default: workspace root)")},
			Run:         listFiles,
		},
		{
			Name: "read_file",
			Description: "Read a file from the workspace. Text comes back as text (long files are paged: pass offset). Images (PNG, JPEG, GIF, WebP) are shown to you, so you can look at them, e.g. to check an image you generated. " +
				"PDFs come back as text; pass page to see that page as an image (scans, charts, forms).",
			Params:   map[string]any{"path": p("string", "File path"), "offset": p("integer", "Character offset"), "page": p("integer", "For a PDF: the page to look at, from 1")},
			Required: []string{"path"},
			Run:      readFile,
		},
		{
			Name:        "write_file",
			Description: "Create or overwrite a text file in the workspace (reports, notes, CSV, markdown, HTML…).",
			Params:      map[string]any{"path": p("string", "File path"), "content": p("string", "File content"), "append": p("boolean", "Append instead of overwrite")},
			Required:    []string{"path", "content"},
			Run:         writeFile,
		},
		{
			Name:        "memory_save",
			Description: "Save a durable fact about the user to long-term memory (preferences, personal details, accounts they use). One fact per call. Never store secrets.",
			Params:      map[string]any{"content": p("string", "The fact, as a short self-contained sentence")},
			Required:    []string{"content"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				m, err := tc.rt.st.AddMemory(tc.ctx, tc.run.user.ID, strings.TrimSpace(str(a, "content")))
				if err != nil {
					return nil, err
				}
				return &toolResult{Text: fmt.Sprintf("Saved memory [%d].", m.ID)}, nil
			},
		},
		{
			Name:        "memory_delete",
			Description: "Delete an outdated or wrong memory by id.",
			Params:      map[string]any{"id": p("integer", "Memory id")},
			Required:    []string{"id"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				ok, err := tc.rt.st.DeleteMemory(tc.ctx, tc.run.user.ID, int64(num(a, "id", 0)))
				if err != nil {
					return nil, err
				}
				if !ok {
					return &toolResult{Text: "No such memory."}, nil
				}
				return &toolResult{Text: "Deleted."}, nil
			},
		},
		{
			Name: "schedule_create",
			Description: "Create a schedule: recurring (cron, in the user's timezone) or once at a time. When it fires, its prompt arrives in this conversation as a scheduled message for you to act on. " +
				"Use for reminders, daily briefings and monitoring (\"check X and tell me if Y\"). Write the prompt as a self-contained instruction to yourself.",
			Params: map[string]any{
				"name":   p("string", "Short name, e.g. \"Morning briefing\""),
				"prompt": p("string", "What to do each time it fires"),
				"cron":   p("string", "Standard 5-field cron expression (minute hour day month weekday) for recurring runs, e.g. \"0 8 * * 1-5\". As in any cron, if both day and weekday are set it runs on days matching either one"),
				"run_at": p("string", "For a one-off run: local date-time like 2026-10-21T09:00 or RFC3339"),
			},
			Required: []string{"name", "prompt"},
			Run:      scheduleCreate,
		},
		{
			Name:        "schedule_list",
			Description: "List the user's schedules.",
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				return &toolResult{Text: tc.rt.describeSchedules(tc.ctx, tc.run.user)}, nil
			},
		},
		{
			Name:        "schedule_delete",
			Description: "Delete a schedule by id.",
			Params:      map[string]any{"id": p("string", "Schedule id")},
			Required:    []string{"id"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				ok, err := tc.rt.st.DeleteSchedule(tc.ctx, tc.run.user.ID, str(a, "id"))
				if err != nil {
					return nil, err
				}
				tc.rt.sched.Reload(tc.ctx)
				if !ok {
					return &toolResult{Text: "No such schedule."}, nil
				}
				return &toolResult{Text: "Deleted."}, nil
			},
		},
	}
	if rt.cfg.ShellEnabled {
		tools = append(tools, &tool{
			Name:        "run_shell",
			Description: "Run a shell command (bash) in the workspace directory. " + shellTools() + " Use for calculations, data processing, charts, reading and converting files, and generating documents. Not for browsing.",
			Params:      map[string]any{"command": p("string", "The bash command"), "timeout_seconds": p("integer", "Timeout (default 120, max 900)")},
			Required:    []string{"command"},
			Run:         runShell,
		})
	}
	if rt.cfg.ImageModel != "" {
		tools = append(tools, &tool{
			Name:        "generate_image",
			Description: "Generate an image from a text prompt and save it to the workspace.",
			Params:      map[string]any{"prompt": p("string", "Detailed description"), "path": p("string", "Workspace path, e.g. images/poster.png")},
			Required:    []string{"prompt", "path"},
			Run:         generateImage,
		})
	}
	return tools
}

// ---------- implementations ----------

func fetchURL(tc *toolCtx, a map[string]any) (*toolResult, error) {
	u := str(a, "url")
	ctx, cancel := context.WithTimeout(tc.ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/json,text/plain,*/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if dest := str(a, "save_as"); dest != "" {
		full, err := tc.run.env.Resolve(dest)
		if err != nil {
			return nil, err
		}
		_ = tc.run.env.MkdirAll(filepath.Dir(full))
		f, err := os.Create(full)
		if err != nil {
			return nil, err
		}
		n, err := io.Copy(f, io.LimitReader(resp.Body, 200<<20))
		f.Close()
		tc.run.env.Chown(full)
		if err != nil {
			return nil, err
		}
		return &toolResult{Text: fmt.Sprintf("HTTP %d. Saved %d bytes to %s (%s).", resp.StatusCode, n, tc.run.env.Rel(full), resp.Header.Get("Content-Type"))}, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, err
	}
	ct := resp.Header.Get("Content-Type")
	text := string(body)
	if strings.Contains(ct, "html") {
		if md, err := htmltomd.ConvertString(text); err == nil {
			text = md
		}
	} else if !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "json") && !strings.Contains(ct, "xml") && !strings.Contains(ct, "javascript") && ct != "" {
		return &toolResult{Text: fmt.Sprintf("HTTP %d, %s, %d bytes of binary content. Use save_as to download it.", resp.StatusCode, ct, len(body))}, nil
	}
	if len(text) > 30000 {
		text = text[:30000] + "\n[…truncated]"
	}
	return &toolResult{Text: fmt.Sprintf("HTTP %d (%s)\n\n%s", resp.StatusCode, ct, text), Kind: "long"}, nil
}

func listFiles(tc *toolCtx, a map[string]any) (*toolResult, error) {
	dir, err := tc.run.env.Resolve(str(a, "path"))
	if err != nil {
		return nil, err
	}
	var lines []string
	count := 0
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil
		}
		rel := tc.run.env.Rel(path)
		depth := strings.Count(strings.TrimPrefix(path, dir), string(os.PathSeparator))
		if d.IsDir() {
			if depth > 3 || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			lines = append(lines, rel+"/")
			return nil
		}
		count++
		if count > 300 {
			return filepath.SkipAll
		}
		info, _ := d.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		lines = append(lines, fmt.Sprintf("%s (%d bytes)", rel, size))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return &toolResult{Text: "(empty)"}, nil
	}
	return &toolResult{Text: strings.Join(lines, "\n")}, nil
}

func readFile(tc *toolCtx, a map[string]any) (*toolResult, error) {
	full, err := tc.run.env.Resolve(str(a, "path"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if res, ok := readBinary(tc, full, data, int(num(a, "page", 0)), int(num(a, "offset", 0))); ok {
		return res, nil
	}
	if !isText(data) {
		return &toolResult{Text: fmt.Sprintf("%s is a binary file (%d bytes, %s). Use run_shell to process it.", str(a, "path"), len(data), mime.TypeByExtension(filepath.Ext(full)))}, nil
	}
	off := int(num(a, "offset", 0))
	if off > len(data) {
		off = len(data)
	}
	s := string(data[off:])
	more := ""
	if len(s) > 30000 {
		s = s[:30000]
		more = fmt.Sprintf("\n[…truncated; continue with offset=%d]", off+30000)
	}
	return &toolResult{Text: s + more, Kind: "long"}, nil
}

func isText(b []byte) bool {
	n := min(len(b), 4096)
	return !bytes.ContainsRune(b[:n], 0)
}

func writeFile(tc *toolCtx, a map[string]any) (*toolResult, error) {
	full, err := tc.run.env.Resolve(str(a, "path"))
	if err != nil {
		return nil, err
	}
	if err := tc.run.env.MkdirAll(filepath.Dir(full)); err != nil {
		return nil, err
	}
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if boolean(a, "append", false) {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(full, flag, 0o600)
	if err != nil {
		return nil, err
	}
	_, err = f.WriteString(str(a, "content"))
	f.Close()
	tc.run.env.Chown(full)
	tc.run.env.Chown(filepath.Dir(full))
	if err != nil {
		return nil, err
	}
	return &toolResult{Text: "Wrote " + tc.run.env.Rel(full)}, nil
}

func runShell(tc *toolCtx, a map[string]any) (*toolResult, error) {
	timeout := time.Duration(min(max(num(a, "timeout_seconds", 120), 1), 900)) * time.Second
	ctx, cancel := context.WithTimeout(tc.ctx, timeout)
	defer cancel()
	env := tc.run.env
	cmd := exec.CommandContext(ctx, "bash", "-c", str(a, "command"))
	cmd.Dir = env.Workspace
	cmd.Env = []string{
		"HOME=" + env.Home,
		"PATH=" + shellPath,
		"LANG=C.UTF-8",
		"TZ=" + tc.run.user.Timezone,
		"WORKSPACE=" + env.Workspace,
		"MPLBACKEND=Agg",
	}
	// Commands reach the network the same way the server does.
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		if v, ok := os.LookupEnv(k); ok {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if env.UID > 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(env.UID), Gid: uint32(env.GID)}
	}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	var out bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, n: 60000}
	cmd.Stderr = cmd.Stdout
	err := cmd.Run()
	text := out.String()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			text += fmt.Sprintf("\n[timed out after %s]", timeout)
			code = -1
		} else {
			return nil, err
		}
	}
	if len(text) > 30000 {
		text = text[:15000] + "\n[…]\n" + text[len(text)-15000:]
	}
	return &toolResult{Text: fmt.Sprintf("exit code %d\n%s", code, text), Kind: "long"}, nil
}

const shellPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/homebrew/bin"

// shellTools says what run_shell has to work with, so the model doesn't
// plan around tools that aren't there. It's checked once at startup: the
// image (or the machine) decides what's installed.
func shellTools() string {
	var have []string
	if py := findTool("python3"); py != "" {
		// Import names, and what to call them.
		mods := [][2]string{{"matplotlib", "matplotlib"}, {"pandas", "pandas"}, {"numpy", "numpy"}, {"openpyxl", "openpyxl"},
			{"docx", "python-docx"}, {"pypdf", "pypdf"}, {"PIL", "Pillow"}, {"bs4", "BeautifulSoup"}, {"requests", "requests"}}
		args := []string{"-c", "import importlib.util as u, sys\nprint(' '.join(m for m in sys.argv[1:] if u.find_spec(m)))"}
		for _, m := range mods {
			args = append(args, m[0])
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, py, args...).Output()
		found := map[string]bool{}
		for _, m := range strings.Fields(string(out)) {
			found[m] = true
		}
		var libs []string
		for _, m := range mods {
			if found[m[0]] {
				libs = append(libs, m[1])
			}
		}
		if len(libs) > 0 {
			have = append(have, "Python 3 (with "+strings.Join(libs, ", ")+")")
		} else {
			have = append(have, "Python 3 (standard library only)")
		}
	}
	for _, name := range []string{"ffmpeg", "pdftotext", "pdftoppm", "tesseract", "jq", "curl", "unzip", "zip"} {
		if findTool(name) != "" {
			have = append(have, name)
		}
	}
	if len(have) == 0 {
		return "Only basic shell utilities are installed."
	}
	return "Installed: " + strings.Join(have, ", ") + "."
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if len(q) > l.n {
		q = q[:l.n]
	}
	l.n -= len(q)
	_, err := l.w.Write(q)
	return len(p), err
}

func scheduleCreate(tc *toolCtx, a map[string]any) (*toolResult, error) {
	sc := &store.Schedule{
		ID:      uuid.NewString()[:8],
		UserID:  tc.run.user.ID,
		Name:    str(a, "name"),
		Prompt:  str(a, "prompt"),
		Cron:    strings.TrimSpace(str(a, "cron")),
		Enabled: true,
	}
	if ra := strings.TrimSpace(str(a, "run_at")); ra != "" {
		t, err := parseLocalTime(ra, tc.run.loc())
		if err != nil {
			return nil, err
		}
		if t.Before(time.Now()) {
			return nil, fmt.Errorf("run_at %s is in the past", t.Format(time.RFC3339))
		}
		sc.RunAt = t.UnixMilli()
	}
	if sc.Cron == "" && sc.RunAt == 0 {
		return nil, errors.New("give either cron or run_at")
	}
	if sc.Cron != "" {
		if _, err := cronParser.Parse(sc.Cron); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
	}
	if err := tc.rt.st.CreateSchedule(tc.ctx, sc); err != nil {
		return nil, err
	}
	tc.rt.sched.Reload(tc.ctx)
	// Listing the next few runs lets the model check the schedule does what
	// was asked.
	var when []string
	for _, t := range tc.rt.sched.NextRuns(sc, tc.run.loc(), 4) {
		when = append(when, t.In(tc.run.loc()).Format("Mon Jan 2 2006 15:04 MST"))
	}
	msg := fmt.Sprintf("Created schedule %s (%q). Next run: %s.", sc.ID, sc.Name, strings.Join(when, ""))
	if len(when) > 1 {
		msg = fmt.Sprintf("Created schedule %s (%q). Next runs: %s.", sc.ID, sc.Name, strings.Join(when, "; "))
	}
	if w := cronDayWarning(sc.Cron); w != "" {
		msg += " " + w
	}
	return &toolResult{Text: msg}, nil
}

func parseLocalTime(s string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse time %q (use 2006-01-02T15:04)", s)
}

// Messages must stay glanceable; longer content belongs in a report.
const (
	sayHint = 300 // what the model is told
	sayMax  = 400 // what is refused
)

func say(tc *toolCtx, a map[string]any) (*toolResult, error) {
	text := cleanText(str(a, "text"))
	if text == "" {
		return nil, errors.New("empty message")
	}
	if n := utf8.RuneCountInString(text); n > sayMax {
		return &toolResult{Text: fmt.Sprintf("Not sent: %d characters is too long for a message. Keep messages under %d characters. Put the details in a report (present_report) and say a one-line takeaway.", n, sayHint)}, nil
	}
	r := tc.run
	if err := tc.rt.addEntry(tc.ctx, &store.Entry{UserID: r.user.ID, Kind: "say", Text: text}); err != nil {
		return nil, err
	}
	r.replied()
	if err := tc.rt.notify(tc.ctx, r.user, "", "Alexa", text, boolean(a, "push", false), boolean(a, "email", false)); err != nil {
		return &toolResult{Text: "Shown to the user, but the notification failed: " + err.Error()}, nil
	}
	return &toolResult{Text: "Said."}, nil
}

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
	tc.run.replied()
	return &toolResult{Suspend: &Needs{Kind: "question", Message: q, Options: opts}}, nil
}

func presentReport(tc *toolCtx, a map[string]any) (*toolResult, error) {
	r := tc.run
	rep := &store.Report{UserID: r.user.ID, Title: cleanText(str(a, "title")), Format: "markdown", Content: strings.TrimSpace(str(a, "content"))}
	if str(a, "format") == "html" {
		rep.Format = "html"
	} else {
		rep.Content = cleanText(rep.Content)
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
	if arr, ok := a["sources"].([]any); ok {
		for _, v := range arr {
			if m, ok := v.(map[string]any); ok && str(m, "url") != "" {
				data.Sources = appendLink(data.Sources, Link{Title: str(m, "title"), URL: str(m, "url")})
			}
		}
	}
	var missing []string
	for _, f := range strList(a, "files") {
		full, err := r.env.Resolve(f)
		if err == nil {
			_, err = os.Stat(full)
		}
		if err != nil {
			missing = append(missing, f)
			continue
		}
		data.Files = append(data.Files, r.env.Rel(full))
	}
	if rep.Title == "" {
		return nil, errors.New("give the report a title")
	}
	if rep.Content == "" && len(data.Items) == 0 {
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
	tc.rt.showReport(tc.ctx, r.user, rep, updated)
	r.replied()
	msg := fmt.Sprintf("Report %s (%q) is open on the user's screen.", rep.ID, rep.Title)
	if len(missing) > 0 {
		msg += " These files don't exist and were left out: " + strings.Join(missing, ", ")
	}
	return &toolResult{Text: msg}, nil
}

func generateImage(tc *toolCtx, a map[string]any) (*toolResult, error) {
	full, err := tc.run.env.Resolve(str(a, "path"))
	if err != nil {
		return nil, err
	}
	img, err := tc.rt.generateImage(tc.ctx, str(a, "prompt"))
	if err != nil {
		return nil, err
	}
	// Name the file after what the model actually returned (often WebP).
	exts := map[string]string{"image/webp": ".webp", "image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif"}
	if ext, ok := exts[http.DetectContentType(img)]; ok && !strings.EqualFold(filepath.Ext(full), ext) && !(ext == ".jpg" && strings.EqualFold(filepath.Ext(full), ".jpeg")) {
		full = strings.TrimSuffix(full, filepath.Ext(full)) + ext
	}
	_ = tc.run.env.MkdirAll(filepath.Dir(full))
	if err := os.WriteFile(full, img, 0o600); err != nil {
		return nil, err
	}
	tc.run.env.Chown(full)
	return &toolResult{Text: "Saved image to " + tc.run.env.Rel(full) + ". To show it, attach it to a report (present_report files)."}, nil
}

func marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
