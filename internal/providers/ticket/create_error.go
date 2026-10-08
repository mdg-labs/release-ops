package ticket

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// httpStatusError is a non-2xx answer from a ticket system. It carries the status code
// so a create failure can be classified; the message is the one callers have always seen.
type httpStatusError struct {
	code   int
	detail string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("unexpected status %d: %s", e.code, strings.TrimSpace(e.detail))
}

// newHTTPStatusError builds the error for a non-2xx answer; detail is the (already
// size-limited) response body, never the request.
func newHTTPStatusError(code int, detail string) error {
	return &httpStatusError{code: code, detail: detail}
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
