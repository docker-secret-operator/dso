// Package sqlite implements store.Store on SQLite in WAL mode using the
// pure-Go modernc.org/sqlite driver (no cgo). All SQL is parameterised.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	msqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/docker-secret-operator/dso/internal/store"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration is one forward-only schema step.
type Migration struct {
	Version int
	SQL     string
}

// Options configures Open. The zero value is production-correct.
type Options struct {
	// BusyTimeout is how long a writer waits for another writer (in this or
	// another process) before failing with a busy error. Default 5s.
	BusyTimeout time.Duration
	// Migrations overrides the embedded set. Intended for tests.
	Migrations []Migration
	// Now overrides the clock. Intended for tests.
	Now func() time.Time
}

// Store is the SQLite-backed store.Store.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

var _ store.Store = (*Store)(nil)

// EmbeddedMigrations returns the migrations compiled into the binary, sorted.
func EmbeddedMigrations() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		name := e.Name()
		i := strings.IndexByte(name, '_')
		if i < 0 || !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("bad migration file name %q", name)
		}
		v, err := strconv.Atoi(name[:i])
		if err != nil {
			return nil, fmt.Errorf("bad migration file name %q", name)
		}
		b, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Open opens (creating if necessary) the database at path with mode 0600 in a
// 0700 directory, enables WAL and foreign keys, and applies pending
// migrations. It returns clear errors for a database that is unreadable,
// insecurely permissioned, corrupt, or from a newer schema.
func Open(ctx context.Context, path string, opts Options) (*Store, error) {
	if opts.BusyTimeout <= 0 {
		opts.BusyTimeout = 5 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	migs := opts.Migrations
	if migs == nil {
		var err error
		if migs, err = EmbeddedMigrations(); err != nil {
			return nil, fmt.Errorf("loading migrations: %w", err)
		}
	}
	if err := prepareFile(path); err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", opts.BusyTimeout.Milliseconds()))
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate") // writers take the lock up front: no deadlocking upgrades
	dsn := (&url.URL{Scheme: "file", Opaque: escapePath(path), RawQuery: q.Encode()}).String()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	db.SetMaxOpenConns(8)
	s := &Store{db: db, now: opts.Now}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	var mode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || !strings.EqualFold(mode, "wal") {
		db.Close()
		return nil, fmt.Errorf("database %s: WAL mode could not be enabled (journal_mode=%q, err=%v)", path, mode, err)
	}
	if err := s.migrate(ctx, path, migs); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func escapePath(p string) string {
	// url.URL.Opaque is emitted verbatim; escape '?' '#' '%' so a path cannot
	// inject DSN options.
	r := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")
	return r.Replace(p)
}

func prepareFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating database directory: %w", err)
	}
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("creating database file: %w", err)
		}
		if err == nil {
			return f.Close()
		}
		return nil // lost a creation race: the other process made it 0600
	case err != nil:
		return fmt.Errorf("inspecting database file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("database path %s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("database file %s has mode %04o; it must not be accessible by group or others (chmod 600 %s)",
			path, fi.Mode().Perm(), path)
	}
	return nil
}

// Close checkpoints the WAL (best effort) and closes the handle.
func (s *Store) Close() error {
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context, path string, migs []Migration) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("initialising schema_migrations: %w", err)
	}
	latest := 0
	for i, m := range migs {
		if m.Version <= latest {
			return fmt.Errorf("migrations are not strictly increasing at version %d", m.Version)
		}
		if i == 0 && m.Version != 1 {
			return fmt.Errorf("first migration must be version 1, got %d", m.Version)
		}
		latest = m.Version
	}
	cur, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if cur > latest {
		return fmt.Errorf("%w: database is at schema %d, this binary supports up to %d", store.ErrSchemaTooNew, cur, latest)
	}
	if cur == latest {
		return nil
	}
	if cur > 0 {
		// Existing data: take a restorable copy before changing the schema.
		if err := s.backupBeforeMigration(ctx, path, cur); err != nil {
			return fmt.Errorf("pre-migration backup failed, migration not attempted: %w", err)
		}
	}
	for _, m := range migs {
		if m.Version <= cur {
			continue
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("applying migration %d (database left at schema %d): %w", m.Version, m.Version-1, err)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, m Migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Another process may have applied it between our check and our lock.
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.Version).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
		m.Version, s.now().UTC().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) backupBeforeMigration(ctx context.Context, path string, from int) error {
	dst := fmt.Sprintf("%s.pre-migration-v%d.bak", path, from)
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil // an earlier attempt already preserved this schema version
		}
		return err
	}
	f.Close()
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Chmod(dst, 0o600)
}

// SchemaVersion returns the highest applied migration (0 for a fresh database).
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, fmt.Errorf("reading schema version: %w", err)
	}
	return int(v.Int64), nil
}

// ---- helpers --------------------------------------------------------------

func isCode(err error, codes ...int) bool {
	var se *msqlite.Error
	if errors.As(err, &se) {
		for _, c := range codes {
			if se.Code() == c {
				return true
			}
		}
	}
	return false
}

// mapErr converts driver errors to store sentinel errors. Raw driver errors
// (which can quote SQL fragments) are kept out of the message for conflicts.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return store.ErrNotFound
	case isCode(err, sqlitelib.SQLITE_CONSTRAINT_UNIQUE, sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY):
		return store.ErrConflict
	case isCode(err, sqlitelib.SQLITE_CONSTRAINT_FOREIGNKEY):
		return fmt.Errorf("%w: referenced parent does not exist", store.ErrNotFound)
	}
	return err
}

func ts(t time.Time) int64 { return t.UTC().Unix() }
func fromTS(v int64) time.Time {
	return time.Unix(v, 0).UTC()
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Rolled back on error and on panic (e.g. in a caller-supplied SealFunc);
	// otherwise the open write transaction would block every other writer.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// ---- projects / environments ----------------------------------------------

func (s *Store) CreateProject(ctx context.Context, p store.Project) error {
	if p.ID == "" || p.Slug == "" || len(p.WrappedDEK) == 0 || p.DEKKeyID == "" {
		return store.ErrInvalid
	}
	if p.DEKVersion == 0 {
		p.DEKVersion = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id, slug, name, wrapped_dek, dek_key_id, dek_version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, p.ID, p.Slug, p.Name, p.WrappedDEK, p.DEKKeyID, p.DEKVersion, s.now().UTC().Unix())
	return mapErr(err)
}

const projectCols = `id, slug, name, wrapped_dek, dek_key_id, dek_version, created_at`

func scanProject(r interface{ Scan(...any) error }) (store.Project, error) {
	var p store.Project
	var c int64
	err := r.Scan(&p.ID, &p.Slug, &p.Name, &p.WrappedDEK, &p.DEKKeyID, &p.DEKVersion, &c)
	p.CreatedAt = fromTS(c)
	return p, err
}

func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (store.Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE slug = ?`, slug))
	return p, mapErr(err)
}

func (s *Store) GetProject(ctx context.Context, id string) (store.Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
	return p, mapErr(err)
}

func (s *Store) ListProjects(ctx context.Context) ([]store.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectCols+` FROM projects ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreateEnvironment(ctx context.Context, e store.Environment) error {
	if e.ID == "" || e.ProjectID == "" || e.Slug == "" {
		return store.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO environments(id, project_id, slug, name, created_at) VALUES (?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.Slug, e.Name, s.now().UTC().Unix())
	return mapErr(err)
}

func scanEnv(r interface{ Scan(...any) error }) (store.Environment, error) {
	var e store.Environment
	var c int64
	err := r.Scan(&e.ID, &e.ProjectID, &e.Slug, &e.Name, &c)
	e.CreatedAt = fromTS(c)
	return e, err
}

func (s *Store) GetEnvironmentBySlug(ctx context.Context, projectID, slug string) (store.Environment, error) {
	e, err := scanEnv(s.db.QueryRowContext(ctx,
		`SELECT id, project_id, slug, name, created_at FROM environments WHERE project_id = ? AND slug = ?`, projectID, slug))
	return e, mapErr(err)
}

func (s *Store) ListEnvironments(ctx context.Context, projectID string) ([]store.Environment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, slug, name, created_at FROM environments WHERE project_id = ? ORDER BY slug`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Environment
	for rows.Next() {
		e, err := scanEnv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- secrets ----------------------------------------------------------------

func (s *Store) CreateSecret(ctx context.Context, n store.NewSecret) error {
	if n.ID == "" || n.ProjectID == "" || n.EnvironmentID == "" || n.Name == "" ||
		len(n.Ciphertext) == 0 || len(n.Nonce) == 0 || n.DEKVersion < 1 {
		return store.ErrInvalid
	}
	now := s.now().UTC().Unix()
	return mapErr(s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO secrets(id, project_id, environment_id, name, created_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?)`, n.ID, n.ProjectID, n.EnvironmentID, n.Name, now, n.CreatedBy); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO secret_versions(secret_id, version, ciphertext, nonce, dek_version, created_at, created_by, comment)
			VALUES (?, 1, ?, ?, ?, ?, ?, ?)`, n.ID, n.Ciphertext, n.Nonce, n.DEKVersion, now, n.CreatedBy, n.Comment)
		return err
	}))
}

const secretSelect = `SELECT s.id, s.project_id, s.environment_id, s.name, s.created_at, s.created_by,
	COALESCE((SELECT MAX(v.version) FROM secret_versions v WHERE v.secret_id = s.id AND v.disabled_at IS NULL), 0),
	(SELECT COUNT(*) FROM secret_versions v WHERE v.secret_id = s.id)
	FROM secrets s`

func scanSecret(r interface{ Scan(...any) error }) (store.SecretMeta, error) {
	var m store.SecretMeta
	var c int64
	err := r.Scan(&m.ID, &m.ProjectID, &m.EnvironmentID, &m.Name, &c, &m.CreatedBy, &m.LatestVersion, &m.VersionCount)
	m.CreatedAt = fromTS(c)
	return m, err
}

func (s *Store) GetSecret(ctx context.Context, environmentID, name string) (store.SecretMeta, error) {
	m, err := scanSecret(s.db.QueryRowContext(ctx,
		secretSelect+` WHERE s.environment_id = ? AND s.name = ? AND s.deleted_at IS NULL`, environmentID, name))
	return m, mapErr(err)
}

func (s *Store) ListSecrets(ctx context.Context, environmentID string) ([]store.SecretMeta, error) {
	rows, err := s.db.QueryContext(ctx,
		secretSelect+` WHERE s.environment_id = ? AND s.deleted_at IS NULL ORDER BY s.name`, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SecretMeta
	for rows.Next() {
		m, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSecret(ctx context.Context, secretID, actor string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE secrets SET deleted_at = ?, deleted_by = ? WHERE id = ? AND deleted_at IS NULL`,
		s.now().UTC().Unix(), actor, secretID)
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ---- versions -----------------------------------------------------------------

func (s *Store) AddVersion(ctx context.Context, secretID, actor, comment string, seal store.SealFunc) (int, error) {
	var version int
	err := s.tx(ctx, func(tx *sql.Tx) error {
		// The IMMEDIATE transaction holds the write lock from here to commit,
		// so MAX+1 cannot race with another writer in any process. The
		// (secret_id, version) primary key is the backstop.
		var live int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets WHERE id = ? AND deleted_at IS NULL`, secretID).Scan(&live); err != nil {
			return err
		}
		if live == 0 {
			return store.ErrNotFound
		}
		var max sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(version) FROM secret_versions WHERE secret_id = ?`, secretID).Scan(&max); err != nil {
			return err
		}
		version = int(max.Int64) + 1
		ct, nonce, dekVersion, err := seal(version)
		if err != nil {
			return err
		}
		if len(ct) == 0 || len(nonce) == 0 || dekVersion < 1 {
			return store.ErrInvalid
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO secret_versions(secret_id, version, ciphertext, nonce, dek_version, created_at, created_by, comment)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, secretID, version, ct, nonce, dekVersion, s.now().UTC().Unix(), actor, comment)
		return err
	})
	if err != nil {
		return 0, mapErr(err)
	}
	return version, nil
}

const versionCols = `secret_id, version, dek_version, created_at, created_by, comment, disabled_at, disabled_by`

func scanVersionMeta(r interface{ Scan(...any) error }, extra ...any) (store.VersionMeta, error) {
	var v store.VersionMeta
	var created int64
	var dis sql.NullInt64
	var disBy sql.NullString
	dest := append([]any{&v.SecretID, &v.Version, &v.DEKVersion, &created, &v.CreatedBy, &v.Comment, &dis, &disBy}, extra...)
	err := r.Scan(dest...)
	v.CreatedAt = fromTS(created)
	if dis.Valid {
		t := fromTS(dis.Int64)
		v.DisabledAt = &t
		v.DisabledBy = disBy.String
	}
	return v, err
}

func (s *Store) liveSecret(ctx context.Context, secretID string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets WHERE id = ? AND deleted_at IS NULL`, secretID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetVersion(ctx context.Context, secretID string, version int) (store.VersionRecord, error) {
	if err := s.liveSecret(ctx, secretID); err != nil {
		return store.VersionRecord{}, err
	}
	var rec store.VersionRecord
	m, err := scanVersionMeta(s.db.QueryRowContext(ctx,
		`SELECT `+versionCols+`, ciphertext, nonce FROM secret_versions WHERE secret_id = ? AND version = ?`, secretID, version),
		&rec.Ciphertext, &rec.Nonce)
	if err != nil {
		return store.VersionRecord{}, mapErr(err)
	}
	rec.VersionMeta = m
	if m.DisabledAt != nil {
		return store.VersionRecord{VersionMeta: m}, store.ErrVersionDisabled
	}
	return rec, nil
}

func (s *Store) GetLatestVersion(ctx context.Context, secretID string) (store.VersionRecord, error) {
	if err := s.liveSecret(ctx, secretID); err != nil {
		return store.VersionRecord{}, err
	}
	var rec store.VersionRecord
	m, err := scanVersionMeta(s.db.QueryRowContext(ctx,
		`SELECT `+versionCols+`, ciphertext, nonce FROM secret_versions
		 WHERE secret_id = ? AND disabled_at IS NULL ORDER BY version DESC LIMIT 1`, secretID),
		&rec.Ciphertext, &rec.Nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return store.VersionRecord{}, store.ErrNoActiveVersion
	}
	if err != nil {
		return store.VersionRecord{}, mapErr(err)
	}
	rec.VersionMeta = m
	return rec, nil
}

func (s *Store) ListVersions(ctx context.Context, secretID string) ([]store.VersionMeta, error) {
	if err := s.liveSecret(ctx, secretID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+versionCols+` FROM secret_versions WHERE secret_id = ? ORDER BY version`, secretID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.VersionMeta
	for rows.Next() {
		v, err := scanVersionMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) DisableVersion(ctx context.Context, secretID string, version int, actor string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var live int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets WHERE id = ? AND deleted_at IS NULL`, secretID).Scan(&live); err != nil {
			return err
		}
		if live == 0 {
			return store.ErrNotFound
		}
		res, err := tx.ExecContext(ctx, `UPDATE secret_versions SET disabled_at = ?, disabled_by = ?
			WHERE secret_id = ? AND version = ? AND disabled_at IS NULL`, s.now().UTC().Unix(), actor, secretID, version)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM secret_versions WHERE secret_id = ? AND version = ?`, secretID, version).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				return store.ErrNotFound
			}
			// already disabled: idempotent no-op
		}
		return nil
	})
}
