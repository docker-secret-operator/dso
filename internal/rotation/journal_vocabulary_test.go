package rotation

import "testing"

// Batch A / step S3 (A-02, A-14): the persisted vocabulary. These strings are written to
// rotations.json and read by other binaries, so a rename is a compatibility decision,
// not a refactor. The twelve ManualReason codes are approved vocabulary, not a
// validation list (the journal stores any string).
func TestJournalVocabulary_AttemptBeginAndManualReasonCodes(t *testing.T) {
	if TrAttemptBegin != "attempt_begin" {
		t.Fatalf("TrAttemptBegin = %q", TrAttemptBegin)
	}
	want := map[string]string{
		"ManualReasonNoWorkload":                      "no_workload",
		"ManualReasonArtifactIdentity":                "artifact_identity",
		"ManualReasonUnresolvedIntent":                "unresolved_intent",
		"ManualReasonRollingBetweenRenames":           "rolling_between_renames",
		"ManualReasonRollingAfterSwapBeforeCommitAck": "rolling_after_swap_before_commit_ack",
		"ManualReasonRecoveryNotConverging":           "recovery_not_converging",
		"ManualReasonIdentityConflict":                "identity_conflict",
		"ManualReasonPlanRefused":                     "plan_refused",
		"ManualReasonLayoutInconsistent":              "layout_inconsistent",
		"ManualReasonRollbackFailed":                  "rollback_failed",
		"ManualReasonStateUnknown":                    "state_unknown",
		"ManualReasonCandidateNotRemoved":             "candidate_not_removed",
	}
	got := map[string]string{
		"ManualReasonNoWorkload":                      ManualReasonNoWorkload,
		"ManualReasonArtifactIdentity":                ManualReasonArtifactIdentity,
		"ManualReasonUnresolvedIntent":                ManualReasonUnresolvedIntent,
		"ManualReasonRollingBetweenRenames":           ManualReasonRollingBetweenRenames,
		"ManualReasonRollingAfterSwapBeforeCommitAck": ManualReasonRollingAfterSwapBeforeCommitAck,
		"ManualReasonRecoveryNotConverging":           ManualReasonRecoveryNotConverging,
		"ManualReasonIdentityConflict":                ManualReasonIdentityConflict,
		"ManualReasonPlanRefused":                     ManualReasonPlanRefused,
		"ManualReasonLayoutInconsistent":              ManualReasonLayoutInconsistent,
		"ManualReasonRollbackFailed":                  ManualReasonRollbackFailed,
		"ManualReasonStateUnknown":                    ManualReasonStateUnknown,
		"ManualReasonCandidateNotRemoved":             ManualReasonCandidateNotRemoved,
	}
	seen := map[string]string{}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q", name, got[name], w)
		}
		if other, dup := seen[w]; dup {
			t.Errorf("%s and %s share the value %q", name, other, w)
		}
		seen[w] = name
	}
	if len(want) != 12 {
		t.Fatalf("the approved vocabulary has twelve codes, the table lists %d", len(want))
	}
	// The event carries the attempt number and the reason as plain fields.
	e := JournalEvent{Attempt: 2, ManualReason: ManualReasonStateUnknown}
	if e.Attempt != 2 || e.ManualReason != "state_unknown" {
		t.Fatalf("event fields = %+v", e)
	}
}
