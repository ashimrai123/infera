// Package breaker implements a per-provider circuit breaker.
//
// State machine:
//
//	Closed  --[5 consecutive failures]--> Open
//	Open    --[30s cooldown elapsed]----> HalfOpen
//	HalfOpen --[success]----------------> Closed
//	HalfOpen --[failure]----------------> Open
package breaker

import (
	"fmt"
	"sync"
	"time"
)

// State represents the three possible states of a circuit breaker.
type State int

const (
	// StateClosed is normal operation. Requests pass through.
	StateClosed State = iota
	// StateOpen means the circuit is tripped. Requests are rejected immediately.
	StateOpen
	// StateHalfOpen means we are probing whether the provider recovered.
	// One request is allowed through to test it.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Breaker is a single circuit breaker for one provider.
// All methods are safe for concurrent use.
type Breaker struct {
	mu        sync.Mutex
	state     State
	failures  int       // consecutive failures in Closed state
	threshold int       // failures needed to trip to Open
	cooldown  time.Duration
	openedAt  time.Time // when the breaker last transitioned to Open
}

// New returns a Breaker that opens after threshold consecutive failures and
// waits cooldown before probing again.
func New(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// Allow reports whether a request should be attempted for this provider.
//
//   - Closed: always yes.
//   - Open: no, unless the cooldown has elapsed, in which case we transition
//     to HalfOpen and allow exactly one probe request.
//   - HalfOpen: yes (the probe request).
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(b.openedAt) >= b.cooldown {
			// Cooldown elapsed -- allow one probe to test the provider.
			b.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		// Already in probe mode; allow the probe through.
		return true
	}
	return false
}

// RecordSuccess resets the breaker to Closed.
// Call this after a provider call succeeds.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.state = StateClosed
}

// RecordFailure records one failure.
//   - In Closed state: increments failure count. If threshold reached, trips to Open.
//   - In HalfOpen state: the probe failed, immediately trips back to Open.
//
// Call this after a provider call fails.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		b.failures++
		if b.failures >= b.threshold {
			b.trip()
		}
	case StateHalfOpen:
		// Probe failed -- provider still down. Reset cooldown.
		b.trip()
	}
	// Already Open: nothing to do, it stays open.
}

// State returns the current state (useful for logging/metrics).
func (b *Breaker) CurrentState() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// trip moves the breaker to Open and records when it happened.
// Must be called with b.mu held.
func (b *Breaker) trip() {
	b.state = StateOpen
	b.openedAt = time.Now()
	b.failures = 0
}

// ErrOpen is returned when a request is rejected because the breaker is open.
type ErrOpen struct {
	Provider string
	RetryIn  time.Duration
}

func (e *ErrOpen) Error() string {
	return fmt.Sprintf("circuit breaker open for provider %q, retry in %s", e.Provider, e.RetryIn.Round(time.Second))
}
