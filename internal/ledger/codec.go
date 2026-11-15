package ledger

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ErrChecksum is wrapped (together with ErrInvalid) when a document's
// body_sha256 does not match its content.
var ErrChecksum = errors.New("ledger: body checksum mismatch")

// SchemaVersionError reports a document whose schema_version is newer than
// this package supports. It is deliberately not an ErrInvalid: a newer file is
// not corrupt, and a caller must be able to tell the two apart (the file must
// then be left untouched). It is returned as soon as the minimum envelope of
// the document has been validated (design §36.2, phase 1), before any
// structural check of the document, so a newer document that also contains
// fields unknown to this version, duplicate keys or case-variant names is still
// reported as a schema-version problem.
type SchemaVersionError struct {
	Found     int
	Supported int
}

func (e *SchemaVersionError) Error() string {
	return fmt.Sprintf("ledger: schema version %d is newer than the supported version %d", e.Found, e.Supported)
}

// CanonicalBody returns the canonical JSON of the document with body_sha256
// forced to "": compact, UTF-8, struct-declaration field order, no HTML
// escaping, no trailing newline, every slice rendered as [] rather than null,
// and entries, replicas, prior replicas and recovery records in the canonical
// order of design §16.2 rules 12-14. planned actions and done results keep
// their order (they are positional). The same content always yields the same
// bytes, whatever the order it was supplied in. l is not modified; CanonicalBody
// does not validate l (Marshal does).
func CanonicalBody(l *Ledger) ([]byte, error) {
	if l == nil {
		return nil, fmt.Errorf("%w: document is nil", ErrInvalid)
	}
	c := normalized(l)
	c.BodySHA256 = ""
	return encode(c)
}

// ComputeBodySHA256 returns the lowercase-hex SHA-256 of CanonicalBody(l).
func ComputeBodySHA256(l *Ledger) (string, error) {
	body, err := CanonicalBody(l)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// Marshal validates l (in the order it was supplied, so a duplicate logical key
// is rejected before any sorting) and returns its canonical JSON with
// body_sha256 set to the checksum of the content. It does not modify l and
// performs no I/O. The output is deterministic and independent of the order of
// the entries, replicas and recovery records in l.
func Marshal(l *Ledger) ([]byte, error) {
	if l == nil {
		return nil, fmt.Errorf("%w: document is nil", ErrInvalid)
	}
	probe := *l
	probe.BodySHA256 = "" // a stale checksum in the input is irrelevant: it is recomputed
	if err := probe.Validate(); err != nil {
		return nil, err
	}
	c := normalized(l)
	sum, err := ComputeBodySHA256(c)
	if err != nil {
		return nil, err
	}
	c.BodySHA256 = sum
	return encode(c)
}

// Unmarshal strictly decodes and fully checks a document, in two phases
// (design §36.2):
//
// Phase 1, the minimum envelope, for every document: valid UTF-8, no byte-order
// mark, valid JSON with no trailing data, an object with exactly one integer
// top-level schema_version. A schema_version greater than the supported one
// returns *SchemaVersionError immediately; nothing else is examined.
//
// Phase 2, only for the supported version: duplicate keys at every depth,
// exact case-sensitive field names, unknown fields, the presence policy of
// design §29.1, decoding, Validate, and finally the body_sha256 checksum of the
// canonical decoded value (so harmless formatting changes and a different
// entry order do not invalidate a document).
//
// Every failure other than a newer schema version wraps ErrInvalid. The result
// is in canonical order. Error messages name a field path and a reason class
// and never echo document content.
func Unmarshal(data []byte) (*Ledger, error) {
	if err := checkEnvelope(data); err != nil {
		return nil, err
	}
	var l Ledger
	if err := decodeStrict(data, &l); err != nil {
		return nil, err
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	want, err := ComputeBodySHA256(&l)
	if err != nil {
		return nil, err
	}
	if l.BodySHA256 == "" || subtle.ConstantTimeCompare([]byte(want), []byte(l.BodySHA256)) != 1 {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, ErrChecksum)
	}
	return normalized(&l), nil
}

func encode(l *Ledger) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(l); err != nil {
		return nil, fmt.Errorf("ledger: encode: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// normalized returns a copy of l in canonical form: every nil slice is empty,
// and entries, replicas, prior replicas and recovery records are sorted
// (design §16.2 rules 3 and 12-14). Every slice that is reordered or exposed is
// copied first, so the input is never modified and the copy never aliases the
// input's slices; planned actions and done results keep their order. Version
// and request pointers are shared but never written.
func normalized(l *Ledger) *Ledger {
	c := *l
	c.Entries = make([]Entry, len(l.Entries))
	for i := range l.Entries {
		e := l.Entries[i]
		e.Replicas = sortedReplicas(e.Replicas)
		if e.Rotation != nil {
			r := *e.Rotation
			r.PriorReplicas = sortedReplicas(r.PriorReplicas)
			e.Rotation = &r
		}
		c.Entries[i] = e
	}
	sort.SliceStable(c.Entries, func(i, j int) bool { return keyLess(c.Entries[i].Key, c.Entries[j].Key) })

	c.Recoveries = make([]RecoveryRecord, len(l.Recoveries))
	for i := range l.Recoveries {
		r := l.Recoveries[i]
		r.Planned = copyNonNil(r.Planned)
		r.Done = copyNonNil(r.Done)
		c.Recoveries[i] = r
	}
	sort.SliceStable(c.Recoveries, func(i, j int) bool {
		a, b := c.Recoveries[i], c.Recoveries[j]
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.Before(b.StartedAt)
		}
		return a.RecoveryID < b.RecoveryID
	})
	return &c
}

// keyLess orders entry keys by (project, service, provider, secret), each
// compared bytewise as a UTF-8 string, field by field.
func keyLess(a, b Key) bool {
	if a.Project != b.Project {
		return a.Project < b.Project
	}
	if a.Service != b.Service {
		return a.Service < b.Service
	}
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	return a.Secret < b.Secret
}

// sortedReplicas returns a sorted copy, by (container_name, container_id).
func sortedReplicas(s []Replica) []Replica {
	c := copyNonNil(s)
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].ContainerName != c[j].ContainerName {
			return c[i].ContainerName < c[j].ContainerName
		}
		return c[i].ContainerID < c[j].ContainerID
	})
	return c
}

// copyNonNil returns a new, non-nil slice with the same elements.
func copyNonNil[T any](s []T) []T {
	c := make([]T, len(s))
	copy(c, s)
	return c
}
