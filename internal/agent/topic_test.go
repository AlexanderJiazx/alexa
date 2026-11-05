package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"alexagent/internal/store"
)

// judged turns the topic call on with a stand-in for the model: answer
// says what it replies to each input, and the inputs are kept.
type judged struct {
	mu     sync.Mutex
	inputs []string
}

func (e *progressEnv) judgeTopics(answer func(input string) (string, error)) *judged {
	j := &judged{}
	e.cfg.TopicJudge = true
	e.rt.judgeTopic = func(_ context.Context, _, input string) (string, error) {
		j.mu.Lock()
		j.inputs = append(j.inputs, input)
		j.mu.Unlock()
		return answer(input)
	}
	return j
}

func (j *judged) calls() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.inputs...)
}

// lastUser is the newest user entry's data, once the topic call on it is
// back.
func (e *progressEnv) lastUser() map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		users := entriesOf(e.feed(), "user")
		if len(users) == 0 {
			e.t.Fatal("no user entry")
		}
		d := map[string]any{}
		_ = json.Unmarshal(users[len(users)-1].Data, &d)
		if d["topic"] == nil {
			return d
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("the topic call never came back: %v", d)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A follow-up carries on the topic: its entry says so, and the report the
// topic left open stays on screen.
func TestFollowUpKeepsTheTopic(t *testing.T) {
	e := newProgressEnv(t)
	j := e.judgeTopics(func(string) (string, error) { return "Same", nil })
	e.model.script(does(report("Lisbon hotels", nil)))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	if d := e.lastUser(); d["start"] != true || d["followUp"] != nil {
		t.Errorf("the first message: %v, want a plain start", d)
	}
	if n := len(j.calls()); n != 0 {
		t.Errorf("the topic call was made %d times with no topic on screen", n)
	}
	id, _ := e.lastReport()

	e.model.script(does(fc("say", map[string]any{"text": "Done. Casa Alfama is €118.", "final": true})))
	e.send("Make it under €120")
	e.settle()
	if d := e.lastUser(); d["start"] != true || d["followUp"] != true {
		t.Errorf("the follow-up: %v, want start and followUp", d)
	}
	calls := j.calls()
	if len(calls) != 1 {
		t.Fatalf("topic calls = %d, want 1", len(calls))
	}
	for _, want := range []string{"User: Find me a quiet hotel in Lisbon", "Alexa: (A report) Lisbon hotels", "New message: Make it under €120"} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("the topic call doesn't read %q:\n%s", want, calls[0])
		}
	}
	if s := e.surface(); s != (Surface{Kind: "report", ID: id}) {
		t.Errorf("a follow-up closed the topic's report: surface = %+v", s)
	}

	// The thread reaches back through the follow-up to the topic's start.
	e.model.script(does(fc("say", map[string]any{"text": "Booked.", "final": true})))
	e.send("Book it")
	e.settle()
	e.lastUser()
	calls = j.calls()
	if last := calls[len(calls)-1]; !strings.Contains(last, "User: Find me a quiet hotel in Lisbon\nUser: Make it under €120\nAlexa: Done. Casa Alfama is €118.") {
		t.Errorf("the second call doesn't read the whole topic:\n%s", last)
	}
}

// A new topic, or a call that fails, starts the screen afresh: the panel
// the last topic left open closes once the answer is in.
func TestNewTopicClosesThePanel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(string) (string, error)
	}{
		{"new", func(string) (string, error) { return "new", nil }},
		{"failed", func(string) (string, error) { return "", errors.New("timeout") }},
		{"unclear", func(string) (string, error) { return "It's hard to say.", nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newProgressEnv(t)
			e.judgeTopics(tc.answer)
			e.model.script(does(report("Lisbon hotels", nil)))
			e.send("Find me a quiet hotel in Lisbon")
			e.settle()
			e.model.script(does(fc("say", map[string]any{"text": "It's 18°C and sunny.", "final": true})))
			e.send("What's the weather in Porto?")
			e.settle()
			if d := e.lastUser(); d["start"] != true || d["followUp"] != nil {
				t.Errorf("entry data = %v, want a plain start", d)
			}
			if s := e.surface(); s != (Surface{}) {
				t.Errorf("a new topic kept the last one's panel: %+v", s)
			}
		})
	}
}

// When Alexa has moved the screen before the answer is back, a new topic
// leaves it where she put it.
func TestLateNewTopicLeavesTheScreen(t *testing.T) {
	e := newProgressEnv(t)
	e.fakeBrowser()
	release := make(chan struct{})
	e.judgeTopics(func(string) (string, error) { <-release; return "new", nil })
	e.model.script(does(report("Lisbon hotels", nil)))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	e.model.script(does(openShop()), does(fc("say", map[string]any{"text": "United's site is open."})), does())
	e.send("Open United's site")
	e.settle()
	if s := e.surface(); s.Kind != "browser" {
		t.Fatalf("after browsing, surface = %+v", s)
	}
	close(release)
	e.lastUser()
	if s := e.surface(); s.Kind != "browser" {
		t.Errorf("a late answer closed the browser Alexa opened: %+v", s)
	}
}

// A scheduled job never carries on the topic, and isn't asked about.
func TestScheduledJobIsANewTopic(t *testing.T) {
	e := newProgressEnv(t)
	j := e.judgeTopics(func(string) (string, error) { return "same", nil })
	e.model.script(does(fc("say", map[string]any{"text": "It's 18°C.", "final": true})))
	e.send("What's the weather in Porto?")
	e.settle()
	e.model.script(does(fc("say", map[string]any{"text": "Three new emails.", "final": true})))
	if err := e.rt.Send(e.ctx, e.user, Message{Text: "Check my inbox", Source: "schedule", Label: "Inbox"}); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if n := len(j.calls()); n != 0 {
		t.Errorf("a scheduled job made %d topic calls", n)
	}
	notes := entriesOf(e.feed(), "note")
	var d map[string]any
	_ = json.Unmarshal(notes[len(notes)-1].Data, &d)
	if d["start"] != true || d["topic"] != nil || d["followUp"] != nil {
		t.Errorf("the job's entry: %v", d)
	}
}

// currentThread reads the topic as the app shows it: back through
// follow-ups, past a scheduled job that had nothing to say (though it made
// a plan).
func TestCurrentThread(t *testing.T) {
	e := newProgressEnv(t)
	add := func(kind, text string, data map[string]any) {
		en := &store.Entry{UserID: e.user.ID, Kind: kind, Text: text}
		if data != nil {
			en.Data = marshal(data)
		}
		if err := e.st.AddEntry(e.ctx, en); err != nil {
			t.Fatal(err)
		}
	}
	add("user", "Plan a weekend in Kyoto", map[string]any{"start": true})
	add("say", "Here's a plan.", nil)
	add("user", "What's the weather in Porto?", map[string]any{"start": true})
	add("say", "18°C.", nil)
	add("user", "And tomorrow?", map[string]any{"start": true, "followUp": true})
	add("say", "Rain.", nil)
	add("note", "Scheduled: Inbox", map[string]any{"start": true, "scheduled": true})
	add("plan", "Inbox", map[string]any{"plan": map[string]any{"title": "Inbox"}})
	add("step", "Read your inbox", nil)

	th, ok := e.rt.currentThread(e.ctx, e.user.ID)
	if !ok {
		t.Fatal("no thread")
	}
	if got := strings.Join(th.asks, " | "); got != "What's the weather in Porto? | And tomorrow?" {
		t.Errorf("asks = %q", got)
	}
	if th.reply != "Rain." {
		t.Errorf("reply = %q", th.reply)
	}
	in := topicInput(th, "thanks", time.UnixMilli(th.at).Add(3*time.Hour))
	if !strings.Contains(in, "last active 3 hours ago") || !strings.HasSuffix(in, "New message: thanks") {
		t.Errorf("input:\n%s", in)
	}
}

// The model's answer, read leniently.
func TestFollowsUpReadsTheAnswer(t *testing.T) {
	e := newProgressEnv(t)
	for answer, want := range map[string]bool{"same": true, "Same.": true, "\"same\"": true, "**SAME**": true, "new": false, "": false, "not the same": false} {
		e.rt.judgeTopic = func(context.Context, string, string) (string, error) { return answer, nil }
		if got := e.rt.followsUp(e.ctx, thread{asks: []string{"x"}}, "y"); got != want {
			t.Errorf("answer %q: followsUp = %v, want %v", answer, got, want)
		}
	}
}

// The browser the last topic left open, and the new run is using again
// before the answer is back, stays: a late "new" can't tell it from the
// old one by what the screen shows.
func TestLateNewTopicKeepsTheBrowserInUse(t *testing.T) {
	e := newProgressEnv(t)
	e.fakeBrowser()
	release := make(chan struct{})
	e.model.script(does(openShop()), does(fc("say", map[string]any{"text": "United's site is open."})), does())
	e.send("Open United's site")
	e.settle()
	if s := e.surface(); s.Kind != "browser" {
		t.Fatalf("after browsing, surface = %+v", s)
	}
	e.judgeTopics(func(string) (string, error) { <-release; return "new", nil })
	e.model.script(does(openShop()), does(fc("say", map[string]any{"text": "Found 3 flights.", "final": true})))
	e.send("Find flights to Lisbon")
	e.settle()
	close(release)
	e.lastUser()
	if s := e.surface(); s.Kind != "browser" {
		t.Errorf("a late answer closed the browser the new run used: %+v", s)
	}
}

// The user's hold ends with their message, so the new run can use the
// screen while the call is out; the panel they chose stays either way.
func TestHoldEndsWhileTheCallIsOut(t *testing.T) {
	e := newProgressEnv(t)
	release := make(chan struct{})
	e.model.script(does(report("Lisbon hotels", nil)))
	e.send("Find me a quiet hotel in Lisbon")
	e.settle()
	id, _ := e.lastReport()
	e.setSurface(Surface{Kind: "report", ID: id})
	if s := e.surface(); !s.Held {
		t.Fatalf("the user's choice isn't held: %+v", s)
	}
	e.judgeTopics(func(string) (string, error) { <-release; return "new", nil })
	e.model.script(does(fc("say", map[string]any{"text": "It's 18°C.", "final": true})))
	e.send("What's the weather in Porto?")
	if s := e.surface(); s != (Surface{Kind: "report", ID: id}) {
		t.Errorf("while the call is out, surface = %+v, want the report, not held", s)
	}
	e.settle()
	close(release)
	e.lastUser()
	if s := e.surface(); s != (Surface{Kind: "report", ID: id}) {
		t.Errorf("a new topic closed the report the user chose: %+v", s)
	}
}

// A message whose call a stop cut short is settled as a new topic on the
// next start.
func TestLeftTopicsAreSettled(t *testing.T) {
	e := newProgressEnv(t)
	en := &store.Entry{UserID: e.user.ID, Kind: "user", Text: "And tomorrow?", Data: marshal(map[string]any{"start": true, "topic": "pending"})}
	if err := e.st.AddEntry(e.ctx, en); err != nil {
		t.Fatal(err)
	}
	e.rt.ResumeInterrupted(e.ctx)
	if d := e.lastUser(); d["start"] != true || d["followUp"] != nil {
		t.Errorf("left entry = %v, want a plain start", d)
	}
}

// A choice sent from a report shown earlier in the topic (before a
// follow-up) carries on the exchange.
func TestReportShownEarlierInTheTopic(t *testing.T) {
	e := newProgressEnv(t)
	add := func(kind, text string, data map[string]any) {
		en := &store.Entry{UserID: e.user.ID, Kind: kind, Text: text, Data: marshal(data)}
		if err := e.st.AddEntry(e.ctx, en); err != nil {
			t.Fatal(err)
		}
	}
	add("user", "Find me a hotel", map[string]any{"start": true})
	add("report", "Lisbon hotels", map[string]any{"id": "r1"})
	add("user", "Under €120", map[string]any{"start": true, "followUp": true})
	add("say", "Casa Alfama.", map[string]any{})
	if !e.rt.shownInExchange(e.ctx, e.user.ID, "r1") {
		t.Error("a report shown earlier in the topic counts as another topic's")
	}
	add("user", "Weather in Porto?", map[string]any{"start": true})
	if e.rt.shownInExchange(e.ctx, e.user.ID, "r1") {
		t.Error("a report from the topic before counts as this one's")
	}
}
