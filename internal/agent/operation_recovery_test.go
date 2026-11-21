package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

func recordAll(t *testing.T, st *StateTracker, evs ...rotation.JournalEvent) {
	t.Helper()
	for _, e := range evs {
		if err := st.Record(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
}

// reload simulates the agent restarting: a fresh tracker over the same directory.
func reloadRec(t *testing.T, f *recFixture, dir string) *recFixture {
	t.Helper()
	logger := zaptest.NewLogger(t)
	st2, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	ar := NewAutomaticRecovery(f.ar.cli, logger, st2)
	ar.OwnerID = me
	ar.Timings = &recoveryTimings{healthWait: 0, minStableRunning: 30 * time.Second}
	return &recFixture{t: t, d: f.d, ar: ar, st: st2}
}

// Signal rotation: an interrupted operation must stay UNCERTAIN across a restart,
// nothing in Docker may be touched, nothing re-delivered or re-signalled.
func TestSignalRecovery_InterruptedOperationsStayUncertainAfterRestart(t *testing.T) {
	steps := []struct {
		name   string
		events []rotation.JournalEvent
		want   string // substring of the manual-recovery message
	}{
		{"before publication completed", []rotation.JournalEvent{{Transition: rotation.TrIntentSignalPublish}}, "partially published"},
		{"published, reload never requested", []rotation.JournalEvent{{Transition: rotation.TrIntentSignalPublish}, {Transition: rotation.TrSignalPublished}}, "never requested"},
		{"reload intent without delivery ack", []rotation.JournalEvent{{Transition: rotation.TrIntentSignalPublish}, {Transition: rotation.TrSignalPublished}, {Transition: rotation.TrIntentSignalReload}}, "may or may not"},
		{"signal delivered, outcome unknown", []rotation.JournalEvent{{Transition: rotation.TrIntentSignalPublish}, {Transition: rotation.TrSignalPublished}, {Transition: rotation.TrIntentSignalReload}, {Transition: rotation.TrSignalReloadSent}}, "outcome and readiness are unknown"},
	}
	for _, c := range steps {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			d := newFakeDaemon(healthy("c1", "app"))
			logger := zaptest.NewLogger(t)
			st, _ := NewStateTracker(dir, logger)
			for i := range c.events {
				c.events[i].RotationID, c.events[i].Strategy = "sig-1", "signal"
				c.events[i].ServiceID, c.events[i].OriginalIDs = "app", []string{"c1"}
			}
			recordAll(t, st, c.events...)
			f := reloadRec(t, &recFixture{d: d, ar: newRec(t, d, "-").ar}, dir)
			f.run()
			if len(d.removed)+len(d.started)+len(d.stopped) != 0 {
				t.Fatalf("recovery must not touch Docker for an interrupted signal rotation: %+v", d)
			}
			rec := f.st.snapshotRecords()[0]
			if rec.Status != "critical_error" || !strings.Contains(rec.ErrorMessage, c.want) {
				t.Fatalf("status %s message %q, want critical_error mentioning %q", rec.Status, rec.ErrorMessage, c.want)
			}
			if !strings.Contains(rec.ErrorMessage, "no automatic re-delivery or re-signal") {
				t.Fatalf("must state that nothing was re-applied: %q", rec.ErrorMessage)
			}
			// Stays uncertain on every further restart; never becomes 'successful'.
			for i := 0; i < 2; i++ {
				f = reloadRec(t, f, dir)
				f.run()
				if f.st.snapshotRecords()[0].Status != "critical_error" {
					t.Fatal("an uncertain operation must not turn into success by restarting")
				}
			}
			// Surfaced for degraded restoration at startup.
			if rec.ServiceID != "app" {
				t.Fatalf("service id missing for degraded restoration: %+v", rec)
			}
		})
	}
}

func TestSignalRecovery_TerminalRecordsAreNotReopenedAndLaterSuccessSupersedes(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	st, _ := NewStateTracker(dir, logger)
	ev := func(id, tr, state string, manual bool) rotation.JournalEvent {
		return rotation.JournalEvent{RotationID: id, Strategy: "signal", ServiceID: "app", OriginalIDs: []string{"c1"}, Transition: tr, State: state, ManualRecovery: manual}
	}
	// A failed op that needs manual recovery...
	recordAll(t, st, ev("s1", rotation.TrIntentSignalPublish, "publishing", false), ev("s1", rotation.TrSignalFailed, "published_signal_not_sent", true))
	if st.snapshotRecords()[0].Status != "critical_error" {
		t.Fatal("manual-recovery failure must persist as critical_error")
	}
	// ...a clean success for the same service supersedes it, so a restart does not re-flag it.
	recordAll(t, st, ev("s2", rotation.TrIntentSignalPublish, "publishing", false), ev("s2", rotation.TrSignalDone, "reload_requested_adoption_unverified", false))
	for _, r := range st.snapshotRecords() {
		if r.RotationID == "s1" && r.Status != "recovered" {
			t.Fatalf("superseded record still %s", r.Status)
		}
		if r.RotationID == "s2" && (r.Status != "completed" || r.OperationState != "reload_requested_adoption_unverified") {
			t.Fatalf("success record: %+v", r)
		}
	}
	// A failure with nothing published is terminal without manual recovery.
	recordAll(t, st, ev("s3", rotation.TrSignalFailed, "nothing_published", false))
	for _, r := range st.snapshotRecords() {
		if r.RotationID == "s3" && (r.Status != "recovered" || r.ManualRecovery) {
			t.Fatalf("%+v", r)
		}
	}
}

// Restart (recreation) strategy: the decision after an interruption at each durable step.
func TestRestartRecovery_EachInterruptionPoint(t *testing.T) {
	type layout func() []*fakeCtr
	cand := func(running bool, health string) *fakeCtr {
		c := &fakeCtr{id: "n1", name: "app", owner: me, running: running, health: health, upFor: time.Hour,
			labels: map[string]string{rotation.LabelRotationID: "rs-1"}}
		return c
	}
	oldC := func(running bool) *fakeCtr {
		return &fakeCtr{id: "o1", name: "app_old_1700000000", owner: me, running: running, health: "healthy", upFor: time.Hour}
	}
	cases := []struct {
		name     string
		events   []string
		layout   layout
		removed  []string
		critical bool
		note     string
	}{
		{"intent_rename, nothing changed", []string{rotation.TrIntentRenameOriginal},
			func() []*fakeCtr { return []*fakeCtr{healthy("o1", "app")} }, nil, false, "original untouched"},
		{"renamed away, no replacement yet", []string{rotation.TrIntentRenameOriginal},
			func() []*fakeCtr { return []*fakeCtr{oldC(true)} }, nil, true, "no holder of the real name: preserved, manual"},
		{"replacement created, not started, original running under old name", []string{rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated},
			func() []*fakeCtr { return []*fakeCtr{oldC(true), cand(false, "")} }, nil, true, "holder not running: preserve both"},
		{"stop intent, both present", []string{rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrIntentStopOriginal},
			func() []*fakeCtr { return []*fakeCtr{oldC(false), cand(false, "")} }, nil, true, "neither running: preserve both"},
		// N21: started and healthy is NOT verified: file-secret delivery is proven only by
		// the probe that precedes candidate_ready. The stopped original must be kept.
		{"replacement started and healthy, original stopped, NOT journalled ready", []string{rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrIntentStopOriginal, rotation.TrOriginalStopped, rotation.TrIntentStartReplacement, rotation.TrReplacementStarted},
			func() []*fakeCtr { return []*fakeCtr{oldC(false), cand(true, "healthy")} }, nil, true, "unverified replacement must not retire the original"},
		{"replacement journalled ready, original stopped", []string{rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrIntentStopOriginal, rotation.TrOriginalStopped, rotation.TrIntentStartReplacement, rotation.TrReplacementStarted, rotation.TrCandidateReady},
			func() []*fakeCtr { return []*fakeCtr{oldC(false), cand(true, "healthy")} }, []string{"app_old_1700000000"}, false, "journalled replacement serves"},
		{"replacement started but UNHEALTHY", []string{rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrIntentStopOriginal, rotation.TrOriginalStopped, rotation.TrIntentStartReplacement},
			func() []*fakeCtr { return []*fakeCtr{oldC(false), cand(true, "unhealthy")} }, nil, true, "holder unhealthy: never remove the only other workload"},
		{"cleanup intent, original already gone", []string{rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrIntentStopOriginal, rotation.TrOriginalStopped, rotation.TrIntentStartReplacement, rotation.TrReplacementStarted, rotation.TrCandidateReady, rotation.TrIntentCleanup},
			func() []*fakeCtr { return []*fakeCtr{cand(true, "healthy")} }, nil, false, "explained by the journalled cleanup"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			d := newFakeDaemon(c.layout()...)
			logger := zaptest.NewLogger(t)
			st, _ := NewStateTracker(dir, logger)
			var evs []rotation.JournalEvent
			for _, tr := range c.events {
				e := rotation.JournalEvent{RotationID: "rs-1", Strategy: "restart", ServiceID: "app", OriginalIDs: []string{"o1"}, Transition: tr}
				if tr == rotation.TrCandidateCreated || tr == rotation.TrIntentStopOriginal || tr == rotation.TrIntentStartReplacement || tr == rotation.TrReplacementStarted || tr == rotation.TrCandidateReady {
					e.CandidateIDs = []string{"n1"}
				}
				evs = append(evs, e)
			}
			recordAll(t, st, evs...)
			f := reloadRec(t, &recFixture{d: d, ar: newRec(t, d, "-").ar}, dir)
			f.run()
			eq(t, "removed ("+c.note+")", d.removedSorted(), c.removed)
			if len(d.started) != 0 {
				t.Fatalf("recovery must not start anything here: %v", d.started)
			}
			status := f.st.snapshotRecords()[0].Status
			if c.critical && status != "critical_error" {
				t.Fatalf("want critical_error (%s), got %s", c.note, status)
			}
			if !c.critical && status != "recovered" {
				t.Fatalf("want recovered (%s), got %s", c.note, status)
			}
			before := d.names()
			f = reloadRec(t, f, dir)
			f.run()
			eq(t, "idempotent after restart", d.names(), before)
		})
	}
}
