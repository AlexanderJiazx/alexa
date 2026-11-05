package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"alexagent/internal/hub"
	"alexagent/internal/store"
)

// A message that sets Alexa to work while she's idle starts a new exchange.
// Whether it carries on the topic before it or starts a new one is the
// model's call, a quick one made while Alexa gets going. A follow-up keeps
// the topic on screen, and the panel it has open; a new topic starts the
// screen afresh. Either way the earlier exchanges are in Reflect.
//
// The message's entry says which: data.followUp beside data.start when it
// carries on. While the call is out it has data.topic "pending", and the
// app shows it as the screen was: with the topic if it was on screen, on
// its own if the topic had been put away.

const (
	// Past this, a message starts a new topic whatever it says.
	topicGap = 72 * time.Hour
	// The call has this long (it usually takes one to three seconds). Without
	// an answer the message starts a new topic, as every message once did.
	topicTimeout = 8 * time.Second
)

const topicInstructions = `You sort the messages a user sends Alexa, their personal assistant, into topics. Given the conversation on screen and a new message, answer "same" if the new message carries on that conversation: it follows up on it, narrows or corrects it, answers Alexa, refers back to it ("that one", "the second", "and tomorrow?"), thanks her for it, or would make less sense without it. Answer "new" if it asks for something unrelated that stands on its own. Reply with the one word.`

// thread is what the call reads of the topic on screen.
type thread struct {
	asks  []string // what the user asked in it, latest last (at most 3)
	reply string   // Alexa's latest answer in it
	at    int64    // when it last changed (ms)
}

// entryFlags is what an entry's data says about the exchange it's in.
type entryFlags struct {
	Start     bool   `json:"start"`
	FollowUp  bool   `json:"followUp"`
	Topic     string `json:"topic"`
	Scheduled bool   `json:"scheduled"`
}

func flagsOf(e *store.Entry) entryFlags {
	var f entryFlags
	if len(e.Data) > 0 {
		_ = json.Unmarshal(e.Data, &f)
	}
	return f
}

// startBefore returns where the exchange that ends at feed[end] begins, as
// the app reads it (exchangeStart in state/exchange.ts): the last entry
// marked as a start, or for entries from before exchanges were marked, the
// last message or scheduled job.
func startBefore(feed []*store.Entry, flags []entryFlags, end int) int {
	for i := end - 1; i >= 0; i-- {
		if flags[i].Start {
			return i
		}
	}
	for i := end - 1; i >= 0; i-- {
		if feed[i].Kind == "user" || flags[i].Scheduled {
			return i
		}
	}
	return 0
}

// quietJob says whether an exchange is a scheduled job that hasn't had
// anything to say (its note, its plan and its steps): the app keeps those
// off the screen.
func quietJob(ex []*store.Entry, flags []entryFlags) bool {
	if len(ex) == 0 || !flags[0].Scheduled {
		return false
	}
	for _, e := range ex {
		if e.Kind != "step" && e.Kind != "note" && e.Kind != "plan" {
			return false
		}
	}
	return true
}

// currentThread reads the topic on screen from the end of the feed, as the
// app does: the latest exchange that isn't a quiet scheduled job, and the
// ones before it that it carried on from. ok is false when there's none.
func (rt *Runtime) currentThread(ctx context.Context, userID int64) (t thread, ok bool) {
	feed, err := rt.st.Feed(ctx, userID, 0, 300)
	if err != nil || len(feed) == 0 {
		return t, false
	}
	flags := make([]entryFlags, len(feed))
	for i, e := range feed {
		flags[i] = flagsOf(e)
	}
	for end := len(feed); end > 0; {
		s := startBefore(feed, flags, end)
		ex, fl := feed[s:end], flags[s:end]
		end = s
		if quietJob(ex, fl) {
			continue
		}
		if !ok {
			ok, t.at = true, ex[len(ex)-1].UpdatedAt
		}
		for i := len(ex) - 1; i >= 0; i-- {
			e := ex[i]
			switch {
			case t.reply == "" && (e.Kind == "say" || e.Kind == "ask"):
				t.reply = e.Text
			case t.reply == "" && e.Kind == "report":
				t.reply = "(A report) " + e.Text
			case len(t.asks) < 3 && (e.Kind == "user" || e.Kind == "note" && fl[i].Scheduled):
				t.asks = append([]string{e.Text}, t.asks...)
			}
		}
		if !fl[0].FollowUp {
			break
		}
	}
	return t, ok && (len(t.asks) > 0 || t.reply != "")
}

// topicInput is what the call reads: the topic on screen and the message.
func topicInput(t thread, msg string, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The conversation on screen (last active %s ago):\n", ago(now.Sub(time.UnixMilli(t.at))))
	for _, a := range t.asks {
		b.WriteString("User: " + oneLine(trim(a, 300)) + "\n")
	}
	if t.reply != "" {
		b.WriteString("Alexa: " + oneLine(trim(t.reply, 500)) + "\n")
	}
	b.WriteString("\nNew message: " + oneLine(trim(msg, 600)))
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// ago says roughly how long d is, in words.
func ago(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

// topicToJudge returns the topic a message that starts an exchange might
// carry on, when there's a call to make: only for what the user sends (a
// scheduled job always starts its own topic), and only while the topic on
// screen is recent.
func (rt *Runtime) topicToJudge(ctx context.Context, userID int64, kind string) (thread, bool) {
	if !rt.cfg.TopicJudge || kind != "user" || rt.judgeTopic == nil {
		return thread{}, false
	}
	t, ok := rt.currentThread(ctx, userID)
	if !ok || time.Since(time.UnixMilli(t.at)) > topicGap {
		return thread{}, false
	}
	return t, true
}

// followsUp asks the model whether msg carries on topic t. Anything but a
// clear "same", in time, is a new topic.
func (rt *Runtime) followsUp(ctx context.Context, t thread, msg string) bool {
	ctx, cancel := context.WithTimeout(ctx, topicTimeout)
	defer cancel()
	out, err := rt.judgeTopic(ctx, topicInstructions, topicInput(t, msg, time.Now()))
	if err != nil {
		slog.Warn("topic call failed", "err", err)
		return false
	}
	w := strings.Trim(strings.ToLower(strings.TrimSpace(out)), "\"'.*` ")
	return strings.HasPrefix(w, "same")
}

// pendingTopic is a message's topic call in flight: the topic it might
// carry on, and the screen as the message left it.
type pendingTopic struct {
	thread
	held  bool   // the user held the panel on screen, so it stays either way
	moves uint64 // surfaceMoves once the message was in
}

// settleTopic makes the call on a message that started an exchange while a
// topic was on screen, and writes the answer into its entry. A new topic
// closes the panel the last one left open, as onNewExchange does, unless
// the user chose it or the screen has been used since the message came (by
// the new run, or the user). A call cut short by a shutdown leaves the
// entry pending, for settleLeftTopics on the next start.
func (rt *Runtime) settleTopic(userID int64, e *store.Entry, p pendingTopic, msg string) {
	rt.wg.Add(1)
	go func() {
		defer rt.wg.Done()
		follow := rt.followsUp(rt.baseCtx, p.thread, msg)
		if rt.baseCtx.Err() != nil {
			return
		}
		ctx := context.Background()
		if !follow && !p.held && rt.surfaceMoves(userID) == p.moves {
			rt.surfaceOnNewExchange(ctx, userID, false)
		}
		rt.settleEntry(ctx, userID, e.ID, follow)
	}()
}

// settleEntry writes the topic call's answer into a message's entry, and
// nothing else of it: the entry may have changed since the message came
// in. Clients get the entry as it now is.
func (rt *Runtime) settleEntry(ctx context.Context, userID, id int64, follow bool) {
	e, err := rt.st.SettleTopic(ctx, userID, id, follow)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("settle topic", "user", userID, "entry", id, "err", err)
		}
		return
	}
	rt.hub.Publish(userID, hub.Event{Type: "entry", Data: e})
}

// settleLeftTopics decides the messages whose topic call a stop cut short:
// they start new topics, as every message did before the call. The screen
// is left as it is.
func (rt *Runtime) settleLeftTopics(ctx context.Context) {
	left, err := rt.st.PendingTopics(ctx)
	if err != nil {
		slog.Warn("settle topics", "err", err)
		return
	}
	for _, e := range left {
		rt.settleEntry(ctx, e.UserID, e.ID, false)
	}
}
