package watcher

// N19 managed file delivery through the restart (full recreation) strategy and the
// rolling fallback (executeSimpleRestart): stateful-lite HTTP daemon, real gate arming,
// real verification and journal code; the in-container operations are the delivery
// seam. No Docker daemon.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/rotation"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

const (
	n19RestartSentinel = "DSO_N19_RESTART_SENTINEL_77aa"
	n19RestartVersion  = "hmac:k1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

type restartRig struct {
	d    *n22Daemon
	gate *gateRec
	j    *memJ
	r    *ReloaderController
}

func newRestartRig(t *testing.T) *restartRig {
	t.Helper()
	d := &n22Daemon{}
	gate := &gateRec{}
	j := &memJ{}
	r := n22Controller(t, d, "restart",
		map[string]map[string]string{"vault:db": {"value": n19RestartSentinel}}, n22FileSecret(), dsoConfig.InjectionConfig{})
	// re-wrap the daemon so create bodies are captured and the delivery seam is used
	r2 := newMockController(t, gate.wrap(d))
	r2.Cache, r2.Config, r2.deliveryOps, r2.Journal = r.Cache, r.Config, gate.ops(), j
	r2.DeliveredVersion = func(prov, sec string, data map[string]string) string { return n19RestartVersion }
	r2.Targets.Store("orig-1", &TargetContainer{ID: "orig-1", Strategy: "restart", Secrets: []string{"db"}})
	gate.onEvent = nil
	return &restartRig{d: d, gate: gate, j: j, r: r2}
}

func TestN19Restart_Success_GatedDeliveryReleasedBeforeReady(t *testing.T) {
	g := newRestartRig(t)
	if err := waitOutcome(t, g.r, "db"); err != nil {
		t.Fatalf("restart rotation failed: %v", err)
	}
	if g.d.calls("/archive") != 0 {
		t.Fatal("the archive endpoint was used for file delivery")
	}
	// the candidate carries the gate; the original contract is preserved
	cfg := g.gate.last.Config
	if !rotation.IsGated(&cfg) {
		t.Fatal("candidate not gated")
	}
	if _, ok := g.gate.last.HostConfig.Tmpfs["/run/secrets/dso"]; !ok {
		t.Fatal("candidate lost the declared tmpfs")
	}
	ep, cmd := rotation.OriginalCommand(&cfg)
	if strings.Join(ep, "|") != "/app/entry" || strings.Join(cmd, "|") != "--serve|a b" {
		t.Fatalf("original command changed: %q %q", ep, cmd)
	}
	for _, b := range g.gate.bodies {
		if strings.Contains(string(b), n19RestartSentinel) {
			t.Fatal("secret value in the create request")
		}
	}
	ev := g.gate.eventList()
	for _, want := range []string{"inject:/run/secrets/dso", "probe:/run/secrets/dso", "release:/run/secrets/dso"} {
		if indexOf(ev, want) < 0 {
			t.Fatalf("missing %s in %v", want, ev)
		}
	}
	if indexOf(ev, "inject:/run/secrets/dso") >= indexOf(ev, "release:/run/secrets/dso") {
		t.Fatalf("release before injection: %v", ev)
	}
	tr := g.j.transitions()
	last := -1
	for _, w := range []string{rotation.TrCandidateCreated, rotation.TrGateStarted, rotation.TrIntentDeliverSecrets, rotation.TrSecretsDelivered, rotation.TrIntentReleaseGate, rotation.TrGateReleased, rotation.TrCandidateReady, rotation.TrCompleted} {
		i := indexOf(tr, w)
		if i < 0 || i < last {
			t.Fatalf("journal %v lacks %s in order", tr, w)
		}
		last = i
	}
	if e := g.j.event(rotation.TrCandidateCreated); e == nil || !e.Gated {
		t.Error("candidate_created not Gated")
	}
	if e := g.j.event(rotation.TrSecretsDelivered); e == nil || len(e.DeliveredVersions) != 1 || e.DeliveredVersions[0] != n19RestartVersion {
		t.Errorf("delivered versions not journalled: %+v", e)
	}
	if g.d.calls("DELETE /v1.24/containers/orig-1") == 0 && g.d.calls("DELETE") == 0 {
		t.Error("original not retired after a verified candidate")
	}
}

func TestN19Restart_RefusalBeforeAnyChange(t *testing.T) {
	g := newRestartRig(t)
	// the original declares no tmpfs: refused before the rename, journal untouched
	g.r.Config.Secrets[0].Inject.Path = "/data/secret" // not under the declared tmpfs
	err := waitOutcome(t, g.r, "db")
	if err == nil || !strings.Contains(err.Error(), "not covered by a tmpfs") {
		t.Fatalf("err = %v", err)
	}
	for _, sub := range []string{"/rename", "/containers/create", "/start", "/stop"} {
		if g.d.calls(sub) != 0 {
			t.Fatalf("%s called: nothing may change for a refused delivery (log %v)", sub, g.d.log)
		}
	}
	if len(g.j.transitions()) != 0 {
		t.Fatalf("journal trace for a refusal: %v", g.j.transitions())
	}
}

func TestN19Restart_FailuresRollBackToTheOriginal(t *testing.T) {
	cases := []struct {
		name    string
		prep    func(g *restartRig)
		wantErr string
		never   []string // journal transitions that must be absent
		started bool     // gate released?
	}{
		{"gate start failure", func(g *restartRig) { g.d.startErr = true }, "", []string{rotation.TrGateStarted, rotation.TrSecretsDelivered}, false},
		{"delivery failure", func(g *restartRig) { g.gate.injectErr = errSimulated }, "managed file delivery", []string{rotation.TrSecretsDelivered, rotation.TrGateReleased}, false},
		{"verification failure", func(g *restartRig) { g.gate.probeErr = errSimulated }, "probe", []string{rotation.TrSecretsDelivered, rotation.TrGateReleased}, false},
		{"release failure", func(g *restartRig) { g.gate.releaseErr = errSimulated }, "managed file delivery", []string{rotation.TrGateReleased, rotation.TrCandidateReady}, false},
		{"evidence cannot be journalled", func(g *restartRig) { g.j.failOn = rotation.TrSecretsDelivered }, "managed file delivery", []string{rotation.TrSecretsDelivered, rotation.TrGateReleased}, false},
		{"application unhealthy after release", func(g *restartRig) { g.d.health = "unhealthy" }, "", []string{rotation.TrCandidateReady}, true},
		{"post-readiness probe fails", func(g *restartRig) { g.gate.probeErr = errSimulated; g.gate.probeAfter = 2 }, "", []string{rotation.TrCandidateReady}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := rollbackRetryUnit
			rollbackRetryUnit = time.Millisecond
			t.Cleanup(func() { rollbackRetryUnit = old })
			g := newRestartRig(t)
			tc.prep(g)
			if tc.name == "evidence cannot be journalled" {
				g.r.Journal = g.j
			}
			err := waitOutcome(t, g.r, "db")
			if err == nil {
				t.Fatal("rotation reported success")
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if got := g.gate.has("release:/run/secrets/dso"); got != tc.started && tc.name != "release failure" {
				t.Fatalf("gate released = %v, want %v (events %v)", got, tc.started, g.gate.eventList())
			}
			for _, tr := range tc.never {
				if g.j.has(tr) {
					t.Fatalf("journal records %s although it did not happen: %v", tr, g.j.transitions())
				}
			}
			if g.d.calls("DELETE /v1.24/containers/orig-1") != 0 {
				t.Fatalf("the original was deleted (log %v)", g.d.log)
			}
			if g.d.calls("DELETE /v1.24/containers/new-1") == 0 {
				t.Fatalf("the failed replacement was not removed (log %v)", g.d.log)
			}
			if g.d.calls("POST /v1.24/containers/orig-1/start") == 0 {
				t.Fatalf("the original was not restarted (log %v)", g.d.log)
			}
		})
	}
}

// Crash at each delivery point of the restart strategy: the panic models the process
// dying; the controller's panic recovery runs no rollback, so the journal and the
// daemon keep exactly what recovery would find.
func TestN19Restart_CrashAtEveryDeliveryPoint(t *testing.T) {
	cases := []struct {
		point    string
		last     string
		injected bool
		released bool
	}{
		{"restart:after_gate_started", rotation.TrGateStarted, false, false},
		{"restart:after_intent_deliver", rotation.TrIntentDeliverSecrets, false, false},
		{"restart:after_spec_delivered", rotation.TrIntentDeliverSecrets, true, false},
		{"restart:after_probe_before_ack", rotation.TrIntentDeliverSecrets, true, false},
		{"restart:after_secrets_delivered", rotation.TrSecretsDelivered, true, false},
		{"restart:after_intent_release", rotation.TrIntentReleaseGate, true, false},
		{"restart:after_release_before_ack", rotation.TrIntentReleaseGate, true, true},
		{"restart:after_gate_released", rotation.TrGateReleased, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.point, func(t *testing.T) {
			g := newRestartRig(t)
			reached := false
			g.r.FaultHook = func(p string) {
				if p == tc.point {
					reached = true
					panic("crash:" + p)
				}
			}
			_ = waitOutcome(t, g.r, "db")
			if !reached {
				t.Fatalf("%s never reached", tc.point)
			}
			if got := g.j.last(); got != tc.last {
				t.Errorf("last durable transition = %s, want %s (%v)", got, tc.last, g.j.transitions())
			}
			if g.gate.has("inject:/run/secrets/dso") != tc.injected || g.gate.has("release:/run/secrets/dso") != tc.released {
				t.Errorf("injected/released wrong at %s: %v", tc.point, g.gate.eventList())
			}
			if g.j.has(rotation.TrCandidateReady) {
				t.Error("candidate_ready after a crash before readiness")
			}
			if g.d.calls("DELETE /v1.24/containers/orig-1") != 0 {
				t.Error("the original must never be deleted by a crashed rotation")
			}
		})
	}
}

// executeSimpleRestart (the rolling fallback) uses the same gated delivery.
func TestN19Fallback_GatedDelivery_SuccessAndRollback(t *testing.T) {
	spec := []rotation.FileSecretSpec{{Path: "/run/secrets/dso", Data: map[string]string{"db": n19RestartSentinel}, UID: 1000, GID: 1000, Version: n19RestartVersion}}
	run := func(t *testing.T, prep func(g *restartRig)) (*restartRig, error) {
		g := newRestartRig(t)
		prep(g)
		return g, g.r.executeSimpleRestart(context.Background(), "orig-1", nil, spec, nil, time.Second, "svc")
	}
	t.Run("success", func(t *testing.T) {
		g, err := run(t, func(*restartRig) {})
		if err != nil {
			t.Fatal(err)
		}
		ev := g.gate.eventList()
		if indexOf(ev, "inject:/run/secrets/dso") < 0 || indexOf(ev, "release:/run/secrets/dso") < indexOf(ev, "inject:/run/secrets/dso") {
			t.Fatalf("events %v", ev)
		}
		if !g.j.has(rotation.TrSecretsDelivered) || !g.j.has(rotation.TrCandidateReady) || g.j.last() != rotation.TrCompleted {
			t.Fatalf("journal %v", g.j.transitions())
		}
	})
	t.Run("delivery failure rolls back", func(t *testing.T) {
		old := rollbackRetryUnit
		rollbackRetryUnit = time.Millisecond
		t.Cleanup(func() { rollbackRetryUnit = old })
		g, err := run(t, func(g *restartRig) { g.gate.injectErr = errSimulated })
		if err == nil || !strings.Contains(err.Error(), "managed file delivery") || !strings.Contains(err.Error(), "rolled back") {
			t.Fatalf("err = %v", err)
		}
		if g.gate.has("release:/run/secrets/dso") || g.j.has(rotation.TrSecretsDelivered) {
			t.Fatal("released or journalled delivery after a failure")
		}
		if g.d.calls("DELETE /v1.24/containers/orig-1") != 0 || g.d.calls("DELETE /v1.24/containers/new-1") == 0 {
			t.Fatalf("log %v", g.d.log)
		}
	})
	t.Run("refusal before any change", func(t *testing.T) {
		g := newRestartRig(t)
		bad := []rotation.FileSecretSpec{{Path: "/not/a/tmpfs", Data: map[string]string{"db": "x"}, UID: 1000, GID: 1000}}
		err := g.r.executeSimpleRestart(context.Background(), "orig-1", nil, bad, nil, time.Second, "svc")
		if err == nil || !strings.Contains(err.Error(), "not covered by a tmpfs") {
			t.Fatalf("err = %v", err)
		}
		if g.d.calls("/rename") != 0 || len(g.j.transitions()) != 0 {
			t.Fatalf("a refusal changed something: %v %v", g.d.log, g.j.transitions())
		}
	})
}
