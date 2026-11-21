package ledger

import (
	"errors"
	"fmt"
	"testing"
)

// The recovery-journal result semantics tests (T-U38, blocked_persistence,
// Q1, Q3, Q4) were removed with the recovery journal in schema version 3
// (design section 1.2, owner decision O-1).

// partialEntry builds a partial entry whose replicas have the given outcomes.
func replicaCombo(outs []ReplicaOutcome) *Ledger {
	va, vb := fxVer0("1"), fxVer0("2")
	e := Entry{Key: Key{Project: "shop", Service: "web", Provider: "aws", Secret: "tls_cert"}, Desired: vb, DesiredSeq: 2, Applied: va, State: EntryPartial}
	for i, o := range outs {
		applied := va
		if o == ReplicaAdopted {
			applied = nil // an adopted replica holds no applied version (O-4)
		}
		e.Replicas = append(e.Replicas, replicaAt(fmt.Sprint(i+1), fmt.Sprintf("c-%d", i), fmt.Sprintf("shop-web-%d", i+1), applied, o))
	}
	l := vecHeader(vecPlaceholderKeyID)
	l.HMACKeyID = derivedKeyID(fixtureKey)
	l.Entries = []Entry{e}
	return l
}

func fxVer0(p string) *Version {
	v, err := NewHMACVersion(derivedKeyID(fixtureKey), fixtureKey, "aws", "db", map[string]string{"p": p})
	if err != nil {
		panic(err)
	}
	return &v
}

var allReplicaOutcomes = []ReplicaOutcome{ReplicaAdopted, ReplicaSucceeded, ReplicaRolledBack, ReplicaFailed, ReplicaBlocked, ReplicaPending}

// partialRule is the rule of design §27.3, written independently of the
// validator: at least one succeeded replica and at least one failed,
// rolled_back or pending replica; blocked and adopted replicas neither satisfy
// nor prevent it.
func partialRule(outs []ReplicaOutcome) bool {
	s, o := 0, 0
	for _, x := range outs {
		switch x {
		case ReplicaSucceeded:
			s++
		case ReplicaFailed, ReplicaRolledBack, ReplicaPending:
			o++
		}
	}
	return s >= 1 && o >= 1
}

// previousPartialRule is the rule of S1 as built (failed or rolled_back only).
func previousPartialRule(outs []ReplicaOutcome) bool {
	s, o := 0, 0
	for _, x := range outs {
		switch x {
		case ReplicaSucceeded:
			s++
		case ReplicaFailed, ReplicaRolledBack:
			o++
		}
	}
	return s >= 1 && o >= 1
}

func eachCombo(n int, f func([]ReplicaOutcome)) {
	var rec func(prefix []ReplicaOutcome)
	rec = func(prefix []ReplicaOutcome) {
		if len(prefix) == n {
			f(append([]ReplicaOutcome(nil), prefix...))
			return
		}
		for _, o := range allReplicaOutcomes {
			rec(append(prefix, o))
		}
	}
	rec(nil)
}

// T-U39 (design §19.2, §27.3, §19.5 Q2): the widened partial rule, over every
// combination of replica outcomes.
func TestPartialRule_AllReplicaOutcomeCombinations(t *testing.T) {
	want := map[int]struct{ combos, accepted, previously int }{1: {6, 0, 0}, 2: {36, 6, 4}, 3: {216, 72, 54}}
	for n := 1; n <= 3; n++ {
		combos, accepted, previously, newly, lost, idleOK := 0, 0, 0, 0, 0, 0
		eachCombo(n, func(outs []ReplicaOutcome) {
			combos++
			l := replicaCombo(outs)
			ok := l.Validate() == nil
			if ok != partialRule(outs) {
				t.Errorf("%v: partial accepted=%v, rule says %v", outs, ok, partialRule(outs))
			}
			if ok {
				accepted++
			}
			if previousPartialRule(outs) {
				previously++
				if !ok {
					lost++
				}
			}
			if ok && !previousPartialRule(outs) {
				newly++
			}
			// A success next to blocked replicas needs no partial: idle (and
			// pending) accept every combination.
			for _, st := range []EntryState{EntryIdle, EntryPending} {
				li := replicaCombo(outs)
				li.Entries[0].State = st
				if li.Validate() == nil {
					idleOK++
				}
			}
		})
		w := want[n]
		if combos != w.combos || accepted != w.accepted || previously != w.previously {
			t.Errorf("n=%d: combinations %d, accepted %d, previously %d; design §27.3 says %d, %d, %d", n, combos, accepted, previously, w.combos, w.accepted, w.previously)
		}
		if lost != 0 {
			t.Errorf("n=%d: %d combinations accepted before are now rejected", n, lost)
		}
		if n >= 2 && newly != w.accepted-w.previously {
			t.Errorf("n=%d: newly accepted %d, want %d", n, newly, w.accepted-w.previously)
		}
		if idleOK != 2*combos {
			t.Errorf("n=%d: idle/pending must accept every combination, accepted %d of %d", n, idleOK, 2*combos)
		}
	}
	// Named rows.
	row := func(outs ...ReplicaOutcome) error { return replicaCombo(outs).Validate() }
	if row(ReplicaSucceeded, ReplicaPending) != nil {
		t.Error("a success with a pending replica is a valid partial entry (Q2)")
	}
	if err := row(ReplicaSucceeded, ReplicaBlocked); err == nil {
		t.Error("a success with only a blocked replica is not partial")
	}
	if row(ReplicaSucceeded, ReplicaAdopted) == nil {
		t.Error("a success with only an adopted replica is not partial")
	}
	if row(ReplicaFailed, ReplicaPending) == nil {
		t.Error("partial needs a success")
	}
}

// T-U39 row Q2 (design §19.5, §32.3): a success with a pending replica is a
// valid partial; with a blocked one it is idle or pending, not partial.
func TestQuestionableStates_FinalRulings(t *testing.T) {
	if err := replicaCombo([]ReplicaOutcome{ReplicaSucceeded, ReplicaPending}).Validate(); err != nil {
		t.Errorf("Q2 partial: %v", err)
	}
	blocked := replicaCombo([]ReplicaOutcome{ReplicaSucceeded, ReplicaBlocked})
	if blocked.Validate() == nil {
		t.Error("Q2: success + blocked must not be accepted as partial")
	}
	blocked.Entries[0].State = EntryIdle
	if err := blocked.Validate(); err != nil {
		t.Errorf("Q2: success + blocked as idle: %v", err)
	}
	// The retired in_progress state is rejected whatever the replicas say.
	l := sampleLedger(t)
	l.Entries[4].State = "in_progress"
	if err := l.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("in_progress is not a schema 3 state, got %v", err)
	}
}

// T-U43 (design §43.2, amended for schema 3): the persisted vocabularies.
func TestVersion3Vocabularies(t *testing.T) {
	universe := []string{
		"", "adopted", "idle", "pending", "in_progress", "partial", "failed", "rolled_back", "blocked", "succeeded",
		"requested", "dispatched", "superseded", "cancelled", "none", "verified", "unverified", "unknown",
		"yes", "VERIFIED", "done ", " idle",
	}
	check := func(name string, accepts func(string) bool, want ...string) {
		t.Helper()
		wantSet := map[string]bool{}
		for _, w := range want {
			wantSet[w] = true
		}
		got := map[string]bool{}
		for _, u := range universe {
			if accepts(u) {
				got[u] = true
			}
		}
		for w := range wantSet {
			if !got[w] {
				t.Errorf("%s: %q must be accepted", name, w)
			}
		}
		for g := range got {
			if !wantSet[g] {
				t.Errorf("%s: %q must not be accepted", name, g)
			}
		}
	}
	check("EntryState", func(s string) bool { return validEntryState(EntryState(s)) },
		"adopted", "idle", "pending", "partial", "failed", "rolled_back", "blocked")
	check("ReplicaOutcome", func(s string) bool { return validReplicaOutcome(ReplicaOutcome(s)) },
		"adopted", "succeeded", "rolled_back", "failed", "blocked", "pending")
	check("RedeliveryState", func(s string) bool { return validRedeliveryState(RedeliveryState(s)) },
		"requested", "dispatched", "succeeded", "failed", "superseded", "cancelled")
	check("DeliveryState", func(s string) bool { return validDelivery(DeliveryState(s)) },
		"none", "requested", "verified")
	check("AdoptionState", func(s string) bool { return validAdoption(AdoptionState(s)) },
		"unknown", "unverified", "verified")
	// VersionKind: provider and hmac only.
	for _, k := range []VersionKind{KindProvider, KindHMAC} {
		if string(k) != "provider" && string(k) != "hmac" {
			t.Errorf("unexpected kind %q", k)
		}
	}
	if err := (Version{Kind: "sha256", ID: "x"}).validate("v", ""); err == nil {
		t.Error("an unknown version kind must be rejected")
	}
}
