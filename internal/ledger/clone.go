package ledger

import (
	"encoding/hex"
	"io"
	"time"
)

// Clone returns a deep copy of the document: every pointer and every slice is
// copied, so the copy shares no mutable memory with l (time.Time values share
// only their immutable location pointers). Nil slices stay nil, so a clone is
// reflect.DeepEqual to its source. It is the copy-on-write primitive of the
// store (design §8.1, owner decision D-S2-3).
func (l *Ledger) Clone() *Ledger {
	if l == nil {
		return nil
	}
	c := *l
	c.Entries = cloneSlice(l.Entries, cloneEntry)
	c.Recoveries = cloneSlice(l.Recoveries, cloneRecovery)
	return &c
}

func cloneSlice[T any](s []T, f func(T) T) []T {
	if s == nil {
		return nil
	}
	out := make([]T, len(s))
	for i := range s {
		out[i] = f(s[i])
	}
	return out
}

func cloneVersion(v *Version) *Version {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

func cloneReplica(r Replica) Replica {
	r.Applied = cloneVersion(r.Applied)
	return r
}

func cloneEntry(e Entry) Entry {
	e.Desired = cloneVersion(e.Desired)
	e.Applied = cloneVersion(e.Applied)
	e.Replicas = cloneSlice(e.Replicas, cloneReplica)
	if e.Rotation != nil {
		r := *e.Rotation
		r.PriorReplicas = cloneSlice(r.PriorReplicas, cloneReplica)
		e.Rotation = &r
	}
	if e.Request != nil {
		r := *e.Request
		e.Request = &r
	}
	if e.LastOutcome != nil {
		o := *e.LastOutcome
		e.LastOutcome = &o
	}
	return e
}

func cloneRecovery(r RecoveryRecord) RecoveryRecord {
	r.Planned = cloneSlice(r.Planned, func(a Action) Action { return a })
	r.Done = cloneSlice(r.Done, func(a ActionResult) ActionResult { return a })
	return r
}

// newID returns a random 128-bit identifier as 32 lowercase hex characters.
func newID(random io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// newLedger returns a valid, empty document for the given key id: the starting
// point of a first run and of every recovery that begins a new ledger. It is
// not yet persisted (generation 0).
func newLedger(keyID string, now time.Time, random io.Reader) (*Ledger, error) {
	id, err := newID(random)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	return &Ledger{SchemaVersion: SchemaVersion, LedgerID: id, HMACKeyID: keyID, CreatedAt: now, UpdatedAt: now}, nil
}

// hasHMACIdentity reports whether any persisted value anywhere in the document
// is a key-dependent (hmac) Version. It walks the whole document by reflection,
// so a Version in any record, including one a later schema might add to the
// recovery journal, is covered (owner decision, empty hmac_key_id Option B).
func hasHMACIdentity(v any) bool {
	return walkHMAC(reflectValue(v))
}
