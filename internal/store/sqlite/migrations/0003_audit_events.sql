-- Append-only, hash-chained audit history (Phase 2 Step 5.3, approved design:
-- docs/reviews/2026-10-25-central-store-phase2-audit-chain-design.md).
--
-- Rows are insert-only, enforced by the triggers below -- same convention as
-- store_meta and secret_versions. seq is the chain's total order; event_hash
-- is computed by the application as H(prev_hash || canonical(fields)) and
-- stored verbatim, never recomputed by SQL.
--
-- Explicit, approved limitations (do not weaken this comment to imply more
-- than the design actually provides):
--   * This chain detects modification of a covered row and detects a broken
--     prev_hash/event_hash link between rows.
--   * It does NOT by itself detect deletion of the chain's current tail, and
--     it does NOT detect the whole database file being replaced by an older,
--     internally-valid backup -- both are explicitly deferred (Gate 1 of the
--     approved decision resolution), pending a future external trust anchor.
--   * There is no separate MAC/signing key in this milestone (Gate 3,
--     approved): tamper-evidence here relies on the hash chain alone.
--   * reason_category is a closed set of failure categories, never a raw
--     error string. actor_label is caller-supplied attribution only, never
--     authenticated identity. A verified-identity column for a future Option
--     B integration (Step 5.2) was deliberately NOT added here: this
--     milestone has no socket to populate it, and an unused, never-hashed
--     column is a latent tamper-evidence gap waiting to be populated without
--     a matching canonicalization update. Add it in the same change that
--     starts writing to it, with canonicalizeAuditEvent and
--     supportedAuditEventSchemaVersion updated together (see audit.go).

CREATE TABLE audit_events (
    seq                 INTEGER PRIMARY KEY AUTOINCREMENT,
    id                  TEXT NOT NULL UNIQUE CHECK (length(id) > 0),
    schema_version      INTEGER NOT NULL CHECK (schema_version >= 1),
    event_type          TEXT NOT NULL CHECK (length(event_type) > 0),
    outcome             TEXT NOT NULL CHECK (outcome IN ('success', 'failure')),
    reason_category     TEXT,
    actor_label         TEXT,
    project_slug        TEXT,
    environment_slug    TEXT,
    secret_name         TEXT,
    version             INTEGER,
    created_at          INTEGER NOT NULL,
    prev_hash           BLOB,
    event_hash          BLOB NOT NULL
);

CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events are immutable');
END;

CREATE TRIGGER audit_events_no_delete BEFORE DELETE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events cannot be deleted (append-only; see Gate 5 of the approved decision resolution)');
END;
