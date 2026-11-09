package audit

import (
	"context"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/observability"
)

// drainOneStreamEvent reads exactly one event off observability.EventStream,
// failing the test if none arrives -- avoids a real network/timer wait since
// the channel is buffered and Log's send is synchronous relative to this
// call (both run on the same goroutine).
func drainOneStreamEvent(t *testing.T) map[string]interface{} {
	t.Helper()
	select {
	case ev := <-observability.EventStream:
		return ev
	default:
		t.Fatal("expected an event on observability.EventStream, got none")
		return nil
	}
}

// TestLog_SecretFetch_IncludesUserAsActor is the Phase 5 regression test for
// the Audit page's Actor column: secret_fetch is the one EventStream
// producer that already receives a meaningful caller identity (the Unix
// socket peer's resolved OS username), so it must be forwarded onto the
// stream rather than leaving every /api/audit row with no actor at all.
func TestLog_SecretFetch_IncludesUserAsActor(t *testing.T) {
	Log(context.Background(), "secret_fetch", "alice", "vault", "db-password", "container123", "success")

	ev := drainOneStreamEvent(t)
	if ev["user"] != "alice" {
		t.Errorf("expected user=alice on the streamed event, got %+v", ev)
	}
}

// TestLog_SecretFetch_EmptyUserOmitted verifies an empty caller identity
// (e.g. a code path that never resolved one) omits the field entirely
// rather than streaming an empty string -- so the frontend's honest
// "Not available" fallback (never a fabricated "System") actually triggers.
func TestLog_SecretFetch_EmptyUserOmitted(t *testing.T) {
	Log(context.Background(), "secret_fetch", "", "vault", "db-password", "", "success")

	ev := drainOneStreamEvent(t)
	if _, present := ev["user"]; present {
		t.Errorf("expected no user field for an empty caller identity, got %+v", ev)
	}
}

// TestLog_RotateAction_NotProjectedToEventStream documents the existing,
// deliberate routing: "rotate" audit actions are NOT pushed onto
// EventStream (rotation outcomes already flow through
// internal/agent.TriggerEngine.emitEvent as rotation_succeeded/failed --
// routing "rotate" here too would double-count). This guards against a
// future edit accidentally widening the projection.
func TestLog_RotateAction_NotProjectedToEventStream(t *testing.T) {
	Log(context.Background(), "rotate", "system", "vault", "db-password", "container123", "success")

	select {
	case ev := <-observability.EventStream:
		t.Fatalf("expected no EventStream event for a \"rotate\" action, got %+v", ev)
	default:
		// Correct: nothing streamed.
	}
}
