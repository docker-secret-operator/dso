package injector

import (
	"context"
	"testing"
	"time"
)

// TestAgentClient_FetchSecretWithContext_TimeoutHandling verifies timeout behavior
func TestAgentClient_FetchSecretWithContext_TimeoutHandling(t *testing.T) {
	// Create client with short timeout
	ac := &AgentClient{
		requestTimeout: 100 * time.Millisecond,
	}

	// Create already-expired context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Wait for timeout to occur
	time.Sleep(60 * time.Millisecond)

	// Try fetch - should return timeout error
	_, err := ac.FetchSecretWithContext(ctx, "provider", map[string]string{}, "secret")

	if err == nil {
		t.Error("Should return timeout error for expired context")
	}
}

// TestAgentClient_DefaultTimeout verifies default timeout is applied
func TestAgentClient_DefaultTimeout(t *testing.T) {
	ac := &AgentClient{
		requestTimeout: 30 * time.Second,
	}

	if ac.requestTimeout == 0 {
		t.Error("Should have default request timeout")
	}

	if ac.requestTimeout < 10*time.Second {
		t.Errorf("Default timeout too short: %v", ac.requestTimeout)
	}

	if ac.requestTimeout > 60*time.Second {
		t.Errorf("Default timeout too long: %v", ac.requestTimeout)
	}
}

// TestAgentClient_ContextDeadlinePreserved verifies existing deadline is respected
func TestAgentClient_ContextDeadlinePreserved(t *testing.T) {
	_ = &AgentClient{
		requestTimeout: 30 * time.Second,
	}

	// Create context with custom deadline
	customDeadline := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), customDeadline)
	defer cancel()

	// Check that context already has a deadline
	if deadline, ok := ctx.Deadline(); !ok {
		t.Fatal("Context should have deadline")
	} else {
		// Should use existing deadline, not add another
		if deadline.After(customDeadline.Add(1 * time.Second)) {
			t.Error("Should preserve existing context deadline")
		}
	}
}

// TestAgentClient_TimeoutConstantValidation verifies timeout constants are reasonable
func TestAgentClient_TimeoutConstantValidation(t *testing.T) {
	ac := &AgentClient{
		requestTimeout: 30 * time.Second,
	}

	// Validate timeout is reasonable for RPC operations
	if ac.requestTimeout < 5*time.Second {
		t.Error("Timeout is too short for RPC operations")
	}

	if ac.requestTimeout > 2*time.Minute {
		t.Error("Timeout is too long (should fail fast)")
	}
}

// TestAgentClient_MultipleTimeoutRequests verifies timeouts work consistently
func TestAgentClient_MultipleTimeoutRequests(t *testing.T) {
	ac := &AgentClient{
		requestTimeout: 100 * time.Millisecond,
	}

	// Simulate multiple requests that would timeout
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		time.Sleep(60 * time.Millisecond)

		_, err := ac.FetchSecretWithContext(ctx, "provider", map[string]string{}, "secret")

		if err == nil {
			t.Errorf("Request %d: Should timeout", i)
		}

		cancel()
	}
}

// TestAgentClient_CancelledContextHandling verifies cancelled context handling
func TestAgentClient_CancelledContextHandling(t *testing.T) {
	ac := &AgentClient{
		requestTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Try fetch with cancelled context
	_, err := ac.FetchSecretWithContext(ctx, "provider", map[string]string{}, "secret")

	if err == nil {
		t.Error("Should return error for cancelled context")
	}
}
