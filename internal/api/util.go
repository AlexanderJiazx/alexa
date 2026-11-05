package api

import (
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func bcryptCompare(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._ -]+`)

func safeName(s string) string {
	s = unsafeChars.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		s = "file"
	}
	return s
}

func shortName(prompt string) string {
	f := strings.Fields(prompt)
	if len(f) > 5 {
		f = f[:5]
	}
	return strings.Join(f, " ")
}
