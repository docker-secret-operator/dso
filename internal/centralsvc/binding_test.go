package centralsvc

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

func otherKey(t *testing.T) crypto.KeyProvider {
	t.Helper()
	p := filepath.Join(securefstest.TempDir(t), "other.key")
	if _, err := crypto.InstallKeyFile(p); err != nil {
		t.Fatal(err)
	}
	kp, err := crypto.NewFileKeyProvider(p)
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

// snapshot counts the rows of every table: a rejected operation must change nothing.
func snapshot(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tbl := range []string{"projects", "environments", "secrets", "secret_versions", "store_meta"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[tbl] = n
	}
	return out
}

func sameSnapshot(t *testing.T, a, b map[string]int) {
	t.Helper()
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("table %s changed: %d -> %d (a rejected key must not mutate anything)", k, v, b[k])
		}
	}
}

func TestBinding_FirstUseBindsAndSurvivesRestart(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	for _, k := range []string{metaBindingVersion, metaBindingKeyID, metaBindingCanary} {
		if v, err := e.st.GetMeta(ctx, k); err != nil || len(v) == 0 {
			t.Fatalf("binding row %s missing: %v", k, err)
		}
	}
	id, _ := e.st.GetMeta(ctx, metaBindingKeyID)
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	if string(id) != kp.ID() {
		t.Fatal("binding names the wrong key id")
	}
	canary, _ := e.st.GetMeta(ctx, metaBindingCanary)
	if len(canary) == 0 || string(canary) == string(id) {
		t.Fatal("canary looks wrong")
	}
	e.reopen(t) // restart with the right key still works
	if _, err := e.svc.CreateProject(ctx, "second", ""); err != nil {
		t.Fatalf("right key rejected after restart: %v", err)
	}
}

// Review finding M1-2: a different key used to create projects in an
// initialised database (a mixed-key database).
func TestBinding_SecondKeyCannotCreateProjectsOrAnythingElse(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("a"), "x", "")
	db := e.rawDB(t)
	before := snapshot(t, db)

	bad := New(e.st, otherKey(t), nil)
	checks := map[string]error{
		"CreateProject":     func() error { _, err := bad.CreateProject(ctx, "intruder", ""); return err }(),
		"CreateEnvironment": func() error { _, err := bad.CreateEnvironment(ctx, "app", "staging", ""); return err }(),
		"CreateSecret":      bad.CreateSecret(ctx, "app", "prod", "N", []byte("x"), "x", ""),
		"AddVersion":        func() error { _, err := bad.AddVersion(ctx, "app", "prod", "A", []byte("x"), "x", ""); return err }(),
		"GetValue":          func() error { _, err := bad.GetValue(ctx, "app", "prod", "A", 0); return err }(),
		"ListProjects":      func() error { _, err := bad.ListProjects(ctx); return err }(),
		"ListSecrets":       func() error { _, err := bad.ListSecrets(ctx, "app", "prod"); return err }(),
		"ListVersions":      func() error { _, err := bad.ListVersions(ctx, "app", "prod", "A"); return err }(),
		"DisableVersion":    bad.DisableVersion(ctx, "app", "prod", "A", 1, "x"),
		"DeleteSecret":      bad.DeleteSecret(ctx, "app", "prod", "A", "x"),
	}
	for op, err := range checks {
		if !errors.Is(err, ErrWrongRootKey) {
			t.Errorf("%s with a different root key = %v, want ErrWrongRootKey", op, err)
		}
	}
	sameSnapshot(t, before, snapshot(t, db))
	if got := mustGet(t, e, "app", "prod", "A", 0); got != "a" { // the real key is unaffected
		t.Fatalf("data damaged: %q", got)
	}
}

func TestBinding_EmptyBoundDatabaseCannotBeTakenOverByAnotherKey(t *testing.T) {
	e := setup(t)
	if err := e.svc.Bind(ctx); err != nil { // what `init` does: bind with zero projects
		t.Fatal(err)
	}
	bad := New(e.st, otherKey(t), nil)
	if _, err := bad.CreateProject(ctx, "intruder", ""); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("second key created a project in an initialised-but-empty database: %v", err)
	}
	if ps, _ := e.svc.ListProjects(ctx); len(ps) != 0 {
		t.Fatal("a project exists")
	}
}

// The stored key id is NOT authenticated; the check must not depend on it.
func TestBinding_TamperingWithTheKeyIdFieldCannotBypassTheCheck(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	db := e.rawDB(t)
	dropMetaTriggers(t, db)

	attacker := otherKey(t)
	// Attacker rewrites the visible key id to THEIR key's id.
	if _, err := db.Exec(`UPDATE store_meta SET value = ? WHERE key = ?`, []byte(attacker.ID()), metaBindingKeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := New(e.st, attacker, nil).CreateProject(ctx, "intruder", ""); err == nil {
		t.Fatal("attacker key accepted after rewriting the key-id field")
	}
	// ...and the legitimate key (id field now wrong) is judged on the canary, not the label,
	// so it still works: the label cannot be used to lock it out either.
	if _, err := e.svc.CreateProject(ctx, "second", ""); err != nil {
		t.Fatalf("legitimate key rejected because only the label changed: %v", err)
	}
	// Garbage label, right key: still fine.
	db.Exec(`UPDATE store_meta SET value = ? WHERE key = ?`, []byte("garbage"), metaBindingKeyID)
	e.reopen(t)
	if _, err := e.svc.CreateProject(ctx, "third", ""); err != nil {
		t.Fatalf("label garbage broke the legitimate key: %v", err)
	}
}

func dropMetaTriggers(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, tr := range []string{"store_meta_no_update", "store_meta_no_delete"} {
		if _, err := db.Exec(`DROP TRIGGER ` + tr); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBinding_CorruptedMetadataFailsClosed(t *testing.T) {
	setupBound := func(t *testing.T) (*env, *sql.DB) {
		e := setup(t)
		e.project(t, "app", "prod")
		db := e.rawDB(t)
		dropMetaTriggers(t, db)
		return e, db
	}
	for name, tamper := range map[string]func(*sql.DB){
		"canary row deleted":   func(db *sql.DB) { db.Exec(`DELETE FROM store_meta WHERE key = ?`, metaBindingCanary) },
		"key-id row deleted":   func(db *sql.DB) { db.Exec(`DELETE FROM store_meta WHERE key = ?`, metaBindingKeyID) },
		"version row deleted":  func(db *sql.DB) { db.Exec(`DELETE FROM store_meta WHERE key = ?`, metaBindingVersion) },
		"canary bit flipped":   func(db *sql.DB) { flipLastByte(t, db, metaBindingCanary) },
		"canary truncated":     func(db *sql.DB) { db.Exec(`UPDATE store_meta SET value = x'0102' WHERE key = ?`, metaBindingCanary) },
		"unknown format":       func(db *sql.DB) { db.Exec(`UPDATE store_meta SET value = x'39' WHERE key = ?`, metaBindingVersion) },
		"canary from other db": nil, // filled below
	} {
		if tamper == nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			e, db := setupBound(t)
			tamper(db)
			e.reopen(t) // fresh service: no cached verification
			before := snapshot(t, db)
			_, err := e.svc.CreateProject(ctx, "x", "")
			if !errors.Is(err, ErrBindingCorrupt) {
				t.Fatalf("got %v, want ErrBindingCorrupt (it must NOT silently re-bind)", err)
			}
			if _, err := e.svc.GetValue(ctx, "app", "prod", "none", 0); !errors.Is(err, ErrBindingCorrupt) {
				t.Fatalf("read path: %v", err)
			}
			sameSnapshot(t, before, snapshot(t, db))
		})
	}
}

func flipLastByte(t *testing.T, db *sql.DB, key string) {
	t.Helper()
	var b []byte
	if err := db.QueryRow(`SELECT value FROM store_meta WHERE key = ?`, key).Scan(&b); err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 1
	if _, err := db.Exec(`UPDATE store_meta SET value = ? WHERE key = ?`, b, key); err != nil {
		t.Fatal(err)
	}
}

func TestBinding_CanaryFromAnotherDatabaseUnderTheSameKeyIdIsStillAcceptedOnlyIfItOpens(t *testing.T) {
	// A canary wrapped under a DIFFERENT key but labelled with our id must not verify.
	e := setup(t)
	e.project(t, "app", "prod")
	db := e.rawDB(t)
	dropMetaTriggers(t, db)
	attacker := otherKey(t)
	forged, err := attacker.Wrap(ctx, make([]byte, crypto.KeySize), mustAAD(t)(crypto.BindingAAD(attacker.ID())))
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE store_meta SET value = ? WHERE key = ?`, forged, metaBindingCanary)
	e.reopen(t)
	if _, err := e.svc.CreateProject(ctx, "x", ""); err == nil {
		t.Fatal("forged canary accepted")
	}
}

func TestBinding_DeletedBindingCannotBeAdoptedByAWrongKey(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	db := e.rawDB(t)
	dropMetaTriggers(t, db)
	db.Exec(`DELETE FROM store_meta`) // attacker wipes the binding hoping to re-bind to their key
	before := snapshot(t, db)
	bad := New(e.st, otherKey(t), nil)
	if _, err := bad.CreateProject(ctx, "intruder", ""); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("a wrong key adopted an unbound database that already holds projects: %v", err)
	}
	sameSnapshot(t, before, snapshot(t, db))
	// The legitimate key re-establishes the binding (it can unwrap every project key).
	e.reopen(t)
	if _, err := e.svc.CreateProject(ctx, "second", ""); err != nil {
		t.Fatalf("legitimate key could not recover the binding: %v", err)
	}
}

// A database created before store_meta existed (schema 1) is bound on first use,
// but only by the key that actually owns its projects.
func TestBinding_UpgradedSchema1DatabaseIsAdoptedOnlyByItsOwnKey(t *testing.T) {
	dir := securefstest.TempDir(t)
	dbPath := filepath.Join(dir, "old.db")
	keyPath := filepath.Join(dir, "k", "root.key")
	crypto.InstallKeyFile(keyPath)
	kp, _ := crypto.NewFileKeyProvider(keyPath)

	all, _ := sqlite.EmbeddedMigrations()
	old, err := sqlite.Open(ctx, dbPath, sqlite.Options{Migrations: all[:1]}) // schema 1 only
	if err != nil {
		t.Fatal(err)
	}
	dek, _ := crypto.GenerateKey()
	wrapped, _ := kp.Wrap(ctx, dek, mustAAD(t)(crypto.DEKAAD("p-old", kp.ID(), 1)))
	if err := old.CreateProject(ctx, store.Project{ID: "p-old", Slug: "legacy", WrappedDEK: wrapped, DEKKeyID: kp.ID()}); err != nil {
		t.Fatal(err)
	}
	old.Close()

	st, err := sqlite.Open(ctx, dbPath, sqlite.Options{}) // upgrade to current schema
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.LastMigrationBackup() == "" {
		t.Fatal("upgrade of an existing database took no backup")
	}
	if _, err := st.GetMeta(ctx, metaBindingCanary); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("upgrade must not invent a binding: %v", err)
	}
	if _, err := New(st, otherKey(t), nil).ListProjects(ctx); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("a foreign key adopted the old database: %v", err)
	}
	if _, err := st.GetMeta(ctx, metaBindingCanary); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("a rejected key left a binding behind")
	}
	right := New(st, kp, nil)
	ps, err := right.ListProjects(ctx)
	if err != nil || len(ps) != 1 || ps[0].Slug != "legacy" {
		t.Fatalf("owner key could not adopt its database: %v %v", ps, err)
	}
	if _, err := st.GetMeta(ctx, metaBindingCanary); err != nil {
		t.Fatalf("binding not created on adoption: %v", err)
	}
}

// ---- concurrent first initialisation --------------------------------------------

func TestBinding_ConcurrentFirstInitSameKeyAllSucceedWithOneBinding(t *testing.T) {
	dir := securefstest.TempDir(t)
	dbPath := filepath.Join(dir, "c.db")
	keyPath := filepath.Join(dir, "k", "root.key")
	crypto.InstallKeyFile(keyPath)
	first, err := sqlite.Open(ctx, dbPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := sqlite.Open(ctx, dbPath, sqlite.Options{})
			if err != nil {
				errs <- err
				return
			}
			defer st.Close()
			kp, _ := crypto.NewFileKeyProvider(keyPath)
			<-start
			errs <- New(st, kp, nil).Bind(ctx)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent initialiser with the SAME key failed: %v", err)
		}
	}
	st, _ := sqlite.Open(ctx, dbPath, sqlite.Options{})
	defer st.Close()
	for _, k := range []string{metaBindingVersion, metaBindingKeyID, metaBindingCanary} {
		if _, err := st.GetMeta(ctx, k); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
	}
}

func TestBinding_ConcurrentFirstInitDifferentKeysExactlyOneWins(t *testing.T) {
	dir := securefstest.TempDir(t)
	dbPath := filepath.Join(dir, "c.db")
	first, err := sqlite.Open(ctx, dbPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	const keys, per = 6, 3
	var wg sync.WaitGroup
	type res struct {
		key int
		err error
	}
	out := make(chan res, keys*per)
	start := make(chan struct{})
	for k := 0; k < keys; k++ {
		kp := otherKey(t)
		for j := 0; j < per; j++ {
			wg.Add(1)
			go func(k int) {
				defer wg.Done()
				st, err := sqlite.Open(ctx, dbPath, sqlite.Options{})
				if err != nil {
					out <- res{k, err}
					return
				}
				defer st.Close()
				<-start
				out <- res{k, New(st, kp, nil).Bind(ctx)}
			}(k)
		}
	}
	close(start)
	wg.Wait()
	close(out)
	winners := map[int]int{}
	losers := 0
	for r := range out {
		switch {
		case r.err == nil:
			winners[r.key]++
		case errors.Is(r.err, ErrWrongRootKey):
			losers++
		default:
			t.Fatalf("unexpected error: %v", r.err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("%d different keys believe they own the database: %v", len(winners), winners)
	}
	for _, c := range winners {
		if c != per || losers != keys*per-per {
			t.Fatalf("winner instances=%d losers=%d, want %d and %d", c, losers, per, keys*per-per)
		}
	}
}
