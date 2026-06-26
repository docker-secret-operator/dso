package setup

import (
	"context"
	"errors"
	"testing"
)

// ─── Engine tests ─────────────────────────────────────────────────────────────

func TestEngine_Setup_Success(t *testing.T) {
	var called bool
	wizard := func(_ context.Context, mode, provider string, autoDetect, nonRoot bool) error {
		called = true
		return nil
	}

	eng := NewEngine(wizard)
	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !called {
		t.Fatal("expected legacy wizard to be called")
	}
	if result.Status != "success" {
		t.Errorf("expected status 'success', got %q", result.Status)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestEngine_Setup_Failure(t *testing.T) {
	sentinel := errors.New("wizard exploded")
	wizard := func(_ context.Context, _, _ string, _, _ bool) error {
		return sentinel
	}

	eng := NewEngine(wizard)
	result, err := eng.Setup(context.Background(), SetupOptions{})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", result.Status)
	}
}

func TestEngine_Setup_EmitsLifecycleEvents(t *testing.T) {
	wizard := func(_ context.Context, _, _ string, _, _ bool) error { return nil }
	eng := NewEngine(wizard)

	var events []EventType
	eng.Events.Subscribe(func(evt Event) {
		events = append(events, evt.Type)
	})

	_, _ = eng.Setup(context.Background(), SetupOptions{})

	want := []EventType{EventSetupStarted, EventSetupCompleted}
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d: %v", len(want), len(events), events)
	}
	for i, e := range want {
		if events[i] != e {
			t.Errorf("event[%d]: want %q, got %q", i, e, events[i])
		}
	}
}

func TestEngine_Setup_EmitsFailureEvent(t *testing.T) {
	wizard := func(_ context.Context, _, _ string, _, _ bool) error {
		return errors.New("boom")
	}
	eng := NewEngine(wizard)

	var events []EventType
	eng.Events.Subscribe(func(evt Event) {
		events = append(events, evt.Type)
	})

	_, _ = eng.Setup(context.Background(), SetupOptions{})

	want := []EventType{EventSetupStarted, EventSetupFailed}
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d: %v", len(want), len(events), events)
	}
	for i, e := range want {
		if events[i] != e {
			t.Errorf("event[%d]: want %q, got %q", i, e, events[i])
		}
	}
}

func TestEngine_Setup_OptionsPassedToWizard(t *testing.T) {
	var (
		capturedMode     string
		capturedProvider string
		capturedDetect   bool
		capturedNonRoot  bool
	)
	wizard := func(_ context.Context, mode, provider string, autoDetect, nonRoot bool) error {
		capturedMode = mode
		capturedProvider = provider
		capturedDetect = autoDetect
		capturedNonRoot = nonRoot
		return nil
	}

	eng := NewEngine(wizard)
	_, _ = eng.Setup(context.Background(), SetupOptions{
		Mode:       ModeAgent,
		Provider:   "aws",
		AutoDetect: true,
		NonRoot:    true,
	})

	if capturedMode != "agent" {
		t.Errorf("mode: want 'agent', got %q", capturedMode)
	}
	if capturedProvider != "aws" {
		t.Errorf("provider: want 'aws', got %q", capturedProvider)
	}
	if !capturedDetect {
		t.Error("autoDetect: expected true")
	}
	if !capturedNonRoot {
		t.Error("nonRoot: expected true")
	}
}

// ─── Emitter tests ────────────────────────────────────────────────────────────

func TestEmitter_Subscribe_ReceivesEvent(t *testing.T) {
	e := &Emitter{}
	var received []Event
	e.Subscribe(func(evt Event) {
		received = append(received, evt)
	})

	e.Emit(Event{Type: EventSetupStarted})

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Type != EventSetupStarted {
		t.Errorf("expected EventSetupStarted, got %q", received[0].Type)
	}
}

func TestEmitter_Emit_SetsTimestamp(t *testing.T) {
	e := &Emitter{}
	var received Event
	e.Subscribe(func(evt Event) {
		received = evt
	})

	e.Emit(Event{Type: EventSetupStarted})

	if received.Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
}

func TestEmitter_MultipleListeners(t *testing.T) {
	e := &Emitter{}
	const n = 5
	count := 0
	for range n {
		e.Subscribe(func(_ Event) { count++ })
	}

	e.Emit(Event{Type: EventSetupStarted})

	if count != n {
		t.Errorf("expected %d listeners called, got %d", n, count)
	}
}

func TestEmitter_NoListeners_NoPanic(t *testing.T) {
	e := &Emitter{}
	// Must not panic with zero listeners.
	e.Emit(Event{Type: EventSetupStarted})
}
