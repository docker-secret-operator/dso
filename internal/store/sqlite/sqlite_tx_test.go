package sqlite

// Tests for Store.WithTx / store.Tx (M2.1: AC-TX-02…06 and the OQ-TX-1 option (a)
// contract). Orderings are forced with channels; no test uses a sleep or a
// "who was faster" oracle. Where a bound is asserted (rejection is immediate) the
// bound is far below the 5 s busy timeout, i.e. the failure it distinguishes is
// "waited for the lock" versus "did not touch the database".

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	sqlitelib "modernc.org/sqlite/lib"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
)

func openWith(t *testing.T, o Options) (*Store, string) {
	t.Helper()
	path := filepath.Join(securefstest.TempDir(t), "db", "central.db")
	s, err := Open(ctx, path, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- AC-TX-02 / lifecycle -------------------------------------------------------------------

func TestWithTxCommitsOperationsTogether(t *testing.T) {
	s, _ := open(t)
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateProject(ctx, store.Project{ID: "p1", Slug: "proj", WrappedDEK: []byte("w"), DEKKeyID: "k"}); err != nil {
			return err
		}
		if err := tx.CreateEnvironment(ctx, store.Environment{ID: "e1", ProjectID: "p1", Slug: "prod"}); err != nil {
			return err
		}
		if err := tx.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
			return err
		}
		// read-your-writes inside the transaction
		if _, err := tx.GetProject(ctx, "p1"); err != nil {
			return err
		}
		v, err := tx.AddVersion(ctx, "s1", "t", "", sealN("x"))
		if err != nil || v != 2 {
			return errors.New("AddVersion in tx failed")
		}
		// the transaction is not visible to other connections until it commits
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&n); err != nil || n != 0 {
			return errors.New("uncommitted project visible outside the transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, "projects") != 1 || countRows(t, s, "environments") != 1 || countRows(t, s, "secret_versions") != 2 {
		t.Fatal("committed rows missing")
	}
}

func TestWithTxCallbackErrorRollsBackEarlierOperations(t *testing.T) {
	s, _ := open(t)
	boom := errors.New("callback failed")
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateProject(ctx, store.Project{ID: "p1", Slug: "proj", WrappedDEK: []byte("w"), DEKKeyID: "k"}); err != nil {
			return err
		}
		return boom
	})
	if err != boom {
		t.Fatalf("got %v, want the callback's error unchanged", err)
	}
	if countRows(t, s, "projects") != 0 {
		t.Fatal("first operation survived the failed callback")
	}
	// a failing Tx operation (second one) rolls back the first, and is mapped as in the standalone form
	err = s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateProject(ctx, store.Project{ID: "p1", Slug: "proj", WrappedDEK: []byte("w"), DEKKeyID: "k"}); err != nil {
			return err
		}
		return tx.CreateProject(ctx, store.Project{ID: "p2", Slug: "proj", WrappedDEK: []byte("w"), DEKKeyID: "k"}) // duplicate slug
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if countRows(t, s, "projects") != 0 {
		t.Fatal("first operation survived the failed second operation")
	}
	if err := s.WithTx(ctx, nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("nil callback: %v", err)
	}
}

func TestWithTxPanicRollsBackIsPropagatedAndStoreStaysUsable(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("panic not propagated unchanged: %v", r)
			}
		}()
		s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
			if err := tx.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
				return err
			}
			panic("boom")
		})
		t.Fatal("WithTx returned after a panic")
	}()
	if countRows(t, s, "secrets") != 0 {
		t.Fatal("rows survived a panicking callback")
	}
	// the write lock was released: both forms still work
	if err := s.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatal(err)
	}
	if err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error { _, err := tx.GetProject(ctx, "p1"); return err }); err != nil {
		t.Fatal(err)
	}
}

func TestWithTxContextCancellationRollsBackWithoutLeaks(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	before := runtime.NumGoroutine()
	cctx, cancel := context.WithCancel(ctx)
	err := s.WithTx(cctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
			return err
		}
		cancel()
		_, err := tx.AddVersion(ctx, "s1", "t", "", sealN("x")) // must fail: context cancelled
		return err
	})
	if err == nil {
		t.Fatal("cancelled transaction succeeded")
	}
	cancel()
	if countRows(t, s, "secrets") != 0 {
		t.Fatal("rows survived cancellation")
	}
	if st := s.db.Stats(); st.InUse != 0 {
		t.Fatalf("%d connections still in use after WithTx returned (leaked transaction)", st.InUse)
	}
	// already-cancelled context: nothing is begun
	if err := s.WithTx(cctx, func(context.Context, store.Tx) error { t.Error("callback ran"); return nil }); err == nil {
		t.Fatal("WithTx on a cancelled context succeeded")
	}
	if err := s.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatalf("store unusable after cancellation: %v", err)
	}
	// goroutines started by database/sql for the transaction have exited
	for i := 0; i < 1000 && runtime.NumGoroutine() > before; i++ {
		runtime.Gosched()
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before, %d after", before, after)
	}
}

// ---- AC-TX-04 / 04a: the marked context -----------------------------------------------------

func TestNestedWithTxWithMarkedContextIsRejectedImmediately(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
			return err
		}
		derived, cancel := context.WithCancel(context.WithValue(ctx, struct{}{}, 1)) // derived contexts carry the mark
		defer cancel()
		for _, c := range []context.Context{ctx, derived} {
			start := time.Now()
			got := s.WithTx(c, func(context.Context, store.Tx) error { t.Error("nested callback ran"); return nil })
			if !errors.Is(got, store.ErrTxActive) {
				t.Errorf("nested WithTx: %v, want ErrTxActive", got)
			}
			if d := time.Since(start); d > time.Second {
				t.Errorf("nested WithTx took %v (waited on the lock instead of being rejected)", d)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, "secrets") != 1 {
		t.Fatal("outer transaction did not commit normally after the rejected nested calls")
	}
}

// The 19 operations as standalone Store calls, keyed by method name.
func standaloneCalls() map[string]func(s *Store, c context.Context) error {
	return map[string]func(*Store, context.Context) error{
		"CreateProject": func(s *Store, c context.Context) error {
			return s.CreateProject(c, store.Project{ID: "px", Slug: "px", WrappedDEK: []byte("w"), DEKKeyID: "k"})
		},
		"GetProjectBySlug": func(s *Store, c context.Context) error { _, e := s.GetProjectBySlug(c, "proj"); return e },
		"GetProject":       func(s *Store, c context.Context) error { _, e := s.GetProject(c, "p1"); return e },
		"ListProjects":     func(s *Store, c context.Context) error { _, e := s.ListProjects(c); return e },
		"CreateEnvironment": func(s *Store, c context.Context) error {
			return s.CreateEnvironment(c, store.Environment{ID: "ex", ProjectID: "p1", Slug: "ex"})
		},
		"GetEnvironmentBySlug": func(s *Store, c context.Context) error { _, e := s.GetEnvironmentBySlug(c, "p1", "prod"); return e },
		"ListEnvironments":     func(s *Store, c context.Context) error { _, e := s.ListEnvironments(c, "p1"); return e },
		"CreateSecret":         func(s *Store, c context.Context) error { return s.CreateSecret(c, newSecret("sx", "X")) },
		"GetSecret":            func(s *Store, c context.Context) error { _, e := s.GetSecret(c, "e1", "A"); return e },
		"ListSecrets":          func(s *Store, c context.Context) error { _, e := s.ListSecrets(c, "e1"); return e },
		"DeleteSecret":         func(s *Store, c context.Context) error { return s.DeleteSecret(c, "s1", "t") },
		"AddVersion": func(s *Store, c context.Context) error {
			_, e := s.AddVersion(c, "s1", "t", "", sealN("x"))
			return e
		},
		"GetVersion":       func(s *Store, c context.Context) error { _, e := s.GetVersion(c, "s1", 1); return e },
		"GetLatestVersion": func(s *Store, c context.Context) error { _, e := s.GetLatestVersion(c, "s1"); return e },
		"ListVersions":     func(s *Store, c context.Context) error { _, e := s.ListVersions(c, "s1"); return e },
		"DisableVersion":   func(s *Store, c context.Context) error { return s.DisableVersion(c, "s1", 1, "t") },
		"GetMeta":          func(s *Store, c context.Context) error { _, e := s.GetMeta(c, "m"); return e },
		"ListMeta":         func(s *Store, c context.Context) error { _, e := s.ListMeta(c, "m"); return e },
		"PutMetaIfAbsent": func(s *Store, c context.Context) error {
			_, e := s.PutMetaIfAbsent(c, []store.MetaEntry{{Key: "mx", Value: []byte("1")}})
			return e
		},
	}
}

func TestStandaloneCallTableCoversExactlyTheNineteenOperations(t *testing.T) {
	var want []string
	ot := reflect.TypeOf((*store.Operations)(nil)).Elem()
	for i := 0; i < ot.NumMethod(); i++ {
		want = append(want, ot.Method(i).Name)
	}
	var got []string
	for k := range standaloneCalls() {
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	if len(want) != 19 || !reflect.DeepEqual(want, got) {
		t.Fatalf("operations %v (%d) vs table %v", want, len(want), got)
	}
	// Tx offers the same set, and Store offers it plus WithTx/SchemaVersion/Close only.
	if tt := reflect.TypeOf((*store.Tx)(nil)).Elem(); tt.NumMethod() != 19 {
		t.Fatalf("Tx has %d methods", tt.NumMethod())
	}
	if st := reflect.TypeOf((*store.Store)(nil)).Elem(); st.NumMethod() != 22 {
		t.Fatalf("Store has %d methods, want 19 + WithTx + SchemaVersion + Close", st.NumMethod())
	}
}

func TestStandaloneOperationsRejectTheMarkedContext(t *testing.T) {
	ts, plain, log := traced(t)
	seed(t, plain)
	if err := plain.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "m", Value: []byte("1")}}); err != nil {
		t.Fatal(err)
	}

	for name, call := range standaloneCalls() {
		name, call := name, call
		t.Run(name, func(t *testing.T) {
			var fromCallbackGoroutine, fromSecondGoroutine error
			var stmtsDuringRejections int
			err := ts.WithTx(ctx, func(c context.Context, tx store.Tx) error {
				// an uncommitted write of the outer transaction: rejected calls must neither see nor disturb it
				if err := tx.CreateProject(c, store.Project{ID: "pin-" + name, Slug: "pin-" + name, WrappedDEK: []byte("w"), DEKKeyID: "k"}); err != nil {
					return err
				}
				stmts0, _, _, _ := log.snapshot()
				start := time.Now()
				fromCallbackGoroutine = call(ts, c)
				done := make(chan struct{})
				go func() {
					defer close(done)
					fromSecondGoroutine = call(ts, c)
				}()
				<-done
				if d := time.Since(start); d > time.Second {
					t.Errorf("rejections took %v", d)
				}
				stmts1, _, _, _ := log.snapshot()
				stmtsDuringRejections = len(stmts1) - len(stmts0)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !errors.Is(fromCallbackGoroutine, store.ErrTxActive) || !errors.Is(fromSecondGoroutine, store.ErrTxActive) {
				t.Fatalf("callback goroutine: %v; second goroutine: %v; want ErrTxActive for both", fromCallbackGoroutine, fromSecondGoroutine)
			}
			if stmtsDuringRejections != 0 {
				t.Fatalf("rejected calls sent %d statements to the database", stmtsDuringRejections)
			}
			// the outer transaction committed normally, and the rejected calls changed nothing
			if _, err := plain.GetProject(ctx, "pin-"+name); err != nil {
				t.Fatalf("outer transaction's write missing: %v", err)
			}
			if _, err := plain.GetProject(ctx, "px"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("a rejected CreateProject wrote a row: %v", err)
			}
			if vs, _ := plain.ListVersions(ctx, "s1"); len(vs) != 1 {
				t.Fatalf("a rejected write changed s1: %d versions", len(vs))
			}
			if _, err := plain.GetSecret(ctx, "e1", "A"); err != nil {
				t.Fatalf("a rejected DeleteSecret deleted s1: %v", err)
			}
		})
	}
}

func TestMarkIsPerStoreAndEndsWithTheTransaction(t *testing.T) {
	a, _ := open(t)
	seed(t, a)
	b, _ := openWith(t, Options{})
	var leaked context.Context
	err := a.WithTx(ctx, func(c context.Context, tx store.Tx) error {
		leaked = c
		// another Store instance (here: another database) is an independent caller
		if _, err := b.ListProjects(c); err != nil {
			t.Errorf("another Store instance rejected the context: %v", err)
		}
		if err := b.WithTx(c, func(context.Context, store.Tx) error { return nil }); err != nil {
			t.Errorf("another Store instance rejected WithTx: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// once the transaction has ended its context is no longer "inside a transaction"
	if _, err := a.ListProjects(leaked); err != nil {
		t.Fatalf("stale marked context still rejected: %v", err)
	}
	if err := a.WithTx(leaked, func(context.Context, store.Tx) error { return nil }); err != nil {
		t.Fatalf("WithTx with a stale marked context: %v", err)
	}
}

// ---- OQ-TX-1 option (a): behaviour for an unrelated context ----------------------------------

// AC-TX-04b-1: the outer commit is confirmed (WithTx has returned) before the
// standalone write starts.
func TestAC_TX_04b1_StandaloneWriteAfterConfirmedCommit(t *testing.T) {
	s, _ := openWith(t, Options{BusyTimeout: 30 * time.Second})
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		return tx.CreateProject(ctx, store.Project{ID: "outer", Slug: "outer", WrappedDEK: []byte("w"), DEKKeyID: "k"})
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var gErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-started
		gErr = s.CreateProject(context.Background(), store.Project{ID: "g", Slug: "g", WrappedDEK: []byte("w"), DEKKeyID: "k"})
	}()
	close(started) // only after WithTx returned
	wg.Wait()
	if gErr != nil {
		t.Fatal(gErr)
	}
	if countRows(t, s, "projects") != 2 {
		t.Fatal("expected both rows")
	}
}

// AC-TX-04b-2 (G demonstrably blocked on the held write lock, then the outer
// commit) is NOT tested: it needs an observation of SQLite's internal lock wait.
// modernc.org/sqlite v1.50.0 exposes the wait only through the busy_timeout
// pragma (no busy handler or wait notification), database/sql exposes none, and a
// wrapper around the driver sees that G's statement STARTED, not that it is
// BLOCKED. Observing it would need a production change (for example a store-side
// retry loop with busy_timeout(0)), which M2.1 forbids. Reported as unproven.
func TestAC_TX_04b2_StandaloneWriteDemonstrablyBlockedThenCommit(t *testing.T) {
	t.Skip("UNPROVEN: no deterministic way to observe that the standalone write is blocked on SQLite's write lock without a production seam outside M2.1 (see comment)")
}

// AC-TX-04b-3: whichever order occurs, the write succeeds. This does NOT prove
// that the lock wait was exercised; it shows only that the outcome does not
// depend on the race.
func TestAC_TX_04b3_StandaloneWriteOutcomeIndependentOfOrdering(t *testing.T) {
	s, _ := openWith(t, Options{BusyTimeout: 30 * time.Second})
	gAboutToCall := make(chan struct{})
	gDone := make(chan error, 1)
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateProject(ctx, store.Project{ID: "outer", Slug: "outer", WrappedDEK: []byte("w"), DEKKeyID: "k"}); err != nil {
			return err
		}
		go func() {
			close(gAboutToCall)
			gDone <- s.CreateProject(context.Background(), store.Project{ID: "g", Slug: "g", WrappedDEK: []byte("w"), DEKKeyID: "k"})
		}()
		<-gAboutToCall
		return nil // commit; G may reach the lock before or after this
	})
	if err != nil {
		t.Fatal(err)
	}
	if gErr := <-gDone; gErr != nil {
		t.Fatalf("standalone write: %v", gErr)
	}
	if countRows(t, s, "projects") != 2 {
		t.Fatal("expected both rows")
	}
}

func isBusy(err error) bool {
	return isCode(err, sqlitelib.SQLITE_BUSY, sqlitelib.SQLITE_BUSY_SNAPSHOT, sqlitelib.SQLITE_LOCKED)
}

// AC-TX-04c: timeout first. fn waits for G to return, so the lock cannot be
// released before G's wait ends. AC-TX-04e: the outer outcome is asserted.
func TestAC_TX_04c_StandaloneWriteTimesOutWithRawBusyError(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "outer commits"
		if !commit {
			name = "outer rolls back"
		}
		t.Run(name, func(t *testing.T) {
			s, _ := openWith(t, Options{BusyTimeout: 200 * time.Millisecond})
			boom := errors.New("outer fails")
			var gErr error
			err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
				if err := tx.CreateProject(ctx, store.Project{ID: "outer", Slug: "outer", WrappedDEK: []byte("w"), DEKKeyID: "k"}); err != nil {
					return err
				}
				done := make(chan struct{})
				go func() {
					defer close(done)
					gErr = s.CreateProject(context.Background(), store.Project{ID: "g", Slug: "g", WrappedDEK: []byte("w"), DEKKeyID: "k"})
				}()
				<-done // the transaction (and its lock) outlives G's whole wait
				if commit {
					return nil
				}
				return boom
			})
			if commit && err != nil {
				t.Fatal(err)
			}
			if !commit && err != boom {
				t.Fatalf("outer: %v", err)
			}
			if gErr == nil || errors.Is(gErr, store.ErrTxActive) || !isBusy(gErr) {
				t.Fatalf("standalone write: %v; want the raw driver busy error", gErr)
			}
			if _, err := s.GetProject(ctx, "g"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("timed-out write left a row: %v", err)
			}
			_, err = s.GetProject(ctx, "outer")
			if commit && err != nil {
				t.Fatalf("outer transaction did not commit: %v", err)
			}
			if !commit && !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("outer transaction did not roll back: %v", err)
			}
		})
	}
}

// AC-TX-04d: a standalone read with an unrelated context does not wait and runs
// against committed state, outside the transaction's snapshot.
func TestAC_TX_04d_StandaloneReadRunsOutsideTheTransaction(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "outer commits"
		if !commit {
			name = "outer rolls back"
		}
		t.Run(name, func(t *testing.T) {
			s, _ := open(t)
			seed(t, s)
			if err := s.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
				t.Fatal(err)
			}
			boom := errors.New("outer fails")
			var newRowErr, deletedSecretErr, listLen = error(nil), error(nil), -1
			err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
				if err := tx.CreateProject(ctx, store.Project{ID: "p2", Slug: "two", WrappedDEK: []byte("w"), DEKKeyID: "k"}); err != nil {
					return err
				}
				if err := tx.DeleteSecret(ctx, "s1", "t"); err != nil {
					return err
				}
				// inside the transaction the new state is visible
				if _, err := tx.GetProject(ctx, "p2"); err != nil {
					return err
				}
				if _, err := tx.GetSecret(ctx, "e1", "A"); !errors.Is(err, store.ErrNotFound) {
					return errors.New("deleted secret still visible inside the transaction")
				}
				done := make(chan struct{})
				go func() {
					defer close(done)
					bg := context.Background()
					_, newRowErr = s.GetProject(bg, "p2")            // uncommitted insert
					_, deletedSecretErr = s.GetSecret(bg, "e1", "A") // uncommitted delete: previous committed value
					ps, _ := s.ListProjects(bg)
					listLen = len(ps)
				}()
				<-done
				if commit {
					return nil
				}
				return boom
			})
			if commit && err != nil || !commit && err != boom {
				t.Fatalf("outer: %v", err)
			}
			if !errors.Is(newRowErr, store.ErrNotFound) {
				t.Fatalf("standalone read saw the uncommitted insert: %v", newRowErr)
			}
			if deletedSecretErr != nil {
				t.Fatalf("standalone read did not see the previously committed secret: %v", deletedSecretErr)
			}
			if listLen != 1 {
				t.Fatalf("standalone list saw %d projects, want the 1 committed", listLen)
			}
			// final outcome of the outer transaction, asserted explicitly
			_, perr := s.GetProject(ctx, "p2")
			_, serr := s.GetSecret(ctx, "e1", "A")
			if commit && (perr != nil || !errors.Is(serr, store.ErrNotFound)) {
				t.Fatalf("committed state wrong: project %v, secret %v", perr, serr)
			}
			if !commit && (!errors.Is(perr, store.ErrNotFound) || serr != nil) {
				t.Fatalf("rolled-back state wrong: project %v, secret %v", perr, serr)
			}
		})
	}
}

// Inside Tx the two-statement reads DO share the transaction (one snapshot):
// the shared-transaction behaviour belongs to Tx, not to the standalone methods.
func TestTxTwoStatementReadsShareTheTransaction(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	if err := s.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatal(err)
	}
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.AddVersion(ctx, "s1", "t", "", sealN("in")); err != nil {
			return err
		}
		r, err := tx.GetLatestVersion(ctx, "s1")
		if err != nil || r.Version != 2 {
			return errors.New("latest read inside the transaction missed its own write")
		}
		vs, err := tx.ListVersions(ctx, "s1")
		if err != nil || len(vs) != 2 {
			return errors.New("list inside the transaction missed its own write")
		}
		if err := tx.DisableVersion(ctx, "s1", 2, "t"); err != nil {
			return err
		}
		if _, err := tx.GetLatestVersion(ctx, "s1"); !errors.Is(err, store.ErrLatestVersionDisabled) {
			return errors.New("latest disabled not reported inside the transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
