package providers

import (
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cb := NewCircuitBreaker("test-provider", logger, 3, 2, 100*time.Millisecond)

	if !cb.IsAvailable() {
		t.Fatal("expected available in closed state")
	}
	if cb.GetState() != StateClosed {
		t.Fatalf("expected closed, got %s", cb.GetState())
	}

	// Record successes — should stay closed
	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.GetState() != StateClosed {
		t.Fatalf("expected still closed after successes, got %s", cb.GetState())
	}

	// Trip the breaker
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure() // hits threshold of 3

	if cb.GetState() != StateOpen {
		t.Fatalf("expected open after %d failures, got %s", 3, cb.GetState())
	}
	if cb.IsAvailable() {
		t.Fatal("expected unavailable in open state")
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	logger := zaptest.NewLogger(t)
	// Very short reset timeout so we can test the transition
	cb := NewCircuitBreaker("test-provider", logger, 1, 1, 1*time.Millisecond)

	cb.RecordFailure() // opens immediately (threshold=1)
	if cb.GetState() != StateOpen {
		t.Fatalf("expected open, got %s", cb.GetState())
	}

	time.Sleep(5 * time.Millisecond) // exceed reset timeout

	// IsAvailable transitions open → half_open
	if !cb.IsAvailable() {
		t.Fatal("expected available after reset timeout (half-open)")
	}
	if cb.GetState() != StateHalf {
		t.Fatalf("expected half_open, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cb := NewCircuitBreaker("test-provider", logger, 1, 2, 1*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)
	cb.IsAvailable() // transition to half_open

	cb.RecordSuccess()
	cb.RecordSuccess() // hits successThreshold of 2

	if cb.GetState() != StateClosed {
		t.Fatalf("expected closed after enough successes, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenReopensOnFailure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cb := NewCircuitBreaker("test-provider", logger, 1, 3, 1*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)
	cb.IsAvailable() // transition to half_open

	cb.RecordFailure() // should reopen immediately

	if cb.GetState() != StateOpen {
		t.Fatalf("expected reopened, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_GetStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cb := NewCircuitBreaker("my-prov", logger, 5, 3, 30*time.Second)

	status := cb.GetStatus()
	if status["provider"] != "my-prov" {
		t.Errorf("unexpected provider: %v", status["provider"])
	}
	if status["state"] != StateClosed {
		t.Errorf("unexpected state: %v", status["state"])
	}
}
