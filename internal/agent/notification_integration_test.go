package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker/docker/client"
	"github.com/testcontainers/testcontainers-go"
	"go.uber.org/zap/zaptest"
)

// TestNotificationIntegration_RealRotationDeliversWebhook is the end-to-end
// proof the unit-level tests (internal/notify) can't provide on their own:
// that a REAL rotation, executed by the REAL TriggerEngine.ExecuteRotation
// against a REAL container on a REAL Docker daemon, results in a REAL
// webhook delivery -- not a synthetic event constructed by hand.
//
// Uses the "signal" strategy specifically: it's the simplest real rotation
// path (a single ContainerKill(SIGHUP), no health-check machinery), which
// keeps this test's Docker dependency minimal while still exercising the
// exact production call chain: ExecuteRotation -> TriggerReload -> signal
// strategy -> onComplete -> emitEvent -> Dispatcher -> WebhookNotifier.
func TestNotificationIntegration_RealRotationDeliversWebhook(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker-backed integration test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Real container on the real Docker daemon.
	testContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "alpine:latest",
			Cmd:   []string{"sh", "-c", "trap 'echo got-sighup' HUP; while true; do sleep 1; done"},
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("failed to start test container: %v", err)
	}
	defer func() { _ = testContainer.Terminate(ctx) }()
	containerID := testContainer.GetContainerID()

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	defer func() { _ = dockerCli.Close() }()

	// Real webhook destination: an actual HTTP server receiving an actual
	// POST from an actual WebhookNotifier, not a hand-constructed event.
	received := make(chan notify.RotationEvent, 1)
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e notify.RotationEvent
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Errorf("webhook received invalid JSON: %v", err)
		}
		received <- e
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookSrv.Close()

	logger := zaptest.NewLogger(t)

	notifier, err := notify.NewWebhookNotifier(notify.WebhookOptions{URL: webhookSrv.URL, AllowInsecureHTTP: true}, logger)
	if err != nil {
		t.Fatalf("NewWebhookNotifier: %v", err)
	}
	dispatcher := notify.NewDispatcher([]*notify.WebhookNotifier{notifier}, logger)
	defer dispatcher.Stop(5 * time.Second)

	reloader, err := watcher.NewReloaderController(logger)
	if err != nil {
		t.Fatalf("NewReloaderController: %v", err)
	}
	reloader.Targets.Store(containerID, &watcher.TargetContainer{
		ID:       containerID,
		Strategy: "signal",
		Secrets:  []string{"db_password"},
	})

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{"vault": {Type: "vault"}},
		Secrets:   []config.SecretMapping{{Name: "db_password", Provider: "vault"}},
	}
	cache := NewSecretCache(5 * time.Minute)
	defer cache.Close()

	trigger := NewTriggerEngineForTest(t, cache, (*providers.SecretStoreManager)(nil), reloader, logger, cfg, nil)
	trigger.Dispatcher = dispatcher
	defer trigger.Stop()

	// The real, production authoritative-rotation entrypoint -- exactly
	// what a real poll cycle calls on a detected secret change.
	trigger.ExecuteRotation("vault", "db_password", map[string]string{"password": "new-value"}, cfg.Secrets[0])

	select {
	case event := <-received:
		if event.Type != notify.RotationSucceeded {
			t.Errorf("expected rotation_succeeded, got %s (error_message=%q)", event.Type, event.ErrorMessage)
		}
		if event.SecretName != "db_password" || event.Provider != "vault" {
			t.Errorf("event metadata mismatch: %+v", event)
		}
		if len(event.Containers) != 1 || event.Containers[0] != containerID {
			t.Errorf("expected affected_containers=[%s], got %v", containerID, event.Containers)
		}
		t.Logf("real end-to-end delivery confirmed: %+v", event)
	case <-time.After(20 * time.Second):
		t.Fatal("no webhook delivery received for a real rotation within 20s")
	}
}
