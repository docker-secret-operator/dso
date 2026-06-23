package api

import (
	"testing"
	"time"
)

// ---- sortAuditEvents ----

func TestSortAuditEvents_OrdersChronologically(t *testing.T) {
	now := time.Now()
	events := []*AuditEventResponse{
		{ID: "c", Timestamp: now.Add(2 * time.Second)},
		{ID: "a", Timestamp: now},
		{ID: "b", Timestamp: now.Add(time.Second)},
	}
	sortAuditEvents(events)
	if events[0].ID != "a" || events[1].ID != "b" || events[2].ID != "c" {
		t.Errorf("unexpected order after sort: %v %v %v", events[0].ID, events[1].ID, events[2].ID)
	}
}

func TestSortAuditEvents_EmptySlice(t *testing.T) {
	sortAuditEvents(nil)
	sortAuditEvents([]*AuditEventResponse{})
}

// ---- parseLimitOffset ----

func TestParseLimitOffset_Defaults(t *testing.T) {
	l, o := parseLimitOffset("", "", 50, 1000)
	if l != 50 || o != 0 {
		t.Errorf("expected (50, 0) got (%d, %d)", l, o)
	}
}

func TestParseLimitOffset_Clamp(t *testing.T) {
	l, _ := parseLimitOffset("9999", "0", 50, 1000)
	if l != 50 {
		t.Errorf("expected clamped to default 50, got %d", l)
	}
}

func TestParseLimitOffset_ValidValues(t *testing.T) {
	l, o := parseLimitOffset("100", "200", 50, 1000)
	if l != 100 || o != 200 {
		t.Errorf("expected (100, 200) got (%d, %d)", l, o)
	}
}

func TestParseLimitOffset_NegativeOffset(t *testing.T) {
	_, o := parseLimitOffset("10", "-5", 50, 1000)
	if o != 0 {
		t.Errorf("expected offset clamped to 0, got %d", o)
	}
}

// ---- buildAuditWhere ----

func TestBuildAuditWhere_Empty(t *testing.T) {
	where, args := buildAuditWhere("", "", "", "", "", "", "", "", "", "")
	if where != "" {
		t.Errorf("expected empty where clause, got %q", where)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %d", len(args))
	}
}

func TestBuildAuditWhere_CorrelationOnly(t *testing.T) {
	where, args := buildAuditWhere("corr-123", "", "", "", "", "", "", "", "", "")
	if where == "" {
		t.Error("expected non-empty where clause")
	}
	if len(args) != 1 || args[0] != "corr-123" {
		t.Errorf("expected args=[corr-123], got %v", args)
	}
}

func TestBuildAuditWhere_AllFilters(t *testing.T) {
	// start/end must be valid RFC3339 so they are parsed to time.Time and included as args.
	where, args := buildAuditWhere("corr", "exec-1", "action.test", "alice", "user-1", "secret",
		"res-1", "secret", "2024-01-01T00:00:00Z", "2024-12-31T23:59:59Z")
	if where == "" {
		t.Error("expected where clause with all filters")
	}
	if len(args) == 0 {
		t.Errorf("expected args, got none: %v", args)
	}
}

func TestBuildAuditWhere_InvalidTimesIgnored(t *testing.T) {
	// Non-RFC3339 time strings must be silently ignored — no clause generated, no arg added.
	_, args := buildAuditWhere("", "", "", "", "", "", "", "", "2024-01-01", "2024-12-31")
	if len(args) != 0 {
		t.Errorf("expected 0 args for invalid time strings, got %d: %v", len(args), args)
	}
}

// ---- actionToStep ----

func TestActionToStep_KnownActions(t *testing.T) {
	cases := map[string]string{
		"execution.queued":    "queued",
		"execution.started":   "started",
		"execution.completed": "completed",
		"execution.failed":    "failed",
		"execution.cancelled": "cancelled",
		"execution.recovered": "recovered",
		"execution.timeout":   "timed_out",
		"execution.dlq_retry": "dlq",
	}
	for action, want := range cases {
		got := actionToStep(action)
		if got != want {
			t.Errorf("actionToStep(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestActionToStep_Unknown(t *testing.T) {
	got := actionToStep("user.something.custom")
	if got != "user.something.custom" {
		t.Errorf("expected passthrough for unknown action, got %q", got)
	}
}
