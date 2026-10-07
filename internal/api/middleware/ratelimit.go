package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
)

const rateLimitedCode = "RATE_LIMITED"
const rateLimitedMessage = "Too many requests. Try again later."

// Rate limit windows per specs §7.
const (
	LoginLimit               = 10
	LoginWindow              = time.Minute
	ForgotPasswordLimit      = 5
	ForgotPasswordWindow     = time.Hour
	AcceptInvitationLimit    = 10
	AcceptInvitationWindow   = time.Hour
	ResetPasswordLimit       = 10
	ResetPasswordWindow      = time.Hour
	ConfirmEmailChangeLimit  = 10
	ConfirmEmailChangeWindow = time.Hour
)

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// sweepInterval is how often stale buckets are evicted from the in-memory store.
const sweepInterval = time.Minute

// RateLimiter enforces per-IP sliding-window limits for public auth endpoints.
type RateLimiter struct {
	mu        sync.Mutex
	clock     clock
	store     map[string]*rateBucket
	lastSweep time.Time
}

type rateBucket struct {
	hits   []time.Time
	window time.Duration
}

// NewRateLimiter returns an in-memory rate limiter for a single container.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		clock: realClock{},
		store: make(map[string]*rateBucket),
	}
}

// NewRateLimiterForTest constructs a limiter with a fixed clock for unit tests.
func NewRateLimiterForTest(clock interface{ Now() time.Time }) *RateLimiter {
	return &RateLimiter{
		clock: clock,
		store: make(map[string]*rateBucket),
	}
}

// Login limits POST /api/v1/auth/login to 10 requests per minute per IP.
func (rl *RateLimiter) Login(next http.Handler) http.Handler {
	return rl.limit("login", LoginLimit, LoginWindow, next)
}

// ForgotPassword limits POST /api/v1/auth/forgot-password to 5 requests per hour per IP.
func (rl *RateLimiter) ForgotPassword(next http.Handler) http.Handler {
	return rl.limit("forgot-password", ForgotPasswordLimit, ForgotPasswordWindow, next)
}

// AcceptInvitation limits POST /api/v1/auth/accept-invitation to 10 requests per hour per IP.
func (rl *RateLimiter) AcceptInvitation(next http.Handler) http.Handler {
	return rl.limit("accept-invitation", AcceptInvitationLimit, AcceptInvitationWindow, next)
}

// ResetPassword limits POST /api/v1/auth/reset-password to 10 requests per hour per IP.
func (rl *RateLimiter) ResetPassword(next http.Handler) http.Handler {
	return rl.limit("reset-password", ResetPasswordLimit, ResetPasswordWindow, next)
}

// ConfirmEmailChange limits POST /api/v1/auth/confirm-email-change to 10 requests per hour per IP.
func (rl *RateLimiter) ConfirmEmailChange(next http.Handler) http.Handler {
	return rl.limit("confirm-email-change", ConfirmEmailChangeLimit, ConfirmEmailChangeWindow, next)
}

func (rl *RateLimiter) limit(endpoint string, max int, window time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := rl.clock.Now()

		retryAfter, allowed := rl.allow(ip, endpoint, max, window, now)
		if !allowed {
			w.Header().Set("Retry-After", formatRetryAfter(retryAfter))
			auth.WriteError(w, rateLimitedCode, rateLimitedMessage, http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(ip, endpoint string, max int, window time.Duration, now time.Time) (time.Duration, bool) {
	key := ip + "|" + endpoint
	cutoff := now.Add(-window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.sweep(now)

	bucket, ok := rl.store[key]
	if !ok {
		bucket = &rateBucket{window: window}
		rl.store[key] = bucket
	}
	active := bucket.hits[:0]
	for _, ts := range bucket.hits {
		if ts.After(cutoff) {
			active = append(active, ts)
		}
	}

	if len(active) >= max {
		oldest := active[0]
		for _, ts := range active[1:] {
			if ts.Before(oldest) {
				oldest = ts
			}
		}
		retryAfter := oldest.Add(window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		bucket.hits = active
		return retryAfter, false
	}

	bucket.hits = append(active, now)
	return 0, true
}

// sweep drops buckets whose newest hit is outside their window, so the map stays
// bounded by the clients seen within the longest window. Caller holds rl.mu.
func (rl *RateLimiter) sweep(now time.Time) {
	if now.Sub(rl.lastSweep) < sweepInterval {
		return
	}
	rl.lastSweep = now
	for key, bucket := range rl.store {
		if len(bucket.hits) == 0 || !bucket.hits[len(bucket.hits)-1].After(now.Add(-bucket.window)) {
			delete(rl.store, key)
		}
	}
}

// Len returns the number of tracked buckets (tests).
func (rl *RateLimiter) Len() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.store)
}

// clientIP returns the first X-Forwarded-For hop (specs §7.0) when the request comes from
// a loopback peer — the bundled Next.js proxy, which forwards the browser's address —
// and the RemoteAddr host otherwise, so a direct caller cannot pick its own bucket.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if first = strings.TrimSpace(first); first != "" {
				return first
			}
		}
	}
	return host
}

func formatRetryAfter(d time.Duration) string {
	seconds := int(d.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
