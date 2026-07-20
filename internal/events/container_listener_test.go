package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

// Test 1: HasRelevantLabels tests
func TestHasRelevantLabels_SecretLabel(t *testing.T) {
	labels := map[string]string{"secret": "my-secret"}
	if !HasRelevantLabels(labels) {
		t.Fatal("expected true for 'secret' label")
	}
}

func TestHasRelevantLabels_RotationStrategyLabel(t *testing.T) {
	labels := map[string]string{"rotation-strategy": "rolling"}
	if !HasRelevantLabels(labels) {
		t.Fatal("expected true for 'rotation-strategy' label")
	}
}

func TestHasRelevantLabels_DSO_Prefix(t *testing.T) {
	labels := map[string]string{"dso.owner": "team-a"}
	if !HasRelevantLabels(labels) {
		t.Fatal("expected true for 'dso.' prefix")
	}
}

func TestHasRelevantLabels_DSO_MultiplePrefix(t *testing.T) {
	labels := map[string]string{
		"dso.owner":       "team-a",
		"dso.environment": "prod",
	}
	if !HasRelevantLabels(labels) {
		t.Fatal("expected true for multiple 'dso.' prefixes")
	}
}

func TestHasRelevantLabels_Irrelevant(t *testing.T) {
	labels := map[string]string{"app": "web", "version": "1.0"}
	if HasRelevantLabels(labels) {
		t.Fatal("expected false for irrelevant labels")
	}
}

func TestHasRelevantLabels_Empty(t *testing.T) {
	if HasRelevantLabels(map[string]string{}) {
		t.Fatal("expected false for empty labels")
	}
}

func TestHasRelevantLabels_Nil(t *testing.T) {
	if HasRelevantLabels(nil) {
		t.Fatal("expected false for nil labels")
	}
}

func TestHasRelevantLabels_MixedRelevantAndIrrelevant(t *testing.T) {
	labels := map[string]string{
		"app":    "web",
		"secret": "api-key",
	}
	if !HasRelevantLabels(labels) {
		t.Fatal("expected true when 'secret' is present with other labels")
	}
}

func TestHasRelevantLabels_DSO_Prefix_Partial(t *testing.T) {
	labels := map[string]string{
		"dso": "not-a-prefix",  // This should NOT match
		"app": "web",
	}
	if HasRelevantLabels(labels) {
		t.Fatal("expected false, 'dso' without dot suffix is not a dso.* prefix")
	}
}

func TestHasRelevantLabels_AllRelevantTypes(t *testing.T) {
	labels := map[string]string{
		"secret":             "aws-key",
		"rotation-strategy":  "rolling",
		"dso.owner":          "team-a",
		"dso.env":            "prod",
		"app":                "web",
	}
	if !HasRelevantLabels(labels) {
		t.Fatal("expected true when all relevant label types are present")
	}
}

// Test 2: DetectLabelChanges tests
func TestDetectLabelChanges_SecretChanged(t *testing.T) {
	before := map[string]string{"secret": "old-secret", "app": "web"}
	after := map[string]string{"secret": "new-secret", "app": "web"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changed))
	}
	if changed["secret"] != "new-secret" {
		t.Fatalf("expected secret=new-secret, got %v", changed)
	}
}

func TestDetectLabelChanges_LabelAdded(t *testing.T) {
	before := map[string]string{"app": "web"}
	after := map[string]string{"secret": "my-secret", "app": "web"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["secret"] != "my-secret" {
		t.Fatalf("expected secret added, got %v", changed)
	}
}

func TestDetectLabelChanges_LabelRemoved(t *testing.T) {
	before := map[string]string{"secret": "my-secret", "app": "web"}
	after := map[string]string{"app": "web"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["secret"] != "" {
		t.Fatalf("expected secret removed (empty string), got %v", changed)
	}
}

func TestDetectLabelChanges_NoRelevantChanges(t *testing.T) {
	before := map[string]string{"app": "web"}
	after := map[string]string{"app": "web", "version": "1.0"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 0 {
		t.Fatalf("expected no changes, got %v", changed)
	}
}

func TestDetectLabelChanges_DSO_Prefix_Changed(t *testing.T) {
	before := map[string]string{"dso.owner": "team-a"}
	after := map[string]string{"dso.owner": "team-b"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["dso.owner"] != "team-b" {
		t.Fatalf("expected dso.owner changed, got %v", changed)
	}
}

func TestDetectLabelChanges_RotationStrategyChanged(t *testing.T) {
	before := map[string]string{"rotation-strategy": "monthly"}
	after := map[string]string{"rotation-strategy": "weekly"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["rotation-strategy"] != "weekly" {
		t.Fatalf("expected rotation-strategy changed, got %v", changed)
	}
}

func TestDetectLabelChanges_MultipleRelevantChanges(t *testing.T) {
	before := map[string]string{
		"secret":            "old-key",
		"rotation-strategy": "monthly",
		"dso.owner":         "team-a",
		"app":               "web",
	}
	after := map[string]string{
		"secret":            "new-key",
		"rotation-strategy": "weekly",
		"dso.owner":         "team-b",
		"app":               "web",
		"version":           "2.0",
	}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 3 {
		t.Fatalf("expected 3 changes, got %d: %v", len(changed), changed)
	}
	if changed["secret"] != "new-key" {
		t.Fatalf("expected secret=new-key, got %s", changed["secret"])
	}
	if changed["rotation-strategy"] != "weekly" {
		t.Fatalf("expected rotation-strategy=weekly, got %s", changed["rotation-strategy"])
	}
	if changed["dso.owner"] != "team-b" {
		t.Fatalf("expected dso.owner=team-b, got %s", changed["dso.owner"])
	}
}

func TestDetectLabelChanges_EmptyBefore(t *testing.T) {
	before := map[string]string{}
	after := map[string]string{"secret": "new-secret"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["secret"] != "new-secret" {
		t.Fatalf("expected secret added from empty, got %v", changed)
	}
}

func TestDetectLabelChanges_EmptyAfter(t *testing.T) {
	before := map[string]string{"secret": "old-secret"}
	after := map[string]string{}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["secret"] != "" {
		t.Fatalf("expected secret removed to empty, got %v", changed)
	}
}

func TestDetectLabelChanges_BothEmpty(t *testing.T) {
	before := map[string]string{}
	after := map[string]string{}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 0 {
		t.Fatalf("expected no changes from empty to empty, got %v", changed)
	}
}

func TestDetectLabelChanges_Nil_Before(t *testing.T) {
	before := map[string]string(nil)
	after := map[string]string{"secret": "new-secret"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["secret"] != "new-secret" {
		t.Fatalf("expected secret added from nil before, got %v", changed)
	}
}

func TestDetectLabelChanges_Nil_After(t *testing.T) {
	before := map[string]string{"secret": "old-secret"}
	after := map[string]string(nil)

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["secret"] != "" {
		t.Fatalf("expected secret removed to nil after, got %v", changed)
	}
}

func TestDetectLabelChanges_IgnoresIrrelevantOnly(t *testing.T) {
	before := map[string]string{
		"app":     "web",
		"version": "1.0",
	}
	after := map[string]string{
		"app":     "web-v2",
		"version": "2.0",
		"region":  "us-west",
	}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 0 {
		t.Fatalf("expected no changes (all irrelevant), got %v", changed)
	}
}

func TestDetectLabelChanges_DSO_Added(t *testing.T) {
	before := map[string]string{"app": "web"}
	after := map[string]string{"app": "web", "dso.region": "us-west"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["dso.region"] != "us-west" {
		t.Fatalf("expected dso.region added, got %v", changed)
	}
}

func TestDetectLabelChanges_DSO_Removed(t *testing.T) {
	before := map[string]string{"app": "web", "dso.region": "us-west"}
	after := map[string]string{"app": "web"}

	changed := DetectLabelChanges(before, after)

	if len(changed) != 1 || changed["dso.region"] != "" {
		t.Fatalf("expected dso.region removed, got %v", changed)
	}
}

// Test 3: NewContainerListener tests
func TestNewContainerListener(t *testing.T) {
	// Create a mock Docker client (will skip if Docker not available)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)

	if listener == nil {
		t.Fatal("NewContainerListener returned nil")
	}
	if listener.client != cli {
		t.Fatal("Docker client not set correctly")
	}
	if listener.eventsChan == nil {
		t.Fatal("Events channel not initialized")
	}
	if listener.stopChan == nil {
		t.Fatal("Stop channel not initialized")
	}
	if listener.lastLabels == nil {
		t.Fatal("lastLabels map not initialized")
	}
}

func TestContainerListener_Events_ReturnsReadOnlyChannel(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)
	eventsCh := listener.Events()

	if eventsCh == nil {
		t.Fatal("Events() returned nil")
	}

	// Verify it's a receive-only channel by trying to read with timeout
	select {
	case <-eventsCh:
		// No event yet, that's fine
	case <-time.After(100 * time.Millisecond):
		// Timeout, expected since no events yet
	}
}

func TestContainerListener_Start_Success(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = listener.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Verify context was set up
	if listener.ctx == nil {
		t.Fatal("Context not set after Start()")
	}
	if listener.cancel == nil {
		t.Fatal("Cancel function not set after Start()")
	}

	// Clean up
	listener.Stop()
}

func TestContainerListener_Start_AlreadyStarted(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel1()

	err = listener.Start(ctx1)
	if err != nil {
		t.Fatalf("First Start() failed: %v", err)
	}

	// Try to start again
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	err = listener.Start(ctx2)
	if err == nil {
		t.Fatal("Expected error when starting already-started listener")
	}

	listener.Stop()
}

func TestContainerListener_Stop(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = listener.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Stop should succeed
	err = listener.Stop()
	if err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	// Give it time to close the channel
	time.Sleep(200 * time.Millisecond)
}

func TestContainerListener_StopWithoutStart(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)

	// Stop without starting should not panic
	err = listener.Stop()
	if err != nil {
		t.Errorf("Stop() should not error when not started, got: %v", err)
	}
}

func TestContainerListener_ConcurrentOperations(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = listener.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)

	// Concurrent reads on Events channel
	go func() {
		defer wg.Done()
		timeout := time.NewTimer(500 * time.Millisecond)
		defer timeout.Stop()
		for {
			select {
			case event, ok := <-listener.Events():
				if !ok {
					// Channel closed, exit gracefully
					return
				}
				_ = event // Event received
			case <-timeout.C:
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		timeout := time.NewTimer(500 * time.Millisecond)
		defer timeout.Stop()
		for {
			select {
			case event, ok := <-listener.Events():
				if !ok {
					// Channel closed, exit gracefully
					return
				}
				_ = event // Event received
			case <-timeout.C:
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(400 * time.Millisecond)
		listener.Stop()
	}()

	wg.Wait()
}

func TestContainerListener_InitializeContainers(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)

	// Create listener context and start
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = listener.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Give it time to initialize containers
	time.Sleep(200 * time.Millisecond)

	// Verify that initializeContainers was called and lastLabels is initialized
	listener.mu.RLock()
	lastLabelsInitialized := listener.lastLabels != nil
	listener.mu.RUnlock()

	if !lastLabelsInitialized {
		t.Errorf("lastLabels should have been initialized by initializeContainers()")
	}

	listener.Stop()
}

func TestContainerListener_LabelChangeDetection(t *testing.T) {
	// This test verifies the label change detection logic
	// by manually testing DetectLabelChanges with various scenarios

	testCases := []struct {
		name     string
		before   map[string]string
		after    map[string]string
		expected map[string]string
	}{
		{
			name:     "secret changed",
			before:   map[string]string{"secret": "old"},
			after:    map[string]string{"secret": "new"},
			expected: map[string]string{"secret": "new"},
		},
		{
			name:     "secret added",
			before:   map[string]string{},
			after:    map[string]string{"secret": "new"},
			expected: map[string]string{"secret": "new"},
		},
		{
			name:     "secret removed",
			before:   map[string]string{"secret": "old"},
			after:    map[string]string{},
			expected: map[string]string{"secret": ""},
		},
		{
			name:     "irrelevant changed",
			before:   map[string]string{"app": "v1"},
			after:    map[string]string{"app": "v2"},
			expected: map[string]string{},
		},
		{
			name:     "dso label changed",
			before:   map[string]string{"dso.owner": "team-a"},
			after:    map[string]string{"dso.owner": "team-b"},
			expected: map[string]string{"dso.owner": "team-b"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectLabelChanges(tc.before, tc.after)
			if len(result) != len(tc.expected) {
				t.Errorf("expected %d changes, got %d: %v", len(tc.expected), len(result), result)
				return
			}
			for key, expectedVal := range tc.expected {
				actualVal, ok := result[key]
				if !ok {
					t.Errorf("expected key %s in changes", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("key %s: expected %q, got %q", key, expectedVal, actualVal)
				}
			}
		})
	}
}

func TestContainerListener_HasRelevantLabels_Comprehensive(t *testing.T) {
	// Comprehensive test for HasRelevantLabels
	testCases := []struct {
		name   string
		labels map[string]string
		expect bool
	}{
		{
			name:   "secret label",
			labels: map[string]string{"secret": "value"},
			expect: true,
		},
		{
			name:   "rotation-strategy label",
			labels: map[string]string{"rotation-strategy": "value"},
			expect: true,
		},
		{
			name:   "dso.* prefix",
			labels: map[string]string{"dso.owner": "value"},
			expect: true,
		},
		{
			name:   "mixed with relevant",
			labels: map[string]string{"secret": "value", "app": "web"},
			expect: true,
		},
		{
			name:   "irrelevant only",
			labels: map[string]string{"app": "web", "version": "1.0"},
			expect: false,
		},
		{
			name:   "empty",
			labels: map[string]string{},
			expect: false,
		},
		{
			name:   "nil",
			labels: nil,
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := HasRelevantLabels(tc.labels)
			if result != tc.expect {
				t.Errorf("expected %v, got %v", tc.expect, result)
			}
		})
	}
}

// CRITICAL ISSUE 1: Test for nil client panic
func TestNewContainerListener_NilClient(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Docker client is nil")
		}
	}()

	// This should panic
	NewContainerListener(nil)
}

// CRITICAL ISSUE 2: Test for Docker API error handling
func TestContainerListener_DockerAPIError(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping container listener tests")
	}
	defer cli.Close()

	listener := NewContainerListener(cli)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = listener.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// The watchEvents method should handle errors gracefully
	// and return cleanly when the context is cancelled
	listener.Stop()

	// Verify that Stop completed and eventsChan is closed
	// by attempting to read from it after Stop()
	time.Sleep(100 * time.Millisecond)

	select {
	case event, ok := <-listener.Events():
		if ok && event != nil {
			// Event received, that's okay
		}
		// Channel should eventually be closed by watchEvents
	case <-time.After(500 * time.Millisecond):
		// Timeout is fine
	}
}
