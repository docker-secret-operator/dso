package ledger

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// Directory-fsync outcome tests (owner decision E).
//
// The accepted guarantee: Update publishes the candidate in memory only after
// the whole durable write succeeded. If the rename succeeded and the directory
// fsync then failed, memory stays at the previously published document, the file
// on disk may already hold the complete candidate, and after a crash either the
// previous or the candidate document may be what a restart finds.
//
// These tests do NOT simulate power loss. The file content is fsynced before the
// rename, so the only two post-crash states are two complete files; the tests
// reproduce them by file-state substitution (keep the candidate, or put the
// previous bytes back while the store is closed) and then reopen.

type uncertain struct {
	dir       string
	s         *Store
	fk        *fakeKeys
	log       *eventLog
	ia        *instantAfter
	armed     *atomic.Bool
	syncCalls *atomic.Int32
	oldBytes  []byte
	before    *Ledger // published document before the failed update
}

// newUncertain opens a store holding one entry ("web") and returns it with the
// directory fsync armed to fail. The next Update is expected to fail after the
// rename.
func newUncertain(t *testing.T, failures int) *uncertain {
	t.Helper()
	u := &uncertain{dir: stateDir(t), fk: newFakeKeys(validKey(s2KeyA)), armed: &atomic.Bool{}, syncCalls: &atomic.Int32{}}
	var failed atomic.Int32
	hook := func(op string) error {
		if op == "syncdir" && u.armed.Load() {
			u.syncCalls.Add(1)
			if failures < 0 || int(failed.Add(1)) <= failures {
				return errors.New("directory fsync failed")
			}
		}
		return nil
	}
	u.s, _, u.log, u.ia = openT(t, u.dir, u.fk, openOpts{ops: faultOps(hook)})
	if err := u.s.update(addEntries("web")); err != nil {
		t.Fatal(err)
	}
	u.oldBytes = readLedgerBytes(t, u.dir)
	u.before = u.s.Snapshot().Ledger
	return u
}

func sum(b []byte) [32]byte { return sha256.Sum256(b) }

func hasEntry(l *Ledger, svc string) bool {
	return findEntry(l, Key{Service: svc, Provider: "p", Secret: "s"}) != nil
}

// replaceFile atomically puts content at ledger.json while no store is open.
func replaceFile(t *testing.T, dir string, content []byte) {
	t.Helper()
	tmp := filepath.Join(dir, "substitute.tmp")
	writeFile(t, tmp, content, 0o600)
	if err := os.Rename(tmp, filepath.Join(dir, "ledger.json")); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- 1. failure before rename

func TestS2_DirSync_FailureBeforeRename_OldDocumentRemains(t *testing.T) {
	for _, op := range []string{"create", "write", "fsync", "close", "rename"} {
		t.Run(op, func(t *testing.T) {
			dir := stateDir(t)
			var armed atomic.Bool
			s, _, log, _ := openT(t, dir, newFakeKeys(validKey(s2KeyA)), openOpts{ops: faultOps(func(o string) error {
				if armed.Load() && o == op {
					return syscall.EIO
				}
				return nil
			})})
			if err := s.update(addEntries("web")); err != nil {
				t.Fatal(err)
			}
			old := readLedgerBytes(t, dir)
			before := s.Snapshot()
			armed.Store(true)
			err := s.update(addEntries("db"))

			if !errors.Is(err, ErrPersistenceFailed) {
				t.Fatalf("caller result: %v", err)
			}
			if !bytes.Equal(old, readLedgerBytes(t, dir)) {
				t.Fatal("disk must remain the old document: the candidate was never installed")
			}
			if tmp := tempFiles(t, dir); len(tmp) != 0 {
				t.Fatalf("temporary files left: %v", tmp)
			}
			after := s.Snapshot()
			if !reflect.DeepEqual(before.Ledger, after.Ledger) || after.Ledger.Generation != before.Ledger.Generation {
				t.Fatal("memory and generation must be unchanged")
			}
			if s.PersistenceHealthy() || s.Gate() == nil || log.count(EventPersistenceDegraded) != 1 {
				t.Fatal("health must be degraded and the gate closed")
			}
			// O-3: the gate is advisory; a write while degraded fails and
			// changes nothing (it is never a Docker precondition).
			if err := recordHint(s, "web"); err == nil || !reflect.DeepEqual(s.Snapshot().Ledger, after.Ledger) {
				t.Fatalf("a write while degraded must fail without changing state: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------- 2. failure after rename

func TestS2_DirSync_FailureAfterRename_CandidateOnDisk_MemoryUnchanged(t *testing.T) {
	u := newUncertain(t, -1)
	u.armed.Store(true)
	err := u.s.update(addEntries("db"))

	if err == nil || !errors.Is(err, ErrPersistenceFailed) {
		t.Fatalf("the caller must see a persistence failure, never success: %v", err)
	}
	if !strings.Contains(err.Error(), "rename durability unknown") {
		t.Fatalf("the error must say the outcome is uncertain: %v", err)
	}
	if u.syncCalls.Load() != 3 {
		t.Fatalf("directory fsync attempted %d times, want the 3 bounded attempts", u.syncCalls.Load())
	}
	disk := readLedgerBytes(t, u.dir)
	if bytes.Equal(disk, u.oldBytes) {
		t.Fatal("the rename happened, so the file holds the candidate")
	}
	cand, perr := Unmarshal(disk)
	if perr != nil {
		t.Fatalf("the file on disk must be a complete, valid document: %v", perr)
	}
	if cand.Generation != u.before.Generation+1 || !hasEntry(cand, "db") || !hasEntry(cand, "web") {
		t.Fatalf("candidate: generation %d, entries %d", cand.Generation, len(cand.Entries))
	}
	mem := u.s.Snapshot()
	if !reflect.DeepEqual(mem.Ledger, u.before) || hasEntry(mem.Ledger, "db") {
		t.Fatal("memory must remain at the previously published document")
	}
	if mem.Ledger.Generation == cand.Generation {
		t.Fatal("memory and disk now represent different generations, as the guarantee states")
	}
	// Snapshot.Durable acknowledges the published document only; it does not say
	// the file currently holds exactly that generation.
	if !mem.Durable {
		t.Fatal("the published document was durably acknowledged earlier")
	}
	if len(tempFiles(t, u.dir)) != 0 {
		t.Fatal("no temporary file may remain after the rename")
	}
	if u.s.PersistenceHealthy() || u.s.Gate() == nil || u.log.count(EventPersistenceDegraded) != 1 {
		t.Fatal("health must be degraded and the gate closed")
	}
	if h := u.s.Health(); h.Healthy || h.ConsecutiveFailures != 1 || h.LastError == "" {
		t.Fatalf("health: %+v", h)
	}
	if err := recordHint(u.s, "web"); err == nil {
		t.Fatal("a write while degraded must fail")
	}
}

// ---------------------------------------------------------------- 3 and 4. restart outcomes

func TestS2_DirSync_RestartAfterOldDocumentOutcome(t *testing.T) {
	u := newUncertain(t, -1)
	u.armed.Store(true)
	if err := u.s.update(addEntries("db")); err == nil {
		t.Fatal("expected failure")
	}
	_ = u.s.Close()
	replaceFile(t, u.dir, u.oldBytes) // the directory entry did not become durable

	s2, res, _, _ := openT(t, u.dir, u.fk)
	if res.Mode != ModeDurable || res.Reason != ReasonNone || u.fk.calls() != 0 {
		t.Fatalf("%+v", res)
	}
	l := s2.Snapshot().Ledger
	if l.Generation != u.before.Generation || hasEntry(l, "db") || !hasEntry(l, "web") {
		t.Fatalf("the previous complete document must be recovered: generation %d", l.Generation)
	}
	if !s2.PersistenceHealthy() || s2.Gate() != nil || !s2.Snapshot().Durable {
		t.Fatal("a fresh start on a valid document is healthy")
	}
	if err := s2.update(addEntries("db")); err != nil {
		t.Fatal(err)
	}
	if got := s2.Snapshot().Ledger; got.Generation != u.before.Generation+1 || !hasEntry(got, "db") {
		t.Fatal("the next update continues from the recovered generation")
	}
}

func TestS2_DirSync_RestartAfterCandidateDocumentOutcome(t *testing.T) {
	u := newUncertain(t, -1)
	u.armed.Store(true)
	if err := u.s.update(addEntries("db")); err == nil {
		t.Fatal("expected failure")
	}
	_ = u.s.Close()

	s2, res, _, _ := openT(t, u.dir, u.fk)
	if res.Mode != ModeDurable || res.Reason != ReasonNone || u.fk.calls() != 0 {
		t.Fatalf("%+v", res)
	}
	l := s2.Snapshot().Ledger
	if l.Generation != u.before.Generation+1 || !hasEntry(l, "db") {
		t.Fatal("the complete candidate document must be recovered (an update reported as failed is now visible)")
	}
	if !s2.PersistenceHealthy() || s2.Gate() != nil {
		t.Fatal("healthy after a valid load")
	}
	if err := s2.update(addEntries("cache")); err != nil || s2.Snapshot().Ledger.Generation != u.before.Generation+2 {
		t.Fatalf("the next update continues from the candidate generation: %v", err)
	}
}

func TestS2_DirSync_PartialOrMalformedCandidateIsNeverAccepted(t *testing.T) {
	u := newUncertain(t, -1)
	u.armed.Store(true)
	_ = u.s.update(addEntries("db"))
	cand := readLedgerBytes(t, u.dir)
	_ = u.s.Close()
	for name, content := range map[string][]byte{
		"truncated candidate": cand[:len(cand)/2],
		"candidate with a flipped byte": append(append([]byte{}, cand[:len(cand)-40]...),
			append([]byte{cand[len(cand)-40] ^ 0x01}, cand[len(cand)-39:]...)...),
		"empty file": {},
	} {
		t.Run(name, func(t *testing.T) {
			replaceFile(t, u.dir, content)
			s2, res, _, _ := openT(t, u.dir, u.fk, openOpts{cfg: constClock(stampT)})
			if res.Reason != ReasonCorrupt || res.Preserved == "" {
				t.Fatalf("a partial or malformed document must follow the corrupt path: %+v", res)
			}
			if b, _ := os.ReadFile(res.Preserved); !bytes.Equal(b, content) {
				t.Fatal("the bad file must be preserved byte for byte")
			}
			if len(s2.Snapshot().Ledger.Entries) != 0 {
				t.Fatal("nothing from a malformed document may be adopted")
			}
			_ = s2.Close()
			_ = os.Remove(res.Preserved)
		})
	}
}

// ---------------------------------------------------------------- 5. subsequent write and retry

func TestS2_DirSync_SubsequentWriteHealsUncertainState(t *testing.T) {
	t.Run("a later Update", func(t *testing.T) {
		u := newUncertain(t, -1)
		u.armed.Store(true)
		_ = u.s.update(addEntries("db"))
		candBytes := readLedgerBytes(t, u.dir)
		u.armed.Store(false)

		if err := u.s.update(addEntries("cache")); err != nil {
			t.Fatalf("the later write must succeed: %v", err)
		}
		mem := u.s.Snapshot()
		disk := mustLoad(t, readLedgerBytes(t, u.dir))
		if !reflect.DeepEqual(disk, mem.Ledger) {
			t.Fatal("after a successful write the disk holds exactly the published document")
		}
		if hasEntry(disk, "db") || !hasEntry(disk, "cache") || !hasEntry(disk, "web") {
			t.Fatal("the unacknowledged candidate content must be gone; the later update builds on the published state")
		}
		if mem.Ledger.Generation != u.before.Generation+1 {
			t.Fatalf("generation %d, want %d", mem.Ledger.Generation, u.before.Generation+1)
		}
		if sum(candBytes) == sum(readLedgerBytes(t, u.dir)) {
			t.Fatal("two different documents carried the same generation number; generation is a diagnostic counter")
		}
		if !u.s.PersistenceHealthy() || u.s.Gate() != nil || u.log.count(EventPersistenceRestored) != 1 || !mem.Durable {
			t.Fatal("health must be restored by the first successful write")
		}
	})
	t.Run("the background probe", func(t *testing.T) {
		u := newUncertain(t, -1)
		u.armed.Store(true)
		_ = u.s.update(addEntries("db"))
		u.armed.Store(false)
		u.ia.fireProbe(t)
		waitFor(t, func() bool { return u.s.PersistenceHealthy() })
		mem := u.s.Snapshot()
		disk := mustLoad(t, readLedgerBytes(t, u.dir))
		if !reflect.DeepEqual(disk, mem.Ledger) || hasEntry(disk, "db") {
			t.Fatal("the harmless probe write replaces the uncertain candidate with the published state")
		}
		if mem.Ledger.Generation != u.before.Generation+1 {
			t.Fatalf("generation %d", mem.Ledger.Generation)
		}
	})
}

func TestS2_DirSync_RetryHealsAndReportsSuccessOnlyAfterDurability(t *testing.T) {
	u := newUncertain(t, 1) // the first directory fsync fails, the retry succeeds
	var seen []byte
	var published atomic.Bool
	// Observe, at the moment of the failing fsync, that nothing is published yet.
	realHook := u.s.ops.syncDir
	u.s.ops.syncDir = func(dir string) error {
		err := realHook(dir)
		if u.armed.Load() && seen == nil {
			seen = readLedgerBytes(t, dir)
			if hasEntry(u.s.Snapshot().Ledger, "db") {
				published.Store(true)
			}
		}
		return err
	}
	u.armed.Store(true)
	// faultOps wraps syncDir at open time; the wrapper above sits outside it, so
	// the failure comes from the first call of the original hook.
	if err := u.s.update(addEntries("db")); err != nil {
		t.Fatalf("the retry should succeed: %v", err)
	}
	if published.Load() {
		t.Fatal("the candidate was visible in memory before the write was durable")
	}
	mem := u.s.Snapshot()
	if !hasEntry(mem.Ledger, "db") || mem.Ledger.Generation != u.before.Generation+1 {
		t.Fatal("after the successful retry the candidate is published")
	}
	if !bytes.Equal(seen, readLedgerBytes(t, u.dir)) {
		t.Fatal("the retry rewrites the same document, so the bytes are identical")
	}
	if !u.s.PersistenceHealthy() || u.log.count(EventPersistenceDegraded) != 0 {
		t.Fatal("a recovered retry never degrades health")
	}
}

// ---------------------------------------------------------------- 6. redelivery request

func TestS2_DirSync_RedeliveryRequestUncertainOutcome(t *testing.T) {
	setup := func(t *testing.T) (*uncertain, Key) {
		u := newUncertain(t, -1)
		k := Key{Service: "web", Provider: "p", Secret: "s"}
		u.armed.Store(true)
		id, err := u.s.RequestRedelivery(k, "alice", "stuck rollout")
		if err == nil || id != "" || !errors.Is(err, ErrPersistenceFailed) {
			t.Fatalf("a request that cannot be confirmed durable must fail: %q %v", id, err)
		}
		if e, _ := u.s.Get(k); e.Request != nil {
			t.Fatal("memory must not hold the request")
		}
		cand := mustLoad(t, readLedgerBytes(t, u.dir))
		ce := findEntry(cand, k)
		if ce == nil || ce.Request == nil || ce.Request.State != RedeliveryRequested || ce.Request.Actor != "alice" {
			t.Fatal("the candidate on disk holds the complete request")
		}
		_ = u.s.Close()
		return u, k
	}
	t.Run("candidate outcome: restart cancels the undispatched request", func(t *testing.T) {
		u, k := setup(t)
		s2, res, log, _ := openT(t, u.dir, u.fk)
		if res.CancelledRequests != 1 {
			t.Fatalf("%+v", res)
		}
		e, _ := s2.Get(k)
		if e.Request == nil || e.Request.State != RedeliveryCancelled || e.Request.Reason != CancelReasonRestart || e.Request.Actor != "alice" {
			t.Fatalf("persisted request: %+v", e.Request)
		}
		d := eventDetails(log, EventRequestCancelled)
		if len(d) != 1 || !strings.Contains(d[0], `actor="alice"`) || !strings.Contains(d[0], `reason="stuck rollout"`) {
			t.Fatalf("the event must carry the original actor and reason: %q", d)
		}
		if e.Applied != nil || e.State != EntryIdle {
			t.Fatal("a request never changes applied or state")
		}
		if got := s2.Snapshot().Ledger.Generation; got != u.before.Generation+2 {
			t.Fatalf("generation %d: candidate plus the cancellation write", got)
		}
	})
	t.Run("old outcome: no request exists", func(t *testing.T) {
		u, k := setup(t)
		replaceFile(t, u.dir, u.oldBytes)
		s2, res, log, _ := openT(t, u.dir, u.fk)
		if res.CancelledRequests != 0 || log.count(EventRequestCancelled) != 0 {
			t.Fatalf("%+v", res)
		}
		if e, _ := s2.Get(k); e.Request != nil || s2.Snapshot().Ledger.Generation != u.before.Generation {
			t.Fatal("the previous document has no request")
		}
	})
}
