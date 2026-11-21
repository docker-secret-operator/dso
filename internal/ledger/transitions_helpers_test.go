package ledger

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Deterministic, Docker-free scaffolding for the S4a facade tests. A "crash"
// is Close followed by a reopen of the same directory; write faults are
// injected at the store's file-operation seam (faultOps).

var s4Key = []byte("0123456789abcdef0123456789abcdef") // 32 bytes, test only

const s4Secret = "S4-PLAINTEXT-SECRET-VALUE-7f3a9c" // must never appear anywhere

// fakeKM is a KeyMaterialSource returning a copy of a fixed key.
type fakeKM struct {
	id  string
	key []byte
	err error
}

func (f fakeKM) KeyMaterial() (string, []byte, error) {
	if f.err != nil {
		return "", nil, f.err
	}
	return f.id, append([]byte(nil), f.key...), nil
}

var s4K = Key{Project: "shop", Service: "api", Provider: "p", Secret: "s"}

func s4Live(n int) []LiveReplica {
	var out []LiveReplica
	for i := 1; i <= n; i++ {
		d := string(rune('0' + i))
		out = append(out, LiveReplica{Number: d, ContainerID: "c" + d, ContainerName: "shop-api-" + d, Created: "2026-01-01T00:00:0" + d + "Z", Eligible: true})
	}
	return out
}

// rig holds one ledger directory, its store and facade, and a fault switch.
type rig struct {
	t     *testing.T
	dir   string
	keys  *fakeKeys
	km    fakeKM
	s     *Store
	v     *Versions
	res   OpenResult
	log   *eventLog
	logs  []*eventLog  // every log of every open, for scans
	fault atomic.Value // string: "", "create", "rename", "syncdir"
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, dir: stateDir(t), keys: newFakeKeys(validKey(s2KeyA)), km: fakeKM{id: s2KeyA, key: s4Key}}
	r.fault.Store("")
	r.open()
	return r
}

func (r *rig) open() {
	r.t.Helper()
	r.s, r.res, r.log, _ = openT(r.t, r.dir, r.keys, openOpts{ops: faultOps(func(op string) error {
		if f, _ := r.fault.Load().(string); f != "" && f == op {
			return errors.New("injected " + op + " fault")
		}
		return nil
	})})
	r.logs = append(r.logs, r.log)
	v, err := NewVersions(r.s, r.km)
	if err != nil {
		r.t.Fatal(err)
	}
	r.v = v
}

// crash closes the store and reopens the same directory.
func (r *rig) crash() {
	r.t.Helper()
	_ = r.s.Close()
	r.open()
}

func (r *rig) ver(value string) Version {
	r.t.Helper()
	v, err := r.v.NewVersion(s4K.Provider, s4K.Secret, map[string]string{"password": value})
	if err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *rig) entry() Entry {
	r.t.Helper()
	e, ok := r.v.Get(s4K)
	if !ok {
		r.t.Fatal("entry missing")
	}
	return e
}

func (r *rig) decide() Decision { return r.v.NeedsRotation(s4K) }

func (r *rig) mustObserve(ver Version, live []LiveReplica) ObserveResult {
	r.t.Helper()
	res, err := r.v.Observe(s4K, ver, live)
	if err != nil {
		r.t.Fatalf("Observe: %v", err)
	}
	return res
}

// deliver runs a complete successful rotation for ver as the engine would
// (Correlate after the journal start, verified delivery after the journal
// commit) and returns the operation id.
func (r *rig) deliver(rid string, ver Version) {
	r.t.Helper()
	e := r.entry()
	if err := r.v.Correlate(s4K, rid, e.DesiredSeq, ver); err != nil {
		r.t.Fatalf("Correlate: %v", err)
	}
	if _, err := r.v.RecordDeliveryVerified(s4K, evOK(rid, ver)); err != nil {
		r.t.Fatalf("RecordDeliveryVerified: %v", err)
	}
}

// baseline returns a rig whose entry has D1 applied and verified on 2
// replicas, idle.
func baseline(t *testing.T) (*rig, Version) {
	t.Helper()
	r := newRig(t)
	d1 := r.ver("D1")
	r.mustObserve(d1, s4Live(2))
	r.deliver("rot-0000000000000d01", d1)
	if e := r.entry(); e.State != EntryIdle || e.Applied == nil || Compare(*e.Applied, d1) != Same {
		t.Fatalf("baseline not established: %+v", e)
	}
	return r, d1
}

func evOK(rid string, ver Version) Evidence {
	return Evidence{RotationID: rid, Kind: EvidenceRestartVerified, Status: JournalCompleted, Seq: 5, Version: ver}
}

// jrec is a journal record; ready is candidateVerified(Transition) for
// restart/rolling and Transition == signal_done for signal.
func jrec(rid, strategy string, st JournalStatus, seq uint64, ready bool, ver *Version) JournalRecord {
	return JournalRecord{RotationID: rid, Strategy: strategy, Status: st, Seq: seq, Version: ver,
		CandidateVerified: strategy != "signal" && ready, SignalDone: strategy == "signal" && ready,
		UpdatedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// requireVocabulary asserts the closed reason vocabulary over the document.
func requireVocabulary(t *testing.T, l *Ledger) {
	t.Helper()
	for _, e := range l.Entries {
		for _, r := range e.Replicas {
			if r.Reason != "" && !ValidOutcomeReason(r.Reason) {
				t.Errorf("replica reason %q is outside the closed vocabulary", r.Reason)
			}
		}
		if e.LastOutcome != nil && e.LastOutcome.Reason != "" && !ValidOutcomeReason(e.LastOutcome.Reason) {
			t.Errorf("last outcome reason %q is outside the closed vocabulary", e.LastOutcome.Reason)
		}
	}
}

func requireErr(t *testing.T, err, want error, what string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: want %v, got %v", what, want, err)
	}
}

func readDirNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out, err
}

func readFileIfRegular(p string) ([]byte, error) {
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return os.ReadFile(p) // #nosec G304 -- test-owned temporary directory
}
