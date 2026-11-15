package ledger

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

// This file holds the generic persistence operations S2 owns on top of Update:
// manual redelivery requests (design §9.7.5, §30.1) and the recovery journal
// (§8.5, §30.2). None of them changes applied, replicas or rotation. The
// rotation transitions (Observe, Adopt, BeginRotation, CompleteRotation,
// Interrupted, Unblock) belong to S4 and are not implemented here.

// RecoveryRetention is the retention target for done recovery records (§30.2).
const RecoveryRetention = 50

// CancelReasonRestart is recorded as the reason of a request cancelled because
// the agent restarted before dispatching it (§9.7.5).
const CancelReasonRestart = "agent_restarted"

func isActive(s RedeliveryState) bool {
	return s == RedeliveryRequested || s == RedeliveryDispatched
}

// RequestRedelivery durably records a manual redelivery request for the entry's
// current desired version and returns its id. The request is persisted before
// the caller may touch Docker; if persistence fails the error is returned and
// nothing is recorded.
//
// A second call while a request created by this process is still requested or
// dispatched is coalesced: the existing id is returned and nothing is written. A
// leftover active request from an earlier process (a crash) is replaced by the
// new one, and EventRequestSuperseded names both ids; there is one request slot
// per entry and no history.
func (s *Store) RequestRedelivery(k Key, actor, reason string) (string, error) {
	var id string
	var evs []Event
	err := s.Update(func(l *Ledger) error {
		e := findEntry(l, k)
		if e == nil {
			return ErrNotFound
		}
		if e.Desired == nil || e.DesiredSeq == 0 {
			return ErrNoDesired
		}
		var old *Redelivery
		if r := e.Request; r != nil && isActive(r.State) {
			s.hmu.Lock()
			live := s.liveRequests[r.RequestID]
			s.hmu.Unlock()
			if live {
				id = r.RequestID
				return ErrNoChange
			}
			c := *r
			old = &c
		}
		nid, err := newID(s.random)
		if err != nil {
			return err
		}
		e.Request = &Redelivery{
			RequestID: nid, RequestedAt: s.utcNow(), Actor: actor, Reason: reason,
			TargetSeq: e.DesiredSeq, State: RedeliveryRequested,
		}
		id = nid
		if old != nil {
			evs = append(evs, Event{Kind: EventRequestSuperseded, Detail: old.RequestID + " -> " + nid, At: s.utcNow()})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	s.hmu.Lock()
	s.liveRequests[id] = true
	s.hmu.Unlock()
	s.emit(evs)
	return id, nil
}

// cancelUndispatched marks every request that is requested and has no rotation
// as cancelled with reason agent_restarted (§9.7.5). Operator requests are not
// replayed. Dispatched requests follow the unresolved-intent rules and are left.
func (s *Store) cancelUndispatched() (int, []Event, error) {
	n := 0
	var evs []Event
	s.mu.Lock()
	more, err := s.updateLocked(func(l *Ledger) error {
		for i := range l.Entries {
			r := l.Entries[i].Request
			if r == nil || r.State != RedeliveryRequested || r.RotationID != "" {
				continue
			}
			// The persisted request keeps its actor and takes the restart reason;
			// the operator's original reason survives in the event only (no
			// schema change). Capture it before it is overwritten.
			origActor, origReason := r.Actor, r.Reason
			r.State, r.Reason = RedeliveryCancelled, CancelReasonRestart
			n++
			evs = append(evs, Event{Kind: EventRequestCancelled, Detail: cancelDetail(r.RequestID, origActor, origReason), At: s.utcNow()})
		}
		if n == 0 {
			return ErrNoChange
		}
		return nil
	}, len(retryDelays))
	s.mu.Unlock()
	if err != nil {
		// Nothing was persisted: report no cancellations, only the events the
		// failed write itself produced (e.g. persistence degraded).
		return 0, more, err
	}
	return n, append(evs, more...), nil
}

// cancelDetail formats the Detail of an EventRequestCancelled event as
// space-separated key=value pairs in a fixed order. The free-form operator
// values are written with strconv.Quote, so spaces, quotes, delimiters and
// non-ASCII text are unambiguous, and they are never truncated.
func cancelDetail(requestID, actor, reason string) string {
	return "request=" + requestID + " actor=" + strconv.Quote(actor) +
		" reason=" + strconv.Quote(reason) + " cancelled_reason=" + CancelReasonRestart
}

func findEntry(l *Ledger, k Key) *Entry {
	for i := range l.Entries {
		if l.Entries[i].Key == k {
			return &l.Entries[i]
		}
	}
	return nil
}

func sameScope(a, b RecoveryScope) bool { return a == b }

func nonTerminal(s RecoveryState) bool {
	return s == RecoveryPlanned || s == RecoveryInProgress
}

// BeginRecovery durably records a recovery plan before any recovery mutation
// (R-V1). At most one non-terminal record exists per scope: if one exists it is
// returned (resumed=true) and nothing is written (R-V8). Done records beyond the
// retention target are pruned, oldest first, inside the same update; planned,
// in_progress and failed records are never evicted, and EventRetentionExceeded
// is emitted when that keeps the count above the target.
func (s *Store) BeginRecovery(scope RecoveryScope, trigger RecoveryTrigger, planned []Action) (id string, resumed bool, err error) {
	var evs []Event
	err = s.Update(func(l *Ledger) error {
		for i := range l.Recoveries {
			if r := &l.Recoveries[i]; sameScope(r.Scope, scope) && nonTerminal(r.State) {
				id, resumed = r.RecoveryID, true
				return ErrNoChange
			}
		}
		nid, err := newID(s.random)
		if err != nil {
			return err
		}
		l.Recoveries = append(l.Recoveries, RecoveryRecord{
			RecoveryID: nid, Scope: scope, Trigger: trigger,
			Planned: append([]Action(nil), planned...), Done: []ActionResult{},
			State: RecoveryPlanned, StartedAt: s.utcNow(),
		})
		id, resumed = nid, false
		if pruneDone(l) {
			evs = append(evs, Event{Kind: EventRetentionExceeded, Detail: fmt.Sprintf("%d recovery records, none evictable", len(l.Recoveries)), At: s.utcNow()})
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	s.emit(evs)
	return id, resumed, nil
}

// pruneDone evicts the oldest done records while more than RecoveryRetention
// records are held. It reports whether the count still exceeds the target
// because nothing evictable remains.
func pruneDone(l *Ledger) (exceeded bool) {
	for len(l.Recoveries) > RecoveryRetention {
		oldest := -1
		for i := range l.Recoveries {
			r := &l.Recoveries[i]
			if r.State != RecoveryDone {
				continue
			}
			if oldest < 0 || r.FinishedAt.Before(l.Recoveries[oldest].FinishedAt) {
				oldest = i
			}
		}
		if oldest < 0 {
			return true
		}
		l.Recoveries = append(l.Recoveries[:oldest:oldest], l.Recoveries[oldest+1:]...)
	}
	return false
}

// CompleteRecovery records the results and the new state of a recovery record.
// Only a durable write makes a record done (R-V3). The state must be
// in_progress, done or failed; blocked_persistence is an in-memory diagnostic
// that is never written (§30.3). It touches nothing but the named record
// (R-V4, R-V5, R-V7).
func (s *Store) CompleteRecovery(id string, results []ActionResult, state RecoveryState) error {
	switch state {
	case RecoveryInProgress, RecoveryDone, RecoveryFailed:
	default:
		return fmt.Errorf("ledger: a recovery record cannot be written in state %q", state)
	}
	return s.Update(func(l *Ledger) error {
		for i := range l.Recoveries {
			r := &l.Recoveries[i]
			if r.RecoveryID != id {
				continue
			}
			r.Done = append([]ActionResult(nil), results...)
			r.State = state
			if state == RecoveryInProgress {
				r.FinishedAt = time.Time{}
			} else {
				r.FinishedAt = s.utcNow()
			}
			return nil
		}
		return ErrNotFound
	})
}

// RecoveryRecords returns an independent, deterministic copy of the journal.
func (s *Store) RecoveryRecords() []RecoveryRecord {
	l := s.cur.Load()
	out := cloneSlice(l.Recoveries, cloneRecovery)
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}
