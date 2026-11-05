package agent

import (
	"regexp"
	"strings"
)

// Memory goes into Alexa's instructions in every later conversation, so
// what's kept there is checked first. A page that talked her into saving
// "ignore your earlier instructions…" would steer her for good, and a
// password saved there would be sent to the model on every call. The
// patterns aim at unmistakable attack wording and the shapes of secrets,
// not at bossy English: "Prefers you to ask before booking" is fine.

// Each pattern runs on the text with full-width letters folded to ASCII
// (foldWidth), so "ＩＧＮＯＲＥ" counts as "IGNORE".
var (
	memInstructionRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget|override|bypass)\W+(?:\w+\W+){0,3}?(?:previous|prior|above|earlier|preceding|all|any|your|system|original|existing)\W+(?:\w+\W+){0,2}?(?:instructions?|rules|guidelines|prompts?|directives?|guardrails|constraints)\b`),
		regexp.MustCompile(`(?i)\bsystem\s+prompt\b`),
		regexp.MustCompile(`(?i)\byou\s+are\s+now\s+(?:a|an|the|in|my|no\s+longer)\b`),
		regexp.MustCompile(`(?i)\bpretend\s+(?:\w+\s+){0,3}?(?:you\s+are|you're|to\s+be)\b`),
		regexp.MustCompile(`(?i)\b(?:do\s+not|don't|dont|never)\s+(?:\w+\s+){0,3}?(?:tell|inform|notify|alert|show|warn)\s+(?:\w+\s+){0,2}?user\b`),
		regexp.MustCompile(`(?i)\b(?:new|updated|real|actual|hidden|secret)\s+instructions?\b`),
		regexp.MustCompile(`(?i)\bdeveloper\s+mode\b|\bjailbreak`),
		regexp.MustCompile(`(?i)<!--|display\s*:\s*none|<\s*(?:script|iframe|style)\b`),
		// Sending what Alexa knows somewhere.
		regexp.MustCompile(`(?i)\b(?:send|post|upload|forward|transmit|leak|exfiltrate|share|copy)\b[^\n]{0,80}?\b(?:conversation|chat\s+history|history|memory|memories|messages|passwords?|credentials|cookies|tokens?|api\s*keys?|secrets?|files|data)\b[^\n]{0,80}?(?:https?://|\bto\s+\S+@\S+\.\w+)`),
		regexp.MustCompile(`(?i)\b(?:curl|wget)\b[^\n]{0,100}https?://`),
		regexp.MustCompile(`(?i)\b(?:include|output|reveal|print|repeat|leak|share)\s+(?:\w+\s+){0,3}?(?:conversation|chat\s+history|system\s+prompt|your\s+instructions|your\s+memory)\b`),
	}
	memSecretRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:api[\s_-]?key|access[\s_-]?token|auth[\s_-]?token|bearer|token|secret|password|passwd|passcode|pwd|pin)\b\s*(?:is|was|=|:)\s*\S{4,}`),
		regexp.MustCompile(`(?i)\b(?:otp|2fa|mfa|one[\s-]?time\s+(?:pass)?code|verification\s+code|security\s+code|login\s+code|sign[\s-]?in\s+code|cvv|cvc|backup\s+codes?|recovery\s+codes?)\b\D{0,12}\d{3,8}\b`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}|\b(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}|\bxox[abprs]-[A-Za-z0-9-]{10,}|\bAKIA[0-9A-Z]{16}\b|\bAIza[0-9A-Za-z_-]{30,}`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.`),
	}
	memCardRe = regexp.MustCompile(`\d(?:[ -]?\d){12,18}`)
)

// scanMemory says what text looks like if it can't be kept in memory, or
// "" if it can.
func scanMemory(text string) string {
	for _, r := range text {
		if hiddenRune(r) {
			return "hidden characters"
		}
	}
	t := foldWidth(text)
	for _, re := range memInstructionRes {
		if re.MatchString(t) {
			return "an instruction to an AI"
		}
	}
	for _, re := range memSecretRes {
		if re.MatchString(t) {
			return "a password or code"
		}
	}
	for _, m := range memCardRe.FindAllString(t, -1) {
		d := strings.NewReplacer(" ", "", "-", "").Replace(m)
		if len(d) >= 13 && len(d) <= 19 && luhn(d) {
			return "a card number"
		}
	}
	return ""
}

// hiddenRune reports characters that don't show: zero-width spaces and
// joiners, bidi controls, and the tag characters text can be smuggled in.
func hiddenRune(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200F, r == 0x2060, r >= 0x2062 && r <= 0x2064, r == 0xFEFF,
		r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r >= 0xE0000 && r <= 0xE007F:
		return true
	}
	return false
}

// foldWidth turns full-width ASCII look-alikes into ASCII.
func foldWidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r == 0x3000:
			return ' '
		}
		return r
	}, s)
}
