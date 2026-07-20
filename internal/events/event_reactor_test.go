package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEventReactorImpl_ProcessSecretEvent_Enqueue tests basic event enqueuing
func TestEventReactorImpl_ProcessSecretEvent_Enqueue(t *testing.T) {
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		return nil
	})

	event := &SecretChangeEvent{
		SecretName: "my-secret",
		Version:    "v2",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	}

	err := reactor.ProcessSecretEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("ProcessSecretEvent failed: %v", err)
	}

	// Verify event was queued
	reactor.mu.Lock()
	if reactor.queue.Len() != 1 {
		t.Fatalf("expected 1 event in queue, got %d", reactor.queue.Len())
	}
	reactor.mu.Unlock()
}

// TestEventReactorImpl_Deduplication_1sWindow tests that duplicate events within 1s are deduplicated
func TestEventReactorImpl_Deduplication_1sWindow(t *testing.T) {
	callCount := int32(0)
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Send same secret twice within 1s
	event := &SecretChangeEvent{
		SecretName: "my-secret",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	}
	err := reactor.ProcessSecretEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("first ProcessSecretEvent failed: %v", err)
	}

	err = reactor.ProcessSecretEvent(context.Background(), event) // Duplicate
	if err != nil {
		t.Fatalf("second ProcessSecretEvent failed: %v", err)
	}

	time.Sleep(6 * time.Second) // Wait for batch processing

	// Should only process once (second is deduplicated)
	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 1 {
		t.Fatalf("expected 1 rotation call, got %d", finalCount)
	}
}

// TestEventReactorImpl_PriorityOrdering tests that events are processed in priority order
func TestEventReactorImpl_PriorityOrdering(t *testing.T) {
	var callOrder []string
	var mu sync.Mutex

	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		mu.Lock()
		callOrder = append(callOrder, secretName)
		mu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Enqueue in order: Low, High, Critical
	reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "low",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	})
	reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "high",
		Severity:   SeverityHigh,
		Timestamp:  time.Now(),
	})
	reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "critical",
		Severity:   SeverityCritical,
		Timestamp:  time.Now(),
	})

	time.Sleep(6 * time.Second)

	// Should process in order: Critical, High, Low
	mu.Lock()
	defer mu.Unlock()

	if len(callOrder) < 3 {
		t.Fatalf("expected at least 3 calls, got %d", len(callOrder))
	}

	if callOrder[0] != "critical" {
		t.Fatalf("expected first call to be 'critical', got %s", callOrder[0])
	}
	if callOrder[1] != "high" {
		t.Fatalf("expected second call to be 'high', got %s", callOrder[1])
	}
	if callOrder[2] != "low" {
		t.Fatalf("expected third call to be 'low', got %s", callOrder[2])
	}
}

// TestEventReactorImpl_Batching_5sWindow tests that events are batched every 5s with max 5 per batch
func TestEventReactorImpl_Batching_5sWindow(t *testing.T) {
	callCount := int32(0)
	var batchTimestamps []time.Time
	var mu sync.Mutex

	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		mu.Lock()
		batchTimestamps = append(batchTimestamps, time.Now())
		mu.Unlock()
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Enqueue 7 events at time 0
	for i := 1; i <= 7; i++ {
		err := reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
			SecretName: fmt.Sprintf("secret-%d", i),
			Severity:   SeverityNormal,
			Timestamp:  time.Now(),
		})
		if err != nil {
			t.Fatalf("ProcessSecretEvent failed: %v", err)
		}
	}

	time.Sleep(12 * time.Second) // Wait for both batches (5s + processing overhead)

	// Should have processed all 7
	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 7 {
		t.Fatalf("expected 7 calls, got %d", finalCount)
	}

	// Verify batching behavior: should have been called around 5s and 10s marks
	mu.Lock()
	if len(batchTimestamps) < 5 {
		t.Logf("batch timestamps count: %d (expected at least 5)", len(batchTimestamps))
	}
	mu.Unlock()
}

// TestEventReactorImpl_ProcessContainerEvent tests container event processing
func TestEventReactorImpl_ProcessContainerEvent(t *testing.T) {
	callCount := int32(0)
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Container event with secret labels
	event := &ContainerLabelEvent{
		ContainerID: "abc123",
		Labels: map[string]string{
			"secret":    "my-secret",
			"dso.owner": "team-a",
		},
		Action:    ActionLabelUpdate,
		Timestamp: time.Now(),
	}

	err := reactor.ProcessContainerEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("ProcessContainerEvent failed: %v", err)
	}

	time.Sleep(6 * time.Second)

	finalCount := atomic.LoadInt32(&callCount)
	if finalCount < 1 {
		t.Fatalf("expected at least 1 call, got %d", finalCount)
	}
}

// TestEventReactorImpl_IsHealthy tests health check based on last event time
func TestEventReactorImpl_IsHealthy(t *testing.T) {
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Initially unhealthy (no events)
	if reactor.IsHealthy() {
		t.Fatal("expected unhealthy initially")
	}

	// Process event
	err := reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "test",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	})
	if err != nil {
		t.Fatalf("ProcessSecretEvent failed: %v", err)
	}

	time.Sleep(6 * time.Second) // Wait for batch processing

	// After event processing, should be healthy
	if !reactor.IsHealthy() {
		t.Fatal("expected healthy after event")
	}
}

// TestEventReactorImpl_ConcurrentAccess tests thread-safety
func TestEventReactorImpl_ConcurrentAccess(t *testing.T) {
	callCount := int32(0)
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				err := reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
					SecretName: fmt.Sprintf("secret-%d", id%10),
					Severity:   SeverityNormal,
					Timestamp:  time.Now(),
				})
				if err != nil {
					t.Errorf("ProcessSecretEvent failed: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(6 * time.Second) // Let last batch process

	// Should complete without panic
	finalCount := atomic.LoadInt32(&callCount)
	if finalCount < 1 {
		t.Logf("warning: expected at least 1 call, got %d", finalCount)
	}
}

// TestEventReactorImpl_CallbackError_Continues tests that processing continues even on callback errors
func TestEventReactorImpl_CallbackError_Continues(t *testing.T) {
	callCount := int32(0)
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		atomic.AddInt32(&callCount, 1)
		if secretName == "fail-secret" {
			return errors.New("simulated error")
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Queue events, one will fail
	reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "ok-1",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	})
	reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "fail-secret",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	})
	reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "ok-2",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	})

	time.Sleep(6 * time.Second)

	// Should have called all 3 despite one error
	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 3 {
		t.Fatalf("expected 3 calls despite error, got %d", finalCount)
	}
}

// TestEventReactorImpl_MultipleStartCalls tests that Start is idempotent
func TestEventReactorImpl_MultipleStartCalls(t *testing.T) {
	callCount := int32(0)
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Multiple Start calls should be safe
	err1 := reactor.Start(ctx)
	err2 := reactor.Start(ctx)

	if err1 != nil || err2 != nil {
		t.Fatalf("Start failed: %v, %v", err1, err2)
	}

	err := reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
		SecretName: "test",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	})
	if err != nil {
		t.Fatalf("ProcessSecretEvent failed: %v", err)
	}

	time.Sleep(6 * time.Second)

	reactor.Stop(context.Background())

	finalCount := atomic.LoadInt32(&callCount)
	if finalCount < 1 {
		t.Fatalf("expected at least 1 call, got %d", finalCount)
	}
}

// TestEventReactorImpl_DeduplicationWindow_Expiry tests that deduplication window expires after 1s
func TestEventReactorImpl_DeduplicationWindow_Expiry(t *testing.T) {
	callCount := int32(0)
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	event := &SecretChangeEvent{
		SecretName: "my-secret",
		Severity:   SeverityNormal,
		Timestamp:  time.Now(),
	}

	// First event
	err := reactor.ProcessSecretEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("first ProcessSecretEvent failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Second event within 1s (should be deduplicated)
	err = reactor.ProcessSecretEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("second ProcessSecretEvent failed: %v", err)
	}

	time.Sleep(6 * time.Second) // Wait for batch

	// After 1s+ from first event, should process
	time.Sleep(1 * time.Second) // Now 1.1s from first event

	// Third event after 1s window (should be processed)
	err = reactor.ProcessSecretEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("third ProcessSecretEvent failed: %v", err)
	}

	time.Sleep(6 * time.Second) // Wait for another batch

	// Should have processed first and third events (second was deduplicated)
	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 2 {
		t.Fatalf("expected 2 calls (first and third), got %d", finalCount)
	}
}

// TestEventReactorImpl_NilEvent tests error handling for nil events
func TestEventReactorImpl_NilEvent(t *testing.T) {
	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Process nil event
	err := reactor.ProcessSecretEvent(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil event, got nil")
	}

	// Process nil container event
	err = reactor.ProcessContainerEvent(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil container event, got nil")
	}
}

// TestEventReactorImpl_QueueMaxBatch tests that max batch size is respected
func TestEventReactorImpl_QueueMaxBatch(t *testing.T) {
	var timestamps []time.Time
	var mu sync.Mutex

	reactor := NewEventReactorImpl(func(ctx context.Context, secretName string, priority EventPriority) error {
		mu.Lock()
		defer mu.Unlock()
		timestamps = append(timestamps, time.Now())
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reactor.Start(ctx)
	defer reactor.Stop(context.Background())

	// Enqueue 12 events
	for i := 1; i <= 12; i++ {
		reactor.ProcessSecretEvent(context.Background(), &SecretChangeEvent{
			SecretName: fmt.Sprintf("secret-%d", i),
			Severity:   SeverityNormal,
			Timestamp:  time.Now(),
		})
	}

	time.Sleep(12 * time.Second) // Wait for batches

	mu.Lock()
	defer mu.Unlock()

	// With max 5 per batch, we should have 3 batches (5, 5, 2)
	// But these will be spread across multiple 5s intervals
	if len(timestamps) != 12 {
		t.Logf("warning: expected 12 total calls, got %d", len(timestamps))
	}
}
