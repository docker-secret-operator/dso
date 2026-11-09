-- Database-level metadata. Used to bind the database to its root key: the
-- service writes a canary here that only the right root key can unwrap, so a
-- different key can never silently create projects or wrap new data keys in
-- this database (see centralsvc.Service.Bind).
--
-- Rows are insert-only. A later migration that needs to change them (root-key
-- rotation) must do so explicitly. An attacker with raw write access can drop
-- these triggers; that only denies service (the canary still has to unwrap
-- under the real key), it does not grant access.

CREATE TABLE store_meta (
    key   TEXT PRIMARY KEY CHECK (length(key) > 0),
    value BLOB NOT NULL
) WITHOUT ROWID;

CREATE TRIGGER store_meta_no_update BEFORE UPDATE ON store_meta
BEGIN
    SELECT RAISE(ABORT, 'store metadata is immutable');
END;

CREATE TRIGGER store_meta_no_delete BEFORE DELETE ON store_meta
BEGIN
    SELECT RAISE(ABORT, 'store metadata cannot be deleted');
END;
