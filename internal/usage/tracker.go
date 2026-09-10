// Package usage tracks per-provider request, token, and error counts in
// memory. All methods are safe for concurrent use.
package usage

import (
	"sync"
	"sync/atomic"
)

// ProviderStats holds cumulative counters for a single provider.
// Fields are updated atomically so readers never need to hold the mutex.
type ProviderStats struct {
	Requests int64 `json:"requests"`
	Tokens   int64 `json:"tokens"`
	Errors   int64 `json:"errors"`
}

// Tracker keeps per-provider counters for the lifetime of the process.
type Tracker struct {
	mu    sync.RWMutex
	stats map[string]*ProviderStats
}

func New() *Tracker {
	return &Tracker{stats: make(map[string]*ProviderStats)}
}

func (t *Tracker) providerStats(name string) *ProviderStats {
	t.mu.RLock()
	s, ok := t.stats[name]
	t.mu.RUnlock()
	if ok {
		return s
	}

	// First time we see this provider: upgrade to write lock and insert.
	t.mu.Lock()
	defer t.mu.Unlock()
	// Re-check under write lock (another goroutine may have beaten us).
	if s, ok = t.stats[name]; ok {
		return s
	}
	s = &ProviderStats{}
	t.stats[name] = s
	return s
}

// RecordRequest increments the request counter for the named provider.
func (t *Tracker) RecordRequest(provider string) {
	atomic.AddInt64(&t.providerStats(provider).Requests, 1)
}

// RecordTokens adds n tokens to the running total for the named provider.
func (t *Tracker) RecordTokens(provider string, n int64) {
	if n > 0 {
		atomic.AddInt64(&t.providerStats(provider).Tokens, n)
	}
}

// RecordError increments the error counter for the named provider.
func (t *Tracker) RecordError(provider string) {
	atomic.AddInt64(&t.providerStats(provider).Errors, 1)
}

// Snapshot returns a copy of all counters at the time of the call.
// The returned map is safe to marshal to JSON without holding any lock.
func (t *Tracker) Snapshot() map[string]ProviderStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make(map[string]ProviderStats, len(t.stats))
	for name, s := range t.stats {
		out[name] = ProviderStats{
			Requests: atomic.LoadInt64(&s.Requests),
			Tokens:   atomic.LoadInt64(&s.Tokens),
			Errors:   atomic.LoadInt64(&s.Errors),
		}
	}
	return out
}
