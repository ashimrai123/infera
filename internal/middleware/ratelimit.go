// Package middleware provides HTTP middleware for the infera gateway.
package middleware

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// ipLimiter pairs a rate limiter with a last-seen counter so the cleanup
// goroutine can evict stale entries (not needed for v2, kept for clarity).
type ipLimiter struct {
	limiter *rate.Limiter
}

// RateLimiter is a per-IP token-bucket middleware.
// Each unique client IP gets its own bucket: 10 requests/second, burst of 20.
// Requests that exceed the limit receive 429 Too Many Requests immediately.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	rps      rate.Limit
	burst    int
	next     http.Handler
}

// NewRateLimiter wraps next with per-IP rate limiting.
// Rate: 10 req/s per IP, burst cap: 20.
func NewRateLimiter(next http.Handler) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*ipLimiter),
		rps:      10,
		burst:    20,
		next:     next,
	}
}

func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if il, ok := rl.limiters[ip]; ok {
		return il.limiter
	}
	l := rate.NewLimiter(rl.rps, rl.burst)
	rl.limiters[ip] = &ipLimiter{limiter: l}
	return l
}

func (rl *RateLimiter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !rl.getLimiter(ip).Allow() {
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
		return
	}
	rl.next.ServeHTTP(w, r)
}

// clientIP extracts the client IP, preferring X-Forwarded-For when present
// (set by load balancers / reverse proxies in front of infera).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may be a comma-separated list; take the first entry.
		if i := len(xff); i > 0 {
			for j := 0; j < i; j++ {
				if xff[j] == ',' {
					return xff[:j]
				}
			}
			return xff
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
