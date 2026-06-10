package auth

import (
	"testing"
	"time"
)

func TestRateLimiter_NotLimitedInitially(t *testing.T) {
	rl := NewRateLimiter(5, 15*time.Minute)
	if rl.IsLimited("user:alice") {
		t.Fatal("should not be limited before any failures")
	}
}

func TestRateLimiter_LimitedAfterThreshold(t *testing.T) {
	rl := NewRateLimiter(5, 15*time.Minute)
	for i := 0; i < 5; i++ {
		rl.RecordFailure("user:alice")
	}
	if !rl.IsLimited("user:alice") {
		t.Fatal("should be limited after 5 failures")
	}
}

func TestRateLimiter_ResetClearsLimit(t *testing.T) {
	rl := NewRateLimiter(5, 15*time.Minute)
	for i := 0; i < 5; i++ {
		rl.RecordFailure("user:alice")
	}
	rl.Reset("user:alice")
	if rl.IsLimited("user:alice") {
		t.Fatal("should not be limited after reset")
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	// Very short window so entries expire quickly
	rl := NewRateLimiter(3, 10*time.Millisecond)
	for i := 0; i < 3; i++ {
		rl.RecordFailure("ip:1.2.3.4")
	}
	if !rl.IsLimited("ip:1.2.3.4") {
		t.Fatal("should be limited immediately after 3 failures")
	}
	// Wait for window to expire
	time.Sleep(20 * time.Millisecond)
	if rl.IsLimited("ip:1.2.3.4") {
		t.Fatal("should not be limited after window expires")
	}
}

func TestRateLimiter_DifferentKeysDontInterfere(t *testing.T) {
	rl := NewRateLimiter(3, 15*time.Minute)
	for i := 0; i < 3; i++ {
		rl.RecordFailure("user:alice")
	}
	if rl.IsLimited("user:bob") {
		t.Fatal("bob should not be limited because alice exceeded the threshold")
	}
}
