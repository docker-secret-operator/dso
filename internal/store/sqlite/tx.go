package sqlite

// Unit of work (M2.1): Store.WithTx and the transaction-scoped store.Tx.
//
// The 19 data/metadata operations are written once, as methods of ops, against
// the method set that *sql.DB and *sql.Tx share (queryer):
//
//   - store.Tx is ops over the open *sql.Tx, so every operation runs inside the
//     caller's single IMMEDIATE transaction;
//   - the 15 operations that were plain autocommit calls in M1 are exposed by
//     Store as ops over *sql.DB, i.e. exactly the statements M1 sent, with no
//     added BEGIN (and the three two-statement reads still send two separate
//     statements);
//   - the 4 operations that used the private s.tx helper in M1 (CreateSecret,
//     AddVersion, DisableVersion, PutMetaIfAbsent) now delegate to WithTx.
//
// ops methods are only ever used with a *sql.DB for the 15 autocommit
// operations; the four transactional ones must only run on a transaction.

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"

	"github.com/docker-secret-operator/dso/internal/store"
)

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type ops struct {
	q   queryer
	now func() time.Time
}

var _ store.Operations = ops{}

type txImpl struct{ ops }

var _ store.Tx = txImpl{}

// txMarkKey/txMark carry "this context belongs to an open transaction of this
// Store". It recognises only contexts derived from the one WithTx handed to its
// callback; it cannot (and does not claim to) recognise an unrelated context, a
// discarded context, another goroutine that was given a different context,
// another Store instance or another process. Those remain independent callers.
type txMarkKey struct{}

type txMark struct {
	s    *Store
	done atomic.Bool // set when the transaction has ended (commit or rollback)
}

// guard returns ErrTxActive, without touching the database, if ctx is (derived
// from) the context of a transaction of this Store that is still open.
func (s *Store) guard(ctx context.Context) error {
	if m, _ := ctx.Value(txMarkKey{}).(*txMark); m != nil && m.s == s && !m.done.Load() {
		return store.ErrTxActive
	}
	return nil
}

func (s *Store) ops() ops { return ops{q: s.db, now: s.now} }

// WithTx implements store.Store. Lifecycle (shared with the M1 helper s.tx):
// commit when fn returns nil; roll back on an error from fn, on a cancelled
// context (database/sql rolls the transaction back) and on a panic, which is
// propagated, not swallowed; the store stays usable afterwards.
func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context, tx store.Tx) error) error {
	if fn == nil {
		return store.ErrInvalid
	}
	if err := s.guard(ctx); err != nil {
		return err
	}
	mark := &txMark{s: s}
	defer mark.done.Store(true) // after commit/rollback, also on panic
	return s.tx(ctx, func(t *sql.Tx) error {
		return fn(context.WithValue(ctx, txMarkKey{}, mark), txImpl{ops{q: t, now: s.now}})
	})
}

// ---- the 15 autocommit operations: unchanged statements, guarded entry ----------------------

func (s *Store) CreateProject(ctx context.Context, p store.Project) error {
	if err := s.guard(ctx); err != nil {
		return err
	}
	return s.ops().CreateProject(ctx, p)
}

func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (store.Project, error) {
	if err := s.guard(ctx); err != nil {
		return store.Project{}, err
	}
	return s.ops().GetProjectBySlug(ctx, slug)
}

func (s *Store) GetProject(ctx context.Context, id string) (store.Project, error) {
	if err := s.guard(ctx); err != nil {
		return store.Project{}, err
	}
	return s.ops().GetProject(ctx, id)
}

func (s *Store) ListProjects(ctx context.Context) ([]store.Project, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s.ops().ListProjects(ctx)
}

func (s *Store) CreateEnvironment(ctx context.Context, e store.Environment) error {
	if err := s.guard(ctx); err != nil {
		return err
	}
	return s.ops().CreateEnvironment(ctx, e)
}

func (s *Store) GetEnvironmentBySlug(ctx context.Context, projectID, slug string) (store.Environment, error) {
	if err := s.guard(ctx); err != nil {
		return store.Environment{}, err
	}
	return s.ops().GetEnvironmentBySlug(ctx, projectID, slug)
}

func (s *Store) ListEnvironments(ctx context.Context, projectID string) ([]store.Environment, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s.ops().ListEnvironments(ctx, projectID)
}

func (s *Store) GetSecret(ctx context.Context, environmentID, name string) (store.SecretMeta, error) {
	if err := s.guard(ctx); err != nil {
		return store.SecretMeta{}, err
	}
	return s.ops().GetSecret(ctx, environmentID, name)
}

func (s *Store) ListSecrets(ctx context.Context, environmentID string) ([]store.SecretMeta, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s.ops().ListSecrets(ctx, environmentID)
}

func (s *Store) DeleteSecret(ctx context.Context, secretID, actor string) error {
	if err := s.guard(ctx); err != nil {
		return err
	}
	return s.ops().DeleteSecret(ctx, secretID, actor)
}

func (s *Store) GetVersion(ctx context.Context, secretID string, version int) (store.VersionRecord, error) {
	if err := s.guard(ctx); err != nil {
		return store.VersionRecord{}, err
	}
	return s.ops().GetVersion(ctx, secretID, version)
}

func (s *Store) GetLatestVersion(ctx context.Context, secretID string) (store.VersionRecord, error) {
	if err := s.guard(ctx); err != nil {
		return store.VersionRecord{}, err
	}
	return s.ops().GetLatestVersion(ctx, secretID)
}

func (s *Store) ListVersions(ctx context.Context, secretID string) ([]store.VersionMeta, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s.ops().ListVersions(ctx, secretID)
}

// AppendAuditEvent, called standalone, wraps its own read-latest-hash-then-
// insert sequence in one IMMEDIATE transaction (unlike the 15 genuinely
// single-statement autocommit operations above). Without this, two
// concurrent standalone appends (e.g. two best-effort failure events racing
// each other) could both read the same "latest" event_hash before either
// commits its INSERT, forking the chain rather than linking it. A Tx-scoped
// call (inside Store.WithTx, used by the Phase 2 Step 5.3 atomic write paths)
// is unaffected: it already runs on the surrounding transaction and this
// wrapper is never on that path.
func (s *Store) AppendAuditEvent(ctx context.Context, ev store.AuditEvent) (store.AuditEvent, error) {
	if err := s.guard(ctx); err != nil {
		return store.AuditEvent{}, err
	}
	var result store.AuditEvent
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		r, err := tx.AppendAuditEvent(ctx, ev)
		result = r
		return err
	})
	return result, err
}

func (s *Store) ListAuditEvents(ctx context.Context) ([]store.AuditEvent, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s.ops().ListAuditEvents(ctx)
}

func (s *Store) GetMeta(ctx context.Context, key string) ([]byte, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s.ops().GetMeta(ctx, key)
}

func (s *Store) ListMeta(ctx context.Context, prefix string) ([]store.MetaEntry, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s.ops().ListMeta(ctx, prefix)
}

// ---- the 4 operations that were transactional in M1: now delegate to WithTx -----------------
//
// Input validation stays BEFORE the transaction, as in M1, so invalid input
// never begins one. Errors are mapped exactly as in M1 (DisableVersion returns
// its error unmapped).

func (s *Store) CreateSecret(ctx context.Context, n store.NewSecret) error {
	if err := s.guard(ctx); err != nil {
		return err
	}
	if !validNewSecret(n) {
		return store.ErrInvalid
	}
	return mapErr(s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		return tx.CreateSecret(ctx, n)
	}))
}

func (s *Store) AddVersion(ctx context.Context, secretID, actor, comment string, seal store.SealFunc) (int, error) {
	if err := s.guard(ctx); err != nil {
		return 0, err
	}
	var version int
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		v, err := tx.AddVersion(ctx, secretID, actor, comment, seal)
		version = v
		return err
	})
	if err != nil {
		return 0, mapErr(err)
	}
	return version, nil
}

func (s *Store) DisableVersion(ctx context.Context, secretID string, version int, actor string) error {
	if err := s.guard(ctx); err != nil {
		return err
	}
	return s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		return tx.DisableVersion(ctx, secretID, version, actor)
	})
}

func (s *Store) PutMetaIfAbsent(ctx context.Context, entries []store.MetaEntry) (bool, error) {
	if err := s.guard(ctx); err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, store.ErrInvalid
	}
	inserted := false
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		ok, err := tx.PutMetaIfAbsent(ctx, entries)
		inserted = ok
		return err
	})
	return inserted, mapErr(err)
}
