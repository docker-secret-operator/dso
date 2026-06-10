package api

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ── test schema ──────────────────────────────────────────────────────────────

const testSchema = `
CREATE TABLE IF NOT EXISTS audit_events (
	id TEXT PRIMARY KEY,
	timestamp TIMESTAMP NOT NULL,
	actor_id TEXT NOT NULL,
	actor_name TEXT NOT NULL,
	actor_email TEXT,
	action TEXT NOT NULL,
	resource TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	resource_type TEXT NOT NULL,
	status TEXT NOT NULL,
	result_code TEXT,
	result_message TEXT,
	old_value TEXT,
	new_value TEXT,
	delta TEXT,
	correlation_id TEXT NOT NULL,
	request_id TEXT NOT NULL,
	ip_address TEXT,
	user_agent TEXT,
	severity TEXT NOT NULL,
	retention_until TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ae_ts        ON audit_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_ae_actor_id  ON audit_events(actor_id);
CREATE INDEX IF NOT EXISTS idx_ae_corr_id   ON audit_events(correlation_id);
CREATE INDEX IF NOT EXISTS idx_ae_res_id    ON audit_events(resource_id);

CREATE TABLE IF NOT EXISTS execution_audit_events (
	id TEXT PRIMARY KEY,
	execution_id TEXT NOT NULL,
	correlation_id TEXT NOT NULL,
	action TEXT NOT NULL,
	status TEXT NOT NULL,
	details TEXT,
	resource_id TEXT,
	resource_type TEXT,
	timestamp DATETIME NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reviews (
	id TEXT PRIMARY KEY,
	draft_id TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	created_by TEXT NOT NULL,
	modified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	status TEXT NOT NULL,
	checklist TEXT NOT NULL DEFAULT '{}',
	risk_assessment TEXT NOT NULL DEFAULT '{}',
	required_approvals INTEGER NOT NULL DEFAULT 1,
	title TEXT NOT NULL,
	description TEXT
);

CREATE TABLE IF NOT EXISTS approvals (
	id TEXT PRIMARY KEY,
	review_id TEXT NOT NULL,
	reviewer_id TEXT NOT NULL,
	reviewer_name TEXT NOT NULL,
	decision TEXT NOT NULL,
	comments TEXT,
	rejection_reason TEXT,
	approval_sequence INTEGER NOT NULL,
	is_required INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	decided_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS review_activities (
	id TEXT PRIMARY KEY,
	review_id TEXT NOT NULL,
	type TEXT NOT NULL,
	actor_id TEXT NOT NULL,
	description TEXT NOT NULL,
	metadata TEXT,
	timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// newTestDB creates an in-memory SQLite DB with the audit schema and sample rows.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if _, err := db.Exec(testSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func insertAuditEvent(t *testing.T, db *sql.DB, id, correlationID, actorID, actorName, action, resource, resourceID, resourceType, status, severity string, ts time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO audit_events
		(id, timestamp, actor_id, actor_name, actor_email, action, resource, resource_id, resource_type,
		 status, result_message, correlation_id, request_id, ip_address, severity)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, ts, actorID, actorName, actorName+"@example.com", action, resource, resourceID, resourceType,
		status, "ok", correlationID, "req-"+id, "10.0.0.1", severity)
	if err != nil {
		t.Fatalf("insert audit event %s: %v", id, err)
	}
}

func insertExecAuditEvent(t *testing.T, db *sql.DB, id, executionID, correlationID, action, status string, ts time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO execution_audit_events
		(id, execution_id, correlation_id, action, status, details, resource_type, timestamp)
		VALUES (?,?,?,?,?,?,?,?)`,
		id, executionID, correlationID, action, status, "detail", "execution", ts)
	if err != nil {
		t.Fatalf("insert exec audit event %s: %v", id, err)
	}
}

// ── GET /api/audit ────────────────────────────────────────────────────────────

func TestHandleList_ReturnsNewestFirst(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	insertAuditEvent(t, db, "e1", "corr-1", "user-1", "alice", "secret.rotate", "secret", "s1", "secret", "success", "info", now.Add(-2*time.Second))
	insertAuditEvent(t, db, "e2", "corr-1", "user-1", "alice", "secret.read", "secret", "s1", "secret", "success", "info", now.Add(-1*time.Second))
	insertAuditEvent(t, db, "e3", "corr-1", "user-1", "alice", "secret.delete", "secret", "s1", "secret", "failure", "warning", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp AuditExplorerResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 3 {
		t.Errorf("expected total=3, got %d", resp.Total)
	}
	if resp.Count != 3 {
		t.Errorf("expected count=3, got %d", resp.Count)
	}
	// Newest first
	if resp.Events[0].ID != "e3" {
		t.Errorf("expected first event e3 (newest), got %s", resp.Events[0].ID)
	}
	if resp.Events[2].ID != "e1" {
		t.Errorf("expected last event e1 (oldest), got %s", resp.Events[2].ID)
	}
}

func TestHandleList_FilterByActor(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-1", "user-1", "alice", "secret.rotate", "secret", "s1", "secret", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-2", "user-2", "bob", "secret.read", "secret", "s2", "secret", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit?actor=alice", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 || resp.Events[0].Actor != "alice" {
		t.Errorf("expected 1 alice event, got count=%d", resp.Count)
	}
}

func TestHandleList_FilterByActorID(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-1", "uid-abc", "alice", "login", "auth", "uid-abc", "user", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-2", "uid-xyz", "bob", "login", "auth", "uid-xyz", "user", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit?actor_id=uid-abc", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 || resp.Events[0].ActorID != "uid-abc" {
		t.Errorf("expected 1 event for uid-abc, got count=%d", resp.Count)
	}
}

func TestHandleList_FilterByAction(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-1", "u1", "alice", "secret.rotate", "secret", "s1", "secret", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-2", "u1", "alice", "secret.read", "secret", "s1", "secret", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit?action=secret.rotate", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 || resp.Events[0].Action != "secret.rotate" {
		t.Errorf("expected 1 rotate event, got count=%d", resp.Count)
	}
}

func TestHandleList_FilterByResource(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-1", "u1", "alice", "secret.rotate", "secret", "s1", "secret", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-2", "u1", "alice", "review.submit", "review", "r1", "review", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit?resource=review", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 || resp.Events[0].Resource != "review" {
		t.Errorf("expected 1 review event, got count=%d", resp.Count)
	}
}

func TestHandleList_FilterByCorrelationID(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-A", "u1", "alice", "action.a", "res", "r1", "type", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-B", "u1", "alice", "action.b", "res", "r2", "type", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit?correlation_id=corr-A", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 || resp.Events[0].CorrelationID != "corr-A" {
		t.Errorf("expected 1 event for corr-A, got count=%d", resp.Count)
	}
}

func TestHandleList_FilterByExecutionID(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	// Execution events are stored with resource_type='execution' and resource_id=executionID
	insertAuditEvent(t, db, "e1", "corr-1", "u1", "alice", "execution.started", "execution", "exec-42", "execution", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-2", "u1", "alice", "secret.rotate", "secret", "s1", "secret", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit?execution_id=exec-42", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 {
		t.Errorf("expected 1 execution event, got count=%d", resp.Count)
	}
	// ExecutionID must be derived from resource_id
	if resp.Events[0].ExecutionID != "exec-42" {
		t.Errorf("ExecutionID not derived: got %q", resp.Events[0].ExecutionID)
	}
}

func TestHandleList_FilterByTimeRange(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	base := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	insertAuditEvent(t, db, "e1", "c1", "u1", "a", "action.a", "res", "r1", "t", "success", "info", base.Add(-1*time.Hour))
	insertAuditEvent(t, db, "e2", "c2", "u1", "a", "action.b", "res", "r2", "t", "success", "info", base)
	insertAuditEvent(t, db, "e3", "c3", "u1", "a", "action.c", "res", "r3", "t", "success", "info", base.Add(1*time.Hour))

	h := NewAuditExplorerHandler(db)
	// Only the middle event falls in this window
	start := base.Add(-30 * time.Minute).Format(time.RFC3339)
	end := base.Add(30 * time.Minute).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/audit?start_time="+start+"&end_time="+end, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 || resp.Events[0].ID != "e2" {
		t.Errorf("expected only e2 in range, got count=%d events=%v", resp.Count, resp.Events)
	}
}

func TestHandleList_Pagination(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		insertAuditEvent(t, db, "e"+string(rune('0'+i)), "c1", "u1", "alice", "act", "res", "r1", "t",
			"success", "info", now.Add(time.Duration(i)*time.Second))
	}

	h := NewAuditExplorerHandler(db)

	// Page 1: limit=2, offset=0
	req := httptest.NewRequest(http.MethodGet, "/api/audit?limit=2&offset=0", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var page1 AuditExplorerResponse
	json.NewDecoder(rr.Body).Decode(&page1)
	if page1.Total != 5 {
		t.Errorf("expected total=5, got %d", page1.Total)
	}
	if page1.Count != 2 {
		t.Errorf("expected count=2 for page1, got %d", page1.Count)
	}

	// Page 2: limit=2, offset=2
	req2 := httptest.NewRequest(http.MethodGet, "/api/audit?limit=2&offset=2", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)

	var page2 AuditExplorerResponse
	json.NewDecoder(rr2.Body).Decode(&page2)
	if page2.Count != 2 {
		t.Errorf("expected count=2 for page2, got %d", page2.Count)
	}

	// Ensure no overlap between pages
	ids1 := map[string]bool{}
	for _, e := range page1.Events {
		ids1[e.ID] = true
	}
	for _, e := range page2.Events {
		if ids1[e.ID] {
			t.Errorf("duplicate event %s across pages", e.ID)
		}
	}
}

// ── GET /api/audit/correlation/{id} ─────────────────────────────────────────

func TestHandleCorrelationChain_MergesAllEventTypes(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	corr := "corr-test-99"

	// Audit event
	insertAuditEvent(t, db, "ae1", corr, "u1", "alice", "execution.queued", "execution", "exec-1", "execution", "success", "info", now)
	// Execution audit event
	insertExecAuditEvent(t, db, "ee1", "exec-1", corr, "execution.started", "success", now.Add(time.Second))

	// Review audit event (links to review r1)
	insertAuditEvent(t, db, "ae2", corr, "u1", "alice", "review.submitted", "review", "r1", "review", "success", "info", now.Add(2*time.Second))

	// Insert review + review_activity + approval for that review
	db.Exec(`INSERT INTO reviews (id, draft_id, created_by, status, checklist, risk_assessment, title) VALUES ('r1','d1','u1','under_review','{}','{}','Test')`)
	db.Exec(`INSERT INTO review_activities (id, review_id, type, actor_id, description, timestamp) VALUES ('ra1','r1','comment','u2','LGTM',?)`, now.Add(3*time.Second))
	db.Exec(`INSERT INTO approvals (id, review_id, reviewer_id, reviewer_name, decision, approval_sequence, created_at, decided_at) VALUES ('app1','r1','u2','bob','approved',1,?,?)`, now.Add(3*time.Second), now.Add(4*time.Second))

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/correlation/"+corr, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp CorrelationChainResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.CorrelationID != corr {
		t.Errorf("wrong correlation_id: %s", resp.CorrelationID)
	}

	// Expect: ae1, ee1, ae2, ra1, app1 = 5 events
	if resp.Count != 5 {
		t.Errorf("expected 5 events in chain, got %d: %v", resp.Count, eventIDs(resp.Events))
	}

	// Events must be in chronological order
	for i := 1; i < len(resp.Events); i++ {
		if resp.Events[i].Timestamp.Before(resp.Events[i-1].Timestamp) {
			t.Errorf("events not chronological at index %d: %v before %v",
				i, resp.Events[i].Timestamp, resp.Events[i-1].Timestamp)
		}
	}
}

func TestHandleCorrelationChain_ExecutionIDPopulated(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	corr := "corr-exec-id"
	insertExecAuditEvent(t, db, "ee1", "exec-99", corr, "execution.started", "success", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/correlation/"+corr, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp CorrelationChainResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Count != 1 {
		t.Fatalf("expected 1 exec event, got %d", resp.Count)
	}
	if resp.Events[0].ExecutionID != "exec-99" {
		t.Errorf("ExecutionID not set, got %q", resp.Events[0].ExecutionID)
	}
}

func TestHandleCorrelationChain_MissingID_Returns400(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	h := NewAuditExplorerHandler(db)
	// /api/audit/correlation/ with empty id
	req := httptest.NewRequest(http.MethodGet, "/api/audit/correlation/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing correlation id, got %d", rr.Code)
	}
}

func TestHandleCorrelationChain_NoDuplicateEvents(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	corr := "corr-dedup"
	// Same correlation ID, multiple events
	insertAuditEvent(t, db, "ae1", corr, "u1", "alice", "act.a", "res", "r1", "type", "success", "info", now)
	insertAuditEvent(t, db, "ae2", corr, "u1", "alice", "act.b", "res", "r2", "type", "success", "info", now.Add(time.Second))
	insertExecAuditEvent(t, db, "ee1", "exec-1", corr, "execution.started", "success", now.Add(2*time.Second))

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/correlation/"+corr, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp CorrelationChainResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	seen := map[string]bool{}
	for _, e := range resp.Events {
		if seen[e.ID] {
			t.Errorf("duplicate event ID %s in correlation chain", e.ID)
		}
		seen[e.ID] = true
	}
}

// ── GET /api/audit/actors/{id} ───────────────────────────────────────────────

func TestHandleActorTimeline_24h(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	// Within 24h
	insertAuditEvent(t, db, "e1", "c1", "uid-1", "alice", "login", "auth", "uid-1", "user", "success", "info", now.Add(-1*time.Hour))
	// Outside 24h (36h ago)
	insertAuditEvent(t, db, "e2", "c2", "uid-1", "alice", "login", "auth", "uid-1", "user", "success", "info", now.Add(-36*time.Hour))

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/actors/uid-1?period=24h", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp ActorTimelineResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Period != "24h" {
		t.Errorf("expected period=24h, got %s", resp.Period)
	}
	if resp.Count != 1 {
		t.Errorf("expected 1 event in 24h window, got %d", resp.Count)
	}
	if resp.ActorName != "alice" {
		t.Errorf("expected actor_name=alice, got %s", resp.ActorName)
	}
}

func TestHandleActorTimeline_7d(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "c1", "uid-2", "bob", "approval.approved", "approval", "a1", "approval", "success", "info", now.Add(-2*24*time.Hour))
	insertAuditEvent(t, db, "e2", "c2", "uid-2", "bob", "review.submitted", "review", "r1", "review", "success", "info", now.Add(-5*24*time.Hour))
	// Outside 7d
	insertAuditEvent(t, db, "e3", "c3", "uid-2", "bob", "login", "auth", "uid-2", "user", "success", "info", now.Add(-8*24*time.Hour))

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/actors/uid-2?period=7d", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp ActorTimelineResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Period != "7d" {
		t.Errorf("expected period=7d, got %s", resp.Period)
	}
	if resp.Count != 2 {
		t.Errorf("expected 2 events in 7d window, got %d", resp.Count)
	}
}

func TestHandleActorTimeline_30d(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "c1", "uid-3", "carol", "execution.started", "execution", "exec-1", "execution", "success", "info", now.Add(-20*24*time.Hour))
	insertAuditEvent(t, db, "e2", "c2", "uid-3", "carol", "password.reset", "auth", "uid-3", "user", "success", "info", now.Add(-25*24*time.Hour))
	// Outside 30d
	insertAuditEvent(t, db, "e3", "c3", "uid-3", "carol", "session.revoked", "auth", "uid-3", "user", "success", "info", now.Add(-35*24*time.Hour))

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/actors/uid-3?period=30d", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp ActorTimelineResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Period != "30d" {
		t.Errorf("expected period=30d, got %s", resp.Period)
	}
	if resp.Count != 2 {
		t.Errorf("expected 2 events in 30d window, got %d", resp.Count)
	}
}

func TestHandleActorTimeline_DefaultPeriod(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/actors/nobody", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp ActorTimelineResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Period != "24h" {
		t.Errorf("expected default period=24h, got %s", resp.Period)
	}
}

func TestHandleActorTimeline_MissingID_Returns400(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/actors/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing actor id, got %d", rr.Code)
	}
}

// ── GET /api/audit/export ────────────────────────────────────────────────────

func TestHandleExport_JSON(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-1", "u1", "alice", "secret.rotate", "secret", "s1", "secret", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-2", "u1", "alice", "secret.read", "secret", "s2", "secret", "failure", "warning", now.Add(time.Second))

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/export?format=json", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected content-type application/json, got %s", ct)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode json export: %v", err)
	}
	count, ok := body["count"].(float64)
	if !ok || int(count) != 2 {
		t.Errorf("expected count=2, got %v", body["count"])
	}
}

func TestHandleExport_CSV(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-1", "u1", "alice", "secret.rotate", "secret", "s1", "secret", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/export?format=csv", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("expected content-type text/csv, got %s", ct)
	}

	records, err := csv.NewReader(rr.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	// header + 1 data row
	if len(records) != 2 {
		t.Errorf("expected 2 CSV rows (header + 1), got %d", len(records))
	}
	header := records[0]
	expected := []string{"id", "timestamp", "actor", "actor_id", "actor_email", "action",
		"resource", "resource_id", "resource_type", "status", "severity",
		"details", "correlation_id", "execution_id", "ip_address"}
	if strings.Join(header, ",") != strings.Join(expected, ",") {
		t.Errorf("unexpected CSV header: %v", header)
	}
	if records[1][0] != "e1" {
		t.Errorf("expected row ID=e1, got %s", records[1][0])
	}
}

func TestHandleExport_FilteredByCorrelation(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "corr-X", "u1", "alice", "act.a", "res", "r1", "t", "success", "info", now)
	insertAuditEvent(t, db, "e2", "corr-Y", "u1", "alice", "act.b", "res", "r2", "t", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/export?format=json&correlation_id=corr-X", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var body map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&body)
	if int(body["count"].(float64)) != 1 {
		t.Errorf("export filter: expected count=1, got %v", body["count"])
	}
}

func TestHandleExport_ActorAttributionPreserved(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	insertAuditEvent(t, db, "e1", "c1", "uid-99", "dave", "secret.rotate", "secret", "s1", "secret", "success", "info", now)

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/export?format=json", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var body struct {
		Events []AuditEventResponse `json:"events"`
	}
	json.NewDecoder(rr.Body).Decode(&body)
	if len(body.Events) != 1 {
		t.Fatalf("expected 1 exported event, got %d", len(body.Events))
	}
	e := body.Events[0]
	if e.Actor != "dave" || e.ActorID != "uid-99" {
		t.Errorf("actor attribution lost: actor=%s actor_id=%s", e.Actor, e.ActorID)
	}
}

func TestHandleExport_DefaultsToJSON(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	h := NewAuditExplorerHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/export", nil) // no format param
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected json default, got content-type: %s", ct)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func eventIDs(events []*AuditEventResponse) []string {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}
