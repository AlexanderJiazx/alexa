package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// A page's alert/confirm/prompt pauses its renderer until it's answered.
// alert and beforeunload are accepted at once (nothing to decide, and the
// reply is reported in the next state). confirm and prompt are held for the
// agent: the page stays paused, state shows the dialog, and the agent
// answers it with browser_answer_dialog. A held dialog is auto-dismissed
// after a while so a forgotten one can't freeze the tab for good.
type dialog struct {
	Type    string // confirm | prompt | beforeunload | alert
	Message string
	Default string // a prompt's default text
	page    context.Context
	timer   *time.Timer
}

const dialogHoldTimeout = 2 * time.Minute

func (t *Tab) onDialog(pctx context.Context, e *page.EventJavascriptDialogOpening) {
	typ := string(e.Type)
	t.mu.Lock()
	human := time.Since(t.humanAt) < 5*time.Second
	t.mu.Unlock()
	// A person in control can't see dialogs in the live view; what they
	// clicked goes ahead, as it always has.
	if (typ != "confirm" && typ != "prompt") || human {
		// alert / beforeunload: accept and note it for the next state.
		t.mu.Lock()
		t.dialogs = append(t.dialogs, fmt.Sprintf("%s dialog: %q", typ, e.Message))
		t.mu.Unlock()
		go func() { _ = chromedp.Run(pctx, page.HandleJavaScriptDialog(true)) }()
		return
	}
	d := &dialog{Type: typ, Message: e.Message, Default: e.DefaultPrompt, page: pctx}
	d.timer = time.AfterFunc(dialogHoldTimeout, func() {
		if t.clearDialog(d) {
			_ = chromedp.Run(pctx, page.HandleJavaScriptDialog(false))
			t.mu.Lock()
			t.dialogs = append(t.dialogs, fmt.Sprintf("%s dialog %q was dismissed after no answer", typ, e.Message))
			t.mu.Unlock()
		}
	})
	t.mu.Lock()
	t.dialog = d
	if t.dialogWait != nil {
		close(t.dialogWait)
		t.dialogWait = nil
	}
	t.mu.Unlock()
}

// input dispatches input events. A click or key that opens a confirm or
// prompt doesn't complete until the dialog is answered, so this returns as
// soon as one opens and lets the events finish in the background.
func (t *Tab) input(timeout time.Duration, actions ...chromedp.Action) error {
	if err := t.blocked(); err != nil {
		return err
	}
	sig := make(chan struct{})
	t.mu.Lock()
	t.dialogWait = sig
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		if t.dialogWait == sig {
			t.dialogWait = nil
		}
		t.mu.Unlock()
	}()
	done := make(chan error, 1)
	go func() { done <- t.run(timeout, actions...) }()
	select {
	case err := <-done:
		return err
	case <-sig:
		return nil
	}
}

func (t *Tab) pendingDialog() *dialog {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dialog
}

// clearDialog removes d if it's still the pending one; reports whether it
// was.
func (t *Tab) clearDialog(d *dialog) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dialog != d {
		return false
	}
	t.dialog = nil
	return true
}

// answer replies to a held dialog.
func (t *Tab) answer(d *dialog, accept bool, text string) error {
	if d.timer != nil {
		d.timer.Stop()
	}
	if !t.clearDialog(d) {
		return nil // already resolved
	}
	act := page.HandleJavaScriptDialog(accept)
	if accept && d.Type == "prompt" {
		act = act.WithPromptText(text)
	}
	ctx, cancel := context.WithTimeout(d.page, 5*time.Second)
	defer cancel()
	return chromedp.Run(ctx, act)
}

func (d *dialog) describe() string {
	switch d.Type {
	case "prompt":
		def := ""
		if d.Default != "" {
			def = fmt.Sprintf(" (default text: %q)", d.Default)
		}
		return fmt.Sprintf("The page is showing a prompt dialog asking: %q%s\nIt's waiting for you: call browser_answer_dialog with accept=true and your text, or accept=false to dismiss it. Nothing else on the page works until then.", d.Message, def)
	case "confirm":
		return fmt.Sprintf("The page is showing a confirm dialog: %q\nIt's waiting for you: call browser_answer_dialog with accept=true to click OK or accept=false for Cancel. Nothing else on the page works until then.", d.Message)
	}
	return fmt.Sprintf("The page is showing a %s dialog: %q. Answer it with browser_answer_dialog.", d.Type, d.Message)
}

// blocked reports an error when a dialog is holding the page, so other
// actions tell the agent to deal with it first.
func (t *Tab) blocked() error {
	if d := t.pendingDialog(); d != nil {
		return fmt.Errorf("a %s dialog is open and the page is frozen until it's answered: %q. Use browser_answer_dialog first", d.Type, d.Message)
	}
	return nil
}
