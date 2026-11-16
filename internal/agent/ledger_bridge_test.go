package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/ledger"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/securefs"
)

// S4b bridge tests (docs/design/m1-ledger-reconciliation.md section 11). Every
// test drives a REAL StateTracker in a temp directory (no tracker mock) and a
// REAL ledger.Store with the real S3 FileKeyProvider. A crash is "close the
// ledger store and reopen the same directory" and/or "construct a new
// StateTracker on the same state directory" (a new process inherits the
// journal). M0 recovery is either the real AutomaticRecovery against the
// Docker-free fake daemon (recovery_safety_test.go) or the journal marks it
// would write (MarkRecoveredRecord / MarkCriticalRecord), as the design's
// harness allows. Docker is never touched.
//
// "Dispatch" is modelled as the decision the S6 wiring will make:
// ledger.NeedsRotation says rotate AND the M0 operation guard does not block
// the service. Trigger wiring itself is S6.
//
// O-2 (the journal records the delivered SecretVersion) is not implemented in
// M0. Tests marked "O-2 emulated" put the version into the journal through the
// real StateTracker.Record (JournalEvent.SecretVersion, a field M0 already
// has); the "M0 as-is" variants do not, and document the cost.

const (
	lbProvider = "vault"
	lbSecret   = "db"
	lbService  = "app"
)

var lbKey = ledger.Key{Project: "shop", Service: lbService, Provider: lbProvider, Secret: lbSecret}

// lbClock is the ledger clock: real time plus an adjustable offset.
type lbClock struct{ off atomic.Int64 }

func (c *lbClock) now() time.Time { return time.Now().Add(time.Duration(c.off.Load())) }

// lbAfter fires at once for the store's write retries (< 5 s) and never for
// the background probe, which Close stops.
func lbAfter(d time.Duration) <-chan time.Time {
	if d >= 5*time.Second {
		return nil
	}
	c := make(chan time.Time, 1)
	c <- time.Now()
	return c
}

type lbRig struct {
	t     *testing.T
	ldir  string
	jdir  string
	clock *lbClock
	keys  *ledger.FileKeyProvider
	s     *ledger.Store
	v     *ledger.Versions
	res   ledger.OpenResult
	st    *StateTracker
	f     *recFixture // set when a fake daemon runs the real AutomaticRecovery
	// lastRID is the rotation id of the most recent journalStart: the engine
	// finishes a dispatch by ITS rotation id (F-S6-1), not by provider:secret:ids.
	lastRID string
}

func lbLedgerDir(t *testing.T) string {
	t.Helper()
	if !securefs.Supported {
		t.Skip("securefs is unsupported on this platform")
	}
	d, err := os.MkdirTemp("", "dso-ledger-bridge-")
	if err != nil {
		t.Skipf("no temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(d, 0o700)
		_ = os.RemoveAll(d)
	})
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := securefs.CheckDirChain(d); err != nil {
		t.Skipf("the temp base is not a safe state-directory chain here: %v", err)
	}
	return d
}

// newLBRig opens a fresh ledger and a fresh journal. With a fake daemon the
// journal is the recovery fixture's own StateTracker.
func newLBRig(t *testing.T, d *fakeDaemon) *lbRig {
	t.Helper()
	r := &lbRig{t: t, ldir: lbLedgerDir(t), clock: &lbClock{}}
	kp, err := ledger.NewFileKeyProvider(r.ldir)
	if err != nil {
		t.Fatal(err)
	}
	r.keys = kp
	r.openLedger()
	if d != nil {
		r.f = newRec(t, d, "-")
		r.st = r.f.st
		r.jdir = r.f.st.stateDir
	} else {
		r.jdir = t.TempDir()
		r.openJournal()
	}
	t.Cleanup(func() { _ = r.s.Close() })
	return r
}

func (r *lbRig) openLedger() {
	r.t.Helper()
	s, res, err := ledger.Open(r.ldir, ledger.Config{Keys: r.keys, Now: r.clock.now, After: lbAfter})
	if err != nil {
		r.t.Fatal(err)
	}
	v, err := ledger.NewVersions(s, r.keys)
	if err != nil {
		r.t.Fatal(err)
	}
	r.s, r.v, r.res = s, v, res
}

func (r *lbRig) openJournal() {
	r.t.Helper()
	st, err := NewStateTracker(r.jdir, zap.NewNop())
	if err != nil {
		r.t.Fatal(err)
	}
	r.st = st
	if r.f != nil {
		r.f.st, r.f.ar.st = st, st
	}
}

// crash simulates a process death: both stores are reopened from disk.
func (r *lbRig) crash() {
	r.t.Helper()
	_ = r.s.Close()
	r.openLedger()
	r.openJournal()
}

// recover runs M0 recovery: the real AutomaticRecovery when a fake daemon is
// attached, else nothing (callers mark records themselves).
func (r *lbRig) recover() {
	r.t.Helper()
	if r.f != nil {
		r.f.run()
	}
}

func (r *lbRig) ver(value string) ledger.Version {
	r.t.Helper()
	v, err := r.v.NewVersion(lbProvider, lbSecret, map[string]string{"password": value})
	if err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *lbRig) entry() ledger.Entry {
	r.t.Helper()
	e, ok := r.v.Get(lbKey)
	if !ok {
		r.t.Fatal("ledger entry missing")
	}
	return e
}

func (r *lbRig) generation() uint64 { return r.s.Health().Generation }

func (r *lbRig) record(rid string) *RotationState { return JournalRecordByID(r.st, rid) }

func (r *lbRig) journalBytes() []byte {
	b, err := os.ReadFile(filepath.Join(r.jdir, "rotations.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.t.Fatal(err)
	}
	return b
}

// blocked is the M0 operation guard's verdict for the service (keys as
// TriggerReload passes them: service name and container ids).
func (r *lbRig) blocked() bool {
	_, b := newJournalGuard(r.st).RotationBlocked(lbService, "o0", "o1", "c1", "c2")
	return b
}

// wouldDispatch is the S6 decision: the ledger wants a rotation and the guard
// permits it.
func (r *lbRig) wouldDispatch() bool {
	return r.v.NeedsRotation(lbKey).Rotate() && !r.blocked()
}

func (r *lbRig) reconcile() ledger.ReconcileReport {
	r.t.Helper()
	before := r.journalBytes()
	rep, err := ReconcileLedgerFromTracker(r.v, r.st, LedgerReconcileInput{Now: time.Now()})
	if err != nil {
		r.t.Fatalf("reconcile: %v", err)
	}
	if !bytes.Equal(before, r.journalBytes()) {
		r.t.Fatal("the bridge wrote the journal")
	}
	return rep
}

// reconcileTwice runs the startup reconcile and asserts that a second run
// changes nothing (idempotence: no write, empty report).
func (r *lbRig) reconcileTwice() ledger.ReconcileReport {
	r.t.Helper()
	rep := r.reconcile()
	g := r.generation()
	again := r.reconcile()
	if len(again.Changes) != 0 || r.generation() != g {
		r.t.Fatalf("second reconcile changed the ledger: %+v (generation %d -> %d)", again.Changes, g, r.generation())
	}
	return rep
}

func lbCreated(id string) string {
	n := 0
	for _, c := range id {
		n += int(c)
	}
	return fmt.Sprintf("2026-01-01T00:%02d:%02dZ", (n/60)%60, n%60)
}

func lbLive(ids ...string) []ledger.LiveReplica {
	var out []ledger.LiveReplica
	for i, id := range ids {
		name := lbService
		if len(ids) > 1 {
			name = fmt.Sprintf("shop-app-%d", i+1)
		}
		out = append(out, ledger.LiveReplica{Number: strconv.Itoa(i + 1), ContainerID: id, ContainerName: name, Created: lbCreated(id), Eligible: true})
	}
	return out
}

func (r *lbRig) observe(ver ledger.Version, live []ledger.LiveReplica) ledger.ObserveResult {
	r.t.Helper()
	res, err := r.v.Observe(lbKey, ver, live)
	if err != nil {
		r.t.Fatalf("Observe: %v", err)
	}
	return res
}

// lbFlow describes one dispatch.
type lbFlow struct {
	strategy string // restart, rolling, signal
	orig     string // original container id (journal key)
	cand     string // candidate container id (orig for signal)
	o2       bool   // emulate O-2: the journal carries the delivered version
}

func lbSteps(strategy string) []string {
	switch strategy {
	case "restart":
		return []string{rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated,
			rotation.TrIntentStopOriginal, rotation.TrOriginalStopped, rotation.TrIntentStartReplacement,
			rotation.TrReplacementStarted, rotation.TrCandidateReady, rotation.TrIntentCleanup, rotation.TrCompleted}
	case "rolling":
		return []string{rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrCandidateReady,
			rotation.TrIntentHandoff, rotation.TrHandoffDone, rotation.TrIntentSwap, rotation.TrSwapCommitted,
			rotation.TrIntentCleanup, rotation.TrCompleted}
	case "signal":
		return []string{rotation.TrIntentSignalPublish, rotation.TrSignalPublished, rotation.TrIntentSignalReload,
			rotation.TrSignalReloadSent, rotation.TrSignalDone}
	}
	return nil
}

// journalStart is step 3: StartRotationWithID through the real tracker.
func (r *lbRig) journalStart(rid string, fl lbFlow) error {
	r.lastRID = rid
	return r.st.StartRotationWithID(rid, lbProvider, lbSecret, fl.orig, "")
}

// journalTo is step 5: the strategy's journal transitions up to and including
// upTo, written through the real StateTracker.Record.
func (r *lbRig) journalTo(rid string, fl lbFlow, ver ledger.Version, upTo string) {
	r.t.Helper()
	ctx := rotation.WithRotationID(context.Background(), rid)
	for i, tr := range lbSteps(fl.strategy) {
		ev := rotation.JournalEvent{RotationID: rid, Strategy: fl.strategy, Transition: tr}
		if i == 0 {
			ev.ServiceID, ev.OriginalIDs = lbService, []string{fl.orig}
			if fl.o2 {
				ev.SecretVersion = FormatJournalSecretVersion(ver)
			}
		}
		if fl.strategy != "signal" && i > 0 {
			ev.CandidateIDs = []string{fl.cand}
		}
		if err := r.st.Record(ctx, ev); err != nil {
			r.t.Fatal(err)
		}
		if tr == upTo {
			return
		}
	}
	if upTo != "" {
		r.t.Fatalf("unknown step %s for %s", upTo, fl.strategy)
	}
}

func lastStep(strategy string) string { s := lbSteps(strategy); return s[len(s)-1] }

// correlate is step 4.
func (r *lbRig) correlate(rid string, ver ledger.Version, startErr error) error {
	return CorrelateAfterJournalStart(r.v, r.st, startErr, lbKey, rid, r.entry().DesiredSeq, ver)
}

// finishOK is step 6 on success (finishRotation -> CompleteRotation).
func (r *lbRig) finishOK(fl lbFlow) {
	r.t.Helper()
	if err := r.st.CompleteRotationID(r.lastRID); err != nil {
		r.t.Fatal(err)
	}
}

// terminal is step 7 (in process, from finishRotation).
func (r *lbRig) terminal(rid string, hint *ledger.Version, fl lbFlow) (BridgeResult, error) {
	before := r.journalBytes()
	res, err := RecordRotationOutcome(r.v, r.st, lbKey, rid, hint, false, lbLive(fl.cand))
	if !bytes.Equal(before, r.journalBytes()) {
		r.t.Fatal("the bridge wrote the journal")
	}
	return res, err
}

// rotate runs one complete successful dispatch (steps 3 to 7) and checks it
// verified the delivery.
func (r *lbRig) rotate(rid string, ver ledger.Version, fl lbFlow) {
	r.t.Helper()
	if err := r.correlate(rid, ver, r.journalStart(rid, fl)); err != nil {
		r.t.Fatalf("correlate: %v", err)
	}
	r.journalTo(rid, fl, ver, lastStep(fl.strategy))
	r.finishOK(fl)
	res, err := r.terminal(rid, &ver, fl)
	if err != nil || res.Outcome != OutcomeDeliveryVerified {
		r.t.Fatalf("terminal: %+v %v", res, err)
	}
}

// baseline: D1 observed on a pre-existing container o0, then a verified
// restart delivery of D1 (R0: o0 -> o1). Afterwards applied == desired == D1,
// the live container is o1. It returns D1.
func (r *lbRig) baseline(o2 bool) ledger.Version {
	r.t.Helper()
	d1 := r.ver("one")
	r.observe(d1, lbLive("o0"))
	r.rotate("rot-r0", d1, lbFlow{strategy: "restart", orig: "o0", cand: "o1", o2: o2})
	e := r.entry()
	if e.State != ledger.EntryIdle || e.Applied == nil || ledger.Compare(*e.Applied, d1) != ledger.Same {
		r.t.Fatalf("baseline not established: %+v", e)
	}
	return d1
}

func appliedIs(e ledger.Entry, v ledger.Version) bool {
	return e.Applied != nil && ledger.Compare(*e.Applied, v) == ledger.Same
}

func rules(rep ledger.ReconcileReport) []string {
	var out []string
	for _, c := range rep.Changes {
		out = append(out, c.Rule)
	}
	sort.Strings(out)
	return out
}

func hasRule(rep ledger.ReconcileReport, rule string) bool {
	for _, c := range rep.Changes {
		if c.Rule == rule {
			return true
		}
	}
	return false
}

// ============================================================ T-EV

// lbTransitions classifies EVERY rotation.Tr* constant: true when
// candidateVerified must accept it. TestLedgerBridge_TEV_TableCoversEveryTransition
// fails when a transition is added to internal/rotation without a row here.
var lbTransitions = map[string]bool{
	rotation.TrIntentCreateCandidate:  false,
	rotation.TrCandidateCreated:       false,
	rotation.TrCandidateReady:         true,
	rotation.TrIntentHandoff:          true,
	rotation.TrHandoffDone:            true,
	rotation.TrIntentSwap:             true,
	rotation.TrSwapCommitted:          true,
	rotation.TrIntentCleanup:          true,
	rotation.TrCompleted:              true,
	rotation.TrIntentRollback:         false,
	rotation.TrRolledBack:             false,
	rotation.TrFailed:                 false,
	rotation.TrIntentRenameOriginal:   false,
	rotation.TrIntentStopOriginal:     false,
	rotation.TrOriginalStopped:        false,
	rotation.TrIntentStartReplacement: false,
	rotation.TrReplacementStarted:     false,
	rotation.TrIntentSignalPublish:    false,
	rotation.TrSignalPublished:        false,
	rotation.TrIntentSignalReload:     false,
	rotation.TrSignalReloadSent:       false,
	rotation.TrSignalDone:             false, // signal evidence, not a verified candidate
	rotation.TrSignalFailed:           false,
}

// rotationTransitionConstants parses internal/rotation/journal.go and returns
// every exported Tr* string constant (name -> value).
func rotationTransitionConstants(t *testing.T) map[string]string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller information")
	}
	path := filepath.Join(filepath.Dir(file), "..", "rotation", "journal.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if !strings.HasPrefix(n.Name, "Tr") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("%s is not a string literal constant", n.Name)
				}
				v, _ := strconv.Unquote(lit.Value)
				out[n.Name] = v
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("only %d Tr* constants found: the scan is broken", len(out))
	}
	return out
}

func TestLedgerBridge_TEV_TableCoversEveryTransition(t *testing.T) {
	consts := rotationTransitionConstants(t)
	seen := map[string]bool{}
	for name, val := range consts {
		want, ok := lbTransitions[val]
		if !ok {
			t.Errorf("rotation.%s (%q) has no row in lbTransitions: classify it (is it delivery evidence?)", name, val)
			continue
		}
		seen[val] = true
		if got := candidateVerified(val); got != want {
			t.Errorf("candidateVerified(%q) = %v, the table says %v", val, got, want)
		}
	}
	for val := range lbTransitions {
		if !seen[val] {
			t.Errorf("lbTransitions row %q is not a rotation.Tr* constant", val)
		}
	}
}

// lbExpectAccept is the evidence rule of design 4.2 written out once, for the
// cross-check only.
func lbExpectAccept(strategy, tr, status string, committed bool) bool {
	switch strategy {
	case "restart", "rolling":
		if !lbTransitions[tr] {
			return false
		}
		return status == "completed" || (committed && (status == "recovered" || status == "in_progress"))
	case "signal":
		return tr == rotation.TrSignalDone && status == "completed"
	}
	return false
}

var lbStatuses = []string{"in_progress", "completed", "rollback_required", "recovered", "critical_error", "bogus"}

// The bridge's acceptance equals candidateVerified (restart/rolling) and
// signal_done (signal) under the terminal status rule, for every transition x
// strategy x status x committed; and the ledger facade (RecordDeliveryVerified)
// and its reconcile (W6/W9) accept exactly the same records.
func TestLedgerBridge_TEV_BridgeLedgerAndReconcileAgree(t *testing.T) {
	r := newLBRig(t, nil)
	type tuple struct {
		kind      ledger.EvidenceKind
		status    string
		committed bool
	}
	ledgerAccept := map[tuple]bool{}
	n := 0
	key := func(prefix string) ledger.Key {
		n++
		return ledger.Key{Project: "ev", Service: "svc", Provider: lbProvider, Secret: fmt.Sprintf("%s-%d", prefix, n)}
	}
	verFor := func(k ledger.Key) ledger.Version {
		v, err := r.v.NewVersion(k.Provider, k.Secret, map[string]string{"v": "x"})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	prepare := func(k ledger.Key, rid string) ledger.Version {
		v := verFor(k)
		if _, err := r.v.Observe(k, v, lbLive("o1")); err != nil {
			t.Fatal(err)
		}
		if err := r.v.Correlate(k, rid, 1, v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	// The ledger's own verdict for one evidence tuple, through the real facade.
	ledgerVerdict := func(tp tuple) bool {
		if v, ok := ledgerAccept[tp]; ok {
			return v
		}
		k := key("facade")
		rid := fmt.Sprintf("rot-f%d", n)
		v := prepare(k, rid)
		ev := ledger.Evidence{RotationID: rid, Kind: tp.kind, Status: ledger.JournalStatus(tp.status), Committed: tp.committed, Seq: 1, Version: v}
		_, err := r.v.RecordDeliveryVerified(k, ev)
		if err != nil && !errors.Is(err, ledger.ErrNoEvidence) {
			t.Fatalf("unexpected facade error for %+v: %v", tp, err)
		}
		ledgerAccept[tp] = err == nil
		return err == nil
	}

	// Reconcile cross-check: once per distinct reconcile input (the ledger's
	// JournalRecord sees strategy, candidateVerified, signal_done, status and
	// committed), on one small ledger per strategy.
	type rtuple struct {
		strategy  string
		cv, sd    bool
		status    string
		committed bool
	}
	reconWant := map[rtuple]bool{}
	check := func(rt rtuple, accept bool, label string) {
		if prev, ok := reconWant[rt]; ok && prev != accept {
			t.Errorf("%s: bridge verdict differs between transitions with the same reconcile input", label)
		}
		reconWant[rt] = accept
	}
	checked := 0
	hint := r.ver("hint")
	for _, strategy := range []string{"restart", "rolling", "signal", "unknown"} {
		for tr := range lbTransitions {
			for _, status := range lbStatuses {
				for _, committed := range []bool{false, true} {
					label := fmt.Sprintf("%s/%s/%s/committed=%v", strategy, tr, status, committed)
					rec := &RotationState{RotationID: "rot-x", Strategy: strategy, Transition: tr, Status: status, Committed: committed, Seq: 3, LastUpdate: time.Now()}
					_, berr := EvidenceForRotation(rec, &hint, false, nil)
					bridge := berr == nil
					want := lbExpectAccept(strategy, tr, status, committed)
					if bridge != want {
						t.Errorf("%s: bridge accepts=%v, rule says %v (%v)", label, bridge, want, berr)
					}
					if berr != nil && !errors.Is(berr, ledger.ErrNoEvidence) {
						t.Errorf("%s: a refusal must wrap ledger.ErrNoEvidence: %v", label, berr)
					}
					if (strategy == "restart" || strategy == "rolling") && status == "completed" && bridge != candidateVerified(tr) {
						t.Errorf("%s: bridge acceptance diverges from candidateVerified", label)
					}
					tp := tuple{journalEvidenceKind(rec), status, committed}
					if lv := ledgerVerdict(tp); lv != bridge {
						t.Errorf("%s: bridge accepts=%v but the ledger facade accepts=%v", label, bridge, lv)
					}
					check(rtuple{strategy, candidateVerified(tr), tr == rotation.TrSignalDone, status, committed}, bridge, label)
					checked++
				}
			}
		}
	}
	for _, strategy := range []string{"restart", "rolling", "signal", "unknown"} {
		rr := newLBRig(t, nil)
		type recon struct {
			k  ledger.Key
			rt rtuple
		}
		var recs []recon
		var journal []ledger.JournalRecord
		for rt := range reconWant {
			if rt.strategy != strategy {
				continue
			}
			n++
			k := ledger.Key{Project: "rc", Service: "svc", Provider: lbProvider, Secret: fmt.Sprintf("rc-%d", n)}
			rid := fmt.Sprintf("rot-r%d", n)
			v, err := rr.v.NewVersion(k.Provider, k.Secret, map[string]string{"v": "x"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rr.v.Observe(k, v, lbLive("o1")); err != nil {
				t.Fatal(err)
			}
			if err := rr.v.Correlate(k, rid, 1, v); err != nil {
				t.Fatal(err)
			}
			tr := rotation.TrIntentStartReplacement
			switch {
			case rt.cv:
				tr = rotation.TrCandidateReady
			case rt.sd:
				tr = rotation.TrSignalDone
			}
			jr := &RotationState{RotationID: rid, Strategy: strategy, Transition: tr, Status: rt.status, Committed: rt.committed,
				Seq: 3, LastUpdate: time.Now(), SecretVersion: FormatJournalSecretVersion(v)}
			journal = append(journal, JournalRecordFor(jr))
			recs = append(recs, recon{k: k, rt: rt})
		}
		rep, err := rr.v.ReconcileFromJournal(ledger.JournalSnapshot{Readable: true, Records: journal, Horizon: time.Now().Add(-ledgerJournalRetention)})
		if err != nil {
			t.Fatal(err)
		}
		byKey := map[ledger.Key]string{}
		for _, c := range rep.Changes {
			byKey[c.Key] = c.Rule
		}
		for _, rc := range recs {
			rule := byKey[rc.k]
			applied := rule == "W6" || rule == "W9"
			if applied != reconWant[rc.rt] {
				t.Errorf("%+v: reconcile rule %q, bridge accepts=%v", rc.rt, rule, reconWant[rc.rt])
			}
			e, _ := rr.v.Get(rc.k)
			if applied != (e.Applied != nil) {
				t.Errorf("%+v: applied advanced=%v, want %v", rc.rt, e.Applied != nil, applied)
			}
		}
	}
	if checked != 4*len(lbTransitions)*len(lbStatuses)*2 {
		t.Fatalf("checked %d combinations", checked)
	}
}

// Adoption verdicts: signal never adoption verified, file-mode unverified,
// restart/rolling env-mode verified.
func TestLedgerBridge_TEV_AdoptionVerdicts(t *testing.T) {
	cases := []struct {
		name     string
		strategy string
		fileMode bool
		want     ledger.AdoptionState
	}{
		{"restart env", "restart", false, ledger.AdoptionVerified},
		{"rolling env", "rolling", false, ledger.AdoptionVerified},
		{"restart file (N19)", "restart", true, ledger.AdoptionUnverified},
		{"signal", "signal", false, ledger.AdoptionUnverified},
		{"signal file", "signal", true, ledger.AdoptionUnverified},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newLBRig(t, nil)
			d1 := r.ver("one")
			r.observe(d1, lbLive("o1"))
			fl := lbFlow{strategy: c.strategy, orig: "o1", cand: "c1"}
			if c.strategy == "signal" {
				fl.cand = "o1"
			}
			if err := r.correlate("rot-a", d1, r.journalStart("rot-a", fl)); err != nil {
				t.Fatal(err)
			}
			r.journalTo("rot-a", fl, d1, lastStep(c.strategy))
			r.finishOK(fl)
			res, err := RecordRotationOutcome(r.v, r.st, lbKey, "rot-a", &d1, c.fileMode, nil)
			if err != nil || res.Outcome != OutcomeDeliveryVerified || res.Adoption != c.want {
				t.Fatalf("got %+v %v, want adoption %s", res, err, c.want)
			}
			for _, rp := range r.entry().Replicas {
				if rp.Adoption != c.want || rp.Delivery != ledger.DeliveryVerified {
					t.Fatalf("replica %+v", rp)
				}
			}
		})
	}
}

// Records produced by the REAL StateTracker for every transition: the bridge
// accepts exactly the candidateVerified ones (restart/rolling) and signal_done
// (signal), once the dispatch completed; a journal-only start (Seq 0) is never
// evidence.
func TestLedgerBridge_TEV_RealTrackerRecords(t *testing.T) {
	d := r0ver(t)
	for _, strategy := range []string{"restart", "rolling", "signal"} {
		for tr := range lbTransitions {
			for _, complete := range []bool{false, true} {
				st, err := NewStateTracker(t.TempDir(), zap.NewNop())
				if err != nil {
					t.Fatal(err)
				}
				if err := st.StartRotationWithID("rot-t", lbProvider, lbSecret, "o1", ""); err != nil {
					t.Fatal(err)
				}
				if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "rot-t", Strategy: strategy, Transition: tr, ServiceID: lbService}); err != nil {
					t.Fatal(err)
				}
				if complete {
					if err := st.CompleteRotation(lbProvider, lbSecret, "o1"); err != nil {
						t.Fatal(err)
					}
				}
				rec := JournalRecordByID(st, "rot-t")
				if jr := JournalRecordFor(rec); jr.CandidateVerified != candidateVerified(tr) || jr.SignalDone != (tr == rotation.TrSignalDone) || jr.Seq != 1 {
					t.Fatalf("JournalRecordFor(%s/%s) = %+v", strategy, tr, jr)
				}
				_, berr := EvidenceForRotation(rec, &d, false, nil)
				want := lbExpectAccept(strategy, tr, rec.Status, rec.Committed)
				if (berr == nil) != want {
					t.Errorf("%s/%s complete=%v status=%s: bridge accepts=%v want %v", strategy, tr, complete, rec.Status, berr == nil, want)
				}
			}
		}
	}
	st, err := NewStateTracker(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.StartRotationWithID("rot-z", lbProvider, lbSecret, "o1", "")
	_ = st.CompleteRotation(lbProvider, lbSecret, "o1")
	rec := JournalRecordByID(st, "rot-z")
	if _, err := EvidenceForRotation(rec, &d, false, nil); !errors.Is(err, ledger.ErrNoEvidence) {
		t.Fatalf("a completed record with Seq 0 must not be evidence: %v", err)
	}
	if InterruptCauseFor(rec) != ledger.CauseNeverRan {
		t.Fatalf("cause %s", InterruptCauseFor(rec))
	}
	if _, err := EvidenceForRotation(nil, &d, false, nil); !errors.Is(err, ledger.ErrNoEvidence) {
		t.Fatalf("nil record: %v", err)
	}
}

// r0ver returns a syntactically valid version for checks that never reach a store.
func r0ver(t *testing.T) ledger.Version {
	t.Helper()
	v, err := ledger.NewHMACVersion("k-0123456789abcdef", bytes.Repeat([]byte{7}, ledger.HMACKeySize), lbProvider, lbSecret, map[string]string{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLedgerBridge_JournalVersionEncoding(t *testing.T) {
	h := r0ver(t)
	s := FormatJournalSecretVersion(h)
	if !strings.HasPrefix(s, "hmac:k-0123456789abcdef:") {
		t.Fatalf("encoding %q", s)
	}
	if p := parseJournalSecretVersion(s); p == nil || ledger.Compare(*p, h) != ledger.Same {
		t.Fatalf("round trip failed: %+v", p)
	}
	pv := ledger.Version{Kind: ledger.KindProvider, ID: "v:42"}
	if p := parseJournalSecretVersion(FormatJournalSecretVersion(pv)); p == nil || ledger.Compare(*p, pv) != ledger.Same {
		t.Fatalf("provider round trip failed: %+v", p)
	}
	if FormatJournalSecretVersion(ledger.Version{Kind: ledger.KindProvider, ID: "x", Source: "s1:AAAA"}) != "" {
		t.Fatal("a provider identity with a source is not representable")
	}
	for _, bad := range []string{"", "hmac", "hmac::x", "hmac:k-1:", "provider:k-1:x", "other:a:b", "v42"} {
		if p := parseJournalSecretVersion(bad); p != nil {
			t.Errorf("%q parsed as %+v", bad, p)
		}
	}
	// Conflicting journal and hint versions are refused, never resolved.
	rec := &RotationState{RotationID: "rot-c", Strategy: "restart", Transition: rotation.TrCompleted, Status: "completed", Seq: 9, SecretVersion: s}
	other := ledger.Version{Kind: ledger.KindHMAC, KeyID: h.KeyID, ID: strings.Repeat("A", 43)}
	if _, err := EvidenceForRotation(rec, &other, false, nil); !errors.Is(err, ledger.ErrNoEvidence) {
		t.Fatalf("conflict: %v", err)
	}
	rec.SecretVersion = ""
	if _, err := EvidenceForRotation(rec, nil, false, nil); !errors.Is(err, ErrBridgeNoVersion) {
		t.Fatalf("no version anywhere: %v", err)
	}
}

// ============================================================ windows

// T-W1: crash between Observe and the journal start.
func TestLedgerBridge_TW1_CrashBeforeJournalStart(t *testing.T) {
	r := newLBRig(t, nil)
	d1 := r.baseline(true)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	r.crash()
	r.recover()
	if rep := r.reconcileTwice(); len(rep.Changes) != 0 {
		t.Fatalf("nothing to reconcile: %+v", rep.Changes)
	}
	e := r.entry()
	if e.Desired == nil || ledger.Compare(*e.Desired, d2) != ledger.Same || !appliedIs(e, d1) || e.State != ledger.EntryPending {
		t.Fatalf("desired D2 must be durable, applied D1: %+v", e)
	}
	if !r.wouldDispatch() {
		t.Fatal("exactly one rotation is wanted")
	}
	r.rotate("rot-r1", d2, lbFlow{strategy: "restart", orig: "o1", cand: "c1", o2: true})
	if r.wouldDispatch() || !appliedIs(r.entry(), d2) {
		t.Fatalf("after the one rotation nothing more is wanted: %+v", r.entry())
	}
}

// T-W2: crash between the journal start and Correlate. M0 recovery resolves
// the record (recovered: originals healthy) or escalates it (critical_error).
func TestLedgerBridge_TW2_CrashAfterJournalStartBeforeCorrelate(t *testing.T) {
	for _, outcome := range []string{"recovered", "critical_error"} {
		t.Run(outcome, func(t *testing.T) {
			r := newLBRig(t, nil)
			d1 := r.baseline(true)
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			if err := r.journalStart("rot-r1", lbFlow{orig: "o1"}); err != nil {
				t.Fatal(err)
			}
			r.crash()
			rec := r.record("rot-r1")
			if outcome == "recovered" {
				_ = r.st.MarkRecoveredRecord(rec)
			} else {
				_ = r.st.MarkCriticalRecord(rec, "test: escalated")
			}
			g := r.generation()
			if rep := r.reconcileTwice(); len(rep.Changes) != 0 || r.generation() != g {
				t.Fatalf("no evidence and no citation: ledger unchanged, got %+v", rep.Changes)
			}
			if e := r.entry(); !appliedIs(e, d1) || e.State != ledger.EntryPending {
				t.Fatalf("entry %+v", e)
			}
			if !r.v.NeedsRotation(lbKey).Rotate() {
				t.Fatal("the ledger still wants the rotation")
			}
			// The guard decides: an unresolved record blocks, a recovered one does not.
			if got := r.wouldDispatch(); got != (outcome == "recovered") {
				t.Fatalf("dispatch=%v with record %s", got, outcome)
			}
		})
	}
}

// F-B2 (fixed by F-S6-1): this test used to pin the DEFECT. StartRotationWithID
// upserted by provider:secret:originalIDs, so a dispatch whose original container
// ids equal the previous successful dispatch's (signal keeps its containers)
// REPLACED the previous completed record; a crash before Correlate then left the
// ledger citing a rotation the journal no longer held, and rule R-B downgraded
// the verified D1 claim to unknown. Records are now keyed by rotation identity, so
// the completed predecessor stays in the journal (bounded by the 7-day retention),
// the D1 claim stays supported (no R-B), and the engine still dispatches exactly the
// one wanted rotation.
func TestLedgerBridge_TW2_SameContainerIDsPredecessorRetained_FB2(t *testing.T) {
	r := newLBRig(t, nil)
	d1 := r.ver("one")
	r.observe(d1, lbLive("o1"))
	sig := lbFlow{strategy: "signal", orig: "o1", cand: "o1", o2: true}
	r.rotate("rot-s0", d1, sig)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	if err := r.journalStart("rot-s1", sig); err != nil {
		t.Fatal(err)
	}
	if rec := r.record("rot-s0"); rec == nil || rec.Status != "completed" || rec.Transition != rotation.TrSignalDone {
		t.Fatalf("the completed predecessor must be retained with its evidence: %+v", rec)
	}
	r.crash()
	_ = r.st.MarkRecoveredRecord(r.record("rot-s1"))
	rep := r.reconcileTwice()
	if hasRule(rep, "R-B") {
		t.Fatalf("the D1 claim is still supported by the retained predecessor, rules %v", rules(rep))
	}
	if e := r.entry(); !appliedIs(e, d1) {
		t.Fatalf("the verified D1 claim must survive: %+v", e)
	}
	if !r.wouldDispatch() {
		t.Fatal("the wanted rotation is still dispatched exactly once")
	}
	r.rotate("rot-s2", d2, sig)
	if r.wouldDispatch() || !appliedIs(r.entry(), d2) {
		t.Fatalf("converged: %+v", r.entry())
	}
}

// T-W3: crash after Correlate, before any Docker action.
func TestLedgerBridge_TW3_CrashAfterCorrelate(t *testing.T) {
	r := newLBRig(t, nil)
	d1 := r.baseline(true)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", lbFlow{orig: "o1"})); err != nil {
		t.Fatal(err)
	}
	r.crash()
	if !r.blocked() {
		t.Fatal("an inherited in_progress record blocks until recovery settles it")
	}
	_ = r.st.MarkRecoveredRecord(r.record("rot-r1")) // M0: originals healthy, nothing mutated
	rep := r.reconcileTwice()
	if got := rules(rep); len(got) != 1 || got[0] != "W3" {
		t.Fatalf("rules %v", got)
	}
	e := r.entry()
	for _, rp := range e.Replicas {
		if rp.Delivery != ledger.DeliveryNone || rp.Outcome != ledger.ReplicaPending {
			t.Fatalf("requested must be cleared: %+v", rp)
		}
	}
	if !appliedIs(e, d1) || !r.wouldDispatch() {
		t.Fatalf("D1 stays, one re-dispatch: %+v", e)
	}
}

// T-W4 (N21): crash before candidate_ready. The REAL AutomaticRecovery
// preserves everything and escalates to critical_error; the ledger never
// advances, never trusts D2, and no dispatch happens while the record is
// unresolved. After the operator resolves it, exactly one rotation.
func TestLedgerBridge_TW4_PreReadyCrash_RealRecovery(t *testing.T) {
	for _, step := range []string{rotation.TrIntentStartReplacement, rotation.TrReplacementStarted} {
		t.Run(step, func(t *testing.T) {
			d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"), healthy("b1", "unrelated"))
			r := newLBRig(t, d)
			d1 := r.baseline(true)
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			fl := lbFlow{strategy: "restart", orig: "o1", cand: "c1", o2: true}
			if err := r.correlate(n21RID, d2, r.journalStart(n21RID, fl)); err != nil {
				t.Fatal(err)
			}
			r.journalTo(n21RID, fl, d2, step)
			r.crash()
			r.recover()
			if s := r.record(n21RID).Status; s != "critical_error" {
				t.Fatalf("M0 recovery must escalate a pre-ready crash, got %s", s)
			}
			if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
				t.Fatalf("recovery mutated: %+v", d)
			}
			rep := r.reconcileTwice()
			if got := rules(rep); len(got) != 1 || got[0] != "W4" {
				t.Fatalf("rules %v", got)
			}
			e := r.entry()
			if !appliedIs(e, d1) || e.State != ledger.EntryFailed {
				t.Fatalf("applied unchanged (never D2), state failed: %+v", e)
			}
			for _, rp := range e.Replicas {
				if rp.Adoption != ledger.AdoptionUnknown || rp.Delivery != ledger.DeliveryNone || rp.Reason != string(ledger.WhyInterrupted) {
					t.Fatalf("replica must be unknown/interrupted: %+v", rp)
				}
				if rp.Applied != nil && ledger.Compare(*rp.Applied, d2) == ledger.Same {
					t.Fatal("D2 must never be recorded for an unverified candidate")
				}
			}
			if !r.v.NeedsRotation(lbKey).Rotate() || !r.blocked() || r.wouldDispatch() {
				t.Fatal("the ledger may want the rotation but the guard blocks it while critical_error")
			}
			_ = r.st.MarkRecoveredRecord(r.record(n21RID)) // the operator resolves the record
			if !r.wouldDispatch() {
				t.Fatal("after resolution exactly one rotation")
			}
			r.rotate("rot-r2", d2, lbFlow{strategy: "restart", orig: "c1", cand: "c2", o2: true})
			if r.wouldDispatch() || !appliedIs(r.entry(), d2) {
				t.Fatalf("converged: %+v", r.entry())
			}
		})
	}
}

// T-W5: crash after candidate_ready, before the commit. The REAL recovery
// marks it recovered (the verified candidate serves) without a committed swap:
// no advance, one extra rotation (D-2), none skipped.
//
// FINDING (M0, recovery.go, not changed here): journalRecordFor returns the
// FIRST record (map order) naming the artifact id as original or candidate,
// terminal records included. After a previous restart rotation R0 whose
// candidate o1 is this rotation's original, recovery may judge o1 against R0
// ("holder is not the journalled original") and escalate to critical_error
// instead of recovered: nondeterministic, but always in the safe direction.
// The "predecessor in journal" variant asserts the ledger is safe under either
// M0 outcome; the deterministic variant drops R0 from the journal first.
func TestLedgerBridge_TW5_AfterReadyBeforeCommit_RealRecovery(t *testing.T) {
	for _, predecessor := range []bool{false, true} {
		t.Run(fmt.Sprintf("predecessor_in_journal=%v", predecessor), func(t *testing.T) {
			d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
			r := newLBRig(t, d)
			d1 := r.baseline(true)
			if !predecessor {
				_ = r.st.DeleteState(lbProvider, lbSecret, "o0")
			}
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			fl := lbFlow{strategy: "restart", orig: "o1", cand: "c1", o2: true}
			if err := r.correlate(n21RID, d2, r.journalStart(n21RID, fl)); err != nil {
				t.Fatal(err)
			}
			r.journalTo(n21RID, fl, d2, rotation.TrCandidateReady)
			r.crash()
			r.recover()
			status := r.record(n21RID).Status
			switch {
			case !predecessor && status != "recovered":
				t.Fatalf("status %s", status)
			case status != "recovered" && status != "critical_error":
				t.Fatalf("status %s", status)
			}
			t.Logf("M0 recovery outcome: %s", status)
			rep := r.reconcileTwice()
			if !hasRule(rep, "W4") {
				t.Fatalf("rules %v", rules(rep))
			}
			e := r.entry()
			if !appliedIs(e, d1) {
				t.Fatalf("no advance without a commit: %+v", e)
			}
			if status == "critical_error" {
				if r.wouldDispatch() {
					t.Fatal("blocked while unresolved")
				}
				_ = r.st.MarkRecoveredRecord(r.record(n21RID)) // operator
			}
			if !r.wouldDispatch() {
				t.Fatal("one extra rotation is allowed and none is skipped")
			}
			r.rotate("rot-r2", d2, lbFlow{strategy: "restart", orig: "c1", cand: "c2", o2: true})
			if r.wouldDispatch() {
				t.Fatal("exactly one extra rotation")
			}
		})
	}
}

// T-W5 committed variant: a rolling swap_committed record that M0 recovery
// marked recovered IS evidence (Committed): advance, zero dispatches.
func TestLedgerBridge_TW5_CommittedRecovered_Advances(t *testing.T) {
	r := newLBRig(t, nil)
	r.baseline(true)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	fl := lbFlow{strategy: "rolling", orig: "o1", cand: "c1", o2: true}
	if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", fl)); err != nil {
		t.Fatal(err)
	}
	r.journalTo("rot-r1", fl, d2, rotation.TrSwapCommitted)
	r.crash()
	_ = r.st.MarkRecoveredRecord(r.record("rot-r1"))
	if rep := r.reconcileTwice(); !hasRule(rep, "W6") {
		t.Fatalf("rules %v", rules(rep))
	}
	if e := r.entry(); !appliedIs(e, d2) || e.State != ledger.EntryIdle || r.wouldDispatch() {
		t.Fatalf("entry %+v", e)
	}
}

// T-W6 = N21 case 8 (m0-04-recovery-design.md section 5 row 8): the journal
// commit is recorded, the ledger update is interrupted. With the version in
// the journal (O-2 emulated) reconcile applies D2 from the journal outcome
// without rotating again; the real M0 recovery does not reopen the completed
// rotation; a second reconcile writes nothing.
func TestLedgerBridge_TW6_N21Case8_JournalCommittedLedgerInterrupted(t *testing.T) {
	for _, strategy := range []string{"restart", "rolling"} {
		t.Run(strategy, func(t *testing.T) {
			d := newFakeDaemon(n21Candidate(true, "healthy"))
			r := newLBRig(t, d)
			r.baseline(true)
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			fl := lbFlow{strategy: strategy, orig: "o1", cand: "c1", o2: true}
			if err := r.correlate(n21RID, d2, r.journalStart(n21RID, fl)); err != nil {
				t.Fatal(err)
			}
			r.journalTo(n21RID, fl, d2, lastStep(strategy))
			r.finishOK(fl)
			r.crash() // step 7 never ran
			r.recover()
			if len(d.removed)+len(d.started)+len(d.stopped) != 0 {
				t.Fatalf("a completed rotation must not be reopened: %+v", d)
			}
			rep := r.reconcileTwice()
			if got := rules(rep); len(got) != 1 || got[0] != "W6" {
				t.Fatalf("rules %v", got)
			}
			e := r.entry()
			if !appliedIs(e, d2) || e.State != ledger.EntryIdle {
				t.Fatalf("applied must be D2 from the journal, idle: %+v", e)
			}
			if r.wouldDispatch() {
				t.Fatal("zero dispatches: reconciled without re-rotation")
			}
		})
	}
}

// T-W6, M0 as-is (no O-2): the journal proves the delivery but names no
// version, so reconcile cannot apply it (S4a decision): replicas unknown, no
// version advance. DOCUMENTED COST: one extra rotation of a secret that was in
// fact delivered.
func TestLedgerBridge_TW6_WithoutO2_CostsOneExtraRotation(t *testing.T) {
	r := newLBRig(t, nil)
	d1 := r.baseline(false)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	fl := lbFlow{strategy: "restart", orig: "o1", cand: "c1"}
	if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", fl)); err != nil {
		t.Fatal(err)
	}
	r.journalTo("rot-r1", fl, d2, lastStep("restart"))
	r.finishOK(fl)
	if r.record("rot-r1").SecretVersion != "" {
		t.Fatal("M0 must not carry a version here")
	}
	r.crash()
	rep := r.reconcileTwice()
	if got := rules(rep); len(got) != 1 || got[0] != "no_version" {
		t.Fatalf("rules %v", got)
	}
	e := r.entry()
	if !appliedIs(e, d1) || e.State != ledger.EntryFailed {
		t.Fatalf("no advance without a version: %+v", e)
	}
	for _, rp := range e.Replicas {
		if rp.Adoption != ledger.AdoptionUnknown || rp.Delivery != ledger.DeliveryNone {
			t.Fatalf("replica unknown: %+v", rp)
		}
	}
	if !r.wouldDispatch() {
		t.Fatal("the cost: one extra rotation is wanted")
	}
	r.rotate("rot-r2", d2, lbFlow{strategy: "restart", orig: "c1", cand: "c2"})
	if r.wouldDispatch() {
		t.Fatal("and only one: in process the correlated version stands in for O-2")
	}
}

// T-W7: verified and applied are one single update: one generation step, and a
// reopened ledger shows both together.
func TestLedgerBridge_TW7_VerifiedAndAppliedAreOneUpdate(t *testing.T) {
	r := newLBRig(t, nil)
	r.baseline(true)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	fl := lbFlow{strategy: "rolling", orig: "o1", cand: "c1", o2: true}
	if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", fl)); err != nil {
		t.Fatal(err)
	}
	r.journalTo("rot-r1", fl, d2, lastStep("rolling"))
	r.finishOK(fl)
	g := r.generation()
	if _, err := r.terminal("rot-r1", &d2, fl); err != nil {
		t.Fatal(err)
	}
	if r.generation() != g+1 {
		t.Fatalf("one write expected, generation %d -> %d", g, r.generation())
	}
	r.crash()
	e := r.entry()
	if !appliedIs(e, d2) || e.Replicas[0].Delivery != ledger.DeliveryVerified || e.Replicas[0].ContainerID != "c1" {
		t.Fatalf("durable document: %+v", e)
	}
	if res, err := r.terminal("rot-r1", &d2, fl); err != nil || !res.Unchanged {
		t.Fatalf("idempotent: %+v %v", res, err)
	}
}

// T-W8: ledger persistence fails at step 7 (real EACCES on the ledger
// directory). The ledger is unchanged and degraded; the restart converges from
// the journal (O-2 emulated), or pays the documented cost without O-2. "No
// re-dispatch in process" is the engine's secretHashes: S6.
func TestLedgerBridge_TW8_PersistenceFault(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	for _, o2 := range []bool{true, false} {
		t.Run(fmt.Sprintf("o2=%v", o2), func(t *testing.T) {
			r := newLBRig(t, nil)
			d1 := r.baseline(o2)
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			fl := lbFlow{strategy: "restart", orig: "o1", cand: "c1", o2: o2}
			if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", fl)); err != nil {
				t.Fatal(err)
			}
			r.journalTo("rot-r1", fl, d2, lastStep("restart"))
			r.finishOK(fl)
			if err := os.Chmod(r.ldir, 0o500); err != nil {
				t.Fatal(err)
			}
			_, err := r.terminal("rot-r1", &d2, fl)
			if cerr := os.Chmod(r.ldir, 0o700); cerr != nil {
				t.Fatal(cerr)
			}
			if !errors.Is(err, ledger.ErrPersistenceFailed) {
				t.Fatalf("want a persistence failure, got %v", err)
			}
			if e := r.entry(); !appliedIs(e, d1) || e.Replicas[0].Delivery != ledger.DeliveryRequested || r.s.PersistenceHealthy() {
				t.Fatalf("published ledger unchanged and degraded: %+v", e)
			}
			r.crash()
			rep := r.reconcileTwice()
			if o2 {
				if !hasRule(rep, "W6") || !appliedIs(r.entry(), d2) || r.wouldDispatch() {
					t.Fatalf("converged from the journal: %v %+v", rules(rep), r.entry())
				}
				return
			}
			if !hasRule(rep, "no_version") || !appliedIs(r.entry(), d1) || !r.wouldDispatch() {
				t.Fatalf("without O-2: unknown, one extra rotation: %v %+v", rules(rep), r.entry())
			}
		})
	}
}

// T-W9: signal_done is delivery verified, adoption UNVERIFIED, applied
// advances, and no signal loop follows; in process and after a crash.
func TestLedgerBridge_TW9_Signal(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash=%v", crash), func(t *testing.T) {
			r := newLBRig(t, nil)
			d1 := r.ver("one")
			r.observe(d1, lbLive("o1"))
			sig := lbFlow{strategy: "signal", orig: "o1", cand: "o1", o2: true}
			r.rotate("rot-s0", d1, sig)
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			if err := r.correlate("rot-s1", d2, r.journalStart("rot-s1", sig)); err != nil {
				t.Fatal(err)
			}
			r.journalTo("rot-s1", sig, d2, rotation.TrSignalDone)
			r.finishOK(sig)
			if crash {
				r.crash()
				if rep := r.reconcileTwice(); !hasRule(rep, "W9") {
					t.Fatalf("rules %v", rules(rep))
				}
			} else if res, err := r.terminal("rot-s1", &d2, sig); err != nil || res.Adoption != ledger.AdoptionUnverified {
				t.Fatalf("%+v %v", res, err)
			}
			e := r.entry()
			if !appliedIs(e, d2) || e.State != ledger.EntryIdle {
				t.Fatalf("entry %+v", e)
			}
			for _, rp := range e.Replicas {
				if rp.Delivery != ledger.DeliveryVerified || rp.Adoption != ledger.AdoptionUnverified || rp.Reason != string(ledger.WhyAdoptionUnverifiedSignal) {
					t.Fatalf("replica %+v", rp)
				}
			}
			if r.wouldDispatch() {
				t.Fatal("no loop of signal rotations")
			}
			if err := r.v.RecordAdoptionVerified(lbKey, ledger.ReplicaRef{Number: "1", ContainerName: lbService}, ledger.AdoptionEvidence{Kind: ledger.AdoptionEvidenceRestart, RotationID: "rot-s1", Version: d2}); !errors.Is(err, ledger.ErrNoEvidence) {
				t.Fatalf("a signal delivery is never adoption verified: %v", err)
			}
		})
	}
}

// T-W10: the dispatch never ran (finishRotation deleted the journal record:
// deferred / blocked / no targets): the requested hint is cleared, in process
// and by the startup reconcile.
func TestLedgerBridge_TW10_JournalRecordDeleted(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash=%v", crash), func(t *testing.T) {
			r := newLBRig(t, nil)
			d1 := r.baseline(true)
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			fl := lbFlow{strategy: "restart", orig: "o1", cand: "c1"}
			if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", fl)); err != nil {
				t.Fatal(err)
			}
			if err := r.st.DeleteState(lbProvider, lbSecret, "o1"); err != nil {
				t.Fatal(err)
			}
			if crash {
				r.crash()
				if got := rules(r.reconcileTwice()); len(got) != 1 || got[0] != "W10" {
					t.Fatalf("rules %v", got)
				}
			} else if res, err := r.terminal("rot-r1", &d2, fl); err != nil || res.Cause != ledger.CauseNeverRan {
				t.Fatalf("%+v %v", res, err)
			}
			e := r.entry()
			if !appliedIs(e, d1) || e.Replicas[0].Delivery != ledger.DeliveryNone || e.Replicas[0].Reason != string(ledger.WhyDeferred) {
				t.Fatalf("entry %+v", e)
			}
			if !r.wouldDispatch() {
				t.Fatal("retried on the next poll")
			}
		})
	}
}

// T-W11: a fresh (lost) ledger never replays journal history.
func TestLedgerBridge_TW11_FreshLedgerIgnoresHistory(t *testing.T) {
	r := newLBRig(t, nil)
	r.baseline(true)
	// The ledger is lost: a new ledger directory, the journal (with the
	// completed, versioned R0) survives.
	_ = r.s.Close()
	r.ldir = lbLedgerDir(t)
	kp, err := ledger.NewFileKeyProvider(r.ldir)
	if err != nil {
		t.Fatal(err)
	}
	r.keys = kp
	r.clock.off.Store(int64(time.Second)) // the new ledger is created after the journal history
	r.openLedger()
	r.openJournal()
	if rec := r.record("rot-r0"); rec == nil || rec.Status != "completed" || rec.SecretVersion == "" {
		t.Fatalf("journal history %+v", rec)
	}
	if rep := r.reconcileTwice(); len(rep.Changes) != 0 {
		t.Fatalf("history replayed: %+v", rep.Changes)
	}
	res := r.observe(r.ver("one"), lbLive("o1"))
	if res.Kind != ledger.ObserveAdopted || res.Entry.Applied != nil || r.wouldDispatch() {
		t.Fatalf("UNKNOWN, no rotation: %+v", res)
	}
	if rep := r.reconcileTwice(); len(rep.Changes) != 0 {
		t.Fatalf("history replayed after adoption: %+v", rep.Changes)
	}
}

// T-W12: the ledger claims more than the journal supports.
func TestLedgerBridge_TW12_LedgerClaimNotSupportedByJournal(t *testing.T) {
	cases := []struct {
		name     string
		old      bool // the claim is older than the retention horizon
		mutate   func(r *lbRig)
		rule     string
		accepted bool
	}{
		{"R absent and recent", false, func(r *lbRig) { _ = r.st.DeleteState(lbProvider, lbSecret, "o0") }, "R-B", false},
		{"R absent and older than 7 days", true, func(r *lbRig) { _ = r.st.DeleteState(lbProvider, lbSecret, "o0") }, "", true},
		{"R present without evidence (critical_error)", false, func(r *lbRig) { _ = r.st.MarkCriticalRecord(r.record("rot-r0"), "test") }, "R-A", false},
		{"R recovered only", false, func(r *lbRig) { _ = r.st.MarkRecoveredRecord(r.record("rot-r0")) }, "R-A", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newLBRig(t, nil)
			if c.old { // a ledger created and written ten days ago
				_ = r.s.Close()
				if err := os.Remove(filepath.Join(r.ldir, "ledger.json")); err != nil {
					t.Fatal(err)
				}
				r.clock.off.Store(int64(-10 * 24 * time.Hour))
				r.openLedger()
			}
			d1 := r.baseline(true)
			r.clock.off.Store(0)
			c.mutate(r)
			r.crash()
			rep := r.reconcileTwice()
			e := r.entry()
			if c.accepted {
				if len(rep.Changes) != 0 || !appliedIs(e, d1) {
					t.Fatalf("an old claim is accepted (R-C): %v %+v", rules(rep), e)
				}
				return
			}
			if !hasRule(rep, c.rule) {
				t.Fatalf("rules %v, want %s", rules(rep), c.rule)
			}
			if e.Applied != nil || e.State != ledger.EntryFailed {
				t.Fatalf("not accepted, corrected to unknown: %+v", e)
			}
			for _, rp := range e.Replicas {
				if rp.Applied != nil || rp.Delivery != ledger.DeliveryNone || rp.Adoption != ledger.AdoptionUnknown {
					t.Fatalf("replica %+v", rp)
				}
			}
			if r.wouldDispatch() {
				t.Fatal("a downgrade never causes a rotation by itself")
			}
		})
	}
}

// T-W13: a torn ledger write leaves either the old or the new complete
// document; both converge to the same final state after reconcile.
func TestLedgerBridge_TW13_TornWriteConverges(t *testing.T) {
	var final [2]ledger.Entry
	for i, torn := range []bool{false, true} {
		r := newLBRig(t, nil)
		r.baseline(true)
		d2 := r.ver("two")
		r.observe(d2, lbLive("o1"))
		fl := lbFlow{strategy: "restart", orig: "o1", cand: "c1", o2: true}
		if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", fl)); err != nil {
			t.Fatal(err)
		}
		r.journalTo("rot-r1", fl, d2, lastStep("restart"))
		r.finishOK(fl)
		path := filepath.Join(r.ldir, "ledger.json")
		old, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.terminal("rot-r1", &d2, fl); err != nil {
			t.Fatal(err)
		}
		_ = r.s.Close()
		if torn { // the crash hit before the rename: the old document survives
			if err := os.WriteFile(path, old, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		r.openLedger()
		r.openJournal()
		r.reconcileTwice()
		final[i] = r.entry()
		if !appliedIs(final[i], d2) || final[i].State != ledger.EntryIdle || r.wouldDispatch() {
			t.Fatalf("torn=%v: %+v", torn, final[i])
		}
	}
	for i := range final {
		final[i].LastOutcome, final[i].DesiredObservedAt = nil, time.Time{}
		for j := range final[i].Replicas {
			final[i].Replicas[j].At = time.Time{}
			final[i].Replicas[j].Reason = ""
		}
	}
	if fmt.Sprint(final[0].State, final[0].Replicas[0].Delivery, final[0].Replicas[0].Adoption) !=
		fmt.Sprint(final[1].State, final[1].Replicas[0].Delivery, final[1].Replicas[0].Adoption) {
		t.Fatalf("different final states: %+v vs %+v", final[0], final[1])
	}
}

// T-W14: the journal start fails: the ledger may not cite the operation. The
// tracker keeps the record in memory even though it is not durable, which is
// why the bridge takes StartRotationWithID's error rather than looking.
// Stopping the dispatch itself is O-7 (S6).
func TestLedgerBridge_TW14_JournalStartFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	r := newLBRig(t, nil)
	r.baseline(true)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	g := r.generation()
	if err := os.Chmod(r.jdir, 0o500); err != nil {
		t.Fatal(err)
	}
	startErr := r.journalStart("rot-r1", lbFlow{orig: "o1"})
	_ = os.Chmod(r.jdir, 0o700)
	if startErr == nil {
		t.Fatal("the journal write should have failed")
	}
	if r.record("rot-r1") == nil {
		t.Log("note: the tracker no longer keeps an undurable record in memory")
	}
	if err := r.correlate("rot-r1", d2, startErr); !errors.Is(err, ErrBridgeJournalStartFailed) {
		t.Fatalf("correlate must be refused: %v", err)
	}
	if err := CorrelateAfterJournalStart(r.v, r.st, nil, lbKey, "rot-unknown", r.entry().DesiredSeq, d2); !errors.Is(err, ErrBridgeJournalStartFailed) {
		t.Fatalf("an id unknown to the journal must be refused: %v", err)
	}
	if r.generation() != g {
		t.Fatal("no ledger citation may be written")
	}
	for _, rp := range r.entry().Replicas {
		if rp.RotationID == "rot-r1" || rp.Delivery == ledger.DeliveryRequested {
			t.Fatalf("replica cites the unjournalled operation: %+v", rp)
		}
	}
}

// T-W15: a failed rotation (rollback_required) and a rolled-back one leave
// applied unchanged and are retried; in process and after a crash.
func TestLedgerBridge_TW15_FailedRotation(t *testing.T) {
	cases := []struct {
		name  string
		steps func(r *lbRig, rid string, fl lbFlow, v ledger.Version)
		cause ledger.InterruptCause
		state ledger.EntryState
		rule  string
	}{
		{"rollback_required", func(r *lbRig, rid string, fl lbFlow, v ledger.Version) {
			r.journalTo(rid, fl, v, rotation.TrIntentCreateCandidate)
			_ = r.st.Record(context.Background(), rotation.JournalEvent{RotationID: rid, Transition: rotation.TrFailed})
			_ = r.st.MarkRollback(lbProvider, lbSecret, fl.orig)
		}, ledger.CauseFailed, ledger.EntryFailed, "W15"},
		{"rolled_back", func(r *lbRig, rid string, fl lbFlow, v ledger.Version) {
			r.journalTo(rid, fl, v, rotation.TrCandidateReady)
			_ = r.st.Record(context.Background(), rotation.JournalEvent{RotationID: rid, Transition: rotation.TrIntentRollback})
			_ = r.st.Record(context.Background(), rotation.JournalEvent{RotationID: rid, Transition: rotation.TrRolledBack})
			_ = r.st.MarkRollback(lbProvider, lbSecret, fl.orig)
		}, ledger.CauseFailed, ledger.EntryFailed, "W15"},
	}
	for _, c := range cases {
		for _, crash := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/crash=%v", c.name, crash), func(t *testing.T) {
				r := newLBRig(t, nil)
				d1 := r.baseline(true)
				d2 := r.ver("two")
				r.observe(d2, lbLive("o1"))
				fl := lbFlow{strategy: "rolling", orig: "o1", cand: "c1", o2: true}
				if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", fl)); err != nil {
					t.Fatal(err)
				}
				c.steps(r, "rot-r1", fl, d2)
				if crash {
					r.crash()
					if got := rules(r.reconcileTwice()); len(got) != 1 || got[0] != c.rule {
						t.Fatalf("rules %v", got)
					}
				} else if res, err := r.terminal("rot-r1", &d2, fl); err != nil || res.Cause != c.cause {
					t.Fatalf("%+v %v", res, err)
				}
				e := r.entry()
				if !appliedIs(e, d1) || e.State != c.state {
					t.Fatalf("entry %+v", e)
				}
				if !r.wouldDispatch() {
					t.Fatal("retried on the next poll")
				}
			})
		}
	}
	// A journal-recorded rolled_back that M0 left "recovered" (no engine
	// MarkRollback) maps to CauseRolledBack in process.
	if InterruptCauseFor(&RotationState{Status: "recovered", Transition: rotation.TrRolledBack, Seq: 3}) != ledger.CauseRolledBack {
		t.Fatal("rolled_back cause")
	}
	if InterruptCauseFor(&RotationState{Status: "recovered", Transition: rotation.TrSignalFailed, Seq: 3}) != ledger.CauseFailed {
		t.Fatal("signal_failed (nothing published) cause")
	}
	if InterruptCauseFor(&RotationState{Status: "critical_error", Transition: rotation.TrReplacementStarted, Seq: 3}) != ledger.CauseUnresolved {
		t.Fatal("critical_error cause")
	}
}

// T-GUARD (X1): no bridge call changes the guard's verdict or the journal; a
// service with an unresolved record is not dispatched however the ledger reads.
func TestLedgerBridge_TGUARD_BridgeNeverChangesTheGuard(t *testing.T) {
	r := newLBRig(t, nil)
	d1 := r.baseline(true)
	d2 := r.ver("two")
	ctx := context.Background()
	_ = r.st.Record(ctx, rotation.JournalEvent{RotationID: "rot-bad", Strategy: "restart", ServiceID: lbService, Transition: rotation.TrReplacementStarted, OriginalIDs: []string{"o1"}})
	_ = r.st.MarkCriticalRecord(r.record("rot-bad"), "test: operator required")
	guard := newJournalGuard(r.st)
	reason, blocked := guard.RotationBlocked(lbService)
	if !blocked {
		t.Fatal("setup: the service must be blocked")
	}
	journal := r.journalBytes()
	check := func(what string) {
		t.Helper()
		if gr, gb := guard.RotationBlocked(lbService); gb != blocked || gr != reason {
			t.Fatalf("%s changed the guard: %q %v", what, gr, gb)
		}
		if !bytes.Equal(journal, r.journalBytes()) {
			t.Fatalf("%s wrote the journal", what)
		}
	}
	r.observe(d2, lbLive("o1"))
	check("Observe")
	if !r.v.NeedsRotation(lbKey).Rotate() || r.wouldDispatch() {
		t.Fatal("the ledger wants the rotation; the guard still blocks it")
	}
	_, _ = RecordRotationOutcome(r.v, r.st, lbKey, "rot-bad", &d2, false, nil)
	check("RecordRotationOutcome")
	_, _ = ReconcileLedgerFromTracker(r.v, r.st, LedgerReconcileInput{})
	check("ReconcileLedgerFromTracker")
	_ = CorrelateAfterJournalStart(r.v, r.st, nil, lbKey, "rot-bad", r.entry().DesiredSeq, d2)
	check("CorrelateAfterJournalStart")
	_, _ = EvidenceForRotation(r.record("rot-bad"), &d1, false, nil)
	_ = JournalSnapshotFromTracker(r.st, LedgerReconcileInput{})
	check("EvidenceForRotation/JournalSnapshotFromTracker")
	if r.wouldDispatch() {
		t.Fatal("still blocked")
	}
}

// A corrupt journal: reconcile is skipped, nothing changes (design 4.3).
func TestLedgerBridge_CorruptJournal_ReconcileSkipped(t *testing.T) {
	r := newLBRig(t, nil)
	r.baseline(true)
	d2 := r.ver("two")
	r.observe(d2, lbLive("o1"))
	if err := r.correlate("rot-r1", d2, r.journalStart("rot-r1", lbFlow{orig: "o1"})); err != nil {
		t.Fatal(err)
	}
	_ = r.s.Close()
	if err := os.WriteFile(filepath.Join(r.jdir, "rotations.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.openLedger()
	r.openJournal()
	if c, _ := r.st.Corrupt(); !c {
		t.Fatal("setup: corrupt journal")
	}
	g := r.generation()
	rep, err := ReconcileLedgerFromTracker(r.v, r.st, LedgerReconcileInput{})
	if err != nil || !rep.Skipped || rep.SkipReason != ledger.WhyJournalUnreadable || r.generation() != g {
		t.Fatalf("%+v %v", rep, err)
	}
	if r.entry().Replicas[0].Delivery != ledger.DeliveryRequested || r.wouldDispatch() {
		t.Fatal("ledger left as is; the guard blocks everything")
	}
}

// ============================================================ owner's scenarios

// Scenarios 1 and 2: initial start, no ledger, one or three existing replicas:
// adopted, applied UNKNOWN, zero dispatches. (Hash/cache storage: S6.)
func TestLedgerBridge_Scenario01_02_InitialAdoption(t *testing.T) {
	for _, live := range [][]ledger.LiveReplica{lbLive("o1"), lbLive("o1", "o2", "o3")} {
		r := newLBRig(t, nil)
		res := r.observe(r.ver("one"), live)
		if res.Kind != ledger.ObserveAdopted || res.Entry.Applied != nil || res.Entry.State != ledger.EntryAdopted || len(res.Entry.Replicas) != len(live) {
			t.Fatalf("%+v", res)
		}
		for _, rp := range res.Entry.Replicas {
			if rp.Adoption != ledger.AdoptionUnknown || rp.Delivery != ledger.DeliveryNone {
				t.Fatalf("replica %+v", rp)
			}
		}
		if r.wouldDispatch() {
			t.Fatal("0 dispatches")
		}
		r.crash()
		if rep := r.reconcileTwice(); len(rep.Changes) != 0 || r.wouldDispatch() {
			t.Fatal("restart: still nothing")
		}
	}
}

// Scenario 3: restart with desired == applied verified: no dispatch, no write.
func TestLedgerBridge_Scenario03_RestartUnchanged(t *testing.T) {
	r := newLBRig(t, nil)
	d1 := r.baseline(true)
	r.crash()
	r.reconcileTwice()
	g := r.generation()
	if res := r.observe(d1, lbLive("o1")); res.Kind != ledger.ObserveUnchanged {
		t.Fatalf("%+v", res)
	}
	if r.generation() != g || r.wouldDispatch() {
		t.Fatal("no write, no dispatch")
	}
}

// Scenario 4: restart, provider changed while down: one dispatch, then D2.
func TestLedgerBridge_Scenario04_RestartDesiredChanged(t *testing.T) {
	r := newLBRig(t, nil)
	r.baseline(true)
	r.crash()
	r.reconcileTwice()
	d2 := r.ver("two")
	if res := r.observe(d2, lbLive("o1")); res.Kind != ledger.ObserveChanged || res.Entry.State != ledger.EntryPending {
		t.Fatalf("%+v", res)
	}
	if !r.wouldDispatch() {
		t.Fatal("one dispatch")
	}
	r.rotate("rot-r1", d2, lbFlow{strategy: "rolling", orig: "o1", cand: "c1", o2: true})
	if !appliedIs(r.entry(), d2) || r.wouldDispatch() {
		t.Fatalf("%+v", r.entry())
	}
}

// Scenario 6: a corrupt ledger is preserved aside; fresh ledger, everything
// UNKNOWN, zero dispatches, journal history not replayed.
func TestLedgerBridge_Scenario06_LedgerCorruption(t *testing.T) {
	r := newLBRig(t, nil)
	d1 := r.baseline(true)
	_ = r.s.Close()
	if err := os.WriteFile(filepath.Join(r.ldir, "ledger.json"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.openLedger()
	r.openJournal()
	if r.res.Reason != ledger.ReasonCorrupt || r.res.Preserved == "" {
		t.Fatalf("open result %+v", r.res)
	}
	if _, err := os.Stat(r.res.Preserved); err != nil {
		t.Fatalf("preserved file: %v", err)
	}
	if rep := r.reconcileTwice(); len(rep.Changes) != 0 {
		t.Fatalf("%+v", rep.Changes)
	}
	if res := r.observe(d1, lbLive("o1")); res.Kind != ledger.ObserveAdopted || res.Entry.Applied != nil {
		t.Fatalf("%+v", res)
	}
	if r.wouldDispatch() {
		t.Fatal("0 dispatches")
	}
}

// Scenario 7 (bridge view): the key is lost (ledger present, key file gone),
// unsafe (mode) or malformed: memory-only, no keyed identity, no Generate, no
// rotation. Key mismatch preservation is covered by internal/ledger tests.
func TestLedgerBridge_Scenario07_KeyUnavailable(t *testing.T) {
	cases := map[string]func(t *testing.T, keyFile string){
		"lost": func(t *testing.T, keyFile string) {
			if err := os.Remove(keyFile); err != nil {
				t.Fatal(err)
			}
		},
		"unsafe mode": func(t *testing.T, keyFile string) {
			if err := os.Chmod(keyFile, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"malformed": func(t *testing.T, keyFile string) {
			_ = os.Chmod(keyFile, 0o600)
			if err := os.WriteFile(keyFile, bytes.Repeat([]byte("x"), 82), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(keyFile, 0o400)
		},
	}
	for name, breakKey := range cases {
		t.Run(name, func(t *testing.T) {
			r := newLBRig(t, nil)
			r.baseline(true)
			_ = r.s.Close()
			keyFile := filepath.Join(r.ldir, "ledger.key")
			breakKey(t, keyFile)
			before, _ := os.ReadFile(keyFile)
			r.openLedger()
			r.openJournal()
			if r.res.Mode != ledger.ModeMemoryOnly {
				t.Fatalf("open result %+v", r.res)
			}
			if _, err := r.v.NewVersion(lbProvider, lbSecret, map[string]string{"password": "two"}); !errors.Is(err, ledger.ErrKeyUnavailable) {
				t.Fatalf("no keyed identity: %v", err)
			}
			if d := r.v.NeedsRotation(lbKey); d.Rotate() {
				t.Fatalf("decision %+v", d)
			}
			if rep := r.reconcileTwice(); len(rep.Changes) != 0 {
				t.Fatalf("%+v", rep.Changes)
			}
			after, _ := os.ReadFile(keyFile)
			if !bytes.Equal(before, after) {
				t.Fatal("the key file was regenerated or modified")
			}
		})
	}
}

// Scenario 10 (v1 all-or-nothing, design 1.5): three replicas, the fan-out
// ends partial (one target ready, one failed; the engine marks rollback):
// every targeted replica failed, applied unchanged, re-dispatched.
func TestLedgerBridge_Scenario10_PartialFanout(t *testing.T) {
	r := newLBRig(t, nil)
	live := lbLive("o1", "o2", "o3")
	d1 := r.ver("one")
	r.observe(d1, live)
	all := lbFlow{strategy: "rolling", orig: "o1,o2,o3", cand: "o1"}
	if err := r.correlate("rot-b", d1, r.journalStart("rot-b", all)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, ev := range []rotation.JournalEvent{
		{RotationID: "rot-b", Strategy: "rolling", ServiceID: "shop-app-1", Transition: rotation.TrCandidateReady, CandidateIDs: []string{"n1"}},
		{RotationID: "rot-b", Strategy: "rolling", ServiceID: "shop-app-1", Transition: rotation.TrCompleted},
		{RotationID: "rot-b", Strategy: "rolling", ServiceID: "shop-app-2", Transition: rotation.TrFailed},
	} {
		if err := r.st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.st.MarkRollback(lbProvider, lbSecret, all.orig) // finishRotation: asyncErr
	res, err := RecordRotationOutcome(r.v, r.st, lbKey, "rot-b", &d1, false, nil)
	if err != nil || res.Cause != ledger.CauseFailed {
		t.Fatalf("%+v %v", res, err)
	}
	e := r.entry()
	if e.Applied != nil || e.State != ledger.EntryFailed {
		t.Fatalf("entry %+v", e)
	}
	for _, rp := range e.Replicas {
		if rp.Outcome != ledger.ReplicaFailed || rp.Delivery != ledger.DeliveryNone {
			t.Fatalf("every targeted replica failed: %+v", rp)
		}
	}
	// A failed first delivery of an adopted entry leaves desired_seq 1: the
	// ledger has no baseline, so the retry is the engine's (rollbackHash): S6.
	d2 := r.ver("two")
	r.observe(d2, live)
	if !r.wouldDispatch() {
		t.Fatal("a changed value is rotated")
	}
}

// Scenario 15: unknown or ambiguous identity never rotates.
func TestLedgerBridge_Scenario15_IdentityChanged(t *testing.T) {
	t.Run("recreated container (Observe)", func(t *testing.T) {
		r := newLBRig(t, nil)
		d1 := r.baseline(true)
		res := r.observe(d1, lbLive("o9")) // same number and name, new container
		if res.Kind != ledger.ObserveReplicasChanged {
			t.Fatalf("%+v", res)
		}
		rp := res.Entry.Replicas[0]
		if rp.Reason != string(ledger.WhyIdentityChanged) || rp.Adoption != ledger.AdoptionUnknown || rp.Applied != nil {
			t.Fatalf("replica %+v", rp)
		}
		if r.wouldDispatch() {
			t.Fatal("no rotation")
		}
	})
	t.Run("recreated container (reconcile live set)", func(t *testing.T) {
		r := newLBRig(t, nil)
		r.baseline(true)
		r.crash()
		rep, err := ReconcileLedgerFromTracker(r.v, r.st, LedgerReconcileInput{Live: map[ledger.Key][]ledger.LiveReplica{lbKey: lbLive("o9")}})
		if err != nil || !hasRule(rep, "identity_changed") {
			t.Fatalf("%v %v", rules(rep), err)
		}
		if r.wouldDispatch() {
			t.Fatal("no rotation")
		}
	})
	t.Run("duplicate number and name", func(t *testing.T) {
		r := newLBRig(t, nil)
		d1 := r.baseline(true)
		dup := []ledger.LiveReplica{lbLive("o1")[0], lbLive("o7")[0]}
		res := r.observe(d1, dup)
		for _, rp := range res.Entry.Replicas {
			if rp.Reason != string(ledger.WhyIdentityChanged) || rp.Adoption != ledger.AdoptionUnknown {
				t.Fatalf("ambiguous replica must be unknown: %+v", rp)
			}
		}
		if r.wouldDispatch() {
			t.Fatal("no rotation")
		}
	})
	t.Run("label loss makes a new entry", func(t *testing.T) {
		r := newLBRig(t, nil)
		d1 := r.baseline(true)
		before := r.entry()
		bare := ledger.Key{Service: lbService, Provider: lbProvider, Secret: lbSecret}
		res, err := r.v.Observe(bare, d1, lbLive("o1"))
		if err != nil || res.Kind != ledger.ObserveAdopted || res.Entry.Applied != nil {
			t.Fatalf("%+v %v", res, err)
		}
		if after := r.entry(); after.State != before.State || !appliedIs(after, d1) {
			t.Fatal("the old entry is left inert")
		}
		if r.v.NeedsRotation(bare).Rotate() || r.wouldDispatch() {
			t.Fatal("no rotation")
		}
	})
}

// ============================================================ wiring boundary

// S6 wired the bridge: its entry points have exactly one production caller,
// the trigger-engine wiring in ledger_wiring.go (state_tracker.go uses only the
// version-shape check). No other non-test Go file references them.
func TestLedgerBridge_ProductionCallersOnlyInWiring(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root not found: %v", err)
	}
	names := map[string]bool{
		"ReconcileLedgerFromTracker": true, "RecordRotationOutcome": true, "EvidenceForRotation": true,
		"CorrelateAfterJournalStart": true, "JournalSnapshotFromTracker": true, "JournalRecordFor": true,
		"InterruptCauseFor": true, "JournalRecordByID": true, "FormatJournalSecretVersion": true,
		"parseJournalSecretVersion": true, "journalEvidenceKind": true, "journalDeliveryRefusal": true,
	}
	fset := token.NewFileSet()
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); (strings.HasPrefix(n, ".") && path != root) || n == "vendor" || n == "node_modules" || n == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "ledger_bridge.go" ||
			filepath.Base(path) == "ledger_wiring.go" {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && names[id.Name] {
				t.Errorf("%s references the bridge (%s) outside ledger_wiring.go", path, id.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 50 {
		t.Fatalf("only %d files scanned", scanned)
	}
}

// N21 controlled automatic rollback x ledger (kept here: only the bridge tests may import the ledger).
// POLICY: the ledger reconciliation after a rollback records rolled back / failed,
// never applied; the guard rule is unchanged (recovered unblocks, critical blocks).
func TestN21Rollback_LedgerReconcileNeverRecordsApplied(t *testing.T) {
	for _, tc := range []struct {
		name         string
		composeLabel bool // makes the previous version known to the ledger source
		wantStatus   string
	}{
		{"previous version known", true, "recovered"},
		{"previous version unavailable", false, "critical_error"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			o := n21Original(false)
			if tc.composeLabel {
				o.labels = map[string]string{"com.docker.compose.project": "shop", "com.docker.compose.service": "app"}
			}
			d := newFakeDaemon(o, n21Candidate(true, "healthy"))
			r := newLBRig(t, d)
			d1 := r.baseline(true)
			d2 := r.ver("two")
			r.observe(d2, lbLive("o1"))
			fl := lbFlow{strategy: "restart", orig: "o1", cand: "c1", o2: true}
			if err := r.correlate(n21RID, d2, r.journalStart(n21RID, fl)); err != nil {
				t.Fatal(err)
			}
			r.journalTo(n21RID, fl, d2, rotation.TrReplacementStarted)
			r.crash()
			r.f.ar.AutomaticRollback = true
			r.f.ar.PreviousVersion = ledgerPreviousSource{lw: &ledgerWiring{mode: ledgerEnabled, v: r.v}}
			r.recover()
			rbAssertRolledBack(t, d, true)
			if s := r.record(n21RID).Status; s != tc.wantStatus {
				t.Fatalf("journal status %s, want %s", s, tc.wantStatus)
			}
			r.reconcileTwice()
			e := r.entry()
			if !appliedIs(e, d1) {
				t.Fatalf("applied must stay at the previous version: %+v", e)
			}
			for _, rp := range e.Replicas {
				if rp.Applied != nil && ledger.Compare(*rp.Applied, d2) == ledger.Same {
					t.Fatal("the rolled-back version must never be recorded as applied")
				}
			}
			t.Logf("ledger entry after rollback+reconcile: state=%s last=%+v", e.State, e.LastOutcome)
			if tc.wantStatus == "critical_error" && !r.blocked() {
				t.Fatal("an unconfirmed application state must keep the guard blocked")
			}
			if tc.wantStatus == "recovered" && r.blocked() {
				t.Fatal("a recovered rollback must not block the service")
			}
		})
	}
}

func TestN21Rollback_LedgerPreviousSourceRules(t *testing.T) {
	d := newFakeDaemon()
	r := newLBRig(t, d)
	labels := map[string]string{"com.docker.compose.project": "shop", "com.docker.compose.service": "app"}
	src := ledgerPreviousSource{lw: &ledgerWiring{mode: ledgerEnabled, v: r.v}}
	rec := &RotationState{ProviderName: lbProvider, SecretName: lbSecret, ServiceID: lbService}
	if src.PreviousDeliveredKnown(rec, labels) {
		t.Fatal("no ledger entry: the previous version is unknown")
	}
	d1 := r.baseline(true)
	if !src.PreviousDeliveredKnown(rec, labels) {
		t.Fatal("an applied version exists: known")
	}
	rec.SecretVersion = FormatJournalSecretVersion(d1)
	if src.PreviousDeliveredKnown(rec, labels) {
		t.Fatal("the applied version equals the attempted one: nothing distinguishes previous from attempted")
	}
	if (ledgerPreviousSource{}).PreviousDeliveredKnown(rec, labels) ||
		(ledgerPreviousSource{lw: &ledgerWiring{mode: ledgerDisabled}}).PreviousDeliveredKnown(rec, labels) {
		t.Fatal("a missing or disabled ledger can never show the previous version")
	}
}
