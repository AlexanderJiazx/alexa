package agent

import (
	"regexp"
	"strings"
)

// citationRe matches raw search citation markers such as 【123†L4-L9】 that
// the model sometimes copies from web results.
var citationRe = regexp.MustCompile(`\s?【[^】]*】`)

func cleanText(s string) string { return strings.TrimSpace(citationRe.ReplaceAllString(s, "")) }

func trim(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// clip shortens text to at most n characters, ending at a sentence, clause
// or word boundary.
func clip(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= n {
		return text
	}
	head := string([]rune(text)[:n])
	for _, sep := range []string{". ", "! ", "? "} {
		if i := strings.LastIndex(head, sep); i >= n/3 {
			return head[:i+1]
		}
	}
	for _, sep := range []string{"; ", " — ", ", "} {
		if i := strings.LastIndex(head, sep); i >= n/3 {
			return head[:i] + "…"
		}
	}
	if i := strings.LastIndex(head, " "); i >= n/3 {
		return head[:i] + "…"
	}
	return trim(text, n)
}

func plain(md string) string {
	r := strings.NewReplacer("**", "", "__", "", "#", "", "`", "", "*", "")
	return strings.Join(strings.Fields(r.Replace(md)), " ")
}
