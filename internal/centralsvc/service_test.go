package centralsvc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	_ "modernc.org/sqlite"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

var ctx = context.Background()

const plain = "PLAINTEXT-s3cr3t-value-0123456789"

type env struct {
	svc     *Service
	st      *sqlite.Store
	dbPath  string
	keyPath string
	logs    *observer.ObservedLogs
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := securefstest.TempDir(t)
	e := &env{dbPath: filepath.Join(dir, "data", "c.db"), keyPath: filepath.Join(dir, "keys", "root.key")}
	if _, err := crypto.InstallKeyFile(e.keyPath); err != nil {
		t.Fatal(err)
	}
	e.reopen(t)
	return e
}

func (e *env) reopen(t *testing.T) {
	t.Helper()
	if e.st != nil {
		e.st.Close()
	}
	kp, err := crypto.NewFileKeyProvider(e.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, e.dbPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zap.DebugLevel)
	e.logs = logs
	e.st = st
	e.svc = New(st, kp, zap.New(core))
	t.Cleanup(func() { st.Close() })
}

func (e *env) project(t *testing.T, slug string, envs ...string) {
	t.Helper()
	if _, err := e.svc.CreateProject(ctx, slug, slug); err != nil {
		t.Fatal(err)
	}
	for _, en := range envs {
		if _, err := e.svc.CreateEnvironment(ctx, slug, en, en); err != nil {
			t.Fatal(err)
		}
	}
}

func mustGet(t *testing.T, e *env, p, en, n string, v int) string {
	t.Helper()
	val, err := e.svc.GetValue(ctx, p, en, n, v)
	if err != nil {
		t.Fatal(err)
	}
	return string(val.Plaintext)
}

// rawDB opens a second connection for tests that must bypass the service (and
// the immutability triggers) to simulate an attacker with database write access.
func (e *env) rawDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+e.dbPath+"?_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func dropTriggers(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, tr := range []string{"secret_versions_immutable", "secret_versions_no_delete"} {
		if _, err := db.Exec(`DROP TRIGGER ` + tr); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRoundTripAndVersioning(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	if err := e.svc.CreateSecret(ctx, "app", "prod", "db/password", []byte(plain), "alice", "initial"); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, e, "app", "prod", "db/password", 0); got != plain {
		t.Fatalf("got %q", got)
	}
	v2, err := e.svc.AddVersion(ctx, "app", "prod", "db/password", []byte("second"), "bob", "rotated")
	if err != nil || v2 != 2 {
		t.Fatalf("v2=%d %v", v2, err)
	}
	if got := mustGet(t, e, "app", "prod", "db/password", 0); got != "second" {
		t.Fatalf("latest = %q", got)
	}
	if got := mustGet(t, e, "app", "prod", "db/password", 1); got != plain {
		t.Fatalf("v1 changed after v2: %q", got)
	}
	if got := mustGet(t, e, "app", "prod", "db/password", 2); got != "second" {
		t.Fatalf("v2 = %q", got)
	}
	if _, err := e.svc.GetValue(ctx, "app", "prod", "db/password", 3); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("v3: %v", err)
	}
	// duplicate create is a conflict, never a silent new version
	if err := e.svc.CreateSecret(ctx, "app", "prod", "db/password", []byte("x"), "a", ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	if _, err := e.svc.AddVersion(ctx, "app", "prod", "missing", []byte("x"), "a", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("add to missing: %v", err)
	}
	// versions are immutable: adding never changed v1/v2 rows (re-read above); metadata attribution
	vs, _ := e.svc.ListVersions(ctx, "app", "prod", "db/password")
	if len(vs) != 2 || vs[0].CreatedBy != "alice" || vs[1].CreatedBy != "bob" || vs[1].Comment != "rotated" {
		t.Fatalf("metadata: %+v", vs)
	}
}

func TestDisableAndDelete(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "K", []byte("one"), "a", "")
	e.svc.AddVersion(ctx, "app", "prod", "K", []byte("two"), "a", "")
	if err := e.svc.DisableVersion(ctx, "app", "prod", "K", 2, "admin"); err != nil {
		t.Fatal(err)
	}
	// Disabling the newest version must NOT silently serve v1 (review M1-6).
	if v, err := e.svc.GetValue(ctx, "app", "prod", "K", 0); !errors.Is(err, store.ErrLatestVersionDisabled) || len(v.Plaintext) != 0 {
		t.Fatalf("latest after disabling v2 must fail closed, got %q err=%v", v.Plaintext, err)
	}
	if got := mustGet(t, e, "app", "prod", "K", 1); got != "one" { // explicit version still readable
		t.Fatalf("explicit v1 = %q", got)
	}
	if _, err := e.svc.AddVersion(ctx, "app", "prod", "K", []byte("three"), "a", ""); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, e, "app", "prod", "K", 0); got != "three" {
		t.Fatalf("latest after adding v3 = %q", got)
	}
	if _, err := e.svc.GetValue(ctx, "app", "prod", "K", 2); !errors.Is(err, store.ErrVersionDisabled) {
		t.Fatalf("disabled read: %v", err)
	}
	if err := e.svc.DeleteSecret(ctx, "app", "prod", "K", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetValue(ctx, "app", "prod", "K", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted read: %v", err)
	}
	// recreating the name starts a fresh secret at version 1
	if err := e.svc.CreateSecret(ctx, "app", "prod", "K", []byte("fresh"), "a", ""); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, e, "app", "prod", "K", 1); got != "fresh" {
		t.Fatalf("got %q", got)
	}
}

func TestValidation(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	for _, n := range []string{"", "/abs", "a//b", "a/", "../x", "a/../b", "a/./b", "has space", "semi;colon", strings.Repeat("a", 300), "nul\x00"} {
		if err := e.svc.CreateSecret(ctx, "app", "prod", n, []byte("v"), "a", ""); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("name %q accepted: %v", n, err)
		}
	}
	for _, n := range []string{"A", "db/password", "a.b-c_d/e", "_x"} {
		if err := ValidateSecretName(n); err != nil {
			t.Fatalf("valid name %q rejected", n)
		}
	}
	for _, s := range []string{"", "UPPER", "-x", "a b", "a/b", strings.Repeat("a", 64)} {
		if _, err := e.svc.CreateProject(ctx, s, ""); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("project slug %q accepted", s)
		}
	}
	if err := e.svc.CreateSecret(ctx, "app", "prod", "ok", nil, "a", ""); err == nil {
		t.Fatal("empty value accepted")
	}
	if err := e.svc.CreateSecret(ctx, "app", "prod", "ok", make([]byte, crypto.MaxPlaintextSize+1), "a", ""); err == nil {
		t.Fatal("oversize value accepted")
	}
	if err := e.svc.CreateSecret(ctx, "app", "nope", "ok", []byte("v"), "a", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown env: %v", err)
	}
}

func TestMetadataResponsesContainNoPlaintextOrKeyMaterial(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "K", []byte(plain), "a", "note")
	e.svc.AddVersion(ctx, "app", "prod", "K", []byte(plain+"-2"), "a", "")
	ms, _ := e.svc.ListSecrets(ctx, "app", "prod")
	vs, _ := e.svc.ListVersions(ctx, "app", "prod", "K")
	ps, _ := e.svc.ListProjects(ctx)
	pr, _ := e.svc.CreateProject(ctx, "other", "")
	es, _ := e.svc.ListEnvironments(ctx, "app")
	blob, _ := json.Marshal(map[string]any{"s": ms, "v": vs, "p": ps, "pr": pr, "e": es})
	if bytes.Contains(blob, []byte("PLAINTEXT")) {
		t.Fatalf("metadata contains plaintext: %s", blob)
	}
	for _, p := range append(ps, pr) {
		if len(p.WrappedDEK) != 0 {
			t.Fatal("project metadata exposes wrapped key blob")
		}
	}
	// structural guard: metadata types have no byte-slice (payload) fields
	for _, typ := range []any{store.SecretMeta{}, store.VersionMeta{}, store.Environment{}} {
		b, _ := json.Marshal(typ)
		for _, banned := range []string{"Ciphertext", "Nonce", "Plaintext", "Value", "WrappedDEK"} {
			if strings.Contains(string(b), banned) {
				t.Fatalf("%T has field %s", typ, banned)
			}
		}
	}
	if s := fmt.Sprintf("%v %+v %#v", Value{Plaintext: []byte(plain)}, Value{Plaintext: []byte(plain)}, Value{Plaintext: []byte(plain)}); strings.Contains(s, "PLAINTEXT") {
		t.Fatal("Value formatting leaks plaintext")
	}
}

func TestPlaintextNeverOnDisk(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "K", []byte(plain), "a", "")
	e.svc.AddVersion(ctx, "app", "prod", "K", []byte(plain+"-v2"), "a", "")
	check := func(when string) {
		files, _ := filepath.Glob(e.dbPath + "*")
		if len(files) == 0 {
			t.Fatal("no db files")
		}
		for _, f := range files {
			b, _ := os.ReadFile(f)
			if bytes.Contains(b, []byte("PLAINTEXT-s3cr3t")) {
				t.Fatalf("%s: plaintext found in %s", when, f)
			}
			kb, _ := os.ReadFile(e.keyPath)
			raw := strings.TrimPrefix(strings.TrimSpace(string(kb)), "dso-root-key-v1:")
			if bytes.Contains(b, []byte(raw)) {
				t.Fatalf("%s: root key found in %s", when, f)
			}
		}
	}
	check("with live WAL") // WAL not yet checkpointed
	e.st.Close()
	check("after close")
}

func TestLogsNeverContainPlaintextOrKeys(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "K", []byte(plain), "a", "c")
	e.svc.AddVersion(ctx, "app", "prod", "K", []byte(plain+"2"), "a", "")
	e.svc.GetValue(ctx, "app", "prod", "K", 0)
	e.svc.GetValue(ctx, "app", "prod", "K", 9)
	e.svc.DisableVersion(ctx, "app", "prod", "K", 1, "a")
	// force an integrity failure to exercise the warning path
	db := e.rawDB(t)
	dropTriggers(t, db)
	db.Exec(`UPDATE secret_versions SET ciphertext = x'00112233445566778899aabbccddeeff00112233' WHERE version = 2`)
	if _, err := e.svc.GetValue(ctx, "app", "prod", "K", 2); !errors.Is(err, ErrIntegrity) {
		t.Fatal(err)
	}
	e.svc.DeleteSecret(ctx, "app", "prod", "K", "a")
	kb, _ := os.ReadFile(e.keyPath)
	raw := strings.TrimPrefix(strings.TrimSpace(string(kb)), "dso-root-key-v1:")
	if e.logs.Len() == 0 {
		t.Fatal("expected operation logs")
	}
	for _, ent := range e.logs.All() {
		s := ent.Message
		for _, f := range ent.Context {
			s += " " + f.Key + "=" + f.String
		}
		if strings.Contains(s, "PLAINTEXT") || strings.Contains(s, raw) {
			t.Fatalf("log leaks secret material: %s", s)
		}
	}
}

// ---- integrity: tamper & substitution -----------------------------------------

func TestTamperedCiphertextNonceAndTagFailClosed(t *testing.T) {
	for _, col := range []string{"ciphertext", "nonce"} {
		e := setup(t)
		e.project(t, "app", "prod")
		e.svc.CreateSecret(ctx, "app", "prod", "K", []byte(plain), "a", "")
		db := e.rawDB(t)
		dropTriggers(t, db)
		var b []byte
		db.QueryRow(`SELECT ` + col + ` FROM secret_versions`).Scan(&b)
		b[len(b)-1] ^= 0x01 // last byte: GCM tag for ciphertext, nonce byte for nonce
		if _, err := db.Exec(`UPDATE secret_versions SET `+col+` = ?`, b); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.GetValue(ctx, "app", "prod", "K", 1)
		if !errors.Is(err, ErrIntegrity) {
			t.Fatalf("%s tamper: %v", col, err)
		}
	}
}

func TestCiphertextSubstitutionFailsClosed(t *testing.T) {
	// Every scenario keeps the SAME project DEK where possible, so only the AAD
	// binding (not a key mismatch) can stop the substitution.
	type row struct{ ct, nonce []byte }
	build := func(t *testing.T) (*env, *sql.DB) {
		e := setup(t)
		e.project(t, "app", "prod", "staging")
		e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("value-A"), "x", "")
		e.svc.AddVersion(ctx, "app", "prod", "A", []byte("value-A2"), "x", "")
		e.svc.CreateSecret(ctx, "app", "prod", "B", []byte("value-B"), "x", "")
		e.svc.CreateSecret(ctx, "app", "staging", "A", []byte("value-staging-A"), "x", "")
		db := e.rawDB(t)
		dropTriggers(t, db)
		return e, db
	}
	copyRow := func(t *testing.T, db *sql.DB, fromName, fromEnv string, fromVer int, toName, toEnv string, toVer int) {
		var r row
		if err := db.QueryRow(`SELECT v.ciphertext, v.nonce FROM secret_versions v JOIN secrets s ON s.id=v.secret_id
			JOIN environments e ON e.id=s.environment_id WHERE s.name=? AND e.slug=? AND v.version=?`, fromName, fromEnv, fromVer).Scan(&r.ct, &r.nonce); err != nil {
			t.Fatal(err)
		}
		res, err := db.Exec(`UPDATE secret_versions SET ciphertext=?, nonce=? WHERE secret_id=(SELECT s.id FROM secrets s JOIN environments e ON e.id=s.environment_id
			WHERE s.name=? AND e.slug=?) AND version=?`, r.ct, r.nonce, toName, toEnv, toVer)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			t.Fatalf("substitution touched %d rows", n)
		}
	}
	cases := []struct {
		name              string
		fromName, fromEnv string
		fromVer           int
		toName, toEnv     string
		toVer             int
	}{
		{"other secret, same env", "B", "prod", 1, "A", "prod", 1},
		{"same name, other environment", "A", "staging", 1, "A", "prod", 1},
		{"other version of same secret (v2 into v1)", "A", "prod", 2, "A", "prod", 1},
		{"other version of same secret (v1 into v2)", "A", "prod", 1, "A", "prod", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, db := build(t)
			copyRow(t, db, c.fromName, c.fromEnv, c.fromVer, c.toName, c.toEnv, c.toVer)
			_, err := e.svc.GetValue(ctx, "app", c.toEnv, c.toName, c.toVer)
			if !errors.Is(err, ErrIntegrity) {
				t.Fatalf("substituted ciphertext accepted: %v", err)
			}
		})
	}

	t.Run("other project (different DEK)", func(t *testing.T) {
		e := setup(t)
		e.project(t, "app", "prod")
		e.project(t, "other", "prod")
		e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("a"), "x", "")
		e.svc.CreateSecret(ctx, "other", "prod", "A", []byte("o"), "x", "")
		db := e.rawDB(t)
		dropTriggers(t, db)
		var ct, nonce []byte
		db.QueryRow(`SELECT v.ciphertext, v.nonce FROM secret_versions v JOIN secrets s ON s.id=v.secret_id JOIN projects p ON p.id=s.project_id WHERE p.slug='other'`).Scan(&ct, &nonce)
		db.Exec(`UPDATE secret_versions SET ciphertext=?, nonce=? WHERE secret_id=(SELECT s.id FROM secrets s JOIN projects p ON p.id=s.project_id WHERE p.slug='app')`, ct, nonce)
		if _, err := e.svc.GetValue(ctx, "app", "prod", "A", 1); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("cross-project substitution accepted: %v", err)
		}
	})

	t.Run("secret renamed in the database", func(t *testing.T) {
		e, db := build(t)
		db.Exec(`UPDATE secrets SET name='A-renamed' WHERE name='B'`)
		if _, err := e.svc.GetValue(ctx, "app", "prod", "A-renamed", 1); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("renamed secret accepted: %v", err)
		}
	})

	t.Run("secret moved to another environment", func(t *testing.T) {
		e, db := build(t)
		db.Exec(`PRAGMA foreign_keys=ON`)
		db.Exec(`UPDATE secrets SET environment_id=(SELECT e.id FROM environments e WHERE e.slug='staging') WHERE name='B'`)
		if _, err := e.svc.GetValue(ctx, "app", "staging", "B", 1); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("moved secret accepted: %v", err)
		}
	})
}

func TestUntamperedValuesStillReadAfterSubstitutionSetup(t *testing.T) {
	// guards the suite above: the same fixture reads fine when not tampered with
	e := setup(t)
	e.project(t, "app", "prod", "staging")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("value-A"), "x", "")
	e.svc.CreateSecret(ctx, "app", "staging", "A", []byte("value-staging-A"), "x", "")
	if mustGet(t, e, "app", "prod", "A", 1) != "value-A" || mustGet(t, e, "app", "staging", "A", 1) != "value-staging-A" {
		t.Fatal("fixture broken")
	}
}

func TestWrappedDEKTamperAndSubstitution(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.project(t, "other", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("a"), "x", "")
	db := e.rawDB(t)
	// swap in another project's wrapped DEK (same root key, different project id in AAD)
	var other []byte
	db.QueryRow(`SELECT wrapped_dek FROM projects WHERE slug='other'`).Scan(&other)
	db.Exec(`UPDATE projects SET wrapped_dek=? WHERE slug='app'`, other)
	if _, err := e.svc.GetValue(ctx, "app", "prod", "A", 1); err == nil {
		t.Fatal("wrapped DEK from another project accepted")
	}
	if _, err := e.svc.AddVersion(ctx, "app", "prod", "A", []byte("b"), "x", ""); err == nil {
		t.Fatal("writes allowed with an unverifiable DEK")
	}
}

// ---- key handling -----------------------------------------------------------------

func TestWrongRootKeyFailsClosedAndWritesNothing(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("a"), "x", "")

	otherKey := filepath.Join(securefstest.TempDir(t), "other.key")
	crypto.InstallKeyFile(otherKey)
	kp, _ := crypto.NewFileKeyProvider(otherKey)
	bad := New(e.st, kp, nil)
	if _, err := bad.GetValue(ctx, "app", "prod", "A", 0); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("read with wrong key: %v", err)
	}
	if _, err := bad.AddVersion(ctx, "app", "prod", "A", []byte("x"), "x", ""); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("write with wrong key: %v", err)
	}
	if err := bad.CreateSecret(ctx, "app", "prod", "N", []byte("x"), "x", ""); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("create with wrong key: %v", err)
	}
	vs, _ := e.svc.ListVersions(ctx, "app", "prod", "A")
	if len(vs) != 1 {
		t.Fatal("wrong-key write reached the database")
	}
}

type brokenProvider struct{ crypto.KeyProvider }

func (brokenProvider) Unwrap(context.Context, []byte, []byte) ([]byte, error) {
	return nil, errors.New("kms unavailable")
}
func (brokenProvider) Wrap(context.Context, []byte, []byte) ([]byte, error) {
	return nil, errors.New("kms unavailable")
}

func TestKeyProviderFailureFailsClosed(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("a"), "x", "")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	b := New(e.st, brokenProvider{kp}, nil)
	if _, err := b.GetValue(ctx, "app", "prod", "A", 0); err == nil {
		t.Fatal("read succeeded with failing provider")
	}
	if _, err := b.CreateProject(ctx, "new", ""); err == nil {
		t.Fatal("project created with failing provider")
	}
	if ps, _ := e.svc.ListProjects(ctx); len(ps) != 1 {
		t.Fatal("failed CreateProject left a row")
	}
}

func TestKeyProviderInterfaceIsStorageIndependent(t *testing.T) {
	// compile-time + behavioural: a non-file provider works with the same service
	e := setup(t)
	var _ crypto.KeyProvider = brokenProvider{}
	e.project(t, "app", "prod")
}

func TestRestartPersistenceWithFreshKeyProviderAndMissingKey(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte(plain), "x", "")
	e.svc.AddVersion(ctx, "app", "prod", "A", []byte("two"), "x", "")
	e.reopen(t) // new process equivalent: provider re-read from disk, DB reopened
	if mustGet(t, e, "app", "prod", "A", 1) != plain || mustGet(t, e, "app", "prod", "A", 0) != "two" {
		t.Fatal("data not readable after restart")
	}
	if err := os.Chmod(e.keyPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(e.keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.NewFileKeyProvider(e.keyPath); !errors.Is(err, crypto.ErrKeyMaterial) {
		t.Fatalf("missing key: %v", err)
	}
	// the database alone is not enough to read anything
	if _, err := e.st.GetProjectBySlug(ctx, "app"); err != nil {
		t.Fatal(err)
	}
}

func TestPassphraseProviderEndToEnd(t *testing.T) {
	dir := securefstest.TempDir(t)
	kf := filepath.Join(dir, "k", "root.pass")
	pass := []byte("a sufficiently long passphrase")
	if _, err := crypto.InstallPassphraseKeyFile(kf, pass); err != nil {
		t.Fatal(err)
	}
	kp, err := crypto.NewPassphraseKeyProvider(kf, pass)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, filepath.Join(dir, "d", "c.db"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st, kp, nil)
	svc.CreateProject(ctx, "app", "")
	svc.CreateEnvironment(ctx, "app", "prod", "")
	if err := svc.CreateSecret(ctx, "app", "prod", "A", []byte("pv"), "x", ""); err != nil {
		t.Fatal(err)
	}
	kp2, _ := crypto.NewPassphraseKeyProvider(kf, pass)
	v, err := New(st, kp2, nil).GetValue(ctx, "app", "prod", "A", 0)
	if err != nil || string(v.Plaintext) != "pv" {
		t.Fatalf("%v", err)
	}
	if _, err := crypto.NewPassphraseKeyProvider(kf, []byte("wrong passphrase!!")); !errors.Is(err, crypto.ErrWrongPassphrase) {
		t.Fatal("wrong passphrase accepted")
	}
}

func TestProjectsHaveIndependentDEKs(t *testing.T) {
	e := setup(t)
	e.project(t, "a", "prod")
	e.project(t, "b", "prod")
	p1, _ := e.st.GetProjectBySlug(ctx, "a")
	p2, _ := e.st.GetProjectBySlug(ctx, "b")
	if bytes.Equal(p1.WrappedDEK, p2.WrappedDEK) {
		t.Fatal("projects share wrapped DEK")
	}
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	d1, _ := kp.Unwrap(ctx, p1.WrappedDEK, mustAAD(t)(crypto.DEKAAD(p1.ID, p1.DEKKeyID, p1.DEKVersion)))
	d2, _ := kp.Unwrap(ctx, p2.WrappedDEK, mustAAD(t)(crypto.DEKAAD(p2.ID, p2.DEKKeyID, p2.DEKVersion)))
	if len(d1) != 32 || bytes.Equal(d1, d2) {
		t.Fatal("DEKs not independent")
	}
	if p1.DEKKeyID != kp.ID() {
		t.Fatal("key id not recorded")
	}
}

func TestEveryVersionUsesAUniqueNonce(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("same"), "x", "")
	for i := 0; i < 50; i++ {
		e.svc.AddVersion(ctx, "app", "prod", "A", []byte("same"), "x", "")
	}
	db := e.rawDB(t)
	rows, _ := db.Query(`SELECT nonce, ciphertext FROM secret_versions`)
	defer rows.Close()
	n, cts := map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var a, b []byte
		rows.Scan(&a, &b)
		if n[string(a)] || cts[string(b)] {
			t.Fatal("nonce or ciphertext repeated for identical plaintext")
		}
		n[string(a)], cts[string(b)] = true, true
	}
	if len(n) != 51 {
		t.Fatalf("%d versions", len(n))
	}
}

func TestConcurrentServiceUse(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "A", []byte("v0"), "x", "")
	var wg sync.WaitGroup
	errc := make(chan error, 64)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if _, err := e.svc.AddVersion(ctx, "app", "prod", "A", []byte(fmt.Sprintf("w%d-%d", i, j)), "x", ""); err != nil {
					errc <- err
					return
				}
				if _, err := e.svc.GetValue(ctx, "app", "prod", "A", 0); err != nil {
					errc <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	vs, _ := e.svc.ListVersions(ctx, "app", "prod", "A")
	if len(vs) != 41 {
		t.Fatalf("%d versions", len(vs))
	}
	for i := range vs {
		if _, err := e.svc.GetValue(ctx, "app", "prod", "A", i+1); err != nil {
			t.Fatalf("v%d unreadable: %v", i+1, err)
		}
	}
}

// A deleted secret's old ciphertext must not be accepted by a later secret that
// reuses the same name: project, environment, name and version are all equal,
// so only the secret id in the AAD distinguishes them.
func TestCiphertextFromDeletedSecretWithSameNameIsRejected(t *testing.T) {
	e := setup(t)
	e.project(t, "app", "prod")
	e.svc.CreateSecret(ctx, "app", "prod", "K", []byte("old-value"), "x", "")
	e.svc.DeleteSecret(ctx, "app", "prod", "K", "x")
	e.svc.CreateSecret(ctx, "app", "prod", "K", []byte("new-value"), "x", "")
	db := e.rawDB(t)
	dropTriggers(t, db)
	var ct, nonce []byte
	if err := db.QueryRow(`SELECT v.ciphertext, v.nonce FROM secret_versions v JOIN secrets s ON s.id=v.secret_id WHERE s.deleted_at IS NOT NULL`).Scan(&ct, &nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE secret_versions SET ciphertext=?, nonce=? WHERE secret_id=(SELECT id FROM secrets WHERE deleted_at IS NULL)`, ct, nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetValue(ctx, "app", "prod", "K", 1); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("old ciphertext accepted by new secret with same name: %v", err)
	}
}
