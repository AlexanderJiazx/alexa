// Package contract reads the frontend's API types (web/src/api/*.ts), so the
// Go tests can check that the server sends, under the same names, every
// field the client reads. The TypeScript files are the contract both sides
// match (they say so at the top); reading them here, instead of copying the
// field lists into the tests, means a field renamed or added on one side
// fails a test until the other side follows.
//
// It understands only what those files use for wire types: named object
// types (type Name = { … }), whose top-level fields it lists with each
// field's type as written, and the object members of a union (Members).
// Intersections and generics aren't object types; the tests that need one
// of those check it field by field.
package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// Field is one top-level field of a TypeScript object type. Type is its
// type as written, such as "string", "Card[]" or "{ x: number }[]".
type Field struct {
	Name     string
	Optional bool
	Type     string
}

// Types maps each named object type in the files to its fields.
type Types map[string][]Field

// Names returns the type's field names, sorted.
func (ts Types) Names(typ string) []string {
	var out []string
	for _, f := range ts[typ] {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

// APIDir is web/src/api, found from this file's place in the repository so
// it works from any package's test.
func APIDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "web", "src", "api")
}

// Load reads every .ts file in dir (tests excluded).
func Load(dir string) (Types, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.ts"))
	if err != nil {
		return nil, err
	}
	out := Types{}
	for _, f := range files {
		if strings.HasSuffix(f, ".test.ts") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for name, fields := range Parse(string(b)) {
			out[name] = fields
		}
	}
	return out, nil
}

// typeStart finds "type Name = {", exported or not: blocks.ts keeps the
// fields every block shares in an unexported Base.
var typeStart = regexp.MustCompile(`(?m)^(?:export )?type ([A-Z][A-Za-z0-9]*)\s*=\s*\{`)

// Parse lists the object types declared in one file's source.
func Parse(src string) Types {
	src = stripComments(src)
	out := Types{}
	for _, m := range typeStart.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		open := m[1] - 1 // the type's {
		end := matching(src, open)
		if end < 0 {
			continue
		}
		// "= { … } & X" or "= { … } | X" isn't a plain object type.
		if rest := strings.TrimLeft(src[end+1:], " \t"); strings.HasPrefix(rest, "&") || strings.HasPrefix(rest, "|") {
			continue
		}
		out[name] = Fields(src[open+1 : end])
	}
	return out
}

var declStart = regexp.MustCompile(`(?m)^(?:export )?type ([A-Z][A-Za-z0-9]*)\b[^=]*=`)

// Members returns the object members of the union type name in src, keyed
// by the string literal each gives its tag field: Members(src, "Action",
// "do") maps "reply" to { do: 'reply'; label: string; text: string }. A
// member's fields are its own, without anything the union is joined to
// with & (Block's Base).
func Members(src, name, tag string) map[string][]Field {
	src = stripComments(src)
	from, to := -1, len(src)
	for _, m := range declStart.FindAllStringSubmatchIndex(src, -1) {
		if from >= 0 {
			to = m[0]
			break
		}
		if src[m[2]:m[3]] == name {
			from = m[1]
		}
	}
	if from < 0 {
		return nil
	}
	lit := regexp.MustCompile(`^'([^']*)'$`)
	out := map[string][]Field{}
	for i := from; i < to; i++ {
		switch src[i] {
		case '\'', '"', '`':
			i = skipString(src, i)
		case '{':
			end := matching(src, i)
			if end < 0 || end > to {
				return out
			}
			fs := Fields(src[i+1 : end])
			for _, f := range fs {
				if m := lit.FindStringSubmatch(f.Type); f.Name == tag && m != nil {
					out[m[1]] = fs
				}
			}
			i = end
		}
	}
	return out
}

var fieldRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(\??)\s*:`)

// Fields lists the top-level fields of an object type's body (what is
// between its braces): names at depth 0 that start the body or follow a ;
// , or line break, each with the type written after it.
func Fields(body string) []Field {
	var out []Field
	depth, start, typeAt := 0, true, -1
	end := func(i int) { // the field being read, if any, ends before i
		if typeAt >= 0 {
			out[len(out)-1].Type = strings.TrimSpace(body[typeAt:i])
			typeAt = -1
		}
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c == '{' || c == '(' || c == '[' || c == '<':
			depth++
			start = false
		case c == '}' || c == ')' || c == ']' || c == '>':
			if c == '>' && i > 0 && body[i-1] == '=' { // => in a function type
				continue
			}
			depth--
			start = false
		case c == ';' || c == ',' || c == '\n':
			if depth == 0 {
				end(i)
				start = true
			}
		case c == ' ' || c == '\t' || c == '\r':
		case c == '\'' || c == '"' || c == '`':
			i = skipString(body, i)
			start = false
		default:
			if depth == 0 && start {
				if m := fieldRe.FindStringSubmatch(body[i:]); m != nil {
					out = append(out, Field{Name: m[1], Optional: m[2] == "?"})
					i += len(m[0]) - 1
					typeAt = i + 1
				}
			}
			start = false
		}
	}
	end(len(body))
	return out
}

// matching returns the index of the brace that closes the one at open, or
// -1.
func matching(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		case '\'', '"', '`':
			i = skipString(src, i)
		}
	}
	return -1
}

// skipString returns the index of the quote that ends the string starting
// at i.
func skipString(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case q:
			return j
		}
	}
	return len(s) - 1
}

// stripComments blanks out // and /* */ comments, leaving strings alone
// (a URL in a string keeps its //), and keeps line breaks, which separate
// fields.
func stripComments(src string) string {
	b := []byte(src)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\'' || b[i] == '"' || b[i] == '`':
			i = skipString(src, i)
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			for ; i < len(b) && !(b[i] == '*' && i+1 < len(b) && b[i+1] == '/'); i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
			if i+1 < len(b) {
				b[i], b[i+1] = ' ', ' '
				i++
			}
		}
	}
	return string(b)
}
