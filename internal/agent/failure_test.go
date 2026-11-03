package agent

import (
	"strings"
	"testing"
)

// Fix conversation sets aside an older picture only when the model named a
// picture or a file it couldn't read. After a refusal of anything else (a
// policy refusal, a bad tool schema) the picture it read before stays, and
// nothing is tried again, since nothing changed (LLM-6).
func TestRepairOnlyWhenAFileWasRefused(t *testing.T) {
	for _, c := range []struct {
		name, code, msg string
		aside           bool
	}{
		{"policy", "invalid_prompt", "Invalid prompt: your prompt was flagged as potentially violating our usage policy.", false},
		{"picture", "invalid_image", "Invalid image data: the image could not be decoded.", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newProgressEnv(t)
			env, err := e.rt.envs.Ensure(e.user.ID)
			if err != nil {
				t.Fatal(err)
			}
			photo := put(t, env, "photo.png", pngBytes(t, 20, 20))
			refusal := sse{"type": "error", "code": c.code, "message": c.msg}
			e.model.script(does(fc("say", map[string]any{"text": "A nice photo.", "final": true})), does(refusal))
			if c.aside {
				e.model.script(does(fc("say", map[string]any{"text": "Done.", "final": true})))
			}
			if err := e.rt.Send(e.ctx, e.user, Message{Text: "What's this?", Attach: []Attachment{photo}}); err != nil {
				t.Fatal(err)
			}
			e.settle()
			e.send("Describe it in detail")
			e.settle()
			if fs := failuresOf(e.feed()); len(fs) != 1 || fs[0].Reason != "invalid_input" {
				t.Fatalf("failures = %+v", fs)
			}
			names, err := e.rt.Repair(e.ctx, e.user)
			if err != nil {
				t.Fatal(err)
			}
			e.settle()
			if got := len(names) == 1 && names[0] == "photo.png"; got != c.aside {
				t.Errorf("set aside %v, want the photo: %v", names, c.aside)
			}
			in, err := e.rt.buildInput(e.ctx, e.user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if shown := strings.Contains(string(marshal(in)), "input_image"); shown == c.aside {
				t.Errorf("photo still shown = %v", shown)
			}
			if n, want := len(e.model.requests()), map[bool]int{false: 2, true: 3}[c.aside]; n != want {
				t.Errorf("%d model calls, want %d", n, want)
			}
		})
	}
}
