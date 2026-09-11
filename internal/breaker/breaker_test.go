package breaker_test

import (
	"testing"
	"time"

	"github.com/ashimrai123/infera/internal/breaker"
)

func newBreaker() *breaker.Breaker {
	// threshold=3, cooldown=100ms (short for tests)
	return breaker.New(3, 100*time.Millisecond)
}

// --- Closed state ---

func TestBreaker_InitiallyAllows(t *testing.T) {
	b := newBreaker()
	if !b.Allow() {
		t.Fatal("new breaker should allow requests")
	}
}

func TestBreaker_StaysClosedUnderThreshold(t *testing.T) {
	b := newBreaker()
	b.RecordFailure()
	b.RecordFailure() // 2 failures, threshold is 3
	if !b.Allow() {
		t.Fatal("breaker should stay closed before threshold")
	}
	if b.CurrentState() != breaker.StateClosed {
		t.Fatalf("expected closed, got %s", b.CurrentState())
	}
}

func TestBreaker_SuccessResetsFailureCount(t *testing.T) {
	b := newBreaker()
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess() // reset
	b.RecordFailure()
	b.RecordFailure() // only 2 after reset, should not trip
	if b.CurrentState() != breaker.StateClosed {
		t.Fatalf("success should reset counter; expected closed, got %s", b.CurrentState())
	}
}

// --- Open state ---

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	b := newBreaker()
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure() // 3 = threshold
	if b.CurrentState() != breaker.StateOpen {
		t.Fatalf("expected open after threshold, got %s", b.CurrentState())
	}
}

func TestBreaker_OpenRejectsRequests(t *testing.T) {
	b := newBreaker()
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	if b.Allow() {
		t.Fatal("open breaker should reject requests")
	}
}

func TestBreaker_OpenAllowsAfterCooldown(t *testing.T) {
	b := newBreaker()
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()

	time.Sleep(150 * time.Millisecond) // wait past 100ms cooldown

	if !b.Allow() {
		t.Fatal("breaker should allow probe after cooldown")
	}
	if b.CurrentState() != breaker.StateHalfOpen {
		t.Fatalf("expected half-open after cooldown, got %s", b.CurrentState())
	}
}

// --- HalfOpen state ---

func TestBreaker_HalfOpenSuccessCloses(t *testing.T) {
	b := newBreaker()
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	time.Sleep(150 * time.Millisecond)
	b.Allow() // transitions to half-open

	b.RecordSuccess()
	if b.CurrentState() != breaker.StateClosed {
		t.Fatalf("success in half-open should close breaker, got %s", b.CurrentState())
	}
	if !b.Allow() {
		t.Fatal("closed breaker should allow requests")
	}
}

func TestBreaker_HalfOpenFailureReopens(t *testing.T) {
	b := newBreaker()
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	time.Sleep(150 * time.Millisecond)
	b.Allow() // transitions to half-open

	b.RecordFailure() // probe failed
	if b.CurrentState() != breaker.StateOpen {
		t.Fatalf("failure in half-open should reopen breaker, got %s", b.CurrentState())
	}
	if b.Allow() {
		t.Fatal("reopened breaker should reject requests immediately")
	}
}

// --- ErrOpen ---

func TestErrOpen_Message(t *testing.T) {
	err := &breaker.ErrOpen{Provider: "anthropic", RetryIn: 28 * time.Second}
	msg := err.Error()
	if msg == "" {
		t.Fatal("ErrOpen should produce a non-empty message")
	}
}
