package agent

// N19 managed file delivery: journal phases, recovery and the ledger evidence rule.
// Docker-free: the stateful fake daemon of recovery_safety_test.go, the REAL
// StateTracker (journal) and the REAL ledger (lbRig) are used.
//
// The delivery phases lie strictly before candidate_ready. A candidate created with the
// startup gate is verified only if the journal also shows secrets_delivered; recovery,
// the N21 rollback and the ledger bridge all apply that one record-level rule.

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// gatedRestartSteps is the restart journal of a gated candidate with the delivery
// phases, in the order the controller writes them.
var gatedRestartSteps = []string{
	rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated,
	rotation.TrIntentStopOriginal, rotation.TrOriginalStopped,
	rotation.TrIntentStartReplacement, rotation.TrReplacementStarted,
	rotation.TrGateStarted, rotation.TrIntentDeliverSecrets, rotation.TrSecretsDelivered,
	rotation.TrIntentReleaseGate, rotation.TrGateReleased,
	rotation.TrCandidateReady, rotation.TrIntentCleanup, rotation.TrCompleted,
}

func n19Version(seed byte) string {
	mac := make([]byte, 32)
	for i := range mac {
		mac[i] = seed
	}
	return "hmac:k-0123456789abcdef:" + base64.RawURLEncoding.EncodeToString(mac)
}

// n19JournalOpts selects deviations from the straight-line journal.
type n19JournalOpts struct {
	partial bool // delivery_partial instead of secrets_delivered
}

func n19GatedJournal(t *testing.T, st *StateTracker, rid, orig, cand, upTo string, o n19JournalOpts) {
	t.Helper()
	ctx := context.Background()
	for _, tr := range gatedRestartSteps {
		if o.partial && tr == rotation.TrSecretsDelivered {
			tr = rotation.TrDeliveryPartial
		}
		ev := rotation.JournalEvent{RotationID: rid, Transition: tr}
		switch tr {
		case rotation.TrIntentRenameOriginal:
			ev.ServiceID, ev.Strategy, ev.OriginalIDs = "app", "restart", []string{orig}
		case rotation.TrCandidateCreated:
			ev.CandidateIDs, ev.Gated = []string{cand}, true
		case rotation.TrIntentStopOriginal, rotation.TrOriginalStopped, rotation.TrIntentCleanup:
			ev.OriginalIDs = []string{orig}
		case rotation.TrSecretsDelivered:
			ev.CandidateIDs, ev.DeliveredVersions = []string{cand}, []string{n19Version(1)}
		default:
			ev.CandidateIDs = []string{cand}
		}
		if err := st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
		if tr == upTo || (o.partial && upTo == rotation.TrSecretsDelivered && tr == rotation.TrDeliveryPartial) {
			return
		}
	}
	t.Fatalf("unknown step %s", upTo)
}

func n19Candidate(health string, running bool) *fakeCtr {
	c := n21Candidate(running, health)
	c.labels[rotation.LabelRotationID] = "rot-n19"
	return c
}

const n19RID = "rot-n19"

var n19PreReadyPhases = []string{
	rotation.TrCandidateCreated, rotation.TrGateStarted, rotation.TrIntentDeliverSecrets,
	rotation.TrDeliveryPartial, rotation.TrSecretsDelivered, rotation.TrIntentReleaseGate, rotation.TrGateReleased,
}

// With the controlled rollback OFF (the default) every delivery phase preserves
// everything and escalates: the running candidate, even healthy and released, is never
// promoted to "the workload" and the stopped original is never retired.
func TestN19Recovery_EveryDeliveryPhase_PreservesAndEscalates(t *testing.T) {
	for _, phase := range n19PreReadyPhases {
		for _, health := range []string{"", "healthy"} {
			t.Run(phase+"/health="+health, func(t *testing.T) {
				d := newFakeDaemon(n21Original(false), n19Candidate(health, true), healthy("b1", "unrelated"))
				f := newRec(t, d, "-")
				o := n19JournalOpts{partial: phase == rotation.TrDeliveryPartial}
				up := phase
				if o.partial {
					up = rotation.TrSecretsDelivered
				}
				n19GatedJournal(t, f.st, n19RID, "o1", "c1", up, o)
				before := d.snapshot()
				f.run()
				if len(d.removed)+len(d.started)+len(d.stopped) != 0 || !sameSnapshot(before, d.snapshot()) {
					t.Fatalf("recovery mutated: removed=%v started=%v stopped=%v", d.removed, d.started, d.stopped)
				}
				if f.status() != "critical_error" {
					t.Fatalf("status %s: an unverified gated candidate must leave the rotation for manual recovery", f.status())
				}
				if f.record(n19RID).Transition != phase {
					t.Fatalf("recovery rewrote the journal step: %s", f.record(n19RID).Transition)
				}
				// repeated recovery is a no-op and keeps critical_error
				f.run()
				if !sameSnapshot(before, d.snapshot()) || f.status() != "critical_error" {
					t.Fatal("repeated recovery changed the outcome")
				}
				// ... and also across a restart (journal reloaded from disk)
				st2, err := NewStateTracker(f.st.stateDir, f.ar.logger)
				if err != nil {
					t.Fatal(err)
				}
				f.ar.st = st2
				f.run()
				if !sameSnapshot(before, d.snapshot()) {
					t.Fatal("recovery after a restart changed something")
				}
			})
		}
	}
}

// With the N21 rollback ON, every delivery phase rolls back through the existing
// controlled rollback (nothing deleted, the candidate moved aside, the original
// restored and started), never by promoting the candidate.
func TestN19Recovery_EveryDeliveryPhase_RollsBackSafely(t *testing.T) {
	for _, phase := range n19PreReadyPhases {
		t.Run(phase, func(t *testing.T) {
			d := newFakeDaemon(n21Original(false), n19Candidate("healthy", true), healthy("b1", "unrelated"))
			f := rbFixture(t, d, true)
			o := n19JournalOpts{partial: phase == rotation.TrDeliveryPartial}
			up := phase
			if o.partial {
				up = rotation.TrSecretsDelivered
			}
			n19GatedJournal(t, f.st, n19RID, "o1", "c1", up, o)
			f.run()
			rbAssertRolledBack(t, d, true)
			rec := f.record(n19RID)
			if rec.Status != "recovered" || rec.Transition != rotation.TrRolledBack {
				t.Fatalf("status=%s transition=%s", rec.Status, rec.Transition)
			}
			if !strings.Contains(rec.Detail, "secret delivery was NOT rolled back") {
				t.Fatalf("the journal must state what was not rolled back: %q", rec.Detail)
			}
			// idempotent
			snap := d.snapshot()
			f.run()
			if !sameSnapshot(snap, d.snapshot()) {
				t.Fatal("second recovery changed something")
			}
		})
	}
}

// A candidate that went through the complete delivery chain and candidate_ready is
// still accepted: the new rule must not regress successful-rotation recovery.
func TestN19Recovery_FullyVerifiedGatedCandidate_StillRetiresTheOriginal(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n19Candidate("healthy", true))
	f := newRec(t, d, "-")
	n19GatedJournal(t, f.st, n19RID, "o1", "c1", rotation.TrCandidateReady, n19JournalOpts{})
	f.run()
	eq(t, "removed", d.removedSorted(), []string{"app_old_1700000000"})
	if f.status() != "recovered" {
		t.Fatalf("status %s", f.status())
	}
}

// candidate_ready WITHOUT secrets_delivered on a gated record (cannot be produced by a
// crash; a damaged or forged record) is NOT verified: the original is never retired.
func TestN19Recovery_ReadyWithoutDeliveryEvidence_IsNotVerified(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n19Candidate("healthy", true))
	f := newRec(t, d, "-")
	ctx := context.Background()
	for _, ev := range []rotation.JournalEvent{
		{RotationID: n19RID, ServiceID: "app", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal, OriginalIDs: []string{"o1"}},
		{RotationID: n19RID, Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"c1"}, Gated: true},
		{RotationID: n19RID, Transition: rotation.TrCandidateReady, CandidateIDs: []string{"c1"}},
	} {
		if err := f.st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	rec := f.record(n19RID)
	if recordCandidateVerified(rec) {
		t.Fatal("a gated candidate_ready without delivery evidence must not be verified")
	}
	f.run()
	for _, r := range d.removed {
		if strings.HasPrefix(r, "app_old_") {
			t.Fatalf("the original was retired on a record without delivery evidence: %v", d.removed)
		}
	}
	if d.find("o1") == nil {
		t.Fatal("original destroyed")
	}
}

// An UNGATED record (every journal written before N19, env-only rotations) keeps the
// plain transition rule.
func TestN19Recovery_UngatedRecordsKeepTheLegacyRule(t *testing.T) {
	for _, tr := range []string{rotation.TrCandidateReady, rotation.TrCompleted} {
		if !recordCandidateVerified(&RotationState{Transition: tr}) {
			t.Fatalf("%s must stay verified for an ungated record", tr)
		}
	}
	if recordCandidateVerified(&RotationState{Transition: rotation.TrGateReleased}) {
		t.Fatal("gate_released alone is never verified")
	}
	if recordCandidateVerified(nil) {
		t.Fatal("nil record")
	}
}

// The journal refuses anything that is not a keyed identity as a delivered version,
// before merging the event.
func TestN19Journal_DeliveredVersionsMustBeKeyedIdentities(t *testing.T) {
	st, err := NewStateTracker(t.TempDir(), nil)
	if err != nil {
		t.Skip("state tracker unavailable: ", err)
	}
	ctx := context.Background()
	base := rotation.JournalEvent{RotationID: "rot-v", ServiceID: "app", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal, OriginalIDs: []string{"o1"}}
	if err := st.Record(ctx, base); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"plaintext-secret-value", "sha256:" + strings.Repeat("a", 64), "hmac:k-0123456789abcdef:short",
		"hmac:wrong:" + strings.Repeat("A", 43), "provider::abc",
	} {
		err := st.Record(ctx, rotation.JournalEvent{RotationID: "rot-v", Transition: rotation.TrSecretsDelivered, DeliveredVersions: []string{bad}})
		if err == nil {
			t.Fatalf("%q was accepted as a delivered version", bad)
		}
		if r := JournalRecordByID(st, "rot-v"); r.DeliveryVerified || len(r.DeliveredVersions) != 0 || r.Transition != rotation.TrIntentRenameOriginal {
			t.Fatalf("a refused event changed the record: %+v", r)
		}
	}
	good := n19Version(7)
	if err := st.Record(ctx, rotation.JournalEvent{RotationID: "rot-v", Transition: rotation.TrSecretsDelivered, DeliveredVersions: []string{good}}); err != nil {
		t.Fatal(err)
	}
	r := JournalRecordByID(st, "rot-v")
	if !r.DeliveryVerified || len(r.DeliveredVersions) != 1 || r.DeliveredVersions[0] != good {
		t.Fatalf("record %+v", r)
	}
	// a NEW candidate of the same operation (fallback after rolling) starts clean
	if err := st.Record(ctx, rotation.JournalEvent{RotationID: "rot-v", Transition: rotation.TrIntentCreateCandidate}); err != nil {
		t.Fatal(err)
	}
	if r := JournalRecordByID(st, "rot-v"); r.DeliveryVerified || r.GatedDelivery || len(r.DeliveredVersions) != 0 {
		t.Fatalf("a new candidate must not inherit another candidate's delivery evidence: %+v", r)
	}
}

// Independent concurrent rotations never share journal state: gated flags, delivery
// evidence and versions stay per rotation ID.
func TestN19Journal_ConcurrentRotationsAreIsolated(t *testing.T) {
	st, err := NewStateTracker(t.TempDir(), nil)
	if err != nil {
		t.Skip("state tracker unavailable: ", err)
	}
	var wg sync.WaitGroup
	n := 8
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rid := "rot-iso-" + string(rune('a'+i))
			upTo := rotation.TrCandidateCreated
			if i%2 == 0 {
				upTo = rotation.TrGateReleased
			}
			n19GatedJournalQuiet(st, rid, "o"+rid, "c"+rid, upTo, byte(i+1))
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		rid := "rot-iso-" + string(rune('a'+i))
		r := JournalRecordByID(st, rid)
		if r == nil || !r.GatedDelivery {
			t.Fatalf("%s: %+v", rid, r)
		}
		delivered := i%2 == 0
		if r.DeliveryVerified != delivered {
			t.Fatalf("%s: delivery_verified=%v want %v (evidence leaked across rotations?)", rid, r.DeliveryVerified, delivered)
		}
		if delivered && (len(r.DeliveredVersions) != 1 || r.DeliveredVersions[0] != n19Version(byte(i+1))) {
			t.Fatalf("%s: versions %v", rid, r.DeliveredVersions)
		}
		if !delivered && len(r.DeliveredVersions) != 0 {
			t.Fatalf("%s: versions %v", rid, r.DeliveredVersions)
		}
		if recordCandidateVerified(r) {
			t.Fatalf("%s: no record here reached candidate_ready", rid)
		}
	}
}

func n19GatedJournalQuiet(st *StateTracker, rid, orig, cand, upTo string, seed byte) {
	ctx := context.Background()
	for _, tr := range gatedRestartSteps {
		ev := rotation.JournalEvent{RotationID: rid, Transition: tr}
		switch tr {
		case rotation.TrIntentRenameOriginal:
			ev.ServiceID, ev.Strategy, ev.OriginalIDs = "svc-"+rid, "restart", []string{orig}
		case rotation.TrCandidateCreated:
			ev.CandidateIDs, ev.Gated = []string{cand}, true
		case rotation.TrSecretsDelivered:
			ev.CandidateIDs, ev.DeliveredVersions = []string{cand}, []string{n19Version(seed)}
		default:
			ev.CandidateIDs = []string{cand}
		}
		_ = st.Record(ctx, ev)
		if tr == upTo {
			return
		}
	}
}
