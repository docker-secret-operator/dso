package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// setupTestServices creates test services for API testing
func setupTestServices(t *testing.T) (*services.DraftService, *services.AuditService, func()) {
	tmpfile := t.TempDir() + "/test.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	draftService := services.NewDraftService(provider.Drafts())
	auditService := services.NewAuditService(provider.Audit())

	cleanup := func() {
		provider.Close(context.Background())
	}

	return draftService, auditService, cleanup
}

// TestCreateDraft tests POST /api/drafts
func TestCreateDraft(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)

	req := CreateDraftRequest{
		WorkspaceID: "ws-1",
		Title:       "Test Draft",
		Description: "A test draft",
		Config:      `{"mappings": []}`,
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/drafts", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleCreateDraft(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var resp DraftResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Title != "Test Draft" {
		t.Fatalf("expected title 'Test Draft', got %q", resp.Title)
	}

	if resp.Status != "draft" {
		t.Fatalf("expected status 'draft', got %q", resp.Status)
	}

	if resp.VersionNumber != 1 {
		t.Fatalf("expected version 1, got %d", resp.VersionNumber)
	}

	t.Logf("✓ Draft created: %s", resp.ID)
}

// TestCreateDraftInvalidConfig tests validation of config JSON
func TestCreateDraftInvalidConfig(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)

	req := CreateDraftRequest{
		WorkspaceID: "ws-1",
		Title:       "Test",
		Config:      `{invalid json}`,
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/drafts", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleCreateDraft(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	t.Log("✓ Invalid config rejected")
}

// TestCreateDraftMissingFields tests validation of required fields
func TestCreateDraftMissingFields(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)

	req := CreateDraftRequest{
		Title: "Test",
		// Missing workspace_id and config
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/drafts", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleCreateDraft(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	t.Log("✓ Missing fields rejected")
}

// TestGetDraft tests GET /api/drafts/{id}
func TestGetDraft(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)
	ctx := context.Background()

	// Create a draft first
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "user-1", "Test", "", `{}`)

	r := httptest.NewRequest("GET", "/api/drafts/"+draft.ID, nil)
	w := httptest.NewRecorder()

	handler.HandleGetDraft(w, r, resp.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp DraftResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.ID != draft.ID {
		t.Fatalf("expected ID %s, got %s", draft.ID, resp.ID)
	}

	t.Log("✓ Draft retrieved")
}

// TestGetDraftNotFound tests 404 handling
func TestGetDraftNotFound(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)

	r := httptest.NewRequest("GET", "/api/drafts/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleGetDraft(w, r, resp.ID)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	t.Log("✓ Not found handled correctly")
}

// TestListDrafts tests GET /api/drafts
func TestListDrafts(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)
	ctx := context.Background()

	// Create multiple drafts
	for i := 1; i <= 3; i++ {
		draftService.CreateDraft(ctx, "ws-1", "user-1", "Draft "+string(rune(48+i)), "", "{}")
	}

	r := httptest.NewRequest("GET", "/api/drafts", nil)
	w := httptest.NewRecorder()

	handler.HandleListDrafts(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp DraftListResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Total != 3 {
		t.Fatalf("expected 3 drafts, got %d", resp.Total)
	}

	t.Logf("✓ Listed %d drafts", resp.Total)
}

// TestUpdateDraft tests PUT /api/drafts/{id}
func TestUpdateDraft(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)
	ctx := context.Background()

	// Create a draft
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "user-1", "Original", "", `{}`)

	// Update it
	req := UpdateDraftRequest{
		Title: "Updated",
		Config: `{"new": "config"}`,
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("PUT", "/api/drafts/"+draft.ID, bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleUpdateDraft(w, r, resp.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp DraftResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Title != "Updated" {
		t.Fatalf("expected title 'Updated', got %q", resp.Title)
	}

	if resp.VersionNumber != 2 {
		t.Fatalf("expected version 2, got %d", resp.VersionNumber)
	}

	t.Log("✓ Draft updated with version increment")
}

// TestDeleteDraft tests DELETE /api/drafts/{id}
func TestDeleteDraft(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)
	ctx := context.Background()

	// Create a draft
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "user-1", "Test", "", `{}`)

	// Delete it
	r := httptest.NewRequest("DELETE", "/api/drafts/"+draft.ID, nil)
	w := httptest.NewRecorder()

	handler.HandleDeleteDraft(w, r, resp.ID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify it's archived
	retrieved, _ := draftService.GetDraft(ctx, draft.ID)
	if retrieved.Status != "archived" {
		t.Fatalf("expected status 'archived', got %q", retrieved.Status)
	}

	t.Log("✓ Draft deleted (soft delete to archived)")
}

// TestGetDraftVersions tests GET /api/drafts/{id}/versions
func TestGetDraftVersions(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)
	ctx := context.Background()

	// Create draft
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "user-1", "Test", "", `{"v":1}`)

	// Create versions
	draftService.SaveVersion(ctx, draft.ID, `{"v":1}`)
	draftService.UpdateDraft(ctx, draft.ID, "", "", `{"v":2}`)
	draftService.SaveVersion(ctx, draft.ID, `{"v":2}`)

	// Get versions
	r := httptest.NewRequest("GET", "/api/drafts/"+draft.ID+"/versions", nil)
	w := httptest.NewRecorder()

	handler.HandleGetDraftVersions(w, r, resp.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp DraftVersionListResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Total < 2 {
		t.Fatalf("expected at least 2 versions, got %d", resp.Total)
	}

	t.Logf("✓ Retrieved %d versions", resp.Total)
}

// TestPersistenceDisabled tests that endpoints return 503 when persistence is disabled
func TestPersistenceDisabled(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	// Create handler with persistence disabled
	handler := NewDraftHandler(draftService, auditService, "user-1", false)

	req := CreateDraftRequest{
		WorkspaceID: "ws-1",
		Title:       "Test",
		Config:      `{}`,
	}

	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/drafts", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleCreateDraft(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Error != "persistence_disabled" {
		t.Fatalf("expected error 'persistence_disabled', got %q", resp.Error)
	}

	t.Log("✓ Persistence disabled returns 503")
}

// TestDraftLifecycle tests complete draft lifecycle
func TestDraftLifecycle(t *testing.T) {
	draftService, auditService, cleanup := setupTestServices(t)
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)
	ctx := context.Background()

	// 1. Create
	createReq := CreateDraftRequest{
		WorkspaceID: "ws-1",
		Title:       "Lifecycle Test",
		Description: "Testing full lifecycle",
		Config:      `{"initial": true}`,
	}
	createBody, _ := json.Marshal(createReq)
	createR := httptest.NewRequest("POST", "/api/drafts", bytes.NewReader(createBody))
	createW := httptest.NewRecorder()
	handler.HandleCreateDraft(createW, createR)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", createW.Code)
	}

	var draft DraftResponse
	json.NewDecoder(createW.Body).Decode(&draft)
	draftID := draft.ID

	t.Logf("✓ Step 1: Created draft %s", draftID)

	// 2. Retrieve
	getR := httptest.NewRequest("GET", "/api/drafts/"+draftID, nil)
	getW := httptest.NewRecorder()
	handler.HandleGetDraft(getW, getR)

	if getW.Code != http.StatusOK {
		t.Fatalf("get failed: %d", getW.Code)
	}

	t.Log("✓ Step 2: Retrieved draft")

	// 3. Update
	updateReq := UpdateDraftRequest{
		Title: "Updated Title",
		Config: `{"updated": true}`,
	}
	updateBody, _ := json.Marshal(updateReq)
	updateR := httptest.NewRequest("PUT", "/api/drafts/"+draftID, bytes.NewReader(updateBody))
	updateW := httptest.NewRecorder()
	handler.HandleUpdateDraft(updateW, updateR)

	if updateW.Code != http.StatusOK {
		t.Fatalf("update failed: %d", updateW.Code)
	}

	var updated DraftResponse
	json.NewDecoder(updateW.Body).Decode(&updated)

	if updated.Title != "Updated Title" {
		t.Fatalf("title not updated")
	}

	t.Log("✓ Step 3: Updated draft")

	// 4. List
	listR := httptest.NewRequest("GET", "/api/drafts", nil)
	listW := httptest.NewRecorder()
	handler.HandleListDrafts(listW, listR)

	if listW.Code != http.StatusOK {
		t.Fatalf("list failed: %d", listW.Code)
	}

	var list DraftListResponse
	json.NewDecoder(listW.Body).Decode(&list)

	if list.Total == 0 {
		t.Fatal("draft not in list")
	}

	t.Logf("✓ Step 4: Listed %d drafts", list.Total)

	// 5. Delete
	deleteR := httptest.NewRequest("DELETE", "/api/drafts/"+draftID, nil)
	deleteW := httptest.NewRecorder()
	handler.HandleDeleteDraft(deleteW, deleteR)

	if deleteW.Code != http.StatusNoContent {
		t.Fatalf("delete failed: %d", deleteW.Code)
	}

	t.Log("✓ Step 5: Deleted draft")

	// 6. Verify deletion (archived status)
	finalDraft, _ := draftService.GetDraft(ctx, draftID)
	if finalDraft.Status != "archived" {
		t.Fatalf("expected status 'archived', got %q", finalDraft.Status)
	}

	t.Log("✓ Step 6: Verified archived status")

	t.Log("✓ Complete draft lifecycle verified")
}

// BenchmarkCreateDraft benchmarks draft creation
func BenchmarkCreateDraft(b *testing.B) {
	draftService, auditService, cleanup := setupTestServices(&testing.T{})
	defer cleanup()

	handler := NewDraftHandler(draftService, auditService, "user-1", true)

	req := CreateDraftRequest{
		WorkspaceID: "ws-1",
		Title:       "Bench Draft",
		Config:      `{}`,
	}

	body, _ := json.Marshal(req)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest("POST", "/api/drafts", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler.HandleCreateDraft(w, r)
	}
}
