package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alexagent/internal/config"
)

// The tool definitions are sent with every model call, right after the
// instructions, so their order and wording are part of the cached prefix too.
// This golden file pins them. run_shell's description lists what's installed
// on the machine, so that part is swapped for a placeholder before comparing.
func TestToolSchemasGolden(t *testing.T) {
	rt := &Runtime{cfg: &config.Config{ShellEnabled: true, ImageModel: "test-image-model"}}
	var defs []any
	for _, tl := range rt.buildTools() {
		defs = append(defs, tl.schema())
	}
	b, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	installed, _ := json.Marshal(shellTools())
	got := strings.Replace(string(b), strings.Trim(string(installed), `"`), "{{SHELL_TOOLS}}", 1) + "\n"

	path := filepath.Join("testdata", "tools.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Fatalf("tool definitions differ from %s; if the change is intended, run with -update and review the diff", path)
	}
}
