-- Central store schema, version 1. Timestamps are UTC unix seconds.

CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    wrapped_dek BLOB NOT NULL CHECK (length(wrapped_dek) > 0),
    dek_key_id  TEXT NOT NULL CHECK (length(dek_key_id) > 0),
    dek_version INTEGER NOT NULL DEFAULT 1 CHECK (dek_version >= 1),
    created_at  INTEGER NOT NULL
);

CREATE TABLE environments (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (project_id, slug),
    UNIQUE (id, project_id)
);

CREATE TABLE secrets (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    name           TEXT NOT NULL,
    created_at     INTEGER NOT NULL,
    created_by     TEXT NOT NULL,
    deleted_at     INTEGER,
    deleted_by     TEXT,
    -- the environment must belong to the project
    FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE RESTRICT
);
-- Names are unique among live secrets only, so a deleted name can be reused.
CREATE UNIQUE INDEX secrets_live_name ON secrets(environment_id, name) WHERE deleted_at IS NULL;

CREATE TABLE secret_versions (
    secret_id   TEXT NOT NULL REFERENCES secrets(id) ON DELETE RESTRICT,
    version     INTEGER NOT NULL CHECK (version >= 1),
    ciphertext  BLOB NOT NULL CHECK (length(ciphertext) > 0),
    nonce       BLOB NOT NULL CHECK (length(nonce) = 12),
    dek_version INTEGER NOT NULL CHECK (dek_version >= 1),
    created_at  INTEGER NOT NULL,
    created_by  TEXT NOT NULL,
    comment     TEXT NOT NULL DEFAULT '',
    disabled_at INTEGER,
    disabled_by TEXT,
    PRIMARY KEY (secret_id, version)
) WITHOUT ROWID;

-- Versions are immutable. The only permitted change is the one-way transition
-- NULL -> set of disabled_at/disabled_by. Deletion is not permitted (purge is
-- a later, explicit, audited operation with its own migration).
CREATE TRIGGER secret_versions_immutable BEFORE UPDATE ON secret_versions
WHEN NEW.secret_id IS NOT OLD.secret_id OR NEW.version IS NOT OLD.version
  OR NEW.ciphertext IS NOT OLD.ciphertext OR NEW.nonce IS NOT OLD.nonce
  OR NEW.dek_version IS NOT OLD.dek_version OR NEW.created_at IS NOT OLD.created_at
  OR NEW.created_by IS NOT OLD.created_by OR NEW.comment IS NOT OLD.comment
  OR (OLD.disabled_at IS NOT NULL AND (NEW.disabled_at IS NOT OLD.disabled_at OR NEW.disabled_by IS NOT OLD.disabled_by))
BEGIN
    SELECT RAISE(ABORT, 'secret versions are immutable');
END;

CREATE TRIGGER secret_versions_no_delete BEFORE DELETE ON secret_versions
BEGIN
    SELECT RAISE(ABORT, 'secret versions cannot be deleted');
END;
