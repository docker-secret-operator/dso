package agent

// N19 follow-up (docs/design/n19-managed-delivery.md, sections 8 and 11):
//
//	B  dispatch identity vs actual delivery: the ledger never records a version that
//	   differs from the one the journal shows as delivered and verified;
//	D  pre-N19 (legacy) journal records of a file-delivered secret do not establish
//	   required file delivery while unresolved, and fallback candidates never inherit
//	   delivery evidence.
//
// Docker-free: the stateful fake daemon of recovery_safety_test.go, the REAL StateTracker
// and the REAL ledger (lbRig). Nothing here is runtime evidence.

import (
	"context"
	"testing"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// D helpers: a pre-N19 restart journal (no gate flag, no delivery phases).
var legacyRestartSteps = []string{
	rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated,
	rotation.TrIntentStopOriginal, rotation.TrOriginalStopped, rotation.TrIntentStartReplacement,
	rotation.TrReplacementStarted, rotation.TrCandidateReady,
}

func legacyJournal(t *testing.T, st *StateTracker, rid string, ver string) {
	t.Helper()
	if err := st.StartRotationWithID(rid, "vault", "db", "o1", ""); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tr := range legacyRestartSteps {
		ev := rotation.JournalEvent{RotationID: rid, Transition: tr, CandidateIDs: []string{"c1"}}
		if tr == rotation.TrIntentRenameOriginal {
			ev.ServiceID, ev.Strategy, ev.OriginalIDs, ev.CandidateIDs, ev.SecretVersion = "app", "restart", []string{"o1"}, nil, ver
		}
		if err := st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
}

// D. Upgrade with an unresolved LEGACY file-mode record. The legacy journal alone does not
// establish file delivery (archive upload into an unverified target): workloads and evidence
// are preserved, the candidate is not promoted, the known-good original is not retired, the
// operation stays blocked. The same record for an env-delivered secret keeps the plain rule.
func TestN19Legacy_UnresolvedFileModeRecord_IsNotDeliveryEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fileMode func(p, s string) bool
		preserve bool
	}{
		{"file-mode secret", func(string, string) bool { return true }, true},
		{"env-mode secret keeps the plain rule", func(string, string) bool { return false }, false},
		{"no resolver (bare tracker) keeps the plain rule", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDaemon(n21Original(false), n19Candidate("healthy", true))
			f := newRec(t, d, "-")
			f.st.FileModeSecret = tc.fileMode
			legacyJournal(t, f.st, n19RID, "")
			if got := recordCandidateVerified(f.record(n19RID)); got == tc.preserve {
				t.Fatalf("recordCandidateVerified = %v", got)
			}
			before := d.snapshot()
			f.run()
			if tc.preserve {
				if len(d.removed)+len(d.started)+len(d.stopped) != 0 || !sameSnapshot(before, d.snapshot()) {
					t.Fatalf("recovery mutated workloads: removed=%v started=%v stopped=%v", d.removed, d.started, d.stopped)
				}
				if f.status() != "critical_error" {
					t.Fatalf("status %s: the operation must stay blocked for manual recovery", f.status())
				}
				if f.record(n19RID).Transition != rotation.TrCandidateReady {
					t.Fatal("the journal evidence was rewritten")
				}
				if _, blocked := newJournalGuard(f.st).RotationBlocked("app"); !blocked {
					t.Fatal("the service is not blocked")
				}
				f.run() // repeated recovery is idempotent
				if !sameSnapshot(before, d.snapshot()) || f.status() != "critical_error" {
					t.Fatal("repeated recovery changed the outcome")
				}
				return
			}
			eq(t, "removed", d.removedSorted(), []string{"app_old_1700000000"})
			if f.status() != "recovered" {
				t.Fatalf("status %s", f.status())
			}
		})
	}
}

// D. A fallback candidate never inherits delivery evidence of an earlier candidate of the
// same operation: after the first candidate delivered and failed, the fallback candidate is
// verified only by ITS OWN secrets_delivered, whatever the journal still remembers about
// the first (the per-candidate evidence is kept for the identity check, never as proof).
func TestN19Journal_FallbackCandidateNeverInheritsDeliveryEvidence(t *testing.T) {
	st, err := NewStateTracker(t.TempDir(), nil)
	if err != nil {
		t.Skip("state tracker unavailable: ", err)
	}
	ctx := context.Background()
	rec := func(ev rotation.JournalEvent) {
		t.Helper()
		ev.RotationID = "rot-fb"
		if err := st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	rec(rotation.JournalEvent{Strategy: "rolling", Transition: rotation.TrIntentCreateCandidate, ServiceID: "app", OriginalIDs: []string{"o1"}})
	rec(rotation.JournalEvent{Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"c1"}, Gated: true})
	rec(rotation.JournalEvent{Transition: rotation.TrSecretsDelivered, CandidateIDs: []string{"c1"}, DeliveredVersions: []string{n19Version(1)}})
	rec(rotation.JournalEvent{Transition: rotation.TrCandidateReady, CandidateIDs: []string{"c1"}})
	if !recordCandidateVerified(JournalRecordByID(st, "rot-fb")) {
		t.Fatal("precondition: candidate 1 is verified")
	}
	// rolling failed later; the fallback restart creates a NEW gated candidate
	rec(rotation.JournalEvent{Strategy: "restart", Transition: rotation.TrIntentCreateCandidate})
	rec(rotation.JournalEvent{Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"c2"}, Gated: true})
	rec(rotation.JournalEvent{Transition: rotation.TrCandidateReady, CandidateIDs: []string{"c2"}}) // no delivery of its own
	r := JournalRecordByID(st, "rot-fb")
	if r.DeliveryVerified || len(r.DeliveredVersions) != 0 || recordCandidateVerified(r) {
		t.Fatalf("the fallback candidate inherited the first candidate's delivery evidence: %+v", r)
	}
	if len(r.DeliveredByCandidate["c1"]) != 1 {
		t.Fatal("the first candidate's evidence must stay on record for the identity cross-check")
	}
	// and its own delivery makes it verified
	rec(rotation.JournalEvent{Transition: rotation.TrSecretsDelivered, CandidateIDs: []string{"c2"}, DeliveredVersions: []string{n19Version(1)}})
	rec(rotation.JournalEvent{Transition: rotation.TrCandidateReady, CandidateIDs: []string{"c2"}})
	if !recordCandidateVerified(JournalRecordByID(st, "rot-fb")) {
		t.Fatal("the fallback candidate with its own delivery evidence must verify")
	}
}
