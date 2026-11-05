package llm

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

// Why a model call failed, as the agent and the user's screen tell them
// apart (SYNTHESIS Appendix B.4): a busy service is waited out, a request
// the model can't take never is.
const (
	ReasonRateLimited     = "rate_limited"      // 429: too many requests, or the quota ran out
	ReasonUnavailable     = "model_unavailable" // 5xx, a dropped connection, a broken stream
	ReasonStalled         = "stalled"           // no headers or no events for too long
	ReasonContextOverflow = "context_overflow"  // the prompt is too long for the model
	ReasonInvalidInput    = "invalid_input"     // the model refused the request (a bad image, a filter)
	ReasonAuth            = "auth"              // the key was refused
)

type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	// RetryAfter is how long the service asked to wait, from its headers
	// or its message; zero if it didn't say.
	RetryAfter time.Duration `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("model API error %d (%s): %s", e.Status, e.Type, e.Message)
}

// Error is how a model call failed for good: after its retries, or at once
// for a request that would fail the same way again. It wraps the last
// error (an *APIError, a network error or a stall), so errors.As still
// finds the API's answer.
type Error struct {
	Reason    string    // one of the Reason constants
	Retryable bool      // trying again later can work: the service was busy or broken, not the request
	Attempts  int       // how many requests were made
	RetryAt   time.Time // when the service said to come back, if that was too long to wait
	Err       error
}

func (e *Error) Error() string {
	return fmt.Sprintf("model call failed (%s) after %d attempt(s): %v", e.Reason, e.Attempts, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// errStalled is a request that got no response headers, or a stream that
// went quiet, for too long. The connection may be half-open: nothing else
// would notice for minutes.
var errStalled = errors.New("the model stopped responding")

// errUnfinished is a stream that ended without saying how the response
// ended: the connection dropped.
var errUnfinished = errors.New("stream ended without completion")

// Classify says why a model call failed (one of the Reason constants) and
// whether the same request is worth sending again.
func Classify(err error) (reason string, retry bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason, e.Retryable
	}
	if errors.Is(err, errStalled) {
		return ReasonStalled, true
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		// A dropped connection, a broken stream, a body that wasn't JSON:
		// the service or the way to it, not the request.
		return ReasonUnavailable, true
	}
	if overflow(apiErr.Code + " " + apiErr.Message) {
		return ReasonContextOverflow, false
	}
	switch s := apiErr.Status; {
	case s == 429:
		return ReasonRateLimited, apiErr.Code != "insufficient_quota"
	case s == 401 || s == 403:
		return ReasonAuth, false
	case s == 413:
		return ReasonContextOverflow, false
	case s == 408 || s >= 500:
		return ReasonUnavailable, true
	case s == 404:
		return ReasonUnavailable, false // no such model or endpoint
	}
	return ReasonInvalidInput, false
}

// overflow recognises the API's "prompt too long" errors.
func overflow(s string) bool {
	s = strings.ToLower(s)
	if strings.Contains(s, "too many tokens") || strings.Contains(s, "prompt is too long") || strings.Contains(s, "context_length") {
		return true
	}
	if strings.Contains(s, "deadline") {
		return false // "context deadline exceeded" is a timeout, not a long prompt
	}
	return strings.Contains(s, "context") && (strings.Contains(s, "length") || strings.Contains(s, "window") ||
		strings.Contains(s, "exceed") || strings.Contains(s, "too long") || strings.Contains(s, "maximum"))
}

// streamError is the error a failed response reports in the stream
// (response.failed, or an error event). The HTTP status was 200, so one is
// read from the error code: a refused request must not be retried as if
// the server broke, and an overflow must reach the caller as one.
func streamError(code, typ, msg string) *APIError {
	status := 500
	c := strings.ToLower(code + " " + typ)
	switch {
	case strings.Contains(c, "rate_limit"), strings.Contains(c, "insufficient_quota"):
		status = 429
	case overflow(code + " " + msg):
		status = 400
	case strings.Contains(c, "invalid"), strings.Contains(c, "content_filter"), strings.Contains(c, "unsupported"):
		status = 400
	case strings.Contains(c, "auth"), strings.Contains(c, "permission"):
		status = 403
	}
	if msg == "" {
		msg = "response failed"
	}
	msg = bounded(msg)
	return &APIError{Status: status, Code: code, Type: typ, Message: msg, RetryAfter: messageHint(msg)}
}

var (
	tagRe   = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]*>`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// bounded keeps an error body readable and small: a gateway's HTML error
// page becomes its title or its text, and nothing over 500 characters
// travels on.
func bounded(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") {
		if m := titleRe.FindStringSubmatch(s); m != nil && strings.TrimSpace(m[1]) != "" {
			s = m[1]
		} else {
			s = tagRe.ReplaceAllString(s, " ")
		}
		s = html.UnescapeString(s)
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 500 {
		s = string(r[:499]) + "…"
	}
	return s
}
