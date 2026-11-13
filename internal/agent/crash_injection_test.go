package agent

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Deterministic crash injection (M0-04). The REAL RollingStrategy.Execute runs
// against a stateful fake daemon with the REAL StateTracker as the durable journal.
// A test-only FaultHook panics at a named transition point to model the process dying
// at exactly that point: no deferred cleanup runs and no later step executes. The
// "restarted" agent then loads the journal FROM DISK (a new StateTracker over the
// same directory) and runs the real recovery. This proves the journal + recovery
// logic against a fake daemon; it is NOT a real process kill against Docker (that
// scenario is prepared for the disposable VM and has not been run).

type crashSignal struct{ point string }

func newCrashClient(t *testing.T, d *fakeDaemon) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"),
		client.WithHTTPClient(&http.Client{Transport: &recoveryMockTransport{reqFunc: func(req *http.Request) (*http.Response, error) {
			rec := &captureWriter{h: http.Header{}}
			d.handler().ServeHTTP(rec, req)
			return rec.response(), nil
		}}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

// runCrashing executes one rolling rotation and returns the point it "crashed" at
// ("" when it ran to completion).
func runCrashing(t *testing.T, cli *client.Client, st *StateTracker, crashAt string, proxied bool) (crashed string, execErr error) {
	t.Helper()
	rs := rotation.NewRollingStrategyWithLogger(cli, zap.NewNop())
	rs.Journal = st
	rs.OwnerID = me
	rs.FaultHook = func(point string) {
		if point == crashAt {
			panic(crashSignal{point})
		}
	}
	if proxied {
		rs.OnReady = func(ctx context.Context, newID string) error { return nil } // proxy handoff stand-in
	}
	defer func() {
		if r := recover(); r != nil {
			if cs, ok := r.(crashSignal); ok {
				crashed = cs.point
				return
			}
			panic(r)
		}
	}()
	execErr = rs.Execute(rotation.WithRotationID(context.Background(), "rot-test"), "o1", nil, nil, nil, 2*time.Second)
	return "", execErr
}

// crashRouting models the live proxy table at each crash point: before the handoff
// the proxy routes to the original; from the handoff on it routes to the candidate.
type crashRouting struct {
	d     *fakeDaemon
	point string
}

func (c crashRouting) ActiveBackends(_ context.Context, _ string) ([]string, bool) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	var original, candidate string
	for _, x := range c.d.ctrs {
		if x.id == "o1" {
			original = x.id
		}
		if strings.HasPrefix(x.id, "created-") {
			candidate = x.id
		}
	}
	switch c.point {
	case "after_handoff_before_ack", "after_intent_swap", "between_renames", "after_swap_before_commit_ack", "after_swap_committed", "after_intent_cleanup", "after_cleanup_before_ack":
		if candidate != "" {
			return []string{candidate}, true
		}
	}
	if original != "" {
		return []string{original}, true
	}
	return nil, true
}

func owned(d *fakeDaemon) (running []string, all []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.ctrs {
		all = append(all, c.name)
		if c.running {
			running = append(running, c.name)
		}
	}
	return
}

func TestCrashInjection_EveryTransition_RecoveryPreservesAHealthyWorkloadAndIsIdempotent(t *testing.T) {
	type expectation struct {
		removed  []string // containers recovery may remove (exact)
		critical bool     // rotation must be left for manual recovery
		name     string
	}
	// For every crash point: the resulting Docker layout, what recovery must do.
	points := []struct {
		point  string
		expect expectation
	}{
		{"after_intent_create_candidate", expectation{name: "nothing created yet", removed: nil}},
		{"after_candidate_create_before_ack", expectation{name: "candidate exists, ID unjournalled (found by label)", removed: []string{"app_dso_new_*"}}},
		{"after_secret_delivery", expectation{name: "candidate created, not started", removed: []string{"app_dso_new_*"}}},
		{"after_start_before_verify", expectation{name: "candidate started, unverified", removed: []string{"app_dso_new_*"}}},
		{"after_candidate_verified", expectation{name: "candidate healthy, no handoff", removed: []string{"app_dso_new_*"}}},
		{"after_intent_handoff", expectation{name: "handoff intent without ack", removed: []string{"app_dso_new_*"}}},
		{"after_handoff_before_ack", expectation{name: "handoff done, unacknowledged", removed: []string{"app_dso_new_*"}}},
		{"after_intent_swap", expectation{name: "swap intent, no rename yet", removed: []string{"app_dso_new_*"}}},
		{"between_renames", expectation{name: "original renamed away, candidate not yet promoted: no holder", critical: true}},
		{"after_swap_before_commit_ack", expectation{name: "swap done, commit unacknowledged", removed: []string{"app_dso_backup_*"}}},
		{"after_swap_committed", expectation{name: "swap committed", removed: []string{"app_dso_backup_*"}}},
		{"after_intent_cleanup", expectation{name: "cleanup intent, backup still present", removed: []string{"app_dso_backup_*"}}},
		{"after_cleanup_before_ack", expectation{name: "backup already removed, completion unacknowledged", removed: nil}},
	}
	for _, mode := range []string{"unproxied", "proxied-routing-unknown", "proxied-routing-verified"} {
		proxied := mode != "unproxied"
		for _, pt := range points {
			pt := pt
			name := pt.point + "/" + mode
			// Proxied rotations need LIVE routing evidence before anything that might
			// be serving is retired. Without it (unknown) every workload is preserved
			// once the handoff intent exists; with it, retirement follows the routing.
			handoffStarted := map[string]bool{"after_intent_handoff": true, "after_handoff_before_ack": true, "after_intent_swap": true,
				"between_renames": true, "after_swap_before_commit_ack": true, "after_swap_committed": true, "after_intent_cleanup": true, "after_cleanup_before_ack": true}
			if mode == "proxied-routing-unknown" && handoffStarted[pt.point] && pt.point != "between_renames" && pt.point != "after_cleanup_before_ack" {
				pt.expect = expectation{name: pt.expect.name + " (live routing unknown: preserve)", critical: true}
			}
			if mode == "proxied-routing-verified" {
				switch pt.point {
				case "after_handoff_before_ack", "after_intent_swap":
					// The proxy already points at the CANDIDATE while the original holds the name.
					pt.expect = expectation{name: pt.expect.name + " (proxy routes to the candidate: must not remove it)", critical: true}
				}
			}
			t.Run(name, func(t *testing.T) {
				// A bystander that must never be touched, plus the original workload.
				original := healthy("o1", "app")
				original.labels = map[string]string{}
				if proxied {
					original.labels["dso.host_ports"] = "127.0.0.1:8080:80"
				}
				bystander := healthy("b1", "unrelated")
				foreign := &fakeCtr{id: "f1", name: "app_dso_backup_999", owner: "owner-B", running: true, health: "healthy", upFor: time.Hour}
				d := newFakeDaemon(original, bystander, foreign)
				cli := newCrashClient(t, d)

				stateDir := t.TempDir()
				logger := zaptest.NewLogger(t)
				st, err := NewStateTracker(stateDir, logger)
				if err != nil {
					t.Fatal(err)
				}
				crashed, execErr := runCrashing(t, cli, st, pt.point, proxied)
				if crashed != pt.point && pt.point != "after_cleanup_before_ack" && execErr != nil {
					// Some points only exist on the proxied path.
					if !proxied && strings.Contains(pt.point, "handoff") {
						t.Skip("point exists only on the proxied path")
					}
					t.Fatalf("did not crash at %s (crashed=%q err=%v)", pt.point, crashed, execErr)
				}
				if !proxied && (pt.point == "after_intent_handoff" || pt.point == "after_handoff_before_ack") {
					t.Skip("point exists only on the proxied path")
				}

				// "Restart": a NEW tracker loads the journal from disk, then real recovery runs.
				st2, err := NewStateTracker(stateDir, logger)
				if err != nil {
					t.Fatal(err)
				}
				if c, _ := st2.Corrupt(); c {
					t.Fatal("journal written by the crashed process must be readable")
				}
				removedByCrashedRun := len(d.removed) // removals the crashed rotation itself made
				ar := NewAutomaticRecovery(cli, logger, st2)
				ar.OwnerID = me
				ar.Timings = &recoveryTimings{healthWait: 0, minStableRunning: 30 * time.Second}
				if mode == "proxied-routing-verified" {
					ar.Routing = crashRouting{d: d, point: pt.point}
				}
				if err := ar.RecoverFromCrash(context.Background()); err != nil {
					t.Fatal(err)
				}

				// 1. A healthy owned workload survives under the real service identity.
				runningNow, _ := owned(d)
				hasWorkload := false
				for _, n := range runningNow {
					if n == "app" || strings.HasPrefix(n, "app_dso_backup_") && pt.expect.critical || strings.HasPrefix(n, "app_dso_new_") && pt.expect.critical {
						hasWorkload = true
					}
				}
				if !hasWorkload {
					t.Fatalf("NO running workload for the service remains after recovery: running=%v", runningNow)
				}
				// 2. Untouched: the bystander and the foreign-owned look-alike.
				if d.find("unrelated") == nil || d.find("app_dso_backup_999") == nil {
					t.Fatalf("an unmanaged or foreign container was touched: %v", d.removedSorted())
				}
				// 3. Exactly the permitted removals happened.
				var got []string
				recoveryRemoved := append([]string(nil), d.removed[removedByCrashedRun:]...)
				sort.Strings(recoveryRemoved)
				for _, r := range recoveryRemoved {
					if strings.HasPrefix(r, "app_dso_new_") {
						r = "app_dso_new_*"
					}
					if strings.HasPrefix(r, "app_dso_backup_") {
						r = "app_dso_backup_*"
					}
					got = append(got, r)
				}
				eq(t, "removed ("+pt.expect.name+")", got, pt.expect.removed)
				// 4. Rotation state.
				recs := st2.snapshotRecords()
				if len(recs) != 1 {
					t.Fatalf("expected one journal record, got %d", len(recs))
				}
				status := recs[0].Status
				if pt.expect.critical && status != "critical_error" {
					t.Fatalf("an unresolvable layout must be flagged for manual recovery, got %s", status)
				}
				if !pt.expect.critical && status != "recovered" && status != "completed" {
					t.Fatalf("a resolvable layout must end recovered/completed, got %s (%s)", status, recs[0].ErrorMessage)
				}
				// 5. Repeated recovery never changes anything further.
				before := d.names()
				for i := 0; i < 2; i++ {
					if err := ar.RecoverFromCrash(context.Background()); err != nil {
						t.Fatal(err)
					}
					if err := ar.CleanupOrphanedContainers(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				eq(t, "names after repeated recovery", d.names(), before)
			})
		}
	}
}

// Journal-specific rules: Docker alone says "a healthy container holds the real
// name"; the journal says whether it is the journalled candidate and whether the
// handoff was committed.
func TestRecoveryJournal_ProxiedRotationNeedsHandoffEvidenceBeforeRetiringTheOriginal(t *testing.T) {
	ctx := context.Background()
	setup := func(transition string, observed string) (*fakeDaemon, *recFixture) {
		old := artifact("o1", "app_dso_backup_1")
		old.labels = map[string]string{"dso.host_ports": "8080:80"}
		cand := healthy("n1", "app")
		cand.labels = map[string]string{rotation.LabelRotationID: "rot-x", "dso.host_ports": "8080:80"}
		d := newFakeDaemon(old, cand)
		f := newRec(t, d, "-")
		if err := f.st.Record(ctx, rotation.JournalEvent{RotationID: "rot-x", ServiceID: "app", Strategy: "rolling",
			Transition: rotation.TrIntentCreateCandidate, OriginalIDs: []string{"o1"}}); err != nil {
			t.Fatal(err)
		}
		if err := f.st.Record(ctx, rotation.JournalEvent{RotationID: "rot-x", Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"n1"}}); err != nil {
			t.Fatal(err)
		}
		if err := f.st.Record(ctx, rotation.JournalEvent{RotationID: "rot-x", Transition: rotation.TrIntentHandoff, ExpectedProxyTarget: "n1"}); err != nil {
			t.Fatal(err)
		}
		ev := rotation.JournalEvent{RotationID: "rot-x", Transition: transition, ObservedProxyTarget: observed}
		if err := f.st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
		return d, f
	}

	t.Run("handoff intent only, no evidence the proxy moved: the old workload is PRESERVED", func(t *testing.T) {
		d, f := setup(rotation.TrIntentHandoff, "")
		f.run()
		if len(d.removed) != 0 {
			t.Fatalf("must preserve both workloads, removed %v", d.removed)
		}
		if f.status() != "critical_error" && f.status() != "in_progress" && f.status() != "recovered" {
			t.Logf("status %s", f.status())
		}
	})
	t.Run("handoff observed on the holder: the old workload may be retired", func(t *testing.T) {
		d, f := setup(rotation.TrHandoffDone, "n1")
		f.ar.Routing = fixedRouting{ids: []string{"n1"}, known: true}
		f.run()
		eq(t, "removed", d.removedSorted(), []string{"app_dso_backup_1"})
	})
	t.Run("handoff observed on a DIFFERENT container: preserved", func(t *testing.T) {
		d, f := setup(rotation.TrHandoffDone, "someone-else")
		f.run()
		if len(d.removed) != 0 {
			t.Fatalf("removed %v", d.removed)
		}
	})
	t.Run("swap intent (only written after the handoff returned) permits retirement", func(t *testing.T) {
		d, f := setup(rotation.TrIntentSwap, "")
		f.ar.Routing = fixedRouting{ids: []string{"n1"}, known: true}
		f.run()
		eq(t, "removed", d.removedSorted(), []string{"app_dso_backup_1"})
	})
}

func TestRecoveryJournal_UnjournalledHolderIsNotTheCandidate(t *testing.T) {
	ctx := context.Background()
	old := artifact("o1", "app_dso_backup_1")
	someoneElse := healthy("x9", "app") // healthy, owned, holds the name, but is NOT the journalled candidate
	d := newFakeDaemon(old, someoneElse)
	f := newRec(t, d, "-")
	_ = f.st.Record(ctx, rotation.JournalEvent{RotationID: "rot-y", ServiceID: "app", Transition: rotation.TrIntentCreateCandidate, OriginalIDs: []string{"o1"}})
	_ = f.st.Record(ctx, rotation.JournalEvent{RotationID: "rot-y", Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"n1"}})
	f.run()
	if len(d.removed) != 0 {
		t.Fatalf("an unexpected container identity must not authorise removing the original: %v", d.removed)
	}
}

func TestRecoveryJournal_ProxyManagedArtifactWithNoJournalIsPreserved(t *testing.T) {
	old := artifact("o1", "app_dso_backup_1")
	old.labels = map[string]string{"dso.host_ports": "8080:80"}
	d := newFakeDaemon(healthy("n1", "app"), old)
	f := newRec(t, d, "o1")
	f.run()
	if len(d.removed) != 0 {
		t.Fatalf("no routing evidence for a proxy-managed workload: must preserve, removed %v", d.removed)
	}
}

// The journal fails closed: if an INTENT cannot be recorded the guarded action must
// not happen.
type failingJournal struct{ failOn string }

func (f failingJournal) Record(_ context.Context, e rotation.JournalEvent) error {
	if e.Transition == f.failOn {
		return context.DeadlineExceeded
	}
	return nil
}

func TestJournal_FailedIntentBlocksTheGuardedAction(t *testing.T) {
	for _, tr := range []string{rotation.TrIntentCreateCandidate, rotation.TrIntentHandoff, rotation.TrIntentSwap} {
		t.Run(tr, func(t *testing.T) {
			d := newFakeDaemon(healthy("o1", "app"))
			cli := newCrashClient(t, d)
			rs := rotation.NewRollingStrategyWithLogger(cli, zap.NewNop())
			rs.Journal = failingJournal{failOn: tr}
			rs.OnReady = func(ctx context.Context, id string) error { return nil }
			err := rs.Execute(context.Background(), "o1", nil, nil, nil, 2*time.Second)
			if err == nil || !strings.Contains(err.Error(), "journal") {
				t.Fatalf("want a journal refusal, got %v", err)
			}
			// The original must be untouched, and no candidate may survive.
			if d.find("app") == nil || d.find("app").id != "o1" || !d.find("app").running {
				t.Fatal("the original workload must be untouched")
			}
			for _, n := range d.names() {
				if strings.Contains(n, "_dso_backup_") {
					t.Fatalf("original was renamed despite a failed swap intent: %v", d.names())
				}
			}
			_, all := owned(d)
			if tr == rotation.TrIntentCreateCandidate && len(all) != 1 {
				t.Fatalf("no candidate may be created without a durable intent: %v", all)
			}
		})
	}
}

func TestJournal_NoPlaintextSecretIsEverPersisted(t *testing.T) {
	d := newFakeDaemon(healthy("o1", "app"))
	cli := newCrashClient(t, d)
	dir := t.TempDir()
	st, _ := NewStateTracker(dir, zaptest.NewLogger(t))
	rs := rotation.NewRollingStrategyWithLogger(cli, zap.NewNop())
	rs.Journal = st
	rs.OwnerID = me
	const secret = "SYNTHETIC-JOURNAL-LEAK-CANARY-3309"
	_ = rs.Execute(rotation.WithRotationID(context.Background(), "rot-z"), "o1", map[string]string{"DB_PASSWORD": secret}, nil, nil, 2*time.Second)
	b, err := os.ReadFile(dir + "/rotations.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatal("a secret value reached the journal")
	}
}

type fixedRouting struct {
	ids   []string
	known bool
}

func (f fixedRouting) ActiveBackends(context.Context, string) ([]string, bool) { return f.ids, f.known }

// The durable handoff acknowledgement is necessary but NOT sufficient: the proxy may
// have restarted or been reconfigured since. Live routing decides.
func TestRecoveryRouting_LiveStateOverridesDurableHandoffEvidence(t *testing.T) {
	ctx := context.Background()
	build := func(r RoutingVerifier) (*fakeDaemon, *recFixture) {
		old := artifact("o1", "app_dso_backup_1")
		old.labels = map[string]string{"dso.host_ports": "8080:80"}
		cand := healthy("n1", "app")
		cand.labels = map[string]string{rotation.LabelRotationID: "rot-r", "dso.host_ports": "8080:80"}
		d := newFakeDaemon(old, cand)
		f := newRec(t, d, "-")
		for _, e := range []rotation.JournalEvent{
			{RotationID: "rot-r", ServiceID: "app", Transition: rotation.TrIntentCreateCandidate, OriginalIDs: []string{"o1"}},
			{RotationID: "rot-r", Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"n1"}},
			{RotationID: "rot-r", Transition: rotation.TrIntentHandoff, ExpectedProxyTarget: "n1"},
			{RotationID: "rot-r", Transition: rotation.TrHandoffDone, ObservedProxyTarget: "n1"},
			{RotationID: "rot-r", Transition: rotation.TrSwapCommitted},
		} {
			if err := f.st.Record(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		f.ar.Routing = r
		return d, f
	}
	cases := []struct {
		name   string
		r      RoutingVerifier
		remove bool
	}{
		{"journal says committed but routing cannot be inspected", fixedRouting{known: false}, false},
		{"no verifier at all", nil, false},
		{"proxy restarted and lost its table (empty, known)", fixedRouting{ids: nil, known: true}, false},
		{"proxy still points at the ORIGINAL", fixedRouting{ids: []string{"o1"}, known: true}, false},
		{"proxy points at an UNEXPECTED workload", fixedRouting{ids: []string{"stranger"}, known: true}, false},
		{"proxy points at both candidate and original", fixedRouting{ids: []string{"n1", "o1"}, known: true}, false},
		{"proxy points only at the candidate", fixedRouting{ids: []string{"n1"}, known: true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, f := build(c.r)
			f.run()
			if c.remove {
				eq(t, "removed", d.removedSorted(), []string{"app_dso_backup_1"})
				if f.status() != "recovered" {
					t.Fatalf("status %s", f.status())
				}
				return
			}
			if len(d.removed) != 0 {
				t.Fatalf("live routing does not clearly favour the holder: nothing may be removed, got %v", d.removed)
			}
			if f.status() != "critical_error" {
				t.Fatalf("preserved workloads must leave the rotation flagged for manual recovery, got %s", f.status())
			}
		})
	}
}
