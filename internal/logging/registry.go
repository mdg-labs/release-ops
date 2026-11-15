// Package logging is the one place the app's and the worker's own logs pass through: an
// slog handler that removes secret values from every message and attribute before the line is
// written. The secrets are registered where they are loaded or minted, so no caller has to
// remember to mask one; shapes of tokens and keys are removed with the redactor the Renovate
// job excerpts use.
package logging

import (
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/renovate/redact"
)

const (
	// maxTemporary and maxUnauthenticated bound the entries that expire, each in its own
	// budget. A budget past its bound drops the entries registered first.
	maxTemporary       = 8192
	maxUnauthenticated = 1024
	// expiryGrace keeps an entry a little past the lifetime of its secret, so a line written
	// as the secret lapses is still covered.
	expiryGrace = 10 * time.Minute
	// dropWarnEvery spaces the warnings about live entries that a full budget dropped.
	dropWarnEvery = time.Minute
)

// Registry is the set of secret values to remove from logs. Secrets that live as long as the
// process (configuration, the App's key) are registered without an end; the ones minted at run
// time (installation tokens, sessions, OAuth codes) are registered for their lifetime and
// dropped when it ends, so the registry does not grow with the number of tokens minted.
//
// The entries with an end are held in two budgets. Values that a request without a session can
// make the server mint (the token of a session that has no user, a sign-in attempt's state and
// verifier, a password-reset token) go in the unauthenticated budget, so traffic anyone can
// send fills that budget and nothing else: it can age out another unauthenticated entry, never
// an integration credential, a signed-in user's session or an installation token. The other
// budget holds what only the system or a signed-in user mints and is bounded by its size alone.
// When a budget is full the entries registered first are dropped, so a secret registered now is
// never the one dropped, and the drop is counted and logged as a warning, never silently.
type Registry struct {
	now func() time.Time

	mu        sync.Mutex
	permanent map[string]struct{}
	temporary budget
	unauth    budget
	redactor  *redact.Redactor
	// dirty is set when the secrets changed and the redactor must be rebuilt; nextExpiry is
	// the earliest end of an entry, after which the redactor is rebuilt as well.
	dirty      bool
	nextExpiry time.Time
	// dropped counts live entries a full budget dropped; warned is when that was last logged.
	dropped    uint64
	warnedAt   time.Time
	warnedDrop uint64
}

// budget is a bounded set of secrets that end.
type budget struct {
	max     int
	entries map[string]entry
}

// entry is when a secret ends and when it was registered.
type entry struct {
	end, at time.Time
}

// NewRegistry returns an empty registry that reads the time from now (time.Now when nil).
func NewRegistry(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{
		now:       now,
		permanent: map[string]struct{}{},
		temporary: budget{max: maxTemporary, entries: map[string]entry{}},
		unauth:    budget{max: maxUnauthenticated, entries: map[string]entry{}},
		redactor:  redact.New(),
	}
}

// Add registers secrets that stay secret for the life of the process.
func (r *Registry) Add(secrets ...string) {
	secrets = withLines(secrets)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if _, ok := r.permanent[s]; !ok {
			r.permanent[s] = struct{}{}
			r.dirty = true
		}
	}
}

// AddFor registers secrets for ttl from now. A secret registered again has its end moved
// later, but only once a quarter of ttl has passed, so a secret that is registered on every
// request does not rebuild the redactor on every request.
func (r *Registry) AddFor(ttl time.Duration, secrets ...string) {
	r.addFor(&r.temporary, ttl, secrets)
}

// AddUnauthenticatedFor is AddFor for a value that a request without a session can make the
// server mint. It has its own budget, so such traffic cannot push out any other entry.
func (r *Registry) AddUnauthenticatedFor(ttl time.Duration, secrets ...string) {
	r.addFor(&r.unauth, ttl, secrets)
}

func (r *Registry) addFor(b *budget, ttl time.Duration, secrets []string) {
	secrets = withLines(secrets)
	r.mu.Lock()
	now := r.now()
	end := now.Add(ttl + expiryGrace)
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if _, ok := r.permanent[s]; ok {
			continue
		}
		if old, ok := b.entries[s]; ok && end.Sub(old.end) <= ttl/4 {
			continue
		}
		b.entries[s] = entry{end: end, at: now}
		r.dirty = true
	}
	if len(b.entries) > b.max {
		r.shrink(b, now)
	}
	warn, total := r.dropWarning(now)
	r.mu.Unlock()
	if warn {
		slog.Warn("log redaction registry full: live secrets were dropped", "dropped_total", total)
	}
}

// dropWarning reports whether the drops since the last warning should be logged now.
func (r *Registry) dropWarning(now time.Time) (bool, uint64) {
	if r.dropped == r.warnedDrop || now.Sub(r.warnedAt) < dropWarnEvery {
		return false, 0
	}
	r.warnedAt, r.warnedDrop = now, r.dropped
	return true, r.dropped
}

// Dropped reports how many live entries a full budget has dropped.
func (r *Registry) Dropped() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// minLineLen is the shortest line of a multi-line secret that is registered on its own.
const minLineLen = 16

// withLines adds each long line of a multi-line secret, such as the body of a PEM key, so a
// line logged alone is removed as well as the whole.
func withLines(secrets []string) []string {
	out := slices.Clone(secrets)
	for _, s := range secrets {
		if !strings.Contains(s, "\n") {
			continue
		}
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if len(line) >= minLineLen && !strings.HasPrefix(line, "-----") {
				out = append(out, line)
			}
		}
	}
	return out
}

// shrink drops the entries of b that have ended and, if it is still full, the ones registered
// first; the latter are live and are counted. The newest entry is never the one dropped, so a
// secret that is registered is covered at least until others have filled the budget after it.
func (r *Registry) shrink(b *budget, now time.Time) {
	for s, e := range b.entries {
		if !e.end.After(now) {
			delete(b.entries, s)
		}
	}
	if len(b.entries) <= b.max {
		return
	}
	type named struct {
		secret string
		at     time.Time
	}
	all := make([]named, 0, len(b.entries))
	for s, e := range b.entries {
		all = append(all, named{s, e.at})
	}
	slices.SortFunc(all, func(a, b named) int { return a.at.Compare(b.at) })
	for _, e := range all[:len(all)-b.max] {
		delete(b.entries, e.secret)
		r.dropped++
	}
}

// redactorNow returns a redactor for the secrets registered now, rebuilt only when they
// changed or an entry ended.
func (r *Registry) redactorNow() *redact.Redactor {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if !r.dirty && (r.nextExpiry.IsZero() || now.Before(r.nextExpiry)) {
		return r.redactor
	}
	secrets := make([]string, 0, len(r.permanent)+len(r.temporary.entries)+len(r.unauth.entries))
	for s := range r.permanent {
		secrets = append(secrets, s)
	}
	r.nextExpiry = time.Time{}
	for _, b := range []*budget{&r.temporary, &r.unauth} {
		for s, e := range b.entries {
			if !e.end.After(now) {
				delete(b.entries, s)
				continue
			}
			secrets = append(secrets, s)
			if r.nextExpiry.IsZero() || e.end.Before(r.nextExpiry) {
				r.nextExpiry = e.end
			}
		}
	}
	r.redactor = redact.New(secrets...)
	r.dirty = false
	return r.redactor
}

// Len reports how many secrets are registered.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.permanent) + len(r.temporary.entries) + len(r.unauth.entries)
}

// Default is the registry the process's handler reads. Packages register into it where a
// secret is loaded or minted.
var Default = NewRegistry(nil)

// Register adds secrets that stay secret for the life of the process to the default registry.
func Register(secrets ...string) { Default.Add(secrets...) }

// RegisterFor adds secrets to the default registry for ttl, the lifetime of a minted token.
func RegisterFor(ttl time.Duration, secrets ...string) { Default.AddFor(ttl, secrets...) }

// RegisterUnauthenticatedFor adds a value that a request without a session can make the server
// mint to the default registry for ttl, in the budget that other traffic cannot push out of.
func RegisterUnauthenticatedFor(ttl time.Duration, secrets ...string) {
	Default.AddUnauthenticatedFor(ttl, secrets...)
}

// RegisterJSONFor adds a JSON document that holds credentials, whole and each string value in
// it, to the default registry for ttl. A payload that is not JSON is registered whole.
func RegisterJSONFor(ttl time.Duration, payload []byte) {
	values := []string{string(payload)}
	var doc any
	if json.Unmarshal(payload, &doc) == nil {
		values = appendStrings(values, doc)
	}
	Default.AddFor(ttl, values...)
}

func appendStrings(values []string, doc any) []string {
	switch v := doc.(type) {
	case string:
		values = append(values, v)
	case []any:
		for _, e := range v {
			values = appendStrings(values, e)
		}
	case map[string]any:
		for _, e := range v {
			values = appendStrings(values, e)
		}
	}
	return values
}
