package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alexagent/internal/llm"
	"alexagent/internal/store"
)

// The compaction the user asks for lets go of the user lock while the
// model writes the summary (BH-2): a message sent meanwhile starts its run
// at once, and the summary, written without it, is dropped rather than
// folding the conversation out from under the run.
func TestCompactLetsGoOfTheLock(t *testing.T) {
	e := newProgressEnv(t)
	for _, msg := range []string{"Hi", "Find me a hotel in Lisbon"} {
		e.model.script(does(fc("say", map[string]any{"text": "On it.", "final": true})), does())
		e.send(msg)
		e.settle()
	}
	started, release := e.model.holdSummaries()
	errc := make(chan error, 1)
	go func() { errc <- e.rt.Compact(e.ctx, e.user) }()
	<-started
	g := newGate()
	defer func() {
		select {
		case <-g.out:
		default:
			g.open()
		}
	}()
	e.model.script(g.turn(fc("say", map[string]any{"text": "Sure, in May.", "final": true})), does())
	sent := make(chan error, 1)
	go func() { sent <- e.rt.Send(e.ctx, e.user, Message{Text: "Make it May"}) }()
	select {
	case err := <-sent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("Send waited for the summary")
	}
	g.wait() // the run is in its model call
	close(release)
	if err := <-errc; !errors.Is(err, ErrBusy) {
		t.Errorf("Compact = %v, want ErrBusy: a run started while it wrote the summary", err)
	}
	g.open()
	e.settle()
	if ss, _ := e.st.GetSession(e.ctx, e.user.ID); ss.Compactions != 0 || ss.Summary != "" {
		t.Errorf("the dropped summary was stored: %d compactions, summary %q", ss.Compactions, ss.Summary)
	}
	conv := e.conversation()
	for _, want := range []string{"Hi", "Find me a hotel in Lisbon", "Make it May", "Sure, in May."} {
		if !strings.Contains(conv, want) {
			t.Errorf("the conversation lost %q", want)
		}
	}
}

// A compaction deletes the items it folds, and the screenshots only they
// showed; what's replayed next is the summary and the items after it
// (BH-2, BH-3).
func TestCompactDeletesWhatItFolds(t *testing.T) {
	e := newProgressEnv(t)
	uid := e.user.ID
	env, err := e.rt.envs.Ensure(uid)
	if err != nil {
		t.Fatal(err)
	}
	loc := userLoc(e.user)
	shotOutput := func(text, shot string) json.RawMessage {
		return marshal(storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": lastCallID(), "output": text}), Shot: shot})
	}
	items := []json.RawMessage{storedMsg("user", stamp(loc, "", "Open the hotel page"))}
	items = append(items, storedCall("browser_open", map[string]any{"url": "https://hotel.example"}))
	items = append(items, shotOutput("URL: https://hotel.example", "folded.jpg"))
	for i := 0; i < 6; i++ {
		items = append(items, storedMsg("user", stamp(loc, "", fmt.Sprintf("Question %d: %s", i, strings.Repeat("more detail ", 100)))))
		items = append(items, storedCall("say", map[string]any{"text": fmt.Sprintf("Answer %d", i)}))
		items = append(items, storedOutput(lastCallID(), "Sent.", ""))
	}
	items = append(items, storedMsg("user", stamp(loc, "", "Open it again")))
	items = append(items, storedCall("browser_open", map[string]any{"url": "https://hotel.example/rooms"}))
	items = append(items, shotOutput("URL: https://hotel.example/rooms", "kept.jpg"))
	if err := e.st.AppendItems(e.ctx, uid, items); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for _, name := range []string{"folded.jpg", "kept.jpg", "stray.jpg", "fresh.jpg"} {
		path := filepath.Join(env.Shots, name)
		if err := os.WriteFile(path, []byte("jpeg"), 0o600); err != nil {
			t.Fatal(err)
		}
		if name != "fresh.jpg" {
			_ = os.Chtimes(path, old, old)
		}
	}

	if err := e.rt.Compact(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	ss, rows, err := e.st.Conversation(e.ctx, uid)
	if err != nil || ss.Compactions != 1 || len(rows) == 0 {
		t.Fatalf("after compacting: %+v, %d items, %v", ss, len(rows), err)
	}
	all, _ := e.st.Items(e.ctx, uid, 0)
	for _, r := range all {
		if r.ID <= ss.CompactedThrough {
			t.Errorf("item %d is still stored, folded into the summary (through %d)", r.ID, ss.CompactedThrough)
		}
	}
	for name, want := range map[string]bool{"folded.jpg": false, "stray.jpg": false, "kept.jpg": true, "fresh.jpg": true} {
		if _, err := os.Stat(filepath.Join(env.Shots, name)); (err == nil) != want {
			t.Errorf("%s: kept = %v, want %v", name, err == nil, want)
		}
	}

	var input string
	e.model.script(func(req *llm.Request) []any {
		input = inputOf(req)
		return []any{fc("say", map[string]any{"text": "The first one.", "final": true})}
	}, does())
	e.send("Which one should I book?")
	e.settle()
	for _, want := range []string{"Finding a hotel in Lisbon", "Open it again", "Which one should I book?"} {
		if !strings.Contains(input, want) {
			t.Errorf("the next turn's input lacks %q:\n%s", want, tailOf(input, 800))
		}
	}
}

// A reset deletes the screenshots of the conversation it forgets (BH-3).
func TestResetDeletesShots(t *testing.T) {
	e := newProgressEnv(t)
	env, err := e.rt.envs.Ensure(e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.AppendItems(e.ctx, e.user.ID, []json.RawMessage{marshal(storedItem{Item: marshal(map[string]any{"type": "function_call_output", "call_id": "c1", "output": "URL: https://a.example"}), Shot: "a.jpg"})}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", "b.jpg"} {
		if err := os.WriteFile(filepath.Join(env.Shots, name), []byte("jpeg"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.rt.Reset(e.ctx, e.user); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(env.Shots); len(left) != 0 {
		t.Errorf("%d screenshots left after a reset", len(left))
	}
}

// The sweep deletes the image tokens of deleted users, and those nothing
// shows that weren't minted or shown again within the grace; the rest stay
// (BH-3).
func TestSweepImageTokens(t *testing.T) {
	e := newProgressEnv(t)
	other, err := e.st.CreateUser(e.ctx, "bo", "x", "Bo", false)
	if err != nil {
		t.Fatal(err)
	}
	mint := func(userID int64, src string, age time.Duration) string {
		t.Helper()
		u, ok := e.rt.mintImage(userID, src)
		if !ok {
			t.Fatal("mint failed")
		}
		token := strings.TrimPrefix(u, "/api/img/")
		if age > 0 {
			at := time.Now().Add(-age)
			_ = os.Chtimes(e.rt.imageTokenPath(token), at, at)
		}
		return token
	}
	old := imageTokenGrace + time.Hour
	shownOld := mint(e.user.ID, "https://img.example/shown.jpg", old)
	inReport := mint(e.user.ID, "https://img.example/report.jpg", old)
	unshownOld := mint(e.user.ID, "https://img.example/gone.jpg", old)
	unshownNew := mint(e.user.ID, "https://img.example/new.jpg", 0)
	shownAgain := mint(e.user.ID, "https://img.example/again.jpg", old)
	mint(e.user.ID, "https://img.example/again.jpg", 0)
	deleted := mint(other.ID, "https://img.example/theirs.jpg", 0)
	dir := filepath.Dir(e.rt.imageTokenPath(shownOld))
	for name, age := range map[string]time.Duration{"x.json.tmp": old, "y.json.tmp": 0} {
		path := filepath.Join(dir, name)
		_ = os.WriteFile(path, []byte("{"), 0o600)
		if age > 0 {
			at := time.Now().Add(-age)
			_ = os.Chtimes(path, at, at)
		}
	}
	_ = e.st.AddEntry(e.ctx, &store.Entry{UserID: e.user.ID, Kind: "say", Text: "Here", Data: marshal(map[string]any{"blocks": []any{map[string]any{"type": "image", "src": "/api/img/" + shownOld}}})})
	_ = e.st.SaveReport(e.ctx, &store.Report{ID: "r1", UserID: e.user.ID, Title: "Trip", Content: "![](/api/img/" + inReport + ")"})
	if err := e.st.DeleteUser(e.ctx, other.ID); err != nil {
		t.Fatal(err)
	}

	e.rt.Sweep(e.ctx)
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	for token, want := range map[string]bool{shownOld: true, inReport: true, unshownNew: true, shownAgain: true, unshownOld: false, deleted: false} {
		if exists(token+".json") != want {
			t.Errorf("token %s…: kept = %v, want %v", token[:6], !want, want)
		}
	}
	if exists("x.json.tmp") || !exists("y.json.tmp") {
		t.Errorf("stray files: old kept = %v, new kept = %v", exists("x.json.tmp"), exists("y.json.tmp"))
	}
	if src, err := e.rt.ImageSource(e.user.ID, shownOld); err != nil || src != "https://img.example/shown.jpg" {
		t.Errorf("a token still shown no longer resolves: %q, %v", src, err)
	}
}
