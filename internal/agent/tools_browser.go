package agent

import (
	"fmt"
	"time"

	"alexagent/internal/browser"
)

// browserTools act on the shared browser. Each returns the page's new state
// (see browserTool), except browser_read, which returns the page as text.
func browserTools() []*tool {
	return []*tool{
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
				u, title = tab.Redact(u), tab.Redact(title)
				more := ""
				if off+len(md) < total {
					more = fmt.Sprintf("\n\n[Showing %d–%d of %d characters. Call browser_read with offset=%d for more.]", off, off+len(md), total, off+len(md))
				}
				return &toolResult{Text: fmt.Sprintf("# %s\n%s\n\n%s%s", title, u, md, more), Kind: "long"}, nil
			},
		},
	}
}

func (tc *toolCtx) tab() (*browser.Tab, error) {
	b, err := tc.rt.browsers.Get(tc.run.user.ID)
	if err != nil {
		return nil, err
	}
	return b.Tab(tabKey)
}

// browserState returns the current page state as a tool result, prefixed by
// msg (which can quote the page, so it's kept clear of typed passwords).
func (tc *toolCtx) browserState(tab *browser.Tab, msg string) (*toolResult, error) {
	msg = tab.Redact(msg)
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
