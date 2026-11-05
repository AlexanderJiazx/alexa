package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// nudge is added when Alexa ends its turn without answering the user in a
// way they can see.
const nudge = "[Alexa system note] The user can't see plain text or reasoning — only say, ask and present_report reach them. Answer their last message now with say (a few words when you've done what they asked, one or two short sentences otherwise), then end your turn."

// outOfSteps closes a stretch of work that used all its turns: Alexa tells
// the user where things stand instead of stopping without a word.
const outOfSteps = "[Alexa system note] That's all the steps for this stretch of work. Call no more tools except say, ask or present_report: tell the user what you've done and what's left, and that you can carry on if they want."

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

	retriedOverflow, retriedAttach := false, false
	for turn := 0; ; turn++ {
		if turn > rt.cfg.MaxTurns {
			rt.fail(r, errOutOfSteps(rt.cfg.MaxTurns))
			return
		}
		wrapUp := turn == rt.cfg.MaxTurns
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
		if wrapUp {
			if err := rt.appendMessage(ctx, uid, "system", outOfSteps, nil); err != nil {
				rt.fail(r, err)
				return
			}
		}
		if err := rt.compact(ctx, r.user, false); err != nil {
			slog.Warn("compaction failed", "user", uid, "err", err)
		}
		if ctx.Err() != nil {
			rt.stopped(r)
			return
		}
		// Compacting can take a while: what the user sent meanwhile is read
		// in this turn, not after a turn written without it. So is a hand
		// back's note; after a take-over past this mark, the turn's calls
		// aimed at the page don't run (runBatch).
		r.markTurn()
		if err := rt.drainInbox(ctx, r); err != nil {
			rt.fail(r, err)
			return
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
		resp, err := rt.streamModel(ctx, r, turn, req, func(ev llm.Event) {
			if firstEvent == 0 {
				firstEvent = time.Since(t0)
			}
			if firstOut == 0 && strings.HasSuffix(ev.Type, ".delta") {
				firstOut = time.Since(t0)
			}
			// Web searches run inside the model's turn, and the reasoning
			// headline and the message being written show as they stream.
			rt.onStreamEvent(r, ev)
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
			// A picture or file the model can't read would fail every turn
			// from now on: set it aside once and try again (L3).
			if AttachmentRejected(err) && !retriedAttach {
				retriedAttach = true
				if names, qerr := rt.QuarantineAttachments(ctx, uid); qerr == nil && len(names) > 0 {
					slog.Warn("model refused an attachment; set aside", "user", uid, "names", names)
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
			if !wrapUp && r.shouldNudge() {
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
			if wrapUp {
				turn = -1 // a message came in meanwhile: a new stretch of work
			}
			continue
		}

		// The batch runs in exec.go (runBatch): nothing after a question,
		// nothing before it that would act, nothing aimed at a page that
		// changed, nothing new once the user has written, and every output
		// saved even when Alexa is stopped mid-batch. A call the output limit
		// cut off isn't run: its arguments are incomplete (cutoff.go).
		calls, cutOff := splitCutOff(resp, calls)
		b := rt.runBatch(ctx, r, calls, wrapUp)
		if b.err == nil && len(cutOff) > 0 {
			b.err = rt.notRunCutOff(ctx, uid, cutOff)
			b.final = false // the model meant to do more
		}
		if b.err != nil {
			rt.fail(r, b.err)
			return
		}
		if ctx.Err() != nil {
			rt.stopped(r)
			return
		}
		if b.suspend != nil {
			if rt.suspend(r, b.suspend) {
				return
			}
			if wrapUp {
				turn = -1 // the user had answered already: a new stretch of work
			}
			continue
		}
		if b.final && !wrapUp {
			// Alexa said she's done: no extra turn to find that out (H3).
			if rt.finish(r) {
				return
			}
			continue
		}
		if wrapUp {
			if !b.told {
				rt.fail(r, errOutOfSteps(rt.cfg.MaxTurns))
				return
			}
			if rt.finish(r) {
				return
			}
			turn = -1 // a message came in meanwhile: a new stretch of work
		}
	}
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

func (rt *Runtime) drainInbox(ctx context.Context, r *run) error {
	select { // a wake-up for messages about to be read is stale
	case <-r.wake:
	default:
	}
	// Taken from the inbox and added to the conversation in one step, so
	// a Stop or a crash in between can't lose them (L14).
	msgs, err := rt.moveInbox(context.WithoutCancel(ctx), r.user.ID, nil)
	if err != nil {
		return err
	}
	read := false
	for _, m := range msgs {
		if m.Kind == "user" {
			r.mu.Lock()
			r.awaiting, r.nudged, r.held, r.job = true, false, false, false
			if r.steer && m.ID > r.steerAt {
				// The interrupt is read in this turn: what the model writes
				// with it runs (exec.go).
				r.steer, r.steerAt = false, 0
			}
			r.mu.Unlock()
			read = true
		}
	}
	if read {
		rt.publishState(r.user.ID) // nothing of the user's is queued now
	}
	return nil
}
