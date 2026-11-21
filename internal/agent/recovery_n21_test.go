package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// N21: recovery must not accept a real-name holder as the workload that justifies
// retiring the original unless the journal shows the holder is NOT an unverified
// candidate. For the restart (full recreation) strategy the original is renamed
// "<name>_old_<ts>" and (without a proxy) stopped BEFORE the replacement starts, so a
// replacement that is merely running (even for longer than the 30 s stability
// window, even HEALTHY) has not been through the journalled readiness gate
// (TrCandidateReady = started, health verified, file-secret probe passed). Docker
// status and application health are not evidence of secret delivery.
//
// The daemon layouts below are the exact layouts the real restart code leaves at each
// journal step (internal/watcher/controller.go executeSimpleRestart / the primary
// restart branch); the journal is written through the REAL StateTracker.

var restartSteps = []string{
	rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated,
	rotation.TrIntentStopOriginal, rotation.TrOriginalStopped,
	rotation.TrIntentStartReplacement, rotation.TrReplacementStarted,
	rotation.TrCandidateReady, rotation.TrIntentCleanup,
}

const n21RID = "rot-n21"

// n21Journal records the restart journal up to and including upTo.
func n21Journal(t *testing.T, f *recFixture, upTo string) {
	t.Helper()
	ctx := context.Background()
	for _, tr := range restartSteps {
		ev := rotation.JournalEvent{RotationID: n21RID, Transition: tr}
		switch tr {
		case rotation.TrIntentRenameOriginal:
			ev.ServiceID, ev.Strategy, ev.OriginalIDs = "app", "restart", []string{"o1"}
		case rotation.TrCandidateCreated, rotation.TrIntentStopOriginal, rotation.TrIntentStartReplacement,
			rotation.TrReplacementStarted, rotation.TrCandidateReady:
			ev.CandidateIDs = []string{"c1"}
		case rotation.TrIntentCleanup, rotation.TrOriginalStopped:
			ev.OriginalIDs = []string{"o1"}
		}
		if err := f.st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
		if tr == upTo {
			return
		}
	}
	t.Fatalf("unknown step %s", upTo)
}

func n21Original(running bool) *fakeCtr {
	o := &fakeCtr{id: "o1", name: "app_old_1700000000", owner: me, running: running, upFor: time.Hour, startHealth: "healthy"}
	if running {
		o.health = "healthy"
	}
	return o
}

func n21Candidate(running bool, health string) *fakeCtr {
	c := &fakeCtr{id: "c1", name: "app", owner: me, running: running, health: health, startHealth: "healthy",
		labels: map[string]string{rotation.LabelRotationID: n21RID}}
	if running {
		c.upFor = time.Hour // running far longer than the 30 s stability window
	}
	return c
}

func (f *recFixture) transition() string {
	for _, r := range f.st.snapshotRecords() {
		if r.RotationID == n21RID {
			return r.Transition
		}
	}
	return "<none>"
}

// The reproduction: original stopped and renamed, replacement started and running
// (healthy, or stable with no HEALTHCHECK), journal NOT at candidate_ready. Before the
// fix recovery accepted the replacement as "the healthy workload" and removed the
// stopped original, the only workload known to hold a working secret.
func TestN21_UnverifiedStartedCandidate_MustNotRetireTheOriginal(t *testing.T) {
	for _, health := range []string{"", "healthy"} {
		for _, step := range []string{rotation.TrIntentStartReplacement, rotation.TrReplacementStarted} {
			name := step + "/health=" + health
			t.Run(name, func(t *testing.T) {
				d := newFakeDaemon(n21Original(false), n21Candidate(true, health), healthy("b1", "unrelated"))
				f := newRec(t, d, "-")
				n21Journal(t, f, step)
				f.run()

				for _, r := range d.removedSorted() {
					if strings.HasPrefix(r, "app_old_") {
						t.Fatalf("the original was retired on the strength of an UNVERIFIED candidate: removed=%v", d.removedSorted())
					}
				}
				n21AssertPreserved(t, d, f)
				if c := d.find("c1"); c == nil || !c.running {
					t.Fatal("the running candidate must be left exactly as it was (it may be the serving workload)")
				}
			})
		}
	}
}

// Every pre-ready step of the non-proxy restart flow, with the layout Docker is in at
// that step: recovery makes NO mutation (nothing removed, started, stopped or
// renamed), flags the rotation for manual recovery, and is idempotent. The original is
// never destroyed and an unverified candidate is never promoted to "the workload".
func TestN21_EveryPreReadyRestartStep_PreservesEverythingAndEscalates(t *testing.T) {
	cases := []struct {
		step        string
		origRunning bool
		candidate   *fakeCtr // nil = no candidate yet
		desc        string
	}{
		{rotation.TrIntentRenameOriginal, true, nil, "original renamed away, nothing created"},
		{rotation.TrIntentCreateCandidate, true, nil, "create intent, candidate absent"},
		{rotation.TrIntentCreateCandidate, true, n21Candidate(false, ""), "candidate created before its ack (found by rotation label)"},
		{rotation.TrCandidateCreated, true, n21Candidate(false, ""), "candidate created, secret delivery not attempted"},
		{rotation.TrIntentStopOriginal, true, n21Candidate(false, ""), "delivery done or partial or failed (unknown), stop intent only"},
		{rotation.TrIntentStopOriginal, false, n21Candidate(false, ""), "original stopped, ack lost"},
		{rotation.TrOriginalStopped, false, n21Candidate(false, ""), "original stopped, candidate not started"},
		{rotation.TrIntentStartReplacement, false, n21Candidate(false, ""), "start intent, candidate never started"},
		{rotation.TrIntentStartReplacement, false, n21Candidate(true, "starting"), "candidate started, health still starting"},
		{rotation.TrReplacementStarted, false, n21Candidate(true, "unhealthy"), "candidate unhealthy"},
		{rotation.TrReplacementStarted, false, n21Candidate(true, ""), "candidate running, no HEALTHCHECK, long stable"},
		{rotation.TrReplacementStarted, false, n21Candidate(true, "healthy"), "candidate healthy (health is not secret delivery)"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.step+"/"+c.desc, func(t *testing.T) {
			cs := []*fakeCtr{n21Original(c.origRunning), healthy("b1", "unrelated")}
			if c.candidate != nil {
				cs = append(cs, c.candidate)
			}
			d := newFakeDaemon(cs...)
			f := newRec(t, d, "-")
			n21Journal(t, f, c.step)
			before := d.names()
			f.run()
			n21AssertPreserved(t, d, f)
			eq(t, "names", d.names(), before)
			if f.transition() != c.step {
				t.Fatalf("recovery must not rewrite the journal's last step: %s", f.transition())
			}
			// Idempotent: nothing changes on repeat, orphan cleanup included.
			f.run()
			if err := f.ar.CleanupOrphanedContainers(context.Background()); err != nil {
				t.Fatal(err)
			}
			eq(t, "names after repeat", d.names(), before)
			n21AssertPreserved(t, d, f)
		})
	}
}

// n21AssertPreserved: no container was removed, started, or stopped, the original
// survives, the bystander is untouched, and the rotation is flagged for manual recovery.
func n21AssertPreserved(t *testing.T, d *fakeDaemon, f *recFixture) {
	t.Helper()
	if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
		t.Fatalf("recovery must not mutate: removed=%v started=%v stopped=%v", d.removed, d.started, d.stopped)
	}
	if d.find("o1") == nil {
		t.Fatal("the original was destroyed")
	}
	if f.status() != "critical_error" {
		t.Fatalf("an unverified candidate must leave the rotation for manual recovery, got %s", f.status())
	}
}

// A candidate that DID pass the readiness gate (candidate_ready, or any later
// journalled step) is still accepted and the original retired: the fix must not
// regress the successful-rotation recovery.
func TestN21_VerifiedCandidate_StillRetiresTheOriginal(t *testing.T) {
	for _, step := range []string{rotation.TrCandidateReady, rotation.TrIntentCleanup} {
		t.Run(step, func(t *testing.T) {
			d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
			f := newRec(t, d, "-")
			n21Journal(t, f, step)
			f.run()
			eq(t, "removed", d.removedSorted(), []string{"app_old_1700000000"})
			eq(t, "remaining", d.names(), []string{"app"})
			if f.status() != "recovered" {
				t.Fatalf("status %s", f.status())
			}
			if len(d.started) != 0 {
				t.Fatalf("no restart expected: %v", d.started)
			}
		})
	}
}

// Proxy-managed restart (original keeps running until the swap): an unverified
// candidate never displaces the original, which keeps serving; nothing is touched.
func TestN21_ProxiedRestart_UnverifiedCandidateNeverDisplacesTheServingOriginal(t *testing.T) {
	o := n21Original(true)
	o.labels = map[string]string{"dso.host_ports": "8080:80"}
	c := n21Candidate(true, "healthy")
	c.labels["dso.host_ports"] = "8080:80"
	d := newFakeDaemon(o, c)
	f := newRec(t, d, "-")
	n21Journal(t, f, rotation.TrIntentStartReplacement)
	f.run()
	n21AssertPreserved(t, d, f)
	if !d.find("o1").running || !d.find("c1").running {
		t.Fatal("both workloads must be left running")
	}
}

// Ambiguity is preserved, never resolved destructively.
func TestN21_AmbiguousStates_PreserveEverythingAndEscalate(t *testing.T) {
	t.Run("candidate owned by another agent", func(t *testing.T) {
		c := n21Candidate(true, "healthy")
		c.owner = "owner-B"
		d := newFakeDaemon(n21Original(false), c)
		f := newRec(t, d, "-")
		n21Journal(t, f, rotation.TrReplacementStarted)
		f.run()
		if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
			t.Fatalf("no mutation allowed: removed=%v started=%v stopped=%v", d.removed, d.started, d.stopped)
		}
		if f.status() != "critical_error" {
			t.Fatalf("status %s", f.status())
		}
	})
	t.Run("original missing", func(t *testing.T) {
		d := newFakeDaemon(n21Candidate(true, "healthy"))
		f := newRec(t, d, "-")
		n21Journal(t, f, rotation.TrReplacementStarted)
		f.run()
		if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
			t.Fatalf("the only remaining workload must not be touched: removed=%v stopped=%v", d.removed, d.stopped)
		}
		if f.status() != "critical_error" {
			t.Fatalf("status %s", f.status())
		}
	})
	t.Run("original owned by another agent", func(t *testing.T) {
		o := n21Original(false)
		o.owner = "owner-B"
		d := newFakeDaemon(o, n21Candidate(true, "healthy"))
		f := newRec(t, d, "-")
		n21Journal(t, f, rotation.TrReplacementStarted)
		f.run()
		if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 || f.status() != "critical_error" {
			t.Fatalf("removed=%v started=%v stopped=%v status=%s", d.removed, d.started, d.stopped, f.status())
		}
	})
	t.Run("a healthy real-name holder that is NOT the journalled candidate", func(t *testing.T) {
		stranger := healthy("x9", "app")
		d := newFakeDaemon(n21Original(false), stranger)
		f := newRec(t, d, "-")
		n21Journal(t, f, rotation.TrReplacementStarted)
		f.run()
		if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
			t.Fatalf("an unrelated holder must authorise nothing: removed=%v stopped=%v", d.removed, d.stopped)
		}
	})
	t.Run("unknown transition name (inconsistent journal) is treated as unverified", func(t *testing.T) {
		d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
		f := newRec(t, d, "-")
		n21Journal(t, f, rotation.TrCandidateCreated)
		if err := f.st.Record(context.Background(), rotation.JournalEvent{RotationID: n21RID, Transition: "some_future_transition"}); err != nil {
			t.Fatal(err)
		}
		f.run()
		for _, r := range d.removed {
			if strings.HasPrefix(r, "app_old_") {
				t.Fatalf("original retired on an unrecognised journal state: %v", d.removed)
			}
		}
		if d.find("o1") == nil {
			t.Fatal("original destroyed")
		}
	})
}

// A candidate that holds the real name but is not journalled as ready is never a
// "verified healthy holder" for orphan cleanup either (it does not go through
// RecoverFromCrash at all there).
func TestN21_OrphanCleanupDoesNotRetireTheOriginalForAnUnverifiedCandidate(t *testing.T) {
	d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
	f := newRec(t, d, "-")
	n21Journal(t, f, rotation.TrReplacementStarted)
	if err := f.ar.CleanupOrphanedContainers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(d.removed) != 0 {
		t.Fatalf("orphan cleanup removed %v on the strength of an unverified candidate", d.removed)
	}
}

// Recovery of a crash after the journal committed but before any later (ledger)
// reconciliation, and a crash with no journal record at all for the holder, keep
// their M0 behaviour.
func TestN21_CompletedRotationIsNotReopened(t *testing.T) {
	d := newFakeDaemon(n21Candidate(true, "healthy"))
	f := newRec(t, d, "-")
	n21Journal(t, f, rotation.TrIntentCleanup)
	if err := f.st.Record(context.Background(), rotation.JournalEvent{RotationID: n21RID, Transition: rotation.TrCompleted}); err != nil {
		t.Fatal(err)
	}
	f.run() // nothing pending
	if len(d.removed) != 0 || len(d.stopped) != 0 || len(d.started) != 0 {
		t.Fatalf("a completed rotation must not be touched: %+v", d)
	}
}
