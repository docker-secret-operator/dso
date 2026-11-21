//go:build dockerint

package docker

// Guarded migration of internal/agent/notification_integration_test.go (M0-11).
//
// End-to-end proof that a REAL rotation, executed by the REAL
// TriggerEngine.ExecuteRotation against a REAL container on the guarded daemon,
// produces a REAL webhook delivery (ExecuteRotation -> TriggerReload -> signal
// strategy -> emitEvent -> observability.EventStream -> alert.Evaluator ->
// Dispatcher -> WebhookNotifier -> HTTP server).
//
// Deviation from the original: the original used an ENV-mode secret with the
// "signal" strategy and a bare SIGHUP. Since M0-03 that combination is REJECTED (a
// signal cannot change a running process's environment), so a faithful copy would
// now observe rotation_failed. The migrated test uses the supported shape: a
// file-mode secret delivered to a tmpfs owned by a non-root app user, then SIGHUP.
// The webhook assertions are unchanged; the application actually loading the new
// value is an added assertion. The container is also discovered through label
// discovery (owner gate) instead of a hand-stored target.
//
// The TriggerEngine state/lock directories are per-test temp dirs via
// agent.NewTriggerEngineWithDirs (production uses /var/lib/dso).
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/alert"
	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/observability"
)

func TestNotificationIntegration_RealRotationDeliversWebhook(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	logger := zaptest.NewLogger(t)

	id := startReloadApp(t, runLabel, false)
	seedFile(t, id, syntheticValue("v1"))
	waitFor(t, "app start", 20*time.Second, func() bool { _, err := execOut(t, id, "cat", "/tmp/started"); return err == nil })

	// Real webhook destination: an actual HTTP server receiving an actual POST.
	received := make(chan notify.RotationEvent, 4)
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e notify.RotationEvent
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Errorf("webhook received invalid JSON: %v", err)
		}
		received <- e
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookSrv.Close()

	notifier, err := notify.NewWebhookNotifier(notify.WebhookOptions{URL: webhookSrv.URL, AllowInsecureHTTP: true}, logger)
	if err != nil {
		t.Fatalf("NewWebhookNotifier: %v", err)
	}
	dispatcher := notify.NewDispatcher([]*notify.WebhookNotifier{notifier}, logger)
	defer dispatcher.Stop(5 * time.Second)

	cfg := fileModeConfig()
	cache := agent.NewSecretCache(5 * time.Minute)
	defer cache.Close()

	reloader, err := watcher.NewReloaderController(logger)
	if err != nil {
		t.Fatalf("NewReloaderController: %v", err)
	}
	trigger, err := agent.NewTriggerEngineWithDirs(cache, nil, reloader, logger, cfg, cli, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewTriggerEngineWithDirs: %v", err)
	}
	trigger.Dispatcher = dispatcher
	defer trigger.Stop()

	evCtx, evCancel := context.WithCancel(context.Background())
	defer evCancel()
	reloader.StartEventLoop(evCtx)
	waitTarget(t, reloader, id)

	// Drain events left on the shared EventStream by earlier tests in this binary,
	// then run an Evaluator against it (the wiring internal/server.StartRESTServer
	// performs in production).
	for drained := false; !drained; {
		select {
		case <-observability.EventStream:
		default:
			drained = true
		}
	}
	evaluator := alert.NewEvaluator(dispatcher, time.Minute, logger)
	go func() {
		for {
			select {
			case <-evCtx.Done():
				return
			case ev := <-observability.EventStream:
				evaluator.Evaluate(ev)
			}
		}
	}()

	// A standalone rotation_succeeded with no prior failure is deliberately a no-op
	// (Phase 4.1). Seed a synthetic prior rotation_failed for the same secret so the
	// real rotation has something real to resolve.
	evaluator.Evaluate(map[string]interface{}{
		"event_type": "rotation_failed",
		"secret":     secretName,
		"status":     "failure",
	})
	select {
	case seed := <-received:
		if seed.Type != notify.RotationFailed {
			t.Fatalf("expected the seed step's synthetic notification to be rotation_failed, got %s", seed.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("seed rotation_failed notification was never delivered")
	}

	// The production authoritative-rotation entrypoint.
	trigger.ExecuteRotation(providerName, secretName, map[string]string{"value": syntheticValue("v2")}, cfg.Secrets[0])

	select {
	case event := <-received:
		if event.Type != notify.RotationSucceeded {
			t.Errorf("expected rotation_succeeded, got %s (error_message=%q)", event.Type, event.ErrorMessage)
		}
		if event.SecretName != secretName || event.Provider != providerName {
			t.Errorf("event metadata mismatch: secret=%q provider=%q", event.SecretName, event.Provider)
		}
		if len(event.Containers) != 1 || event.Containers[0] != id {
			t.Errorf("expected affected_containers=[%s], got %v", id[:12], event.Containers)
		}
		if strings.Contains(event.ErrorMessage, sentinel) {
			t.Error("webhook payload leaks the synthetic secret value")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("no webhook delivery received for a real rotation within 60s")
	}

	// Added: the real rotation really reached the application.
	waitFor(t, "application to load v2", 20*time.Second, func() bool {
		got, err := execOut(t, id, "cat", "/tmp/loaded")
		return err == nil && got == syntheticValue("v2")
	})
}
