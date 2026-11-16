package agent

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// N21 controlled automatic rollback (recovery_rollback.go). Docker-free: the stateful
// fake daemon of recovery_safety_test.go plus the REAL StateTracker in t.TempDir. The
// decision table of docs/design/m0-04-recovery-design.md section 6 has one test per
// row. The pre-existing N21 tests keep running with AutomaticRollback off and are
// unchanged: the preserve-and-escalate behaviour stays the default.

type prevStub bool

func (p prevStub) PreviousDeliveredKnown(*RotationState, map[string]string) bool { return bool(p) }

// rbFixture is a recovery fixture with the rollback enabled.
func rbFixture(t *testing.T, d *fakeDaemon, previousKnown bool) *recFixture {
	t.Helper()
	f := newRec(t, d, "-")
	f.ar.AutomaticRollback = true
	f.ar.PreviousVersion = prevStub(previousKnown)
	return f
}

type snapshotCtr struct {
	name    string
	running bool
}

func (d *fakeDaemon) snapshot() map[string]snapshotCtr {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]snapshotCtr{}
	for id, c := range d.ctrs {
		out[id] = snapshotCtr{c.name, c.running}
	}
	return out
}

func sameSnapshot(a, b map[string]snapshotCtr) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (f *recFixture) record(rid string) *RotationState { return JournalRecordByID(f.st, rid) }

// rbAssertRolledBack asserts the original holds the real name and runs, the candidate
// (if any) is stopped and moved aside under an artifact name, nothing was deleted.
func rbAssertRolledBack(t *testing.T, d *fakeDaemon, hadCandidate bool) {
	t.Helper()
	if len(d.removed) != 0 {
		t.Fatalf("rollback must never delete: removed=%v", d.removed)
	}
	o := d.find("o1")
	if o == nil || o.name != "app" || !o.running {
		t.Fatalf("the original must hold its real name and run: %+v", o)
	}
	if hadCandidate {
		c := d.find("c1")
		if c == nil || c.running || !strings.HasPrefix(c.name, "app_dso_new_") {
			t.Fatalf("the unverified candidate must be stopped and moved aside, not deleted: %+v", c)
		}
	}
	if b := d.find("b1"); b != nil && (b.name != "unrelated" || !b.running) {
		t.Fatalf("a bystander was touched: %+v", b)
	}
}

// POLICY: replacement failed/unverified and the original verified recoverable =>
// controlled automatic rollback; the unverified replacement is never promoted.
func TestN21Rollback_SafeRollback(t *testing.T) {
	for _, health := range []string{"", "healthy"} {
		for _, step := range []string{rotation.TrIntentStartReplacement, rotation.TrReplacementStarted} {
			t.Run(step+"/health="+health, func(t *testing.T) {
				d := newFakeDaemon(n21Original(false), n21Candidate(true, health), healthy("b1", "unrelated"))
				f := rbFixture(t, d, true)
				n21Journal(t, f, step)
				f.run()

				rbAssertRolledBack(t, d, true)
				eq(t, "stopped", d.stopped, []string{"c1"})
				eq(t, "started", d.started, []string{"o1"})
				rec := f.record(n21RID)
				if rec.Status != "recovered" || rec.Transition != rotation.TrRolledBack {
					t.Fatalf("status=%s transition=%s", rec.Status, rec.Transition)
				}
				// Container recovery is reported separately from application recovery.
				if rec.OperationState != opStateRollbackAppUnverified ||
					!strings.Contains(rec.Detail, "container restored") || !strings.Contains(rec.Detail, "UNVERIFIED") ||
					!strings.Contains(rec.Detail, "secret delivery was NOT rolled back") {
					t.Fatalf("the journal must state precisely what was and was not done: %q / %q", rec.OperationState, rec.Detail)
				}
				// The existing orphan sweep (unchanged rule) then retires the moved-aside
				// candidate because the verified original holds the name.
				if err := f.ar.CleanupOrphanedContainers(context.Background()); err != nil {
					t.Fatal(err)
				}
				if o := d.find("o1"); o == nil || o.name != "app" || !o.running {
					t.Fatal("the original must survive the sweep")
				}
				if len(d.removedSorted()) != 1 || !strings.HasPrefix(d.removedSorted()[0], "app_dso_new_") {
					t.Fatalf("only the moved-aside candidate may be retired: %v", d.removedSorted())
				}
			})
		}
	}
}

// Every pre-ready layout of the restart flow that now rolls back. Failed or partial
// delivery (delivery done, partial or failed: the journal cannot say) never promotes a
// candidate; a SERVING original is preserved (not stopped, not restarted).
func TestN21Rollback_EveryEligibleLayout(t *testing.T) {
	cases := []struct {
		step      string
		origRun   bool
		candidate *fakeCtr
		desc      string
	}{
		{rotation.TrIntentRenameOriginal, true, nil, "renamed away, nothing created"},
		{rotation.TrIntentCreateCandidate, true, nil, "create intent, candidate absent"},
		{rotation.TrIntentCreateCandidate, true, n21Candidate(false, ""), "candidate created before its ack (found by label)"},
		{rotation.TrCandidateCreated, true, n21Candidate(false, ""), "delivery not attempted"},
		{rotation.TrIntentStopOriginal, true, n21Candidate(false, ""), "partial/failed delivery, original still serving"},
		{rotation.TrIntentStopOriginal, false, n21Candidate(false, ""), "original stopped, ack lost"},
		{rotation.TrOriginalStopped, false, n21Candidate(false, ""), "original stopped, candidate not started"},
		{rotation.TrIntentStartReplacement, false, n21Candidate(false, ""), "candidate never started"},
		{rotation.TrIntentStartReplacement, false, n21Candidate(true, "starting"), "candidate started, health starting"},
		{rotation.TrReplacementStarted, false, n21Candidate(true, "unhealthy"), "candidate unhealthy"},
		{rotation.TrReplacementStarted, false, n21Candidate(true, "healthy"), "candidate healthy but delivery unverified"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.step+"/"+c.desc, func(t *testing.T) {
			cs := []*fakeCtr{n21Original(c.origRun), healthy("b1", "unrelated")}
			if c.candidate != nil {
				cs = append(cs, c.candidate)
			}
			d := newFakeDaemon(cs...)
			f := rbFixture(t, d, true)
			n21Journal(t, f, c.step)
			f.run()
			rbAssertRolledBack(t, d, c.candidate != nil)
			if c.origRun {
				for _, id := range append(append([]string(nil), d.started...), d.stopped...) {
					if id == "o1" {
						t.Fatalf("a serving original must be preserved, not restarted: started=%v stopped=%v", d.started, d.stopped)
					}
				}
			}
			for _, id := range d.started {
				if id == "c1" {
					t.Fatal("the unverified candidate must never be started or promoted")
				}
			}
			if s := f.record(n21RID).Status; s != "recovered" {
				t.Fatalf("status %s", s)
			}
		})
	}
}

// POLICY: original healthy + serving under its real name => preserve it, untouched.
func TestN21Rollback_OriginalHealthyServingIsPreserved(t *testing.T) {
	o := healthy("o1", "app")
	c := n21Candidate(false, "")
	c.name = "app_dso_new_1700000000"
	d := newFakeDaemon(o, c, healthy("b1", "unrelated"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrCandidateCreated)
	f.run()
	// (The existing, unchanged retirement rule stops and removes the leftover candidate.)
	if len(d.started) != 0 || len(d.stopped) != 1 || d.stopped[0] != "c1" {
		t.Fatalf("the serving original must not be touched: %+v", d)
	}
	if o := d.find("o1"); o == nil || o.name != "app" || !o.running {
		t.Fatal("original changed")
	}
}

// POLICY: a candidate that passed the readiness gate is not rolled back; the existing
// rule (original retired, candidate serves) is unchanged with the rollback enabled.
func TestN21Rollback_VerifiedCandidateIsNotRolledBack(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrCandidateReady)
	f.run()
	eq(t, "removed", d.removedSorted(), []string{"app_old_1700000000"})
	// (The existing retirement rule stops the retired original before removing it.)
	if len(d.started) != 0 || len(d.stopped) != 1 || d.stopped[0] != "o1" || f.status() != "recovered" {
		t.Fatalf("%+v %s", d, f.status())
	}
}

// POLICY: original missing => no invented rollback target; ownership/identity
// ambiguity => block. Nothing is mutated and the reason is in the escalation.
func TestN21Rollback_BlockedWhenSafetyCannotBeEstablished(t *testing.T) {
	cases := []struct {
		name string
		make func() []*fakeCtr
		want string
	}{
		{"original missing", func() []*fakeCtr { return []*fakeCtr{n21Candidate(true, "healthy")} }, "missing"},
		{"original owned by another agent", func() []*fakeCtr {
			o := n21Original(false)
			o.owner = "owner-B"
			return []*fakeCtr{o, n21Candidate(true, "healthy")}
		}, ""},
		{"candidate owned by another agent", func() []*fakeCtr {
			c := n21Candidate(true, "healthy")
			c.owner = "owner-B"
			return []*fakeCtr{n21Original(false), c}
		}, "ownership"},
		{"two candidates (label + journalled id)", func() []*fakeCtr {
			c2 := n21Candidate(true, "healthy")
			c2.id, c2.name = "c2", "app_dso_new_1700000001"
			return []*fakeCtr{n21Original(false), n21Candidate(true, "healthy"), c2}
		}, "ambiguous"},
		{"a stranger holds the real name", func() []*fakeCtr {
			return []*fakeCtr{n21Original(false), healthy("x9", "app")}
		}, "not this operation's candidate"},
		{"candidate carries another operation's rotation id", func() []*fakeCtr {
			c := n21Candidate(true, "healthy")
			c.labels = map[string]string{rotation.LabelRotationID: "rot-other"}
			c.id = "c1" // journalled id, foreign label
			return []*fakeCtr{n21Original(false), c}
		}, "another operation"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			d := newFakeDaemon(c.make()...)
			f := rbFixture(t, d, true)
			n21Journal(t, f, rotation.TrReplacementStarted)
			before := d.snapshot()
			f.run()
			if len(d.removed)+len(d.started)+len(d.stopped) != 0 || !sameSnapshot(before, d.snapshot()) {
				t.Fatalf("no mutation allowed: %+v", d)
			}
			rec := f.record(n21RID)
			if rec.Status != "critical_error" {
				t.Fatalf("status %s", rec.Status)
			}
			if c.want != "" && !strings.Contains(rec.ErrorMessage, c.want) {
				t.Fatalf("the escalation must say why: %q (want %q)", rec.ErrorMessage, c.want)
			}
			// Repeated recovery changes nothing.
			f.run()
			if !sameSnapshot(before, d.snapshot()) {
				t.Fatal("repeat recovery mutated")
			}
		})
	}
}

// POLICY: journal corrupt => preserve evidence, fail closed (rollback enabled).
func TestN21Rollback_CorruptJournalFailsClosed(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrReplacementStarted)
	f.st.mu.Lock()
	f.st.corrupt, f.st.corruptPath = true, "rotations.json.corrupt-test"
	f.st.mu.Unlock()
	before := d.snapshot()
	f.run()
	if len(d.removed)+len(d.started)+len(d.stopped) != 0 || !sameSnapshot(before, d.snapshot()) {
		t.Fatal("a corrupt journal must disable every automatic action")
	}
}

// POLICY: inconsistent journal (unknown transition) => no rollback.
func TestN21Rollback_UnknownTransitionIsIneligible(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrCandidateCreated)
	if err := f.st.Record(context.Background(), rotation.JournalEvent{RotationID: n21RID, Transition: "some_future_transition"}); err != nil {
		t.Fatal(err)
	}
	f.run()
	if len(d.removed)+len(d.started)+len(d.stopped) != 0 {
		t.Fatalf("%+v", d)
	}
}

// POLICY: unresolved critical_error stays blocked and is never rolled back or
// downgraded, not even by a sibling target's success.
func TestN21Rollback_CriticalErrorStaysBlockedAndIsNotDowngraded(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrReplacementStarted)
	if err := f.st.MarkCriticalRecord(f.record(n21RID), "operator attention"); err != nil {
		t.Fatal(err)
	}
	before := d.snapshot()
	f.run() // the existing flow re-evaluates it: the unverified candidate is still no holder
	if !sameSnapshot(before, d.snapshot()) || len(d.removed)+len(d.started)+len(d.stopped) != 0 {
		t.Fatalf("a critical_error record is never rolled back: %+v", d)
	}
	// A sibling target of the same dispatch finishing successfully does not downgrade it.
	if err := f.st.Record(context.Background(), rotation.JournalEvent{RotationID: n21RID, Transition: rotation.TrCompleted}); err != nil {
		t.Fatal(err)
	}
	if s := f.record(n21RID).Status; s != "critical_error" {
		t.Fatalf("a sibling's completion downgraded critical_error to %s", s)
	}
	if _, blocked := newJournalGuard(f.st).RotationBlocked("app"); !blocked {
		t.Fatal("the service must stay blocked")
	}
}

// POLICY: stale operation: an OLDER RotationID cannot roll back containers a NEWER
// operation names, nor can a record with an unresolved sibling.
func TestN21Rollback_StaleOperationCannotRollBackANewerOperationsContainers(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrReplacementStarted)
	time.Sleep(5 * time.Millisecond)
	// A newer, completed operation whose original is the older one's candidate (F-B1 shape).
	ctx := context.Background()
	for _, ev := range []rotation.JournalEvent{
		{RotationID: "rot-newer", ServiceID: "app", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal, OriginalIDs: []string{"c1"}},
		{RotationID: "rot-newer", Transition: rotation.TrCompleted},
	} {
		if err := f.st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	before := d.snapshot()
	f.run()
	if !sameSnapshot(before, d.snapshot()) || len(d.removed)+len(d.started)+len(d.stopped) != 0 {
		t.Fatalf("the stale operation acted on a newer operation's containers: %+v", d)
	}
	if s := f.record(n21RID).Status; s != "critical_error" {
		t.Fatalf("status %s", s)
	}
	if !strings.Contains(f.record(n21RID).ErrorMessage, "newer operation") {
		t.Fatalf("reason missing: %q", f.record(n21RID).ErrorMessage)
	}

	// An unresolved sibling operation of the same service also blocks it.
	d2 := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f2 := rbFixture(t, d2, true)
	if err := f2.st.Record(ctx, rotation.JournalEvent{RotationID: "rot-sibling", ServiceID: "app", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal, OriginalIDs: []string{"zz"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	n21Journal(t, f2, rotation.TrReplacementStarted)
	before2 := d2.snapshot()
	f2.run()
	if !sameSnapshot(before2, d2.snapshot()) {
		t.Fatalf("an unresolved sibling must block the rollback: %+v", d2)
	}
}

// POLICY: repeated recovery is idempotent: the second run is a no-op (no double
// rollback), also across a restart (state reloaded from disk).
func TestN21Rollback_RepeatedRecoveryIsIdempotent(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"), healthy("b1", "unrelated"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrReplacementStarted)
	f.run()
	after1 := d.snapshot()
	s1, st1 := len(d.started), len(d.stopped)
	f.run()
	if !sameSnapshot(after1, d.snapshot()) || len(d.started) != s1 || len(d.stopped) != st1 {
		t.Fatalf("second recovery must be a no-op: started=%v stopped=%v", d.started, d.stopped)
	}
	// A new process (journal reloaded from disk) also does nothing.
	st2, err := NewStateTracker(f.st.stateDir, f.ar.logger)
	if err != nil {
		t.Fatal(err)
	}
	f.ar.st = st2
	f.run()
	if !sameSnapshot(after1, d.snapshot()) || len(d.started) != s1 || len(d.stopped) != st1 {
		t.Fatal("recovery after a restart changed something")
	}
}

// A rollback this code started and a crash interrupted is resumed from the layout.
func TestN21Rollback_ResumesAfterACrashInsideTheRollback(t *testing.T) {
	t.Run("candidate already aside, original not yet restored", func(t *testing.T) {
		c := n21Candidate(false, "")
		c.name = "app_dso_new_1700000009"
		d := newFakeDaemon(n21Original(false), c)
		f := rbFixture(t, d, true)
		n21Journal(t, f, rotation.TrReplacementStarted)
		if err := f.st.Record(context.Background(), rotation.JournalEvent{RotationID: n21RID, Transition: rotation.TrIntentRollback}); err != nil {
			t.Fatal(err)
		}
		f.run()
		rbAssertRolledBack(t, d, true)
		if f.record(n21RID).Status != "recovered" {
			t.Fatalf("status %s", f.record(n21RID).Status)
		}
	})
	t.Run("original already restored: ordinary rules finish the job", func(t *testing.T) {
		c := n21Candidate(false, "")
		c.name = "app_dso_new_1700000009"
		o := healthy("o1", "app")
		d := newFakeDaemon(o, c)
		f := rbFixture(t, d, true)
		n21Journal(t, f, rotation.TrReplacementStarted)
		if err := f.st.Record(context.Background(), rotation.JournalEvent{RotationID: n21RID, Transition: rotation.TrIntentRollback}); err != nil {
			t.Fatal(err)
		}
		f.run()
		if o := d.find("o1"); o == nil || o.name != "app" || !o.running {
			t.Fatal("original must keep serving")
		}
		if f.record(n21RID).Status != "recovered" {
			t.Fatalf("status %s", f.record(n21RID).Status)
		}
	})
}

// POLICY: recovery does not depend on Docker listing or journal map order.
func TestN21Rollback_OrderIndependent(t *testing.T) {
	type outcome struct{ names, status string }
	run := func(seed int64) outcome {
		rnd := rand.New(rand.NewSource(seed)) //nolint:gosec // test shuffling only
		mk := func(id, name string, running bool, rid string) *fakeCtr {
			c := &fakeCtr{id: id, name: name, owner: me, running: running, startHealth: "healthy", labels: map[string]string{}}
			if running {
				c.upFor, c.health = time.Hour, "healthy"
			}
			if rid != "" {
				c.labels[rotation.LabelRotationID] = rid
			}
			return c
		}
		cs := []*fakeCtr{
			mk("a-o", "alpha_old_1700000000", false, ""), mk("a-c", "alpha", true, "rot-a"), // rolls back
			mk("b-o", "beta_old_1700000000", false, ""), mk("b-c", "beta", true, "rot-b"), mk("b-c2", "beta_dso_new_1700000002", true, "rot-b"), // two candidates
			healthy("z", "unrelated"),
		}
		rnd.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
		d := newFakeDaemon(cs...)
		f := rbFixture(t, d, true)
		type rot struct{ rid, svc, o, c string }
		rots := []rot{{"rot-a", "alpha", "a-o", "a-c"}, {"rot-b", "beta", "b-o", "b-c"}}
		rnd.Shuffle(len(rots), func(i, j int) { rots[i], rots[j] = rots[j], rots[i] })
		for _, r := range rots {
			for _, ev := range []rotation.JournalEvent{
				{RotationID: r.rid, ServiceID: r.svc, Strategy: "restart", Transition: rotation.TrIntentRenameOriginal, OriginalIDs: []string{r.o}},
				{RotationID: r.rid, Transition: rotation.TrReplacementStarted, CandidateIDs: []string{r.c}},
			} {
				if err := f.st.Record(context.Background(), ev); err != nil {
					t.Fatal(err)
				}
			}
		}
		f.run()
		var st []string
		for _, r := range f.st.snapshotRecords() {
			st = append(st, r.RotationID+"="+r.Status)
		}
		sort.Strings(st)
		return outcome{strings.Join(d.names(), ","), strings.Join(st, ",")}
	}
	want := run(1)
	if !strings.Contains(want.status, "rot-a=recovered") || !strings.Contains(want.status, "rot-b=critical_error") {
		t.Fatalf("unexpected baseline outcome: %+v", want)
	}
	for seed := int64(2); seed < 14; seed++ {
		if got := run(seed); got != want {
			t.Fatalf("seed %d: outcome depends on order: %+v vs %+v", seed, got, want)
		}
	}
}

// POLICY: previous secret version unavailable => container restored, application
// rollback NOT claimed, record stays blocked and is not re-resolved by later starts.
func TestN21Rollback_PreviousSecretUnavailable_ContainerRestoredApplicationNotClaimed(t *testing.T) {
	for _, source := range []string{"stub says unknown", "no source wired"} {
		t.Run(source, func(t *testing.T) {
			d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
			f := rbFixture(t, d, false)
			if source == "no source wired" {
				f.ar.PreviousVersion = nil
			}
			n21Journal(t, f, rotation.TrReplacementStarted)
			f.run()
			rbAssertRolledBack(t, d, true) // the container IS restored
			rec := f.record(n21RID)
			if rec.Status != "critical_error" || !rec.ManualRecovery || rec.OperationState != opStateRollbackPreviousUnavailable {
				t.Fatalf("application rollback must not be claimed: %+v", rec)
			}
			if rec.Transition != rotation.TrRolledBack || !strings.Contains(rec.ErrorMessage, "application rollback is NOT claimed") {
				t.Fatalf("%s / %q", rec.Transition, rec.ErrorMessage)
			}
			if _, blocked := newJournalGuard(f.st).RotationBlocked("app"); !blocked {
				t.Fatal("the service must stay blocked until an operator confirms")
			}
			before := d.snapshot()
			f.run() // a later start must not downgrade it nor remove the preserved candidate
			if !sameSnapshot(before, d.snapshot()) || f.record(n21RID).Status != "critical_error" {
				t.Fatalf("critical record re-resolved: %s %+v", f.record(n21RID).Status, d)
			}
		})
	}
}

// POLICY: routing cannot be verified => no destructive handoff (proxied operations).
func TestN21Rollback_ProxiedRoutingMustBeVerified(t *testing.T) {
	layout := func() []*fakeCtr {
		o := n21Original(true)
		o.labels = map[string]string{"dso.host_ports": "8080:80"}
		c := n21Candidate(true, "healthy")
		c.labels["dso.host_ports"] = "8080:80"
		return []*fakeCtr{o, c}
	}
	for _, tc := range []struct {
		name     string
		routing  RoutingVerifier
		rollback bool
	}{
		{"no routing verifier", nil, false},
		{"routing unknown", fixedRouting{known: false}, false},
		{"candidate receives traffic", fixedRouting{ids: []string{"c1"}, known: true}, false},
		{"original receives traffic, candidate does not", fixedRouting{ids: []string{"o1"}, known: true}, true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDaemon(layout()...)
			f := rbFixture(t, d, true)
			f.ar.Routing = tc.routing
			n21Journal(t, f, rotation.TrIntentStartReplacement)
			before := d.snapshot()
			f.run()
			if !tc.rollback {
				if !sameSnapshot(before, d.snapshot()) || len(d.removed)+len(d.started)+len(d.stopped) != 0 {
					t.Fatalf("no destructive handoff without verified routing: %+v", d)
				}
				if f.record(n21RID).Status != "critical_error" {
					t.Fatalf("status %s", f.record(n21RID).Status)
				}
				return
			}
			rbAssertRolledBack(t, d, true)
			eq(t, "stopped", d.stopped, []string{"c1"})
			if len(d.started) != 0 {
				t.Fatalf("the serving original must not be restarted: %v", d.started)
			}
		})
	}
}

// If the restored original does not become healthy nothing further is done: no
// deletion, escalation with the layout preserved.
func TestN21Rollback_OriginalNotHealthyAfterRestore_Escalates(t *testing.T) {
	o := n21Original(false)
	o.startHealth = "unhealthy"
	d := newFakeDaemon(o, n21Candidate(true, "healthy"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrReplacementStarted)
	f.run()
	if len(d.removed) != 0 {
		t.Fatalf("nothing may be deleted: %v", d.removed)
	}
	rec := f.record(n21RID)
	if rec.Status != "critical_error" || !strings.Contains(rec.ErrorMessage, "health could not be verified") {
		t.Fatalf("%s %q", rec.Status, rec.ErrorMessage)
	}
}

// Without AutomaticRollback the behaviour is exactly the pre-existing preserve and
// escalate (the production startup path enables it; the zero value does not).
func TestN21Rollback_DisabledByDefault(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f := newRec(t, d, "-")
	n21Journal(t, f, rotation.TrReplacementStarted)
	f.run()
	n21AssertPreserved(t, d, f)
}

// An original whose name is not this operation's rename-away name is not a rollback
// target: the rollback does not act (the unverified candidate keeps its place).
func TestN21Rollback_OriginalWithForeignNameIsNotATarget(t *testing.T) {
	o := n21Original(false)
	o.name = "app-renamed-by-someone"
	d := newFakeDaemon(o, n21Candidate(true, "healthy"))
	f := rbFixture(t, d, true)
	n21Journal(t, f, rotation.TrReplacementStarted)
	f.run()
	if c := d.find("c1"); c == nil || c.name != "app" || !c.running || len(d.removed) != 0 {
		t.Fatalf("the rollback must not act on a foreign-named original: %+v", d)
	}
}
