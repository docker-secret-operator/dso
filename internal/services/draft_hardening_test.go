package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// TestStatusTransitions validates all allowed status transitions
func TestStatusTransitions(t *testing.T) {
	transitions := map[string][]string{
		"draft":       {"under_review"},
		"under_review": {"approved", "rejected"},
		"approved":    {"archived"},
		"rejected":    {"archived"},
		"archived":    {}, // Terminal state
	}

	for from, allowedTo := range transitions {
		for _, to := range allowedTo {
			if !IsValidTransition(from, to) {
				t.Errorf("Expected valid transition: %s → %s", from, to)
			}
		}

		// Test disallowed transitions
		allStatuses := []string{"draft", "under_review", "approved", "rejected", "archived"}
		for _, to := range allStatuses {
			isAllowed := false
			for _, valid := range allowedTo {
				if valid == to {
					isAllowed = true
					break
				}
			}
			if !isAllowed && from != to {
				if IsValidTransition(from, to) {
					t.Errorf("Expected invalid transition: %s → %s", from, to)
				}
			}
		}
	}

	t.Log("✓ All status transitions validated")
}

// TestGetAllowedNextStatuses validates next status lookup
func TestGetAllowedNextStatuses(t *testing.T) {
	tests := map[string][]string{
		"draft":        {"under_review"},
		"under_review": {"approved", "rejected"},
		"approved":     {"archived"},
		"rejected":     {"archived"},
		"archived":     {},
	}

	for status, expected := range tests {
		next := GetAllowedNextStatuses(status)
		if len(next) != len(expected) {
			t.Errorf("Status %s: expected %d next statuses, got %d", status, len(expected), len(next))
		}
	}

	t.Log("✓ GetAllowedNextStatuses validated")
}

// TestConfigValidationClean validates clean configuration
func TestConfigValidationClean(t *testing.T) {
	config := `{
		"mappings": [
			{"name": "database_config", "path": "/var/run/secrets/db-config"},
			{"name": "auth_token", "path": "/var/run/secrets/auth-token"}
		],
		"secrets": ["database_config", "auth_token"],
		"containers": ["app-server", "worker"]
	}`

	result := ValidateDraftConfig(config)

	if !result.Valid {
		t.Errorf("Clean config should be valid, got errors: %v", result.Errors)
	}

	if result.MappingCount != 2 {
		t.Errorf("Expected 2 mappings, got %d", result.MappingCount)
	}

	if result.SecretCount != 2 {
		t.Errorf("Expected 2 secrets, got %d", result.SecretCount)
	}

	t.Log("✓ Clean configuration validated")
}

// TestConfigValidationForbidden validates config with forbidden patterns
func TestConfigValidationForbidden(t *testing.T) {
	tests := map[string]string{
		"password_in_value": `{"secret": "password123"}`,
		"token_in_value":    `{"auth": "bearer token123"}`,
		"private_key":       `{"key": "private_key_data"}`,
		"api_secret":        `{"creds": "api_secret_value"}`,
	}

	for name, config := range tests {
		result := ValidateDraftConfig(config)
		if result.Valid {
			t.Errorf("Test %s: config should be invalid but was valid", name)
		}
		if len(result.Errors) == 0 {
			t.Errorf("Test %s: expected errors, got none", name)
		}
	}

	t.Log("✓ Forbidden patterns detected correctly")
}

// TestLoadManyDrafts tests creation of many drafts
func TestLoadManyDrafts(t *testing.T) {
	tmpfile := t.TempDir() + "/load_test.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	service := NewDraftService(provider.Drafts())
	ctx := context.Background()

	t.Logf("Creating 100 drafts for load testing...")
	start := time.Now()

	for i := 0; i < 100; i++ {
		config := fmt.Sprintf(`{"mappings": [{"id": %d, "name": "mapping_%d"}]}`, i, i)
		_, err := service.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", config)
		if err != nil {
			t.Fatalf("Failed to create draft %d: %v", i, err)
		}
	}

	duration := time.Since(start)
	opsPerSec := float64(100) / duration.Seconds()

	t.Logf("✓ Created 100 drafts in %v (%.1f ops/sec)", duration, opsPerSec)

	// Test list performance
	listStart := time.Now()
	drafts, err := service.ListDrafts(ctx, "owner-1")
	listDuration := time.Since(listStart)

	if len(drafts) != 100 {
		t.Fatalf("Expected 100 drafts, got %d", len(drafts))
	}

	t.Logf("✓ Listed 100 drafts in %v", listDuration)
}

// TestLoadVersionHistory tests version accumulation
func TestLoadVersionHistory(t *testing.T) {
	tmpfile := t.TempDir() + "/versions_test.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	service := NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create a draft
	draft, _ := service.CreateDraft(ctx, "ws-1", "owner-1", "Version Test", "", `{}`)

	t.Logf("Creating 50 versions for single draft...")
	start := time.Now()

	for i := 1; i <= 50; i++ {
		config := fmt.Sprintf(`{"version": %d}`, i)
		_, err := service.SaveVersion(ctx, draft.ID, config)
		if err != nil {
			t.Fatalf("Failed to save version %d: %v", i, err)
		}
	}

	duration := time.Since(start)

	// Retrieve versions
	versions, err := service.GetDraftVersions(ctx, draft.ID)
	if err != nil {
		t.Fatalf("Failed to get versions: %v", err)
	}

	if len(versions) < 50 {
		t.Fatalf("Expected 50+ versions, got %d", len(versions))
	}

	t.Logf("✓ Created and retrieved 50 versions in %v", duration)
}

// TestAuditEventGeneration verifies audit events are created
func TestAuditEventGeneration(t *testing.T) {
	tmpfile := t.TempDir() + "/audit_test.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	auditService := NewAuditService(provider.Audit())
	ctx := context.Background()

	// Log an event
	auditService.LogEvent(ctx, "user-1", "User One", "draft.created", "draft", "draft-1", "draft")

	// Query audit events
	events, err := auditService.GetEventsByResource(ctx, "draft-1")
	if err != nil {
		t.Fatalf("Failed to query audit events: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 audit event, got %d", len(events))
	}

	if events[0].Action != "draft.created" {
		t.Fatalf("Expected action 'draft.created', got %q", events[0].Action)
	}

	t.Log("✓ Audit event generation verified")
}

// BenchmarkDraftCreation benchmarks draft creation at scale
func BenchmarkDraftCreation(b *testing.B) {
	tmpfile := b.TempDir() + "/bench.db"
	provider, _ := sqlite.NewSQLiteProvider(tmpfile)
	defer provider.Close(context.Background())

	service := NewDraftService(provider.Drafts())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{}`)
	}
}

// BenchmarkListDrafts benchmarks listing drafts
func BenchmarkListDrafts(b *testing.B) {
	tmpfile := b.TempDir() + "/bench_list.db"
	provider, _ := sqlite.NewSQLiteProvider(tmpfile)
	defer provider.Close(context.Background())

	service := NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create 100 drafts first
	for i := 0; i < 100; i++ {
		service.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{}`)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.ListDrafts(ctx, "owner-1")
	}
}

// BenchmarkGetVersions benchmarks version retrieval
func BenchmarkGetVersions(b *testing.B) {
	tmpfile := b.TempDir() + "/bench_versions.db"
	provider, _ := sqlite.NewSQLiteProvider(tmpfile)
	defer provider.Close(context.Background())

	service := NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create draft with 50 versions
	draft, _ := service.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	for i := 1; i <= 50; i++ {
		service.SaveVersion(ctx, draft.ID, fmt.Sprintf(`{"v": %d}`, i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.GetDraftVersions(ctx, draft.ID)
	}
}
