package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── journey handler tests ─────────────────────────────────────────────────

func TestGetJourney_StepsOrdered(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	execID := "exec-ordered-1"
	corr := "corr-j1"

	insertAuditEvent(t, db, "j1", corr, "uid-1", "alice", "execution.queued", "execution", execID, "execution", "success", "info", now)
	insertAuditEvent(t, db, "j2", corr, "uid-1", "alice", "execution.completed", "execution", execID, "execution", "success", "info", now.Add(5*time.Second))
	insertExecAuditEvent(t, db, "je1", execID, corr, "execution.started", "success", now.Add(time.Second))

	h := &ExecutionHandler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp ExecutionJourneyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ExecutionID != execID {
		t.Errorf("wrong execution_id: %s", resp.ExecutionID)
	}
	if resp.TotalSteps != 3 {
		t.Errorf("expected 3 steps, got %d", resp.TotalSteps)
	}
	for i := 1; i < len(resp.Steps); i++ {
		if resp.Steps[i].Timestamp.Before(resp.Steps[i-1].Timestamp) {
			t.Errorf("steps not ordered at index %d", i)
		}
	}
	if resp.Steps[0].Step != "queued" {
		t.Errorf("expected first step=queued, got %s", resp.Steps[0].Step)
	}
	if resp.Steps[len(resp.Steps)-1].Step != "completed" {
		t.Errorf("expected last step=completed, got %s", resp.Steps[len(resp.Steps)-1].Step)
	}
}

func TestGetJourney_DurationCalculated(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	execID := "exec-dur-2"
	corr := "corr-j2"

	insertAuditEvent(t, db, "d1", corr, "uid-1", "alice", "execution.queued", "execution", execID, "execution", "success", "info", now)
	insertAuditEvent(t, db, "d2", corr, "uid-1", "alice", "execution.completed", "execution", execID, "execution", "success", "info", now.Add(10*time.Second))

	h := &ExecutionHandler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	var resp ExecutionJourneyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.DurationMs != 10000 {
		t.Errorf("expected duration=10000ms, got %d", resp.DurationMs)
	}
}

func TestGetJourney_CorrelationIDFromSteps(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	execID := "exec-corr-3"
	corr := "corr-j3"

	insertAuditEvent(t, db, "c1", corr, "uid-1", "alice", "execution.started", "execution", execID, "execution", "success", "info", now)

	h := &ExecutionHandler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	var resp ExecutionJourneyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.CorrelationID != corr {
		t.Errorf("expected correlation_id=%s, got %s", corr, resp.CorrelationID)
	}
}

func TestGetJourney_ActorAttributionPreserved(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	execID := "exec-actor-4"
	corr := "corr-j4"

	insertAuditEvent(t, db, "a1", corr, "uid-carol", "carol", "execution.queued", "execution", execID, "execution", "success", "info", now)

	h := &ExecutionHandler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	var resp ExecutionJourneyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(resp.Steps))
	}
	if resp.Steps[0].Actor != "carol" || resp.Steps[0].ActorID != "uid-carol" {
		t.Errorf("actor attribution lost: actor=%s actor_id=%s", resp.Steps[0].Actor, resp.Steps[0].ActorID)
	}
}

func TestGetJourney_SystemStepsHaveSystemActor(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	execID := "exec-sys-5"
	corr := "corr-j5"

	insertExecAuditEvent(t, db, "s1", execID, corr, "execution.started", "success", now)

	h := &ExecutionHandler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	var resp ExecutionJourneyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Steps) != 1 {
		t.Fatalf("expected 1 system step, got %d", len(resp.Steps))
	}
	if resp.Steps[0].Actor != "system" {
		t.Errorf("expected actor=system for exec_audit_event, got %s", resp.Steps[0].Actor)
	}
}

func TestGetJourney_AllLifecycleStepsMapped(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	execID := "exec-full-6"
	corr := "corr-j6"

	type lc struct {
		id, action, want string
		offset           time.Duration
	}
	lifecycle := []lc{
		{"lc1", "execution.queued", "queued", 0},
		{"lc2", "execution.started", "started", time.Second},
		{"lc3", "execution.paused", "paused", 2 * time.Second},
		{"lc4", "execution.resumed", "resumed", 3 * time.Second},
		{"lc5", "execution.cancelled", "cancelled", 4 * time.Second},
	}
	for _, step := range lifecycle {
		insertExecAuditEvent(t, db, step.id, execID, corr, step.action, "success", now.Add(step.offset))
	}
	insertAuditEvent(t, db, "lc6", corr, "u1", "sys", "execution.dlq_retry", "execution", execID, "execution", "success", "info", now.Add(5*time.Second))
	insertAuditEvent(t, db, "lc7", corr, "u1", "sys", "execution.failed", "execution", execID, "execution", "failure", "error", now.Add(6*time.Second))

	h := &ExecutionHandler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	var resp ExecutionJourneyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.TotalSteps != 7 {
		t.Errorf("expected 7 total steps, got %d", resp.TotalSteps)
	}

	stepMap := map[string]bool{}
	for _, s := range resp.Steps {
		stepMap[s.Step] = true
	}
	for _, step := range lifecycle {
		if !stepMap[step.want] {
			t.Errorf("missing lifecycle step %q", step.want)
		}
	}
	if !stepMap["dlq"] {
		t.Error("missing DLQ step")
	}
	if !stepMap["failed"] {
		t.Error("missing failed step")
	}
}

func TestGetJourney_EmptyExecution(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	h := &ExecutionHandler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/exec-nonexistent/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty execution, got %d", rr.Code)
	}
	var resp ExecutionJourneyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.TotalSteps != 0 {
		t.Errorf("expected 0 steps for non-existent execution, got %d", resp.TotalSteps)
	}
	if resp.DurationMs != 0 {
		t.Errorf("expected 0 duration for empty, got %d", resp.DurationMs)
	}
}

func TestGetJourney_NilDB_ReturnsEmpty(t *testing.T) {
	h := &ExecutionHandler{db: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/executions/exec-nildb/journey", nil)
	rr := httptest.NewRecorder()
	h.getJourney(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even with nil db, got %d", rr.Code)
	}
	var resp ExecutionJourneyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.TotalSteps != 0 {
		t.Errorf("expected 0 steps with nil db, got %d", resp.TotalSteps)
	}
}

// ── actionToStep ─────────────────────────────────────────────────────────

func TestActionToStep_RecoveredAndTimeout(t *testing.T) {
	cases := map[string]string{
		"execution.recovered": "recovered",
		"execution.timeout":   "timed_out",
		"execution.dlq_retry": "dlq",
	}
	for action, want := range cases {
		if got := actionToStep(action); got != want {
			t.Errorf("actionToStep(%q) = %q, want %q", action, got, want)
		}
	}
}
