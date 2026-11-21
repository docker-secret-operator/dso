package watcher

// N19: a promoted gated container restarted by the daemon comes up with an empty tmpfs
// and a re-armed gate. The Docker `start` event handler re-delivers through the same
// verified path and releases the gate; every failure leaves the gate closed.

import (
	"net/http"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/docker-secret-operator/dso/internal/rotation"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

func redeliveryRig(t *testing.T) (*ReloaderController, *gateRec) {
	t.Helper()
	gate := &gateRec{}
	r := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r.deliveryOps = gate.ops()
	r.Cache = &fakeSecretCache{data: map[string]map[string]string{"vault:db": {"value": n19RestartSentinel}}}
	r.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets:   []dsoConfig.SecretMapping{n22FileSecret()},
	}
	r.Targets.Store("app-1", &TargetContainer{ID: "app-1", Strategy: "restart", Secrets: []string{"db"}})
	// the container the daemon restarted: gated config and declared tmpfs
	cfg := container.Config{Entrypoint: []string{"/app/entry"}, Cmd: []string{"serve"}}
	hc := &container.HostConfig{Tmpfs: map[string]string{"/run/secrets/dso": ""}}
	if err := rotation.PrepareGatedCandidate(&cfg, hc, []rotation.FileSecretSpec{{Path: "/run/secrets/dso", Data: map[string]string{"value": "x"}, UID: 1000, GID: 1000}}, 0); err != nil {
		t.Fatal(err)
	}
	gate.last = createReq{Config: cfg, HostConfig: hc}
	return r, gate
}

func gatedStartEvent(id string, gated bool) events.Message {
	attrs := map[string]string{"name": "app"}
	if gated {
		attrs[rotation.GateLabel] = "marker"
	}
	return events.Message{Action: "start", Actor: events.Actor{ID: id, Attributes: attrs}}
}

func TestN19Redelivery_RestartedGatedContainer_DeliversAndReleases(t *testing.T) {
	r, gate := redeliveryRig(t)
	if err := r.handleContainerEvent(t.Context(), gatedStartEvent("app-1", true)); err != nil {
		t.Fatal(err)
	}
	ev := gate.eventList()
	if indexOf(ev, "inject:/run/secrets/dso") < 0 || indexOf(ev, "release:/run/secrets/dso") < indexOf(ev, "inject:/run/secrets/dso") {
		t.Fatalf("events %v", ev)
	}
}

func TestN19Redelivery_NeverTouchesWhatItMustNot(t *testing.T) {
	cases := []struct {
		name string
		prep func(r *ReloaderController, g *gateRec)
		ev   events.Message
	}{
		{"not gated", func(*ReloaderController, *gateRec) {}, gatedStartEvent("app-1", false)},
		{"rotation candidate in flight", func(r *ReloaderController, _ *gateRec) { r.markGateOwned("app-1") }, gatedStartEvent("app-1", true)},
		{"proxy-pending candidate", func(r *ReloaderController, _ *gateRec) { r.markProxyPending("app-1") }, gatedStartEvent("app-1", true)},
		{"gate already released", func(_ *ReloaderController, g *gateRec) { g.flagPresent = true }, gatedStartEvent("app-1", true)},
		{"file secret unavailable (fail closed)", func(r *ReloaderController, _ *gateRec) {
			r.Cache = &fakeSecretCache{data: map[string]map[string]string{}}
		}, gatedStartEvent("app-1", true)},
		{"not a registered target", func(r *ReloaderController, _ *gateRec) { r.Targets.Delete("app-1") }, gatedStartEvent("app-1", true)},
		{"delivery failure leaves the gate closed", func(_ *ReloaderController, g *gateRec) { g.injectErr = errSimulated }, gatedStartEvent("app-1", true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, gate := redeliveryRig(t)
			tc.prep(r, gate)
			done := make(chan struct{})
			go func() { _ = r.handleContainerEvent(t.Context(), tc.ev); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("handler hung")
			}
			if gate.has("release:/run/secrets/dso") {
				t.Fatalf("the gate was released: %v", gate.eventList())
			}
		})
	}
}
