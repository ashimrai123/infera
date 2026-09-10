package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ashimrai123/infera/internal/middleware"
)

// nopHandler is an inner handler that always returns 200.
var nopHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := middleware.NewRateLimiter(nopHandler)

	// Burst is 20, so the first 20 requests from the same IP must all pass.
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "1.2.3.4:9999"
		w := httptest.NewRecorder()
		rl.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimiter_BlocksAfterBurst(t *testing.T) {
	rl := middleware.NewRateLimiter(nopHandler)

	// Drain the entire burst (20 tokens).
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rl.ServeHTTP(httptest.NewRecorder(), req)
	}

	// The 21st request must be rejected.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	rl.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w.Code)
	}
}

func TestRateLimiter_DifferentIPsHaveSeparateBuckets(t *testing.T) {
	rl := middleware.NewRateLimiter(nopHandler)

	// Drain IP A's burst.
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "192.168.1.1:1"
		rl.ServeHTTP(httptest.NewRecorder(), req)
	}

	// IP B should still have a full bucket.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "192.168.1.2:1"
	w := httptest.NewRecorder()
	rl.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("IP B should not be rate limited, got %d", w.Code)
	}
}

func TestRateLimiter_XForwardedFor(t *testing.T) {
	rl := middleware.NewRateLimiter(nopHandler)

	// Drain via X-Forwarded-For.
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "127.0.0.1:9999" // proxy address
		req.Header.Set("X-Forwarded-For", "203.0.113.5")
		rl.ServeHTTP(httptest.NewRecorder(), req)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	w := httptest.NewRecorder()
	rl.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 for XFF IP after burst, got %d", w.Code)
	}
}
