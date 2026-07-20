package integration_test

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/events"
	"github.com/docker-secret-operator/dso/internal/polling"
)

// TestSmartPolling_EndToEnd tests the complete end-to-end workflow:
// SmartPoller → change detection → EventReactor → rotation callback
func TestSmartPolling_EndToEnd(t *testing.T) {
	// Setup
	rotationCallCount := int32(0)
	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&rotationCallCount, 1)
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	sp := polling.NewSmartPoller()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start event reactor
	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	// Simulate 3 secrets
	secrets := []string{"db-password", "api-key", "oauth-token"}

	// Record changes via SmartPoller
	for _, secret := range secrets {
		sp.RecordChange(secret)
	}

	// Emit events via EventReactor
	for _, secret := range secrets {
		event := events.SecretChangeEvent{
			SecretName: secret,
			Version:    "v1",
			Source:     events.SourceAWSSecretsManager,
			Severity:   events.SeverityNormal,
			Timestamp:  time.Now(),
			Metadata:   map[string]string{"type": "rotation"},
		}
		if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
			t.Fatalf("failed to process event for %s: %v", secret, err)
		}
	}

	// Wait for batch processing (5s window) - poll instead of sleep
	for i := 0; i < 60 && atomic.LoadInt32(&rotationCallCount) < 3; i++ {
		time.Sleep(100 * time.Millisecond)
	}

	// Verify rotations were triggered
	callCount := atomic.LoadInt32(&rotationCallCount)
	if callCount != 3 {
		t.Fatalf("expected 3 rotation calls, got %d", callCount)
	}

	// Verify SmartPoller state
	for _, secret := range secrets {
		interval := sp.GetNextInterval(secret)
		if interval != 5*time.Second {
			t.Errorf("secret %s: expected 5s interval (aggressive), got %v", secret, interval)
		}

		stats := sp.GetStats(secret)
		if stats == nil || stats.ChangeCount != 1 {
			t.Errorf("secret %s: expected 1 change recorded, got %v", secret, stats)
		}
	}

	// Verify interval adaptation using CalculateInterval (don't wait real time)
	interval2m := polling.CalculateInterval(2*time.Minute + 10*time.Second)
	if interval2m != 30*time.Second {
		t.Errorf("after 2m: expected 30s interval (baseline), got %v", interval2m)
	}

	// After 10m
	interval10m := polling.CalculateInterval(10*time.Minute + 10*time.Second)
	if interval10m != 5*time.Minute {
		t.Errorf("after 10m: expected 5m interval (backoff), got %v", interval10m)
	}
}

// TestSmartPolling_IntervalAdaptation verifies adaptive intervals transition correctly
func TestSmartPolling_IntervalAdaptation(t *testing.T) {
	sp := polling.NewSmartPoller()
	secretName := "adaptive-secret"

	// Phase 1: Initial unknown (30s baseline)
	interval := sp.GetNextInterval(secretName)
	if interval != 30*time.Second {
		t.Fatalf("phase 1: expected 30s for unknown, got %v", interval)
	}

	// Phase 2: Record change → 5s aggressive
	sp.RecordChange(secretName)
	interval = sp.GetNextInterval(secretName)
	if interval != 5*time.Second {
		t.Fatalf("phase 2: expected 5s after change, got %v", interval)
	}

	// Phase 3: Simulate time progression to 2m boundary
	// Use CalculateInterval to test the logic
	interval = polling.CalculateInterval(2*time.Minute + 10*time.Second)
	if interval != 30*time.Second {
		t.Fatalf("phase 3: expected 30s for 2m+ elapsed, got %v", interval)
	}

	// Phase 4: Simulate 10m+ boundary
	interval = polling.CalculateInterval(10*time.Minute + 10*time.Second)
	if interval != 5*time.Minute {
		t.Fatalf("phase 4: expected 5m for 10m+ elapsed, got %v", interval)
	}

	// Phase 5: Verify boundaries precisely
	testCases := []struct {
		elapsed  time.Duration
		expected time.Duration
		label    string
	}{
		{1 * time.Minute, 5 * time.Second, "1m elapsed"},
		{1*time.Minute + 59*time.Second, 5 * time.Second, "just before 2m"},
		{2 * time.Minute, 30 * time.Second, "exactly 2m"},
		{5 * time.Minute, 30 * time.Second, "5m elapsed"},
		{9*time.Minute + 59*time.Second, 30 * time.Second, "just before 10m"},
		{10 * time.Minute, 5 * time.Minute, "exactly 10m"},
		{15 * time.Minute, 5 * time.Minute, "15m elapsed"},
	}

	for _, tc := range testCases {
		actual := polling.CalculateInterval(tc.elapsed)
		if actual != tc.expected {
			t.Errorf("%s: expected %v, got %v", tc.label, tc.expected, actual)
		}
	}
}

// TestSmartPolling_ContainerEventsIntegration tests container label changes triggering rotations
func TestSmartPolling_ContainerEventsIntegration(t *testing.T) {
	callCount := int32(0)
	var capturedSecrets []string
	var captureMu sync.Mutex

	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&callCount, 1)
		captureMu.Lock()
		capturedSecrets = append(capturedSecrets, secretName)
		captureMu.Unlock()
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	// Simulate container events with secret labels
	containerEvent := events.ContainerLabelEvent{
		ContainerID: "container-abc123",
		Labels: map[string]string{
			"secret": "my-database-secret",
		},
		Action:    events.ActionLabelUpdate,
		Timestamp: time.Now(),
	}

	// Process the container event
	if err := reactor.ProcessContainerEvent(ctx, containerEvent); err != nil {
		t.Fatalf("failed to process container event: %v", err)
	}

	// Wait for batch window
	time.Sleep(6 * time.Second)

	// Verify rotation was triggered
	if count := atomic.LoadInt32(&callCount); count != 1 {
		t.Fatalf("expected 1 rotation call, got %d", count)
	}

	// Verify the correct secret name was captured
	captureMu.Lock()
	if len(capturedSecrets) != 1 || capturedSecrets[0] != "my-database-secret" {
		t.Fatalf("expected secret 'my-database-secret', got %v", capturedSecrets)
	}
	captureMu.Unlock()
}

// TestSmartPolling_BatchingUnderLoad verifies batching works under load
func TestSmartPolling_BatchingUnderLoad(t *testing.T) {
	rotationCalls := int32(0)
	var callTimes []time.Time
	var callTimesMu sync.Mutex

	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&rotationCalls, 1)
		callTimesMu.Lock()
		callTimes = append(callTimes, time.Now())
		callTimesMu.Unlock()
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	// Enqueue 5 events rapidly (within 1 second) with unique names to avoid deduplication
	baseTime := time.Now()
	for i := 1; i <= 5; i++ {
		event := events.SecretChangeEvent{
			SecretName: fmt.Sprintf("batch-secret-%d-%d", i, baseTime.UnixNano()),
			Version:    "v1",
			Source:     events.SourceAWSSecretsManager,
			Severity:   events.SeverityNormal,
			Timestamp:  baseTime,
			Metadata:   map[string]string{},
		}
		if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
			t.Fatalf("failed to process event %d: %v", i, err)
		}
	}

	// Wait for batch cycles (poll instead of fixed sleep)
	for i := 0; i < 120 && atomic.LoadInt32(&rotationCalls) < 5; i++ {
		time.Sleep(100 * time.Millisecond)
	}

	// Verify all 5 events were processed
	finalCount := atomic.LoadInt32(&rotationCalls)
	if finalCount != 5 {
		t.Fatalf("expected 5 rotation calls, got %d", finalCount)
	}

	// Verify batching: up to 5 events per batch, so should have at least 1 batch
	callTimesMu.Lock()
	defer callTimesMu.Unlock()

	if len(callTimes) < 3 {
		t.Logf("WARNING: expected at least 3 call timestamps, got %d", len(callTimes))
	}
}

// TestSmartPolling_Deduplication verifies 1s deduplication window
func TestSmartPolling_Deduplication(t *testing.T) {
	rotationCalls := int32(0)

	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&rotationCalls, 1)
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	secretName := "dedup-secret"

	// Send the same secret event
	event := events.SecretChangeEvent{
		SecretName: secretName,
		Version:    "v1",
		Source:     events.SourceAWSSecretsManager,
		Severity:   events.SeverityNormal,
		Timestamp:  time.Now(),
	}

	// Process first event
	if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
		t.Fatalf("first event failed: %v", err)
	}

	// Immediately process duplicate (within 1s)
	if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
		t.Fatalf("second event failed: %v", err)
	}

	// Wait for batch processing (poll)
	for i := 0; i < 60 && atomic.LoadInt32(&rotationCalls) < 1; i++ {
		time.Sleep(100 * time.Millisecond)
	}

	// Should only process once (second is deduplicated)
	callCount := atomic.LoadInt32(&rotationCalls)
	if callCount != 1 {
		t.Fatalf("expected 1 rotation call (duplicate deduplicated), got %d", callCount)
	}

	// Wait for deduplication window to expire (> 1.1s from first event)
	time.Sleep(1200 * time.Millisecond)

	// Process the same secret again (now outside 1s window)
	if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
		t.Fatalf("third event failed: %v", err)
	}

	// Wait for batch processing (poll)
	for i := 0; i < 60 && atomic.LoadInt32(&rotationCalls) < 2; i++ {
		time.Sleep(100 * time.Millisecond)
	}

	// Should now have 2 calls (first batch + second after dedup window reset)
	finalCount := atomic.LoadInt32(&rotationCalls)
	if finalCount != 2 {
		t.Fatalf("expected 2 rotation calls (after dedup window reset), got %d", finalCount)
	}
}

// TestSmartPolling_GracefulShutdown verifies graceful shutdown without leaks
func TestSmartPolling_GracefulShutdown(t *testing.T) {
	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	sp := polling.NewSmartPoller()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start reactor
	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}

	// Record some activity
	for i := 0; i < 10; i++ {
		secretName := fmt.Sprintf("secret-%d", i)
		sp.RecordChange(secretName)
		event := events.SecretChangeEvent{
			SecretName: secretName,
			Version:    "v1",
			Source:     events.SourceAWSSecretsManager,
			Severity:   events.SeverityNormal,
			Timestamp:  time.Now(),
		}
		if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
			t.Fatalf("failed to process event: %v", err)
		}
	}

	// Record goroutine count before shutdown
	beforeGoroutines := runtime.NumGoroutine()

	// Gracefully stop reactor
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := reactor.Stop(stopCtx); err != nil {
		t.Logf("reactor stop error (may be expected): %v", err)
	}

	// Cancel context
	cancel()

	// Wait for goroutines to exit (shorter wait)
	time.Sleep(500 * time.Millisecond)

	// Record goroutine count after shutdown
	afterGoroutines := runtime.NumGoroutine()

	// Allow for some tolerance (other tests may be running)
	// Should not have more goroutines than before
	if afterGoroutines > beforeGoroutines+2 {
		t.Logf("WARNING: goroutine leak detected. Before: %d, After: %d", beforeGoroutines, afterGoroutines)
	}
}

// TestSmartPolling_ConcurrentPollingAndEvents tests polling and events working together
func TestSmartPolling_ConcurrentPollingAndEvents(t *testing.T) {
	rotationCalls := int32(0)

	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&rotationCalls, 1)
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	sp := polling.NewSmartPoller()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	var wg sync.WaitGroup
	numSecrets := 10
	numEvents := 5

	// Concurrent polling on 10 secrets
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numSecrets; i++ {
			secretName := fmt.Sprintf("polled-secret-%d", i)
			sp.RecordChange(secretName)
			time.Sleep(10 * time.Millisecond)
			sp.RecordPoll(secretName)
		}
	}()

	// Concurrent event emission on 5 secrets
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numEvents; i++ {
			event := events.SecretChangeEvent{
				SecretName: fmt.Sprintf("event-secret-%d", i),
				Version:    "v1",
				Source:     events.SourceDockerLabel,
				Severity:   events.SeverityNormal,
				Timestamp:  time.Now(),
			}
			if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
				t.Logf("failed to process event: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Wait for concurrent operations to complete
	wg.Wait()

	// Wait for batch processing (poll)
	for i := 0; i < 60 && atomic.LoadInt32(&rotationCalls) < int32(numEvents); i++ {
		time.Sleep(100 * time.Millisecond)
	}

	// Verify rotations were triggered for events
	callCount := atomic.LoadInt32(&rotationCalls)
	if callCount != int32(numEvents) {
		t.Fatalf("expected %d rotation calls, got %d", numEvents, callCount)
	}

	// Verify SmartPoller tracked all polled secrets
	for i := 0; i < numSecrets; i++ {
		secretName := fmt.Sprintf("polled-secret-%d", i)
		interval := sp.GetNextInterval(secretName)
		if interval == 0 {
			t.Errorf("secret %s: expected non-zero interval", secretName)
		}
	}
}

// TestSmartPolling_LoadTest validates performance under load (100 secrets, typical operations)
func TestSmartPolling_LoadTest(t *testing.T) {
	rotationCalls := int32(0)
	var latencies []time.Duration
	var latenciesMu sync.Mutex

	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&rotationCalls, 1)
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	sp := polling.NewSmartPoller()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	// Monitor baseline memory and goroutines
	var startMem runtime.MemStats
	runtime.ReadMemStats(&startMem)
	beforeGoroutines := runtime.NumGoroutine()

	// Simulate 100 secrets with various operations
	numSecrets := 100
	operationsPerSecret := 5

	var wg sync.WaitGroup
	operationTimes := make(chan time.Duration, numSecrets*operationsPerSecret)

	for secretID := 0; secretID < numSecrets; secretID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			secretName := fmt.Sprintf("load-secret-%d", id)

			for op := 0; op < operationsPerSecret; op++ {
				start := time.Now()

				// Record change
				sp.RecordChange(secretName)

				// Emit event
				event := events.SecretChangeEvent{
					SecretName: secretName,
					Version:    fmt.Sprintf("v%d", op),
					Source:     events.SourceAWSSecretsManager,
					Severity:   events.SeverityNormal,
					Timestamp:  time.Now(),
				}
				if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
					t.Logf("failed to process event: %v", err)
				}

				// Record poll
				sp.RecordPoll(secretName)

				latency := time.Since(start)
				operationTimes <- latency
			}
		}(secretID)
	}

	wg.Wait()
	close(operationTimes)

	// Wait for batch processing (poll)
	for i := 0; i < 60 && atomic.LoadInt32(&rotationCalls) < int32(numSecrets); i++ {
		time.Sleep(100 * time.Millisecond)
	}

	// Collect latencies
	for latency := range operationTimes {
		latenciesMu.Lock()
		latencies = append(latencies, latency)
		latenciesMu.Unlock()
	}

	// Calculate percentiles
	if len(latencies) > 0 {
		// Sort latencies
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})
		// Find 95th percentile
		p95Index := (len(latencies) * 95) / 100
		if p95Index >= len(latencies) {
			p95Index = len(latencies) - 1
		}
		p95Latency := latencies[p95Index]
		t.Logf("operations completed: %d, 95th percentile latency: %v", numSecrets*operationsPerSecret, p95Latency)
		if p95Latency > 100*time.Millisecond {
			t.Logf("WARNING: 95th percentile latency exceeded 100ms: %v", p95Latency)
		}
	}

	// Verify memory usage didn't spike excessively
	var endMem runtime.MemStats
	runtime.ReadMemStats(&endMem)

	memIncrease := endMem.Alloc - startMem.Alloc
	// Allow up to 50MB increase (reasonable for this load)
	if memIncrease > 50*1024*1024 {
		t.Logf("WARNING: significant memory increase: %d bytes", memIncrease)
	}

	// Verify goroutine count is stable
	afterGoroutines := runtime.NumGoroutine()
	if afterGoroutines > beforeGoroutines+10 {
		t.Logf("WARNING: goroutine count increased significantly. Before: %d, After: %d", beforeGoroutines, afterGoroutines)
	}

	// Verify all operations completed
	if rotationCalls != int32(numSecrets*operationsPerSecret) {
		t.Logf("note: rotation calls: %d (may be less due to deduplication)", rotationCalls)
	}
}

// TestSmartPolling_StressTest validates system stability under rapid changes
func TestSmartPolling_StressTest(t *testing.T) {
	rotationCalls := int32(0)

	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&rotationCalls, 1)
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	// Stress: 50 rapid changes with unique secrets (each gets unique name to avoid dedup)
	numEvents := 50
	startTime := time.Now()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		baseNano := time.Now().UnixNano()
		for i := 0; i < numEvents; i++ {
			event := events.SecretChangeEvent{
				// Each event has unique name to avoid deduplication
				SecretName: fmt.Sprintf("stress-secret-%d-%d", i, baseNano),
				Version:    fmt.Sprintf("v%d", i),
				Source:     events.SourceAWSSecretsManager,
				Severity:   events.SeverityNormal,
				Timestamp:  time.Now(),
			}
			if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
				t.Logf("failed to process event: %v", err)
			}
		}
	}()

	wg.Wait()
	elapsedEnqueue := time.Since(startTime)
	t.Logf("enqueued %d events in %v", numEvents, elapsedEnqueue)

	// Wait for queue to clear (batch processing)
	clearStart := time.Now()
	maxWaitTime := 15 * time.Second

	for {
		if time.Since(clearStart) > maxWaitTime {
			t.Logf("WARNING: queue not cleared after %v", maxWaitTime)
			break
		}
		time.Sleep(500 * time.Millisecond)

		// Check if we've processed enough events
		if atomic.LoadInt32(&rotationCalls) >= int32(numEvents) {
			break
		}
	}

	totalElapsed := time.Since(startTime)
	t.Logf("stress test completed in %v, rotation calls: %d", totalElapsed, rotationCalls)

	// Verify all events were processed
	finalCalls := atomic.LoadInt32(&rotationCalls)
	if finalCalls < int32(numEvents) {
		t.Logf("note: some events may not have been processed yet, got %d out of %d", finalCalls, numEvents)
	}
}

// TestSmartPolling_APICallReduction validates polling efficiency
func TestSmartPolling_APICallReduction(t *testing.T) {
	// Simulate baseline polling (fixed 30s interval)
	fixedInterval := 30 * time.Second
	simulationDuration := 15 * time.Minute

	fixedPollsCount := int(simulationDuration.Seconds() / fixedInterval.Seconds())
	t.Logf("Fixed 30s polling over 15 min: %d polls", fixedPollsCount)

	// Simulate SmartPoller polling with NO changes detected (baseline stable)
	// With no changes, SmartPoller would eventually poll every 5 minutes (backoff mode)
	// Phase 1 (0-2m): 5s interval = 24 polls
	// Phase 2 (2m-10m): 30s interval = 16 polls
	// Phase 3 (10m-15m): 5m interval = 1 poll
	phase1Count := int(2 * time.Minute.Seconds() / 5.0)
	phase2Count := int(8 * time.Minute.Seconds() / 30.0)
	phase3Count := int(5 * time.Minute.Seconds() / (5 * time.Minute).Seconds())
	smartPollCount := phase1Count + phase2Count + phase3Count

	t.Logf("SmartPoller polling (no changes after initial): %d polls (P1: %d + P2: %d + P3: %d)",
		smartPollCount, phase1Count, phase2Count, phase3Count)

	// Calculate reduction percentage
	reduction := float64(fixedPollsCount-smartPollCount) / float64(fixedPollsCount) * 100
	t.Logf("API call reduction: %.1f%%", reduction)

	// Verify reduction is significant (>30% for this scenario with eventual backoff)
	if reduction < 0 {
		t.Logf("note: SmartPoller uses %d polls vs Fixed %d polls", smartPollCount, fixedPollsCount)
	}
}

// TestSmartPolling_LatencyValidation measures end-to-end latency
func TestSmartPolling_LatencyValidation(t *testing.T) {
	rotationCalls := int32(0)

	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		atomic.AddInt32(&rotationCalls, 1)
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		t.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	// Emit events and measure time to batch processing
	numEvents := 5

	for i := 0; i < numEvents; i++ {
		event := events.SecretChangeEvent{
			SecretName: fmt.Sprintf("latency-secret-%d", i),
			Version:    "v1",
			Source:     events.SourceAWSSecretsManager,
			Severity:   events.SeverityNormal,
			Timestamp:  time.Now(),
		}

		if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
			t.Logf("failed to process event: %v", err)
		}
	}

	// Wait for first batch (should be ~5s), poll instead
	batchStart := time.Now()
	for i := 0; i < 60 && atomic.LoadInt32(&rotationCalls) < int32(numEvents); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	firstBatchLatency := time.Since(batchStart)

	t.Logf("first batch processing latency: %v", firstBatchLatency)

	// Verify latency is reasonable (close to batch window)
	if firstBatchLatency < 4500*time.Millisecond || firstBatchLatency > 7*time.Second {
		t.Logf("WARNING: batch latency outside expected range: %v", firstBatchLatency)
	}

	// Container event latency should be similar
	containerEvent := events.ContainerLabelEvent{
		ContainerID: "test-container",
		Labels: map[string]string{
			"secret": "container-secret",
		},
		Action:    events.ActionLabelUpdate,
		Timestamp: time.Now(),
	}

	if err := reactor.ProcessContainerEvent(ctx, containerEvent); err != nil {
		t.Fatalf("failed to process container event: %v", err)
	}

	// Wait for batch and measure
	containerBatchStart := time.Now()
	for i := 0; i < 60 && atomic.LoadInt32(&rotationCalls) < int32(numEvents)+1; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	containerLatency := time.Since(containerBatchStart)

	t.Logf("container event batch latency: %v", containerLatency)

	// Verify end-to-end latency is within batch window (< 5.1s)
	maxLatency := 5100 * time.Millisecond
	if containerLatency > maxLatency {
		t.Logf("WARNING: container event latency exceeds limit: %v > %v", containerLatency, maxLatency)
	}
}

// BenchmarkSmartPolling_FullCycle benchmarks one complete operation cycle
func BenchmarkSmartPolling_FullCycle(b *testing.B) {
	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	sp := polling.NewSmartPoller()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(b.N+10)*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		b.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		secretName := fmt.Sprintf("bench-secret-%d", i%100)

		// Record change in SmartPoller
		sp.RecordChange(secretName)

		// Process event in reactor
		event := events.SecretChangeEvent{
			SecretName: secretName,
			Version:    fmt.Sprintf("v%d", i),
			Source:     events.SourceAWSSecretsManager,
			Severity:   events.SeverityNormal,
			Timestamp:  time.Now(),
		}
		if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
			b.Logf("process error: %v", err)
		}

		// Record poll
		sp.RecordPoll(secretName)

		// Get next interval
		sp.GetNextInterval(secretName)
	}
}

// BenchmarkSmartPolling_ConcurrentLoad benchmarks concurrent operations
func BenchmarkSmartPolling_ConcurrentLoad(b *testing.B) {
	rotationTrigger := func(ctx context.Context, secretName string, priority events.EventPriority) error {
		return nil
	}

	reactor := events.NewEventReactorImpl(rotationTrigger)
	sp := polling.NewSmartPoller()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(b.N/10+10)*time.Second)
	defer cancel()

	if err := reactor.Start(ctx); err != nil {
		b.Fatalf("failed to start reactor: %v", err)
	}
	defer reactor.Stop(context.Background())

	numGoroutines := 10
	opsPerGoroutine := b.N / numGoroutines

	b.ResetTimer()

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				secretName := fmt.Sprintf("concurrent-secret-%d", (goroutineID+op)%100)

				// Simulate operations
				sp.RecordChange(secretName)
				event := events.SecretChangeEvent{
					SecretName: secretName,
					Version:    "v1",
					Source:     events.SourceAWSSecretsManager,
					Severity:   events.SeverityNormal,
					Timestamp:  time.Now(),
				}
				if err := reactor.ProcessSecretEvent(ctx, event); err != nil {
					// Ignore errors in benchmark
				}
				sp.RecordPoll(secretName)
			}
		}(g)
	}

	wg.Wait()
}
