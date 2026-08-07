package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
)

type fixedClock struct {
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	return c.now
}

func TestRateLimiterEnforcesLoginLimit(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
	rl := apimw.NewRateLimiterForTest(clock)

	handler := rl.Login(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < apimw.LoginLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.1:12345"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected Retry-After header")
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `"code":"RATE_LIMITED"`) {
		t.Fatalf("body = %s, want RATE_LIMITED code", body)
	}
	if !strings.Contains(string(body), "Too many requests") {
		t.Fatalf("body = %s, want rate limit message", body)
	}
}

func TestRateLimiterUsesXForwardedForFirstHop(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
	rl := apimw.NewRateLimiterForTest(clock)

	handler := rl.Login(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < apimw.LoginLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "198.51.100.42, 203.0.113.9")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.42, 203.0.113.9")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	// Different first hop should still be allowed.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req2.RemoteAddr = "10.0.0.1:12345"
	req2.Header.Set("X-Forwarded-For", "198.51.100.99")
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("other IP status = %d, want %d", rec2.Code, http.StatusOK)
	}
}

func TestRateLimiterWindowResetAllowsRetry(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	clock := &fixedClock{now: start}
	rl := apimw.NewRateLimiterForTest(clock)

	handler := rl.Login(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < apimw.LoginLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.5:12345"
		handler.ServeHTTP(rec, req)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	clock.now = start.Add(apimw.LoginWindow + time.Second)

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req2.RemoteAddr = "203.0.113.5:12345"
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("after window reset status = %d, want %d", rec2.Code, http.StatusOK)
	}
}

func TestRateLimiterForgotPasswordLimit(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
	rl := apimw.NewRateLimiterForTest(clock)

	handler := rl.ForgotPassword(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < apimw.ForgotPasswordLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", nil)
		req.RemoteAddr = "203.0.113.2:12345"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", nil)
	req.RemoteAddr = "203.0.113.2:12345"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}
