package agent

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alexagent/internal/store"
)

// updateGolden rewrites the golden prompts from the current code. Run it only
// when a prompt change is intended, and read the diff of testdata before
// committing: go test ./internal/agent -run TestInstructionsGolden -update
var updateGolden = flag.Bool("update", false, "rewrite the golden prompt files in testdata")

// The instructions are part of the model's cached prefix, so any change to
// them, even a stray space, is a behaviour change. These golden files pin the
// exact bytes for a spread of profiles.
func TestInstructionsGolden(t *testing.T) {
	paris := time.FixedZone("Europe/Paris", 3600)
	mems := []*store.Memory{
		{ID: 3, Content: "Lives in Montréal, near Jean-Talon market"},
		{ID: 7, Content: "Vegetarian; partner is called Sam"},
	}
	for _, c := range []struct {
		name  string
		user  *store.User
		mems  []*store.Memory
		shell bool
		loc   *time.Location
	}{
		{
			name:  "full_shell",
			user:  &store.User{Username: "alex", DisplayName: "Alexander", Email: "alex@example.com", About: "  Product designer. Prefers trains to planes.  "},
			mems:  mems,
			shell: true,
			loc:   paris,
		},
		{
			name:  "full_noshell",
			user:  &store.User{Username: "alex", DisplayName: "Alexander", Email: "alex@example.com", About: "Product designer."},
			mems:  mems,
			shell: false,
			loc:   paris,
		},
		{
			name:  "bare_shell",
			user:  &store.User{Username: "sam"},
			shell: true,
			loc:   time.UTC,
		},
		{
			name:  "bare_noshell",
			user:  &store.User{Username: "sam", About: "   "},
			shell: false,
			loc:   time.UTC,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := buildInstructions(c.user, c.mems, c.shell, c.loc)
			path := filepath.Join("testdata", "prompt_"+c.name+".golden")
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
				t.Fatalf("instructions for %s differ from %s; if the change is intended, run with -update and review the diff", c.name, path)
			}
		})
	}
}
