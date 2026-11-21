package ledger

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func constClock(at time.Time) func(*Config) {
	return func(c *Config) { c.Now = func() time.Time { return at } }
}

var stampT = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

const stamp = "20261111T120000Z"

// ---------------------------------------------------------------- T-U9 corrupt

func TestT_U9_CorruptLedger_PreservedNewLedgerEmpty_KeyKept(t *testing.T) {
	good := func(t *testing.T) []byte {
		dir := stateDir(t)
		return seedLedger(t, dir, s2KeyA, s2Entry("web"))
	}
	base := good(t)
	flip := bytes.Replace(base, []byte(`"web"`), []byte(`"wex"`), 1) // body_sha256 mismatch
	cases := map[string][]byte{
		"garbage":            []byte("not json at all"),
		"truncated":          base[:len(base)/2],
		"checksum mismatch":  flip,
		"duplicate key":      bytes.Replace(base, []byte(`"schema_version":3,`), []byte(`"schema_version":3,"ledger_id":"x",`), 1),
		"unknown field":      bytes.Replace(base, []byte(`"entries"`), []byte(`"surprise":1,"entries"`), 1),
		"empty file":         {},
		"version 1 document": bytes.Replace(base, []byte(`"schema_version":3`), []byte(`"schema_version":1`), 1),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := stateDir(t)
			writeFile(t, filepath.Join(dir, "ledger.json"), content, 0o600)
			fk := newFakeKeys(validKey(s2KeyA))
			s, res, log, _ := openT(t, dir, fk, openOpts{cfg: constClock(stampT)})
			if res.Mode != ModeDurable || res.Reason != ReasonCorrupt {
				t.Fatalf("%+v", res)
			}
			want := filepath.Join(dir, "ledger.json.corrupt-"+stamp)
			if res.Preserved != want {
				t.Fatalf("preserved at %s, want %s", res.Preserved, want)
			}
			got, err := os.ReadFile(want)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("the preserved file must hold the original bytes: %v", err)
			}
			l := s.Snapshot().Ledger
			if len(l.Entries) != 0 || l.HMACKeyID != s2KeyA {
				t.Fatalf("new ledger must be empty and bound to the kept key: %+v", l)
			}
			if fk.calls() != 0 {
				t.Fatal("the existing key is kept: Generate must not be called for a corrupt ledger")
			}
			if _, err := Unmarshal(readLedgerBytes(t, dir)); err != nil {
				t.Fatalf("the new ledger must be persisted: %v", err)
			}
			if !s.PersistenceHealthy() || log.count(EventPreserved) != 1 {
				t.Fatalf("events %v", log.kinds())
			}
		})
	}
}

// ---------------------------------------------------------------- T-U10 newer schema

func TestT_U10_NewerSchema_UntouchedMemoryOnly_NoRotation(t *testing.T) {
	for name, key := range map[string]KeyInspection{
		"matching key": validKey(s2KeyA), "mismatching key": validKey(s2KeyB), "absent key": {State: KeyAbsent},
	} {
		t.Run(name, func(t *testing.T) {
			dir := stateDir(t)
			orig := newerSchemaFile(t, dir)
			fp := dirFingerprint(t, dir)
			fk := newFakeKeys(key)
			s, res, log, _ := openT(t, dir, fk)
			if res.Mode != ModeMemoryOnly || res.Reason != ReasonNewerSchema {
				t.Fatalf("%+v", res)
			}
			if err := recordHint(s, "web"); !errors.Is(err, ErrPersistenceUnavailable) {
				t.Fatalf("no BeginRotation may succeed: %v", err)
			}
			_ = s.updateMemoryOnly(addEntries("web")) // adopt in memory
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if dirFingerprint(t, dir) != fp {
				t.Fatal("the directory changed")
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "ledger.json")); !bytes.Equal(b, orig) {
				t.Fatal("ledger bytes are not identical")
			}
			if fk.calls() != 0 {
				t.Fatal("no key may be generated for a newer schema")
			}
			if s.Health().Healthy || log.count(EventRecovery) != 1 {
				t.Fatalf("gauge 0 and a recovery event expected: %v", log.kinds())
			}
		})
	}
}

// ---------------------------------------------------------------- T-U16 flock

func TestT_U16_SecondWriterRefused_NoPersistence_NoMutation(t *testing.T) {
	dir := stateDir(t)
	fk := newFakeKeys(validKey(s2KeyA))
	first, _, _, _ := openT(t, dir, fk)
	if err := first.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	// A stale temp file must survive the refused second opener: cleanup runs only
	// while holding the lock.
	tmp := filepath.Join(dir, "ledger.json.tmp-0123456789abcdef")
	writeFile(t, tmp, []byte("x"), 0o600)
	fp := dirFingerprint(t, dir)
	second, res, _, _ := openT(t, dir, fk)
	if res.Mode != ModeMemoryOnly || res.Reason != ReasonLocked {
		t.Fatalf("%+v", res)
	}
	if err := second.update(addEntries("db")); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal(err)
	}
	if err := recordHint(second, "web"); err == nil {
		t.Fatal("no ledger-governed mutation for the second writer")
	}
	if dirFingerprint(t, dir) != fp {
		t.Fatal("the refused second writer changed the directory")
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatal("cleanup ran without holding the lock")
	}
	// The first writer is unaffected.
	if err := first.update(addEntries("db")); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- key states (T-U11, T-U27, T-U28)

func TestT_U11_KeyLoss_AbsentAndMismatch_PreservedUniqueNeverOverwritten(t *testing.T) {
	for name, key := range map[string]KeyInspection{
		"key absent": {State: KeyAbsent}, "valid key with another id": validKey(s2KeyB),
	} {
		t.Run(name, func(t *testing.T) {
			dir := stateDir(t)
			orig := seedLedger(t, dir, s2KeyA, s2Entry("web"))
			origID := mustLoad(t, orig).LedgerID
			fk := newFakeKeys(key)
			fk.genID = "k-cccccccccccccccc"
			s, res, log, _ := openT(t, dir, fk, openOpts{cfg: constClock(stampT)})
			wantReason := ReasonKeyMissing
			if key.State == KeyValid {
				wantReason = ReasonKeyMismatch
			}
			if res.Reason != wantReason || res.Mode != ModeDurable {
				t.Fatalf("%+v", res)
			}
			if fk.calls() != 1 {
				t.Fatalf("Generate calls %d, want 1", fk.calls())
			}
			p := filepath.Join(dir, "ledger.json.keyloss-"+stamp)
			if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, orig) {
				t.Fatalf("the old ledger must be preserved byte for byte: %v", err)
			}
			l := s.Snapshot().Ledger
			if l.LedgerID == origID || len(l.Entries) != 0 || l.HMACKeyID != "k-cccccccccccccccc" {
				t.Fatalf("new ledger must be empty (adopt), new id, bound to the new key: %+v", l)
			}
			if log.count(EventPreserved) != 1 {
				t.Fatal("preserved event expected")
			}
			// A later run leaves the preserved file untouched.
			_ = s.Close()
			s2, _, _, _ := openT(t, dir, fk, openOpts{cfg: constClock(stampT)})
			_ = s2
			if b, _ := os.ReadFile(p); !bytes.Equal(b, orig) {
				t.Fatal("the preserved file changed in a later run")
			}
		})
	}
}

func mustLoad(t *testing.T, b []byte) *Ledger {
	t.Helper()
	l, err := Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestT_U26_PreservedNames_NeverOverwriteExisting_CollisionGetsSuffix(t *testing.T) {
	dir := stateDir(t)
	orig := seedLedger(t, dir, s2KeyA, s2Entry("web"))
	taken := filepath.Join(dir, "ledger.json.keyloss-"+stamp)
	writeFile(t, taken, []byte("precious earlier evidence"), 0o600)
	fk := newFakeKeys(validKey(s2KeyB))
	_, res, _, _ := openT(t, dir, fk, openOpts{cfg: constClock(stampT)})
	if b, _ := os.ReadFile(taken); string(b) != "precious earlier evidence" {
		t.Fatal("an existing preserved file was overwritten")
	}
	if !strings.HasPrefix(res.Preserved, taken+"-") || len(res.Preserved) != len(taken)+5 {
		t.Fatalf("collision must add a -<4 hex> suffix: %s", res.Preserved)
	}
	if b, err := os.ReadFile(res.Preserved); err != nil || !bytes.Equal(b, orig) {
		t.Fatal("the ledger was not preserved under the suffixed name")
	}
}

func TestT_U26_Precedence(t *testing.T) {
	type tc struct {
		name     string
		prepare  func(t *testing.T, dir string)
		key      KeyInspection
		hold     bool // a second store holds the lock
		reason   Reason
		untouch  bool
		genCalls int
	}
	corrupt := func(t *testing.T, dir string) {
		writeFile(t, filepath.Join(dir, "ledger.json"), []byte("{broken"), 0o600)
	}
	cases := []tc{
		{"lock held beats unsafe key", corrupt, KeyInspection{State: KeyUnsafe, Reason: "mode 644"}, true, ReasonLocked, true, 0},
		{"unsafe key beats newer schema", func(t *testing.T, d string) { newerSchemaFile(t, d) }, KeyInspection{State: KeyUnsafe, Reason: "symlink"}, false, ReasonKeyUnsafe, true, 0},
		{"unsafe key beats corrupt", corrupt, KeyInspection{State: KeyUnsafe, Reason: "owner"}, false, ReasonKeyUnsafe, true, 0},
		{"unsafe key with no ledger", func(*testing.T, string) {}, KeyInspection{State: KeyUnsafe, Reason: "fifo"}, false, ReasonKeyUnsafe, true, 0},
		{"malformed key beats corrupt", corrupt, KeyInspection{State: KeyMalformed, Reason: "bad base64"}, false, ReasonKeyMalformed, true, 0},
		{"malformed key with no ledger", func(*testing.T, string) {}, KeyInspection{State: KeyMalformed, Reason: "length"}, false, ReasonKeyMalformed, true, 0},
		{"newer schema beats key mismatch", func(t *testing.T, d string) { newerSchemaFile(t, d) }, validKey(s2KeyB), false, ReasonNewerSchema, true, 0},
		{"newer schema beats absent key", func(t *testing.T, d string) { newerSchemaFile(t, d) }, KeyInspection{State: KeyAbsent}, false, ReasonNewerSchema, true, 0},
		{"unparseable is corrupt, not key loss", corrupt, KeyInspection{State: KeyAbsent}, false, ReasonCorrupt, false, 1},
		{"unreadable ledger beats key loss", func(t *testing.T, d string) {
			writeFile(t, filepath.Join(d, "ledger.json"), []byte("{}"), 0o644) // group/other readable: unsafe
		}, KeyInspection{State: KeyAbsent}, false, ReasonLedgerUnreadable, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := stateDir(t)
			c.prepare(t, dir)
			fk := newFakeKeys(c.key)
			if c.hold {
				holder, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
				_ = holder
			}
			fp := dirFingerprint(t, dir)
			s, res, _, _ := openT(t, dir, fk)
			if res.Reason != c.reason {
				t.Fatalf("reason %q, want %q (%s)", res.Reason, c.reason, res.Detail)
			}
			if fk.calls() != c.genCalls {
				t.Fatalf("Generate calls %d, want %d", fk.calls(), c.genCalls)
			}
			if c.untouch {
				if res.Mode != ModeMemoryOnly {
					t.Fatalf("mode %s", res.Mode)
				}
				if c.hold {
					fp = dirFingerprint(t, dir)
				}
				if got := dirFingerprint(t, dir); got != fp {
					t.Fatal("an object was touched")
				}
				if s.PersistenceHealthy() {
					t.Fatal("persistence must be reported unhealthy")
				}
			}
		})
	}
}

func TestT_U27_T_U28_UnsafeAndMalformedKey_FailClosed_NeverGenerate(t *testing.T) {
	states := map[string]KeyInspection{
		"unsafe symlink":     {State: KeyUnsafe, Reason: "key is a symbolic link"},
		"unsafe permissions": {State: KeyUnsafe, Reason: "group/other bits"},
		"unsafe owner":       {State: KeyUnsafe, Reason: "owner not root"},
		"malformed header":   {State: KeyMalformed, Reason: "wrong header line"},
		"malformed length":   {State: KeyMalformed, Reason: "decoded length is not 32 bytes"},
	}
	for name, ki := range states {
		for _, withLedger := range []bool{false, true} {
			t.Run(name+map[bool]string{true: ", ledger present", false: ", no ledger"}[withLedger], func(t *testing.T) {
				dir := stateDir(t)
				if withLedger {
					seedLedger(t, dir, s2KeyA, s2Entry("web"))
				}
				fp := dirFingerprint(t, dir)
				fk := newFakeKeys(ki)
				s, res, _, _ := openT(t, dir, fk)
				want := ReasonKeyUnsafe
				if ki.State == KeyMalformed {
					want = ReasonKeyMalformed
				}
				if res.Mode != ModeMemoryOnly || res.Reason != want || res.Detail != ki.Reason {
					t.Fatalf("%+v", res)
				}
				if fk.calls() != 0 {
					t.Fatal("Generate must never be called for an unsafe or malformed key")
				}
				if dirFingerprint(t, dir) != fp {
					t.Fatal("a fail-closed start must leave every object untouched")
				}
				if err := s.update(addEntries("x")); !errors.Is(err, ErrPersistenceUnavailable) {
					t.Fatal(err)
				}
				if err := s.updateMemoryOnly(addEntries("x")); err != nil {
					t.Fatalf("observations may be adopted in memory: %v", err)
				}
				if dirFingerprint(t, dir) != fp {
					t.Fatal("memory-only adoption touched the disk")
				}
			})
		}
	}
}

func TestS2_KeyGenerateFailure_AfterPreservation_MemoryOnly(t *testing.T) {
	dir := stateDir(t)
	orig := seedLedger(t, dir, s2KeyA, s2Entry("web"))
	fk := newFakeKeys(validKey(s2KeyB))
	fk.genErr = errors.New("disk full")
	s, res, _, _ := openT(t, dir, fk, openOpts{cfg: constClock(stampT)})
	if res.Mode != ModeMemoryOnly || res.Reason != ReasonKeyGenerateFailed {
		t.Fatalf("%+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "ledger.json.keyloss-"+stamp)); err != nil || !bytes.Equal(b, orig) {
		t.Fatal("the old ledger must remain available under its preserved name")
	}
	if err := s.update(addEntries("x")); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- Option B (empty hmac_key_id)

func emptyKeyIDFile(t *testing.T, dir string) []byte {
	t.Helper()
	l := &Ledger{SchemaVersion: SchemaVersion, LedgerID: "0123456789abcdef0123456789abcdef", Generation: 7, CreatedAt: stampT, UpdatedAt: stampT,
		Entries: []Entry{s2Entry("web")}}
	b, err := Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ledger.json"), b, 0o600)
	return b
}

func TestS2_OptionB_EmptyKeyID_NoHMACIdentity_AdoptsValidKey(t *testing.T) {
	dir := stateDir(t)
	emptyKeyIDFile(t, dir)
	fk := newFakeKeys(validKey(s2KeyA))
	s, res, _, _ := openT(t, dir, fk)
	if res.Mode != ModeDurable || res.Reason != ReasonKeyAdopted || res.Preserved != "" {
		t.Fatalf("%+v", res)
	}
	l := s.Snapshot().Ledger
	if l.HMACKeyID != s2KeyA || l.LedgerID != "0123456789abcdef0123456789abcdef" || len(l.Entries) != 1 || l.Generation != 8 {
		t.Fatalf("the ledger must be kept and its key id adopted: %+v", l)
	}
	if fk.calls() != 0 || len(preservedNames(t, dir, "keyloss")) != 0 {
		t.Fatal("adoption is not key loss: no Generate, no preservation")
	}
	if mustLoad(t, readLedgerBytes(t, dir)).HMACKeyID != s2KeyA {
		t.Fatal("the adopted key id must be persisted")
	}
}

// hmacLedgerBytes builds the on-disk bytes of an otherwise valid document that
// holds one structurally valid HMAC identity (a base64url, unpadded 32-byte
// MAC) under the given header hmac_key_id. Marshal would refuse the empty-header
// variant, so the checksum is computed with ComputeBodySHA256 and the bytes are
// produced with the internal encoder, which does not validate.
func hmacLedgerBytes(t *testing.T, header string) []byte {
	t.Helper()
	mac := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	e := s2Entry("web")
	e.Applied = &Version{Kind: KindHMAC, ID: mac, KeyID: s2KeyA}
	l := &Ledger{SchemaVersion: SchemaVersion, LedgerID: "0123456789abcdef0123456789abcdef", Generation: 7,
		HMACKeyID: header, CreatedAt: stampT, UpdatedAt: stampT, Entries: []Entry{e}}
	sum, err := ComputeBodySHA256(l)
	if err != nil {
		t.Fatal(err)
	}
	l.BodySHA256 = sum
	b, err := encode(l)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Pinned outcome (owner decision F): an on-disk ledger whose hmac_key_id is
// empty but which holds an HMAC identity is not a valid document, so Open takes
// the corrupt-file path. It is never adopted and never treated as key mismatch.
func TestS2_OptionB_HMACIdentityWithEmptyHeader_FollowsCorruptPath(t *testing.T) {
	// The in-memory search still finds the identity (defensive coverage).
	if !hasHMACIdentity(&Ledger{Entries: []Entry{{Applied: &Version{Kind: KindHMAC, ID: "x", KeyID: s2KeyA}}}}) {
		t.Fatal("an applied hmac identity must be found")
	}
	if hasHMACIdentity(&Ledger{Entries: []Entry{{Applied: &Version{Kind: KindProvider, ID: "x"}}}}) {
		t.Fatal("a provider identity is not key dependent")
	}

	bad := hmacLedgerBytes(t, "")

	// Unmarshal rejects exactly this, for exactly this reason.
	_, err := Unmarshal(bad)
	var sv *SchemaVersionError
	switch {
	case err == nil:
		t.Fatal("an empty header with an hmac identity must be rejected")
	case !errors.Is(err, ErrInvalid) || errors.Is(err, ErrChecksum) || errors.As(err, &sv):
		t.Fatalf("want a plain validation failure, got %v", err)
	case !strings.Contains(err.Error(), "hmac key id differs from the ledger's hmac_key_id"):
		t.Fatalf("rejected for the wrong reason: %v", err)
	}

	// Negative control: the same document with a matching non-empty header is
	// valid, so the empty header is the only defect above.
	if l, err := Unmarshal(hmacLedgerBytes(t, s2KeyA)); err != nil || l.HMACKeyID != s2KeyA {
		t.Fatalf("the control document must load: %v", err)
	}

	dir := stateDir(t)
	writeFile(t, filepath.Join(dir, "ledger.json"), bad, 0o600)
	fk := newFakeKeys(validKey(s2KeyA))
	s, res, log, _ := openT(t, dir, fk, openOpts{cfg: constClock(stampT)})

	if res.Reason != ReasonCorrupt || res.Mode != ModeDurable {
		t.Fatalf("want exactly ReasonCorrupt in durable mode, got %+v", res)
	}
	wantPreserved := filepath.Join(dir, "ledger.json.corrupt-"+stamp)
	if res.Preserved != wantPreserved {
		t.Fatalf("preserved at %q, want %q", res.Preserved, wantPreserved)
	}
	if b, err := os.ReadFile(wantPreserved); err != nil || !bytes.Equal(b, bad) {
		t.Fatalf("the original bytes must be preserved unchanged: %v", err)
	}
	if got := preservedNames(t, dir, "keyloss"); len(got) != 0 {
		t.Fatalf("no .keyloss- preservation on the corrupt path: %v", got)
	}
	if fk.calls() != 0 {
		t.Fatal("Generate must not be called on the corrupt path")
	}
	if got := fk.Inspect(); got != validKey(s2KeyA) {
		t.Fatalf("the existing key must be unchanged: %+v", got)
	}
	l := s.Snapshot().Ledger
	if res.KeyID != s2KeyA || l.HMACKeyID != s2KeyA || len(l.Entries) != 0 || l.LedgerID == "0123456789abcdef0123456789abcdef" {
		t.Fatalf("corrupt recovery starts a new empty ledger bound to the kept key: %+v", l)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	var preserved, recovered int
	for _, e := range log.ev {
		switch e.Kind {
		case EventPreserved:
			preserved++
			if e.Reason != ReasonCorrupt {
				t.Fatalf("preserved event reason %q", e.Reason)
			}
		case EventRecovery:
			recovered++
			if e.Reason != ReasonCorrupt {
				t.Fatalf("recovery event reason %q (must not be key mismatch or key adopted)", e.Reason)
			}
		}
	}
	if preserved != 1 || recovered != 1 {
		t.Fatalf("events: %d preserved, %d recovery", preserved, recovered)
	}
}

// walkers must see every HMAC identity wherever it sits in the schema,
// including in records a later schema might add to the recovery journal.
func TestS2_HMACWalker_CoversAnyPlaceAVersionCanAppear(t *testing.T) {
	h := Version{Kind: KindHMAC, ID: "a", KeyID: s2KeyA}
	type future struct {
		ByName map[string][]*Version
		Nested struct{ Deep []struct{ V *Version } }
	}
	f := future{ByName: map[string][]*Version{"k": {&h}}}
	if !hasHMACIdentity(f) {
		t.Fatal("map/slice/pointer coverage")
	}
	var g future
	g.Nested.Deep = []struct{ V *Version }{{V: &h}}
	if !hasHMACIdentity(&g) {
		t.Fatal("nested slice of struct coverage")
	}
	if hasHMACIdentity(future{}) || hasHMACIdentity(nil) || hasHMACIdentity((*Ledger)(nil)) {
		t.Fatal("empty values contain no identity")
	}
	// Positions in the real schema: each is detected.
	pos := map[string]func(*Ledger){
		"desired": func(l *Ledger) { l.Entries[0].Desired = &h },
		"applied": func(l *Ledger) { l.Entries[0].Applied = &h },
		"replica": func(l *Ledger) { l.Entries[0].Replicas = []Replica{{Applied: &h}} },
	}
	for name, set := range pos {
		l := &Ledger{Entries: []Entry{{}}}
		set(l)
		if !hasHMACIdentity(l) {
			t.Errorf("%s: not found", name)
		}
	}
}

// Schema guard (schema 3: no intent, no recovery journal). If a later
// schema adds one the walker still finds it (test above), but this guard makes
// the change visible so the owner decision on journal coverage is revisited.
func TestS2_SchemaGuard_VersionPositions(t *testing.T) {
	var paths []string
	var walk func(path string, ty reflect.Type, seen map[reflect.Type]bool)
	walk = func(path string, ty reflect.Type, seen map[reflect.Type]bool) {
		for ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Slice {
			ty = ty.Elem()
		}
		if ty == versionType {
			paths = append(paths, path)
			return
		}
		if ty.Kind() != reflect.Struct || ty == reflect.TypeOf(time.Time{}) || seen[ty] {
			return
		}
		seen[ty] = true
		for i := 0; i < ty.NumField(); i++ {
			walk(path+"."+ty.Field(i).Name, ty.Field(i).Type, seen)
		}
		delete(seen, ty)
	}
	walk("Ledger", reflect.TypeOf(Ledger{}), map[reflect.Type]bool{})
	want := []string{
		"Ledger.Entries.Desired", "Ledger.Entries.Applied", "Ledger.Entries.Replicas.Applied",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("Version positions changed:\n got %v\nwant %v\nrevisit hasHMACIdentity coverage (owner decision: all persisted HMAC identities, including recovery-journal records)", paths, want)
	}
	for _, p := range paths {
		if strings.Contains(p, "Recoveries") {
			t.Fatalf("a recovery record now holds a Version (%s): journal coverage needs an explicit decision", p)
		}
	}
}

// ---------------------------------------------------------------- preservation (D-S2-4)

func TestS2_Preserve_NeverUsesOrdinaryRename(t *testing.T) {
	dir := stateDir(t)
	seedLedger(t, dir, s2KeyA, s2Entry("web"))
	var bad atomic.Int32
	openT(t, dir, newFakeKeys(validKey(s2KeyB)), openOpts{cfg: constClock(stampT), ops: func(o *fileOps) {
		real := o.rename
		o.rename = func(a, b string) error {
			if strings.Contains(b, ".keyloss-") || strings.Contains(b, ".corrupt-") {
				bad.Add(1)
			}
			return real(a, b)
		}
	}})
	if bad.Load() != 0 {
		t.Fatal("preservation used an ordinary (replacing) rename")
	}
}

func TestS2_Preserve_CollisionsExhausted_FailClosed(t *testing.T) {
	dir := stateDir(t)
	orig := seedLedger(t, dir, s2KeyA, s2Entry("web"))
	fp := dirFingerprint(t, dir)
	var calls atomic.Int32
	fk := newFakeKeys(validKey(s2KeyB))
	s, res, _, _ := openT(t, dir, fk, openOpts{ops: func(o *fileOps) {
		o.renameNoReplace = func(a, b string) error { calls.Add(1); return syscall.EEXIST }
	}})
	if res.Mode != ModeMemoryOnly || res.Reason != ReasonPreserveFailed || !strings.Contains(res.Detail, ErrPreserveExhausted.Error()) {
		t.Fatalf("%+v", res)
	}
	if calls.Load() != maxPreserveAttempts {
		t.Fatalf("collision retries %d, want the bound %d", calls.Load(), maxPreserveAttempts)
	}
	if fk.calls() != 0 || dirFingerprint(t, dir) != fp {
		t.Fatal("exhaustion must leave ledger and key untouched and not generate a key")
	}
	if b := readLedgerBytes(t, dir); !bytes.Equal(b, orig) {
		t.Fatal("ledger changed")
	}
	if err := s.update(addEntries("x")); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal(err)
	}
}

func TestS2_Preserve_UnsupportedOrFailing_FailClosed(t *testing.T) {
	for name, e := range map[string]error{
		"unsupported platform": ErrUnsupportedPlatform, "EINVAL": syscall.EINVAL, "ENOSYS": syscall.ENOSYS,
		"ENOTSUP": syscall.ENOTSUP, "EXDEV": syscall.EXDEV, "EACCES": syscall.EACCES,
	} {
		t.Run(name, func(t *testing.T) {
			dir := stateDir(t)
			seedLedger(t, dir, s2KeyA, s2Entry("web"))
			fp := dirFingerprint(t, dir)
			fk := newFakeKeys(validKey(s2KeyB))
			var calls atomic.Int32
			s, res, _, _ := openT(t, dir, fk, openOpts{ops: func(o *fileOps) {
				o.renameNoReplace = func(a, b string) error { calls.Add(1); return e }
			}})
			if res.Mode != ModeMemoryOnly || res.Reason != ReasonPreserveFailed {
				t.Fatalf("%+v", res)
			}
			if calls.Load() != 1 {
				t.Fatalf("a non-collision failure must not be retried blindly: %d calls", calls.Load())
			}
			if fk.calls() != 0 || dirFingerprint(t, dir) != fp {
				t.Fatal("ledger and key must stay untouched")
			}
			if err := recordHint(s, "web"); err == nil {
				t.Fatal("no ledger-governed Docker mutation")
			}
			if err := s.updateMemoryOnly(addEntries("obs")); err != nil {
				t.Fatalf("memory-only observation must be permitted: %v", err)
			}
		})
	}
}

func TestS2_Preserve_Interrupted(t *testing.T) {
	t.Run("renamed then EINTR: resolved by identity, no retry", func(t *testing.T) {
		dir := stateDir(t)
		orig := seedLedger(t, dir, s2KeyA, s2Entry("web"))
		var calls atomic.Int32
		_, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyB)), openOpts{cfg: constClock(stampT), ops: func(o *fileOps) {
			real := o.renameNoReplace
			o.renameNoReplace = func(a, b string) error {
				calls.Add(1)
				if err := real(a, b); err != nil {
					return err
				}
				return syscall.EINTR
			}
		}})
		if calls.Load() != 1 || res.Preserved == "" || res.Mode != ModeDurable {
			t.Fatalf("calls %d, %+v", calls.Load(), res)
		}
		if b, _ := os.ReadFile(res.Preserved); !bytes.Equal(b, orig) {
			t.Fatal("content")
		}
	})
	t.Run("not renamed, EINTR once: established and retried", func(t *testing.T) {
		dir := stateDir(t)
		seedLedger(t, dir, s2KeyA, s2Entry("web"))
		var calls atomic.Int32
		_, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyB)), openOpts{cfg: constClock(stampT), ops: func(o *fileOps) {
			real := o.renameNoReplace
			o.renameNoReplace = func(a, b string) error {
				if calls.Add(1) == 1 {
					return syscall.EINTR
				}
				return real(a, b)
			}
		}})
		if calls.Load() != 2 || res.Preserved == "" {
			t.Fatalf("calls %d, %+v", calls.Load(), res)
		}
	})
	t.Run("outcome cannot be established: no blind retry, fail closed", func(t *testing.T) {
		dir := stateDir(t)
		seedLedger(t, dir, s2KeyA, s2Entry("web"))
		fp := dirFingerprint(t, dir)
		var calls atomic.Int32
		var interrupted atomic.Bool
		fk := newFakeKeys(validKey(s2KeyB))
		_, res, _, _ := openT(t, dir, fk, openOpts{ops: func(o *fileOps) {
			realL := o.lstat
			o.renameNoReplace = func(a, b string) error { calls.Add(1); interrupted.Store(true); return syscall.EINTR }
			o.lstat = func(p string) (os.FileInfo, error) {
				if interrupted.Load() {
					return nil, syscall.EIO // identity cannot be read
				}
				return realL(p)
			}
		}})
		if calls.Load() != 1 || res.Reason != ReasonPreserveFailed || !strings.Contains(res.Detail, ErrPreserveAmbiguous.Error()) {
			t.Fatalf("calls %d, %+v", calls.Load(), res)
		}
		if fk.calls() != 0 || dirFingerprint(t, dir) != fp {
			t.Fatal("untouched expected")
		}
	})
	t.Run("EINTR forever: bounded", func(t *testing.T) {
		dir := stateDir(t)
		seedLedger(t, dir, s2KeyA, s2Entry("web"))
		var calls atomic.Int32
		_, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyB)), openOpts{ops: func(o *fileOps) {
			o.renameNoReplace = func(a, b string) error { calls.Add(1); return syscall.EINTR }
		}})
		if calls.Load() > maxInterruptedRetries+1 || res.Reason != ReasonPreserveFailed {
			t.Fatalf("calls %d, %+v", calls.Load(), res)
		}
	})
}

func TestS2_RealRenameNoReplace_Kernel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("kernel behaviour is tested on Linux only; Darwin is compile-checked")
	}
	dir := stateDir(t)
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	writeFile(t, a, []byte("A"), 0o600)
	writeFile(t, b, []byte("B"), 0o600)
	err := renameNoReplace(a, b)
	if !errors.Is(err, fs.ErrExist) {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOSYS) {
			t.Skipf("filesystem lacks RENAME_NOREPLACE: %v", err)
		}
		t.Fatalf("want EEXIST, got %v", err)
	}
	if x, _ := os.ReadFile(a); string(x) != "A" {
		t.Fatal("source changed")
	}
	if x, _ := os.ReadFile(b); string(x) != "B" {
		t.Fatal("destination was replaced")
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(a, filepath.Join(dir, "d")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("an existing empty directory must also block: %v", err)
	}
	if err := renameNoReplace(a, filepath.Join(dir, "c")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(a); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("source must be gone after success")
	}
}

// ---------------------------------------------------------------- stale temp cleanup (D-S2-5)

func TestS2_StaleTemps_OnlyExactOwnedRegularFilesRemoved(t *testing.T) {
	dir := stateDir(t)
	seedLedger(t, dir, s2KeyA, s2Entry("web"))
	foreign := filepath.Join(dir, "ledger.json.tmp-aaaaaaaaaaaaaaaa")
	remove := []string{"ledger.json.tmp-0123456789abcdef", "ledger.json.tmp-ffffffffffffffff"}
	keep := []string{
		"ledger.json.tmp-0123456789abcde",   // 15 hex
		"ledger.json.tmp-0123456789abcdef0", // 17 hex
		"ledger.json.tmp-0123456789ABCDEF",  // uppercase
		"ledger.json.tmp-0123456789abcdeg",  // not hex
		"ledger.json.tmp-",
		"xledger.json.tmp-0123456789abcdef",
		"ledger.json.tmp-0123456789abcdef.bak",
		"ledger.json.corrupt-20260101T000000Z",
		"ledger.json.keyloss-20260101T000000Z",
		"ledger.json.pre-v1",
		"ledger.json.corrupt-20260101T000000Z-ab12",
		"unrelated.txt",
	}
	for _, n := range append(append([]string{}, remove...), keep...) {
		writeFile(t, filepath.Join(dir, n), []byte(n), 0o600)
	}
	writeFile(t, foreign, []byte("foreign"), 0o600)
	// A symlink with a matching name, a directory with a matching name and a FIFO.
	outside := filepath.Join(dir, "outside-target")
	writeFile(t, outside, []byte("target"), 0o600)
	link := filepath.Join(dir, "ledger.json.tmp-1111111111111111")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	dirName := filepath.Join(dir, "ledger.json.tmp-2222222222222222")
	if err := os.Mkdir(dirName, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "ledger.json.tmp-3333333333333333")
	if err := mkfifo(fifo); err != nil {
		t.Fatal(err)
	}
	_, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: func(o *fileOps) {
		realOwner := o.ownerUID
		o.ownerUID = func(fi os.FileInfo) (uint32, bool) { // a foreign uid for one named file
			uid, ok := realOwner(fi)
			if fi.Name() == "ledger.json.tmp-aaaaaaaaaaaaaaaa" {
				return uid + 1, ok
			}
			return uid, ok
		}
	}})
	for _, n := range remove {
		if _, err := os.Lstat(filepath.Join(dir, n)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s should have been removed", n)
		}
	}
	for _, n := range append(keep, "ledger.json.tmp-aaaaaaaaaaaaaaaa", "ledger.json.tmp-1111111111111111", "ledger.json.tmp-2222222222222222", "ledger.json.tmp-3333333333333333", "outside-target") {
		if _, err := os.Lstat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s must not be removed: %v", n, err)
		}
	}
	if b, _ := os.ReadFile(outside); string(b) != "target" {
		t.Fatal("a symlink was followed")
	}
	if len(res.RemovedTemps) != len(remove) {
		t.Fatalf("removed %v", res.RemovedTemps)
	}
}

func TestS2_StaleTemps_RealForeignOwner_RootOnly(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing a file's owner needs root; the foreign-UID rule is covered by the injected ownerUID test")
	}
	dir := stateDir(t)
	p := filepath.Join(dir, "ledger.json.tmp-0123456789abcdef")
	writeFile(t, p, []byte("x"), 0o600)
	if err := os.Chown(p, 12345, 12345); err != nil {
		t.Skip(err)
	}
	openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if _, err := os.Lstat(p); err != nil {
		t.Fatal("a file owned by another uid must not be removed")
	}
}

func TestS2_StaleTemps_NotRemovedInRefusedModes(t *testing.T) {
	// Unsafe directory (group/other writable): the chain check fails first.
	dir := stateDir(t)
	tmp := filepath.Join(dir, "ledger.json.tmp-0123456789abcdef")
	writeFile(t, tmp, []byte("x"), 0o600)
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	_, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if res.Reason != ReasonUnsafeDir {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatal("cleanup must run only after the directory-chain check")
	}
}

func TestS2_Cleanup_RemovesOwnTempAfterFailedWrite_NotOthers(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() && op == "fsync" {
			return syscall.EIO
		}
		return nil
	})})
	other := filepath.Join(dir, "ledger.json.tmp-0123456789abcdef") // another process's, not ours
	writeFile(t, other, []byte("x"), 0o600)
	armed.Store(true)
	_ = s.update(addEntries("web"))
	if _, err := os.Lstat(other); err != nil {
		t.Fatal("a failed write removed a temp file it did not create")
	}
	if len(tempFiles(t, dir)) != 1 {
		t.Fatalf("only the foreign file should remain: %v", tempFiles(t, dir))
	}
}

// ---------------------------------------------------------------- symlink / special-file protection

func TestS2_SpecialLedgerObjects_FailClosedUntouched(t *testing.T) {
	mk := map[string]func(t *testing.T, dir string){
		"symlink to a valid ledger": func(t *testing.T, dir string) {
			real := filepath.Join(dir, "real-ledger")
			good := seedLedger(t, dir, s2KeyA, s2Entry("web"))
			if err := os.Rename(filepath.Join(dir, "ledger.json"), real); err != nil {
				t.Fatal(err)
			}
			_ = good
			if err := os.Symlink(real, filepath.Join(dir, "ledger.json")); err != nil {
				t.Fatal(err)
			}
		},
		"dangling symlink": func(t *testing.T, dir string) {
			if err := os.Symlink("/nonexistent-target", filepath.Join(dir, "ledger.json")); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "ledger.json"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"fifo": func(t *testing.T, dir string) {
			if err := mkfifo(filepath.Join(dir, "ledger.json")); err != nil {
				t.Fatal(err)
			}
		},
		"group-readable file": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "ledger.json"), []byte("{}"), 0o640)
		},
	}
	for name, setup := range mk {
		t.Run(name, func(t *testing.T) {
			dir := stateDir(t)
			setup(t, dir)
			fp := dirFingerprint(t, dir)
			fk := newFakeKeys(validKey(s2KeyA))
			done := make(chan struct{})
			var res OpenResult
			var s *Store
			go func() { s, res, _, _ = openT(t, dir, fk); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Open blocked (a FIFO must never block it)")
			}
			if res.Mode != ModeMemoryOnly || res.Reason != ReasonLedgerUnreadable {
				t.Fatalf("%+v", res)
			}
			if fk.calls() != 0 || dirFingerprint(t, dir) != fp {
				t.Fatal("an unreadable ledger must leave everything untouched (no preserve-and-restart)")
			}
			if err := s.update(addEntries("x")); !errors.Is(err, ErrPersistenceUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

func TestS2_LockFileSymlink_Refused(t *testing.T) {
	dir := stateDir(t)
	victim := filepath.Join(dir, "victim")
	writeFile(t, victim, []byte("keep"), 0o600)
	if err := os.Symlink(victim, filepath.Join(dir, "ledger.lock")); err != nil {
		t.Fatal(err)
	}
	_, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if res.Mode != ModeMemoryOnly {
		t.Fatalf("%+v", res)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatal("the symlink target was modified")
	}
}

func TestS2_UnsupportedPlatformOps_FailClosed(t *testing.T) {
	dir := stateDir(t)
	seedLedger(t, dir, s2KeyA, s2Entry("web"))
	fp := dirFingerprint(t, dir)
	fk := newFakeKeys(validKey(s2KeyA))
	s, res, _, _ := openT(t, dir, fk, openOpts{ops: func(o *fileOps) {
		o.lock = func(string) (func() error, error) { return nil, ErrUnsupportedPlatform }
	}})
	if res.Mode != ModeMemoryOnly || dirFingerprint(t, dir) != fp || fk.calls() != 0 {
		t.Fatalf("%+v", res)
	}
	if err := s.update(addEntries("x")); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- manual redelivery requests (T-U32, S2 part)

func entryWithDesired(t *testing.T, s *Store, svc string) Key {
	t.Helper()
	if err := s.update(addEntries(svc)); err != nil {
		t.Fatal(err)
	}
	return Key{Service: svc, Provider: "p", Secret: "s"}
}

func TestT_U32_Request_PersistedFirst_PersistenceFailureRecordsNothing(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() && op == "rename" {
			return syscall.EIO
		}
		return nil
	})})
	k := entryWithDesired(t, s, "web")
	armed.Store(true)
	id, err := s.RequestRedelivery(k, "alice", "stuck")
	if err == nil || id != "" || !errors.Is(err, ErrPersistenceFailed) {
		t.Fatalf("a request that cannot be persisted must fail: %q %v", id, err)
	}
	if e, _ := s.Get(k); e.Request != nil {
		t.Fatal("nothing may be recorded or dispatched (503 path)")
	}
	armed.Store(false)
	id, err = s.RequestRedelivery(k, "alice", "stuck")
	if err != nil || len(id) != 32 {
		t.Fatalf("%q %v", id, err)
	}
	disk := mustLoad(t, readLedgerBytes(t, dir))
	if disk.Entries[0].Request == nil || disk.Entries[0].Request.RequestID != id {
		t.Fatal("the request must be durable when the call returns")
	}
}

func TestT_U32_Request_Identity_Coalescing_TerminalReplaced_AppliedUntouched(t *testing.T) {
	dir := stateDir(t)
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	k := entryWithDesired(t, s, "web")
	before, _ := s.Get(k)
	id1, err := s.RequestRedelivery(k, "alice", "r1")
	if err != nil {
		t.Fatal(err)
	}
	gen := s.Snapshot().Ledger.Generation
	id2, err := s.RequestRedelivery(k, "bob", "r2") // double click
	if err != nil || id2 != id1 {
		t.Fatalf("a duplicate must coalesce to the same id: %q vs %q (%v)", id2, id1, err)
	}
	if s.Snapshot().Ledger.Generation != gen {
		t.Fatal("a coalesced request must not write")
	}
	e, _ := s.Get(k)
	if e.Request.TargetSeq != before.DesiredSeq || e.Request.State != RedeliveryRequested || e.Request.Actor != "alice" {
		t.Fatalf("%+v", e.Request)
	}
	if !reflect.DeepEqual(e.Applied, before.Applied) || !reflect.DeepEqual(e.Desired, before.Desired) || e.State != before.State {
		t.Fatal("a request must never change applied, desired or the entry state")
	}
	// Terminal request: replaced by a new one (one slot, no history).
	if err := s.update(func(l *Ledger) error {
		r := &findEntry(l, k).Request
		(*r).State, (*r).RotationID = RedeliverySucceeded, "55555555555555555555555555555555"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id3, err := s.RequestRedelivery(k, "carol", "r3")
	if err != nil || id3 == id1 {
		t.Fatalf("a terminal request is replaced by a new id: %q %v", id3, err)
	}
	if e, _ := s.Get(k); e.Request.RequestID != id3 || e.Request.State != RedeliveryRequested {
		t.Fatal("slot not replaced")
	}
}

func TestT_U32_Request_Errors_And_MemoryOnly(t *testing.T) {
	s, _, _, _ := openT(t, stateDir(t), newFakeKeys(validKey(s2KeyA)))
	if _, err := s.RequestRedelivery(Key{Service: "none"}, "a", "r"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.update(func(l *Ledger) error { l.Entries = append(l.Entries, idleEntry("", "nodesired", "p", "s")); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRedelivery(Key{Service: "nodesired", Provider: "p", Secret: "s"}, "a", "r"); !errors.Is(err, ErrNoDesired) {
		t.Fatal(err)
	}
	dir := stateDir(t)
	newerSchemaFile(t, dir)
	m, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if _, err := m.RequestRedelivery(Key{Service: "web"}, "a", "r"); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal(err)
	}
}

func TestT_U32_Request_RestartCancelsUndispatched_DispatchedFollowsIntentRules(t *testing.T) {
	dir := stateDir(t)
	fk := newFakeKeys(validKey(s2KeyA))
	s, _, _, _ := openT(t, dir, fk)
	ka := entryWithDesired(t, s, "undispatched")
	kb := entryWithDesired(t, s, "dispatched")
	if _, err := s.RequestRedelivery(ka, "alice", "operator reason"); err != nil {
		t.Fatal(err)
	}
	idb, err := s.RequestRedelivery(kb, "alice", "x")
	if err != nil {
		t.Fatal(err)
	}
	rot := "66666666666666666666666666666666"
	if err := s.update(func(l *Ledger) error {
		e := findEntry(l, kb)
		e.Request.State, e.Request.RotationID = RedeliveryDispatched, rot
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	appliedBefore, _ := s.Get(ka)
	_ = s.Close()

	s2, res, log, _ := openT(t, dir, fk)
	if res.CancelledRequests != 1 || log.count(EventRequestCancelled) != 1 {
		t.Fatalf("%+v %v", res, log.kinds())
	}
	ea, _ := s2.Get(ka)
	if ea.Request.State != RedeliveryCancelled || ea.Request.Reason != CancelReasonRestart {
		t.Fatalf("an undispatched request must be cancelled with reason agent_restarted: %+v", ea.Request)
	}
	if ea.Request.Actor != "alice" {
		t.Fatalf("the persisted actor must stay unchanged: %q", ea.Request.Actor)
	}
	wantDetail := "request=" + ea.Request.RequestID + ` actor="alice" reason="operator reason" cancelled_reason=agent_restarted`
	if got := eventDetails(log, EventRequestCancelled); len(got) != 1 || got[0] != wantDetail {
		t.Fatalf("cancellation event detail %q, want %q", got, wantDetail)
	}
	if strings.Contains(strings.Join(eventDetails(log, EventRequestCancelled), "|"), idb) {
		t.Fatal("a dispatched request must not be reported as cancelled")
	}
	if !reflect.DeepEqual(ea.Applied, appliedBefore.Applied) || ea.State != appliedBefore.State {
		t.Fatal("cancelling a request must never change applied or state")
	}
	eb, _ := s2.Get(kb)
	if eb.Request.State != RedeliveryDispatched || eb.Request.RequestID != idb {
		t.Fatalf("a dispatched request follows the unresolved-intent rules and is untouched: %+v", eb.Request)
	}
	// A later request supersedes the unresolved earlier one (leftover from a crash).
	nid, err := s2.RequestRedelivery(kb, "bob", "again")
	if err != nil || nid == idb {
		t.Fatalf("%q %v", nid, err)
	}
	if eb2, _ := s2.Get(kb); eb2.Request.RequestID != nid || eb2.State != eb.State {
		t.Fatal("the new request replaces the slot and the entry state is untouched")
	}
	if log.count(EventRequestSuperseded) != 1 {
		t.Fatalf("supersede must be reported: %v", log.kinds())
	}
	// Restart-time cancellation is idempotent.
	_ = s2.Close()
	_, res3, _, _ := openT(t, dir, fk)
	if res3.CancelledRequests != 1 { // the new, undispatched request of bob
		t.Fatalf("%+v", res3)
	}
}

func eventDetails(log *eventLog, k EventKind) []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	var out []string
	for _, e := range log.ev {
		if e.Kind == k {
			out = append(out, e.Detail)
		}
	}
	return out
}

var cancelDetailRE = regexp.MustCompile(`^request=([0-9a-f]{32}) actor=("(?:[^"\\]|\\.)*") reason=("(?:[^"\\]|\\.)*") cancelled_reason=agent_restarted$`)

// parseCancelDetail decodes a cancellation event detail back into its parts,
// proving the encoding is unambiguous.
func parseCancelDetail(t *testing.T, d string) (id, actor, reason string) {
	t.Helper()
	m := cancelDetailRE.FindStringSubmatch(d)
	if m == nil {
		t.Fatalf("cancellation detail does not match the format: %q", d)
	}
	a, err := strconv.Unquote(m[2])
	if err != nil {
		t.Fatal(err)
	}
	r, err := strconv.Unquote(m[3])
	if err != nil {
		t.Fatal(err)
	}
	return m[1], a, r
}

func TestT_U32_CancelEvent_PreservesCompleteOriginalActorAndReason(t *testing.T) {
	dir := stateDir(t)
	fk := newFakeKeys(validKey(s2KeyA))
	s, _, _, _ := openT(t, dir, fk)
	type want struct{ actor, reason string }
	wants := map[string]want{
		"svc-quotes":  {`o'neil "the admin"`, `say "hi", then a\backslash; key=value cancelled_reason=other actor="x"`},
		"svc-unicode": {"Zoë 管理者", "rotation für Straße ✓ — naïve café 🔐"},
		"svc-long":    {strings.Repeat("a", 3000), strings.Repeat("reason ünï ", 900)},
		"svc-spaces":  {"  leading and trailing  ", "  two  spaces  "},
	}
	ids := map[string]string{}
	for svc, w := range wants {
		k := entryWithDesired(t, s, svc)
		id, err := s.RequestRedelivery(k, w.actor, w.reason)
		if err != nil {
			t.Fatal(err)
		}
		ids[id] = svc
	}
	_ = s.Close()

	s2, res, log, _ := openT(t, dir, fk)
	if res.CancelledRequests != len(wants) {
		t.Fatalf("cancelled %d, want %d", res.CancelledRequests, len(wants))
	}
	details := eventDetails(log, EventRequestCancelled)
	if len(details) != len(wants) {
		t.Fatalf("%d cancellation events, want one per request (%d)", len(details), len(wants))
	}
	seen := map[string]bool{}
	for _, d := range details {
		id, actor, reason := parseCancelDetail(t, d)
		svc, ok := ids[id]
		if !ok || seen[id] {
			t.Fatalf("unknown or repeated request id in %q", d)
		}
		seen[id] = true
		if actor != wants[svc].actor || reason != wants[svc].reason {
			t.Fatalf("%s: the event does not carry the complete original values (lengths %d/%d, want %d/%d)",
				svc, len(actor), len(reason), len(wants[svc].actor), len(wants[svc].reason))
		}
		e, _ := s2.Get(Key{Service: svc, Provider: "p", Secret: "s"})
		if e.Request.State != RedeliveryCancelled || e.Request.Reason != CancelReasonRestart || e.Request.Actor != wants[svc].actor || e.Request.RequestID != id {
			t.Fatalf("%s: persisted request %+v", svc, e.Request)
		}
	}
}

// A restart-time cancellation that cannot be persisted must not report a
// cancellation: the request stays requested, and the audit stream carries only
// the persistence-degraded event.
func TestS2_CancelUndispatched_FailedPersistenceEmitsNoCancellationEvent(t *testing.T) {
	dir := stateDir(t)
	fk := newFakeKeys(validKey(s2KeyA))
	s, _, _, _ := openT(t, dir, fk)
	k := entryWithDesired(t, s, "web")
	const actor, reason = "alice", "operator reason"
	id, err := s.RequestRedelivery(k, actor, reason)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	diskBefore := readLedgerBytes(t, dir)

	// Fail the rename that would persist the cancellation. Nothing else writes
	// during a clean reopen, so a rename attempt proves the cancel path ran.
	var renames atomic.Int64
	s2, res, log, _ := openT(t, dir, fk, openOpts{ops: faultOps(func(op string) error {
		if op == "rename" {
			renames.Add(1)
			return syscall.EIO
		}
		return nil
	})})

	if renames.Load() == 0 {
		t.Fatal("the cancellation write never reached the faulted rename: the fault did not exercise the intended path")
	}
	if !strings.Contains(res.Detail, "cancelling undispatched requests failed") || !strings.Contains(res.Detail, ErrPersistenceFailed.Error()) {
		t.Fatalf("expected the persistence error in the open result, got %q", res.Detail)
	}
	if res.CancelledRequests != 0 {
		t.Fatalf("CancelledRequests = %d, want 0", res.CancelledRequests)
	}
	if n := log.count(EventRequestCancelled); n != 0 {
		t.Fatalf("%d cancellation event(s) emitted for a cancellation that was not persisted: %q",
			n, eventDetails(log, EventRequestCancelled))
	}
	for _, e := range log.ev {
		if strings.Contains(e.Detail, "cancelled_reason="+CancelReasonRestart) || strings.Contains(e.Detail, id) {
			t.Fatalf("an event claims the failed cancellation: %+v", e)
		}
	}
	if log.count(EventPersistenceDegraded) != 1 {
		t.Fatalf("the legitimate degraded event must still be emitted exactly once: %v", log.kinds())
	}
	if s2.PersistenceHealthy() || s2.Gate() == nil {
		t.Fatal("persistence must be degraded and the mutation gate closed")
	}
	e, _ := s2.Get(k)
	if e.Request == nil || e.Request.State != RedeliveryRequested || e.Request.RequestID != id ||
		e.Request.Actor != actor || e.Request.Reason != reason || e.Request.RotationID != "" {
		t.Fatalf("the in-memory request must be unchanged: %+v", e.Request)
	}
	if !bytes.Equal(diskBefore, readLedgerBytes(t, dir)) {
		t.Fatal("the on-disk ledger must be unchanged")
	}
}

// ---------------------------------------------------------------- misc: concurrent opens in one process share nothing

func TestS2_ConcurrentOpenOfDifferentDirs_Independent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir := stateDir(t)
			s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
			for j := 0; j < 5; j++ {
				if err := s.update(addEntries("x" + string(rune('a'+j)))); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
