package cli

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestCentralAuditLogger_UsesOverrideWhenSet and
// TestCentralAuditLogger_FallsBackToARealLoggerWhenNoOverride cover the
// smallest relevant unit for F1's final remediation: the seam
// `dso central init` now uses to report audit-write failures through the
// repository's structured (zap) logging convention, instead of the earlier
// plain-stderr text that an independent audit found did not satisfy the
// approved "existing structured logging conventions, at error level"
// requirement (Step 5.3 decision-resolution, Gate 6).
//
// Limitation, documented rather than worked around: forcing a real
// AppendAuditEvent failure from inside a live `dso central init` run would
// require making the concrete *sqlite.Store centralInitCmd constructs
// swappable for a failing fake -- a larger refactor of centralInitCmd's
// signature than this remediation's scope. The end-to-end logging content
// (that a captured zap entry is error-level, contains the outcome/error
// fields, and never a secret value) is already verified for every other
// audit operation by internal/centralsvc/audit_test.go's
// TestAudit_BestEffortWriteFailureIsLoggedAndDoesNotBlockTheRead and
// TestAudit_TransactionalWriteFailureRollsBackAndIsLogged, which exercise
// the identical zap.Logger.Error(...) call shape recordInitEvent now uses.
// This test instead proves the one piece that's actually new and specific
// to centralInitCmd: that its logger-selection seam works.
func TestCentralAuditLogger_UsesOverrideWhenSet(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	fake := zap.New(core)

	centralAuditLoggerOverride = fake
	defer func() { centralAuditLoggerOverride = nil }()

	got := centralAuditLogger()
	if got != fake {
		t.Fatal("expected centralAuditLogger to return the override when set")
	}
	got.Error("probe", zap.String("k", "v"))
	if logs.Len() != 1 || logs.All()[0].Level.String() != "error" {
		t.Fatal("expected the override logger to actually receive the log entry")
	}
}

func TestCentralAuditLogger_FallsBackToARealLoggerWhenNoOverride(t *testing.T) {
	centralAuditLoggerOverride = nil
	got := centralAuditLogger()
	if got == nil {
		t.Fatal("expected a non-nil logger when no override is set")
	}
	// Must not panic, confirming it's a real, usable *zap.Logger (whether
	// zap.NewProduction succeeded or the zap.NewNop fallback was used).
	got.Error("probe (discarded or written to the process's real stderr/log sink)")
}
