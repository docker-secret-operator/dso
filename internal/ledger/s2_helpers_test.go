package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// Test scaffolding for the S2 store. Nothing here touches Docker, systemd,
// /run/dso or any system configuration: every test works in its own private
// directory under the test temp base.

const (
	s2KeyA = "k-aaaaaaaaaaaaaaaa"
	s2KeyB = "k-bbbbbbbbbbbbbbbb"
)

// fakeKeys is the fake KeyProvider of owner decision D-S2-1.
type fakeKeys struct {
	mu       sync.Mutex
	state    KeyInspection
	genID    string
	genErr   error
	genCalls int
	onGen    func()
}

func newFakeKeys(st KeyInspection) *fakeKeys { return &fakeKeys{state: st, genID: s2KeyB} }

func (f *fakeKeys) Inspect() KeyInspection {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeKeys) Generate() (string, error) {
	f.mu.Lock()
	f.genCalls++
	on, id, err := f.onGen, f.genID, f.genErr
	f.mu.Unlock()
	if on != nil {
		on()
	}
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	f.state = KeyInspection{State: KeyValid, KeyID: id}
	f.mu.Unlock()
	return id, nil
}

func (f *fakeKeys) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.genCalls
}

func validKey(id string) KeyInspection { return KeyInspection{State: KeyValid, KeyID: id} }

// stateDir returns a private (mode 0700) directory whose whole chain passes the
// securefs check, or skips the test when the environment's temp base cannot
// provide one.
func stateDir(t *testing.T) string {
	t.Helper()
	if !securefs.Supported {
		t.Skip("securefs is unsupported on this platform")
	}
	d, err := os.MkdirTemp("", "ledger-s2-")
	if err != nil {
		t.Skipf("no temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := securefs.CheckDirChain(d); err != nil {
		t.Skipf("the temp base is not a safe state-directory chain here: %v", err)
	}
	return d
}

// instantAfter records every requested delay. Retry delays (below the probe
// minimum) fire immediately. Probe waits (5 s and more) block until the test
// releases them with fireProbe, so a degraded store never spins.
type instantAfter struct {
	mu      sync.Mutex
	delays  []time.Duration
	pending []chan time.Time
}

func (a *instantAfter) after(d time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	a.mu.Lock()
	a.delays = append(a.delays, d)
	if d >= probeMin {
		a.pending = append(a.pending, c)
	} else {
		c <- time.Now()
	}
	a.mu.Unlock()
	return c
}

// fireProbe releases the oldest pending probe wait, waiting for one to appear.
func (a *instantAfter) fireProbe(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		if len(a.pending) > 0 {
			c := a.pending[0]
			a.pending = a.pending[1:]
			a.mu.Unlock()
			c <- time.Now()
			return
		}
		a.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no probe wait appeared")
}

func (a *instantAfter) got() []time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]time.Duration(nil), a.delays...)
}

// eventLog collects events.
type eventLog struct {
	mu sync.Mutex
	ev []Event
}

func (e *eventLog) add(ev Event) {
	e.mu.Lock()
	e.ev = append(e.ev, ev)
	e.mu.Unlock()
}

func (e *eventLog) kinds() []EventKind {
	e.mu.Lock()
	defer e.mu.Unlock()
	var k []EventKind
	for _, ev := range e.ev {
		k = append(k, ev.Kind)
	}
	return k
}

func (e *eventLog) count(k EventKind) int {
	n := 0
	for _, x := range e.kinds() {
		if x == k {
			n++
		}
	}
	return n
}

// fixedClock returns a clock that advances one second per call from a fixed
// start, so preserved-file names and timestamps are deterministic.
func fixedClock() func() time.Time {
	var mu sync.Mutex
	t := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t = t.Add(time.Second)
		return t
	}
}

// fixedStamp is the UTC stamp the first clock reading of fixedClock formats to
// when preserve is the first thing to read the clock after Open's own reads;
// tests that need the exact name compute it from the listing instead.
func preservedNames(t *testing.T, dir, kind string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "ledger.json."+kind+"-") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// dirFingerprint is a deterministic digest of every object in dir (name, type,
// mode, size and content), used to prove "left byte-for-byte untouched".
func dirFingerprint(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() == "ledger.lock" {
			continue // created by Open itself; its content is empty by design
		}
		p := filepath.Join(dir, e.Name())
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(h, "%s|%v|%o|%d|", e.Name(), fi.Mode().Type(), fi.Mode().Perm(), fi.Size())
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			h.Write(b)
		} else if fi.Mode()&os.ModeSymlink != 0 {
			tgt, _ := os.Readlink(p)
			h.Write([]byte(tgt))
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readLedgerBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// openOpts customizes openT.
type openOpts struct {
	cfg func(*Config)
	ops func(*fileOps)
}

// openT opens a store, always with an instant After, a fixed clock and a
// collecting event hook; it registers Close as cleanup.
func openT(t *testing.T, dir string, keys KeyProvider, o ...openOpts) (*Store, OpenResult, *eventLog, *instantAfter) {
	t.Helper()
	log := &eventLog{}
	ia := &instantAfter{}
	cfg := Config{Keys: keys, Now: fixedClock(), After: ia.after, Events: log.add}
	ops := realOps()
	for _, x := range o {
		if x.ops != nil {
			x.ops(ops)
		}
		if x.cfg != nil {
			x.cfg(&cfg)
		}
	}
	cfg.ops = ops
	s, res, err := Open(dir, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, res, log, ia
}

// seedLedger persists a valid ledger bound to keyID with the given entries
// through a throw-away store, then closes it, returning the file bytes.
func seedLedger(t *testing.T, dir, keyID string, entries ...Entry) []byte {
	t.Helper()
	s, _, _, _ := openT(t, dir, newFakeKeys(validKey(keyID)))
	if err := s.Update(func(l *Ledger) error {
		l.Entries = append(l.Entries, entries...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return readLedgerBytes(t, dir)
}

// s2Entry is a valid idle entry with an observed provider-id desired version.
func s2Entry(service string) Entry {
	e := idleEntry("", service, "p", "s")
	e.Desired = &Version{Kind: KindProvider, ID: "pv-1"}
	e.DesiredSeq = 1
	e.DesiredObservedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return e
}

func addEntries(names ...string) func(*Ledger) error {
	return func(l *Ledger) error {
		for _, n := range names {
			l.Entries = append(l.Entries, s2Entry(n))
		}
		return nil
	}
}

// failOnce returns an error for the first n calls it counts.
type failN struct {
	mu    sync.Mutex
	n     int
	calls int
	err   error
}

func (f *failN) hit() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.n {
		return f.err
	}
	return nil
}

func (f *failN) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
