package browser

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/input"
)

type HumanInput struct {
	Kind      string  `json:"kind"` // mousedown | mouseup | mousemove | wheel | keydown | keyup | text | navigate | back | forward | reload
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Button    string  `json:"button"`
	Buttons   int64   `json:"buttons"` // the buttons held down, as in a DOM MouseEvent: 1 left, 2 right, 4 middle, 8 back, 16 forward
	Clicks    int64   `json:"clicks"`
	DX        float64 `json:"dx"`
	DY        float64 `json:"dy"`
	Key       string  `json:"key"`
	Code      string  `json:"code"`
	KeyCode   int64   `json:"keyCode"`
	Text      string  `json:"text"`
	Modifiers int64   `json:"modifiers"`
	URL       string  `json:"url"`
}

// Human applies an input event from a person controlling the browser.
func (t *Tab) Human(ev HumanInput) error {
	t.b.touch()
	t.mu.Lock()
	t.humanAt = time.Now()
	t.mu.Unlock()
	// A dialog the agent left open: they can't see it, and the page ignores
	// them until it's answered.
	if d := t.pendingDialog(); d != nil {
		_ = t.answer(d, false, "")
	}
	btn, bit := input.Left, int64(1)
	switch ev.Button {
	case "right":
		btn, bit = input.Right, 2
	case "middle":
		btn, bit = input.Middle, 4
	}
	mods := input.Modifier(ev.Modifiers)
	switch ev.Kind {
	case "mousemove":
		// A move with no button held tells the page the button is up,
		// which ends a drag, a text selection or a slider pull; so a move
		// carries the buttons the person is holding, as a real mouse's do.
		move := input.DispatchMouseEvent(input.MouseMoved, ev.X, ev.Y).WithModifiers(mods)
		if ev.Buttons > 0 {
			move = move.WithButton(heldButton(ev.Buttons)).WithButtons(ev.Buttons)
		}
		return t.run(5*time.Second, move)
	case "mousedown":
		c := ev.Clicks
		if c < 1 {
			c = 1
		}
		return t.run(5*time.Second, input.DispatchMouseEvent(input.MousePressed, ev.X, ev.Y).WithButton(btn).WithButtons(ev.Buttons|bit).WithClickCount(c).WithModifiers(mods))
	case "mouseup":
		c := ev.Clicks
		if c < 1 {
			c = 1
		}
		return t.run(5*time.Second, input.DispatchMouseEvent(input.MouseReleased, ev.X, ev.Y).WithButton(btn).WithButtons(ev.Buttons&^bit).WithClickCount(c).WithModifiers(mods))
	case "wheel":
		return t.run(5*time.Second, input.DispatchMouseEvent(input.MouseWheel, ev.X, ev.Y).WithDeltaX(ev.DX).WithDeltaY(ev.DY))
	case "keydown", "keyup":
		typ := input.KeyRawDown
		if ev.Kind == "keyup" {
			typ = input.KeyUp
		}
		k := input.DispatchKeyEvent(typ).WithKey(ev.Key).WithCode(ev.Code).WithWindowsVirtualKeyCode(ev.KeyCode).WithNativeVirtualKeyCode(ev.KeyCode).WithModifiers(mods)
		if ev.Kind == "keydown" && ev.Text != "" {
			k = input.DispatchKeyEvent(input.KeyDown).WithKey(ev.Key).WithCode(ev.Code).WithWindowsVirtualKeyCode(ev.KeyCode).WithNativeVirtualKeyCode(ev.KeyCode).WithText(ev.Text).WithUnmodifiedText(ev.Text).WithModifiers(mods)
		}
		return t.run(5*time.Second, k)
	case "text":
		return t.run(5*time.Second, input.InsertText(ev.Text))
	case "navigate":
		// Refused here, before anything happens, so the URL bar can say so.
		u, err := navTarget(ev.URL)
		if err != nil {
			return err
		}
		go func() { _ = t.Navigate(u) }()
		return nil
	case "back":
		go func() { _ = t.History(-1) }()
		return nil
	case "forward":
		go func() { _ = t.History(1) }()
		return nil
	case "reload":
		go func() { _ = t.Reload() }()
		return nil
	}
	return fmt.Errorf("unknown input kind %q", ev.Kind)
}

// heldButton is the button a move reports while several may be held: the
// first of left, right, middle, back and forward, as Chrome's own input
// does.
func heldButton(buttons int64) input.MouseButton {
	for _, b := range []struct {
		bit int64
		btn input.MouseButton
	}{{1, input.Left}, {2, input.Right}, {4, input.Middle}, {8, input.Back}, {16, input.Forward}} {
		if buttons&b.bit != 0 {
			return b.btn
		}
	}
	return input.None
}

type keyDef struct {
	key, code, text string
	vk              int64
}

var namedKeys = map[string]keyDef{
	"enter":      {"Enter", "Enter", "\r", 13},
	"return":     {"Enter", "Enter", "\r", 13},
	"tab":        {"Tab", "Tab", "", 9},
	"escape":     {"Escape", "Escape", "", 27},
	"esc":        {"Escape", "Escape", "", 27},
	"backspace":  {"Backspace", "Backspace", "", 8},
	"delete":     {"Delete", "Delete", "", 46},
	"arrowup":    {"ArrowUp", "ArrowUp", "", 38},
	"arrowdown":  {"ArrowDown", "ArrowDown", "", 40},
	"arrowleft":  {"ArrowLeft", "ArrowLeft", "", 37},
	"arrowright": {"ArrowRight", "ArrowRight", "", 39},
	"up":         {"ArrowUp", "ArrowUp", "", 38},
	"down":       {"ArrowDown", "ArrowDown", "", 40},
	"left":       {"ArrowLeft", "ArrowLeft", "", 37},
	"right":      {"ArrowRight", "ArrowRight", "", 39},
	"home":       {"Home", "Home", "", 36},
	"end":        {"End", "End", "", 35},
	"pageup":     {"PageUp", "PageUp", "", 33},
	"pagedown":   {"PageDown", "PageDown", "", 34},
	"space":      {" ", "Space", " ", 32},
}

func lookupKey(name string) keyDef {
	if k, ok := namedKeys[strings.ToLower(name)]; ok {
		return k
	}
	if strings.HasPrefix(strings.ToLower(name), "f") {
		if n, err := strconv.Atoi(name[1:]); err == nil && n >= 1 && n <= 12 {
			return keyDef{name, name, "", int64(111 + n)}
		}
	}
	if len([]rune(name)) == 1 {
		r := []rune(name)[0]
		up := strings.ToUpper(name)
		code := "Key" + up
		if r >= '0' && r <= '9' {
			code = "Digit" + name
		}
		return keyDef{name, code, name, int64([]rune(up)[0])}
	}
	return keyDef{name, name, "", 0}
}
