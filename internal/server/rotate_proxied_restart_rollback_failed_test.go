package server

import (
	"strings"
	"testing"
	"time"
)

// A proxied restart rotation whose proxy swap did not happen is rolled back. When the
// rollback cannot complete (here the daemon refuses to start the original), the restart
// strategy journals a manual-recovery failure (TrFailed with ManualRecovery) naming the
// replacement. That record must be escalated to critical_error, which blocks the service
// until an operator or recovery resolves it, and the original container must not be
// stopped or removed.
//
// Observed sequence of daemon calls on this path (see the assertions below for what is
// pinned): the original is renamed aside, the replacement is created, started and found
// unroutable, the replacement is stopped and force-removed by the rollback, the original's
// name is restored, and the start of the original is refused (retried by the rollback).
func TestManualRotate_ProxiedRestart_SwapDidNotHappen_RollbackFails_EscalatesToCriticalError(t *testing.T) {
	s, ts, calls := rotationFixtureOpts(t, "ok", rotationOpts{hostPorts: "18082:80", failRollbackStart: true})
	eng := wireEngine(t, s)
	postRotate(t, ts)

	waitFor(t, "the rollback to try to start the original", func() bool {
		return calls.countReq("POST", "/containers/orig-1/start") > 0
	})

	// The rollback retries before the rotation gives up; poll for the final record.
	type snapshot struct {
		status, transition, errMsg, detail string
		manual                             bool
		candidates, originals              []string
	}
	var got snapshot
	deadline := time.Now().Add(20 * time.Second)
	for got.status == "" && time.Now().Before(deadline) {
		for _, r := range eng.StateTracker.GetPendingRotations() {
			if r.Status == "critical_error" || r.Status == "rollback_required" {
				got = snapshot{r.Status, r.Transition, r.ErrorMessage, r.Detail, r.ManualRecovery,
					append([]string(nil), r.CandidateContainerIDs...), r.OriginalIDs()}
			}
		}
		if got.status == "" {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got.status == "" {
		t.Fatal("no escalated or rollback-required record appeared")
	}

	// Give a (wrongly) continuing rotation the whole drain window plus margin to reach
	// its destructive steps; it must not.
	time.Sleep(6 * time.Second)

	calls.mu.Lock()
	seq := append([]string(nil), calls.calls...)
	calls.mu.Unlock()
	// indexEnds finds the first request with the method whose request line ends with the
	// suffix (the first rename of the original, which moves it aside, has a longer name).
	indexEnds := func(method, suffix string) int {
		for i, c := range seq {
			if strings.HasPrefix(c, method+" ") && strings.HasSuffix(c, suffix) {
				return i
			}
		}
		return -1
	}

	// The original is preserved: never stopped, never removed.
	if n := calls.countReq("DELETE", "/containers/orig-1"); n != 0 {
		t.Fatalf("the original was deleted %d time(s) although the rollback failed:\n%s", n, strings.Join(seq, "\n"))
	}
	if n := calls.count("/containers/orig-1/stop"); n != 0 {
		t.Fatalf("the original was stopped %d time(s) although the rollback failed:\n%s", n, strings.Join(seq, "\n"))
	}
	// Its name was restored before its start was attempted.
	rename, start := indexEnds("POST", "/containers/orig-1/rename?name=myapp"), indexEnds("POST", "/containers/orig-1/start?")
	if rename < 0 || start < 0 || rename > start {
		t.Fatalf("expected the original's name to be restored before its start was attempted (rename at %d, start at %d):\n%s",
			rename, start, strings.Join(seq, "\n"))
	}

	// The journal record: escalated, with the identities the operation guard matches on.
	if got.status != "critical_error" {
		t.Fatalf("record status = %q, want critical_error (a manual-recovery failure must block the service)", got.status)
	}
	if !got.manual || got.transition != "failed" {
		t.Fatalf("record = transition %q manual %v, want the manual-recovery failure", got.transition, got.manual)
	}
	if got.detail == "" || got.errMsg != "manual recovery required: "+got.detail {
		t.Fatalf("ErrorMessage = %q, want %q", got.errMsg, "manual recovery required: "+got.detail)
	}
	if !hasString(got.candidates, "new-1") || !hasString(got.originals, "orig-1") {
		t.Fatalf("record identities: candidates %v originals %v, want new-1 and orig-1", got.candidates, got.originals)
	}
}

func hasString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
