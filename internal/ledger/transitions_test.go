package ledger

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// S4a transition matrix: preconditions, refusals, effects and idempotence of
// every transition of design section 10.2.

func TestS4_Observe_FirstSightAdopts_AppliedUnknown_NoRotation(t *testing.T) {
	r := newRig(t)
	d1 := r.ver("D1")
	res := r.mustObserve(d1, s4Live(3))
	if res.Kind != ObserveAdopted {
		t.Fatalf("kind %s", res.Kind)
	}
	e := r.entry()
	if e.State != EntryAdopted || e.Applied != nil || e.DesiredSeq != 1 || len(e.Replicas) != 3 {
		t.Fatalf("adoption must record desired only (O-4): %+v", e)
	}
	for _, rp := range e.Replicas {
		// Adoption never claims delivery.
		if rp.Applied != nil || rp.Delivery != DeliveryNone || rp.Adoption != AdoptionUnknown || rp.Outcome != ReplicaAdopted || rp.RotationID != "" {
			t.Fatalf("an adopted replica claims nothing: %+v", rp)
		}
	}
	if d := r.decide(); d.Rotate() || d.Reason != DecisionAdoptedUnknown {
		t.Fatalf("first sight never rotates: %+v", d)
	}
	// Idempotent: the same observation again writes nothing.
	gen := r.s.Health().Generation
	if res := r.mustObserve(d1, s4Live(3)); res.Kind != ObserveUnchanged || r.s.Health().Generation != gen {
		t.Fatalf("steady state must not write: %s", res.Kind)
	}
	// After a restart the unchanged value is still unknown, still no rotation.
	r.crash()
	if res := r.mustObserve(r.ver("D1"), s4Live(3)); res.Kind != ObserveUnchanged {
		t.Fatalf("restart identity must be stable: %s", res.Kind)
	}
	if d := r.decide(); d.Rotate() {
		t.Fatal("restart unchanged after adoption must not rotate")
	}
	requireVocabulary(t, r.s.Snapshot().Ledger)
}

func TestS4_Observe_ZeroReplicaKey_DesiredOnly(t *testing.T) {
	r := newRig(t)
	r.mustObserve(r.ver("D1"), nil)
	if e := r.entry(); len(e.Replicas) != 0 || e.Desired == nil || e.State != EntryAdopted {
		t.Fatalf("%+v", e)
	}
}

func TestS4_Observe_ChangeIncomparableAndRefusals(t *testing.T) {
	r, d1 := baseline(t)
	d2 := r.ver("D2")
	if res := r.mustObserve(d2, s4Live(2)); res.Kind != ObserveChanged {
		t.Fatalf("kind %s", res.Kind)
	}
	e := r.entry()
	if e.DesiredSeq != 2 || e.State != EntryPending || Compare(*e.Applied, d1) != Same {
		t.Fatalf("a change advances desired only: %+v", e)
	}
	if d := r.decide(); !d.Rotate() || d.Reason != DecisionDesiredDiffers {
		t.Fatalf("%+v", d)
	}
	// A provider-kind identity is incomparable with the HMAC: re-adopt UNKNOWN.
	if res := r.mustObserve(Version{Kind: KindProvider, ID: "17"}, s4Live(2)); res.Kind != ObserveReadopted {
		t.Fatalf("kind %s", res.Kind)
	}
	if e := r.entry(); e.Applied != nil || e.State != EntryAdopted {
		t.Fatalf("incomparable means unknown: %+v", e)
	}
	if d := r.decide(); d.Rotate() {
		t.Fatalf("incomparable never means changed: %+v", d)
	}
	// Refusals change nothing.
	before := r.s.Snapshot().Ledger
	for name, call := range map[string]func() error{
		"empty version": func() error { _, err := r.v.Observe(s4K, Version{}, nil); return err },
		"bad key":       func() error { _, err := r.v.Observe(Key{Service: "x"}, d1, nil); return err },
		"foreign key id": func() error {
			v := d1
			v.KeyID = "k-ffffffffffffffff"
			_, err := r.v.Observe(s4K, v, nil)
			return err
		},
		"bad live":       func() error { _, err := r.v.Observe(s4K, d1, []LiveReplica{{ContainerID: "x"}}); return err },
		"duplicate live": func() error { l := s4Live(1); _, err := r.v.Observe(s4K, d1, append(l, l[0])); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s: want ErrInvalidTransition, got %v", name, err)
		}
	}
	if !reflect.DeepEqual(before, r.s.Snapshot().Ledger) {
		t.Fatal("a refused Observe changed the ledger")
	}
}

// Design 1.4 and scenario 15: identity drift, vanished, new and ambiguous
// replicas never cause a rotation.
func TestS4_Observe_ReplicaIdentity_NeverRotates(t *testing.T) {
	r, d1 := baseline(t)
	live := s4Live(2)
	live[0].ContainerID, live[0].Created = "c1-recreated", "2026-02-01T00:00:00Z"
	if res := r.mustObserve(d1, live); res.Kind != ObserveReplicasChanged {
		t.Fatalf("kind %s", res.Kind)
	}
	e := r.entry()
	var drifted, kept Replica
	for _, rp := range e.Replicas {
		if rp.ContainerName == "shop-api-1" {
			drifted = rp
		} else {
			kept = rp
		}
	}
	if drifted.Reason != string(WhyIdentityChanged) || drifted.Applied != nil || drifted.Adoption != AdoptionUnknown || drifted.ContainerID != "c1-recreated" {
		t.Fatalf("a recreated replica becomes unknown: %+v", drifted)
	}
	if kept.Delivery != DeliveryVerified {
		t.Fatalf("an unchanged replica keeps its record: %+v", kept)
	}
	if d := r.decide(); d.Rotate() {
		t.Fatalf("identity drift never rotates: %+v", d)
	}
	// Scale up (a new replica) and down (one vanished): adopted unknown, removed.
	live = append(live[1:], LiveReplica{Number: "3", ContainerID: "c3", ContainerName: "shop-api-3", Created: "2026-01-01T00:00:03Z"})
	r.mustObserve(d1, live)
	e = r.entry()
	if len(e.Replicas) != 2 {
		t.Fatalf("%+v", e.Replicas)
	}
	// Duplicate Compose number and name: ambiguous, recorded unknown.
	dup := []LiveReplica{
		{Number: "2", ContainerID: "c2", ContainerName: "shop-api-2", Created: "2026-01-01T00:00:02Z"},
		{Number: "2", ContainerID: "c2b", ContainerName: "shop-api-2", Created: "2026-01-01T00:00:09Z"},
	}
	r.mustObserve(d1, dup)
	for _, rp := range r.entry().Replicas {
		if rp.Reason != string(WhyIdentityChanged) || rp.Adoption != AdoptionUnknown {
			t.Fatalf("ambiguous identity must be unknown: %+v", rp)
		}
	}
	if d := r.decide(); d.Rotate() {
		t.Fatalf("ambiguous identity never rotates: %+v", d)
	}
	requireVocabulary(t, r.s.Snapshot().Ledger)
}

func TestS4_NeedsRotation_PureTable(t *testing.T) {
	d1 := &Version{Kind: KindProvider, ID: "1"}
	d2 := &Version{Kind: KindProvider, ID: "2"}
	h := &Version{Kind: KindHMAC, ID: "x", KeyID: s2KeyA}
	ent := func(st EntryState, desired, applied *Version, seq uint64, reps ...Replica) *Entry {
		return &Entry{Key: s4K, Desired: desired, Applied: applied, DesiredSeq: seq, State: st, Replicas: reps}
	}
	elig := Replica{Outcome: ReplicaPending, Reason: string(WhyEligibilityChanged), Delivery: DeliveryNone}
	cases := []struct {
		name string
		in   RotationInput
		kind DecisionKind
		why  DecisionReason
	}{
		{"ledger missing", RotationInput{Condition: ConditionMissing, Entry: ent(EntryIdle, d2, d1, 2)}, DecisionUnknown, DecisionLedgerMissing},
		{"ledger corrupt", RotationInput{Condition: ConditionCorrupt, Entry: ent(EntryIdle, d2, d1, 2)}, DecisionUnknown, DecisionLedgerCorrupt},
		{"ledger unreadable", RotationInput{Condition: ConditionUnreadable, Entry: ent(EntryIdle, d2, d1, 2)}, DecisionUnknown, DecisionLedgerUnreadable},
		{"newer schema", RotationInput{Condition: ConditionNewerSchema, Entry: ent(EntryIdle, d2, d1, 2)}, DecisionUnknown, DecisionLedgerNewerSchema},
		{"key lost", RotationInput{Condition: ConditionKeyLost, Entry: ent(EntryIdle, d2, d1, 2)}, DecisionUnknown, DecisionKeyLost},
		{"key unavailable", RotationInput{Condition: ConditionKeyUnavailable, Entry: ent(EntryIdle, d2, d1, 2)}, DecisionUnknown, DecisionKeyUnavailableCause},
		{"unknown condition", RotationInput{Condition: LedgerCondition(99), Entry: ent(EntryIdle, d2, d1, 2)}, DecisionUnknown, DecisionKeyUnavailableCause},
		{"no entry", RotationInput{}, DecisionUnknown, DecisionNoEntry},
		{"no desired", RotationInput{Entry: ent(EntryIdle, nil, nil, 0)}, DecisionNoRotate, DecisionNoDesired},
		{"blocked with change", RotationInput{Entry: ent(EntryBlocked, d2, d1, 2)}, DecisionNoRotate, DecisionBlocked},
		{"adopted", RotationInput{Entry: ent(EntryAdopted, d1, nil, 1)}, DecisionNoRotate, DecisionAdoptedUnknown},
		{"adopted after readoption seq 5", RotationInput{Entry: ent(EntryAdopted, d1, nil, 5)}, DecisionNoRotate, DecisionAdoptedUnknown},
		{"unknown applied, seq 1", RotationInput{Entry: ent(EntryPending, d1, nil, 1)}, DecisionNoRotate, DecisionAdoptedUnknown},
		{"changed since adoption", RotationInput{Entry: ent(EntryPending, d2, nil, 2)}, DecisionRotate, DecisionChangedSinceAdopt},
		{"differs", RotationInput{Entry: ent(EntryPending, d2, d1, 2)}, DecisionRotate, DecisionDesiredDiffers},
		{"failed differs", RotationInput{Entry: ent(EntryFailed, d2, d1, 2)}, DecisionRotate, DecisionDesiredDiffers},
		{"same", RotationInput{Entry: ent(EntryIdle, d1, d1, 1)}, DecisionNoRotate, DecisionUpToDate},
		{"incomparable", RotationInput{Entry: ent(EntryIdle, h, d1, 2)}, DecisionUnknown, DecisionIncomparable},
		{"eligibility changed", RotationInput{Entry: ent(EntryIdle, d1, d1, 1, elig)}, DecisionRotate, DecisionEligibilityChanged},
		{"eligibility while blocked", RotationInput{Entry: ent(EntryBlocked, d1, d1, 1, elig)}, DecisionNoRotate, DecisionBlocked},
	}
	for _, c := range cases {
		d := NeedsRotation(c.in)
		if d.Kind != c.kind || d.Reason != c.why {
			t.Errorf("%s: got %+v, want %s/%s", c.name, d, c.kind, c.why)
		}
		if d.Rotate() != (c.kind == DecisionRotate) {
			t.Errorf("%s: Rotate() disagrees with the kind", c.name)
		}
	}
}

func TestS4_Correlate_Matrix(t *testing.T) {
	r := newRig(t)
	d1 := r.ver("D1")
	r.mustObserve(d1, s4Live(2))
	before := r.s.Snapshot().Ledger
	requireErr(t, r.v.Correlate(s4K, "", 1, d1), ErrInvalidTransition, "empty id")
	requireErr(t, r.v.Correlate(s4K, "rot 1", 1, d1), ErrInvalidTransition, "malformed id")
	requireErr(t, r.v.Correlate(s4K, strings.Repeat("r", 65), 1, d1), ErrInvalidTransition, "long id")
	requireErr(t, r.v.Correlate(Key{Service: "x", Provider: "p", Secret: "s"}, "rot-1", 1, d1), ErrNotFound, "unknown entry")
	requireErr(t, r.v.Correlate(s4K, "rot-1", 2, d1), ErrStale, "stale seq")
	requireErr(t, r.v.Correlate(s4K, "rot-1", 1, r.ver("other")), ErrStale, "wrong version")
	if !reflect.DeepEqual(before, r.s.Snapshot().Ledger) {
		t.Fatal("refused Correlate changed the ledger")
	}
	if err := r.v.Correlate(s4K, "rot-0000000000000001", 1, d1); err != nil {
		t.Fatal(err)
	}
	e := r.entry()
	if e.State != EntryPending || e.Applied != nil {
		t.Fatalf("correlating an adopted entry only leaves adopted: %+v", e)
	}
	for _, rp := range e.Replicas {
		if rp.Delivery != DeliveryRequested || rp.RotationID != "rot-0000000000000001" || rp.Applied != nil {
			t.Fatalf("%+v", rp)
		}
	}
	requireErr(t, r.v.Correlate(s4K, "rot-0000000000000001", 1, d1), ErrNoChange, "idempotent")
	// Blocked entries and entries without targets are refused.
	r2 := newRig(t)
	r2.mustObserve(r2.ver("D1"), nil)
	requireErr(t, r2.v.Correlate(s4K, "rot-1", 1, r2.ver("D1")), ErrNoTargets, "no replicas")
	r3 := newRig(t)
	r3.mustObserve(r3.ver("D1"), s4Live(1))
	if err := r3.v.Block(s4K, BlockNoRotatingStrategy); err != nil {
		t.Fatal(err)
	}
	requireErr(t, r3.v.Correlate(s4K, "rot-1", 1, r3.ver("D1")), ErrInvalidTransition, "blocked")
}

// Evidence rule of design 4.2, refused kinds one by one (T-EV, ledger side).
func TestS4_RecordDeliveryVerified_RefusesWeakerEvidence(t *testing.T) {
	r := newRig(t)
	d1 := r.ver("D1")
	r.mustObserve(d1, s4Live(2))
	rid := "rot-00000000000000aa"
	if err := r.v.Correlate(s4K, rid, 1, d1); err != nil {
		t.Fatal(err)
	}
	before := r.s.Snapshot().Ledger
	base := evOK(rid, d1)
	refused := map[string]func(e *Evidence){
		"pre-ready":                  func(e *Evidence) { e.Kind = EvidencePreReady },
		"recovered alone":            func(e *Evidence) { e.Status = JournalRecovered },
		"in_progress without commit": func(e *Evidence) { e.Status = JournalInProgress },
		"critical_error":             func(e *Evidence) { e.Status = JournalCriticalError },
		"critical_error committed":   func(e *Evidence) { e.Status, e.Committed = JournalCriticalError, true },
		"rollback_required":          func(e *Evidence) { e.Status = JournalRollbackRequired },
		"rollback_required committed": func(e *Evidence) {
			e.Status, e.Committed = JournalRollbackRequired, true
		},
		"seq 0":                     func(e *Evidence) { e.Seq = 0 },
		"unknown status":            func(e *Evidence) { e.Status = "done" },
		"unknown kind":              func(e *Evidence) { e.Kind = "forged" },
		"empty kind":                func(e *Evidence) { e.Kind = "" },
		"signal not completed":      func(e *Evidence) { e.Kind, e.Status, e.Committed = EvidenceSignalDone, JournalRecovered, true },
		"empty rotation id":         func(e *Evidence) { e.RotationID = "" },
		"no version":                func(e *Evidence) { e.Version = Version{} },
		"version under another key": func(e *Evidence) { e.Version.KeyID = "k-ffffffffffffffff" },
	}
	for name, mut := range refused {
		ev := base
		mut(&ev)
		if _, err := r.v.RecordDeliveryVerified(s4K, ev); !errors.Is(err, ErrNoEvidence) {
			t.Errorf("%s: want ErrNoEvidence, got %v", name, err)
		}
	}
	// Valid evidence for an operation nothing was correlated with.
	if _, err := r.v.RecordDeliveryVerified(s4K, evOK("rot-00000000000000bb", d1)); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("uncorrelated rotation: %v", err)
	}
	if !reflect.DeepEqual(before, r.s.Snapshot().Ledger) {
		t.Fatal("refused evidence changed the ledger")
	}
}

func TestS4_RecordDeliveryVerified_AcceptedKinds_AdoptionRule(t *testing.T) {
	cases := []struct {
		name     string
		mut      func(e *Evidence)
		adoption AdoptionState
		why      OutcomeReason
	}{
		{"restart completed env", func(*Evidence) {}, AdoptionVerified, WhyDeliveryVerified},
		{"rolling committed in progress", func(e *Evidence) { e.Kind, e.Status, e.Committed = EvidenceRollingVerified, JournalInProgress, true }, AdoptionVerified, WhyDeliveryVerified},
		{"restart recovered with commit", func(e *Evidence) { e.Status, e.Committed = JournalRecovered, true }, AdoptionVerified, WhyDeliveryVerified},
		{"file mode (N19)", func(e *Evidence) { e.FileMode = true }, AdoptionUnverified, WhyAdoptionUnverifiedFile},
		{"signal", func(e *Evidence) { e.Kind = EvidenceSignalDone }, AdoptionUnverified, WhyAdoptionUnverifiedSignal},
		{"signal file mode", func(e *Evidence) { e.Kind, e.FileMode = EvidenceSignalDone, true }, AdoptionUnverified, WhyAdoptionUnverifiedSignal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			d1 := r.ver("D1")
			r.mustObserve(d1, s4Live(2))
			rid := "rot-00000000000000cc"
			if err := r.v.Correlate(s4K, rid, 1, d1); err != nil {
				t.Fatal(err)
			}
			ev := evOK(rid, d1)
			c.mut(&ev)
			res, err := r.v.RecordDeliveryVerified(s4K, ev)
			if err != nil || !res.Applied || res.Adoption != c.adoption {
				t.Fatalf("%+v %v", res, err)
			}
			e := r.entry()
			if e.State != EntryIdle || Compare(*e.Applied, d1) != Same || e.LastOutcome == nil || e.LastOutcome.Kind != outcomeSuccess {
				t.Fatalf("one update verifies and applies: %+v", e)
			}
			for _, rp := range e.Replicas {
				if rp.Delivery != DeliveryVerified || rp.Adoption != c.adoption || rp.Reason != string(c.why) || rp.Outcome != ReplicaSucceeded {
					t.Fatalf("%+v", rp)
				}
			}
			if _, err := r.v.RecordDeliveryVerified(s4K, ev); !errors.Is(err, ErrNoChange) {
				t.Fatalf("re-application must be a no-op: %v", err)
			}
			if d := r.decide(); d.Rotate() {
				t.Fatalf("a delivered version is not rotated again (no signal loop): %+v", d)
			}
			// Conflicting evidence for the same operation is refused.
			other := evOK(rid, r.ver("OTHER"))
			if _, err := r.v.RecordDeliveryVerified(s4K, other); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("conflicting evidence: %v", err)
			}
			requireVocabulary(t, r.s.Snapshot().Ledger)
		})
	}
}

func TestS4_RecordDeliveryVerified_DesiredMovedOn_StaysPending(t *testing.T) {
	r, _ := baseline(t)
	d2, d3 := r.ver("D2"), r.ver("D3")
	r.mustObserve(d2, s4Live(2))
	rid := "rot-0000000000000d02"
	if err := r.v.Correlate(s4K, rid, 2, d2); err != nil {
		t.Fatal(err)
	}
	r.mustObserve(d3, s4Live(2)) // a newer value arrives mid-rotation
	if _, err := r.v.RecordDeliveryVerified(s4K, evOK(rid, d2)); err != nil {
		t.Fatal(err)
	}
	e := r.entry()
	if e.State != EntryPending || Compare(*e.Applied, d2) != Same || e.DesiredSeq != 3 {
		t.Fatalf("%+v", e)
	}
	if d := r.decide(); !d.Rotate() {
		t.Fatal("D3 is still owed")
	}
}

func TestS4_RecordApplied_Matrix(t *testing.T) {
	r := newRig(t)
	d1 := r.ver("D1")
	r.mustObserve(d1, s4Live(2))
	rid := "rot-00000000000000dd"
	requireErr(t, r.v.RecordApplied(s4K, ""), ErrInvalidTransition, "empty id")
	requireErr(t, r.v.RecordApplied(s4K, rid), ErrInvalidTransition, "nothing correlated")
	if err := r.v.Correlate(s4K, rid, 1, d1); err != nil {
		t.Fatal(err)
	}
	requireErr(t, r.v.RecordApplied(s4K, rid), ErrInvalidTransition, "requested, not verified")
	// Construct the split "verified but not applied" record (design W7 note).
	if err := r.s.update(func(l *Ledger) error {
		e := findEntry(l, s4K)
		for i := range e.Replicas {
			rp := &e.Replicas[i]
			rp.Applied, rp.Delivery, rp.Adoption, rp.Outcome = versionCopy(d1), DeliveryVerified, AdoptionVerified, ReplicaSucceeded
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.v.RecordApplied(s4K, rid); err != nil {
		t.Fatal(err)
	}
	if e := r.entry(); e.State != EntryIdle || Compare(*e.Applied, d1) != Same {
		t.Fatalf("%+v", e)
	}
	requireErr(t, r.v.RecordApplied(s4K, rid), ErrNoChange, "idempotent")
}

func TestS4_RecordAdoptionVerified_Matrix(t *testing.T) {
	setup := func(t *testing.T, mut func(*Evidence)) (*rig, Version, string) {
		r := newRig(t)
		d1 := r.ver("D1")
		r.mustObserve(d1, s4Live(1))
		rid := "rot-00000000000000ee"
		if err := r.v.Correlate(s4K, rid, 1, d1); err != nil {
			t.Fatal(err)
		}
		ev := evOK(rid, d1)
		mut(&ev)
		if _, err := r.v.RecordDeliveryVerified(s4K, ev); err != nil {
			t.Fatal(err)
		}
		return r, d1, rid
	}
	ref := ReplicaRef{Number: "1", ContainerName: "shop-api-1"}
	// Signal: never adoption verified, whatever the evidence.
	r, d1, rid := setup(t, func(e *Evidence) { e.Kind = EvidenceSignalDone })
	for _, k := range []AdoptionEvidenceKind{AdoptionEvidenceRestart, AdoptionEvidenceLabel, AdoptionEvidenceFileProbe, AdoptionEvidenceSignal, AdoptionEvidenceEnvComparison, ""} {
		if err := r.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: k, RotationID: rid, Version: d1}); !errors.Is(err, ErrNoEvidence) {
			t.Errorf("signal replica with %q: %v", k, err)
		}
	}
	if r.entry().Replicas[0].Adoption != AdoptionUnverified {
		t.Fatal("a signal replica stays unverified")
	}
	// File mode: stays unverified (N19).
	r, d1, rid = setup(t, func(e *Evidence) { e.FileMode = true })
	if err := r.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: AdoptionEvidenceLabel, RotationID: rid, Version: d1}); !errors.Is(err, ErrNoEvidence) {
		t.Errorf("file mode: %v", err)
	}
	// Restart env: already verified, idempotent; refused shapes change nothing.
	r, d1, rid = setup(t, func(*Evidence) {})
	requireErr(t, r.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: AdoptionEvidenceLabel, RotationID: rid, Version: d1}), ErrNoChange, "already verified")
	requireErr(t, r.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: AdoptionEvidenceLabel, RotationID: "rot-other", Version: d1}), ErrNoEvidence, "other rotation")
	requireErr(t, r.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: AdoptionEvidenceLabel, RotationID: rid, Version: r.ver("X")}), ErrNoEvidence, "other version")
	requireErr(t, r.v.RecordAdoptionVerified(s4K, ReplicaRef{Number: "9", ContainerName: "nope"}, AdoptionEvidence{Kind: AdoptionEvidenceLabel, RotationID: rid, Version: d1}), ErrNotFound, "unknown replica")
	requireErr(t, r.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: AdoptionEvidenceLabel, Version: d1}), ErrNoEvidence, "no rotation id")
	// Adoption never claims delivery: an adopted (undelivered) replica is refused.
	r2 := newRig(t)
	d := r2.ver("D1")
	r2.mustObserve(d, s4Live(1))
	requireErr(t, r2.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: AdoptionEvidenceLabel, RotationID: "rot-1", Version: d}), ErrNoEvidence, "undelivered")
	if rp := r2.entry().Replicas[0]; rp.Delivery != DeliveryNone || rp.Adoption != AdoptionUnknown {
		t.Fatalf("%+v", rp)
	}
	// Upgrade path: a delivered replica whose adoption was lowered (for example
	// by reconcile) is upgraded by a label-proven container.
	r3, d3, rid3 := setup(t, func(*Evidence) {})
	if err := r3.s.update(func(l *Ledger) error { findEntry(l, s4K).Replicas[0].Adoption = AdoptionUnverified; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r3.v.RecordAdoptionVerified(s4K, ref, AdoptionEvidence{Kind: AdoptionEvidenceLabel, RotationID: rid3, Version: d3}); err != nil {
		t.Fatal(err)
	}
	if r3.entry().Replicas[0].Adoption != AdoptionVerified {
		t.Fatal("label evidence upgrades adoption")
	}
}

func TestS4_HandleInterrupted_Causes(t *testing.T) {
	type want struct {
		state    EntryState
		outcome  ReplicaOutcome
		adoption AdoptionState
		why      OutcomeReason
	}
	cases := map[InterruptCause]want{
		CauseUnresolved: {EntryFailed, ReplicaFailed, AdoptionUnknown, WhyInterrupted},
		CauseNeverRan:   {EntryPending, ReplicaPending, AdoptionVerified, WhyDeferred},
		CauseFailed:     {EntryFailed, ReplicaFailed, AdoptionVerified, WhyFailed},
		CauseRolledBack: {EntryRolledBack, ReplicaRolledBack, AdoptionVerified, WhyRolledBack},
	}
	for cause, w := range cases {
		t.Run(string(cause), func(t *testing.T) {
			r, d1 := baseline(t)
			d2 := r.ver("D2")
			r.mustObserve(d2, s4Live(2))
			rid := "rot-0000000000000d02"
			if err := r.v.Correlate(s4K, rid, 2, d2); err != nil {
				t.Fatal(err)
			}
			if err := r.v.HandleInterrupted(s4K, rid, cause); err != nil {
				t.Fatal(err)
			}
			e := r.entry()
			if e.State != w.state || Compare(*e.Applied, d1) != Same {
				t.Fatalf("applied never advances: %+v", e)
			}
			for _, rp := range e.Replicas {
				if rp.Delivery != DeliveryNone || rp.Outcome != w.outcome || rp.Adoption != w.adoption || rp.Reason != string(w.why) || Compare(*rp.Applied, d1) != Same {
					t.Fatalf("%+v", rp)
				}
			}
			requireErr(t, r.v.HandleInterrupted(s4K, rid, cause), ErrNoChange, "idempotent")
			if d := r.decide(); !d.Rotate() {
				t.Fatal("D2 is still owed after an interrupted rotation")
			}
			requireVocabulary(t, r.s.Snapshot().Ledger)
		})
	}
	r, d1 := baseline(t)
	requireErr(t, r.v.HandleInterrupted(s4K, "rot-x", "maybe"), ErrInvalidTransition, "unknown cause")
	requireErr(t, r.v.HandleInterrupted(s4K, "", CauseFailed), ErrInvalidTransition, "empty id")
	requireErr(t, r.v.HandleInterrupted(Key{Service: "x", Provider: "p", Secret: "s"}, "rot-x", CauseFailed), ErrNotFound, "unknown entry")
	requireErr(t, r.v.HandleInterrupted(s4K, "rot-0000000000000d01", CauseUnresolved), ErrInvalidTransition, "already verified")
	_ = d1
}

func TestS4_BlockUnblock_TwoStep(t *testing.T) {
	// T1: a new eligible replica of a no_rotating_strategy entry.
	r, d1 := baseline(t)
	if err := r.v.Block(s4K, BlockNoRotatingStrategy); err != nil {
		t.Fatal(err)
	}
	requireErr(t, r.v.Block(s4K, BlockNoRotatingStrategy), ErrNoChange, "block idempotent")
	if d := r.decide(); d.Rotate() || d.Reason != DecisionBlocked {
		t.Fatalf("%+v", d)
	}
	live := s4Live(3)
	for i := range live[:2] {
		live[i].Eligible = false
	}
	requireErr(t, r.v.Unblock(s4K, UnblockInput{Trigger: UnblockNewEligibleReplica, Live: live}), ErrInvalidTransition, "not yet observed")
	r.mustObserve(d1, live)
	requireErr(t, r.v.Unblock(s4K, UnblockInput{Trigger: UnblockSecretStrategyChanged, Live: live}), ErrInvalidTransition, "T3 for no_rotating_strategy")
	before := r.entry()
	if err := r.v.Unblock(s4K, UnblockInput{Trigger: UnblockNewEligibleReplica, Live: live}); err != nil {
		t.Fatal(err)
	}
	after := r.entry()
	if after.State != EntryIdle || !reflect.DeepEqual(after.Replicas, before.Replicas) || !reflect.DeepEqual(after.LastOutcome, before.LastOutcome) || !reflect.DeepEqual(after.Applied, before.Applied) {
		t.Fatalf("Unblock changes only the state (I-B4): %+v", after)
	}
	if d := r.decide(); !d.Rotate() || d.Reason != DecisionEligibilityChanged {
		t.Fatalf("step two is the ordinary rotation (D61-2): %+v", d)
	}
	requireErr(t, r.v.Unblock(s4K, UnblockInput{Trigger: UnblockNewEligibleReplica, Live: live}), ErrInvalidTransition, "not blocked")

	// T2: a recorded blocked replica whose strategy now dispatches.
	r2, d := baseline(t)
	if err := r2.v.Block(s4K, BlockNoRotatingStrategy); err != nil {
		t.Fatal(err)
	}
	l2 := s4Live(2)
	l2[0].Eligible, l2[1].Eligible = false, false
	r2.mustObserve(d, l2)
	requireErr(t, r2.v.Unblock(s4K, UnblockInput{Trigger: UnblockStrategyNowDispatches, Live: l2}), ErrInvalidTransition, "nothing eligible")
	l2[1].Eligible = true
	r2.mustObserve(d, l2)
	if err := r2.v.Unblock(s4K, UnblockInput{Trigger: UnblockStrategyNowDispatches, Live: l2}); err != nil {
		t.Fatal(err)
	}

	// T3: strategy_none, and the secret strategy changed.
	r3, d3 := baseline(t)
	if err := r3.v.Block(s4K, BlockStrategyNone); err != nil {
		t.Fatal(err)
	}
	requireErr(t, r3.v.Unblock(s4K, UnblockInput{Trigger: UnblockSecretStrategyChanged, SecretStrategyNone: true}), ErrInvalidTransition, "still none")
	requireErr(t, r3.v.Unblock(s4K, UnblockInput{Trigger: UnblockNewEligibleReplica, Live: s4Live(2)}), ErrInvalidTransition, "T1 for strategy_none")
	r3.mustObserve(r3.ver("D2"), s4Live(2))
	if err := r3.v.Unblock(s4K, UnblockInput{Trigger: UnblockSecretStrategyChanged}); err != nil {
		t.Fatal(err)
	}
	if e := r3.entry(); e.State != EntryPending || Compare(*e.Applied, d3) != Same {
		t.Fatalf("desired differs: pending: %+v", e)
	}

	// ambiguous_provider: no trigger at all; unknown block reasons are refused.
	r4, _ := baseline(t)
	if err := r4.v.Block(s4K, BlockAmbiguousProvider); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []UnblockTrigger{UnblockNewEligibleReplica, UnblockStrategyNowDispatches, UnblockSecretStrategyChanged, "T9"} {
		requireErr(t, r4.v.Unblock(s4K, UnblockInput{Trigger: tr, Live: s4Live(2)}), ErrInvalidTransition, string(tr))
	}
	requireErr(t, r4.v.Block(s4K, "because"), ErrInvalidTransition, "unknown block reason")
	requireVocabulary(t, r4.s.Snapshot().Ledger)
}

// Memory-only mode (design 6): transitions work in memory, nothing on disk
// changes, Unblock needs healthy persistence.
func TestS4_MemoryOnly_NewerSchema_InMemoryAdoption_UnblockRefused(t *testing.T) {
	dir := stateDir(t)
	orig := newerSchemaFile(t, dir)
	fp := dirFingerprint(t, dir)
	s, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if res.Mode != ModeMemoryOnly || res.Reason != ReasonNewerSchema {
		t.Fatalf("%+v", res)
	}
	v, _ := NewVersions(s, fakeKM{id: s2KeyA, key: s4Key})
	d1, err := v.NewVersion("p", "s", map[string]string{"password": "D1"})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := v.Observe(s4K, d1, s4Live(1)); err != nil || r.Kind != ObserveAdopted {
		t.Fatalf("%+v %v", r, err)
	}
	if d := v.NeedsRotation(s4K); d.Rotate() {
		t.Fatalf("memory-only adoption never rotates: %+v", d)
	}
	d2, _ := v.NewVersion("p", "s", map[string]string{"password": "D2"})
	if _, err := v.Observe(s4K, d2, s4Live(1)); err != nil {
		t.Fatal(err)
	}
	if d := v.NeedsRotation(s4K); !d.Rotate() {
		t.Fatal("a change observed during the run is still detected")
	}
	if err := v.Block(s4K, BlockStrategyNone); err != nil {
		t.Fatal(err)
	}
	requireErr(t, v.Unblock(s4K, UnblockInput{Trigger: UnblockSecretStrategyChanged}), ErrPersistenceUnavailable, "memory-only unblock")
	_ = s.Close()
	if b, _ := readFileIfRegular(dir + "/ledger.json"); dirFingerprint(t, dir) != fp || string(b) != string(orig) {
		t.Fatal("memory-only must leave the directory untouched")
	}
}

// Missing, corrupt, newer-schema or key-lost ledgers and lost keys never yield
// "changed" (X3).
func TestS4_NoRotateDecision_ForEveryUnusableLedger(t *testing.T) {
	// Key unsafe (a lost key is reported unsafe by the S3 provider): memory-only,
	// the decision is unknown and no keyed identity can be built.
	dir := stateDir(t)
	s, res, _, _ := openT(t, dir, newFakeKeys(KeyInspection{State: KeyUnsafe, Reason: "lost"}))
	v, _ := NewVersions(s, fakeKM{err: errors.New("ledger key: lost")})
	if res.Reason != ReasonKeyUnsafe {
		t.Fatalf("%+v", res)
	}
	if d := v.NeedsRotation(s4K); d.Kind != DecisionUnknown || d.Reason != DecisionKeyUnavailableCause {
		t.Fatalf("%+v", d)
	}
	if _, err := v.NewVersion("p", "s", map[string]string{"password": s4Secret}); !errors.Is(err, ErrKeyUnavailable) || strings.Contains(err.Error(), s4Secret) {
		t.Fatalf("%v", err)
	}
	if v2, _ := NewVersions(s, nil); v2 != nil {
		if _, err := v2.NewVersion("p", "s", nil); !errors.Is(err, ErrKeyUnavailable) {
			t.Fatal(err)
		}
	}
	// Key id differs from the ledger's: refused, never an incomparable write.
	r := newRig(t)
	r.v.km = fakeKM{id: s2KeyB, key: s4Key}
	if _, err := r.v.NewVersion("p", "s", map[string]string{"password": "x"}); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("%v", err)
	}

	// Corrupt ledger and absent key: the store starts a fresh ledger; the entry
	// is unknown and the first observation adopts without rotating.
	for name, prep := range map[string]func(r *rig){
		"corrupt": func(r *rig) { writeFile(t, r.dir+"/ledger.json", []byte("{garbage"), 0o600) },
		"key lost": func(r *rig) {
			r.keys.mu.Lock()
			r.keys.state = KeyInspection{State: KeyAbsent}
			r.keys.genID = s2KeyB
			r.keys.mu.Unlock()
			r.km = fakeKM{id: s2KeyB, key: s4Key}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := baseline(t)
			r.mustObserve(r.ver("D2"), s4Live(2)) // a change is pending
			_ = r.s.Close()
			prep(r)
			r.open()
			if r.res.Reason != ReasonCorrupt && r.res.Reason != ReasonKeyMissing {
				t.Fatalf("%+v", r.res)
			}
			if d := r.decide(); d.Kind != DecisionUnknown || d.Reason != DecisionNoEntry {
				t.Fatalf("%+v", d)
			}
			r.mustObserve(r.ver("D2"), s4Live(2))
			if d := r.decide(); d.Rotate() {
				t.Fatalf("no mass rotation after %s: %+v", name, d)
			}
		})
	}
}

// Restart identity stability: the same data under the same key gives the same
// identity after reopening, so the steady state writes nothing.
func TestS4_Restart_IdentityStable_NoWrite(t *testing.T) {
	r, d1 := baseline(t)
	gen := r.s.Health().Generation
	r.crash()
	again := r.ver("D1")
	if again != d1 {
		t.Fatal("identity changed across a restart")
	}
	if res := r.mustObserve(again, s4Live(2)); res.Kind != ObserveUnchanged {
		t.Fatalf("%s", res.Kind)
	}
	if r.s.Health().Generation != gen {
		t.Fatal("an unchanged observation after restart must not write")
	}
	if d := r.decide(); d.Rotate() || d.Reason != DecisionUpToDate {
		t.Fatalf("scenario 3: %+v", d)
	}
}

// Concurrent Observe and delivery transitions serialise through the single
// store path (meaningful under -race): desired_seq is monotonic and no update
// is lost.
func TestS4_Concurrent_ObserveAndRecordDelivery(t *testing.T) {
	r, _ := baseline(t)
	vers := make([]Version, 20)
	for i := range vers {
		vers[i] = r.ver("V" + string(rune('a'+i)))
	}
	done := make(chan struct{})
	errs := make(chan error, 64)
	go func() {
		defer close(done)
		for _, ver := range vers {
			if _, err := r.v.Observe(s4K, ver, s4Live(2)); err != nil {
				errs <- err
			}
		}
	}()
	for i := 0; i < 20; i++ {
		e := r.entry()
		rid := "rot-c" + string(rune('a'+i))
		if err := r.v.Correlate(s4K, rid, e.DesiredSeq, *e.Desired); err != nil && !errors.Is(err, ErrStale) && !errors.Is(err, ErrNoChange) {
			errs <- err
			continue
		}
		if _, err := r.v.RecordDeliveryVerified(s4K, evOK(rid, *e.Desired)); err != nil &&
			!errors.Is(err, ErrNoChange) && !errors.Is(err, ErrInvalidTransition) {
			errs <- err
		}
		_ = r.decide()
	}
	<-done
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	e := r.entry()
	if e.DesiredSeq != 21 || Compare(*e.Desired, vers[19]) != Same {
		t.Fatalf("lost or reordered observation: seq %d", e.DesiredSeq)
	}
	requireVocabulary(t, r.s.Snapshot().Ledger)
}

// No secret value is ever stored, preserved, returned in an error or emitted
// in an event.
func TestS4_SecretValueNeverStoredOrLogged(t *testing.T) {
	r := newRig(t)
	var errTexts []string
	note := func(err error) {
		if err != nil {
			errTexts = append(errTexts, err.Error())
		}
	}
	d1, err := r.v.NewVersion("p", "s", map[string]string{"password": s4Secret})
	note(err)
	_, err = r.v.Observe(s4K, d1, s4Live(2))
	note(err)
	note(r.v.Correlate(s4K, "rot-5ec7e7", 1, d1))
	_, err = r.v.RecordDeliveryVerified(s4K, evOK("rot-5ec7e7", d1))
	note(err)
	// Refusals too.
	_, err = r.v.RecordDeliveryVerified(s4K, Evidence{RotationID: "rot-5ec7e7", Kind: "forged", Version: d1})
	note(err)
	_, err = r.v.NewVersion("", "", map[string]string{"password": s4Secret})
	note(err)
	_, err = r.v.ReconcileFromJournal(JournalSnapshot{Readable: true, Records: []JournalRecord{jrec("rot-5ec7e7", "restart", JournalCompleted, 5, true, &d1)}})
	note(err)
	r.crash()
	writeFile(t, r.dir+"/ledger.json", []byte("{corrupt"), 0o600)
	r.crash() // preserved aside
	ents, _ := readDirNames(r.dir)
	for _, n := range ents {
		b, err := readFileIfRegular(r.dir + "/" + n)
		if err == nil && strings.Contains(string(b), s4Secret) {
			t.Errorf("%s contains the secret value", n)
		}
	}
	for _, e := range errTexts {
		if strings.Contains(e, s4Secret) {
			t.Errorf("an error contains the secret value: %s", e)
		}
	}
	events := 0
	for _, lg := range r.logs {
		lg.mu.Lock()
		for _, ev := range lg.ev {
			events++
			if strings.Contains(ev.Detail, s4Secret) || (ev.Err != nil && strings.Contains(ev.Err.Error(), s4Secret)) {
				t.Errorf("an event contains the secret value: %+v", ev)
			}
		}
		lg.mu.Unlock()
	}
	if events == 0 || len(errTexts) < 2 {
		t.Fatalf("vacuous scan: %d events, %d errors", events, len(errTexts))
	}
	if strings.Contains(snapshot(t, r.s.Snapshot().Ledger), s4Secret) {
		t.Error("the in-memory document contains the secret value")
	}
}

// The closed reason vocabulary of design 10.1 (plus ambiguous_provider).
func TestS4_ClosedReasonVocabulary(t *testing.T) {
	want := []string{"adopted_unknown", "delivery_requested", "delivery_verified", "adoption_unverified_signal", "adoption_unverified_file",
		"interrupted", "identity_changed", "deferred", "rolled_back", "failed", "unreported", "no_rotating_strategy", "strategy_none",
		"eligibility_changed", "journal_unreadable", "ambiguous_provider"}
	got := OutcomeReasons()
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i, w := range want {
		if string(got[i]) != w || !ValidOutcomeReason(w) {
			t.Errorf("%d: %q", i, got[i])
		}
	}
	for _, bad := range []string{"", "health verification failed", "Interrupted", "external_recreation", "partial"} {
		if ValidOutcomeReason(bad) {
			t.Errorf("%q must not be in the vocabulary", bad)
		}
	}
	got[0] = "mutated"
	if OutcomeReasons()[0] != WhyAdoptedUnknown {
		t.Error("OutcomeReasons must return a copy")
	}
}
