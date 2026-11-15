package ledger

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------- clone

func populatedLedger() *Ledger {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	v := func(id string) *Version { return &Version{Kind: KindProvider, ID: id} }
	rep := func(n string) Replica {
		return Replica{Number: n, ContainerID: "c" + n, ContainerName: "n" + n, Created: at.Format(time.RFC3339Nano), Applied: v("r" + n), Outcome: ReplicaSucceeded, At: at,
			RotationID: "rot-1111111111111111", Delivery: DeliveryVerified, Adoption: AdoptionVerified}
	}
	return &Ledger{
		SchemaVersion: SchemaVersion, LedgerID: "0123456789abcdef0123456789abcdef", Generation: 3, HMACKeyID: s2KeyA,
		CreatedAt: at, UpdatedAt: at,
		Entries: []Entry{{
			Key: Key{Project: "p", Service: "s", Provider: "x", Secret: "y"}, Desired: v("d"), DesiredSeq: 2, DesiredObservedAt: at,
			Applied: v("a"), State: EntryPending, Replicas: []Replica{rep("1"), rep("2")},
			Request:     &Redelivery{RequestID: "22222222222222222222222222222222", RequestedAt: at, Actor: "a", Reason: "r", TargetSeq: 1, State: RedeliveryRequested},
			LastOutcome: &LastOutcome{Kind: "k", Reason: "r", At: at, Strategy: "s"},
		}},
	}
}

// requirePopulated fails when any pointer or slice field reachable from v is
// nil or empty: it keeps populatedLedger complete when the schema grows, which
// is what makes the aliasing check below meaningful.
func requirePopulated(t *testing.T, path string, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			t.Errorf("%s: nil pointer in the populated fixture", path)
			return
		}
		requirePopulated(t, path, v.Elem())
	case reflect.Slice:
		if v.Len() == 0 {
			t.Errorf("%s: empty slice in the populated fixture", path)
			return
		}
		for i := 0; i < v.Len(); i++ {
			requirePopulated(t, fmt.Sprintf("%s[%d]", path, i), v.Index(i))
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			requirePopulated(t, path+"."+v.Type().Field(i).Name, v.Field(i))
		}
	}
}

// requireNoSharing fails when a and b share any pointer or slice backing array.
func requireNoSharing(t *testing.T, path string, a, b reflect.Value) {
	t.Helper()
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s: clone shares a pointer with the original", path)
			return
		}
		requireNoSharing(t, path, a.Elem(), b.Elem())
	case reflect.Slice:
		if a.Len() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s: clone shares a slice with the original", path)
			return
		}
		for i := 0; i < a.Len(); i++ {
			requireNoSharing(t, fmt.Sprintf("%s[%d]", path, i), a.Index(i), b.Index(i))
		}
	case reflect.Struct:
		if a.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < a.NumField(); i++ {
			requireNoSharing(t, path+"."+a.Type().Field(i).Name, a.Field(i), b.Field(i))
		}
	}
}

func TestS2_Clone_DeepAndIndependent(t *testing.T) {
	orig := populatedLedger()
	requirePopulated(t, "ledger", reflect.ValueOf(orig))
	c := orig.Clone()
	if !reflect.DeepEqual(orig, c) {
		t.Fatal("a clone must equal its source")
	}
	requireNoSharing(t, "ledger", reflect.ValueOf(orig), reflect.ValueOf(c))

	// Mutating every reachable pointer and slice element of the clone leaves the
	// original unchanged.
	before := populatedLedger()
	e := &c.Entries[0]
	e.Desired.ID, e.Applied.ID = "X", "X"
	e.Replicas[0].Applied.ID, e.Replicas[1].ContainerID = "X", "X"
	e.Request.State, e.LastOutcome.Kind = RedeliveryFailed, "X"
	c.Entries = append(c.Entries, Entry{})
	if !reflect.DeepEqual(orig, before) {
		t.Fatal("mutating a clone changed the original")
	}
}

func TestS2_Clone_NilAndEmptyPreserved(t *testing.T) {
	var nl *Ledger
	if nl.Clone() != nil {
		t.Fatal("nil clone")
	}
	l := &Ledger{Entries: []Entry{{Replicas: nil}}}
	c := l.Clone()
	if c.Entries == nil || c.Entries[0].Replicas != nil {
		t.Fatal("nil and empty slices must be preserved")
	}
}

// ---------------------------------------------------------------- update basics

func TestS2_FirstRun_PersistsAndReloads(t *testing.T) {
	dir := stateDir(t)
	fk := newFakeKeys(KeyInspection{State: KeyAbsent})
	s, res, _, _ := openT(t, dir, fk)
	if res.Mode != ModeDurable || fk.calls() != 1 || res.KeyID != s2KeyB {
		t.Fatalf("first run: %+v, generate calls %d", res, fk.calls())
	}
	snap := s.Snapshot()
	if !snap.Durable || snap.Ledger.Generation != 1 || snap.Ledger.HMACKeyID != s2KeyB {
		t.Fatalf("snapshot after first run: %+v", snap.Ledger)
	}
	id := snap.Ledger.LedgerID
	if err := s.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, res2, _, _ := openT(t, dir, fk)
	if res2.Reason != ReasonNone || res2.Mode != ModeDurable || fk.calls() != 1 {
		t.Fatalf("reload: %+v, generate calls %d", res2, fk.calls())
	}
	l := s2.Snapshot().Ledger
	if l.LedgerID != id || l.Generation != 2 || len(l.Entries) != 1 {
		t.Fatalf("reloaded ledger: id %s gen %d entries %d", l.LedgerID, l.Generation, len(l.Entries))
	}
}

func TestS2_Update_StoreSetsGenerationAndTime(t *testing.T) {
	s, _, _, _ := openT(t, stateDir(t), newFakeKeys(validKey(s2KeyA)))
	g0 := s.Snapshot().Ledger.Generation
	// The closure tries to set both; the store overrides them.
	if err := s.update(func(l *Ledger) error {
		l.Generation = 999
		l.UpdatedAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	l := s.Snapshot().Ledger
	if l.Generation != g0+1 || l.UpdatedAt.Year() != 2026 {
		t.Fatalf("generation %d (want %d), updated_at %v", l.Generation, g0+1, l.UpdatedAt)
	}
}

func TestS2_Update_RetainedPointerCannotMutatePublishedState(t *testing.T) {
	dir := stateDir(t)
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	var retained *Ledger
	if err := s.update(func(l *Ledger) error {
		l.Entries = append(l.Entries, s2Entry("web"))
		retained = l
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantFile := readLedgerBytes(t, dir)
	want := s.Snapshot().Ledger
	// The closure's pointer is retained and mutated after the update returned.
	retained.Entries[0].Desired.ID = "tampered"
	retained.Entries[0].State = EntryFailed
	retained.Entries = nil
	retained.HMACKeyID = "garbage"
	if got := s.Snapshot().Ledger; !reflect.DeepEqual(got, want) {
		t.Fatal("a retained closure pointer mutated published state")
	}
	if e, _ := s.Get(Key{Service: "web", Provider: "p", Secret: "s"}); e.Desired.ID != "pv-1" {
		t.Fatal("Get observed the retained pointer's mutation")
	}
	// A later update starts from the untouched published state and writes it.
	if err := s.update(func(l *Ledger) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(wantFile, readLedgerBytes(t, dir)) {
		t.Fatal("the later update should have bumped the generation")
	}
	if e, ok := s.Get(Key{Service: "web", Provider: "p", Secret: "s"}); !ok || e.Desired.ID != "pv-1" {
		t.Fatal("published state lost the entry")
	}
}

func TestS2_GetAndSnapshot_ReturnIndependentCopies(t *testing.T) {
	s, _, _, _ := openT(t, stateDir(t), newFakeKeys(validKey(s2KeyA)))
	if err := s.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	k := Key{Service: "web", Provider: "p", Secret: "s"}
	e, _ := s.Get(k)
	e.Desired.ID = "X"
	e.State = EntryFailed
	snap := s.Snapshot()
	snap.Ledger.Entries[0].Desired.ID = "Y"
	snap.Ledger.Entries = nil
	snap.Ledger.LedgerID = "Z"
	e2, ok := s.Get(k)
	if !ok || e2.Desired.ID != "pv-1" || e2.State != EntryIdle {
		t.Fatalf("Get returned shared state: %+v", e2)
	}
	if got := s.Snapshot().Ledger; len(got.Entries) != 1 || got.LedgerID == "Z" {
		t.Fatal("Snapshot returned shared state")
	}
	if _, ok := s.Get(Key{Service: "none"}); ok {
		t.Fatal("Get of a missing key")
	}
}

func TestS2_Update_ValidationAndClosureErrors_DoNotChangeHealthOrMemory(t *testing.T) {
	dir := stateDir(t)
	s, _, log, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if err := s.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	file := readLedgerBytes(t, dir)
	want := s.Snapshot().Ledger
	boom := errors.New("closure failed")
	cases := map[string]func(*Ledger) error{
		"closure error": func(l *Ledger) error { l.Entries = nil; return boom },
		"duplicate key": func(l *Ledger) error { l.Entries = append(l.Entries, l.Entries[0]); return nil },
		"bad state":     func(l *Ledger) error { l.Entries[0].State = "bogus"; return nil },
		"bad key id":    func(l *Ledger) error { l.HMACKeyID = "nope"; return nil },
	}
	for name, fn := range cases {
		err := s.update(fn)
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if name == "closure error" && !errors.Is(err, boom) {
			t.Fatalf("%s: %v", name, err)
		}
		if name != "closure error" && !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: want ErrInvalid, got %v", name, err)
		}
		if errors.Is(err, ErrPersistenceFailed) {
			t.Fatalf("%s: a validation/closure error must not be a persistence failure", name)
		}
		if !s.PersistenceHealthy() || s.Health().ConsecutiveFailures != 0 {
			t.Fatalf("%s: health changed", name)
		}
		if !reflect.DeepEqual(s.Snapshot().Ledger, want) || !bytes.Equal(file, readLedgerBytes(t, dir)) {
			t.Fatalf("%s: memory or file changed", name)
		}
	}
	if err := s.update(func(*Ledger) error { return ErrNoChange }); err != nil {
		t.Fatalf("ErrNoChange: %v", err)
	}
	if !reflect.DeepEqual(s.Snapshot().Ledger, want) {
		t.Fatal("ErrNoChange changed the document")
	}
	if log.count(EventPersistenceDegraded) != 0 {
		t.Fatal("no degraded event expected")
	}
}

// ---------------------------------------------------------------- T-U8 fault injection

type recFile struct {
	tmpFile
	hook func(op string) error
}

func (r recFile) Write(p []byte) (int, error) {
	if err := r.hook("write"); err != nil {
		return 0, err
	}
	return r.tmpFile.Write(p)
}
func (r recFile) Sync() error {
	if err := r.hook("fsync"); err != nil {
		return err
	}
	return r.tmpFile.Sync()
}
func (r recFile) Close() error {
	if err := r.hook("close"); err != nil {
		_ = r.tmpFile.Close()
		return err
	}
	return r.tmpFile.Close()
}

// faultOps wraps the real operations so op names in faults fail.
func faultOps(hook func(op string) error) func(*fileOps) {
	return func(o *fileOps) {
		realCreate, realRename, realSync := o.createTemp, o.rename, o.syncDir
		o.createTemp = func(p string) (tmpFile, error) {
			if err := hook("create"); err != nil {
				return nil, err
			}
			f, err := realCreate(p)
			if err != nil {
				return nil, err
			}
			return recFile{f, hook}, nil
		}
		o.rename = func(a, b string) error {
			if err := hook("rename"); err != nil {
				return err
			}
			return realRename(a, b)
		}
		o.syncDir = func(d string) error {
			if err := hook("syncdir"); err != nil {
				return err
			}
			return realSync(d)
		}
	}
}

func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "ledger.json.tmp-*"))
	return m
}

func TestT_U8_AtomicWriteFaultInjection(t *testing.T) {
	faults := []struct {
		name, op string
		err      error
		newFile  bool // the file may already hold the new content (rename happened)
	}{
		{"tmp create", "create", errors.New("create failed"), false},
		{"write", "write", errors.New("write failed"), false},
		{"file fsync", "fsync", errors.New("fsync failed"), false},
		{"close", "close", errors.New("close failed"), false},
		{"rename", "rename", errors.New("rename failed"), false},
		{"dir fsync", "syncdir", errors.New("dir fsync failed"), true},
		{"ENOSPC on write", "write", syscall.ENOSPC, false},
		{"EACCES on create", "create", syscall.EACCES, false},
	}
	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			dir := stateDir(t)
			var armed atomic.Bool
			var attempts atomic.Int32
			s, _, log, ia := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
				if armed.Load() && op == f.op {
					if op == "create" {
						attempts.Add(1)
					}
					return f.err
				}
				return nil
			})})
			if err := s.update(addEntries("web")); err != nil {
				t.Fatal(err)
			}
			oldFile := readLedgerBytes(t, dir)
			want := s.Snapshot().Ledger
			armed.Store(true)
			err := s.update(addEntries("db"))
			if !errors.Is(err, ErrPersistenceFailed) || !errors.Is(err, f.err) {
				t.Fatalf("want ErrPersistenceFailed wrapping the fault, got %v", err)
			}
			if !reflect.DeepEqual(s.Snapshot().Ledger, want) {
				t.Fatal("memory changed after a failed write")
			}
			if !f.newFile && !bytes.Equal(oldFile, readLedgerBytes(t, dir)) {
				t.Fatal("the old file is not intact")
			}
			if f.newFile && bytes.Equal(oldFile, readLedgerBytes(t, dir)) {
				t.Fatal("rename happened before the dir fsync: the file should hold the new content")
			}
			if tmp := tempFiles(t, dir); len(tmp) != 0 {
				t.Fatalf("temporary files left behind: %v", tmp)
			}
			if s.PersistenceHealthy() || s.Gate() == nil {
				t.Fatal("a failed durable write must degrade health and close the gate")
			}
			if log.count(EventPersistenceDegraded) != 1 {
				t.Fatalf("want exactly one degraded event, got %v", log.kinds())
			}
			if f.op == "create" && attempts.Load() != 3 {
				t.Fatalf("want 3 attempts, got %d", attempts.Load())
			}
			if d := ia.got(); len(d) < 2 || d[0] != 250*time.Millisecond || d[1] != time.Second {
				t.Fatalf("retry delays %v, want 250ms then 1s", d)
			}
			// The stale on-disk state is still a valid, loadable ledger.
			if _, err := Unmarshal(readLedgerBytes(t, dir)); err != nil {
				t.Fatalf("the file on disk must stay loadable: %v", err)
			}
		})
	}
}

func TestS2_Update_DurableOrdering(t *testing.T) {
	dir := stateDir(t)
	var mu sync.Mutex
	var seq []string
	var s *Store
	var visibleAtDirSync uint64
	rec := func(op string) error {
		mu.Lock()
		seq = append(seq, op)
		mu.Unlock()
		if op == "syncdir" && s != nil {
			visibleAtDirSync = s.Snapshot().Ledger.Generation
		}
		return nil
	}
	var armed atomic.Bool
	st, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() {
			return rec(op)
		}
		return nil
	})})
	s = st
	before := s.Snapshot().Ledger.Generation
	armed.Store(true)
	if err := s.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	want := []string{"create", "write", "fsync", "close", "rename", "syncdir"}
	if !reflect.DeepEqual(seq, want) {
		t.Fatalf("durable order %v, want %v", seq, want)
	}
	if visibleAtDirSync != before {
		t.Fatalf("state was published (generation %d) before the directory fsync completed", visibleAtDirSync)
	}
	if s.Snapshot().Ledger.Generation != before+1 {
		t.Fatal("state not published after success")
	}
}

// ---------------------------------------------------------------- retry, health, probe (T-U18..T-U22 closures)

func TestS2_Retry_ThreeAttempts_DelaysAndRecovery(t *testing.T) {
	dir := stateDir(t)
	f := &failN{n: 2, err: errors.New("transient")}
	var armed atomic.Bool
	s, _, _, ia := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() && op == "create" {
			return f.hit()
		}
		return nil
	})})
	armed.Store(true)
	calls := 0
	if err := s.update(func(l *Ledger) error { calls++; l.Entries = append(l.Entries, s2Entry("web")); return nil }); err != nil {
		t.Fatalf("the third attempt should succeed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("the closure ran %d times, want exactly once per Update", calls)
	}
	if f.total() != 3 {
		t.Fatalf("attempts %d, want 3", f.total())
	}
	if d := ia.got(); len(d) != 2 || d[0] != 250*time.Millisecond || d[1] != time.Second {
		t.Fatalf("delays %v, want [250ms 1s] (the first attempt has no delay)", d)
	}
	if !s.PersistenceHealthy() {
		t.Fatal("a successful write must leave persistence healthy")
	}
}

func TestS2_Retry_Exhaustion_LeavesPublishedStateUnchanged(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() && op == "rename" {
			return syscall.EIO
		}
		return nil
	})})
	want := s.Snapshot().Ledger
	armed.Store(true)
	if err := s.update(addEntries("web")); !errors.Is(err, ErrPersistenceFailed) {
		t.Fatalf("got %v", err)
	}
	h := s.Health()
	if h.Healthy || h.ConsecutiveFailures != 1 || h.LastError == "" || !h.ProbeRunning {
		t.Fatalf("health after exhaustion: %+v", h)
	}
	if !reflect.DeepEqual(s.Snapshot().Ledger, want) {
		t.Fatal("exhaustion changed published state")
	}
}

// The D-3 rules with test-local update closures and a fake dispatcher (owner
// decision D-S2-2: S4 builds the real transitions on top of Update).

func observeClosure(svc, id string) func(*Ledger) error {
	return func(l *Ledger) error {
		e := findEntry(l, Key{Service: svc, Provider: "p", Secret: "s"})
		if e == nil {
			l.Entries = append(l.Entries, idleEntry("", svc, "p", "s"))
			e = &l.Entries[len(l.Entries)-1]
		}
		if e.Desired != nil && e.Desired.ID == id {
			return ErrNoChange
		}
		e.Desired = &Version{Kind: KindProvider, ID: id}
		e.DesiredSeq++
		e.DesiredObservedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		return nil
	}
}

// hintClosure records a requested delivery (the schema 3 correlation hint) on
// the entry of svc. Under owner decision O-3 the ledger never gates Docker, so
// the tests below assert persistence semantics only.
func hintClosure(svc string) func(*Ledger) error {
	return func(l *Ledger) error {
		e := findEntry(l, Key{Service: svc, Provider: "p", Secret: "s"})
		if e == nil {
			return ErrNotFound
		}
		e.State = EntryPending
		e.Replicas = []Replica{{ContainerID: "c-" + svc, ContainerName: svc + "-1", Created: "2026-01-01T00:00:00Z",
			Outcome: ReplicaPending, Reason: "delivery_requested", At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			RotationID: "rot-4444444444444444", Delivery: DeliveryRequested, Adoption: AdoptionUnknown}}
		return nil
	}
}

func recordHint(s *Store, svc string) error { return s.update(hintClosure(svc)) }

func TestT_U18_DesiredWriteFailure_NoDispatchStateUnchangedRetried(t *testing.T) {
	dir := stateDir(t)
	var fail atomic.Bool
	fail.Store(true)
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if fail.Load() && op == "rename" {
			return syscall.EIO
		}
		return nil
	})})
	want := s.Snapshot().Ledger
	err := s.update(observeClosure("web", "v2"))
	if !errors.Is(err, ErrPersistenceFailed) {
		t.Fatalf("got %v", err)
	}
	// Failed observe: the published state is unchanged (the engine then
	// continues per X6; the ledger never decides dispatch).
	if !reflect.DeepEqual(s.Snapshot().Ledger, want) {
		t.Fatal("state must be unchanged")
	}
	fail.Store(false) // next poll re-observes and retries
	if err := s.update(observeClosure("web", "v2")); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Get(Key{Service: "web", Provider: "p", Secret: "s"}); e.Desired == nil || e.Desired.ID != "v2" || e.DesiredSeq != 1 {
		t.Fatalf("retry did not record the observation: %+v", e)
	}
}

func TestT_U19_HintWriteFailure_NothingRecorded_DesiredStaysPending(t *testing.T) {
	dir := stateDir(t)
	var fail atomic.Bool
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if fail.Load() && op == "create" {
			return syscall.ENOSPC
		}
		return nil
	})})
	if err := s.update(observeClosure("web", "v2")); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if err := recordHint(s, "web"); err == nil {
		t.Fatal("the hint write must fail")
	}
	e, _ := s.Get(Key{Service: "web", Provider: "p", Secret: "s"})
	if e.Desired == nil || e.Applied != nil || len(e.Replicas) != 0 || e.State != EntryIdle {
		t.Fatalf("desired must stay pending and durable: %+v", e)
	}
	fail.Store(false)
	// The probe is gated by the test clock; the next explicit attempt succeeds
	// once the write path works, and the gate reopens with it.
	if err := s.update(func(*Ledger) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := recordHint(s, "web"); err != nil {
		t.Fatalf("retry: err %v", err)
	}
}

func TestT_U20_TerminalWriteFailure_ThreeAttempts_Degraded_AppliedNotAdvanced(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	var creates atomic.Int32
	s, _, log, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() && op == "create" {
			creates.Add(1)
			return syscall.EIO
		}
		return nil
	})})
	if err := s.update(observeClosure("web", "v2")); err != nil {
		t.Fatal(err)
	}
	if err := recordHint(s, "web"); err != nil {
		t.Fatal(err)
	}
	fileBefore := readLedgerBytes(t, dir)
	armed.Store(true)
	// Delivery succeeded; the terminal write fails.
	err := s.update(func(l *Ledger) error {
		e := findEntry(l, Key{Service: "web", Provider: "p", Secret: "s"})
		v := *e.Desired.cloneForTest()
		e.Applied, e.State = &v, EntryIdle
		r := &e.Replicas[0]
		r.Applied, r.Outcome, r.Delivery, r.Adoption, r.Reason = e.Desired.cloneForTest(), ReplicaSucceeded, DeliveryVerified, AdoptionVerified, "delivery_verified"
		return nil
	})
	if !errors.Is(err, ErrPersistenceFailed) || creates.Load() != 3 {
		t.Fatalf("err %v, attempts %d (want exactly 3)", err, creates.Load())
	}
	if s.PersistenceHealthy() || log.count(EventPersistenceDegraded) != 1 {
		t.Fatal("degraded must be reported once")
	}
	if !bytes.Equal(fileBefore, readLedgerBytes(t, dir)) {
		t.Fatal("the file's applied state must not advance")
	}
	e, _ := s.Get(Key{Service: "web", Provider: "p", Secret: "s"})
	if e.Applied != nil || e.Replicas[0].Delivery != DeliveryRequested {
		t.Fatalf("published state must not claim durable applied: %+v", e)
	}
}

func (v Version) cloneForTest() *Version { c := v; return &c }

// O-3: while degraded Gate reports persistence unavailable (advisory), every
// write fails without changing the published state, and reads continue. The
// ledger never refuses or gates a Docker mutation.
func TestT_U21_Degraded_GateAdvisory_WritesFailWithoutChange(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() && op == "create" {
			return syscall.EIO
		}
		return nil
	})})
	if err := s.update(addEntries("a", "b", "c")); err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	_ = s.update(observeClosure("a", "v9")) // degrade
	if !errors.Is(s.Gate(), ErrPersistenceUnavailable) {
		t.Fatal("Gate must report degraded persistence")
	}
	before := s.Snapshot().Ledger
	for _, svc := range []string{"a", "b", "c", "unrelated"} {
		if err := recordHint(s, svc); err == nil {
			t.Fatalf("%s: a write must fail while degraded", svc)
		}
	}
	if !reflect.DeepEqual(s.Snapshot().Ledger, before) {
		t.Fatal("failed writes changed the published state")
	}
	// R6: reads, identity computation and diagnostics continue.
	if s.Snapshot().Ledger == nil || s.Health().Healthy {
		t.Fatal("reads and health must keep working while degraded")
	}
}

func TestT_U22_Recovery_HeldRecordFlushed_GateReopens(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	armed.Store(false)
	var held atomic.Bool
	var flushCalls atomic.Int32
	var sp *Store
	s, _, log, ia := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{
		ops: faultOps(func(op string) error {
			if armed.Load() && op == "create" {
				return syscall.EIO
			}
			return nil
		}),
		cfg: func(c *Config) {
			c.Flush = func() (bool, error) {
				flushCalls.Add(1)
				if !held.Load() {
					return false, nil
				}
				if err := sp.update(func(l *Ledger) error { l.Entries[0].State = EntryIdle; return nil }); err != nil {
					return false, err
				}
				held.Store(false)
				return true, nil
			}
		},
	})
	sp = s
	if err := s.update(observeClosure("web", "v2")); err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	held.Store(true) // an unpersisted terminal record is held by the caller
	_ = s.update(observeClosure("web", "v3"))
	if s.Gate() == nil {
		t.Fatal("expected degraded")
	}
	// Probe attempt 1: the flush fails (still failing), degraded stays.
	ia.fireProbe(t)
	waitFor(t, func() bool { return flushCalls.Load() >= 1 && s.Health().ConsecutiveFailures >= 2 })
	if s.Gate() == nil || !held.Load() {
		t.Fatal("still degraded, record still held")
	}
	armed.Store(false) // persistence recovers
	ia.fireProbe(t)
	waitFor(t, func() bool { return s.PersistenceHealthy() })
	if held.Load() {
		t.Fatal("the held record must be flushed first")
	}
	if log.count(EventPersistenceRestored) != 1 {
		t.Fatalf("events %v", log.kinds())
	}
	if err := recordHint(s, "web"); err != nil {
		t.Fatalf("pending work must resume through the gate: %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestS2_Probe_HarmlessWriteWhenNothingHeld_BackoffCapped(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	s, _, log, ia := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(op string) error {
		if armed.Load() && op == "rename" {
			return syscall.EIO
		}
		return nil
	})})
	armed.Store(true)
	_ = s.update(addEntries("web"))
	g := s.Snapshot().Ledger.Generation
	// Seven failing probes: waits are 5s, 10s, 20s, 40s, 60s, 60s, 60s.
	for i := 0; i < 7; i++ {
		ia.fireProbe(t)
		n := i + 1
		waitFor(t, func() bool { return s.Health().ConsecutiveFailures >= 1+n })
	}
	var probeWaits []time.Duration
	for _, d := range ia.got() {
		if d >= probeMin {
			probeWaits = append(probeWaits, d)
		}
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second}
	if len(probeWaits) < 7 || !reflect.DeepEqual(probeWaits[:7], want[:7]) {
		t.Fatalf("probe waits %v, want %v", probeWaits, want)
	}
	armed.Store(false)
	ia.fireProbe(t)
	waitFor(t, func() bool { return s.PersistenceHealthy() })
	if got := s.Snapshot().Ledger.Generation; got != g+1 {
		t.Fatalf("the harmless probe write must bump the generation once: %d -> %d", g, got)
	}
	waitFor(t, func() bool { return !s.Health().ProbeRunning })
	if log.count(EventPersistenceRestored) != 1 {
		t.Fatalf("events %v", log.kinds())
	}
}

func TestS2_Probe_SingleFlight(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	var running, peak, runs atomic.Int32
	release := make(chan struct{})
	s, _, _, ia := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{
		ops: faultOps(func(op string) error {
			if armed.Load() && op == "create" {
				return syscall.EIO
			}
			return nil
		}),
		cfg: func(c *Config) {
			c.Flush = func() (bool, error) {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				runs.Add(1)
				<-release
				running.Add(-1)
				return false, errors.New("still failing")
			}
		},
	})
	armed.Store(true)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ { // many degraded failures start at most one probe
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.update(addEntries("x" + fmt.Sprint(i))) }()
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	ia.mu.Lock()
	waiting := len(ia.pending)
	ia.mu.Unlock()
	if waiting != 1 {
		t.Fatalf("%d probe goroutines are waiting, want exactly 1 (single-flight)", waiting)
	}
	ia.fireProbe(t)
	waitFor(t, func() bool { return running.Load() == 1 })
	time.Sleep(20 * time.Millisecond)
	if peak.Load() != 1 {
		t.Fatalf("probe concurrency peaked at %d", peak.Load())
	}
	close(release)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if running.Load() != 0 {
		t.Fatal("Close must wait for the probe")
	}
	r := runs.Load()
	time.Sleep(20 * time.Millisecond)
	if runs.Load() != r || s.Health().ProbeRunning {
		t.Fatal("no probe may run after Close")
	}
}

// ---------------------------------------------------------------- callbacks and Close

func TestS2_Events_DeliveredOutsideMutex_PanicDoesNotRollBack(t *testing.T) {
	dir := stateDir(t)
	var sp *Store
	var armed atomic.Bool
	done := make(chan struct{})
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{
		ops: faultOps(func(op string) error {
			if armed.Load() && op == "create" {
				return syscall.EIO
			}
			return nil
		}),
		cfg: func(c *Config) {
			c.Events = func(e Event) {
				if e.Kind != EventPersistenceDegraded {
					return
				}
				// Re-entering the store from the callback would deadlock if the
				// mutex were still held.
				_ = sp.Snapshot()
				_ = sp.PersistenceHealthy()
				go func() { _ = sp.update(func(*Ledger) error { return ErrNoChange }); close(done) }()
				<-done
				panic("callback failure")
			}
		},
	})
	sp = s
	armed.Store(true)
	timeout := time.After(5 * time.Second)
	res := make(chan error, 1)
	go func() { res <- s.update(addEntries("web")) }()
	select {
	case err := <-res:
		if !errors.Is(err, ErrPersistenceFailed) {
			t.Fatalf("got %v", err)
		}
	case <-timeout:
		t.Fatal("deadlock: the callback ran while the store mutex was held")
	}
	armed.Store(false)
	// A panicking callback after a successful update does not roll it back.
	if err := s.update(addEntries("db")); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(Key{Service: "db", Provider: "p", Secret: "s"}); !ok {
		t.Fatal("the persisted update was rolled back")
	}
}

func TestS2_Callback_PanicAfterSuccess_DoesNotRollBack(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	s, _, _, ia := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{
		ops: faultOps(func(op string) error {
			if armed.Load() && op == "rename" {
				return syscall.EIO
			}
			return nil
		}),
		cfg: func(c *Config) {
			c.Events = func(e Event) {
				if e.Kind == EventPersistenceRestored {
					panic("boom")
				}
			}
		},
	})
	armed.Store(true)
	_ = s.update(addEntries("web"))
	armed.Store(false)
	ia.fireProbe(t)
	waitFor(t, func() bool { return s.PersistenceHealthy() })
	if err := s.update(addEntries("ok")); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(Key{Service: "ok", Provider: "p", Secret: "s"}); !ok {
		t.Fatal("update lost")
	}
}

func TestS2_Close_Semantics(t *testing.T) {
	dir := stateDir(t)
	fk := newFakeKeys(validKey(s2KeyA))
	s, _, _, _ := openT(t, dir, fk)
	if err := s.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close must be idempotent: %v", err)
	}
	if err := s.update(addEntries("db")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Update after Close: %v", err)
	}
	if err := s.updateMemoryOnly(func(*Ledger) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("updateMemoryOnly after Close: %v", err)
	}
	// The lock was released: a new store opens durably.
	s2, res, _, _ := openT(t, dir, fk)
	if res.Mode != ModeDurable {
		t.Fatalf("after Close the directory must be reusable: %+v", res)
	}
	_ = s2
}

func TestS2_Close_InterruptsRetryWait(t *testing.T) {
	dir := stateDir(t)
	var armed atomic.Bool
	block := make(chan time.Time) // never fires: the retry wait would block forever
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{
		ops: faultOps(func(op string) error {
			if armed.Load() && op == "create" {
				return syscall.EIO
			}
			return nil
		}),
		cfg: func(c *Config) { c.After = func(time.Duration) <-chan time.Time { return block } },
	})
	armed.Store(true)
	res := make(chan error, 1)
	go func() { res <- s.update(addEntries("web")) }()
	time.Sleep(20 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-res:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not wake the waiting update")
	}
}

// ---------------------------------------------------------------- memory-only (D-S2-7)

func newerSchemaFile(t *testing.T, dir string) []byte {
	t.Helper()
	b := []byte(`{"schema_version": 4, "future": {"x": [1,2,3]}}`)
	writeFile(t, filepath.Join(dir, "ledger.json"), b, 0o600)
	return b
}

func TestS2_MemoryOnly_UpdateRefused_updateMemoryOnlyWorks_NoDisk(t *testing.T) {
	dir := stateDir(t)
	orig := newerSchemaFile(t, dir)
	fp := dirFingerprint(t, dir)
	s, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if res.Mode != ModeMemoryOnly || res.Reason != ReasonNewerSchema {
		t.Fatalf("%+v", res)
	}
	before := s.Snapshot()
	if before.Durable || before.Mode != ModeMemoryOnly {
		t.Fatalf("memory-only snapshot must report Durable=false: %+v", before)
	}
	if err := s.update(addEntries("web")); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("Update: %v", err)
	}
	if !reflect.DeepEqual(s.Snapshot().Ledger, before.Ledger) {
		t.Fatal("a refused Update must not change anything")
	}
	if s.Gate() == nil || s.PersistenceHealthy() {
		t.Fatal("the gate must be closed in memory-only mode")
	}
	if err := s.updateMemoryOnly(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.Durable || len(snap.Ledger.Entries) != 1 {
		t.Fatalf("memory-only state: %+v", snap)
	}
	if snap.Ledger.Generation != before.Ledger.Generation {
		t.Fatal("updateMemoryOnly must not bump the generation (no durable write happened)")
	}
	// Validation still applies in memory.
	if err := s.updateMemoryOnly(func(l *Ledger) error { l.Entries[0].State = "bogus"; return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
	if len(s.Snapshot().Ledger.Entries) != 1 || s.Snapshot().Ledger.Entries[0].State != EntryIdle {
		t.Fatal("a failed updateMemoryOnly changed memory")
	}
	if got := dirFingerprint(t, dir); got != fp {
		t.Fatal("memory-only mode touched the disk")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "ledger.json"))
	if !bytes.Equal(b, orig) {
		t.Fatal("ledger bytes changed")
	}
}

func TestS2_updateMemoryOnly_RefusedInDurableMode(t *testing.T) {
	s, _, _, _ := openT(t, stateDir(t), newFakeKeys(validKey(s2KeyA)))
	if err := s.updateMemoryOnly(addEntries("web")); !errors.Is(err, ErrNotMemoryOnly) {
		t.Fatalf("got %v", err)
	}
	if len(s.Snapshot().Ledger.Entries) != 0 {
		t.Fatal("changed")
	}
}

// ---------------------------------------------------------------- T-U17 race stress

func TestT_U17_UpdateClosureRaceStress_NoLostUpdateGenerationMonotonic(t *testing.T) {
	dir := stateDir(t)
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	const keys, perKey = 8, 12
	names := make([]string, keys)
	for i := range names {
		names[i] = fmt.Sprintf("svc%d", i)
	}
	if err := s.update(addEntries(names...)); err != nil {
		t.Fatal(err)
	}
	g0 := s.Snapshot().Ledger.Generation
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var readerWG sync.WaitGroup
	for r := 0; r < 4; r++ { // concurrent readers see monotonic generations
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			var last uint64
			for {
				select {
				case <-stop:
					return
				default:
				}
				g := s.Snapshot().Ledger.Generation
				if g < last {
					t.Errorf("generation went backwards: %d after %d", g, last)
					return
				}
				last = g
			}
		}()
	}
	for _, n := range names {
		for j := 0; j < perKey; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := s.update(func(l *Ledger) error {
					e := findEntry(l, Key{Service: n, Provider: "p", Secret: "s"})
					e.DesiredSeq++ // a counter: a lost update would lose an increment
					return nil
				}); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()
	close(stop)
	readerWG.Wait()
	l := s.Snapshot().Ledger
	if want := g0 + keys*perKey; l.Generation != want {
		t.Fatalf("generation %d, want %d: an update was lost", l.Generation, want)
	}
	for _, n := range names {
		if e, _ := s.Get(Key{Service: n, Provider: "p", Secret: "s"}); e.DesiredSeq != 1+perKey {
			t.Fatalf("%s: counter %d, want %d", n, e.DesiredSeq, 1+perKey)
		}
	}
	// The file holds exactly the published document.
	disk, err := Unmarshal(readLedgerBytes(t, dir))
	if err != nil || disk.Generation != l.Generation {
		t.Fatalf("file generation mismatch: %v", err)
	}
}

// ---------------------------------------------------------------- limits (M-S2-1)

func TestS2_Limits_OversizeUpdate_IsPersistenceFailure(t *testing.T) {
	dir := stateDir(t)
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
	if err := s.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	old := fileSizeLimit
	fileSizeLimit = int64(len(readLedgerBytes(t, dir))) + 10
	defer func() { fileSizeLimit = old }()
	file := readLedgerBytes(t, dir)
	want := s.Snapshot().Ledger
	err := s.update(addEntries("db", "cache"))
	if !errors.Is(err, ErrPersistenceFailed) || !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
	if s.PersistenceHealthy() {
		t.Fatal("a size-cap refusal degrades health (M-S2-2)")
	}
	if !bytes.Equal(file, readLedgerBytes(t, dir)) || !reflect.DeepEqual(want, s.Snapshot().Ledger) {
		t.Fatal("a refused oversize update must change neither file nor memory")
	}
	if len(tempFiles(t, dir)) != 0 {
		t.Fatal("no temporary file may be created for a refused update")
	}
}

func TestS2_Limits_FileSizeBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 16 MiB twice")
	}
	t.Run("one byte over is untouched and memory-only", func(t *testing.T) {
		dir := stateDir(t)
		big := bytes.Repeat([]byte{'x'}, MaxFileSize+1)
		writeFile(t, filepath.Join(dir, "ledger.json"), big, 0o600)
		fp := dirFingerprint(t, dir)
		fk := newFakeKeys(validKey(s2KeyA))
		s, res, _, _ := openT(t, dir, fk)
		if res.Mode != ModeMemoryOnly || res.Reason != ReasonTooLarge {
			t.Fatalf("%+v", res)
		}
		if err := s.update(addEntries("web")); !errors.Is(err, ErrPersistenceUnavailable) {
			t.Fatal(err)
		}
		if dirFingerprint(t, dir) != fp || fk.calls() != 0 {
			t.Fatal("an oversize file must be left untouched")
		}
	})
	t.Run("exactly the limit is read, not too-large", func(t *testing.T) {
		dir := stateDir(t)
		exact := bytes.Repeat([]byte{'x'}, MaxFileSize)
		writeFile(t, filepath.Join(dir, "ledger.json"), exact, 0o600)
		_, res, _, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)))
		if res.Reason == ReasonTooLarge || res.Reason != ReasonCorrupt {
			t.Fatalf("a file of exactly the limit is read and (here) judged corrupt: %+v", res)
		}
	})
}

func deepDoc(depth int, valid bool) []byte {
	var b bytes.Buffer
	b.WriteString(`{"schema_version":3,"x":`)
	for i := 0; i < depth; i++ {
		b.WriteByte('[')
	}
	for i := 0; i < depth; i++ {
		b.WriteByte(']')
	}
	b.WriteByte('}')
	return b.Bytes()
}

func TestS2_Limits_JSONDepth(t *testing.T) {
	// The document object is depth 1, so `x` with n array levels reaches 1+n.
	atLimit := deepDoc(MaxJSONDepth-1, false)
	_, err := Unmarshal(atLimit)
	if err == nil || bytes.Contains([]byte(err.Error()), []byte("nesting")) {
		t.Fatalf("depth %d is allowed by the guard (later schema checks may still reject it): %v", MaxJSONDepth, err)
	}
	over := deepDoc(MaxJSONDepth, false)
	_, err = Unmarshal(over)
	if !errors.Is(err, ErrInvalid) || !bytes.Contains([]byte(err.Error()), []byte("nesting")) {
		t.Fatalf("depth %d must be rejected by the guard with ErrInvalid: %v", MaxJSONDepth+1, err)
	}
	// Extremely deep input is rejected as invalid, never a crash.
	if _, err := Unmarshal(deepDoc(100000, false)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("very deep input: %v", err)
	}
	// Phase-1 precedence: a newer schema is reported as such even when deeply nested.
	deepNewer := bytes.Replace(deepDoc(500, false), []byte(`"schema_version":3`), []byte(`"schema_version":4`), 1)
	var sv *SchemaVersionError
	if _, err := Unmarshal(deepNewer); !errors.As(err, &sv) {
		t.Fatalf("a newer schema must win over the depth guard: %v", err)
	}
	// A normal document is unaffected.
	if _, err := Unmarshal([]byte(vecV10Doc)); err != nil {
		t.Fatalf("regression: %v", err)
	}
}
