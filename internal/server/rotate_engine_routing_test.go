package server

import (
	"net/http"
	"testing"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/pkg/observability"
)

// The manual rotation (POST /api/secrets/{name}/rotate -- the Web UI's rotate
// button) must reach the SAME managed lifecycle as an automatic rotation, through
// the trigger engine's managed entry point, so the journal and the applied-version
// ledger see it. These tests run the real handler against the real engine and the
// mock Docker daemon of rotationFixture. The ledger transitions themselves are
// proven against the same engine method in internal/agent
// (TestManagedRotation_Manual_*); this file proves the handler routes to it.

// wireEngine gives the fixture's REST server a real trigger engine that drives the
// fixture's reloader, as `dso agent` wires them.
func wireEngine(t *testing.T, s *RESTServer) *agent.TriggerEngine {
	t.Helper()
	if eng, ok := s.TriggerEngine.(*agent.TriggerEngine); ok && eng != nil {
		return eng // already wired by rotationFixture
	}
	eng, err := agent.NewTriggerEngineWithDirs(s.Cache, nil, s.Reloader, zap.NewNop(), s.Config, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Stop)
	s.TriggerEngine = eng
	return eng
}

// A failed manual rotation leaves the ENGINE's journal record (provider, target
// count, rollback_required) -- a strategy writing on its own would leave neither
// the provider nor the dispatch scope.
func TestManualRotate_RoutesThroughTheEngine_Journal(t *testing.T) {
	s, ts, calls := rotationFixture(t, "fail")
	eng := wireEngine(t, s)
	postRotate(t, ts)

	waitFor(t, "the engine's journal record of the failed manual rotation", func() bool {
		return len(eng.StateTracker.GetPendingRotations()) == 1
	})
	rec := eng.StateTracker.GetPendingRotations()[0]
	if rec.SecretName != "db" || rec.ProviderName != "vault" || rec.DispatchTargets != 1 || rec.RotationID == "" || rec.Status != "rollback_required" {
		t.Fatalf("journal record %+v", rec)
	}
	// The strategies still do their own rollback: the original gets its name back.
	waitFor(t, "original renamed away and back", func() bool { return calls.count("/containers/orig-1/rename") >= 2 })
}

// A successful manual rotation is completed by the engine (journal completed,
// rotation success counted), not by a bare reloader callback.
func TestManualRotate_RoutesThroughTheEngine_Completion(t *testing.T) {
	s, ts, calls := rotationFixture(t, "ok")
	wireEngine(t, s)
	before := observability.RotationSuccessTotal.Load()
	postRotate(t, ts)
	waitFor(t, "original removed", func() bool { return calls.countReq("DELETE", "/containers/orig-1") > 0 })
	// finishRotation counts the success (a bare reloader callback never did).
	waitFor(t, "the engine's completion handling to count the rotation", func() bool {
		return observability.RotationSuccessTotal.Load() > before
	})
}

// The synchronous refusals keep their status codes through the engine.
func TestManualRotate_Engine_NoManagedContainers_404(t *testing.T) {
	s, ts, _ := rotationFixture(t, "ok")
	eng := wireEngine(t, s)
	s.Reloader.Targets.Delete("orig-1")
	resp, err := http.Post(ts.URL+"/api/secrets/db/rotate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
	if n := len(eng.StateTracker.GetPendingRotations()); n != 0 {
		t.Fatalf("a refused rotation left %d journal records", n)
	}
}
