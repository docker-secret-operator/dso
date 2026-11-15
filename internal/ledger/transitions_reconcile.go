package ledger

import (
	"errors"
	"sort"
	"time"
)

// ReconcileFromJournal converges the ledger with the journal at startup, after
// M0 recovery settled and before the guard admits dispatch (design section 5).
// It applies the windows of design section 4.3 to every replica group that
// cites a rotation id:
//
//   - a requested hint whose operation the journal does not hold is cleared
//     (W2, W3, W10); one with complete evidence and a journal-carried version is
//     verified and applied without rotating again (W6, W9); one without
//     evidence becomes unknown (W4, W5) or failed (W15); evidence without a
//     version cannot be applied and becomes unknown (no advance);
//   - a verified claim the journal does not support is downgraded (rule R-A),
//     as is one whose record is missing and newer than the retention horizon
//     (R-B); an older one is accepted (R-C); a claimed adoption stronger than
//     the evidence is lowered (W12 evidence downgrade); a verified group whose
//     entry did not advance is applied (W7);
//   - with a live set, a recreated replica drops to unknown.
//
// Journal records older than the ledger's created_at are ignored (W11). An
// unreadable journal changes nothing. Each entry is one update, in key order;
// a second run on the same inputs changes nothing. It never touches Docker or
// the journal and cannot unblock anything.
func (v *Versions) ReconcileFromJournal(snap JournalSnapshot) (ReconcileReport, error) {
	if !snap.Readable {
		return ReconcileReport{Skipped: true, SkipReason: WhyJournalUnreadable}, nil
	}
	base := v.s.cur.Load()
	idx := make(map[string]JournalRecord, len(snap.Records))
	for _, r := range snap.Records {
		if !validRotationID(r.RotationID) {
			continue
		}
		if !r.UpdatedAt.IsZero() && r.UpdatedAt.Before(base.CreatedAt) {
			continue // W11: never replay history older than this ledger
		}
		idx[r.RotationID] = r
	}
	keys := make([]Key, 0, len(base.Entries))
	for _, e := range base.Entries {
		keys = append(keys, e.Key)
	}
	sort.Slice(keys, func(i, j int) bool { return keyLess(keys[i], keys[j]) })

	var rep ReconcileReport
	for _, k := range keys {
		var changes []ReconcileChange
		err := v.run(func(l *Ledger, now time.Time) (bool, error) {
			changes = changes[:0]
			e := findEntry(l, k)
			if e == nil {
				return false, nil
			}
			reconcileEntry(e, l.HMACKeyID, idx, snap, now, &changes)
			return len(changes) > 0, nil
		})
		if err != nil && !errors.Is(err, ErrNoChange) {
			return rep, err
		}
		if err == nil {
			rep.Changes = append(rep.Changes, changes...)
		}
	}
	return rep, nil
}

func adoptionRank(a AdoptionState) int {
	switch a {
	case AdoptionVerified:
		return 2
	case AdoptionUnverified:
		return 1
	}
	return 0
}

func reconcileEntry(e *Entry, ledgerKeyID string, idx map[string]JournalRecord, snap JournalSnapshot, now time.Time, changes *[]ReconcileChange) {
	note := func(rid, rule string) {
		*changes = append(*changes, ReconcileChange{Key: e.Key, RotationID: rid, Rule: rule})
	}
	var rids []string
	seen := map[string]bool{}
	for _, r := range e.Replicas {
		if r.RotationID != "" && r.Delivery != DeliveryNone && !seen[r.RotationID] {
			seen[r.RotationID] = true
			rids = append(rids, r.RotationID)
		}
	}
	sort.Strings(rids)
	fileMode := snap.FileMode[e.Key]
	for _, rid := range rids {
		var req, ver []int
		for i, r := range e.Replicas {
			if r.RotationID != rid {
				continue
			}
			switch r.Delivery {
			case DeliveryRequested:
				req = append(req, i)
			case DeliveryVerified:
				ver = append(ver, i)
			}
		}
		rec, found := idx[rid]
		if len(req) > 0 {
			switch {
			case !found:
				interruptLocked(e, req, CauseNeverRan, now)
				note(rid, "W10")
			default:
				ev := rec.evidence(fileMode, snap.Live[e.Key])
				adoption, reason, err := ev.assess()
				switch {
				case err == nil && rec.Version != nil && rec.Version.validate("journal.version", ledgerKeyID) == nil:
					if _, verr := verifyLocked(e, ev, adoption, reason, now); verr != nil {
						interruptLocked(e, req, CauseUnresolved, now)
						note(rid, "conflict")
					} else if ev.Kind == EvidenceSignalDone {
						note(rid, "W9")
					} else {
						note(rid, "W6")
					}
				case err == nil:
					interruptLocked(e, req, CauseUnresolved, now)
					note(rid, "no_version")
				case rec.Seq == 0:
					interruptLocked(e, req, CauseNeverRan, now)
					note(rid, "W3")
				case rec.Status == JournalRollbackRequired:
					interruptLocked(e, req, CauseFailed, now)
					note(rid, "W15")
				default:
					interruptLocked(e, req, CauseUnresolved, now)
					note(rid, "W4")
				}
			}
		}
		if len(ver) == 0 {
			continue
		}
		if !found {
			var recent []int
			for _, i := range ver {
				if snap.Horizon.IsZero() || e.Replicas[i].At.After(snap.Horizon) {
					recent = append(recent, i)
				}
			}
			if len(recent) > 0 {
				downgradeLocked(e, recent, now)
				note(rid, "R-B")
			}
			continue // R-C: older claims are accepted (the journal pruned them)
		}
		ev := rec.evidence(fileMode, nil)
		adoption, reason, err := ev.assess()
		consistent := err == nil
		for _, i := range ver {
			if rec.Version != nil && Compare(*rec.Version, *e.Replicas[i].Applied) != Same {
				consistent = false
			}
		}
		if !consistent {
			downgradeLocked(e, ver, now)
			note(rid, "R-A")
			continue
		}
		lowered := false
		for _, i := range ver {
			r := &e.Replicas[i]
			if adoptionRank(r.Adoption) > adoptionRank(adoption) {
				r.Adoption, r.Reason, r.At = adoption, string(reason), now
				lowered = true
			}
		}
		if lowered {
			note(rid, "W12-adoption")
		}
		if applied, aerr := applyEntryLocked(e, rid, strategyOf(ev.Kind), now); aerr == nil && applied {
			note(rid, "W7")
		}
	}
	if live, ok := snap.Live[e.Key]; ok && validateLive(live) == nil {
		byLive := liveIndex(live)
		for i := range e.Replicas {
			r := &e.Replicas[i]
			if r.Delivery == DeliveryRequested {
				continue
			}
			lc := byLive[refOf(*r)]
			if len(lc) != 1 || (lc[0].ContainerID == r.ContainerID && lc[0].Created == r.Created) {
				continue
			}
			rid := r.RotationID
			*r = adoptedReplica(lc[0], WhyIdentityChanged, now)
			note(rid, "identity_changed")
		}
	}
}

// downgradeLocked applies rules R-A and R-B: the journal wins over a ledger
// delivery claim it does not support.
func downgradeLocked(e *Entry, idx []int, now time.Time) {
	for _, i := range idx {
		r := &e.Replicas[i]
		claimed := r.Applied
		r.Applied, r.Delivery, r.Adoption, r.Outcome, r.Reason, r.At = nil, DeliveryNone, AdoptionUnknown, ReplicaFailed, string(WhyInterrupted), now
		if e.Applied != nil && claimed != nil && Compare(*e.Applied, *claimed) == Same {
			e.Applied = nil
		}
	}
	if e.State != EntryBlocked {
		e.State = EntryFailed
		e.LastOutcome = &LastOutcome{Kind: outcomeInterrupted, Reason: string(WhyInterrupted), At: now}
	}
}
