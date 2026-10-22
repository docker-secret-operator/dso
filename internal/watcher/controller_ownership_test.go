package watcher

// SEC-P0: regression tests for the container-discovery ownership-isolation
// fix. These prove that dso.reloader=true + dso.secrets matching ALONE
// (pre-fix behavior) is no longer sufficient for a container to be
// registered/rotated by this agent -- dso.owner must also match this
// agent's own OwnerID, and a container with no dso.owner label at all
// (a "legacy" pre-fix container) must be skipped, never adopted.
//
// Tests 1-4 and 6 run directly against checkOwnership, the single gate every
// registration path (populateInitialTargets, handleContainerEvent,
// reconcileRuntimeState) calls -- this is deliberate: it proves the SAME
// function used by every real discovery path enforces the invariant,
// instead of duplicating Docker-mock plumbing per path for cases that are
// really about the gate's logic. Tests 5, 8, 9 additionally exercise the
// real discovery paths end-to-end via the mock Docker HTTP server already
// used elsewhere in this package (newMockController) to prove the gate is
// actually wired into each path, not just correct in isolation. Test 7
// covers replacement-container label preservation (rotation package). Test
// 10 (recovery/orphan paths) lives in internal/agent, alongside
// AutomaticRecovery.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"go.uber.org/zap"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker/docker/api/types/events"
	"go.uber.org/zap/zaptest"
)

func controllerWithOwner(t *testing.T, ownerID string) *ReloaderController {
	t.Helper()
	return &ReloaderController{
		Logger: zaptest.NewLogger(t),
		Config: &dsoConfig.Config{Agent: dsoConfig.AgentConfig{OwnerID: ownerID}},
	}
}

// Test 1: two independent agents, identical secret name. Each must see only
// its own container via the shared gate.
func TestOwnership_TwoAgentsSameSecret(t *testing.T) {
	agentA := controllerWithOwner(t, "owner-A")
	agentB := controllerWithOwner(t, "owner-B")

	containerA := map[string]string{"dso.reloader": "true", "dso.secrets": "test_secret", "dso.owner": "owner-A"}
	containerB := map[string]string{"dso.reloader": "true", "dso.secrets": "test_secret", "dso.owner": "owner-B"}

	if !agentA.checkOwnership("cA", "containerA", containerA) {
		t.Error("agent A must accept its own container")
	}
	if agentA.checkOwnership("cB", "containerB", containerB) {
		t.Error("agent A must reject agent B's container despite identical secret name")
	}
	if !agentB.checkOwnership("cB", "containerB", containerB) {
		t.Error("agent B must accept its own container")
	}
	if agentB.checkOwnership("cA", "containerA", containerA) {
		t.Error("agent B must reject agent A's container despite identical secret name")
	}
}

// Test 2: ownership alone (not network) must be doing the work. This is
// proven by never supplying a network at all to checkOwnership -- it has no
// network parameter, so if it correctly separated A/B here, no fallback to
// network-based filtering could be responsible.
func TestOwnership_DifferentNetworksIrrelevant(t *testing.T) {
	agentA := controllerWithOwner(t, "owner-A")
	containerOnOtherNetwork := map[string]string{
		"dso.reloader": "true", "dso.secrets": "test_secret", "dso.owner": "owner-B",
	}
	if agentA.checkOwnership("cB", "containerB", containerOnOtherNetwork) {
		t.Error("network separation must not be relied upon; owner mismatch alone must reject")
	}
}

// Test 3: different Compose projects, same secret name, different owners.
func TestOwnership_DifferentComposeProjectsIrrelevant(t *testing.T) {
	agentA := controllerWithOwner(t, "owner-A")
	containerOtherProject := map[string]string{
		"dso.reloader":               "true",
		"dso.secrets":                "test_secret",
		"dso.owner":                  "owner-B",
		"com.docker.compose.project": "other-project",
	}
	if agentA.checkOwnership("cB", "containerB", containerOtherProject) {
		t.Error("compose project must not substitute for ownership; owner mismatch alone must reject")
	}
}

// Test 4: identical dso.reloader/dso.secrets/dso.update.strategy, only
// dso.owner differs -- owner ID alone must prevent the collision.
func TestOwnership_OnlyOwnerDiffers(t *testing.T) {
	agentA := controllerWithOwner(t, "owner-A")
	same := map[string]string{
		"dso.reloader":        "true",
		"dso.secrets":         "test_secret",
		"dso.update.strategy": "rolling",
		"dso.owner":           "owner-B",
	}
	if agentA.checkOwnership("cB", "containerB", same) {
		t.Error("owner mismatch must reject even when every other label is identical")
	}
	same["dso.owner"] = "owner-A"
	if !agentA.checkOwnership("cB", "containerB", same) {
		t.Error("matching owner must accept when every other label is identical")
	}
}

// Test 5 (own container / normal discovery unaffected) is exercised
// end-to-end via populateInitialTargets against the mock Docker server.
func TestOwnership_OwnContainer_NormalDiscoveryStillWorks(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"Id":     "own1",
				"Names":  []string{"/own-container"},
				"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "test_secret_a", "dso.owner": "owner-A"},
			},
		})
	})
	c := newMockController(t, handler)
	c.Config = &dsoConfig.Config{Agent: dsoConfig.AgentConfig{OwnerID: "owner-A"}}

	c.populateInitialTargets(context.Background())

	if _, ok := c.Targets.Load("own1"); !ok {
		t.Error("agent must register its own owned container")
	}
}

// Test 6: legacy container -- dso.reloader=true + dso.secrets, NO dso.owner.
// Must NOT be registered.
func TestOwnership_LegacyContainerWithoutOwner_NotRegistered(t *testing.T) {
	agentA := controllerWithOwner(t, "owner-A")
	legacy := map[string]string{"dso.reloader": "true", "dso.secrets": "test_secret"} // no dso.owner
	if agentA.checkOwnership("legacy1", "legacy-container", legacy) {
		t.Error("a dso.reloader container with no dso.owner label must be rejected (default-deny), not silently adopted")
	}
}

// Test 8: event-driven registration. Container appears after startup via a
// Docker "start" event -- matching owner registers, wrong owner is ignored.
func TestOwnership_EventDrivenRegistration(t *testing.T) {
	c := controllerWithOwner(t, "owner-A")

	matchEvt := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "evt-match",
			Attributes: map[string]string{
				"dso.reloader": "true", "dso.secrets": "test_secret", "dso.owner": "owner-A", "name": "/evt-match",
			},
		},
	}
	if err := c.handleContainerEvent(context.Background(), matchEvt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.Targets.Load("evt-match"); !ok {
		t.Error("matching-owner container start event must register the container")
	}

	wrongEvt := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "evt-wrong",
			Attributes: map[string]string{
				"dso.reloader": "true", "dso.secrets": "test_secret", "dso.owner": "owner-B", "name": "/evt-wrong",
			},
		},
	}
	if err := c.handleContainerEvent(context.Background(), wrongEvt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.Targets.Load("evt-wrong"); ok {
		t.Error("wrong-owner container start event must NOT register the container")
	}

	noOwnerEvt := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "evt-legacy",
			Attributes: map[string]string{
				"dso.reloader": "true", "dso.secrets": "test_secret", "name": "/evt-legacy",
			},
		},
	}
	if err := c.handleContainerEvent(context.Background(), noOwnerEvt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.Targets.Load("evt-legacy"); ok {
		t.Error("legacy container (no dso.owner) start event must NOT register the container")
	}
}

// Test 9: periodic reconciliation re-registers a matching-owner container
// that started while the event stream was disconnected, and ignores a
// wrong-owner/legacy one.
func TestOwnership_PeriodicReconciliation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Query().Get("filters") != "" && r.Method == http.MethodGet:
			// The labeled-containers pass (label=dso.reloader=true) and the
			// all-containers pass (All:true, no label filter) both hit
			// /containers/json; distinguish by whether a label filter was sent.
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"Id":     "recon-match",
					"Names":  []string{"/recon-match"},
					"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "test_secret", "dso.owner": "owner-A"},
				},
				{
					"Id":     "recon-wrong",
					"Names":  []string{"/recon-wrong"},
					"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "test_secret", "dso.owner": "owner-B"},
				},
				{
					"Id":     "recon-legacy",
					"Names":  []string{"/recon-legacy"},
					"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "test_secret"},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		}
	})
	c := newMockController(t, handler)
	c.Config = &dsoConfig.Config{Agent: dsoConfig.AgentConfig{OwnerID: "owner-A"}}

	c.reconcileRuntimeState(context.Background())

	if _, ok := c.Targets.Load("recon-match"); !ok {
		t.Error("reconciliation must re-register a matching-owner container missed during a reconnect")
	}
	if _, ok := c.Targets.Load("recon-wrong"); ok {
		t.Error("reconciliation must NOT register a wrong-owner container")
	}
	if _, ok := c.Targets.Load("recon-legacy"); ok {
		t.Error("reconciliation must NOT register a legacy (no dso.owner) container")
	}
}

// Config-driven fallback (container named in secrets[].targets.containers,
// no dso.reloader label): a container claimed by ANOTHER agent must never be
// adopted; an unowned one named explicitly in the config still is (explicit
// operator opt-in), and the agent's own container is.
func TestOwnership_ConfigDrivenEventPathRejectsForeignOwner(t *testing.T) {
	c := controllerWithOwner(t, "owner-A")
	c.Config.Secrets = []dsoConfig.SecretMapping{{Name: "db", Targets: dsoConfig.TargetConfig{Containers: []string{"web"}}}}

	mk := func(id string, owner string) events.Message {
		attrs := map[string]string{"name": "/web"}
		if owner != "" {
			attrs["dso.owner"] = owner
		}
		return events.Message{Action: "start", Actor: events.Actor{ID: id, Attributes: attrs}}
	}
	for _, tc := range []struct {
		id, owner string
		want      bool
	}{{"foreign", "owner-B", false}, {"unowned", "", true}, {"mine", "owner-A", true}} {
		if err := c.handleContainerEvent(context.Background(), mk(tc.id, tc.owner)); err != nil {
			t.Fatal(err)
		}
		if _, ok := c.Targets.Load(tc.id); ok != tc.want {
			t.Errorf("%s: registered=%v, want %v", tc.id, ok, tc.want)
		}
	}
}

// Initial population (agent startup): label-driven Pass 1 registers only the
// agent's own container; config-driven Pass 2 skips a foreign-owned
// container but still honors an ownerless container named in the config.
func TestOwnership_InitialPopulation(t *testing.T) {
	labeled := []map[string]interface{}{
		{"Id": "p1-mine", "Names": []string{"/p1-mine"}, "Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "db", "dso.owner": "owner-A"}},
		{"Id": "p1-foreign", "Names": []string{"/p1-foreign"}, "Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "db", "dso.owner": "owner-B"}},
		{"Id": "p1-legacy", "Names": []string{"/p1-legacy"}, "Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "db"}},
	}
	all := []map[string]interface{}{
		{"Id": "p2-foreign", "Names": []string{"/cfg-foreign"}, "Labels": map[string]string{"dso.owner": "owner-B"}},
		{"Id": "p2-unowned", "Names": []string{"/cfg-unowned"}, "Labels": map[string]string{}},
		{"Id": "p2-mine", "Names": []string{"/cfg-mine"}, "Labels": map[string]string{"dso.owner": "owner-A"}},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Query().Get("filters") != "" {
			_ = json.NewEncoder(w).Encode(labeled)
			return
		}
		_ = json.NewEncoder(w).Encode(all)
	})
	c := newMockController(t, handler)
	c.Config = &dsoConfig.Config{
		Agent: dsoConfig.AgentConfig{OwnerID: "owner-A"},
		Secrets: []dsoConfig.SecretMapping{{Name: "db", Targets: dsoConfig.TargetConfig{
			Containers: []string{"cfg-foreign", "cfg-unowned", "cfg-mine"}}}},
	}

	c.populateInitialTargets(context.Background())

	for id, want := range map[string]bool{
		"p1-mine": true, "p1-foreign": false, "p1-legacy": false,
		"p2-foreign": false, "p2-unowned": true, "p2-mine": true,
	} {
		if _, ok := c.Targets.Load(id); ok != want {
			t.Errorf("%s: registered=%v, want %v", id, ok, want)
		}
	}
}

// Start event for a container WITHOUT dso.reloader but with dso.host_ports:
// a foreign-owned one must never reach the proxy (no Docker inspect, so no
// registration); the agent's own and an unowned one still proceed.
func TestOwnership_StartEventProxyRegistrationGate(t *testing.T) {
	inspects := map[string]int{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json") {
			parts := strings.Split(r.URL.Path, "/")
			inspects[parts[len(parts)-2]]++
		}
		http.NotFound(w, r)
	})
	c := newMockController(t, handler)
	c.Config = &dsoConfig.Config{Agent: dsoConfig.AgentConfig{OwnerID: "owner-A"}}
	c.ProxyManager = dsoProxy.NewManager(zap.NewNop())
	t.Cleanup(func() { _ = c.ProxyManager.Stop(100 * time.Millisecond) })

	for _, tc := range []struct{ id, owner string }{{"px-foreign", "owner-B"}, {"px-mine", "owner-A"}, {"px-unowned", ""}} {
		attrs := map[string]string{"name": "/" + tc.id, "dso.host_ports": "18080:80"}
		if tc.owner != "" {
			attrs["dso.owner"] = tc.owner
		}
		if err := c.handleContainerEvent(context.Background(), events.Message{Action: "start", Actor: events.Actor{ID: tc.id, Attributes: attrs}}); err != nil {
			t.Fatal(err)
		}
	}
	if inspects["px-foreign"] != 0 {
		t.Errorf("foreign-owned container reached proxy registration (inspect calls=%d)", inspects["px-foreign"])
	}
	if inspects["px-mine"] == 0 || inspects["px-unowned"] == 0 {
		t.Errorf("own/unowned containers must still proceed to proxy registration: %v", inspects)
	}
}
