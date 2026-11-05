package agent

import (
	"encoding/json"
	"testing"
)

// A call that may have written workspace files tells the user's other
// screens, so a Files tab open on one fetches the list again (WP-18). So
// does a browser download, which shows in the page state after it. A call
// that only reads tells them nothing.
func TestFilesChangedAfterAWrite(t *testing.T) {
	e := newProgressEnv(t)
	e.fakeTool("browser_open", func(map[string]any) (*toolResult, error) {
		return page("Note: downloaded menu.pdf to downloads/ (12 KB).", "https://bistro.example/menu.pdf", "Menu"), nil
	})
	e.model.script(
		does(fc("write_file", map[string]any{"path": "notes.txt", "content": "Table for two at 8", "status": "Saving the booking"})),
		does(fc("read_file", map[string]any{"path": "notes.txt", "status": "Checking the booking"})),
		does(fc("browser_open", map[string]any{"url": "https://bistro.example/menu.pdf", "status": "Getting the menu"})),
		does(fc("say", map[string]any{"text": "Booked, and the menu is in your files.", "final": true})),
	)
	e.send("Book the bistro for two at 8 and get me their menu")
	e.settle()
	files := 0
	for _, ev := range e.events("changed") {
		var c struct{ What string }
		if err := json.Unmarshal(ev.Data, &c); err != nil {
			t.Fatal(err)
		}
		if c.What == "files" {
			files++
		}
	}
	if files != 2 {
		t.Errorf("files changes = %d, want 2: one for the write, one for the download", files)
	}
}

// fetch_url writes a file only when it saves the response; the file tools
// that only look write nothing.
func TestWritesFiles(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want bool
	}{
		{"write_file", map[string]any{"path": "a.txt"}, true},
		{"run_shell", map[string]any{"command": "ls"}, true},
		{"generate_image", map[string]any{"path": "a.png"}, true},
		{"fetch_url", map[string]any{"url": "https://a.example", "save_as": "a.pdf"}, true},
		{"fetch_url", map[string]any{"url": "https://a.example"}, false},
		{"read_file", map[string]any{"path": "a.txt"}, false},
		{"list_files", nil, false},
	} {
		if got := writesFiles(c.name, c.args); got != c.want {
			t.Errorf("writesFiles(%s, %v) = %v, want %v", c.name, c.args, got, c.want)
		}
	}
}
