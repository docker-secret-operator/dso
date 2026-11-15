package ledger

import (
	"errors"
	"fmt"
	"time"
)

// This file is the S4a transition facade (docs/design/m1-ledger-reconciliation.md
// sections 3, 4, 6, 7, 9 and 10). It is the only mutation surface production
// code may use (owner decision O-9); the store's whole-document mutators are
// unexported.
//
// Every transition:
//   - runs as exactly one store update (the single serialised write path with
//     the store's flock and atomic rename), or one in-memory update in a
//     memory-only mode (design section 6);
//   - checks narrow preconditions and refuses an invalid transition with an
//     error, changing nothing;
//   - is idempotent: re-applying it changes nothing and returns ErrNoChange;
//   - performs no Docker I/O, never calls or bypasses the M0 operation guard,
//     never authorises a rotation (X1) and never stores or logs a secret value.
//
// The ledger is advisory (owner decision O-3): every error here leaves the
// caller free to proceed with what M0 alone would permit (X6).

// KeyMaterialSource is the read-only S3 key accessor the facade needs to build
// keyed identities. *FileKeyProvider implements it.
type KeyMaterialSource interface {
	KeyMaterial() (keyID string, key []byte, err error)
}

var _ KeyMaterialSource = (*FileKeyProvider)(nil)

// Versions is the narrow facade over a Store.
type Versions struct {
	s  *Store
	km KeyMaterialSource
}

// NewVersions returns the facade for s. km may be nil only when the caller
// supplies ready-made Versions; NewVersion then returns ErrKeyUnavailable.
func NewVersions(s *Store, km KeyMaterialSource) (*Versions, error) {
	if s == nil {
		return nil, errors.New("ledger: NewVersions needs a store")
	}
	return &Versions{s: s, km: km}, nil
}

// NewVersion builds the keyed HMAC identity of data with the S3 key material.
// The key copy is zeroised before returning; data is read, never retained.
// Unkeyed hashes are never produced: without a usable key the result is
// ErrKeyUnavailable, which callers treat as "unknown", never "changed" (X3).
func (v *Versions) NewVersion(provider, secret string, data map[string]string) (Version, error) {
	if v.km == nil {
		return Version{}, fmt.Errorf("%w: no key material source", ErrKeyUnavailable)
	}
	id, key, err := v.km.KeyMaterial()
	defer clear(key)
	if err != nil {
		return Version{}, fmt.Errorf("%w: %w", ErrKeyUnavailable, err)
	}
	if lid := v.s.cur.Load().HMACKeyID; lid != "" && lid != id {
		return Version{}, fmt.Errorf("%w: the key id differs from the ledger's hmac_key_id", ErrKeyUnavailable)
	}
	ver, err := NewHMACVersion(id, key, provider, secret, data)
	if err != nil {
		return Version{}, err
	}
	return ver, nil
}

// Get returns an independent copy of one entry.
func (v *Versions) Get(k Key) (Entry, bool) { return v.s.Get(k) }

// Snapshot returns an independent copy of the published document.
func (v *Versions) Snapshot() Snapshot { return v.s.Snapshot() }

// apply selects the durable or the memory-only update path (design section 6).
func (v *Versions) apply(fn func(*Ledger) error) error {
	if v.s.mode == ModeMemoryOnly {
		return v.s.updateMemoryOnly(fn)
	}
	return v.s.update(fn)
}

// run executes one transition. fn reports whether it changed the document; an
// unchanged document is never written and the transition returns ErrNoChange.
func (v *Versions) run(fn func(l *Ledger, now time.Time) (bool, error)) error {
	now := v.s.utcNow()
	changed := false
	err := v.apply(func(l *Ledger) error {
		c, err := fn(l, now)
		if err != nil {
			return err
		}
		if !c {
			return ErrNoChange
		}
		changed = true
		return nil
	})
	if err != nil {
		return err
	}
	if !changed {
		return ErrNoChange
	}
	return nil
}

func versionCopy(v Version) *Version { return &v }

func adoptedReplica(l LiveReplica, why OutcomeReason, now time.Time) Replica {
	return Replica{Number: l.Number, ContainerID: l.ContainerID, ContainerName: l.ContainerName, Created: l.Created,
		Outcome: ReplicaAdopted, Reason: string(why), At: now, Delivery: DeliveryNone, Adoption: AdoptionUnknown}
}

func validateLive(live []LiveReplica) error {
	ids := make(map[string]bool, len(live))
	for _, l := range live {
		if err := l.validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
		}
		if ids[l.ContainerID] {
			return invalidTransition("duplicate container id in the live set")
		}
		ids[l.ContainerID] = true
	}
	return nil
}

func liveIndex(live []LiveReplica) map[replicaRef][]LiveReplica {
	m := make(map[replicaRef][]LiveReplica, len(live))
	for _, l := range live {
		m[refOfLive(l)] = append(m[refOfLive(l)], l)
	}
	return m
}

// ---------------------------------------------------------------- Observe

// Observe records the observed desired version of k. It is the sole writer of
// desired (design section 9). First sight is an adoption: desired recorded,
// applied UNKNOWN (nil), every live replica adopted with adoption unknown, and
// no rotation (O-4). A different comparable version advances desired_seq; an
// incomparable one (key or kind change) re-adopts the entry UNKNOWN, never
// "changed". A replica whose (container id, created) changed without an
// in-flight operation drops to adoption unknown with reason identity_changed;
// that never causes a rotation.
func (v *Versions) Observe(k Key, ver Version, live []LiveReplica) (ObserveResult, error) {
	if err := k.validate("key"); err != nil {
		return ObserveResult{}, fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}
	if ver.Kind != KindProvider && ver.Kind != KindHMAC {
		return ObserveResult{}, invalidTransition("the version is not a computable identity")
	}
	if err := validateLive(live); err != nil {
		return ObserveResult{}, err
	}
	kind := ObserveUnchanged
	var out Entry
	err := v.run(func(l *Ledger, now time.Time) (bool, error) {
		kind = ObserveUnchanged
		// A memory-only ledger started without a key id adopts the key id of the
		// first keyed identity when it holds no other (Option B in memory).
		if ver.Kind == KindHMAC && l.HMACKeyID == "" && !hasHMACIdentity(l) {
			l.HMACKeyID = ver.KeyID
		}
		if err := ver.validate("version", l.HMACKeyID); err != nil {
			return false, fmt.Errorf("%w: %w", ErrInvalidTransition, err)
		}
		e := findEntry(l, k)
		if e == nil {
			ne := Entry{Key: k, Desired: versionCopy(ver), DesiredSeq: 1, DesiredObservedAt: now, State: EntryAdopted}
			for _, lr := range live {
				ne.Replicas = append(ne.Replicas, adoptedReplica(lr, WhyAdoptedUnknown, now))
			}
			l.Entries = append(l.Entries, ne)
			kind = ObserveAdopted
			out = cloneEntry(ne)
			return true, nil
		}
		changed := false
		if e.Desired == nil {
			e.Desired, e.DesiredSeq, e.DesiredObservedAt = versionCopy(ver), 1, now
			kind, changed = ObserveAdopted, true
		} else {
			switch Compare(*e.Desired, ver) {
			case Same:
			case Different:
				e.Desired, e.DesiredObservedAt = versionCopy(ver), now
				e.DesiredSeq++
				if e.State == EntryIdle || e.State == EntryAdopted {
					e.State = EntryPending
				}
				kind, changed = ObserveChanged, true
			default: // Incomparable: adopt UNKNOWN again
				e.Desired, e.DesiredObservedAt = versionCopy(ver), now
				e.DesiredSeq++
				e.Applied = nil
				if e.State != EntryBlocked {
					e.State = EntryAdopted
				}
				var reps []Replica
				if len(live) > 0 {
					for _, lr := range live {
						reps = append(reps, adoptedReplica(lr, WhyAdoptedUnknown, now))
					}
				} else {
					for _, r := range e.Replicas {
						reps = append(reps, adoptedReplica(LiveReplica{Number: r.Number, ContainerID: r.ContainerID, ContainerName: r.ContainerName, Created: r.Created}, WhyAdoptedUnknown, now))
					}
				}
				e.Replicas = reps
				kind, changed = ObserveReadopted, true
			}
		}
		if kind != ObserveReadopted && len(live) > 0 && reconcileLive(e, live, now) {
			changed = true
			if kind == ObserveUnchanged {
				kind = ObserveReplicasChanged
			}
		}
		out = cloneEntry(*e)
		return changed, nil
	})
	if errors.Is(err, ErrNoChange) {
		return ObserveResult{Kind: ObserveUnchanged, Entry: out}, nil
	}
	if err != nil {
		return ObserveResult{}, err
	}
	return ObserveResult{Kind: kind, Entry: out}, nil
}

// reconcileLive aligns the recorded replicas with the live set (design 1.4):
// a replica with an in-flight requested delivery is left alone (the journal
// explains its changes); a recreated replica drops to unknown; a vanished one
// is removed; an ambiguous identity (duplicate number and name) is recorded
// unknown; a new replica is adopted unknown, or, for a blocked entry and an
// eligible replica, marked eligibility_changed (D61-2), as is a recorded
// blocked replica whose strategy now dispatches.
func reconcileLive(e *Entry, live []LiveReplica, now time.Time) bool {
	byLive := liveIndex(live)
	stored := map[replicaRef]int{}
	for _, r := range e.Replicas {
		stored[refOf(r)]++
	}
	changed := false
	matched := map[replicaRef]bool{}
	usedIDs := map[string]bool{}
	var out []Replica
	for _, r := range e.Replicas {
		ref := refOf(r)
		if r.Delivery == DeliveryRequested {
			out = append(out, r)
			matched[ref] = true
			usedIDs[r.ContainerID] = true
			continue
		}
		lc := byLive[ref]
		if len(lc) == 0 || len(lc) > 1 || stored[ref] > 1 {
			changed = true // vanished, or ambiguous: the live ones are added below
			continue
		}
		lr := lc[0]
		switch {
		case r.ContainerID != lr.ContainerID || r.Created != lr.Created:
			r = adoptedReplica(lr, WhyIdentityChanged, now)
			changed = true
		case e.State == EntryBlocked && lr.Eligible && r.Outcome == ReplicaBlocked:
			r.Outcome, r.Reason, r.At = ReplicaPending, string(WhyEligibilityChanged), now
			if r.Delivery == DeliveryVerified {
				r.Delivery = DeliveryNone
			}
			changed = true
		}
		out = append(out, r)
		matched[ref] = true
		usedIDs[r.ContainerID] = true
	}
	for _, lr := range live {
		ref := refOfLive(lr)
		if matched[ref] || usedIDs[lr.ContainerID] {
			continue
		}
		var r Replica
		switch {
		case len(byLive[ref]) > 1 || stored[ref] > 1:
			r = adoptedReplica(lr, WhyIdentityChanged, now)
		case e.State == EntryBlocked && lr.Eligible:
			r = adoptedReplica(lr, WhyEligibilityChanged, now)
			r.Outcome = ReplicaPending
		default:
			r = adoptedReplica(lr, WhyAdoptedUnknown, now)
		}
		out = append(out, r)
		usedIDs[lr.ContainerID] = true
		changed = true
	}
	if changed {
		e.Replicas = out
	}
	return changed
}

// ---------------------------------------------------------------- Correlate

// Correlate records delivery=requested and rotation_id for every targeted
// (non-blocked) replica of k. The caller must have received nil from the
// journal's StartRotationWithID for rotationID: the ledger never mints an
// operation id and writes no intent, no phase and no entry state other than
// leaving "adopted" (an adopted entry has only adopted replicas).
func (v *Versions) Correlate(k Key, rotationID string, desiredSeq uint64, ver Version) error {
	if !validRotationID(rotationID) {
		return invalidTransition("the rotation id is empty or malformed")
	}
	return v.run(func(l *Ledger, now time.Time) (bool, error) {
		e := findEntry(l, k)
		if e == nil {
			return false, ErrNotFound
		}
		if e.Desired == nil {
			return false, ErrNoDesired
		}
		if e.DesiredSeq != desiredSeq {
			return false, fmt.Errorf("%w: desired_seq is %d, the rotation targets %d", ErrStale, e.DesiredSeq, desiredSeq)
		}
		if Compare(*e.Desired, ver) != Same {
			return false, fmt.Errorf("%w: the version is not the entry's desired version", ErrStale)
		}
		if e.State == EntryBlocked {
			return false, invalidTransition("the entry is blocked")
		}
		targets := 0
		changed := false
		for i := range e.Replicas {
			r := &e.Replicas[i]
			if r.Outcome == ReplicaBlocked {
				continue
			}
			targets++
			if r.RotationID == rotationID && r.Delivery != DeliveryNone {
				continue // idempotent: already requested or verified for this operation
			}
			r.RotationID, r.Delivery, r.Outcome, r.Reason, r.At = rotationID, DeliveryRequested, ReplicaPending, string(WhyDeliveryRequested), now
			changed = true
		}
		if targets == 0 {
			return false, ErrNoTargets
		}
		if changed && e.State == EntryAdopted {
			e.State = EntryPending
		}
		return changed, nil
	})
}

// ---------------------------------------------------------------- delivery

// RecordDeliveryVerified records journal-verified delivery for every replica
// correlated with ev.RotationID and, in the same single update (window W7),
// advances the entry's applied version. ev must satisfy the evidence rule of
// design section 4.2; anything weaker is refused with ErrNoEvidence. Adoption
// follows the rule: restart/rolling env-mode verified, file-mode unverified
// (N19), signal always unverified.
func (v *Versions) RecordDeliveryVerified(k Key, ev Evidence) (DeliveryResult, error) {
	adoption, reason, err := ev.assess()
	if err != nil {
		return DeliveryResult{}, err
	}
	if err := validateLive(ev.Live); err != nil {
		return DeliveryResult{}, err
	}
	var res DeliveryResult
	err = v.run(func(l *Ledger, now time.Time) (bool, error) {
		e := findEntry(l, k)
		if e == nil {
			return false, ErrNotFound
		}
		if err := ev.Version.validate("evidence.version", l.HMACKeyID); err != nil {
			return false, fmt.Errorf("%w: %w", ErrNoEvidence, err)
		}
		changed, err := verifyLocked(e, ev, adoption, reason, now)
		if err != nil {
			return false, err
		}
		res = DeliveryResult{Adoption: adoption, Applied: e.Applied != nil && Compare(*e.Applied, ev.Version) == Same, Entry: cloneEntry(*e)}
		return changed, nil
	})
	if errors.Is(err, ErrNoChange) {
		return res, ErrNoChange
	}
	return res, err
}

func strategyOf(kind EvidenceKind) string {
	switch kind {
	case EvidenceRestartVerified:
		return "restart"
	case EvidenceRollingVerified:
		return "rolling"
	case EvidenceSignalDone:
		return "signal"
	}
	return ""
}

// verifyLocked applies an assessed Evidence to the replicas correlated with it
// and then advances the entry.
func verifyLocked(e *Entry, ev Evidence, adoption AdoptionState, reason OutcomeReason, now time.Time) (bool, error) {
	if e.State == EntryBlocked {
		return false, invalidTransition("the entry is blocked")
	}
	byLive := liveIndex(ev.Live)
	group := 0
	changed := false
	for i := range e.Replicas {
		r := &e.Replicas[i]
		if r.RotationID != ev.RotationID || r.Delivery == DeliveryNone {
			continue
		}
		group++
		if r.Delivery == DeliveryVerified {
			if r.Applied == nil || Compare(*r.Applied, ev.Version) != Same {
				return false, invalidTransition("conflicting evidence for an already verified rotation")
			}
			continue
		}
		r.Applied, r.Outcome, r.Delivery, r.Adoption, r.Reason, r.At = versionCopy(ev.Version), ReplicaSucceeded, DeliveryVerified, adoption, string(reason), now
		if lc := byLive[refOf(*r)]; len(lc) == 1 {
			r.ContainerID, r.Created = lc[0].ContainerID, lc[0].Created
		}
		changed = true
	}
	if group == 0 {
		return false, invalidTransition("no replica is correlated with the rotation")
	}
	applied, err := applyEntryLocked(e, ev.RotationID, strategyOf(ev.Kind), now)
	if err != nil {
		return false, err
	}
	return changed || applied, nil
}

// RecordApplied advances the entry's applied version for rotationID when every
// targeted replica of that operation is delivery-verified with one version.
// RecordDeliveryVerified already does this in the same update; RecordApplied
// exists for a "verified but not applied" record (design W7 note) and is
// idempotent.
func (v *Versions) RecordApplied(k Key, rotationID string) error {
	if !validRotationID(rotationID) {
		return invalidTransition("the rotation id is empty or malformed")
	}
	return v.run(func(l *Ledger, now time.Time) (bool, error) {
		e := findEntry(l, k)
		if e == nil {
			return false, ErrNotFound
		}
		return applyEntryLocked(e, rotationID, "", now)
	})
}

func applyEntryLocked(e *Entry, rotationID, strategy string, now time.Time) (bool, error) {
	if e.State == EntryBlocked {
		return false, invalidTransition("the entry is blocked")
	}
	var ver *Version
	why := ""
	group := 0
	for _, r := range e.Replicas {
		if r.RotationID != rotationID || r.Delivery == DeliveryNone {
			continue
		}
		group++
		if r.Delivery != DeliveryVerified {
			return false, invalidTransition("not every targeted replica is delivery-verified")
		}
		if ver == nil {
			ver, why = r.Applied, r.Reason
		} else if Compare(*ver, *r.Applied) != Same {
			return false, invalidTransition("the targeted replicas hold different versions")
		}
	}
	if group == 0 || ver == nil {
		return false, invalidTransition("no replica is verified for the rotation")
	}
	next := EntryIdle
	if e.Desired != nil && Compare(*e.Desired, *ver) != Same {
		next = EntryPending // desired moved on meanwhile
	}
	if e.Applied != nil && Compare(*e.Applied, *ver) == Same && e.State == next {
		return false, nil
	}
	e.Applied, e.State = versionCopy(*ver), next
	e.LastOutcome = &LastOutcome{Kind: outcomeSuccess, Reason: why, At: now, Strategy: strategy}
	return true, nil
}

// RecordAdoptionVerified upgrades one delivery-verified replica to adoption
// verified. It needs new evidence (a restart success or a label-proven
// container); a file probe, a signal, an environment comparison or no evidence
// is refused, a signal-delivered or file-mode replica is never upgraded, and a
// replica without verified delivery for that operation is refused: adoption
// never claims delivery.
func (v *Versions) RecordAdoptionVerified(k Key, ref ReplicaRef, ev AdoptionEvidence) error {
	switch ev.Kind {
	case AdoptionEvidenceRestart, AdoptionEvidenceLabel:
	default:
		return fmt.Errorf("%w: adoption evidence %q never proves adoption", ErrNoEvidence, ev.Kind)
	}
	if !validRotationID(ev.RotationID) {
		return fmt.Errorf("%w: rotation id is empty or malformed", ErrNoEvidence)
	}
	return v.run(func(l *Ledger, now time.Time) (bool, error) {
		e := findEntry(l, k)
		if e == nil {
			return false, ErrNotFound
		}
		var r *Replica
		for i := range e.Replicas {
			if refOf(e.Replicas[i]) == (replicaRef{ref.Number, ref.ContainerName}) {
				if r != nil {
					return false, invalidTransition("the replica identity is ambiguous")
				}
				r = &e.Replicas[i]
			}
		}
		if r == nil {
			return false, ErrNotFound
		}
		if r.Delivery != DeliveryVerified || r.RotationID != ev.RotationID || r.Applied == nil || Compare(*r.Applied, ev.Version) != Same {
			return false, fmt.Errorf("%w: delivery of that version is not verified for that rotation", ErrNoEvidence)
		}
		switch OutcomeReason(r.Reason) {
		case WhyAdoptionUnverifiedSignal:
			return false, fmt.Errorf("%w: a signal-delivered replica is never adoption verified", ErrNoEvidence)
		case WhyAdoptionUnverifiedFile:
			return false, fmt.Errorf("%w: a file-mode secret stays adoption unverified (N19)", ErrNoEvidence)
		}
		if r.Adoption == AdoptionVerified {
			return false, nil
		}
		r.Adoption, r.Reason, r.At = AdoptionVerified, string(WhyDeliveryVerified), now
		return true, nil
	})
}

// ---------------------------------------------------------------- interruption

// HandleInterrupted records that the operation rotationID ended without
// delivery evidence. Only replicas with a requested delivery for it change;
// applied is never advanced and never trusted more than before. The caller
// states the cause from the journal (it is told by finishRotation or by the
// journal record, never by guessing); a replica already verified for the
// operation is refused here (journal disagreement is ReconcileFromJournal's
// rule R-A).
func (v *Versions) HandleInterrupted(k Key, rotationID string, cause InterruptCause) error {
	switch cause {
	case CauseUnresolved, CauseNeverRan, CauseFailed, CauseRolledBack:
	default:
		return invalidTransition("unknown interrupt cause")
	}
	if !validRotationID(rotationID) {
		return invalidTransition("the rotation id is empty or malformed")
	}
	return v.run(func(l *Ledger, now time.Time) (bool, error) {
		e := findEntry(l, k)
		if e == nil {
			return false, ErrNotFound
		}
		var idx []int
		for i, r := range e.Replicas {
			if r.RotationID != rotationID {
				continue
			}
			switch r.Delivery {
			case DeliveryVerified:
				return false, invalidTransition("the rotation is already delivery-verified")
			case DeliveryRequested:
				idx = append(idx, i)
			}
		}
		if len(idx) == 0 {
			return false, nil
		}
		interruptLocked(e, idx, cause, now)
		return true, nil
	})
}

func interruptLocked(e *Entry, idx []int, cause InterruptCause, now time.Time) {
	for _, i := range idx {
		r := &e.Replicas[i]
		r.Delivery, r.At = DeliveryNone, now
		switch cause {
		case CauseUnresolved:
			r.Adoption, r.Outcome, r.Reason = AdoptionUnknown, ReplicaFailed, string(WhyInterrupted)
		case CauseNeverRan:
			r.Outcome, r.Reason = ReplicaPending, string(WhyDeferred)
		case CauseFailed:
			r.Outcome, r.Reason = ReplicaFailed, string(WhyFailed)
		case CauseRolledBack:
			r.Outcome, r.Reason = ReplicaRolledBack, string(WhyRolledBack)
		}
	}
	if e.State == EntryBlocked {
		return
	}
	switch cause {
	case CauseUnresolved:
		e.State = EntryFailed
		e.LastOutcome = &LastOutcome{Kind: outcomeInterrupted, Reason: string(WhyInterrupted), At: now}
	case CauseNeverRan:
		if e.State == EntryAdopted {
			e.State = EntryPending
		}
		e.LastOutcome = &LastOutcome{Kind: outcomeDeferred, Reason: string(WhyDeferred), At: now}
	case CauseFailed:
		e.State = EntryFailed
		e.LastOutcome = &LastOutcome{Kind: outcomeFailed, Reason: string(WhyFailed), At: now}
	case CauseRolledBack:
		e.State = EntryRolledBack
		e.LastOutcome = &LastOutcome{Kind: outcomeRolledBack, Reason: string(WhyRolledBack), At: now}
	}
}

// ---------------------------------------------------------------- block / unblock

// Block records that an entry cannot be rotated (ErrNoTargetsRotated:
// no_rotating_strategy; D61-3: strategy_none; ambiguous_provider). It is a
// record for the engine and status, never a Docker precondition.
func (v *Versions) Block(k Key, reason BlockReason) error {
	why, ok := reason.outcomeReason()
	if !ok {
		return invalidTransition("unknown block reason")
	}
	return v.run(func(l *Ledger, now time.Time) (bool, error) {
		e := findEntry(l, k)
		if e == nil {
			return false, ErrNotFound
		}
		for _, r := range e.Replicas {
			if r.Delivery == DeliveryRequested {
				return false, invalidTransition("a delivery is in flight; resolve it first")
			}
		}
		changed := false
		if e.State != EntryBlocked || e.LastOutcome == nil || e.LastOutcome.Kind != outcomeBlocked || e.LastOutcome.Reason != string(why) {
			e.State = EntryBlocked
			e.LastOutcome = &LastOutcome{Kind: outcomeBlocked, Reason: string(why), At: now}
			changed = true
		}
		for i := range e.Replicas {
			r := &e.Replicas[i]
			if r.Outcome == ReplicaBlocked && r.Reason == string(why) {
				continue
			}
			r.Outcome, r.Reason, r.At = ReplicaBlocked, string(why), now
			if r.Delivery == DeliveryVerified {
				r.Delivery = DeliveryNone // applied and adoption are kept
			}
			changed = true
		}
		return changed, nil
	})
}

// Unblock is step one of the two-step unblock (D61-1): it changes only the
// entry state (blocked -> pending when desired differs from applied or applied
// is unknown, else idle) when the trigger of design section 10.2 holds against
// the live set and the recorded replicas. Step two, the rotation, is the
// ordinary engine path under the M0 guard. It needs healthy persistence.
func (v *Versions) Unblock(k Key, in UnblockInput) error {
	if !v.s.PersistenceHealthy() {
		return ErrPersistenceUnavailable
	}
	if err := validateLive(in.Live); err != nil {
		return err
	}
	return v.run(func(l *Ledger, _ time.Time) (bool, error) {
		e := findEntry(l, k)
		if e == nil {
			return false, ErrNotFound
		}
		if e.State != EntryBlocked || e.LastOutcome == nil {
			return false, invalidTransition("the entry is not blocked")
		}
		reason := OutcomeReason(e.LastOutcome.Reason)
		switch in.Trigger {
		case UnblockNewEligibleReplica, UnblockStrategyNowDispatches:
			if reason != WhyNoRotatingStrategy {
				return false, invalidTransition("trigger %s does not apply to block reason %s", in.Trigger, reason)
			}
			if !eligibleReplicaLive(e, in.Live) {
				return false, invalidTransition("trigger %s does not hold against the live set", in.Trigger)
			}
		case UnblockSecretStrategyChanged:
			if reason != WhyStrategyNone {
				return false, invalidTransition("trigger %s does not apply to block reason %s", in.Trigger, reason)
			}
			if in.SecretStrategyNone {
				return false, invalidTransition("the secret strategy is still none")
			}
		default:
			return false, invalidTransition("no unblock trigger for block reason %s", reason)
		}
		e.State = EntryPending
		if e.Applied != nil && e.Desired != nil && Compare(*e.Desired, *e.Applied) == Same {
			e.State = EntryIdle
		}
		return true, nil
	})
}

// eligibleReplicaLive reports whether a recorded replica marked
// eligibility_changed (by Observe) is live, eligible and the same container.
func eligibleReplicaLive(e *Entry, live []LiveReplica) bool {
	for _, r := range e.Replicas {
		if r.Reason != string(WhyEligibilityChanged) || r.Outcome != ReplicaPending {
			continue
		}
		for _, l := range live {
			if refOfLive(l) == refOf(r) && l.ContainerID == r.ContainerID && l.Eligible {
				return true
			}
		}
	}
	return false
}
