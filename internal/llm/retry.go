package llm

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Every request goes through one retry layer. A busy or broken service gets
// a few more tries spread over about two minutes, with full jitter so
// several users' runs don't retry in step, and as long as the service asks
// for when it says how long; a request the model refused gets none. The
// caller hears about each retry (Request.OnRetry), so the user sees
// "Retrying (2 of 4)" instead of a spinner that never changes.
const (
	defaultAttempts      = 4
	defaultRetryBudget   = 2 * time.Minute
	defaultBaseDelay     = 5 * time.Second
	defaultMaxDelay      = 40 * time.Second
	defaultHeaderTimeout = 60 * time.Second
	defaultIdleTimeout   = 90 * time.Second
	defaultCreateTimeout = 5 * time.Minute
	maxHint              = 24 * time.Hour
)

// Retry is a retry about to happen, for the caller to show.
type Retry struct {
	Attempt int       // the attempt about to be made: 2 for the first retry
	Of      int       // how many attempts there can be in all
	Reason  string    // why the last attempt failed (a Reason constant)
	At      time.Time // when it will be sent
	Err     error     // what the last attempt got
}

func (c *Client) attempts() int {
	if c.Attempts > 0 {
		return c.Attempts
	}
	return defaultAttempts
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// call runs attempt until it works, fails in a way that trying again can't
// help, or the attempts or the time run out. The time is counted from the
// first failure: a long answer that streamed for minutes before it broke
// off still gets its retries. Its error is an *Error, or the context's
// error when the caller gave up.
func (c *Client) call(ctx context.Context, onRetry func(Retry), attempt func(ctx context.Context) error) error {
	var start time.Time
	of := c.attempts()
	for n := 1; ; n++ {
		err := attempt(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if start.IsZero() {
			start = time.Now()
		}
		reason, retry := Classify(err)
		fail := &Error{Reason: reason, Retryable: retry, Attempts: n, Err: err}
		wait, hinted := c.backoff(n, reason, err)
		if retry && hinted {
			// Whether or not it's waited out here, the screen can count
			// down to it.
			fail.RetryAt = time.Now().Add(wait)
		}
		if !retry || n >= of || time.Since(start)+wait > orDefault(c.RetryBudget, defaultRetryBudget) {
			return fail
		}
		slog.Warn("model call failed, retrying", "reason", reason, "err", err, "attempt", n+1, "of", of, "in", wait.Round(time.Millisecond))
		if onRetry != nil {
			onRetry(Retry{Attempt: n + 1, Of: of, Reason: reason, At: time.Now().Add(wait), Err: err})
		}
		if err := c.wait(ctx, wait); err != nil {
			return err
		}
	}
}

// backoff is how long to wait before retry n: what the service asked for,
// if it said (hinted), or else full jitter over a doubling cap. A stall has
// already cost a minute or more, so its retry goes out almost at once
// (1-3s with the default base delay).
func (c *Client) backoff(n int, reason string, err error) (wait time.Duration, hinted bool) {
	if h := hintOf(err); h > 0 {
		return h + jitter(250*time.Millisecond), true
	}
	base := orDefault(c.BaseDelay, defaultBaseDelay)
	if reason == ReasonStalled {
		return base/5 + jitter(base*2/5), false
	}
	limit := base << (n - 1)
	limit = min(limit, orDefault(c.MaxDelay, defaultMaxDelay))
	return time.Second + jitter(limit), false
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return rand.N(d)
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func hintOf(err error) time.Duration {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return 0
	}
	return apiErr.RetryAfter
}

// retryHint reads how long the service asked to wait: Retry-After (seconds
// or a date), retry-after-ms, a reset hint in the message ("try again in
// 7s", "resets in 4hr 5min"), or for a 429 the rate-limit reset headers.
// Those say when a whole bucket fills up again, not when to come back, so
// a 5xx that happens to carry them isn't made to wait for them.
func retryHint(status int, h http.Header, msg string, now time.Time) time.Duration {
	var d time.Duration
	if v := strings.TrimSpace(h.Get("Retry-After-Ms")); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms > 0 {
			d = time.Duration(ms * float64(time.Millisecond))
		}
	}
	if v := strings.TrimSpace(h.Get("Retry-After")); d == 0 && v != "" {
		if s, err := strconv.ParseFloat(v, 64); err == nil {
			d = time.Duration(s * float64(time.Second))
		} else if t, err := http.ParseTime(v); err == nil {
			d = t.Sub(now)
		}
	}
	if d == 0 {
		d = messageHint(msg) // it names the limit that was hit
	}
	if d == 0 && status == http.StatusTooManyRequests {
		d = resetHint(h, now)
	}
	if d <= 0 {
		return 0
	}
	return min(d, maxHint)
}

// resetHint reads a 429's rate-limit reset headers: when the limit that ran
// out (its remaining count is 0) is back, or the longest of them when they
// don't say which one did. A per-minute request limit isn't waited out for
// the six minutes the token bucket takes to refill.
func resetHint(h http.Header, now time.Time) time.Duration {
	var spent, longest time.Duration
	for _, k := range []string{"Requests", "Tokens"} {
		d := resetValue(h.Get("X-Ratelimit-Reset-"+k), now)
		longest = max(longest, d)
		if strings.TrimSpace(h.Get("X-Ratelimit-Remaining-"+k)) == "0" {
			spent = max(spent, d)
		}
	}
	if spent > 0 {
		return spent
	}
	return max(longest, resetValue(h.Get("X-Ratelimit-Reset"), now))
}

// resetValue reads a rate-limit reset header: a duration ("6m0s", "20ms"),
// a number of seconds, or a unix time.
func resetValue(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	s, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	if s > 1e9 { // a unix time
		return time.Unix(int64(s), 0).Sub(now)
	}
	return time.Duration(s * float64(time.Second))
}

var (
	hintRe = regexp.MustCompile(`(?i)(?:try again|retry|resets?|available again)(?: after| in)\s+((?:\d+(?:\.\d+)?\s*(?:ms|milliseconds?|s|secs?|seconds?|m|mins?|minutes?|h|hrs?|hours?)\b[\s,and]*)+)`)
	partRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(ms|milliseconds?|s|secs?|seconds?|m|mins?|minutes?|h|hrs?|hours?)\b`)
)

// messageHint reads a reset hint from an error message.
func messageHint(msg string) time.Duration {
	m := hintRe.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	var d time.Duration
	for _, p := range partRe.FindAllStringSubmatch(m[1], -1) {
		n, _ := strconv.ParseFloat(p[1], 64)
		unit := time.Second
		switch u := strings.ToLower(p[2]); {
		case u == "ms" || strings.HasPrefix(u, "milli"):
			unit = time.Millisecond
		case u == "m" || strings.HasPrefix(u, "min"):
			unit = time.Minute
		case strings.HasPrefix(u, "h"):
			unit = time.Hour
		}
		d += time.Duration(n * float64(unit))
	}
	return d
}
