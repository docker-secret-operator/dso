package sqlite

// Characterisation tests for the standalone store operations (M2.1, AC-TX-01a).
//
// They observe the statements each standalone method sends to SQLite through a
// test-only tracing wrapper around the driver (no production code is involved:
// the wrapper is installed by replacing Store.db in this package's tests). They
// were written and run against the M1 baseline (58e711f) BEFORE the WithTx
// refactor and must keep passing, unchanged, after it.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	msqlite "modernc.org/sqlite"

	"github.com/docker-secret-operator/dso/internal/store"
)

// ---- tracing driver ------------------------------------------------------------------------

type traceLog struct {
	mu        sync.Mutex
	stmts     []string
	begins    int
	commits   int
	rollbacks int
	// before, if set, runs at the start of every statement (outside mu).
	before func(query string)
}

func (l *traceLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stmts, l.begins, l.commits, l.rollbacks = nil, 0, 0, 0
}

func (l *traceLog) snapshot() (stmts []string, begins, commits, rollbacks int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.stmts...), l.begins, l.commits, l.rollbacks
}

func (l *traceLog) note(q string) {
	l.mu.Lock()
	l.stmts = append(l.stmts, q)
	hook := l.before
	l.mu.Unlock()
	if hook != nil {
		hook(q)
	}
}

type traceConnector struct {
	dsn string
	log *traceLog
}

func (c *traceConnector) Connect(context.Context) (driver.Conn, error) {
	inner, err := (&msqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &traceConn{inner: inner, log: c.log}, nil
}
func (c *traceConnector) Driver() driver.Driver { return &msqlite.Driver{} }

type traceConn struct {
	inner driver.Conn
	log   *traceLog
}

func (c *traceConn) Prepare(q string) (driver.Stmt, error) { c.log.note(q); return c.inner.Prepare(q) }
func (c *traceConn) Close() error                          { return c.inner.Close() }
func (c *traceConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *traceConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	bt, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return nil, errors.New("inner driver has no BeginTx")
	}
	c.log.mu.Lock()
	c.log.begins++
	c.log.mu.Unlock()
	tx, err := bt.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return &traceTx{inner: tx, log: c.log}, nil
}

func (c *traceConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	e, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.log.note(q)
	return e.ExecContext(ctx, q, a)
}

func (c *traceConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	e, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.log.note(q)
	return e.QueryContext(ctx, q, a)
}

func (c *traceConn) CheckNamedValue(nv *driver.NamedValue) error {
	if ch, ok := c.inner.(driver.NamedValueChecker); ok {
		return ch.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

type traceTx struct {
	inner driver.Tx
	log   *traceLog
}

func (t *traceTx) Commit() error {
	t.log.mu.Lock()
	t.log.commits++
	t.log.mu.Unlock()
	return t.inner.Commit()
}
func (t *traceTx) Rollback() error {
	t.log.mu.Lock()
	t.log.rollbacks++
	t.log.mu.Unlock()
	return t.inner.Rollback()
}

// traced returns a Store over the same database file whose every statement and
// transaction is recorded, plus the plain Store the test opened (used for hooks
// and for inspecting state through an untraced connection).
func traced(t *testing.T) (ts *Store, plain *Store, log *traceLog) {
	t.Helper()
	plain, path := open(t)
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	dsn := (&url.URL{Scheme: "file", Opaque: escapePath(path), RawQuery: q.Encode()}).String()
	log = &traceLog{}
	db := sql.OpenDB(&traceConnector{dsn: dsn, log: log})
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	return &Store{db: db, now: plain.now}, plain, log
}

// ---- helpers --------------------------------------------------------------------------------

func isRead(q string) bool {
	u := strings.ToUpper(strings.TrimSpace(q))
	return strings.HasPrefix(u, "SELECT")
}

// expectShape runs call and asserts the exact number of statements and that no
// transaction was begun (autocommit), returning the error of call.
func expectShape(t *testing.T, log *traceLog, name string, wantStmts int, call func() error) error {
	t.Helper()
	log.reset()
	err := call()
	stmts, begins, commits, rollbacks := log.snapshot()
	if len(stmts) != wantStmts || begins != 0 || commits != 0 || rollbacks != 0 {
		t.Fatalf("%s: %d statements %q, begins=%d commits=%d rollbacks=%d; want %d statements, no transaction",
			name, len(stmts), stmts, begins, commits, rollbacks, wantStmts)
	}
	return err
}

// ---- the 15 autocommit operations --------------------------------------------------------------

func TestStandaloneAutocommitOperationsKeepStatementShape(t *testing.T) {
	ts, plain, log := traced(t)
	seed(t, plain)
	if err := plain.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.AddVersion(ctx, "s1", "t", "c", sealN("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "k.a", Value: []byte("1")}, {Key: "k.b", Value: []byte("2")}}); err != nil {
		t.Fatal(err)
	}

	// success paths: one statement each, no transaction
	if err := expectShape(t, log, "CreateProject", 1, func() error {
		return ts.CreateProject(ctx, store.Project{ID: "p2", Slug: "two", WrappedDEK: []byte("w"), DEKKeyID: "k"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := expectShape(t, log, "CreateEnvironment", 1, func() error {
		return ts.CreateEnvironment(ctx, store.Environment{ID: "e2", ProjectID: "p2", Slug: "dev"})
	}); err != nil {
		t.Fatal(err)
	}
	var perr error
	err := expectShape(t, log, "GetProject", 1, func() error {
		p, e := ts.GetProject(ctx, "p1")
		if e == nil && (p.Slug != "proj" || p.DEKVersion != 1) {
			perr = fmt.Errorf("unexpected project %+v", p)
		}
		return e
	})
	if err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "GetProjectBySlug", 1, func() error {
		p, e := ts.GetProjectBySlug(ctx, "two")
		if e == nil && p.ID != "p2" {
			perr = fmt.Errorf("unexpected project %+v", p)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "ListProjects", 1, func() error {
		ps, e := ts.ListProjects(ctx)
		if e == nil && (len(ps) != 2 || ps[0].Slug != "proj" || ps[1].Slug != "two") {
			perr = fmt.Errorf("unexpected list %+v", ps)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "GetEnvironmentBySlug", 1, func() error {
		e2, e := ts.GetEnvironmentBySlug(ctx, "p1", "prod")
		if e == nil && e2.ID != "e1" {
			perr = fmt.Errorf("unexpected env %+v", e2)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "ListEnvironments", 1, func() error {
		es, e := ts.ListEnvironments(ctx, "p1")
		if e == nil && len(es) != 1 {
			perr = fmt.Errorf("unexpected envs %+v", es)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "GetSecret", 1, func() error {
		m, e := ts.GetSecret(ctx, "e1", "A")
		if e == nil && (m.ID != "s1" || m.LatestVersion != 2 || m.VersionCount != 2) {
			perr = fmt.Errorf("unexpected secret %+v", m)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "ListSecrets", 1, func() error {
		ms, e := ts.ListSecrets(ctx, "e1")
		if e == nil && (len(ms) != 1 || ms[0].Name != "A") {
			perr = fmt.Errorf("unexpected secrets %+v", ms)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "GetMeta", 1, func() error {
		v, e := ts.GetMeta(ctx, "k.a")
		if e == nil && string(v) != "1" {
			perr = fmt.Errorf("unexpected meta %q", v)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "ListMeta", 1, func() error {
		es, e := ts.ListMeta(ctx, "k.")
		if e == nil && (len(es) != 2 || es[0].Key != "k.a") {
			perr = fmt.Errorf("unexpected meta list %+v", es)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	// the three two-statement reads: exactly two separate autocommit statements
	if err := expectShape(t, log, "GetVersion", 2, func() error {
		r, e := ts.GetVersion(ctx, "s1", 1)
		if e == nil && string(r.Ciphertext) != "ct-s1" {
			perr = fmt.Errorf("unexpected record %+v", r)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "GetLatestVersion", 2, func() error {
		r, e := ts.GetLatestVersion(ctx, "s1")
		if e == nil && r.Version != 2 {
			perr = fmt.Errorf("unexpected record %+v", r)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "ListVersions", 2, func() error {
		vs, e := ts.ListVersions(ctx, "s1")
		if e == nil && len(vs) != 2 {
			perr = fmt.Errorf("unexpected versions %+v", vs)
		}
		return e
	}); err != nil || perr != nil {
		t.Fatal(err, perr)
	}
	if err := expectShape(t, log, "DeleteSecret", 1, func() error { return ts.DeleteSecret(ctx, "s1", "t") }); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneAutocommitOperationsKeepErrorSemantics(t *testing.T) {
	ts, plain, log := traced(t)
	seed(t, plain)
	if err := plain.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.AddVersion(ctx, "s1", "t", "", sealN("x")); err != nil {
		t.Fatal(err)
	}
	if err := plain.DisableVersion(ctx, "s1", 2, "t"); err != nil {
		t.Fatal(err)
	}

	type tc struct {
		name  string
		stmts int
		want  error
		call  func() error
	}
	for _, c := range []tc{
		// input validation happens before any statement
		{"CreateProject invalid", 0, store.ErrInvalid, func() error { return ts.CreateProject(ctx, store.Project{ID: "x"}) }},
		{"CreateEnvironment invalid", 0, store.ErrInvalid, func() error { return ts.CreateEnvironment(ctx, store.Environment{ID: "x"}) }},
		// constraint mapping
		{"CreateProject duplicate", 1, store.ErrConflict, func() error {
			return ts.CreateProject(ctx, store.Project{ID: "p1", Slug: "other", WrappedDEK: []byte("w"), DEKKeyID: "k"})
		}},
		{"CreateEnvironment unknown project", 1, store.ErrNotFound, func() error {
			return ts.CreateEnvironment(ctx, store.Environment{ID: "e9", ProjectID: "nope", Slug: "z"})
		}},
		{"GetProject missing", 1, store.ErrNotFound, func() error { _, e := ts.GetProject(ctx, "nope"); return e }},
		{"GetProjectBySlug missing", 1, store.ErrNotFound, func() error { _, e := ts.GetProjectBySlug(ctx, "nope"); return e }},
		{"GetEnvironmentBySlug missing", 1, store.ErrNotFound, func() error { _, e := ts.GetEnvironmentBySlug(ctx, "p1", "nope"); return e }},
		{"GetSecret missing", 1, store.ErrNotFound, func() error { _, e := ts.GetSecret(ctx, "e1", "nope"); return e }},
		{"GetMeta missing", 1, store.ErrNotFound, func() error { _, e := ts.GetMeta(ctx, "nope"); return e }},
		{"DeleteSecret missing", 1, store.ErrNotFound, func() error { return ts.DeleteSecret(ctx, "nope", "t") }},
		// two-statement reads stop after the first statement when the secret is not live
		{"GetVersion missing secret", 1, store.ErrNotFound, func() error { _, e := ts.GetVersion(ctx, "nope", 1); return e }},
		{"GetLatestVersion missing secret", 1, store.ErrNotFound, func() error { _, e := ts.GetLatestVersion(ctx, "nope"); return e }},
		{"ListVersions missing secret", 1, store.ErrNotFound, func() error { _, e := ts.ListVersions(ctx, "nope"); return e }},
		// both statements run; the error comes from the second
		{"GetVersion missing version", 2, store.ErrNotFound, func() error { _, e := ts.GetVersion(ctx, "s1", 9); return e }},
		{"GetVersion disabled", 2, store.ErrVersionDisabled, func() error { _, e := ts.GetVersion(ctx, "s1", 2); return e }},
		{"GetLatestVersion latest disabled (no fallback)", 2, store.ErrLatestVersionDisabled, func() error { _, e := ts.GetLatestVersion(ctx, "s1"); return e }},
	} {
		err := expectShape(t, log, c.name, c.stmts, c.call)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

// The statement-interleaving characterisation. A hook fires between the two
// autocommit statements of the three reads, so the outcome does not depend on
// timing. A single-snapshot implementation fails these.

func hookBeforeSecondStatement(log *traceLog, fire func()) {
	var once sync.Once
	log.mu.Lock()
	log.before = func(q string) {
		if strings.Contains(q, "FROM secret_versions") {
			once.Do(fire)
		}
	}
	log.mu.Unlock()
}

func TestTwoStatementReadsObserveWhatCommitsBetweenTheirStatements(t *testing.T) {
	t.Run("GetLatestVersion returns a version committed in the gap", func(t *testing.T) {
		ts, plain, log := traced(t)
		seed(t, plain)
		if err := plain.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
			t.Fatal(err)
		}
		hookBeforeSecondStatement(log, func() {
			if _, err := plain.AddVersion(ctx, "s1", "t", "", sealN("gap")); err != nil {
				t.Error(err)
			}
		})
		r, err := ts.GetLatestVersion(ctx, "s1")
		if err != nil || r.Version != 2 || string(r.Ciphertext) != "gap-v2" {
			t.Fatalf("got version %d %q, %v; want the version committed between the statements (2)", r.Version, r.Ciphertext, err)
		}
	})
	t.Run("ListVersions includes a version committed in the gap", func(t *testing.T) {
		ts, plain, log := traced(t)
		seed(t, plain)
		if err := plain.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
			t.Fatal(err)
		}
		hookBeforeSecondStatement(log, func() {
			if _, err := plain.AddVersion(ctx, "s1", "t", "", sealN("gap")); err != nil {
				t.Error(err)
			}
		})
		vs, err := ts.ListVersions(ctx, "s1")
		if err != nil || len(vs) != 2 {
			t.Fatalf("got %d versions, %v; want 2", len(vs), err)
		}
	})
	t.Run("GetVersion reports a version disabled in the gap", func(t *testing.T) {
		ts, plain, log := traced(t)
		seed(t, plain)
		if err := plain.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
			t.Fatal(err)
		}
		hookBeforeSecondStatement(log, func() {
			if err := plain.DisableVersion(ctx, "s1", 1, "t"); err != nil {
				t.Error(err)
			}
		})
		if _, err := ts.GetVersion(ctx, "s1", 1); !errors.Is(err, store.ErrVersionDisabled) {
			t.Fatalf("got %v, want ErrVersionDisabled", err)
		}
	})
	t.Run("a secret deleted in the gap does not stop GetVersion (liveness is checked once, first)", func(t *testing.T) {
		ts, plain, log := traced(t)
		seed(t, plain)
		if err := plain.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
			t.Fatal(err)
		}
		hookBeforeSecondStatement(log, func() {
			if err := plain.DeleteSecret(ctx, "s1", "t"); err != nil {
				t.Error(err)
			}
		})
		if r, err := ts.GetVersion(ctx, "s1", 1); err != nil || r.Version != 1 {
			t.Fatalf("got %+v, %v; baseline behaviour is that the second statement does not re-check liveness", r.Version, err)
		}
	})
}

// ---- the 4 transactional operations: exactly one transaction each ---------------------------------

func TestTransactionalOperationsUseExactlyOneTransaction(t *testing.T) {
	ts, plain, log := traced(t)
	seed(t, plain)

	one := func(name string, call func() error) error {
		t.Helper()
		log.reset()
		err := call()
		_, begins, commits, rollbacks := log.snapshot()
		if begins != 1 || commits+rollbacks != 1 {
			t.Fatalf("%s: begins=%d commits=%d rollbacks=%d; want exactly one transaction", name, begins, commits, rollbacks)
		}
		return err
	}
	if err := one("CreateSecret", func() error { return ts.CreateSecret(ctx, newSecret("s1", "A")) }); err != nil {
		t.Fatal(err)
	}
	if err := one("CreateSecret conflict", func() error { return ts.CreateSecret(ctx, newSecret("s2", "A")) }); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if err := one("AddVersion", func() error { _, e := ts.AddVersion(ctx, "s1", "t", "", sealN("x")); return e }); err != nil {
		t.Fatal(err)
	}
	if err := one("AddVersion missing", func() error { _, e := ts.AddVersion(ctx, "nope", "t", "", sealN("x")); return e }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if err := one("DisableVersion", func() error { return ts.DisableVersion(ctx, "s1", 1, "t") }); err != nil {
		t.Fatal(err)
	}
	if err := one("DisableVersion again (no-op)", func() error { return ts.DisableVersion(ctx, "s1", 1, "t") }); err != nil {
		t.Fatal(err)
	}
	if err := one("DisableVersion missing version", func() error { return ts.DisableVersion(ctx, "s1", 9, "t") }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if err := one("PutMetaIfAbsent", func() error {
		ok, e := ts.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "m", Value: []byte("1")}})
		if e == nil && !ok {
			e = errors.New("not inserted")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := one("PutMetaIfAbsent present", func() error {
		ok, e := ts.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "m", Value: []byte("2")}})
		if e == nil && ok {
			e = errors.New("inserted over an existing key")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// invalid input: no transaction at all (validation precedes it)
	log.reset()
	if _, err := ts.PutMetaIfAbsent(ctx, nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	if _, b, _, _ := log.snapshot(); b != 0 {
		t.Fatalf("PutMetaIfAbsent(nil) began %d transactions", b)
	}
	log.reset()
	if err := ts.CreateSecret(ctx, store.NewSecret{ID: "x"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	if _, b, _, _ := log.snapshot(); b != 0 {
		t.Fatalf("CreateSecret(invalid) began %d transactions", b)
	}
	var live int
	if err := plain.db.QueryRow(`SELECT COUNT(*) FROM secret_versions WHERE secret_id='s1'`).Scan(&live); err != nil || live != 2 {
		t.Fatalf("versions of s1: %d, %v", live, err)
	}
}
