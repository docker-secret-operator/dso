package centralsvc

// M2 slice S3: test-only qualification of two audit-roadmap M2 gate items that
// already exist in the central store: "no plaintext at rest" and "backup/restore
// tested". Nothing here changes production behaviour; every assertion is the
// documented truth (docs/central-store-m1.md, docs/central-store-backup-restore.md):
//
//   - secret VALUES are encrypted (AES-256-GCM under a per-project DEK wrapped by
//     the root key); names, project/environment slugs, comments, timestamps and
//     actor strings are metadata and are stored in the clear;
//   - the root key and the DEK never appear in the database, a backup, a
//     preservation/safety copy or any log;
//   - files are 0600 and directories 0700 independent of the process umask.
//
// All marker values are synthetic strings, never real secrets.

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// Synthetic value markers. Each is long and distinctive so an accidental
// substring match is impossible.
const (
	s3V1      = "S3MARK-value-v1-9c41d7e2a0b3"
	s3V2      = "S3MARK-value-v2-51ae0f88c6d4"
	s3V3Dis   = "S3MARK-value-v3-DISABLED-b7720e1f"
	s3Deleted = "S3MARK-value-SOFTDELETED-3d96a4c0"
	s3NameVal = "S3MARK-value-for-name-marked-secret-e84f1b27"
	s3Other   = "S3MARK-value-other-env-2f5c6a91"
	// Metadata markers: documented to be stored in clear.
	s3NameMarker = "S3NAMEMARK-4d2e8a-token"
	s3Comment    = "S3COMMENTMARK-71c9"
	s3Actor      = "s3actor-marker-alice"
)

var s3ValueMarkers = []string{s3V1, s3V2, s3V3Dis, s3Deleted, s3NameVal, s3Other}

// s3Fixture is a populated central store (open, WAL not checkpointed).
type s3Fixture struct {
	*env
	root string // parent of data/ and keys/
}

// newS3Fixture builds: two projects, envs, a multi-version secret whose latest
// version is disabled-in-the-middle, a soft-deleted secret, a secret whose NAME
// contains a metadata marker, and a secret in a second environment.
func newS3Fixture(t *testing.T) *s3Fixture {
	t.Helper()
	e := setup(t)
	e.project(t, "alpha", "prod", "stage")
	e.project(t, "beta", "prod")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(e.svc.CreateSecret(ctx, "alpha", "prod", "multi/ver", []byte(s3V1), s3Actor, s3Comment))
	if v, err := e.svc.AddVersion(ctx, "alpha", "prod", "multi/ver", []byte(s3V2), s3Actor, "rot2"); err != nil || v != 2 {
		t.Fatalf("v2=%d %v", v, err)
	}
	if v, err := e.svc.AddVersion(ctx, "alpha", "prod", "multi/ver", []byte(s3V3Dis), s3Actor, "rot3"); err != nil || v != 3 {
		t.Fatalf("v3=%d %v", v, err)
	}
	must(e.svc.DisableVersion(ctx, "alpha", "prod", "multi/ver", 3, s3Actor))
	must(e.svc.CreateSecret(ctx, "alpha", "prod", "to/delete", []byte(s3Deleted), s3Actor, ""))
	must(e.svc.DeleteSecret(ctx, "alpha", "prod", "to/delete", s3Actor))
	must(e.svc.CreateSecret(ctx, "alpha", "prod", s3NameMarker, []byte(s3NameVal), s3Actor, ""))
	must(e.svc.CreateSecret(ctx, "beta", "prod", "other", []byte(s3Other), s3Actor, ""))
	return &s3Fixture{env: e, root: filepath.Dir(filepath.Dir(e.dbPath))}
}

// ---- scanning -----------------------------------------------------------------------------------------

type needle struct {
	label string
	b     []byte
}

// keyNeedles returns the root key (base64 text form as stored in the key file,
// and raw bytes) and every project's unwrapped DEK as scan needles.
func keyNeedles(t *testing.T, f *s3Fixture) []needle {
	t.Helper()
	kb, err := os.ReadFile(f.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	b64 := strings.TrimPrefix(strings.TrimSpace(string(kb)), fileKeyPrefixForTest)
	raw, err := base64.RawStdEncoding.DecodeString(b64)
	if err != nil || len(raw) != 32 {
		t.Fatalf("unexpected key file format (%v, len %d)", err, len(raw))
	}
	ns := []needle{{"root-key-base64", []byte(b64)}, {"root-key-raw", raw}}
	projects, err := f.svc.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		full, err := f.st.GetProjectBySlug(ctx, p.Slug)
		if err != nil {
			t.Fatal(err)
		}
		dek, err := f.svc.unwrapDEK(ctx, full)
		if err != nil {
			t.Fatal(err)
		}
		ns = append(ns, needle{"dek-" + p.Slug, append([]byte(nil), dek...)})
		crypto.Zero(dek)
	}
	return ns
}

// fileKeyPrefixForTest mirrors the documented key-file prefix (crypto.fileKeyPrefix is unexported).
const fileKeyPrefixForTest = "dso-root-key-v1:"

func valueNeedles() []needle {
	var ns []needle
	for _, m := range s3ValueMarkers {
		ns = append(ns, needle{"value:" + m, []byte(m)})
	}
	return ns
}

// listFiles returns every regular file below root (all names, including
// dotfiles, -wal/-shm/-journal, .bak, staging and preservation directories).
func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// scanHits returns "label in path" for every needle occurrence in every file
// under root, plus the number of files and bytes scanned (so callers can assert
// the scan was not vacuous).
func scanHits(t *testing.T, root string, needles []needle) (hits []string, files int, bytesScanned int64) {
	t.Helper()
	for _, f := range listFiles(t, root) {
		b, err := os.ReadFile(f) // #nosec G304 -- test scans files under its own temp dir
		if err != nil {
			t.Fatal(err)
		}
		files++
		bytesScanned += int64(len(b))
		for _, n := range needles {
			if len(n.b) > 0 && bytes.Contains(b, n.b) {
				hits = append(hits, n.label+" in "+f)
			}
		}
	}
	return hits, files, bytesScanned
}

// scanTree fails the test if any needle occurs in any file under root.
func scanTree(t *testing.T, when, root string, needles []needle) (files int, bytesScanned int64) {
	t.Helper()
	hits, files, bytesScanned := scanHits(t, root, needles)
	for _, h := range hits {
		t.Errorf("%s: %s (cleartext)", when, h)
	}
	return files, bytesScanned
}

// containsAny reports whether any needle occurs in s (error text, log text).
func containsAny(s string, needles []needle) (string, bool) {
	for _, n := range needles {
		if len(n.b) > 0 && strings.Contains(s, string(n.b)) {
			return n.label, true
		}
	}
	return "", false
}

// dumpLogs renders every observed log entry (message, level and all fields).
func dumpLogs(logs *observer.ObservedLogs) string {
	var sb strings.Builder
	for _, ent := range logs.All() {
		b, _ := json.Marshal(ent.ContextMap())
		fmt.Fprintf(&sb, "%s %s %s\n", ent.Level, ent.Message, b)
	}
	return sb.String()
}

// dirState records a deterministic digest of every entry below root.
type entryState struct {
	Mode   fs.FileMode
	Size   int64
	Digest string
}

func treeState(t *testing.T, root string) map[string]entryState {
	t.Helper()
	m := map[string]entryState{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		st := entryState{Mode: fi.Mode(), Size: fi.Size()}
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(p) // #nosec G304 G122 -- test reads its own temp dir
			if err != nil {
				return err
			}
			h := sha256.Sum256(b)
			st.Digest = hex.EncodeToString(h[:])
		}
		m[rel] = st
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertTreeUnchanged(t *testing.T, what string, before, after map[string]entryState) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("%s: directory contents changed\nbefore: %v\nafter:  %v", what, before, after)
	}
}

// ---- (a) no plaintext at rest ---------------------------------------------------------------------------------------

// TestS3_NoPlaintextAtRest_EveryPersistedByte creates the fixture and scans, at
// each stage, EVERY file in the data directory and backup directory, plus the
// service logger output, for value markers, the root key and the DEKs.
func TestS3_NoPlaintextAtRest_EveryPersistedByte(t *testing.T) {
	f := newS3Fixture(t)
	needles := append(valueNeedles(), keyNeedles(t, f)...)
	dataDir := filepath.Dir(f.dbPath)

	// Positive control: the scanner really sees bytes. Metadata markers are
	// documented to be stored in clear, so they MUST be findable in the raw
	// persisted bytes; if they are not, the scan is not reading what we think.
	// (Names/comments/actors are also expected in the audit rows.)
	clear := []needle{{"name-marker", []byte(s3NameMarker)}, {"actor", []byte(s3Actor)}, {"project-slug", []byte("alpha")}}
	live := listFiles(t, dataDir)
	var haveDB, haveWAL bool
	var all []byte
	for _, p := range live {
		if p == f.dbPath {
			haveDB = true
		}
		if p == f.dbPath+"-wal" {
			haveWAL = true
		}
		b, _ := os.ReadFile(p) // #nosec G304 -- test-owned temp file
		all = append(all, b...)
	}
	if !haveDB || !haveWAL {
		t.Fatalf("expected main DB and -wal while the store is open (db=%v wal=%v): %v", haveDB, haveWAL, live)
	}
	for _, c := range clear {
		if !bytes.Contains(all, c.b) {
			t.Errorf("documented-cleartext metadata %s not found in persisted bytes: scanner or docs are wrong", c.label)
		}
	}
	nf, nb := scanTree(t, "live (open store, uncheckpointed WAL)", dataDir, needles)
	if nf < 2 || nb < 4096 {
		t.Fatalf("vacuous scan: %d files, %d bytes", nf, nb)
	}

	// Reads (reveal) must not write plaintext either.
	if got := mustGet(t, f.env, "alpha", "prod", "multi/ver", 1); got != s3V1 {
		t.Fatalf("v1 mismatch")
	}
	if _, err := f.svc.GetValue(ctx, "alpha", "prod", "multi/ver", 3); !errors.Is(err, store.ErrVersionDisabled) {
		t.Fatalf("disabled version read: %v", err)
	}
	if _, err := f.svc.GetValue(ctx, "alpha", "prod", "multi/ver", 0); !errors.Is(err, store.ErrLatestVersionDisabled) {
		t.Fatalf("latest-disabled read (no fallback): %v", err)
	}
	scanTree(t, "after reads", dataDir, needles)

	// Backup via the audited facility, into a separate backup directory.
	bdir := filepath.Join(f.root, "backups")
	if err := os.Mkdir(bdir, 0o700); err != nil {
		t.Fatal(err)
	}
	core, blogs := observer.New(zap.DebugLevel)
	bpath, err := BackupWithAudit(ctx, f.st, filepath.Join(bdir, "b1.db"), s3Actor, zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	if nf, _ := scanTree(t, "backup archive", bdir, needles); nf != 1 {
		t.Fatalf("expected exactly the backup file in the backup dir, got %d", nf)
	}
	scanTree(t, "data dir after backup (no staging leftovers)", dataDir, needles)

	// Close (checkpoint) then re-scan: main DB after WAL folded in.
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	scanTree(t, "after close", dataDir, needles)

	// Restore into a FRESH directory, then scan everything that restore wrote.
	fresh := securefstest.TempDir(t)
	kp, _ := crypto.NewFileKeyProvider(f.keyPath)
	rcore, rlogs := observer.New(zap.DebugLevel)
	res, err := Restore(ctx, RestoreOptions{
		CandidatePath: bpath, DBPath: filepath.Join(fresh, "c.db"), KeyProvider: kp, Actor: s3Actor,
		Logger: zap.New(rcore), Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	scanTree(t, "fresh dir after restore", fresh, needles)
	scanTree(t, "backup dir after restore (candidate never modified)", bdir, needles)

	// Restore OVER a live populated database: displaced/preserved/safety copies
	// of the original are persisted too and must hold only ciphertext.
	f2 := newS3Fixture(t)
	needles2 := append(valueNeedles(), keyNeedles(t, f2)...)
	bpath2, err := f2.st.Backup(ctx, filepath.Join(filepath.Dir(f2.dbPath), "cand.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f2.st.Close(); err != nil {
		t.Fatal(err)
	}
	kp2, _ := crypto.NewFileKeyProvider(f2.keyPath)
	if _, err := Restore(ctx, RestoreOptions{
		CandidatePath: bpath2, DBPath: f2.dbPath, KeyProvider: kp2,
		Confirm:                  func(sqlite.CandidateInfo) bool { return true },
		ConfirmAfterPreservation: func(RestorePlan, PreservationInfo) bool { return true },
	}); err != nil {
		t.Fatal(err)
	}
	scanTree(t, "restore over live DB (safety snapshot / displaced / preserved copies)", filepath.Dir(f2.dbPath), needles2)

	// Failed restore (corrupt candidate): nothing it leaves behind may hold plaintext.
	bad := filepath.Join(bdir, "bad.db")
	mustWriteFile(t, bad, []byte("S3MARK-garbage-not-a-database"), 0o600)
	if _, err := Restore(ctx, RestoreOptions{CandidatePath: bad, DBPath: filepath.Join(fresh, "other.db"), KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true }}); err == nil {
		t.Fatal("garbage candidate must be refused")
	}
	scanTree(t, "after failed restore", fresh, needles)

	// Logger output of the service and of backup/restore.
	logTxt := dumpLogs(f.logs) + dumpLogs(blogs) + dumpLogs(rlogs)
	if f.logs.Len() == 0 {
		t.Fatalf("expected service log output to inspect (service=%d)", f.logs.Len())
	}
	if lbl, hit := containsAny(logTxt, needles); hit {
		t.Fatalf("log output contains %s:\n%s", lbl, logTxt)
	}
	// Documented: names are metadata and may be logged in the clear. Observation only.
	t.Logf("observation: secret NAME marker appears in logs=%v (names are documented metadata)", strings.Contains(logTxt, s3NameMarker))
}

// TestS3_NoPlaintextAtRest_ScannerCatchesAPlantedMarker proves the scanner is not
// vacuous: the same scan flags a planted value marker, a planted key and a
// planted DEK-shaped needle in every file kind (main, wal, shm, journal, bak).
func TestS3_NoPlaintextAtRest_ScannerCatchesAPlantedMarker(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"c.db", "c.db-wal", "c.db-shm", "c.db-journal", "c.db.pre-migration-v1-x.bak", "sub/deep.tmp"} {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, p, []byte("xx"+s3V2+"yy"), 0o600)
	}
	hits, files, _ := scanHits(t, dir, []needle{{"planted", []byte(s3V2)}})
	if files != 6 || len(hits) != 6 {
		t.Fatalf("scanner flagged %d of %d planted files: %v", len(hits), files, hits)
	}
	if hits, _, _ := scanHits(t, dir, []needle{{"absent", []byte("S3MARK-never-written")}}); len(hits) != 0 {
		t.Fatalf("false positive: %v", hits)
	}
}

// TestS3_KeyCustody_ModesAndSeparation asserts only what the docs promise
// (key file 0400 in a 0700 directory, DB 0600, key not in the DB directory) and
// logs everything else as an observation.
func TestS3_KeyCustody_ModesAndSeparation(t *testing.T) {
	f := newS3Fixture(t)
	mode := func(p string) fs.FileMode {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return fi.Mode().Perm()
	}
	// Docs (central-store-m1.md "File and directory safety"): key file 0400, no
	// group/other bits; DB 0600; missing directories created 0700.
	if m := mode(f.keyPath); m != 0o400 {
		t.Errorf("key file mode %o, documented 0400", m)
	}
	if m := mode(filepath.Dir(f.keyPath)); m != 0o700 {
		t.Errorf("key dir mode %o, documented 0700", m)
	}
	if m := mode(f.dbPath); m != 0o600 {
		t.Errorf("db mode %o, documented 0600", m)
	}
	if m := mode(filepath.Dir(f.dbPath)); m != 0o700 {
		t.Errorf("db dir mode %o, documented 0700", m)
	}
	if filepath.Dir(f.keyPath) == filepath.Dir(f.dbPath) {
		t.Error("key and database share a directory; documented as refused by init")
	}
	// Nothing key-shaped sits beside the DB.
	for _, p := range listFiles(t, filepath.Dir(f.dbPath)) {
		if strings.Contains(filepath.Base(p), "key") {
			t.Errorf("unexpected key-like file beside the database: %s", p)
		}
	}
	// Observations (not asserted: the docs make no promise).
	for _, sfx := range []string{"-wal", "-shm"} {
		if fi, err := os.Stat(f.dbPath + sfx); err == nil {
			t.Logf("observation: %s mode %o", filepath.Base(f.dbPath+sfx), fi.Mode().Perm())
			if fi.Mode().Perm()&0o077 != 0 {
				t.Errorf("sidecar %s is group/other accessible (%o)", sfx, fi.Mode().Perm())
			}
		}
	}
	t.Logf("observation: key and DB are in different directories of one parent (%s); DSO does not verify separate volumes (documented)", f.root)
}

// ---- (b) backup/restore round trip ----------------------------------------------------------------------------------

// snapshotOf is everything list/get return, for equality across backup/restore.
type versionSnap struct {
	Meta      store.VersionMeta
	Plaintext string
	ReadErr   string
}
type secretSnap struct {
	Meta     store.SecretMeta
	Versions []versionSnap
}
type stateSnap struct {
	Projects []store.Project
	Envs     map[string][]store.Environment
	Secrets  map[string][]secretSnap // key: project/env
}

func takeSnapshot(t *testing.T, svc *Service, st store.Store) stateSnap {
	t.Helper()
	s := stateSnap{Envs: map[string][]store.Environment{}, Secrets: map[string][]secretSnap{}}
	ps, err := svc.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Projects = ps
	for _, p := range ps {
		es, err := svc.ListEnvironments(ctx, p.Slug)
		if err != nil {
			t.Fatal(err)
		}
		s.Envs[p.Slug] = es
		for _, e := range es {
			ss, err := svc.ListSecrets(ctx, p.Slug, e.Slug)
			if err != nil {
				t.Fatal(err)
			}
			var out []secretSnap
			for _, sec := range ss {
				vs, err := svc.ListVersions(ctx, p.Slug, e.Slug, sec.Name)
				if err != nil {
					t.Fatal(err)
				}
				sn := secretSnap{Meta: sec}
				for _, vm := range vs {
					v := versionSnap{Meta: vm}
					val, err := svc.GetValue(ctx, p.Slug, e.Slug, sec.Name, vm.Version)
					if err != nil {
						v.ReadErr = err.Error()
					} else {
						v.Plaintext = string(val.Plaintext)
						val.Zero()
					}
					sn.Versions = append(sn.Versions, v)
				}
				out = append(out, sn)
			}
			s.Secrets[p.Slug+"/"+e.Slug] = out
		}
	}
	return s
}

func (f *s3Fixture) backupTo(t *testing.T, name string) string {
	t.Helper()
	bdir := filepath.Join(f.root, "backups")
	if err := os.MkdirAll(bdir, 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := BackupWithAudit(ctx, f.st, filepath.Join(bdir, name), "s3", nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func restoreFresh(t *testing.T, bpath, keyPath string) (string, RestoreResult, error) {
	t.Helper()
	fresh := securefstest.TempDir(t)
	kp, err := crypto.NewFileKeyProvider(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(fresh, "data", "c.db")
	if err := os.Mkdir(filepath.Dir(db), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := Restore(ctx, RestoreOptions{CandidatePath: bpath, DBPath: db, KeyProvider: kp, Actor: "s3",
		Confirm: func(sqlite.CandidateInfo) bool { return true }})
	return db, res, err
}

func openSvc(t *testing.T, dbPath, keyPath string) (*Service, *sqlite.Store) {
	t.Helper()
	kp, err := crypto.NewFileKeyProvider(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, dbPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, kp, zap.NewNop()), st
}

// TestS3_BackupRestore_FreshDirRoundTrip: backup -> restore into a new empty
// data dir -> list/get return identical metadata and the same plaintext for every
// version; disabled stays disabled; soft-deleted stays deleted; the audit chain
// verifies; modes are 0600/0700.
func TestS3_BackupRestore_FreshDirRoundTrip(t *testing.T) {
	f := newS3Fixture(t)
	want := takeSnapshot(t, f.svc, f.st)
	// sanity: the snapshot really contains what we claim to compare
	multi := want.Secrets["alpha/prod"]
	var sawDisabled, sawPlain bool
	for _, s := range multi {
		for _, v := range s.Versions {
			if v.Meta.DisabledAt != nil && v.ReadErr != "" && v.Plaintext == "" {
				sawDisabled = true
			}
			if v.Plaintext == s3V1 || v.Plaintext == s3V2 {
				sawPlain = true
			}
		}
	}
	if !sawDisabled || !sawPlain || len(multi) != 2 {
		t.Fatalf("fixture snapshot incomplete: disabled=%v plain=%v secrets=%d", sawDisabled, sawPlain, len(multi))
	}
	bpath := f.backupTo(t, "rt.db")
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}

	db, res, err := restoreFresh(t, bpath, f.keyPath)
	if err != nil {
		t.Fatalf("restore into fresh dir: %v", err)
	}
	if res.RestoreEventID == "" {
		t.Error("restore did not record its audit event")
	}
	svc, st := openSvc(t, db, f.keyPath)
	got := takeSnapshot(t, svc, st)
	if !reflect.DeepEqual(want, got) {
		wb, _ := json.MarshalIndent(redactSnap(want), "", " ")
		gb, _ := json.MarshalIndent(redactSnap(got), "", " ")
		t.Fatalf("restored state differs from source\nwant: %s\ngot:  %s", wb, gb)
	}
	// Soft-deleted secret: not listed, not readable, but its row and versions are retained.
	if _, err := svc.GetValue(ctx, "alpha", "prod", "to/delete", 0); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("soft-deleted secret readable after restore: %v", err)
	}
	var deleted, versions int
	raw := rawOpen(t, db)
	if err := raw.QueryRow(`SELECT COUNT(*) FROM secrets WHERE name='to/delete' AND deleted_at IS NOT NULL`).Scan(&deleted); err != nil || deleted != 1 {
		t.Errorf("deleted secret row after restore: %d %v", deleted, err)
	}
	if err := raw.QueryRow(`SELECT COUNT(*) FROM secret_versions v JOIN secrets s ON s.id=v.secret_id WHERE s.name='to/delete'`).Scan(&versions); err != nil || versions != 1 {
		t.Errorf("deleted secret versions retained after restore: %d %v", versions, err)
	}
	// Audit chain verifies and the first restore is evidenced.
	if r := sqlite.VerifyAuditLog(ctx, db); r.Status != sqlite.AuditVerified {
		t.Errorf("audit chain after restore: %+v", r)
	}
	evs, _ := st.ListAuditEvents(ctx)
	var restores int
	for _, e := range evs {
		if e.EventType == store.AuditRestore {
			restores++
		}
	}
	if restores != 1 {
		t.Errorf("restore events = %d, want 1", restores)
	}
	assertModes(t, filepath.Dir(filepath.Dir(db)), "after restore into fresh dir")
}

// redactSnap hides nothing sensitive (markers are synthetic) but strips IDs of
// time noise; it exists only to keep failure output readable.
func redactSnap(s stateSnap) any { return s }

// rawOpen opens a private connection that bypasses the service (read/write).
func rawOpen(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustWriteFile(t *testing.T, p string, b []byte, m os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, b, m); err != nil { // #nosec G703 -- test-owned temp path
		t.Fatal(err)
	}
}

// assertModes: every regular file under root is exactly 0600 or the documented 0400 (key
// files), every directory 0700 (root's own parent excluded).
func assertModes(t *testing.T, root, when string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		m := fi.Mode().Perm()
		switch {
		case fi.IsDir():
			if m != 0o700 {
				t.Errorf("%s: dir %s mode %o, want 0700", when, p, m)
			}
		case fi.Mode().IsRegular():
			if m != 0o600 && m != 0o400 {
				t.Errorf("%s: file %s mode %o, want 0600 (0400 for keys)", when, p, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestS3_Restore_DryRunDoesNotMutate: a dry run against both a fresh dir and a
// populated live database changes no byte, mode or file anywhere.
func TestS3_Restore_DryRunDoesNotMutate(t *testing.T) {
	f := newS3Fixture(t)
	bpath := f.backupTo(t, "dry.db")
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	kp, _ := crypto.NewFileKeyProvider(f.keyPath)

	fresh := securefstest.TempDir(t)
	beforeFresh := treeState(t, fresh)
	beforeLive := treeState(t, f.root)
	r, err := Restore(ctx, RestoreOptions{CandidatePath: bpath, DBPath: filepath.Join(fresh, "c.db"), KeyProvider: kp, DryRun: true,
		Confirm: func(sqlite.CandidateInfo) bool { t.Error("dry run must not ask for confirmation"); return true }})
	if err != nil {
		t.Fatal(err)
	}
	if !r.DryRun || r.RestoreEventID != "" || r.SafetySnapshot != "" || r.RawPreservation != "" {
		t.Fatalf("dry-run result: %+v", r)
	}
	assertTreeUnchanged(t, "fresh dir (dry run)", beforeFresh, treeState(t, fresh))
	assertTreeUnchanged(t, "source tree incl. candidate (dry run)", beforeLive, treeState(t, f.root))

	// Dry run pointing at the live populated DB.
	if _, err := Restore(ctx, RestoreOptions{CandidatePath: bpath, DBPath: f.dbPath, KeyProvider: kp, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	assertTreeUnchanged(t, "live tree (dry run against live DB)", beforeLive, treeState(t, f.root))
}

// corruptions of a valid backup, each of which restore must refuse.
func s3Corruptions(t *testing.T, good string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(good) // #nosec G304 -- test-owned temp file
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(good)
	w := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		mustWriteFile(t, p, data, 0o600)
		return p
	}
	out := map[string]string{}
	out["truncated-half"] = w("trunc.db", b[:len(b)/2])
	out["truncated-header-only"] = w("hdr.db", b[:100])
	out["empty"] = w("empty.db", nil)
	out["garbage"] = w("garbage.db", []byte(strings.Repeat("not a sqlite database ", 400)))
	flipped := append([]byte(nil), b...)
	for i := 4096; i < len(flipped); i += 512 { // flip a byte in every sampled page after the header page
		flipped[i] ^= 0xFF
	}
	out["bitflipped-pages"] = w("flip.db", flipped)

	// Structurally valid SQLite whose audit chain has been tampered with.
	tp := filepath.Join(dir, "tamper.db")
	mustWriteFile(t, tp, b, 0o600)
	raw := rawOpen(t, tp)
	// An attacker with file write access can drop the immutability trigger on a
	// copy; the chain hash, not the trigger, is what restore relies on.
	if _, err := raw.Exec(`DROP TRIGGER audit_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE audit_events SET actor_label = 'tampered-by-test' WHERE rowid = (SELECT MIN(rowid) FROM audit_events)`); err != nil {
		t.Fatalf("could not tamper audit row (fixture issue): %v", err)
	}
	_ = raw.Close()
	out["tampered-audit-chain"] = tp
	return out
}

// TestS3_Restore_RefusesCorruptBackups_WithoutDisplacingLiveData: every corrupt
// variant is refused, with no change to the live database or any directory, and
// the error never contains plaintext or key material. Also into a fresh dir:
// refused restores leave NO file behind.
func TestS3_Restore_RefusesCorruptBackups_WithoutDisplacingLiveData(t *testing.T) {
	f := newS3Fixture(t)
	good := f.backupTo(t, "good.db")
	cands := s3Corruptions(t, good)
	needles := append(valueNeedles(), keyNeedles(t, f)...)
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	kp, _ := crypto.NewFileKeyProvider(f.keyPath)
	wantLive := treeState(t, filepath.Dir(f.dbPath))
	wantBackups := treeState(t, filepath.Dir(good))

	for name, cand := range cands {
		t.Run(name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			// into the live database
			_, err := Restore(ctx, RestoreOptions{CandidatePath: cand, DBPath: f.dbPath, KeyProvider: kp, Logger: zap.New(core),
				Confirm:                  func(sqlite.CandidateInfo) bool { return true },
				ConfirmAfterPreservation: func(RestorePlan, PreservationInfo) bool { return true }})
			if err == nil {
				t.Fatalf("corrupt candidate %q was accepted", name)
			}
			if lbl, hit := containsAny(err.Error()+dumpLogs(logs), needles); hit {
				t.Errorf("error/log leaks %s: %v", lbl, err)
			}
			assertTreeUnchanged(t, "live data dir after refused restore", wantLive, treeState(t, filepath.Dir(f.dbPath)))

			// into a fresh dir: nothing created
			fresh := securefstest.TempDir(t)
			if _, err := Restore(ctx, RestoreOptions{CandidatePath: cand, DBPath: filepath.Join(fresh, "c.db"), KeyProvider: kp,
				Confirm: func(sqlite.CandidateInfo) bool { return true }}); err == nil {
				t.Fatalf("corrupt candidate %q accepted into fresh dir", name)
			}
			if fs := listFiles(t, fresh); len(fs) != 0 {
				t.Errorf("refused restore left files in the fresh dir: %v", fs)
			}
			// the refused candidate dir has nothing new (no staging copies beside it)
			for rel := range treeState(t, filepath.Dir(good)) {
				if _, ok := wantBackups[rel]; !ok && !isCorruptionName(rel) {
					t.Errorf("unexpected new file beside the candidate: %s", rel)
				}
			}
		})
	}
	// the live database is still fully usable after all the refusals
	svc, st := openSvc(t, f.dbPath, f.keyPath)
	_ = st
	if got, err := svc.GetValue(ctx, "alpha", "prod", "multi/ver", 2); err != nil || string(got.Plaintext) != s3V2 {
		t.Fatalf("live data unreadable after refused restores: %v", err)
	}
}

func isCorruptionName(rel string) bool {
	switch rel {
	case "trunc.db", "hdr.db", "empty.db", "garbage.db", "flip.db", "tamper.db":
		return true
	}
	return false
}

// TestS3_Restore_WrongOrMissingKey_FailsClosed: restore with the wrong root key
// is refused with ErrWrongRootKey, nothing is written, no plaintext/key in the
// error; a missing key file fails before restore can start; and opening a
// restored database with the wrong key fails closed.
func TestS3_Restore_WrongOrMissingKey_FailsClosed(t *testing.T) {
	f := newS3Fixture(t)
	bpath := f.backupTo(t, "k.db")
	needles := append(valueNeedles(), keyNeedles(t, f)...)
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(f.root, "otherkeys", "root.key")
	if _, err := crypto.InstallKeyFile(other); err != nil {
		t.Fatal(err)
	}
	okb, _ := os.ReadFile(other) // #nosec G304 -- test-owned temp file
	needles = append(needles, needle{"other-key", []byte(strings.TrimPrefix(strings.TrimSpace(string(okb)), fileKeyPrefixForTest))})
	wrong, _ := crypto.NewFileKeyProvider(other)

	fresh := securefstest.TempDir(t)
	live := treeState(t, filepath.Dir(f.dbPath))
	_, err := Restore(ctx, RestoreOptions{CandidatePath: bpath, DBPath: filepath.Join(fresh, "c.db"), KeyProvider: wrong,
		Confirm: func(sqlite.CandidateInfo) bool { return true }})
	if !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("wrong key restore error = %v, want ErrWrongRootKey", err)
	}
	if lbl, hit := containsAny(err.Error(), needles); hit {
		t.Errorf("wrong-key error leaks %s: %v", lbl, err)
	}
	if fs := listFiles(t, fresh); len(fs) != 0 {
		t.Errorf("wrong-key restore left files: %v", fs)
	}
	// wrong key over a live DB leaves it untouched
	if _, err := Restore(ctx, RestoreOptions{CandidatePath: bpath, DBPath: f.dbPath, KeyProvider: wrong,
		Confirm: func(sqlite.CandidateInfo) bool { return true }}); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("wrong key over live DB: %v", err)
	}
	assertTreeUnchanged(t, "live dir after wrong-key restore", live, treeState(t, filepath.Dir(f.dbPath)))

	// missing key file: the provider cannot even be constructed -> restore cannot be invoked with it.
	if _, err := crypto.NewFileKeyProvider(filepath.Join(f.root, "nokeys", "absent.key")); err == nil {
		t.Fatal("missing key file must fail closed at provider construction")
	} else if lbl, hit := containsAny(err.Error(), needles); hit {
		t.Errorf("missing-key error leaks %s", lbl)
	}

	// Using a restored DB with the wrong key: service refuses, no plaintext.
	db, _, err := restoreFresh(t, bpath, f.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	svc := New(st, wrong, zap.NewNop())
	val, err := svc.GetValue(ctx, "alpha", "prod", "multi/ver", 1)
	if err == nil || len(val.Plaintext) != 0 {
		t.Fatalf("wrong key must not decrypt: err=%v", err)
	}
	if lbl, hit := containsAny(err.Error(), needles); hit {
		t.Errorf("wrong-key read error leaks %s: %v", lbl, err)
	}
}

// TestS3_Restore_FailedMidwayLeavesLiveDataIntact: a failure injected at the
// replacement step leaves the original database readable and unchanged, and
// leaves only documented artefacts (safety snapshot / preservation) that hold no
// plaintext and no stray temp files.
func TestS3_Restore_FailedMidwayLeavesLiveDataIntact(t *testing.T) {
	f := newS3Fixture(t)
	needles := append(valueNeedles(), keyNeedles(t, f)...)
	cand := f.backupTo(t, "mid.db")
	// give the live DB newer content that the candidate lacks
	if _, err := f.svc.AddVersion(ctx, "alpha", "prod", "multi/ver", []byte(s3V1+"-newer"), "s3", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	kp, _ := crypto.NewFileKeyProvider(f.keyPath)
	boom := errors.New("injected replacement failure")
	res, err := Restore(ctx, RestoreOptions{CandidatePath: cand, DBPath: f.dbPath, KeyProvider: kp,
		Confirm:                  func(sqlite.CandidateInfo) bool { return true },
		ConfirmAfterPreservation: func(RestorePlan, PreservationInfo) bool { return true },
		testReplaceDatabaseFile:  func(string, string) error { return boom }})
	if err == nil {
		t.Fatal("injected failure not reported")
	}
	if lbl, hit := containsAny(err.Error(), needles); hit {
		t.Errorf("error leaks %s", lbl)
	}
	_ = res
	svc, _ := openSvc(t, f.dbPath, f.keyPath)
	vs, err := svc.ListVersions(ctx, "alpha", "prod", "multi/ver")
	if err != nil || len(vs) != 4 {
		t.Fatalf("live data after failed restore: %d versions, %v (want the original 4)", len(vs), err)
	}
	if v, err := svc.GetValue(ctx, "alpha", "prod", "multi/ver", 4); err != nil || string(v.Plaintext) != s3V1+"-newer" {
		t.Fatalf("newest live version lost: %v", err)
	}
	scanTree(t, "after failed mid-restore", filepath.Dir(f.dbPath), needles)
	for _, p := range listFiles(t, filepath.Dir(f.dbPath)) {
		rel, _ := filepath.Rel(filepath.Dir(f.dbPath), p)
		t.Logf("observation: artefact after failed restore: %s", rel)
		if !s3KnownArtefact(rel) {
			t.Errorf("unexplained leftover after failed restore: %s", rel)
		}
	}
	assertModes(t, filepath.Dir(f.dbPath), "after failed restore")
}

// s3KnownArtefact: names a Restore/Backup is documented (or observed, see the
// characterisation tests) to leave in the data directory.
func s3KnownArtefact(rel string) bool {
	base := filepath.Base(rel)
	switch {
	case base == "c.db", base == "c.db-wal", base == "c.db-shm", base == "c.db-journal":
		return true
	case base == "c.db.central.lock":
		return true
	case base == "mid.db":
		return true
	case strings.HasPrefix(base, "c.db.pre-restore-") && strings.HasSuffix(base, ".bak"): // mandatory safety snapshot
		return true
	}
	return false
}

// TestS3_Restore_RepeatRestoreIsIdempotentOnData: restoring the same backup twice
// onto the same path succeeds both times (documented: restore does not compare
// recency), the data is identical, and each restore adds exactly one restore
// audit event with a verifying chain.
func TestS3_Restore_RepeatRestoreIsIdempotentOnData(t *testing.T) {
	f := newS3Fixture(t)
	want := takeSnapshot(t, f.svc, f.st)
	bpath := f.backupTo(t, "rep.db")
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	db, _, err := restoreFresh(t, bpath, f.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	kp, _ := crypto.NewFileKeyProvider(f.keyPath)
	if _, err := Restore(ctx, RestoreOptions{CandidatePath: bpath, DBPath: db, KeyProvider: kp,
		Confirm:                  func(sqlite.CandidateInfo) bool { return true },
		ConfirmAfterPreservation: func(RestorePlan, PreservationInfo) bool { return true }}); err != nil {
		t.Fatalf("second (repeat) restore: %v", err)
	}
	svc, st := openSvc(t, db, f.keyPath)
	got := takeSnapshot(t, svc, st)
	if !reflect.DeepEqual(want, got) {
		t.Fatal("data differs after repeat restore")
	}
	evs, _ := st.ListAuditEvents(ctx)
	var restores int
	for _, e := range evs {
		if e.EventType == store.AuditRestore {
			restores++
		}
	}
	// the second restore replaces the DB with the backup again, so the first
	// restore's event is displaced; exactly one restore event remains.
	if restores != 1 {
		t.Errorf("restore events after repeat restore = %d, want 1 (restored chain + its own event)", restores)
	}
	if r := sqlite.VerifyAuditLog(ctx, db); r.Status != sqlite.AuditVerified {
		t.Errorf("audit chain after repeat restore: %+v", r)
	}
	assertModes(t, filepath.Dir(filepath.Dir(db)), "after repeat restore")
}

// ---- (c) modes independent of umask -------------------------------------------------------------------------------

// TestS3_FileModes_UmaskIndependent: running the whole lifecycle (key install,
// open, writes, backup, restore into fresh dir, restore over live) under three
// umasks yields the same documented modes. Not parallel: umask is process-wide.
func TestS3_FileModes_UmaskIndependent(t *testing.T) {
	for _, um := range []int{0o022, 0o002, 0o077, 0o000} {
		t.Run(fmt.Sprintf("umask-%04o", um), func(t *testing.T) {
			old := syscall.Umask(um)
			defer syscall.Umask(old)

			f := newS3Fixture(t) // keys/ and data/ are created by DSO itself under this umask
			assertModes(t, f.root, "after create")
			bpath := f.backupTo(t, "um.db")
			assertModes(t, f.root, "after backup")
			if err := f.st.Close(); err != nil {
				t.Fatal(err)
			}
			assertModes(t, f.root, "after close")
			db, _, err := restoreFresh(t, bpath, f.keyPath)
			if err != nil {
				t.Fatal(err)
			}
			assertModes(t, filepath.Dir(filepath.Dir(db)), "after restore into fresh dir")
			kp, _ := crypto.NewFileKeyProvider(f.keyPath)
			if _, err := Restore(ctx, RestoreOptions{CandidatePath: bpath, DBPath: f.dbPath, KeyProvider: kp,
				Confirm:                  func(sqlite.CandidateInfo) bool { return true },
				ConfirmAfterPreservation: func(RestorePlan, PreservationInfo) bool { return true }}); err != nil {
				t.Fatal(err)
			}
			assertModes(t, f.root, "after restore over live DB (incl. safety/preserved copies)")
		})
	}
}
