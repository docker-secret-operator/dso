package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// M0-04 findings 3.1 / 3.2 / 3.3: the real startup sequence of `dso agent`
// (BeginStartup -> AttachProxy gated scan -> StartAll recovery -> settle), run
// against a stateful in-memory Docker daemon and a REAL embedded proxy.Manager.
// Docker-free: no daemon is touched.

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func fakeCli(t *testing.T, d *fakeDaemon) *client.Client {
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

// proxied returns a running, healthy, owned container that declares a host port.
func proxied(t *testing.T, id, name string, extra map[string]string) *fakeCtr {
	c := healthy(id, name)
	c.status = "Up 1 hour (healthy)"
	c.labels = map[string]string{"dso.host_ports": fmt.Sprintf("%d:80", freeTCPPort(t))}
	for k, v := range extra {
		c.labels[k] = v
	}
	return c
}

type startup struct {
	t   *testing.T
	d   *fakeDaemon
	dir string
	te  *TriggerEngine
	pm  *proxy.Manager
}

// journalOf records events into a fresh state directory (the journal a previous,
// now dead, agent process left behind).
func journalOf(t *testing.T, evs ...rotation.JournalEvent) string {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if err := st.Record(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// boot is a fresh agent process: engine over the persisted journal, embedded proxy,
// startup gating armed. It does NOT run StartAll; callers step through the sequence.
func boot(t *testing.T, d *fakeDaemon, stateDir string) *startup {
	t.Helper()
	logger := zaptest.NewLogger(t)
	cfg := &config.Config{}
	cfg.Agent.OwnerID = me
	te, err := NewTriggerEngineWithDirs(nil, nil, nil, logger, cfg, fakeCli(t, d), stateDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(te.Stop)
	pm := proxy.NewManager(logger)
	pm.SetOwnerID(me)
	t.Cleanup(func() { _ = pm.Stop(100 * time.Millisecond) })
	te.BeginStartup()
	return &startup{t: t, d: d, dir: stateDir, te: te, pm: pm}
}

func (s *startup) active() []string {
	ids, _ := s.pm.RoutingSnapshot()
	sort.Strings(ids)
	return ids
}

func (s *startup) record() *RotationState {
	s.t.Helper()
	recs := s.te.StateTracker.snapshotRecords()
	if len(recs) != 1 {
		s.t.Fatalf("want exactly one journal record, got %d", len(recs))
	}
	return recs[0]
}

func evs(rid, strategy string, trs ...string) []rotation.JournalEvent {
	var out []rotation.JournalEvent
	for _, tr := range trs {
		out = append(out, rotation.JournalEvent{RotationID: rid, Strategy: strategy, ServiceID: "svc", OwnerID: me, Transition: tr,
			OriginalIDs: []string{"orig"}, CandidateIDs: []string{"cand"}})
	}
	return out
}

func withProxyTarget(es []rotation.JournalEvent, expected, observed string) []rotation.JournalEvent {
	for i := range es {
		switch es[i].Transition {
		case rotation.TrIntentHandoff:
			es[i].ExpectedProxyTarget = expected
		case rotation.TrHandoffDone, rotation.TrIntentSwap, rotation.TrSwapCommitted, rotation.TrIntentCleanup:
			es[i].ExpectedProxyTarget, es[i].ObservedProxyTarget = expected, observed
		}
	}
	return es
}

// --- 3.1 + 3.2: crash after the proxy handoff, before the swap --------------------

// The previous process had pointed the proxy at the candidate and died before the
// rename swap. The candidate is unverified from this process's point of view.
//
// Before the fix: the scan registered BOTH containers (the candidate received
// traffic) and recovery, with no RoutingVerifier, preserved both + manual recovery.
// Now: the candidate is held, only the original is routed, the verifier positively
// shows the original routed and the candidate not, so recovery retires the candidate
// and the rotation is recovered. Nothing in this test relies on the persisted
// handoff acknowledgement.
func TestStartup_HandoffThenCrash_CandidateNeverRoutedAndRecoveredAutomatically(t *testing.T) {
	orig := proxied(t, "orig", "svc", nil)
	cand := proxied(t, "cand", "svc_dso_new_100", map[string]string{rotation.LabelRotationID: "rot-1"})
	d := newFakeDaemon(orig, cand)
	es := withProxyTarget(evs("rot-1", "rolling",
		rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrCandidateReady, rotation.TrIntentHandoff, rotation.TrHandoffDone), "cand", "cand")
	s := boot(t, d, journalOf(t, es...))

	// 3.3: nothing may be dispatched while startup recovery has not settled.
	if _, blocked := s.te.guard.RotationBlocked("svc", "orig"); !blocked {
		t.Fatal("rotation must be blocked until startup recovery has settled")
	}

	s.te.AttachProxy(context.Background(), s.pm)
	eq(t, "routed after the gated scan (candidate held)", s.active(), []string{"orig"})
	if !s.pm.IsHeld("cand") {
		t.Fatal("the unverified candidate must be HELD, not routed")
	}

	_ = s.te.StartAll()

	if got := s.d.removedSorted(); strings.Join(got, ",") != "svc_dso_new_100" {
		t.Fatalf("removed %v, want only the candidate", got)
	}
	if st := s.record().Status; st != "recovered" {
		t.Fatalf("status %s, want recovered", st)
	}
	eq(t, "routed after recovery", s.active(), []string{"orig"})
	if _, blocked := s.te.guard.RotationBlocked("svc", "orig"); blocked {
		t.Fatal("a recovered service must be released for rotation")
	}
}

// Crash AFTER the swap committed: the promoted candidate holds the real name and is
// healthy, the original is a `_dso_backup_` artifact. The promoted candidate is
// eligible (journal committed + real name + not unhealthy), the backup is held, so the
// verifier shows candidate routed / backup not, and recovery retires the backup.
func TestStartup_SwapCommittedThenCrash_PromotedRoutedBackupRetired(t *testing.T) {
	promoted := proxied(t, "cand", "svc", map[string]string{rotation.LabelRotationID: "rot-1"})
	backup := proxied(t, "orig", "svc_dso_backup_100", nil)
	d := newFakeDaemon(promoted, backup)
	es := withProxyTarget(evs("rot-1", "rolling",
		rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrCandidateReady, rotation.TrIntentHandoff, rotation.TrHandoffDone,
		rotation.TrIntentSwap, rotation.TrSwapCommitted), "cand", "cand")
	s := boot(t, d, journalOf(t, es...))
	s.te.AttachProxy(context.Background(), s.pm)
	eq(t, "routed after the gated scan", s.active(), []string{"cand"})
	if !s.pm.IsHeld("orig") {
		t.Fatal("the backup artifact must be held")
	}
	_ = s.te.StartAll()
	if got := s.d.removedSorted(); strings.Join(got, ",") != "svc_dso_backup_100" {
		t.Fatalf("removed %v, want only the backup", got)
	}
	if st := s.record().Status; st != "recovered" {
		t.Fatalf("status %s, want recovered", st)
	}
	eq(t, "routed after recovery", s.active(), []string{"cand"})
}

// Crash in the middle of the swap (intent_swap, no commit ack): which container holds
// the real name is ambiguous, routing cannot be established, so EVERYTHING is
// preserved, nothing is routed, the rotation is manual-recovery, the service stays
// blocked and the held containers stay held after recovery (no deadlock either: the
// whole sequence returned).
func TestStartup_SwapIntentOnlyThenCrash_PreservesEverythingManualRecovery(t *testing.T) {
	cand := proxied(t, "cand", "svc", map[string]string{rotation.LabelRotationID: "rot-1"})
	backup := proxied(t, "orig", "svc_dso_backup_100", nil)
	d := newFakeDaemon(cand, backup)
	es := withProxyTarget(evs("rot-1", "rolling",
		rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrCandidateReady, rotation.TrIntentHandoff, rotation.TrHandoffDone,
		rotation.TrIntentSwap), "cand", "cand")
	s := boot(t, d, journalOf(t, es...))
	s.te.AttachProxy(context.Background(), s.pm)
	eq(t, "nothing is routed: neither container is verified", s.active(), nil)
	_ = s.te.StartAll()
	if len(s.d.removedSorted())+len(s.d.stopped)+len(s.d.started) != 0 {
		t.Fatalf("recovery touched Docker: removed=%v stopped=%v started=%v", s.d.removed, s.d.stopped, s.d.started)
	}
	if st := s.record().Status; st != "critical_error" {
		t.Fatalf("status %s, want critical_error (manual recovery)", st)
	}
	eq(t, "still nothing routed after recovery", s.active(), nil)
	if !s.pm.IsHeld("cand") || !s.pm.IsHeld("orig") {
		t.Fatalf("both must stay held while the operation is unresolved: %v", s.pm.HeldSnapshot())
	}
	if why, blocked := s.te.guard.RotationBlocked("svc"); !blocked {
		t.Fatal("a service whose recovery needs an operator must stay BLOCKED")
	} else if !strings.Contains(why, "unresolved operation rot-1") {
		t.Fatalf("block reason: %q", why)
	}
}

// Crash before the handoff intent: the candidate never carried traffic. It is held at
// scan (rotation label + artifact name), recovery retires it under the name-holder
// rule alone, and the original keeps serving throughout.
func TestStartup_CrashBeforeHandoff_UnreadyCandidateNeverRouted(t *testing.T) {
	for _, tr := range []string{rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrCandidateReady} {
		t.Run(tr, func(t *testing.T) {
			orig := proxied(t, "orig", "svc", nil)
			cand := proxied(t, "cand", "svc_dso_new_100", map[string]string{rotation.LabelRotationID: "rot-1"})
			cand.status = "Up 2 seconds (health: starting)"
			d := newFakeDaemon(orig, cand)
			var seq []string
			for _, x := range []string{rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrCandidateReady} {
				seq = append(seq, x)
				if x == tr {
					break
				}
			}
			s := boot(t, d, journalOf(t, evs("rot-1", "rolling", seq...)...))
			s.te.AttachProxy(context.Background(), s.pm)
			eq(t, "routed before recovery", s.active(), []string{"orig"})
			_ = s.te.StartAll()
			if got := s.d.removedSorted(); strings.Join(got, ",") != "svc_dso_new_100" {
				t.Fatalf("removed %v", got)
			}
			if s.record().Status != "recovered" {
				t.Fatalf("status %s", s.record().Status)
			}
			eq(t, "routed after recovery", s.active(), []string{"orig"})
		})
	}
}

// A candidate whose crash preceded its ID being journalled is identified by the
// dso.rotation.id label alone (partially injected / not yet journalled).
func TestStartup_CandidateKnownOnlyByRotationLabelIsHeld(t *testing.T) {
	orig := proxied(t, "orig", "svc", nil)
	ghost := proxied(t, "ghost", "svc", map[string]string{rotation.LabelRotationID: "rot-1"}) // real name: e.g. renamed, unregistered in journal
	ghost.name = "svc-ghost"
	d := newFakeDaemon(orig, ghost)
	es := evs("rot-1", "rolling", rotation.TrIntentCreateCandidate)
	es[0].CandidateIDs = nil
	s := boot(t, d, journalOf(t, es...))
	s.te.AttachProxy(context.Background(), s.pm)
	eq(t, "routed", s.active(), []string{"orig"})
	if !s.pm.IsHeld("ghost") {
		t.Fatal("a container carrying the rotation label of an unsettled operation must be held")
	}
}

// --- 3.2: candidate kinds, journal-aware gate in isolation ------------------------

func TestProxyEligibility_CandidateKinds(t *testing.T) {
	type kase struct {
		name    string
		events  []rotation.JournalEvent
		corrupt bool
		cand    proxy.ScanCandidate
		want    bool
	}
	lab := map[string]string{rotation.LabelRotationID: "rot-1"}
	ok := func(trs ...string) []rotation.JournalEvent { return evs("rot-1", "rolling", trs...) }
	committed := withProxyTarget(ok(rotation.TrIntentCreateCandidate, rotation.TrCandidateReady, rotation.TrIntentHandoff, rotation.TrHandoffDone, rotation.TrIntentSwap, rotation.TrSwapCommitted), "cand", "cand")
	cases := []kase{
		{"plain service container", nil, false, proxy.ScanCandidate{ID: "x", Names: []string{"/db"}}, true},
		{"newly created candidate (artifact name)", ok(rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated), false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc_dso_new_1"}, Labels: lab}, false},
		{"partially injected candidate (label, unsettled)", ok(rotation.TrIntentCreateCandidate), false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc-x"}, Labels: lab}, false},
		{"not ready candidate under real name (unsettled)", ok(rotation.TrCandidateCreated), false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab, Status: "Up 1 second (health: starting)"}, false},
		{"awaiting handoff (candidate_ready)", ok(rotation.TrCandidateReady), false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc_dso_new_1"}, Labels: lab}, false},
		{"failed candidate left behind (rolled_back, artifact name)", ok(rotation.TrIntentCreateCandidate, rotation.TrRolledBack), false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc_dso_new_1"}, Labels: lab}, false},
		{"leftover old original of an interrupted restart", nil, false, proxy.ScanCandidate{ID: "o", Names: []string{"/svc_old_1700000000"}}, false},
		{"leftover backup", nil, false, proxy.ScanCandidate{ID: "o", Names: []string{"/svc_dso_backup_1"}}, false},
		{"promoted candidate, swap committed, healthy", committed, false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab, Status: "Up 1 hour (healthy)"}, true},
		{"promoted candidate, swap committed, no healthcheck", committed, false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab, Status: "Up 1 hour"}, true},
		{"promoted candidate, swap committed, UNHEALTHY", committed, false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab, Status: "Up 1 hour (unhealthy)"}, false},
		{"promoted candidate, swap committed, still starting", committed, false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab, Status: "Up 1 second (health: starting)"}, false},
		{"committed but NOT holding the real name", committed, false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc-other"}, Labels: lab, Status: "Up 1 hour (healthy)"}, false},
		{"settled promoted workload (completed)", ok(rotation.TrIntentCreateCandidate, rotation.TrCompleted), false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab}, true},
		{"settled promoted workload (record aged out of the journal)", nil, false, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab}, true},
		{"labelled candidate with an UNREADABLE journal", nil, true, proxy.ScanCandidate{ID: "cand", Names: []string{"/svc"}, Labels: lab}, false},
		{"unlabelled service with an UNREADABLE journal", nil, true, proxy.ScanCandidate{ID: "x", Names: []string{"/db"}}, true},
		{"artifact with an UNREADABLE journal", nil, true, proxy.ScanCandidate{ID: "o", Names: []string{"/svc_dso_backup_1"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := journalOf(t, c.events...)
			if c.corrupt {
				dir = t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "rotations.json"), []byte("{ not json"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			st, _ := NewStateTracker(dir, zap.NewNop())
			if got, why := st.ProxyEligibility()(c.cand); got != c.want {
				t.Fatalf("eligible=%v (%s), want %v", got, why, c.want)
			}
		})
	}
	// No journal at all (state tracker could not be created): a labelled candidate is
	// held, an unlabelled service is not.
	var nilSt *StateTracker
	if ok, _ := nilSt.ProxyEligibility()(proxy.ScanCandidate{ID: "c", Names: []string{"/svc"}, Labels: lab}); ok {
		t.Fatal("labelled candidate must be held when no journal is available")
	}
	if ok, _ := nilSt.ProxyEligibility()(proxy.ScanCandidate{ID: "c", Names: []string{"/db"}}); !ok {
		t.Fatal("an unlabelled service must stay routable when no journal is available")
	}
}

// Foreign-owned containers are never routed and never even held (the proxy Manager
// refuses them before the gate), whatever their candidate state.
func TestStartup_ForeignOwnedContainerNeverRegisteredOrHeld(t *testing.T) {
	foreign := proxied(t, "foreign", "svc_dso_new_9", map[string]string{rotation.LabelRotationID: "rot-9"})
	foreign.owner = "owner-B"
	plainForeign := proxied(t, "foreign2", "other", nil)
	plainForeign.owner = "owner-B"
	d := newFakeDaemon(foreign, plainForeign)
	s := boot(t, d, t.TempDir())
	s.te.AttachProxy(context.Background(), s.pm)
	eq(t, "routed", s.active(), nil)
	if s.pm.IsHeld("foreign") || s.pm.IsHeld("foreign2") {
		t.Fatal("foreign-owned containers are not this agent's to hold")
	}
}

// --- 3.2: corrupt journal ---------------------------------------------------------

func TestStartup_CorruptJournal_NoCandidateRoutedNothingTouchedAllRotationsBlocked(t *testing.T) {
	plain := proxied(t, "plain", "db", nil)
	cand := proxied(t, "cand", "svc", map[string]string{rotation.LabelRotationID: "rot-1"})
	backup := proxied(t, "orig", "svc_dso_backup_1", nil)
	d := newFakeDaemon(plain, cand, backup)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rotations.json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := boot(t, d, dir)
	s.te.AttachProxy(context.Background(), s.pm)
	eq(t, "only the unambiguous container is routed", s.active(), []string{"plain"})
	_ = s.te.StartAll()
	if len(s.d.removedSorted())+len(s.d.stopped)+len(s.d.started) != 0 {
		t.Fatalf("corrupt journal: Docker was touched (removed=%v)", s.d.removed)
	}
	eq(t, "routing unchanged after recovery", s.active(), []string{"plain"})
	for _, svc := range []string{"db", "svc", "anything"} {
		if _, blocked := s.te.guard.RotationBlocked(svc); !blocked {
			t.Fatalf("rotation of %q must be blocked while the journal is unreadable", svc)
		}
	}
}

// --- 3.1: the verifier never answers on a persisted ack alone ---------------------

type snapSource struct {
	ids         []string
	established bool
}

func (s snapSource) RoutingSnapshot() ([]string, bool) { return s.ids, s.established }

func TestProxyRoutingVerifier_UnknownUnlessPositivelyEstablished(t *testing.T) {
	if ids, known := (*ProxyRoutingVerifier)(nil).ActiveBackends(context.Background(), "svc"); known || ids != nil {
		t.Fatal("nil verifier must answer unknown")
	}
	if _, known := NewProxyRoutingVerifier(nil).ActiveBackends(context.Background(), "svc"); known {
		t.Fatal("no source must answer unknown")
	}
	if _, known := NewProxyRoutingVerifier(snapSource{ids: []string{"a"}, established: false}).ActiveBackends(context.Background(), "svc"); known {
		t.Fatal("an unreconstructed table must answer unknown even when it lists backends")
	}
	ids, known := NewProxyRoutingVerifier(snapSource{ids: []string{"a"}, established: true}).ActiveBackends(context.Background(), "svc")
	if !known || len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("established table: %v %v", ids, known)
	}
}

// With the journal showing the handoff done AND a healthy name holder, an
// un-established routing table still preserves both workloads and flags manual
// recovery: the persisted ack alone is never enough.
func TestRecovery_HandoffAckAloneIsNeverEnough_RoutingUnknownPreserves(t *testing.T) {
	orig := proxied(t, "orig", "svc", nil)
	cand := proxied(t, "cand", "svc_dso_new_100", map[string]string{rotation.LabelRotationID: "rot-1"})
	d := newFakeDaemon(orig, cand)
	es := withProxyTarget(evs("rot-1", "rolling", rotation.TrIntentCreateCandidate, rotation.TrCandidateReady, rotation.TrIntentHandoff, rotation.TrHandoffDone), "cand", "cand")
	logger := zaptest.NewLogger(t)
	st, _ := NewStateTracker(journalOf(t, es...), logger)
	ar := NewAutomaticRecovery(fakeCli(t, d), logger, st)
	ar.OwnerID = me
	ar.Timings = &recoveryTimings{healthWait: 0, minStableRunning: 30 * time.Second}
	ar.Routing = NewProxyRoutingVerifier(snapSource{ids: []string{"orig"}, established: false})
	if err := ar.RecoverFromCrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(d.removedSorted()) != 0 {
		t.Fatalf("removed %v with routing unknown", d.removedSorted())
	}
	if st.snapshotRecords()[0].Status != "critical_error" {
		t.Fatalf("status %s, want critical_error", st.snapshotRecords()[0].Status)
	}
}

// Docker unreachable at scan time: reconstruction is not established, so the
// verifier answers unknown (and recovery would preserve everything).
func TestStartup_ScanListFailure_RoutingUnknown(t *testing.T) {
	d := newFakeDaemon(proxied(t, "orig", "svc", nil))
	d.listFails = true
	s := boot(t, d, t.TempDir())
	s.te.AttachProxy(context.Background(), s.pm)
	if _, known := s.te.Routing.ActiveBackends(context.Background(), "svc"); known {
		t.Fatal("a failed Docker listing must leave routing unknown")
	}
}

// Plain-proxy API (no journal): a rotation-labelled container is held, an ordinary
// one registered; established only after a successful listing.
func TestProxyManager_DefaultGateHoldsRotationLabelled(t *testing.T) {
	orig := proxied(t, "orig", "svc", nil)
	cand := proxied(t, "cand", "svc_x", map[string]string{rotation.LabelRotationID: "rot-1"})
	d := newFakeDaemon(orig, cand)
	pm := proxy.NewManager(zap.NewNop())
	t.Cleanup(func() { _ = pm.Stop(100 * time.Millisecond) })
	if _, est := pm.RoutingSnapshot(); est {
		t.Fatal("no scan yet: routing cannot be established")
	}
	pm.ScanAndRegister(context.Background(), fakeCli(t, d))
	ids, est := pm.RoutingSnapshot()
	if !est || len(ids) != 1 || ids[0] != "orig" || !pm.IsHeld("cand") {
		t.Fatalf("ids=%v est=%v held=%v", ids, est, pm.HeldSnapshot())
	}
}
