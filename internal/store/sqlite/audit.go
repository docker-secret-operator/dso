package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/docker-secret-operator/dso/internal/store"
)

// supportedAuditEventSchemaVersion is the only store.AuditEvent.SchemaVersion
// this binary knows how to write or verify, deliberately kept separate from
// the main database's own schema_migrations/SchemaVersion (sqlite.go) --
// those version the *table layout* via forward-only SQL migrations; this one
// versions the *canonical field set* that computeEventHash covers, which is
// a Go-level concern with no SQL migration of its own. Bumping this requires
// also updating canonicalizeAuditEvent and VerifyAuditChain together, in the
// same change, since the version number IS the promise that they match.
const supportedAuditEventSchemaVersion = 1

// canonicalizeAuditEvent produces a deterministic byte encoding of the
// hash-covered fields of ev, in a fixed order, with each field length-prefixed
// so no concatenation ambiguity can make two different events hash the same
// (the same convention internal/crypto's AAD builders already use). EventHash
// and PrevHash itself are never part of this encoding; PrevHash is mixed in
// separately by computeEventHash so the chain's linkage is explicit.
func canonicalizeAuditEvent(ev store.AuditEvent) []byte {
	var buf bytes.Buffer
	appendAuditField(&buf, []byte(ev.ID))
	appendAuditField(&buf, []byte(strconv.Itoa(ev.SchemaVersion)))
	appendAuditField(&buf, []byte(ev.EventType))
	appendAuditField(&buf, []byte(ev.Outcome))
	appendAuditField(&buf, []byte(ev.ReasonCategory))
	appendAuditField(&buf, []byte(ev.ActorLabel))
	appendAuditField(&buf, []byte(ev.ProjectSlug))
	appendAuditField(&buf, []byte(ev.EnvironmentSlug))
	appendAuditField(&buf, []byte(ev.SecretName))
	appendAuditField(&buf, []byte(strconv.Itoa(ev.Version)))
	appendAuditField(&buf, []byte(strconv.FormatInt(auditEventTimestamp(ev), 10)))
	return buf.Bytes()
}

func auditEventTimestamp(ev store.AuditEvent) int64 {
	if ev.CreatedAt.IsZero() {
		return 0
	}
	return ts(ev.CreatedAt)
}

func appendAuditField(buf *bytes.Buffer, f []byte) {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(f)))
	buf.Write(lenBuf[:])
	buf.Write(f)
}

// computeEventHash is event_hash = SHA-256(prevHash || canonical(ev)). This is
// the plain hash chain approved for this milestone (Gate 3): no separate
// MAC/signing key. See the migration's own comment for the explicit,
// unchanged limitations (does not detect tail deletion or whole-database
// rollback to an older valid backup).
func computeEventHash(prevHash []byte, ev store.AuditEvent) []byte {
	h := sha256.New()
	h.Write(prevHash)
	h.Write(canonicalizeAuditEvent(ev))
	return h.Sum(nil)
}

// nullableBytes returns a nil-safe driver value: an empty/nil slice becomes
// SQL NULL rather than a zero-length BLOB, so the first event's prev_hash
// reads back as nil, not []byte{}.
func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (o ops) AppendAuditEvent(ctx context.Context, ev store.AuditEvent) (store.AuditEvent, error) {
	if ev.ID == "" || ev.EventType == "" || ev.Outcome == "" {
		return store.AuditEvent{}, store.ErrInvalid
	}
	if ev.Outcome != store.AuditSuccess && ev.Outcome != store.AuditFailure {
		return store.AuditEvent{}, store.ErrInvalid
	}
	if ev.SchemaVersion == 0 {
		ev.SchemaVersion = supportedAuditEventSchemaVersion
	} else if ev.SchemaVersion != supportedAuditEventSchemaVersion {
		// Narrowest backward-compatible rule: this binary can only write the
		// one schema version it also knows how to verify. A caller explicitly
		// asking for any other version (0 already defaults above; negative or
		// a future version both land here) is rejected before anything is
		// written, rather than silently accepted and later failing (or
		// silently "passing") verification.
		return store.AuditEvent{}, store.ErrInvalid
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = o.now()
	}

	var prevHash []byte
	err := o.q.QueryRowContext(ctx, `SELECT event_hash FROM audit_events ORDER BY seq DESC LIMIT 1`).Scan(&prevHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.AuditEvent{}, mapErr(err)
	}
	ev.PrevHash = prevHash
	ev.EventHash = computeEventHash(prevHash, ev)

	var versionCol any
	if ev.Version != 0 {
		versionCol = ev.Version
	}
	_, err = o.q.ExecContext(ctx, `INSERT INTO audit_events(
		id, schema_version, event_type, outcome, reason_category, actor_label,
		project_slug, environment_slug, secret_name,
		version, created_at, prev_hash, event_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.SchemaVersion, string(ev.EventType), string(ev.Outcome),
		nullString(ev.ReasonCategory), nullString(ev.ActorLabel),
		nullString(ev.ProjectSlug), nullString(ev.EnvironmentSlug), nullString(ev.SecretName),
		versionCol, ts(ev.CreatedAt), nullableBytes(ev.PrevHash), ev.EventHash)
	if err != nil {
		return store.AuditEvent{}, mapErr(err)
	}
	return ev, nil
}

func (o ops) ListAuditEvents(ctx context.Context) ([]store.AuditEvent, error) {
	rows, err := o.q.QueryContext(ctx, `SELECT
		id, schema_version, event_type, outcome, reason_category, actor_label,
		project_slug, environment_slug, secret_name, version, created_at, prev_hash, event_hash
		FROM audit_events ORDER BY seq ASC`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []store.AuditEvent
	for rows.Next() {
		var ev store.AuditEvent
		var reason, actor, project, env, secret sql.NullString
		var version sql.NullInt64
		var created int64
		if err := rows.Scan(&ev.ID, &ev.SchemaVersion, &ev.EventType, &ev.Outcome, &reason, &actor,
			&project, &env, &secret, &version, &created, &ev.PrevHash, &ev.EventHash); err != nil {
			return nil, err
		}
		ev.ReasonCategory = reason.String
		ev.ActorLabel = actor.String
		ev.ProjectSlug = project.String
		ev.EnvironmentSlug = env.String
		ev.SecretName = secret.String
		ev.Version = int(version.Int64)
		ev.CreatedAt = fromTS(created)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// VerifyAuditChain recomputes every event's hash from the previous event's
// stored hash and checks it against the stored event_hash, and checks that
// each row's stored prev_hash matches the previous row's stored event_hash.
// It returns a nil error only if the chain is fully internally consistent.
//
// Explicit, approved limitation (do not represent this as stronger than it
// is): this function can only verify the events that are present. It cannot
// detect that the chain's tail was deleted, or that the whole database file
// was replaced by an older, internally-valid backup -- both produce a chain
// that passes this exact check. See the migration's own comment and Gate 1 of
// the approved decision resolution.
func VerifyAuditChain(events []store.AuditEvent) error {
	var prevHash []byte
	for i, ev := range events {
		if ev.SchemaVersion != supportedAuditEventSchemaVersion {
			// Reject outright rather than hash-check it anyway: a different
			// schema version is a different, unverified canonical field set
			// (or none at all, for an invalid value) -- verifying it against
			// THIS binary's canonicalization would either reject a
			// legitimately-written future event for the wrong reason, or
			// worse, silently accept one that happens to collide, neither of
			// which this function should claim. No event detail beyond the
			// index/id/version number is included in the error.
			return &ChainError{Index: i, EventID: ev.ID, Reason: fmt.Sprintf(
				"unsupported audit event schema version %d (this binary supports version %d)",
				ev.SchemaVersion, supportedAuditEventSchemaVersion)}
		}
		wantPrev := prevHash
		if !bytes.Equal(ev.PrevHash, wantPrev) {
			return &ChainError{Index: i, EventID: ev.ID, Reason: "prev_hash does not match the previous event's event_hash"}
		}
		recomputed := computeEventHash(wantPrev, store.AuditEvent{
			ID: ev.ID, SchemaVersion: ev.SchemaVersion, EventType: ev.EventType, Outcome: ev.Outcome,
			ReasonCategory: ev.ReasonCategory, ActorLabel: ev.ActorLabel, ProjectSlug: ev.ProjectSlug,
			EnvironmentSlug: ev.EnvironmentSlug, SecretName: ev.SecretName, Version: ev.Version, CreatedAt: ev.CreatedAt,
		})
		if !bytes.Equal(recomputed, ev.EventHash) {
			return &ChainError{Index: i, EventID: ev.ID, Reason: "event_hash does not match the event's own fields (tampered or corrupt)"}
		}
		prevHash = ev.EventHash
	}
	return nil
}

// ChainError reports where and why audit-chain verification failed.
type ChainError struct {
	Index   int
	EventID string
	Reason  string
}

func (e *ChainError) Error() string {
	return "audit chain verification failed at index " + strconv.Itoa(e.Index) + " (event " + e.EventID + "): " + e.Reason
}
