package watcher

// Docker-free stress/race tests that lived in the quarantined test/integration
// suite (M0-11). They need no daemon, so they are ordinary unit tests: no build tag.
//
// Origin: test/integration/stress_test.go (EventDebouncer_RapidFire,
// EventQueue_Backpressure) and race_test.go (DebounceWindow_ConcurrentUpdates).
// The race-test original exercised a private COPY of the debouncer defined in the
// test file; this version exercises the real EventDebouncer.

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Original: TestStress_EventDebouncer_RapidFire (assertion unchanged: at least 90%
// of the ~80% intended duplicates are detected).
func TestStress_EventDebouncer_RapidFire(t *testing.T) {
	debouncer := NewEventDebouncer(100 * time.Millisecond)
	defer debouncer.Stop()

	numEvents := 10000
	eventDuplicateRatio := 0.8
	var duplicatesDetected, freshDetected int

	// 80% of events are drawn from a small repeating pool, the rest are unique
	// (TEST-1: the earlier modulus condition produced only two duplicates).
	duplicatePoolSize := 10
	duplicateCutoff := int(eventDuplicateRatio * 10)
	for i := 0; i < numEvents; i++ {
		var eventID string
		if i%10 < duplicateCutoff {
			eventID = fmt.Sprintf("container-%d", i%duplicatePoolSize)
		} else {
			eventID = fmt.Sprintf("container-unique-%d", i)
		}
		if debouncer.CheckAndRecord(eventID) {
			freshDetected++
		} else {
			duplicatesDetected++
		}
	}
	t.Logf("rapid-fire: %d fresh, %d duplicates out of %d", freshDetected, duplicatesDetected, numEvents)

	expectedDuplicates := int(float64(numEvents) * eventDuplicateRatio * 0.9)
	if duplicatesDetected < expectedDuplicates {
		t.Errorf("deduplication is weaker than expected: %d duplicates detected, want at least %d",
			duplicatesDetected, expectedDuplicates)
	}
}

// Original: TestRace_DebounceWindow_ConcurrentUpdates (assertions unchanged: first
// record is fresh; the window is NOT extended by duplicates, so the event is fresh
// again just past the original window). Added: duplicates inside the window must
// all be reported as duplicates.
func TestRace_DebounceWindow_ConcurrentUpdates(t *testing.T) {
	const window = 100 * time.Millisecond
	debouncer := NewEventDebouncer(window)
	defer debouncer.Stop()

	eventID := "test-event"
	if !debouncer.CheckAndRecord(eventID) {
		t.Error("First record should be fresh")
	}
	initialTime := time.Now()

	for i := 0; i < 100; i++ {
		if debouncer.CheckAndRecord(eventID) && time.Since(initialTime) < window {
			t.Errorf("duplicate %d inside the window was reported fresh", i)
		}
	}

	time.Sleep(110 * time.Millisecond)

	if !debouncer.CheckAndRecord(eventID) {
		t.Errorf("Event should be fresh after 110ms, but got duplicate at elapsed time %v", time.Since(initialTime))
	}
}

// Original: TestStress_EventQueue_Backpressure was a placeholder that asserted
// nothing ("would require full integration setup"). Replaced by a real test of the
// bounded queue that provides rotation backpressure (WorkerPool): when workers are
// busy and the queue is full, Submit blocks instead of dropping or spawning
// goroutines, it proceeds once capacity frees, and Stop releases blocked submitters
// with a refusal.
func TestStress_EventQueue_Backpressure(t *testing.T) {
	wp := NewWorkerPool(1, 2)
	wp.Start()
	defer wp.Stop()

	release := make(chan struct{})
	running := make(chan struct{})
	var ran int
	var mu sync.Mutex
	job := func() { mu.Lock(); ran++; mu.Unlock() }

	// Occupy the single worker, then fill the 2-slot queue.
	if !wp.Submit(func() { close(running); <-release; job() }) {
		t.Fatal("submit refused")
	}
	<-running
	for i := 0; i < 2; i++ {
		if !wp.Submit(job) {
			t.Fatal("queue submit refused")
		}
	}

	// The next submit must BLOCK (backpressure).
	accepted := make(chan bool, 1)
	go func() { accepted <- wp.Submit(job) }()
	select {
	case <-accepted:
		t.Fatal("Submit returned while the worker was busy and the queue full: no backpressure")
	case <-time.After(200 * time.Millisecond):
	}

	close(release) // free the worker; queue drains; the blocked submit is admitted
	select {
	case ok := <-accepted:
		if !ok {
			t.Fatal("blocked submit was refused instead of admitted once capacity freed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked submit never proceeded after capacity freed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := ran
		mu.Unlock()
		if n == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of 4 accepted jobs ran: jobs were dropped", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Added with the backpressure test: Stop must release a submitter blocked on a full
// queue (returning false) rather than deadlock shutdown.
func TestStress_EventQueue_StopReleasesBlockedSubmitter(t *testing.T) {
	wp := NewWorkerPool(1, 0)
	wp.Start()
	hold := make(chan struct{})
	started := make(chan struct{})
	wp.Submit(func() { close(started); <-hold })
	<-started

	result := make(chan bool, 1)
	go func() { result <- wp.Submit(func() {}) }()
	time.Sleep(100 * time.Millisecond)

	stopped := make(chan struct{})
	go func() { wp.Stop(); close(stopped) }()
	select {
	case ok := <-result:
		if ok {
			t.Error("a submit blocked during Stop was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked submitter was not released by Stop")
	}
	close(hold) // let the in-flight job finish so Stop's wait completes
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return")
	}
}
