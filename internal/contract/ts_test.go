package contract

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	src := `
// A comment with a 'quote' and a field: like this
export type One = { a: string; b?: number /* c: no */; d: { e: string; f?: string }[] }
export type Two = {
  url: string // https://example.com/x: not a field
  kind: 'a' | 'b'
  fn?: (x: number) => void
  rec: Record<string, string | number>
}
export type Both = { x: string } & Other
type Hidden = { y: string }
export type Union = 'a' | 'b'
`
	ts := Parse(src)
	if got, want := ts.Names("One"), []string{"a", "b", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("One = %v, want %v", got, want)
	}
	if got, want := ts.Names("Two"), []string{"fn", "kind", "rec", "url"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Two = %v, want %v", got, want)
	}
	for _, skipped := range []string{"Both", "Union"} {
		if _, ok := ts[skipped]; ok {
			t.Errorf("%s isn't a plain object type", skipped)
		}
	}
	if got := ts.Names("Hidden"); !reflect.DeepEqual(got, []string{"y"}) {
		t.Errorf("Hidden = %v: unexported types are read too", got)
	}
	if f := ts["One"][1]; f.Name != "b" || !f.Optional || f.Type != "number" {
		t.Errorf("b = %+v, want an optional number", f)
	}
	if f := ts["One"][2]; f.Type != "{ e: string; f?: string }[]" {
		t.Errorf("d's type = %q", f.Type)
	}
	if f := ts["Two"][1]; f.Type != "'a' | 'b'" {
		t.Errorf("kind's type = %q", f.Type)
	}
}

func TestMembers(t *testing.T) {
	src := `
export type Action =
  | { do: 'reply'; label: string; text: string } // a comment
  | { do: 'open'; label?: string; url: string }
type Base = { id?: string }
export type Block = Base &
  (
    | { type: 'cards'; items: { title: string }[] }
    | {
        type: 'table'
        rows: string[][]
      }
  )
export type After = { do: 'nope' }
`
	acts := Members(src, "Action", "do")
	if len(acts) != 2 || len(acts["reply"]) != 3 || acts["open"][2].Name != "url" {
		t.Errorf("Action members = %+v", acts)
	}
	blocks := Members(src, "Block", "type")
	if len(blocks) != 2 || blocks["cards"][1].Type != "{ title: string }[]" || blocks["table"][1].Name != "rows" {
		t.Errorf("Block members = %+v", blocks)
	}
}

// The real files parse, and the types the contract tests rely on are found.
func TestLoadAPI(t *testing.T) {
	ts, err := Load(APIDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"SessionState", "Needs", "Entry", "EntryData", "StepData", "Failure", "Card", "FormField", "Changed"} {
		if len(ts[name]) == 0 {
			t.Errorf("no fields read for %s", name)
		}
	}
}
