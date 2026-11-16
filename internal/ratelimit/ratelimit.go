// Package ratelimit reads the rate-limit answers of GitHub and of the ticket systems
// (Retry-After and X-RateLimit-*) and keeps the one rate-limit budget of each GitHub
// installation (specs.html § 2.8): every consumer of an installation token, that is Renovate
// runs, the reconcile, failed-delivery redelivery and the role checks, records what the
// responses say and defers its work while the budget is exhausted.
package ratelimit

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBlock is how long work is held back after an answer that says it is rate limited
	// but not for how long. GitHub documents one minute for a secondary limit without
	// Retry-After.
	DefaultBlock = time.Minute
	// MaxUpstreamDelay caps a delay or a reset time taken from a response, so a wrong or hostile
	// header cannot hold work back for longer than this.
	MaxUpstreamDelay = 24 * time.Hour
)

// Consumers of the shared budget, the values of the consumer label of the deferral counter.
const (
	ConsumerRenovateRun = "renovate_run"
	ConsumerReconcile   = "reconcile"
	ConsumerRedelivery  = "redelivery"
	ConsumerRoleCheck   = "role_check"
)

// ErrRateLimited is matched, with errors.Is, by every error that says work was deferred or
// refused because of a rate limit.
var ErrRateLimited = errors.New("rate limited")

// LimitedError is the answer of a rate-limited call, or of the budget that refused to make
// one. It carries nothing of the response.
type LimitedError struct {
	// Until is the earliest time a retry may succeed.
	Until time.Time
	// After is the delay from when the error was made until Until.
	After time.Duration
}

func (e *LimitedError) Error() string {
	return fmt.Sprintf("rate limited: retry after %s", e.After.Round(time.Second))
}

// Is reports ErrRateLimited.
func (e *LimitedError) Is(target error) bool { return target == ErrRateLimited }

// NewLimitedError returns the error for a limit that lifts at until, as seen at now.
func NewLimitedError(until, now time.Time) *LimitedError {
	return &LimitedError{Until: until, After: max(until.Sub(now), 0)}
}

// DelayOf returns the delay an error asks for when it carries a rate limit.
func DelayOf(err error) (time.Duration, bool) {
	var limited *LimitedError
	if errors.As(err, &limited) {
		return limited.After, true
	}
	return 0, false
}

// Signal is what one response says about the rate limit.
type Signal struct {
	// Limited is true for an answer that refuses the call because of a rate limit: 429, or
	// 403 with a Retry-After or an exhausted X-RateLimit-Remaining.
	Limited bool
	// Remaining and Limit are X-RateLimit-Remaining and X-RateLimit-Limit when present.
	Remaining, Limit       int64
	HasRemaining, HasLimit bool
	// ResetAt is X-RateLimit-Reset, at most MaxUpstreamDelay after the time it was read, and
	// zero when absent.
	ResetAt time.Time
	// RetryAfter is Retry-After as a delay, zero when absent.
	RetryAfter time.Duration
}

// Empty reports that the response said nothing about a rate limit.
func (s Signal) Empty() bool {
	return !s.Limited && !s.HasRemaining && !s.HasLimit && s.ResetAt.IsZero() && s.RetryAfter == 0
}

// Parse reads the rate-limit headers of a response. Retry-After is a number of seconds or an
// HTTP date; X-RateLimit-Reset is the Unix time in seconds. A header that is not readable is
// ignored, and a reset time further away than MaxUpstreamDelay is cut to it.
func Parse(status int, h http.Header, now time.Time) Signal {
	var s Signal
	if v, ok := parseInt(h.Get("X-RateLimit-Remaining")); ok {
		s.Remaining, s.HasRemaining = v, true
	}
	if v, ok := parseInt(h.Get("X-RateLimit-Limit")); ok {
		s.Limit, s.HasLimit = v, true
	}
	if v, ok := parseInt(h.Get("X-RateLimit-Reset")); ok && v > 0 {
		if limit := now.Add(MaxUpstreamDelay); v >= limit.Unix() {
			s.ResetAt = limit.UTC()
		} else {
			s.ResetAt = time.Unix(v, 0).UTC()
		}
	}
	s.RetryAfter = parseRetryAfter(h.Get("Retry-After"), now)
	s.Limited = status == http.StatusTooManyRequests ||
		(status == http.StatusForbidden && (s.RetryAfter > 0 || (s.HasRemaining && s.Remaining == 0)))
	return s
}

func parseInt(v string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n, err == nil && n >= 0
}

func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return min(time.Duration(secs)*time.Second, MaxUpstreamDelay)
	}
	if at, err := http.ParseTime(v); err == nil {
		return min(max(at.Sub(now), 0), MaxUpstreamDelay)
	}
	return 0
}

// Until is the earliest time work may resume after a Limited signal: Retry-After first, then
// the reset time when no request remains, otherwise DefaultBlock. It is the zero time for a
// signal that is not Limited.
func (s Signal) Until(now time.Time) time.Time {
	if !s.Limited {
		return time.Time{}
	}
	switch {
	case s.RetryAfter > 0:
		return now.Add(s.RetryAfter)
	case s.HasRemaining && s.Remaining == 0 && s.ResetAt.After(now):
		return minTime(s.ResetAt, now.Add(MaxUpstreamDelay))
	}
	return now.Add(DefaultBlock)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Jittered spreads d upward by up to the jitter fraction, rnd in [0, 1): the result is never
// earlier than d, so a delay an upstream asked for is never undercut.
func Jittered(d time.Duration, jitter, rnd float64) time.Duration {
	return d + time.Duration(float64(d)*jitter*rnd)
}
