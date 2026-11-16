package ticket

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

// httpStatusError is a non-2xx answer from a ticket system. It carries the status code
// so a create failure can be classified; the message is the one callers have always seen.
type httpStatusError struct {
	code   int
	detail string
	// limited marks an answer that refused the call because of a rate limit, and retryAfter is
	// the delay it asked for (Retry-After, the reset time), at least ratelimit.DefaultBlock.
	limited    bool
	retryAfter time.Duration
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("unexpected status %d: %s", e.code, strings.TrimSpace(e.detail))
}

// newHTTPStatusError builds the error for a non-2xx answer; detail is the (already
// size-limited) response body, never the request. A 429, and a 403 that carries a Retry-After
// or an exhausted X-RateLimit-Remaining, is a rate limit, and so is the 400 with a RATELIMITED
// error code that Linear's GraphQL API answers with; there the delay is the longer of its reset
// time and a Retry-After.
func newHTTPStatusError(resp *http.Response, detail string) error {
	now := time.Now()
	e := &httpStatusError{code: resp.StatusCode, detail: detail}
	sig := ratelimit.Parse(resp.StatusCode, resp.Header, now)
	switch {
	case sig.Limited:
		e.limited, e.retryAfter = true, sig.Until(now).Sub(now)
	case resp.StatusCode == http.StatusBadRequest && strings.Contains(detail, `"RATELIMITED"`):
		e.limited = true
		e.retryAfter = min(max(sig.RetryAfter, linearResetDelay(resp.Header, now)), ratelimit.MaxUpstreamDelay)
	}
	return e
}

// linearResetDelay is the delay until Linear's X-RateLimit-Requests-Reset, a Unix time in
// milliseconds, or ratelimit.DefaultBlock when the header is missing or unreadable.
func linearResetDelay(h http.Header, now time.Time) time.Duration {
	ms, err := strconv.ParseInt(strings.TrimSpace(h.Get("X-RateLimit-Requests-Reset")), 10, 64)
	if err != nil || ms <= 0 {
		return ratelimit.DefaultBlock
	}
	return min(max(time.UnixMilli(ms).Sub(now), ratelimit.DefaultBlock), ratelimit.MaxUpstreamDelay)
}

// RateLimitDelay reports whether err is a ticket system's rate-limit answer, and the delay it
// asked for: work is retried no sooner than that. A rate-limit answer is a 4xx, so on a create it
// is also one CreateDefinitelyNotStored accepts: the ticket system stored nothing, and the retry
// cannot duplicate a ticket.
func RateLimitDelay(err error) (time.Duration, bool) {
	var status *httpStatusError
	if errors.As(err, &status) && status.limited {
		return status.retryAfter, true
	}
	return 0, false
}

// notSentError marks a failure that happened before any byte of the request could
// reach the ticket system: invalid configuration, or a request that could not be built.
type notSentError struct{ err error }

func (e *notSentError) Error() string { return e.err.Error() }
func (e *notSentError) Unwrap() error { return e.err }

// notSent marks err as a failure before the request was sent.
func notSent(err error) error {
	if err == nil {
		return nil
	}
	return &notSentError{err: err}
}

// CreateDefinitelyNotStored reports whether a CreateTicket error proves the ticket
// system stored no ticket, so a later poll may try again. It is deliberately narrow:
//
//   - the ticket system answered with a 4xx status (408 and 429 included: the server did
//     not process that request), or
//   - the request never left: a dial or DNS failure, or a failure before the request was
//     built or sent.
//
// Every other failure is ambiguous and reports false: a 5xx, a timeout, a cancellation
// while the request was in flight, a write or read error or a reset after connecting, a
// 2xx answer whose body is unreadable or carries no ID.
func CreateDefinitelyNotStored(err error) bool {
	if err == nil {
		return false
	}
	var sent *notSentError
	if errors.As(err, &sent) {
		return true
	}
	var status *httpStatusError
	if errors.As(err, &status) {
		return status.code >= http.StatusBadRequest && status.code < http.StatusInternalServerError
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return op.Op == "dial"
	}
	return false
}
