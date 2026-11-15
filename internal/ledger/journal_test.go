package ledger

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func journalLedger(state RecoveryState, planned int, done []ActionResult) *Ledger {
	l := vecHeader(vecPlaceholderKeyID)
	r := RecoveryRecord{RecoveryID: strings.Repeat("e", 32), Scope: RecoveryScope{Project: "p", Service: "s"}, Trigger: TriggerOrphanScan,
		Done: done, State: state, StartedAt: t0}
	for i := 0; i < planned; i++ {
		r.Planned = append(r.Planned, vecAction([]ActionOp{ActionStop, ActionRemove, ActionStart}[i%3], fmt.Sprintf("c-%d", i), fmt.Sprintf("n-%d_dso_new_", i)))
	}
	if state == RecoveryDone || state == RecoveryFailed {
		r.FinishedAt = t0
	}
	l.Recoveries = []RecoveryRecord{r}
	return l
}

var (
	resOK      = ActionResult{Outcome: ActionOK}
	resAlready = ActionResult{Outcome: ActionAlreadyDone}
	resSkipped = ActionResult{Outcome: ActionSkippedMismatch, Reason: "container id changed"}
	resFailed  = ActionResult{Outcome: ActionFailedOp, Reason: "docker error"}
)

// T-U38 (design §18): the recovery-journal result semantics that Validate can
// check.
func TestRecoveryJournal_ResultSemantics(t *testing.T) {
	type tc struct {
		name  string
		state RecoveryState
		plan  int
		done  []ActionResult
		ok    bool
		want  string
	}
	cases := []tc{
		// outcomes
		{"ok", RecoveryInProgress, 1, []ActionResult{resOK}, true, ""},
		{"already_done", RecoveryInProgress, 1, []ActionResult{resAlready}, true, ""},
		{"skipped_mismatch with a reason", RecoveryInProgress, 1, []ActionResult{resSkipped}, true, ""},
		{"failed as the last result", RecoveryInProgress, 1, []ActionResult{resFailed}, true, ""},
		{"skipped_mismatch without a reason", RecoveryInProgress, 1, []ActionResult{{Outcome: ActionSkippedMismatch}}, false, "reason"},
		{"unknown outcome skipped", RecoveryInProgress, 1, []ActionResult{{Outcome: "skipped", Reason: "x"}}, false, "outcome"},
		{"unknown outcome empty", RecoveryInProgress, 1, []ActionResult{{}}, false, "outcome"},
		// done may contain skipped actions (§18.2)
		{"done with every kind of success", RecoveryDone, 3, []ActionResult{resOK, resAlready, resSkipped}, true, ""},
		{"done with only skipped results", RecoveryDone, 2, []ActionResult{resSkipped, resSkipped}, true, ""},
		{"done with a failed result", RecoveryDone, 2, []ActionResult{resOK, resFailed}, false, "done"},
		{"done missing a result", RecoveryDone, 2, []ActionResult{resOK}, false, "done"},
		{"done with zero planned actions", RecoveryDone, 0, nil, true, ""},
		// prefix and ordering (§18.4)
		{"a prefix of the plan", RecoveryInProgress, 3, []ActionResult{resOK, resSkipped}, true, ""},
		{"no results yet", RecoveryInProgress, 3, nil, true, ""},
		{"more results than planned actions", RecoveryInProgress, 1, []ActionResult{resOK, resOK}, false, "done"},
		{"failed in the middle of the prefix", RecoveryInProgress, 3, []ActionResult{resOK, resFailed, resOK}, false, "last result"},
		{"failed first of several", RecoveryInProgress, 3, []ActionResult{resFailed, resOK}, false, "last result"},
		{"two failed results", RecoveryInProgress, 3, []ActionResult{resFailed, resFailed}, false, "last result"},
		// retry overwrites in place; an appended retry is invalid (§18.5)
		{"retry succeeded in place", RecoveryInProgress, 2, []ActionResult{resOK}, true, ""},
		{"retry appended after a failed result", RecoveryInProgress, 1, []ActionResult{resFailed, resOK}, false, "done"},
		{"appended retry inside the plan length", RecoveryInProgress, 3, []ActionResult{resFailed, resOK}, false, "last result"},
		// terminal failed record keeps its last failed result
		{"failed record ends with a failed result", RecoveryFailed, 2, []ActionResult{resOK, resFailed}, true, ""},
		{"failed record with no results", RecoveryFailed, 2, nil, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := journalLedger(c.state, c.plan, c.done).Validate()
			if c.ok && err != nil {
				t.Fatalf("expected a valid record: %v", err)
			}
			if !c.ok {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if !strings.Contains(err.Error(), c.want) {
					t.Fatalf("error %q does not mention %q", err, c.want)
				}
			}
			if c.ok {
				if _, err := Marshal(journalLedger(c.state, c.plan, c.done)); err != nil {
					t.Fatalf("a valid record must also marshal: %v", err)
				}
			}
		})
	}
	// The outcome vocabulary is exactly the four values of §18.1.
	for _, o := range []ActionOutcome{ActionOK, ActionAlreadyDone, ActionFailedOp, ActionSkippedMismatch} {
		if !validActionOutcome(o) {
			t.Errorf("%q must be a valid outcome", o)
		}
	}
	for _, o := range []ActionOutcome{"", "skipped", "SKIPPED_MISMATCH", "skipped-mismatch", "done", "interrupted"} {
		if validActionOutcome(o) {
			t.Errorf("%q must not be a valid outcome", o)
		}
	}
	// A skipped result keeps its reason through a round trip, and the reason is
	// subject to the usual control-character rule.
	rt, err := Unmarshal(mustMarshal(t, journalLedger(RecoveryDone, 1, []ActionResult{resSkipped})))
	if err != nil || rt.Recoveries[0].Done[0] != resSkipped {
		t.Fatalf("a skipped result must round-trip: %v", err)
	}
	if err := journalLedger(RecoveryDone, 1, []ActionResult{{Outcome: ActionSkippedMismatch, Reason: "bad\nreason"}}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("a control character in the reason must be rejected, got %v", err)
	}
}

// blocked_persistence stays in the persisted version-2 RecoveryState enum
// (design §43.2; owner decision Q3b: keep).
func TestBlockedPersistenceRemainsInTheVersion2Enum(t *testing.T) {
	if RecoveryBlockedPersistence != "blocked_persistence" || !validRecoveryState(RecoveryBlockedPersistence) {
		t.Fatal("blocked_persistence must remain a valid RecoveryState")
	}
	l := journalLedger(RecoveryBlockedPersistence, 2, nil)
	out := mustMarshal(t, l)
	if !strings.Contains(string(out), `"state":"blocked_persistence"`) {
		t.Fatal("a blocked_persistence record must serialize")
	}
	rt, err := Unmarshal(out)
	if err != nil || rt.Recoveries[0].State != RecoveryBlockedPersistence {
		t.Fatalf("a blocked_persistence record must round-trip: %v", err)
	}
	// It is not an open (non-terminal) record and has no finish time.
	l2 := journalLedger(RecoveryBlockedPersistence, 1, nil)
	second := l2.Recoveries[0]
	second.RecoveryID = strings.Repeat("f", 32)
	second.State = RecoveryPlanned
	l2.Recoveries = append(l2.Recoveries, second)
	if err := l2.Validate(); err != nil {
		t.Errorf("a planned record may coexist with a blocked_persistence record of the same scope: %v", err)
	}
	l3 := journalLedger(RecoveryBlockedPersistence, 1, nil)
	l3.Recoveries[0].FinishedAt = t0
	if err := l3.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("a blocked_persistence record has no finish time, got %v", err)
	}
}

// partialEntry builds a partial entry whose replicas have the given outcomes.
func replicaCombo(outs []ReplicaOutcome) *Ledger {
	va, vb := fxVer0("1"), fxVer0("2")
	e := Entry{Key: Key{Project: "shop", Service: "web", Provider: "aws", Secret: "tls_cert"}, Desired: vb, DesiredSeq: 2, Applied: va, State: EntryPartial}
	for i, o := range outs {
		e.Replicas = append(e.Replicas, replicaAt(fmt.Sprint(i+1), fmt.Sprintf("c-%d", i), fmt.Sprintf("shop-web-%d", i+1), va, o))
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

// T-U39 rows Q1 to Q4 (design §19.5, §32.3).
func TestQuestionableStates_FinalRulings(t *testing.T) {
	// Q1: a recovery done with skipped actions is valid.
	if err := journalLedger(RecoveryDone, 2, []ActionResult{resSkipped, resOK}).Validate(); err != nil {
		t.Errorf("Q1: %v", err)
	}
	// Q2: a success with a pending replica is a valid partial; with a blocked one
	// it is idle or pending, not partial.
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
	// Q3: an unresolved intent with an entry state other than in_progress is
	// invalid, and in_progress without an intent is invalid (invariant I-1).
	base := sampleLedger(t)
	intent := base.Entries[4].Rotation
	for _, st := range []EntryState{EntryPending, EntryBlocked, EntryPartial, EntryIdle, EntryFailed, EntryRolledBack, EntryAdopted} {
		l := sampleLedger(t)
		l.Entries[4].State = st
		l.Entries[4].Rotation = intent
		if st == EntryPartial {
			l.Entries[4].Replicas = append(l.Entries[4].Replicas, replicaAt("2", "d00dfeee", "legacy-batch-2", vp(*l.Entries[4].Applied), ReplicaFailed))
		}
		if err := l.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Q3: an intent with state %s must be rejected, got %v", st, err)
		}
	}
	l := sampleLedger(t)
	l.Entries[4].Rotation = nil
	if err := l.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Q3: in_progress without an intent must be rejected, got %v", err)
	}
	// Q4: a retried recovery action stored as an appended result is invalid.
	if err := journalLedger(RecoveryInProgress, 1, []ActionResult{resFailed, resOK}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Q4: %v", err)
	}
}

// T-U43 (design §43.2): the persisted vocabularies of schema version 2.
func TestVersion2Vocabularies(t *testing.T) {
	universe := []string{
		"", "adopted", "idle", "pending", "in_progress", "partial", "failed", "rolled_back", "blocked", "succeeded",
		"requested", "dispatched", "superseded", "cancelled", "state_tracker", "ledger_intent", "orphan_scan",
		"planned", "done", "blocked_persistence", "stop", "remove", "start", "ok", "already_done", "skipped_mismatch",
		"skipped", "SKIPPED_MISMATCH", "blocked-persistence", "unblocked", "kill", "cron", "paused", "done ", " idle",
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
		"adopted", "idle", "pending", "in_progress", "partial", "failed", "rolled_back", "blocked")
	check("ReplicaOutcome", func(s string) bool { return validReplicaOutcome(ReplicaOutcome(s)) },
		"adopted", "succeeded", "rolled_back", "failed", "blocked", "pending")
	check("RedeliveryState", func(s string) bool { return validRedeliveryState(RedeliveryState(s)) },
		"requested", "dispatched", "succeeded", "failed", "superseded", "cancelled")
	check("RecoveryTrigger", func(s string) bool { return validTrigger(RecoveryTrigger(s)) },
		"state_tracker", "ledger_intent", "orphan_scan")
	check("RecoveryState", func(s string) bool { return validRecoveryState(RecoveryState(s)) },
		"planned", "in_progress", "done", "failed", "blocked_persistence")
	check("ActionOp", func(s string) bool { return validOp(ActionOp(s)) }, "stop", "remove", "start")
	check("ActionOutcome", func(s string) bool { return validActionOutcome(ActionOutcome(s)) },
		"ok", "already_done", "failed", "skipped_mismatch")
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
