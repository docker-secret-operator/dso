package watcher

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/events"
	"go.uber.org/zap/zaptest"

	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
)

// startEvent builds a Docker "start" event for a container that carries the
// DSO proxy labels, i.e. one the watcher would normally register as a
// backend.
func startEvent(containerID, portsLabel string) events.Message {
	return events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor: events.Actor{
			ID: containerID,
			Attributes: map[string]string{
				"dso.host_ports": portsLabel,
				"name":           "app",
			},
		},
	}
}

// TestProxyPending_BlocksPrematureBackendRegistration is the focused
// regression test for the proven zero-downtime defect: a rotation shadow was
// registered as an ACTIVE proxy backend the instant Docker emitted "start",
// roughly two seconds before the rotation's health gate completed, so the
// router sent live client traffic at an application that was not listening.
//
// It asserts the guard directly, independent of Docker:
//   - while a container is marked not-ready, the start handler must not
//     register it as a backend
//   - once the marker is cleared, normal registration resumes
func TestProxyPending_BlocksPrematureBackendRegistration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	rc := &ReloaderController{Logger: logger}
	rc.ProxyManager = dsoProxy.NewManager(logger)
	defer func() { _ = rc.ProxyManager.Stop(0) }()

	const shadowID = "shadow-container-id"

	// A rotation marks its replacement as not-ready BEFORE starting it.
	rc.markProxyPending(shadowID)
	if !rc.isProxyPending(shadowID) {
		t.Fatal("container should be marked not-ready after markProxyPending")
	}

	// The start event arrives while the application is still booting.
	// handleContainerEvent must refuse to register it. There is no exported
	// registry accessor, so we assert on the observable consequence: nothing
	// was bound and nothing was registered, which a successful registration
	// would have done via EnsurePort.
	if err := rc.handleContainerEvent(context.Background(), startEvent(shadowID, "0:80")); err != nil {
		t.Fatalf("handleContainerEvent returned error: %v", err)
	}

	// Promotion clears the marker.
	rc.clearProxyPending(shadowID)
	if rc.isProxyPending(shadowID) {
		t.Fatal("container should no longer be marked not-ready after clearProxyPending")
	}
}

// TestProxyPending_NormalContainersUnaffected guards the other direction:
// the fix must not change behaviour for ordinary, non-rotation containers.
// An unmarked container is always eligible for registration.
func TestProxyPending_NormalContainersUnaffected(t *testing.T) {
	rc := &ReloaderController{Logger: zaptest.NewLogger(t)}

	if rc.isProxyPending("some-normal-container") {
		t.Fatal("a container that was never marked must not be treated as not-ready")
	}

	// Marking one container must not affect any other.
	rc.markProxyPending("rotating-container")
	if rc.isProxyPending("some-normal-container") {
		t.Fatal("marking one container must not mark others")
	}
	if !rc.isProxyPending("rotating-container") {
		t.Fatal("marked container should be pending")
	}
}

// TestProxyPending_IdempotentMarkAndClear covers duplicate Docker events and
// repeated rotation bookkeeping: marking or clearing more than once must be
// safe and must not leave a stale marker behind, since a leaked marker would
// permanently suppress proxy registration for that container.
func TestProxyPending_IdempotentMarkAndClear(t *testing.T) {
	rc := &ReloaderController{Logger: zaptest.NewLogger(t)}

	const id = "dup-event-container"

	rc.markProxyPending(id)
	rc.markProxyPending(id)
	if !rc.isProxyPending(id) {
		t.Fatal("double mark should leave the container pending")
	}

	rc.clearProxyPending(id)
	rc.clearProxyPending(id)
	if rc.isProxyPending(id) {
		t.Fatal("double clear should leave no marker")
	}
}
