package agent

import (
	"strings"
	"sync/atomic"

	"github.com/docker-secret-operator/dso/internal/watcher"
)

// journalGuard is the watcher.OperationGuard backed by the rotation journal
// (StateTracker). See internal/watcher/operation_guard.go for why a durable guard
// is needed on top of the in-memory service locks.
//
// A service is BLOCKED from any new rotation when
//
//  1. startup gating is armed and startup recovery has not settled yet (no dispatch
//     path may mutate anything while recovery is still deciding what a previous
//     process left behind), or
//  2. the persisted journal was unreadable at startup (nothing is known about what
//     was in flight, so nothing is mutated; the unreadable file is preserved aside
//     and a restart after the operator verified the workloads starts clean), or
//  3. the journal holds an UNRESOLVED operation for the service: a record that is
//     critical_error (needs an operator: partially published signal rotation,
//     rollback failure, recovery that could not establish a safe state), or an
//     in_progress record INHERITED from a previous process that recovery did not
//     resolve. An ordinary failed attempt of the running process (rollback_required)
//     does not block: its strategy already restored the previous workload and the
//     next poll retries.
//
// Releasing a block: automatic recovery re-evaluates every unresolved record on each
// start and moves it to recovered when Docker proves a verified healthy workload;
// otherwise the operator repairs the service and marks the record recovered (or
// removes it from the state file while the agent is stopped). There is deliberately
// no "force" path in the agent: continuing a half-applied rotation is what this
// guard exists to prevent.
type journalGuard struct {
	st      *StateTracker
	armed   atomic.Bool // startup gating in force (set by BeginStartup)
	settled atomic.Bool // startup recovery finished (set by StartAll)
}

var _ watcher.OperationGuard = (*journalGuard)(nil)

func newJournalGuard(st *StateTracker) *journalGuard { return &journalGuard{st: st} }

func (g *journalGuard) RotationBlocked(keys ...string) (string, bool) {
	if g.armed.Load() && !g.settled.Load() {
		return "startup recovery has not completed", true
	}
	if g.st == nil {
		return "", false
	}
	if corrupt, where := g.st.Corrupt(); corrupt {
		return "persisted rotation state is unreadable (preserved at " + where + "): nothing is known about interrupted operations; manual recovery required", true
	}
	if rec := g.st.unresolvedFor(keys); rec != nil {
		return "unresolved operation " + rec.RotationID + " (" + rec.Strategy + ", last " + rec.Transition + ", status " + rec.Status + ")" +
			sentence(rec.ErrorMessage), true
	}
	return "", false
}

func sentence(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return ""
	}
	return ": " + s
}

// unresolvedFor returns a copy of the first record that matches any of keys (as its
// service identifier or as one of its original/candidate container IDs) and is
// unresolved, or nil.
func (st *StateTracker) unresolvedFor(keys []string) *RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, k := range st.sortedKeys() {
		r := st.states[k]
		if r.Status != "critical_error" && !(r.Status == "in_progress" && st.inherited[k]) {
			continue
		}
		if matchesRecord(r, keys) {
			cp := *r
			return &cp
		}
	}
	return nil
}

func matchesRecord(r *RotationState, keys []string) bool {
	for _, k := range keys {
		if k == "" {
			continue
		}
		if r.ServiceID == k || idIn(r.OriginalIDs(), k) || idIn(r.CandidateContainerIDs, k) {
			return true
		}
	}
	return false
}
