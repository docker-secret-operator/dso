package ledger

// NeedsRotation is the pure decision function of design section 10.2 (no I/O,
// no clock, no store). It returns rotate only for a genuinely observed change
// after a baseline exists, or for a replica that became eligible (D61-2). A
// missing, corrupt, unreadable, newer-schema or key-lost ledger, a missing
// entry and an incomparable identity yield DecisionUnknown, never "changed"
// (X3); an adopted-unknown entry yields no_rotate. It never asks for more than
// one rotation of one entry and never decides for any other entry, so it
// cannot cause a mass rotation. The M0 guard is not consulted here: it still
// decides per target whether a wanted rotation may run (X1).
func NeedsRotation(in RotationInput) Decision {
	unknown := func(r DecisionReason) Decision { return Decision{Kind: DecisionUnknown, Reason: r} }
	no := func(r DecisionReason) Decision { return Decision{Kind: DecisionNoRotate, Reason: r} }
	yes := func(r DecisionReason) Decision { return Decision{Kind: DecisionRotate, Reason: r} }
	switch in.Condition {
	case ConditionOK:
	case ConditionMissing:
		return unknown(DecisionLedgerMissing)
	case ConditionCorrupt:
		return unknown(DecisionLedgerCorrupt)
	case ConditionUnreadable:
		return unknown(DecisionLedgerUnreadable)
	case ConditionNewerSchema:
		return unknown(DecisionLedgerNewerSchema)
	case ConditionKeyLost:
		return unknown(DecisionKeyLost)
	default:
		return unknown(DecisionKeyUnavailableCause)
	}
	e := in.Entry
	if e == nil {
		return unknown(DecisionNoEntry)
	}
	if e.Desired == nil {
		return no(DecisionNoDesired)
	}
	switch e.State {
	case EntryBlocked:
		return no(DecisionBlocked)
	case EntryAdopted:
		return no(DecisionAdoptedUnknown)
	}
	if e.Applied == nil {
		if e.DesiredSeq > 1 {
			return yes(DecisionChangedSinceAdopt)
		}
		return no(DecisionAdoptedUnknown)
	}
	switch Compare(*e.Desired, *e.Applied) {
	case Different:
		return yes(DecisionDesiredDiffers)
	case Incomparable:
		return unknown(DecisionIncomparable)
	}
	for _, r := range e.Replicas {
		if r.Reason == string(WhyEligibilityChanged) && r.Outcome == ReplicaPending && r.Delivery == DeliveryNone {
			return yes(DecisionEligibilityChanged)
		}
	}
	return no(DecisionUpToDate)
}

// NeedsRotation evaluates the pure decision for k against the published
// ledger. A store that cannot compute keyed identities (key unsafe,
// malformed or not generated) reports ConditionKeyUnavailable.
func (v *Versions) NeedsRotation(k Key) Decision {
	in := RotationInput{Condition: ConditionOK}
	if v.s.mode == ModeMemoryOnly {
		switch v.s.reason {
		case ReasonKeyUnsafe, ReasonKeyMalformed, ReasonKeyGenerateFailed:
			in.Condition = ConditionKeyUnavailable
		}
	}
	if e, ok := v.s.Get(k); ok {
		in.Entry = &e
	}
	return NeedsRotation(in)
}
