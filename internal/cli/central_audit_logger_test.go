package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/centralsvc"
	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
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

// TestCentralAuditLogger_FiltersBelowErrorLevel is a regression test for
// the CLI-logger decision: centralAuditLogger is now shared by every `dso
// central` command (previously `init` alone), each of which also emits
// routine Info-level logging through internal/centralsvc (e.g. "secret
// read" on every successful reveal). The approved scope is surfacing
// best-effort audit-write FAILURES, not general operational chatter, so
// this must filter to error level regardless of the override mechanism --
// checked here against the real logger's own configured core, not the
// test override (which deliberately accepts any level for capture).
func TestCentralAuditLogger_FiltersBelowErrorLevel(t *testing.T) {
	centralAuditLoggerOverride = nil
	got := centralAuditLogger()
	if ce := got.Check(zap.InfoLevel, "probe"); ce != nil {
		t.Fatal("expected centralAuditLogger's real logger to filter out Info-level entries")
	}
	if ce := got.Check(zap.ErrorLevel, "probe"); ce == nil {
		t.Fatal("expected centralAuditLogger's real logger to accept Error-level entries")
	}
}

// failingAuditStore wraps a real store.Store and makes only AppendAuditEvent
// fail, so a best-effort audit write genuinely fails against the real
// database (lookup, validation, everything else goes through the real
// store) rather than a higher-level mock of the service itself. Mirrors
// internal/centralsvc's own test-only type of the same name and purpose.
type failingAuditStore struct {
	store.Store
}

func (failingAuditStore) AppendAuditEvent(ctx context.Context, ev store.AuditEvent) (store.AuditEvent, error) {
	return store.AuditEvent{}, errors.New("synthetic audit-store failure (test-injected)")
}

// TestCentralAuditLogger_EndToEndFailureIsLoggedAtErrorLevelWithNoSecretLeak
// proves the actual wiring this round's fix is about, not just the logger's
// own level filter in isolation: a real best-effort audit-write failure,
// routed through centralAuditLogger() exactly as (*centralFlags).open and
// centralInitCmd now call it, must (1) leave the business operation's own
// result unaffected (A-2's availability-first guarantee), (2) produce an
// error-level log entry, and (3) never let any secret, plaintext, or root-
// key material reach that log entry.
func TestCentralAuditLogger_EndToEndFailureIsLoggedAtErrorLevelWithNoSecretLeak(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatalf("test setup: init failed: %v", err)
	}

	st, err := sqlite.Open(context.Background(), e.db, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	kp, err := crypto.NewFileKeyProvider(e.key)
	if err != nil {
		t.Fatal(err)
	}

	core, logs := observer.New(zap.DebugLevel)
	centralAuditLoggerOverride = zap.New(core)
	defer func() { centralAuditLoggerOverride = nil }()

	// centralAuditLogger(), exactly as the production call sites use it --
	// this test exercises the real wiring, not a substitute construction.
	svc := centralsvc.New(failingAuditStore{Store: st}, kp, centralAuditLogger())

	// An invalid slug fails validation before any store write at all; its
	// best-effort FAILURE audit event then also fails to persist (the
	// wrapped store always fails AppendAuditEvent). No secret is created or
	// touched anywhere in this path.
	_, opErr := svc.CreateProject(context.Background(), "Invalid Slug With Spaces!", "name")
	if opErr == nil {
		t.Fatal("expected CreateProject to fail on an invalid slug")
	}
	if strings.Contains(opErr.Error(), "synthetic audit-store failure") {
		t.Fatalf("A-2: the audit-write failure must not replace or alter the business operation's own result, got: %v", opErr)
	}

	found := false
	for _, ent := range logs.All() {
		if ent.Level == zap.ErrorLevel {
			found = true
		}
		for _, sensitive := range []string{"root", "key", "plaintext", "secret-value"} {
			if strings.Contains(strings.ToLower(ent.Message), sensitive) {
				t.Fatalf("log message unexpectedly mentions %q: %q", sensitive, ent.Message)
			}
		}
	}
	if !found {
		t.Fatal("expected the real audit-write failure to produce an error-level log entry")
	}
}
