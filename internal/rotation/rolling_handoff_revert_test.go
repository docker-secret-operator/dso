package rotation

import (
	"context"
	"errors"
	"testing"
)

// Failures AFTER the proxy handoff. OnReady (the proxy swap) and handoff_done run before
// intent_swap and the renames; if any later step fails, the candidate used to be discarded
// with nothing undoing the handoff, leaving a proxied service with no backend. The strategy
// must now either revert the handoff before discarding the candidate, or, if it cannot,
// preserve the candidate (it may be the only backend) and escalate to manual recovery.

type handoffRig struct {
	*n19Rig
	reverts       []string
	removedAtHook int
}

func newHandoffRig(t *testing.T) *handoffRig {
	t.Helper()
	h := &handoffRig{n19Rig: newN19Rig(t, nil), removedAtHook: -1}
	h.rs.OnReady = func(context.Context, string) error { return nil } // the handoff succeeds
	return h
}

func (h *handoffRig) withRevert(err error) {
	h.rs.OnRevert = func(_ context.Context, newID string) error {
		h.reverts = append(h.reverts, newID)
		h.removedAtHook = h.d.n("remove:cand-1") // must still be 0: revert BEFORE discard
		return err
	}
}

func (h *handoffRig) manualRecoveryEvent() *JournalEvent {
	h.j.mu.Lock()
	defer h.j.mu.Unlock()
	for i := range h.j.events {
		if h.j.events[i].Transition == TrFailed && h.j.events[i].ManualRecovery {
			e := h.j.events[i]
			return &e
		}
	}
	return nil
}

// Failing first (existing API only): after a completed handoff with no way to revert it,
// the candidate must not be removed and the journal must demand manual recovery. The old
// code discarded it and journalled nothing.
func TestRolling_PostHandoffFailure_WithoutARevert_PreservesTheCandidateAndEscalates(t *testing.T) {
	h := newHandoffRig(t)
	h.j.failOn = TrIntentSwap
	err := h.run()
	if err == nil {
		t.Fatal("the rotation must fail when intent_swap cannot be journalled")
	}
	if n := h.d.n("remove:cand-1"); n != 0 {
		t.Fatalf("the candidate was removed although the handoff could not be reverted (log %v)", h.d.log)
	}
	if h.manualRecoveryEvent() == nil {
		t.Fatalf("no manual-recovery record was journalled (transitions %v)", h.j.transitions())
	}
	if !errors.Is(err, ErrHandoffNotReverted) {
		t.Fatalf("the error must say the handoff was not reverted, got: %v", err)
	}
}

func TestRolling_PostHandoffFailure_RevertsTheHandoffThenDiscardsTheCandidate(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *handoffRig)
	}{
		{"intent_swap cannot be journalled", func(h *handoffRig) { h.j.failOn = TrIntentSwap }},
		{"the original cannot be renamed to its backup name", func(h *handoffRig) { h.d.failRenameTo = "my-container_dso_backup_" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHandoffRig(t)
			h.withRevert(nil)
			tc.setup(h)
			err := h.run()
			if err == nil {
				t.Fatal("the rotation must fail")
			}
			if errors.Is(err, ErrHandoffNotReverted) {
				t.Fatalf("a revert that succeeded must not be reported as not reverted: %v", err)
			}
			if len(h.reverts) != 1 || h.reverts[0] != "cand-1" {
				t.Fatalf("the handoff must be reverted exactly once for the candidate, got %v", h.reverts)
			}
			if h.removedAtHook != 0 {
				t.Fatalf("the handoff must be reverted BEFORE the candidate is removed (removals at hook time: %d)", h.removedAtHook)
			}
			if h.d.n("remove:cand-1") == 0 {
				t.Fatalf("after a successful revert the candidate must be discarded (log %v)", h.d.log)
			}
			if h.manualRecoveryEvent() != nil {
				t.Fatal("a clean revert must not demand manual recovery")
			}
		})
	}
}

func TestRolling_PostHandoffFailure_RevertFailure_PreservesTheCandidate(t *testing.T) {
	h := newHandoffRig(t)
	h.withRevert(errors.New("registry refused"))
	h.j.failOn = TrIntentSwap
	err := h.run()
	if !errors.Is(err, ErrHandoffNotReverted) {
		t.Fatalf("a failed revert must be reported as ErrHandoffNotReverted, got %v", err)
	}
	if n := h.d.n("remove:cand-1"); n != 0 {
		t.Fatalf("the candidate may be the only backend and must be preserved after a failed revert (log %v)", h.d.log)
	}
	if h.manualRecoveryEvent() == nil {
		t.Fatal("a failed revert must be journalled for manual recovery")
	}
}

// The revert is only for failures AFTER the handoff: earlier failures and success never
// call it.
func TestRolling_Revert_NotCalledBeforeTheHandoffOrOnSuccess(t *testing.T) {
	t.Run("OnReady itself fails", func(t *testing.T) {
		h := newHandoffRig(t)
		h.rs.OnReady = func(context.Context, string) error { return errors.New("swap refused") }
		h.withRevert(nil)
		if err := h.run(); err == nil {
			t.Fatal("must fail")
		}
		if len(h.reverts) != 0 {
			t.Fatalf("nothing was handed off, so there is nothing to revert: %v", h.reverts)
		}
		if h.d.n("remove:cand-1") == 0 {
			t.Fatal("the candidate must still be discarded")
		}
	})
	t.Run("the rotation succeeds", func(t *testing.T) {
		h := newHandoffRig(t)
		h.withRevert(nil)
		if err := h.run(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(h.reverts) != 0 {
			t.Fatalf("a successful rotation must not revert: %v", h.reverts)
		}
	})
}
