package harness

import (
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// The identity-based exception to only-artifacts-removed (Stage C, owner-approved). The
// shapes below are the ones observed on hosted CI for the restart strategy: the N21
// rollback restores the original under the real name and the interrupted rotation's
// candidate -- which held the real service name at the crash -- is moved aside and
// retired. Every negative case must still be reported.

const stateRolled = "rollback_container_restored_previous_secret_unavailable"

// rbShape is one restart crash/recovery pair; each test mutates a copy.
type rbShape struct {
	w           World
	crashTr     string
	candInList  bool // the journal lists the candidate's container ID
	candLabel   string
	candOwner   string
	finalTr     string
	finalState  string
	origRunning bool
	origName    string
	finalRID    string
	crashRID    string
}

func rbDefault() rbShape {
	return rbShape{
		w:          World{Service: "app", OwnerID: owner, Strategy: Restart, OriginalID: "orig", Bystanders: []string{"by"}, Foreign: []string{"foreign"}},
		crashTr:    "intent_start_replacement",
		candInList: true, candLabel: "rot", candOwner: owner,
		finalTr: "rolled_back", finalState: stateRolled,
		origRunning: true, origName: "app", finalRID: "rot", crashRID: "rot",
	}
}

func (s rbShape) snapshots(extraCrash ...CView) (Snapshot, Snapshot) {
	cand := CView{ID: "cand", Name: "app", Running: true, Owner: s.candOwner, RotationID: s.candLabel}
	crashRec := rec(s.crashTr, "in_progress", func(r *RotationRecord) {
		r.Strategy, r.RotationID = "restart", s.crashRID
		if s.candInList {
			r.CandidateContainerIDs = []string{"cand"}
		}
	})
	finalRec := rec(s.finalTr, "critical_error", func(r *RotationRecord) {
		r.Strategy, r.RotationID, r.OperationState = "restart", s.finalRID, s.finalState
		if s.candInList {
			r.CandidateContainerIDs = []string{"cand"}
		}
	})
	crash := snap(crashRec, nil, append([]CView{c("orig", "app_old_1", false), cand}, extraCrash...)...)
	final := snap(finalRec, nil, CView{ID: "orig", Name: s.origName, Running: s.origRunning, Owner: owner})
	return crash, final
}

func (s rbShape) check(extraCrash ...CView) []Violation {
	crash, final := s.snapshots(extraCrash...)
	return CheckRecovery(s.w, pt("restart:after_intent_start"), crash, final)
}

func hasRule(vs []Violation, rule string) bool {
	for _, v := range vs {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

func TestRemovalIsRolledBackCandidate_ObservedShapesPass(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*rbShape)
	}{
		{"restart-after-intent-start", func(s *rbShape) { s.crashTr = "intent_start_replacement" }},
		{"restart-after-intent-stop", func(s *rbShape) { s.crashTr = "intent_stop_original" }},
		{"restart-after-stop-before-ack", func(s *rbShape) { s.crashTr = "intent_stop_original" }},
		{"state-after-persist-rename#4", func(s *rbShape) { s.crashTr = "candidate_created" }},
		// The candidate ID was never journalled (the crash preceded the ack); only the
		// rotation label identifies it.
		{"restart-after-create-before-ack (label only)", func(s *rbShape) { s.crashTr = "intent_create_candidate"; s.candInList = false }},
		{"state-before-persist-rename#4 (label only)", func(s *rbShape) { s.crashTr = "intent_create_candidate"; s.candInList = false }},
		{"restart-after-start-before-ack", func(s *rbShape) { s.crashTr = "intent_start_replacement" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := rbDefault()
			tc.mod(&s)
			if vs := s.check(); len(vs) != 0 {
				t.Fatalf("an observed, legitimate N21 retirement was reported:\n%s", FormatViolations(vs))
			}
		})
	}
}

func TestRemovalIsRolledBackCandidate_Negatives(t *testing.T) {
	type neg struct {
		name string
		mod  func(*rbShape)
		rule string // the rule that must still fire
	}
	cases := []neg{
		{"1 service-named removal with no rotation identity", func(s *rbShape) { s.candInList = false; s.candLabel = "" }, "only-artifacts-removed"},
		{"2 different (stale) rotation id", func(s *rbShape) { s.candLabel = "other-rotation" }, "only-artifacts-removed"},
		{"2b different rotation id even though the id is journalled", func(s *rbShape) { s.candInList = true; s.candLabel = "other-rotation" }, "only-artifacts-removed"},
		{"3 original not running after recovery", func(s *rbShape) { s.origRunning = false }, "only-artifacts-removed"},
		{"4 original not holding the real service name", func(s *rbShape) { s.origName = "app_old_1" }, "only-artifacts-removed"},
		{"5a no final rolled_back record (transition)", func(s *rbShape) { s.finalTr = "intent_rollback" }, "only-artifacts-removed"},
		{"5b rolled_back without a restored-container operation state", func(s *rbShape) { s.finalState = "" }, "only-artifacts-removed"},
		{"6 rolling strategy", func(s *rbShape) { s.w.Strategy = Rolling }, "only-artifacts-removed"},
		{"7 signal strategy", func(s *rbShape) { s.w.Strategy = Signal }, "only-artifacts-removed"},
		{"9a crash transition candidate_ready", func(s *rbShape) { s.crashTr = "candidate_ready" }, "only-artifacts-removed"},
		{"9b crash transition after candidate_ready", func(s *rbShape) { s.crashTr = "intent_cleanup" }, "only-artifacts-removed"},
		{"crash and final records are different operations", func(s *rbShape) { s.crashRID, s.finalRID = "rot-A", "rot-B"; s.candLabel = "rot-B" }, "only-artifacts-removed"},
		{"no operation identity in the final record", func(s *rbShape) { s.finalRID = ""; s.crashRID = "" }, "only-artifacts-removed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := rbDefault()
			tc.mod(&s)
			if vs := s.check(); !hasRule(vs, tc.rule) {
				t.Fatalf("the removal was accepted although it must still be reported (%s); got: %q", tc.rule, rules(vs))
			}
		})
	}

	t.Run("8 the original itself is removed", func(t *testing.T) {
		s := rbDefault()
		crash, final := s.snapshots()
		// The original is removed instead of the candidate; it was named like the service
		// at the crash (so the name rule cannot excuse it).
		for i := range crash.Containers {
			if crash.Containers[i].ID == "orig" {
				crash.Containers[i].Name = "app"
			}
		}
		final.Containers = baseContainers()
		vs := CheckRecovery(s.w, pt("restart:after_intent_start"), crash, final)
		if !hasRule(vs, "only-artifacts-removed") || !hasRule(vs, "workload-survives") {
			t.Fatalf("removing the original must be reported; got %q", rules(vs))
		}
		if removalIsRolledBackCandidate(s.w, crash, final, CView{ID: "orig", Name: "app", Owner: owner, RotationID: "rot"}) {
			t.Fatal("the original was accepted as a rolled-back candidate")
		}
	})

	t.Run("10 foreign-owned candidate", func(t *testing.T) {
		s := rbDefault()
		s.candOwner = "owner-B"
		crash, final := s.snapshots()
		if removalIsRolledBackCandidate(s.w, crash, final, CView{ID: "cand", Name: "app", Owner: "owner-B", RotationID: "rot"}) {
			t.Fatal("a foreign-owned container was accepted as this agent's candidate")
		}
		// And a foreign-owned container the harness knows about is still protected.
		s.w.Foreign = append(s.w.Foreign, "cand")
		vs := CheckRecovery(s.w, pt("restart:after_intent_start"), crash, final)
		if !hasRule(vs, "bystander-untouched") {
			t.Fatalf("removing a foreign-owned container must be reported; got %q", rules(vs))
		}
	})

	t.Run("11 legitimate candidate plus an unrelated removed service container", func(t *testing.T) {
		s := rbDefault()
		unrelated := CView{ID: "stranger", Name: "app", Running: true, Owner: owner} // no rotation identity, not journalled
		crash, final := s.snapshots(unrelated)
		vs := CheckRecovery(s.w, pt("restart:after_intent_start"), crash, final)
		var only []Violation
		for _, v := range vs {
			if v.Rule == "only-artifacts-removed" {
				only = append(only, v)
			}
		}
		if len(only) != 1 || !strings.Contains(only[0].Detail, short("stranger")) || strings.Contains(only[0].Detail, short("cand")) {
			t.Fatalf("want exactly one only-artifacts-removed violation, for the unrelated container; got %v", only)
		}
	})
}

// Existing behaviour that must not change.
func TestOnlyArtifactsRemoved_NameRuleUnchanged(t *testing.T) {
	s := rbDefault()
	crash, final := s.snapshots()
	// An artifact-named removal is fine without any rollback record.
	crash.Containers = append(crash.Containers, c("bk", "app_dso_backup_9", false))
	s.finalTr, s.finalState = "intent_rollback", ""
	_, final = s.snapshots()
	final.Containers = baseContainers()
	final.Containers = append(final.Containers, CView{ID: "orig", Name: "app", Running: true, Owner: owner})
	crash2, _ := s.snapshots(c("bk", "app_dso_backup_9", false))
	vs := CheckRecovery(s.w, pt("restart:after_intent_start"), crash2, final)
	for _, v := range vs {
		if v.Rule == "only-artifacts-removed" && strings.Contains(v.Detail, short("bk")) {
			t.Fatalf("an artifact-named removal was reported: %s", v)
		}
	}
}

// rollbackScope is a copy of what the agent's rollbackTransition accepts; make a typo or a
// drift against the real transition names fail here.
func TestRollbackScopeMatchesRotationConstants(t *testing.T) {
	want := []string{
		rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated,
		rotation.TrIntentStopOriginal, rotation.TrOriginalStopped,
		rotation.TrIntentStartReplacement, rotation.TrReplacementStarted, rotation.TrIntentRollback,
		rotation.TrGateStarted, rotation.TrIntentDeliverSecrets, rotation.TrDeliveryPartial,
		rotation.TrSecretsDelivered, rotation.TrIntentReleaseGate, rotation.TrGateReleased,
	}
	if len(want) != len(rollbackScope) {
		t.Fatalf("rollbackScope has %d entries, the agent's scope has %d", len(rollbackScope), len(want))
	}
	for _, tr := range want {
		if !rollbackScope[tr] {
			t.Errorf("%q is in the agent's rollback scope but not in the harness's", tr)
		}
	}
	for _, later := range []string{rotation.TrCandidateReady, rotation.TrIntentHandoff, rotation.TrIntentSwap, rotation.TrSwapCommitted, rotation.TrIntentCleanup, rotation.TrCompleted, rotation.TrRolledBack} {
		if rollbackScope[later] {
			t.Errorf("%q must not be inside the rollback scope", later)
		}
	}
}
