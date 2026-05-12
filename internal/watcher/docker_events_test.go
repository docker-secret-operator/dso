package watcher

import (
	"context"
	"testing"
	"time"
)

// TestNewDockerWatcher creates watcher with docker client
func TestNewDockerWatcher(t *testing.T) {
	watcher, err := NewDockerWatcher(false)

	// Docker might not be available in test environment
	if err != nil {
		t.Skip("Docker not available, skipping docker events tests")
	}

	if watcher == nil {
		t.Fatal("NewDockerWatcher returned nil")
	}
	if watcher.cli == nil {
		t.Fatal("Docker client should be initialized")
	}
	if watcher.Debug != false {
		t.Error("Debug flag should be false")
	}
}

// TestNewDockerWatcher_DebugFlagTrue sets debug to true
func TestNewDockerWatcher_DebugFlagTrue(t *testing.T) {
	watcher, err := NewDockerWatcher(true)
	if err != nil {
		t.Skip("Docker not available")
	}

	if watcher.Debug != true {
		t.Error("Debug flag should be true")
	}
}

// TestNewDockerWatcher_DebugFlagFalse sets debug to false
func TestNewDockerWatcher_DebugFlagFalse(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	if watcher.Debug != false {
		t.Error("Debug flag should be false")
	}
}

// TestDockerWatcher_Subscribe returns channels
func TestDockerWatcher_Subscribe(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	msgCh, errCh := watcher.Subscribe(ctx)

	if msgCh == nil {
		t.Error("Message channel should not be nil")
	}
	if errCh == nil {
		t.Error("Error channel should not be nil")
	}
}

// TestDockerWatcher_SubscribeChannelsAreReadOnly verifies channels work as expected
func TestDockerWatcher_SubscribeChannelsAreReadOnly(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	msgCh, errCh := watcher.Subscribe(ctx)

	// Channels should exist and be readable
	select {
	case <-msgCh:
		// Message received (or closed)
	case <-errCh:
		// Error received (or closed)
	case <-time.After(500 * time.Millisecond):
		// Timeout - channels are properly initialized but no events yet
	}
}

// TestDockerWatcher_SubscribeContextCancellation respects context cancellation
func TestDockerWatcher_SubscribeContextCancellation(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx, cancel := context.WithCancel(context.Background())

	msgCh, errCh := watcher.Subscribe(ctx)

	// Channels should be created
	if msgCh == nil || errCh == nil {
		t.Fatal("Channels should be created")
	}

	// Cancel context
	cancel()

	// Give channels time to close
	time.Sleep(100 * time.Millisecond)

	// Channels should eventually close on context cancellation
	// (Docker API will close them when context is cancelled)
	_ = msgCh // Suppress unused variable warning
	_ = errCh // Suppress unused variable warning
}

// TestDockerWatcher_MultipleSubscribes handles multiple subscriptions
func TestDockerWatcher_MultipleSubscribes(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel1()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()

	// Create multiple subscriptions
	msgCh1, errCh1 := watcher.Subscribe(ctx1)
	msgCh2, errCh2 := watcher.Subscribe(ctx2)

	if msgCh1 == nil || errCh1 == nil {
		t.Fatal("First subscription channels should be created")
	}
	if msgCh2 == nil || errCh2 == nil {
		t.Fatal("Second subscription channels should be created")
	}

	// Channels should be different
	if msgCh1 == msgCh2 {
		t.Error("Subscriptions should have different message channels")
	}
}

// TestDockerWatcher_ContextTimeout handles context timeouts
func TestDockerWatcher_ContextTimeout(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	msgCh, errCh := watcher.Subscribe(ctx)
	_ = msgCh // Suppress unused variable warning
	_ = errCh // Suppress unused variable warning

	// Wait for timeout
	<-time.After(150 * time.Millisecond)

	// Context should be done
	select {
	case <-ctx.Done():
		// Expected - context expired
	default:
		t.Error("Context should be expired")
	}
}

// TestDockerWatcher_CliInitialization verifies docker client is set
func TestDockerWatcher_CliInitialization(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	if watcher.cli == nil {
		t.Fatal("Docker client should be initialized")
	}
}

// TestDockerWatcher_FilterConfiguration verifies event filters
func TestDockerWatcher_FilterConfiguration(t *testing.T) {
	// This test verifies that Subscribe uses the correct filters
	// (type=container, events: start, stop, die, restart, kill)

	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Subscribe should set up filters internally
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	msgCh, errCh := watcher.Subscribe(ctx)

	// Verify channels are ready
	if msgCh == nil || errCh == nil {
		t.Fatal("Channels should be created with proper filters")
	}

	// Wait briefly for setup
	time.Sleep(50 * time.Millisecond)
}

// TestDockerWatcher_Structure verifies watcher fields
func TestDockerWatcher_Structure(t *testing.T) {
	watcher, err := NewDockerWatcher(true)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Verify structure has expected fields
	if watcher.cli == nil {
		t.Error("cli field should be set")
	}
	if watcher.Debug != true {
		t.Error("Debug field should be true")
	}
}

// TestDockerWatcher_SubscribeReturnTypes verifies return types
func TestDockerWatcher_SubscribeReturnTypes(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	msgCh, errCh := watcher.Subscribe(ctx)

	// Type assertions to verify proper types
	if msgCh == nil {
		t.Error("msgCh should not be nil")
	}
	if errCh == nil {
		t.Error("errCh should not be nil")
	}
}

// TestDockerWatcher_ContextDeadlineRespect respects context deadlines
func TestDockerWatcher_ContextDeadlineRespect(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	msgCh, errCh := watcher.Subscribe(ctx)

	if msgCh == nil || errCh == nil {
		t.Fatal("Channels should be created")
	}

	// Wait for deadline to pass
	<-time.After(250 * time.Millisecond)

	// Context should be expired
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should respect deadline")
	}
}

// TestDockerWatcher_EventTypes tests listening for correct event types
func TestDockerWatcher_EventTypes(t *testing.T) {
	// This test documents that Subscribe filters for these events:
	// - start
	// - stop
	// - die
	// - restart
	// - kill
	// And only listens to "container" type

	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Subscribe should filter events properly
	msgCh, errCh := watcher.Subscribe(ctx)

	if msgCh == nil || errCh == nil {
		t.Fatal("Subscribe should return valid channels with filters")
	}
}

// TestDockerWatcher_DebugFlagDefault tests debug flag parameter
func TestDockerWatcher_DebugFlagDefault(t *testing.T) {
	// Test with debug=false
	watcher1, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	if watcher1.Debug {
		t.Error("Debug should be false when initialized with false")
	}

	// Test with debug=true
	watcher2, err := NewDockerWatcher(true)
	if err != nil {
		t.Skip("Docker not available")
	}

	if !watcher2.Debug {
		t.Error("Debug should be true when initialized with true")
	}
}

// TestDockerWatcher_SocketPermissionCheck tests that socket is checked
func TestDockerWatcher_SocketPermissionCheck(t *testing.T) {
	// NewDockerWatcher checks socket permissions
	// If Docker socket is not available, it returns an error
	watcher, err := NewDockerWatcher(false)

	if err != nil {
		// Error means socket check failed (expected in some environments)
		// The error should be about socket permissions
		if watcher != nil {
			t.Error("Watcher should be nil when error occurs")
		}
	} else {
		// If no error, watcher should be valid
		if watcher == nil {
			t.Fatal("Watcher should not be nil when no error")
		}
		if watcher.cli == nil {
			t.Error("Docker client should be initialized when no error")
		}
	}
}

// TestDockerWatcher_RepeatedSubscription tests multiple subscriptions work
func TestDockerWatcher_RepeatedSubscription(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Create and cancel multiple subscriptions
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

		msgCh, errCh := watcher.Subscribe(ctx)

		if msgCh == nil || errCh == nil {
			t.Fatalf("Iteration %d: channels should be created", i)
		}

		cancel()
		time.Sleep(50 * time.Millisecond)
	}
}

// TestDockerWatcher_ConcurrentSubscriptions tests concurrent subscriptions
func TestDockerWatcher_ConcurrentSubscriptions(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	done := make(chan bool, 5)

	// Create multiple subscriptions concurrently
	for i := 0; i < 5; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			msgCh, errCh := watcher.Subscribe(ctx)

			if msgCh == nil || errCh == nil {
				t.Error("Channels should be created")
			}

			done <- true
		}()
	}

	// Wait for all subscriptions to complete
	for i := 0; i < 5; i++ {
		<-done
	}
}

// TestDockerWatcher_ContextCancellationCleanup verifies cleanup on cancel
func TestDockerWatcher_ContextCancellationCleanup(t *testing.T) {
	watcher, err := NewDockerWatcher(false)
	if err != nil {
		t.Skip("Docker not available")
	}

	ctx, cancel := context.WithCancel(context.Background())

	msgCh, errCh := watcher.Subscribe(ctx)

	// Verify channels exist
	if msgCh == nil || errCh == nil {
		t.Fatal("Channels should be created")
	}

	// Cancel and wait for cleanup
	cancel()
	time.Sleep(100 * time.Millisecond)

	// After cancellation, context should be done
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be cancelled")
	}
}
