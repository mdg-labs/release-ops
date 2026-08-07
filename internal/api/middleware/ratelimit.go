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
	LoginLimit            = 10
	LoginWindow           = time.Minute
	ForgotPasswordLimit   = 5
	ForgotPasswordWindow  = time.Hour
	AcceptInvitationLimit = 10
	AcceptInvitationWindow = time.Hour
	ResetPasswordLimit    = 10
	ResetPasswordWindow   = time.Hour
	ConfirmEmailChangeLimit = 10
	ConfirmEmailChangeWindow = time.Hour
)

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// RateLimiter enforces per-IP sliding-window limits for public auth endpoints.
type RateLimiter struct {
	mu    sync.Mutex
	clock clock
	store map[string][]time.Time
}

// NewRateLimiter returns an in-memory rate limiter for a single container.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		clock: realClock{},
		store: make(map[string][]time.Time),
	}
}

// NewRateLimiterForTest constructs a limiter with a fixed clock for unit tests.
func NewRateLimiterForTest(clock interface{ Now() time.Time }) *RateLimiter {
	return &RateLimiter{
		clock: clock,
		store: make(map[string][]time.Time),
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

	timestamps := rl.store[key]
	active := timestamps[:0]
	for _, ts := range timestamps {
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
		rl.store[key] = active
		return retryAfter, false
	}

	rl.store[key] = append(active, now)
	return 0, true
}

// clientIP returns the first hop from X-Forwarded-For when present, else RemoteAddr host.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
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
