package ledger

import "strconv"

// This file holds the manual redelivery request (design §9.7.5, §30.1), which
// is KEEP-UNWIRED (docs/design/m1-ledger-reconciliation.md section 1.2): a
// durable manual request would be a second intent store, so no production code
// may reference it (TestRepositoryReferenceGuard). It does not change applied
// or replicas. The recovery journal of earlier revisions was removed in schema
// version 3: recovery belongs to the M0 journal. The rotation transitions are
// in transitions.go.

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
	err := s.update(func(l *Ledger) error {
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
