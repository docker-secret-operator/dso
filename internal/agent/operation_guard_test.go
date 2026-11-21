package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// M0-04 finding 3.3: a service with an unresolved operation must reconcile or stay
// BLOCKED from competing mutation, whichever path dispatches; legitimate
// initialisation rotation (clean start, restart after success) is NOT removed.
//
// First-poll dispatch, traced: StartPolling fires a timer(0) poll; the engine keeps
// no persisted secret baseline (hashes are in memory, no secret-derived value is ever
// persisted), so the first observation of every secret always dispatches. That is
// intentional (it also catches rotations that happened while the agent was down) and
// is kept. What was missing is that nothing consulted the journal or the
// (in-memory, therefore empty after a restart) service locks first. StartAll already
// ran recovery synchronously BEFORE starting pollers, but the IPC socket, the REST
// API and the watcher were live before StartAll, and a service that recovery left
// critical_error was still rotated by the very first poll.

func sigEvents(rid string, trs ...string) []rotation.JournalEvent {
	return evs(rid, "signal", trs...)
}

func blockedNow(te *TriggerEngine, keys ...string) bool {
	_, b := te.guard.RotationBlocked(keys...)
	return b
}

func TestGuard_CleanStartup_BlockedOnlyUntilRecoverySettles(t *testing.T) {
	d := newFakeDaemon(healthy("orig", "svc"))
	s := boot(t, d, t.TempDir())
	if !blockedNow(s.te, "svc") {
		t.Fatal("first-poll/IPC/REST dispatch must not run before recovery settled")
	}
	_ = s.te.StartAll()
	if blockedNow(s.te, "svc", "orig") {
		t.Fatal("a clean startup must still allow the legitimate initialisation rotation")
	}
}

func TestGuard_RestartAfterSuccessfulRotation_InitialisationRotationStillAllowed(t *testing.T) {
	d := newFakeDaemon(healthy("orig", "svc"))
	es := evs("rot-1", "rolling", rotation.TrIntentCreateCandidate, rotation.TrCandidateReady, rotation.TrIntentSwap, rotation.TrSwapCommitted, rotation.TrIntentCleanup, rotation.TrCompleted)
	s := boot(t, d, journalOf(t, es...))
	_ = s.te.StartAll()
	if blockedNow(s.te, "svc", "orig") {
		t.Fatal("a completed rotation must not block the next one")
	}
}

func TestGuard_UnresolvedRollingOperation(t *testing.T) {
	// Recovery CAN resolve it (original healthy, candidate retired): released.
	t.Run("recovery resolves it", func(t *testing.T) {
		d := newFakeDaemon(healthy("orig", "svc"), artifact("cand", "svc_dso_new_1"))
		es := evs("rot-1", "rolling", rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated)
		s := boot(t, d, journalOf(t, es...))
		if !blockedNow(s.te, "svc") {
			t.Fatal("blocked before recovery")
		}
		s.te.guard.armed.Store(false) // isolate the JOURNAL rule from the startup gate
		if !blockedNow(s.te, "svc") {
			t.Fatal("an inherited in_progress record must block on its own, before recovery ran")
		}
		_ = s.te.StartAll()
		if blockedNow(s.te, "svc") {
			t.Fatalf("recovered service stays blocked: %v", s.te.StateTracker.snapshotRecords()[0].Status)
		}
	})
	// Recovery CANNOT resolve it (original gone, nothing holds the name): stays blocked.
	t.Run("recovery cannot resolve it", func(t *testing.T) {
		d := newFakeDaemon(artifact("cand", "svc_dso_new_1"))
		es := evs("rot-1", "rolling", rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated)
		s := boot(t, d, journalOf(t, es...))
		_ = s.te.StartAll()
		if s.record().Status != "critical_error" {
			t.Fatalf("status %s", s.record().Status)
		}
		if !blockedNow(s.te, "svc") || !blockedNow(s.te, "orig") || !blockedNow(s.te, "cand") {
			t.Fatal("unresolved operation must block by service name and by original/candidate ID")
		}
		if blockedNow(s.te, "unrelated") {
			t.Fatal("an unrelated service must not be blocked")
		}
	})
}

func TestGuard_PartiallyPublishedSignalOperation(t *testing.T) {
	d := newFakeDaemon(healthy("orig", "svc"))
	es := sigEvents("sig-1", rotation.TrIntentSignalPublish)
	s := boot(t, d, journalOf(t, es...))
	_ = s.te.StartAll()
	if s.record().Status != "critical_error" {
		t.Fatalf("status %s", s.record().Status)
	}
	if !blockedNow(s.te, "svc") {
		t.Fatal("a partially published signal rotation must block a competing rotation (no blind re-delivery)")
	}
	// ...across further restarts too: it never becomes 'successful' by restarting.
	s2 := boot(t, d, s.dir)
	_ = s2.te.StartAll()
	if !blockedNow(s2.te, "svc") {
		t.Fatal("still blocked after another restart")
	}
}

func TestGuard_InterruptedRestartOperation(t *testing.T) {
	// The original was renamed away and stopped; no replacement exists.
	stopped := healthy("orig", "svc_old_1700000000")
	stopped.running = false
	d := newFakeDaemon(stopped)
	es := evs("rot-1", "restart", rotation.TrIntentRenameOriginal, rotation.TrIntentStopOriginal, rotation.TrOriginalStopped)
	s := boot(t, d, journalOf(t, es...))
	_ = s.te.StartAll()
	if len(s.d.removedSorted()) != 0 {
		t.Fatalf("the only recoverable workload was removed: %v", s.d.removed)
	}
	if s.record().Status != "critical_error" || !blockedNow(s.te, "svc") {
		t.Fatalf("status %s blocked=%v: an interrupted restart that recovery cannot settle must block", s.record().Status, blockedNow(s.te, "svc"))
	}
}

func TestGuard_CorruptJournalBlocksEverything(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rotations.json"), []byte("\x00garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := boot(t, newFakeDaemon(healthy("orig", "svc")), dir)
	_ = s.te.StartAll()
	if why, b := s.te.guard.RotationBlocked("svc"); !b || why == "" {
		t.Fatal("an unreadable journal must block rotation with a reason")
	}
	// The unreadable file is preserved as evidence; a restart after the operator
	// verified the workloads starts clean.
	matches, _ := filepath.Glob(filepath.Join(dir, "rotations.json.corrupt-*"))
	if len(matches) != 1 {
		t.Fatalf("corrupt state not preserved: %v", matches)
	}
	s2 := boot(t, newFakeDaemon(healthy("orig", "svc")), dir)
	_ = s2.te.StartAll()
	if blockedNow(s2.te, "svc") {
		t.Fatal("after the operator's restart the quarantined file is gone and rotation resumes")
	}
}

// An ordinary failed attempt of the RUNNING process (rollback_required: the strategy
// already restored the previous workload) must not block the retry.
func TestGuard_OrdinaryFailedAttemptDoesNotBlockRetry(t *testing.T) {
	st, _ := NewStateTracker(t.TempDir(), zap.NewNop())
	if err := st.StartRotationWithID("rot-1", "vault", "db", "orig", ""); err != nil {
		t.Fatal(err)
	}
	g := newJournalGuard(st)
	if _, b := g.RotationBlocked("orig"); b {
		t.Fatal("an in-flight record of THIS process is covered by the service lock, not the guard")
	}
	if err := st.MarkRollback("vault", "db", "orig"); err != nil {
		t.Fatal(err)
	}
	if _, b := g.RotationBlocked("orig"); b {
		t.Fatal("rollback_required (ordinary failure, previous workload restored) must not block the retry")
	}
}

// The engine wires the guard into the reloader, so every TriggerReload (poll, webhook,
// IPC, REST, watcher) sees it.
func TestNewTriggerEngine_WiresGuardIntoReloader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	rw, err := watcher.NewReloaderController(logger)
	if err != nil {
		t.Skipf("reloader needs a docker client environment: %v", err)
	}
	defer func() { _ = rw.Close() }()
	te, err := NewTriggerEngineWithDirs(nil, nil, rw, logger, &config.Config{}, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer te.Stop()
	if rw.Guard == nil {
		t.Fatal("reloader has no OperationGuard")
	}
	te.BeginStartup()
	if _, b := rw.Guard.RotationBlocked("x"); !b {
		t.Fatal("startup gate not visible through the reloader's guard")
	}
	// A nil Docker client skips recovery but must still release the gate (no deadlock).
	_ = te.StartAll()
	if _, b := rw.Guard.RotationBlocked("x"); b {
		t.Fatal("gate must be released after StartAll even when recovery was skipped")
	}
}

// Concurrent watcher/scheduled/API readers of the guard while recovery and journal
// writers run: no data race (-race) and the final state is consistent.
func TestGuard_ConcurrentDispatchersDuringRecoveryAndJournalWrites(t *testing.T) {
	d := newFakeDaemon(healthy("orig", "svc"), artifact("cand", "svc_dso_new_1"))
	s := boot(t, d, journalOf(t, evs("rot-1", "rolling", rotation.TrIntentCreateCandidate)...))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					s.te.guard.RotationBlocked("svc", "orig", "cand")
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = s.te.StateTracker.Record(context.Background(), rotation.JournalEvent{RotationID: "rot-live", Strategy: "rolling", ServiceID: "other", Transition: rotation.TrIntentCreateCandidate})
		}
	}()
	_ = s.te.StartAll()
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
	if blockedNow(s.te, "svc") {
		t.Fatal("svc was recovered and must be released")
	}
}
