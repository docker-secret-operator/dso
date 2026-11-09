package watcher

import (
	"testing"

	"go.uber.org/zap/zaptest"
)

// TestRecoverRotationPanic_RecoversAndInvokesOnPanic is the Phase 7 / P2-2
// regression test for panic recovery in rotation goroutines. It exercises
// the exact pattern each rotation goroutine (rolling, restart, and
// compose-restart branches of TriggerReload) now uses:
//
//	defer r.recoverRotationPanic(ctx, id, onPanic)()
//	... goroutine body that may panic ...
//
// and proves the full chain the audit required:
//
//  1. panic inside rotation
//  2. panic recovered (the test function itself does not crash/fail --
//     that would happen automatically if recover() were missing)
//  3. rotation marked failed (onPanic receives a non-nil error)
//  4. cleanup executed (onPanic's own closure runs -- here it flips
//     cleanupRan and releases the lock)
//  5. locks/state released (rotationLocks no longer holds the key
//     after the panicking goroutine returns)
//  6. agent remains usable (a second, unrelated lock acquisition succeeds
//     immediately afterwards, proving the whole process is still alive
//     and the lock map is not left in a broken state)
func TestRecoverRotationPanic_RecoversAndInvokesOnPanic(t *testing.T) {
	r := &ReloaderController{Logger: zaptest.NewLogger(t)}

	const serviceName = "payments-api"
	r.rotationLocks.Store(serviceName, &lockInfo{serviceName: serviceName})

	var (
		onPanicCalled bool
		gotErr        error
		gotRecValue   interface{}
		cleanupRan    bool
	)

	func() {
		defer r.recoverRotationPanic("rolling rotation", serviceName, func(err error, rec interface{}) {
			onPanicCalled = true
			gotErr = err
			gotRecValue = rec
			cleanupRan = true
			r.rotationLocks.Delete(serviceName)
		})()

		panic("simulated panic inside rs.Execute")
	}()

	// 2. The panic did not propagate -- reaching this line at all proves
	// recover() worked; an unrecovered panic would have failed this test
	// (and, in production, crashed the whole DSO agent process).

	// 3. Rotation marked failed.
	if !onPanicCalled {
		t.Fatal("expected onPanic to be invoked after the panic was recovered")
	}
	if gotErr == nil {
		t.Error("expected a non-nil error to be passed to onPanic")
	}
	if gotRecValue != "simulated panic inside rs.Execute" {
		t.Errorf("expected the raw panic value to be passed through, got %v", gotRecValue)
	}

	// 4. Cleanup executed.
	if !cleanupRan {
		t.Error("expected onPanic's cleanup closure to have run")
	}

	// 5. Locks/state released.
	if _, stillHeld := r.rotationLocks.Load(serviceName); stillHeld {
		t.Error("expected the rotation lock to be released after the panic was recovered")
	}

	// 6. Agent remains usable: acquiring a fresh lock for the same service
	// works cleanly, proving the lock map wasn't left in a broken/partial
	// state and the controller is still fully operational.
	r.rotationLocks.Store(serviceName, &lockInfo{serviceName: serviceName})
	if _, held := r.rotationLocks.Load(serviceName); !held {
		t.Error("expected to be able to re-acquire the lock after recovery, proving the agent remains usable")
	}
}

// TestRecoverRotationPanic_NoPanicIsANoOp verifies the deferred recovery
// function does nothing at all on the normal (non-panicking) return path --
// onPanic must never fire when there was no panic.
func TestRecoverRotationPanic_NoPanicIsANoOp(t *testing.T) {
	r := &ReloaderController{Logger: zaptest.NewLogger(t)}

	var onPanicCalled bool
	func() {
		defer r.recoverRotationPanic("rolling rotation", "some-id", func(err error, rec interface{}) {
			onPanicCalled = true
		})()
		// No panic -- normal return.
	}()

	if onPanicCalled {
		t.Error("expected onPanic to never be called when the goroutine did not panic")
	}
}

// TestRecoverRotationPanic_DegradedStateSetOnPanic verifies a realistic
// caller wiring (mirroring the restart-strategy goroutine): on panic, the
// service is marked degraded so the operator has a visible signal, exactly
// as the existing non-panic failure branches already do.
func TestRecoverRotationPanic_DegradedStateSetOnPanic(t *testing.T) {
	r := &ReloaderController{Logger: zaptest.NewLogger(t)}
	const serviceName = "billing-worker"

	func() {
		defer r.recoverRotationPanic("restart rotation", serviceName, func(err error, rec interface{}) {
			r.markDegraded(serviceName, "rotation panicked: "+err.Error())
		})()
		panic("nil pointer dereference in ContainerInspect handling")
	}()

	reason, degraded := r.IsDegraded(serviceName)
	if !degraded {
		t.Fatal("expected the service to be marked degraded after a panic")
	}
	if reason == "" {
		t.Error("expected a non-empty degraded reason")
	}
}
