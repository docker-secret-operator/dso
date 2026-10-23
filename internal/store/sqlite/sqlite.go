// Package sqlite implements store.Store on SQLite in WAL mode using the
// pure-Go modernc.org/sqlite driver (no cgo). All SQL is parameterised.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
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

	"github.com/docker-secret-operator/dso/internal/securefs"
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
	db         *sql.DB
	now        func() time.Time
	lastBackup string
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
		_ = db.Close()
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	var mode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || !strings.EqualFold(mode, "wal") {
		_ = db.Close()
		return nil, fmt.Errorf("database %s: WAL mode could not be enabled (journal_mode=%q, err=%v)", path, mode, err)
	}
	if err := s.migrate(ctx, path, migs); err != nil {
		_ = db.Close()
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
	// The directory chain must not let another account replace the database or
	// pre-create its -wal/-shm companions (see package securefs). Missing
	// directories are created 0700; existing ones are checked, never re-moded.
	if err := securefs.EnsureDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("database directory is not safe: %w", err)
	}
	// Create exclusively (never follow a planted link); if it already exists it
	// must pass the same private-file rules as a key file.
	f, err := securefs.CreateExclusive(path, 0o600)
	if err == nil {
		return f.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("creating database file: %w", err)
	}
	existing, err := securefs.OpenPrivateFile(path)
	if err != nil {
		return fmt.Errorf("database file is not safe to use: %w", err)
	}
	return existing.Close()
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
		bak, err := s.backupBeforeMigration(ctx, path, cur)
		if err != nil {
			return fmt.Errorf("pre-migration backup failed, migration not attempted: %w", err)
		}
		s.lastBackup = bak
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

// backupBeforeMigration writes a complete, verified snapshot of the database to
// a NEW file before any schema change and returns its path. Every migration
// attempt gets its own backup (name: <db>.pre-migration-v<from>-<UTC time>-<id>.bak,
// created exclusively with mode 0600): reusing an older file would present a
// stale snapshot as "the state before this migration" after intervening writes.
// If the snapshot cannot be created and verified the migration is not attempted.
// Backups are never deleted automatically.
func (s *Store) backupBeforeMigration(ctx context.Context, path string, from int) (string, error) {
	var f *os.File
	var dst string
	for attempt := 0; ; attempt++ {
		var rnd [4]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return "", err
		}
		dst = fmt.Sprintf("%s.pre-migration-v%d-%s-%s.bak", path, from,
			s.now().UTC().Format("20060102T150405Z"), hex.EncodeToString(rnd[:]))
		var err error
		f, err = securefs.CreateExclusive(dst, 0o600) // VACUUM INTO accepts an empty existing file
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) || attempt >= 5 {
			return "", err
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(dst) // our own just-created, empty file
		return "", err
	}
	fail := func(err error) (string, error) {
		// Best effort: removing our own just-created, incomplete file. The
		// original error is the one worth reporting.
		_ = os.Remove(dst)
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return fail(err)
	}
	if err := verifyBackup(ctx, dst, from); err != nil {
		return fail(fmt.Errorf("backup failed verification: %w", err))
	}
	if bf, err := os.OpenFile(dst, os.O_RDWR, 0); err == nil { // #nosec G304 -- dst is the backup file this function just created from the configured database path
		err = bf.Sync()
		if cerr := bf.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fail(err)
		}
	}
	if d, err := os.Open(filepath.Dir(dst)); err == nil { // #nosec G304 -- directory of the backup file created above
		_ = d.Sync()
		_ = d.Close()
	}
	return dst, os.Chmod(dst, 0o600)
}

// verifyBackup opens the snapshot read-only and checks SQLite's integrity and
// that it carries the schema version it is supposed to preserve.
func verifyBackup(ctx context.Context, path string, wantVersion int) error {
	q := url.Values{}
	q.Add("_pragma", "query_only(1)")
	dsn := (&url.URL{Scheme: "file", Opaque: escapePath(path), RawQuery: q.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return fmt.Errorf("integrity_check = %q (%v)", res, err)
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return err
	}
	if int(v.Int64) != wantVersion {
		return fmt.Errorf("snapshot is at schema %d, expected %d", v.Int64, wantVersion)
	}
	return nil
}

// LastMigrationBackup returns the path of the snapshot taken by the most
// recent Open that migrated an existing database, or "" if none was needed.
func (s *Store) LastMigrationBackup() string { return s.lastBackup }

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

func (o ops) CreateProject(ctx context.Context, p store.Project) error {
	if p.ID == "" || p.Slug == "" || len(p.WrappedDEK) == 0 || p.DEKKeyID == "" {
		return store.ErrInvalid
	}
	if p.DEKVersion == 0 {
		p.DEKVersion = 1
	}
	_, err := o.q.ExecContext(ctx, `INSERT INTO projects(id, slug, name, wrapped_dek, dek_key_id, dek_version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, p.ID, p.Slug, p.Name, p.WrappedDEK, p.DEKKeyID, p.DEKVersion, o.now().UTC().Unix())
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

func (o ops) GetProjectBySlug(ctx context.Context, slug string) (store.Project, error) {
	p, err := scanProject(o.q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE slug = ?`, slug))
	return p, mapErr(err)
}

func (o ops) GetProject(ctx context.Context, id string) (store.Project, error) {
	p, err := scanProject(o.q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
	return p, mapErr(err)
}

func (o ops) ListProjects(ctx context.Context) ([]store.Project, error) {
	rows, err := o.q.QueryContext(ctx, `SELECT `+projectCols+` FROM projects ORDER BY slug`)
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

func (o ops) CreateEnvironment(ctx context.Context, e store.Environment) error {
	if e.ID == "" || e.ProjectID == "" || e.Slug == "" {
		return store.ErrInvalid
	}
	_, err := o.q.ExecContext(ctx, `INSERT INTO environments(id, project_id, slug, name, created_at) VALUES (?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.Slug, e.Name, o.now().UTC().Unix())
	return mapErr(err)
}

func scanEnv(r interface{ Scan(...any) error }) (store.Environment, error) {
	var e store.Environment
	var c int64
	err := r.Scan(&e.ID, &e.ProjectID, &e.Slug, &e.Name, &c)
	e.CreatedAt = fromTS(c)
	return e, err
}

func (o ops) GetEnvironmentBySlug(ctx context.Context, projectID, slug string) (store.Environment, error) {
	e, err := scanEnv(o.q.QueryRowContext(ctx,
		`SELECT id, project_id, slug, name, created_at FROM environments WHERE project_id = ? AND slug = ?`, projectID, slug))
	return e, mapErr(err)
}

func (o ops) ListEnvironments(ctx context.Context, projectID string) ([]store.Environment, error) {
	rows, err := o.q.QueryContext(ctx,
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

// validNewSecret is checked before any transaction is begun (standalone) and
// again by the Tx form.
func validNewSecret(n store.NewSecret) bool {
	return !(n.ID == "" || n.ProjectID == "" || n.EnvironmentID == "" || n.Name == "" ||
		len(n.Ciphertext) == 0 || len(n.Nonce) == 0 || n.DEKVersion < 1)
}

func (o ops) CreateSecret(ctx context.Context, n store.NewSecret) error {
	if !validNewSecret(n) {
		return store.ErrInvalid
	}
	now := o.now().UTC().Unix()
	// Runs on o.q, which is the open transaction (see Store.CreateSecret): both
	// inserts commit or roll back together.
	if _, err := o.q.ExecContext(ctx, `INSERT INTO secrets(id, project_id, environment_id, name, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?)`, n.ID, n.ProjectID, n.EnvironmentID, n.Name, now, n.CreatedBy); err != nil {
		return mapErr(err)
	}
	_, err := o.q.ExecContext(ctx, `INSERT INTO secret_versions(secret_id, version, ciphertext, nonce, dek_version, created_at, created_by, comment)
		VALUES (?, 1, ?, ?, ?, ?, ?, ?)`, n.ID, n.Ciphertext, n.Nonce, n.DEKVersion, now, n.CreatedBy, n.Comment)
	return mapErr(err)
}

// selectNamedMeta selects named-entry metadata only (never ciphertext). The
// identifier deliberately avoids the word "secret": gosec G101 treats such
// names as hardcoded credentials, but this constant is plain SQL text.
const selectNamedMeta = `SELECT s.id, s.project_id, s.environment_id, s.name, s.created_at, s.created_by,
	COALESCE((SELECT MAX(v.version) FROM secret_versions v WHERE v.secret_id = s.id), 0),
	COALESCE((SELECT v.disabled_at IS NOT NULL FROM secret_versions v WHERE v.secret_id = s.id ORDER BY v.version DESC LIMIT 1), 0),
	(SELECT COUNT(*) FROM secret_versions v WHERE v.secret_id = s.id)
	FROM secrets s`

func scanSecret(r interface{ Scan(...any) error }) (store.SecretMeta, error) {
	var m store.SecretMeta
	var c int64
	err := r.Scan(&m.ID, &m.ProjectID, &m.EnvironmentID, &m.Name, &c, &m.CreatedBy, &m.LatestVersion, &m.LatestVersionDisabled, &m.VersionCount)
	m.CreatedAt = fromTS(c)
	return m, err
}

func (o ops) GetSecret(ctx context.Context, environmentID, name string) (store.SecretMeta, error) {
	m, err := scanSecret(o.q.QueryRowContext(ctx,
		selectNamedMeta+` WHERE s.environment_id = ? AND s.name = ? AND s.deleted_at IS NULL`, environmentID, name))
	return m, mapErr(err)
}

func (o ops) ListSecrets(ctx context.Context, environmentID string) ([]store.SecretMeta, error) {
	rows, err := o.q.QueryContext(ctx,
		selectNamedMeta+` WHERE s.environment_id = ? AND s.deleted_at IS NULL ORDER BY s.name`, environmentID)
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

func (o ops) DeleteSecret(ctx context.Context, secretID, actor string) error {
	res, err := o.q.ExecContext(ctx, `UPDATE secrets SET deleted_at = ?, deleted_by = ? WHERE id = ? AND deleted_at IS NULL`,
		o.now().UTC().Unix(), actor, secretID)
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ---- versions -----------------------------------------------------------------

func (o ops) AddVersion(ctx context.Context, secretID, actor, comment string, seal store.SealFunc) (int, error) {
	// Runs on o.q, which is the open transaction (see Store.AddVersion).
	//
	// The IMMEDIATE transaction holds the write lock from here to commit,
	// so MAX+1 cannot race with another writer in any process. The
	// (secret_id, version) primary key is the backstop.
	var live int
	if err := o.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets WHERE id = ? AND deleted_at IS NULL`, secretID).Scan(&live); err != nil {
		return 0, mapErr(err)
	}
	if live == 0 {
		return 0, store.ErrNotFound
	}
	var max sql.NullInt64
	if err := o.q.QueryRowContext(ctx, `SELECT MAX(version) FROM secret_versions WHERE secret_id = ?`, secretID).Scan(&max); err != nil {
		return 0, mapErr(err)
	}
	version := int(max.Int64) + 1
	ct, nonce, dekVersion, err := seal(version)
	if err != nil {
		return 0, mapErr(err)
	}
	if len(ct) == 0 || len(nonce) == 0 || dekVersion < 1 {
		return 0, store.ErrInvalid
	}
	if _, err = o.q.ExecContext(ctx, `INSERT INTO secret_versions(secret_id, version, ciphertext, nonce, dek_version, created_at, created_by, comment)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, secretID, version, ct, nonce, dekVersion, o.now().UTC().Unix(), actor, comment); err != nil {
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

func (o ops) liveSecret(ctx context.Context, secretID string) error {
	var n int
	if err := o.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets WHERE id = ? AND deleted_at IS NULL`, secretID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (o ops) GetVersion(ctx context.Context, secretID string, version int) (store.VersionRecord, error) {
	if err := o.liveSecret(ctx, secretID); err != nil {
		return store.VersionRecord{}, err
	}
	var rec store.VersionRecord
	m, err := scanVersionMeta(o.q.QueryRowContext(ctx,
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

func (o ops) GetLatestVersion(ctx context.Context, secretID string) (store.VersionRecord, error) {
	if err := o.liveSecret(ctx, secretID); err != nil {
		return store.VersionRecord{}, err
	}
	var rec store.VersionRecord
	m, err := scanVersionMeta(o.q.QueryRowContext(ctx,
		`SELECT `+versionCols+`, ciphertext, nonce FROM secret_versions
		 WHERE secret_id = ? ORDER BY version DESC LIMIT 1`, secretID),
		&rec.Ciphertext, &rec.Nonce)
	if err != nil {
		return store.VersionRecord{}, mapErr(err)
	}
	if m.DisabledAt != nil {
		return store.VersionRecord{VersionMeta: m}, store.ErrLatestVersionDisabled
	}
	rec.VersionMeta = m
	return rec, nil
}

func (o ops) ListVersions(ctx context.Context, secretID string) ([]store.VersionMeta, error) {
	if err := o.liveSecret(ctx, secretID); err != nil {
		return nil, err
	}
	rows, err := o.q.QueryContext(ctx, `SELECT `+versionCols+` FROM secret_versions WHERE secret_id = ? ORDER BY version`, secretID)
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

func (o ops) DisableVersion(ctx context.Context, secretID string, version int, actor string) error {
	// Runs on o.q, which is the open transaction (see Store.DisableVersion).
	// Errors are returned unmapped, exactly as in M1.
	var live int
	if err := o.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets WHERE id = ? AND deleted_at IS NULL`, secretID).Scan(&live); err != nil {
		return err
	}
	if live == 0 {
		return store.ErrNotFound
	}
	res, err := o.q.ExecContext(ctx, `UPDATE secret_versions SET disabled_at = ?, disabled_by = ?
		WHERE secret_id = ? AND version = ? AND disabled_at IS NULL`, o.now().UTC().Unix(), actor, secretID, version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := o.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM secret_versions WHERE secret_id = ? AND version = ?`, secretID, version).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return store.ErrNotFound
		}
		// already disabled: idempotent no-op
	}
	return nil
}

// ---- metadata ----------------------------------------------------------------

func (o ops) GetMeta(ctx context.Context, key string) ([]byte, error) {
	var v []byte
	err := o.q.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key = ?`, key).Scan(&v)
	return v, mapErr(err)
}

func (o ops) PutMetaIfAbsent(ctx context.Context, entries []store.MetaEntry) (bool, error) {
	if len(entries) == 0 {
		return false, store.ErrInvalid
	}
	// Runs on o.q, which is the open IMMEDIATE transaction (see
	// Store.PutMetaIfAbsent): the existence check and the inserts cannot
	// interleave with another initialiser, in this or another process.
	for _, e := range entries {
		var n int
		if err := o.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_meta WHERE key = ?`, e.Key).Scan(&n); err != nil {
			return false, mapErr(err)
		}
		if n > 0 {
			return false, nil // someone already initialised; change nothing
		}
	}
	for _, e := range entries {
		if _, err := o.q.ExecContext(ctx, `INSERT INTO store_meta(key, value) VALUES (?, ?)`, e.Key, e.Value); err != nil {
			return false, mapErr(err)
		}
	}
	return true, nil
}

func (o ops) ListMeta(ctx context.Context, prefix string) ([]store.MetaEntry, error) {
	rows, err := o.q.QueryContext(ctx,
		`SELECT key, value FROM store_meta WHERE substr(key, 1, length(?)) = ? ORDER BY key`, prefix, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.MetaEntry
	for rows.Next() {
		var e store.MetaEntry
		if err := rows.Scan(&e.Key, &e.Value); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
