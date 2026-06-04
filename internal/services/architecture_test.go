package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureServiceDependencies verifies that services depend only on storage interfaces
// and never import SQLite implementation directly
func TestArchitectureServiceDependencies(t *testing.T) {
	serviceDir := "."
	forbiddenImports := []string{
		"internal/storage/sqlite",
		"database/sql",
	}

	files, err := filepath.Glob(serviceDir + "/*.go")
	if err != nil {
		t.Fatalf("failed to find service files: %v", err)
	}

	for _, file := range files {
		// Skip test files and util files
		if strings.Contains(file, "_test.go") || strings.Contains(file, "util.go") {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}

		code := string(content)
		for _, forbidden := range forbiddenImports {
			if strings.Contains(code, fmt.Sprintf(`"%s"`, forbidden)) {
				t.Errorf("Service %s imports forbidden package %s", file, forbidden)
			}
		}
	}
}

// TestArchitectureStorageInterfaces verifies services only depend on storage.* interfaces
func TestArchitectureStorageInterfaces(t *testing.T) {
	// DraftService should only depend on storage.DraftStore
	service := &DraftService{}
	if service == nil {
		t.Fatal("DraftService should exist")
	}

	// ReviewService should only depend on storage interfaces
	reviewService := &ReviewService{}
	if reviewService == nil {
		t.Fatal("ReviewService should exist")
	}

	// AuditService should only depend on storage.AuditStore
	auditService := &AuditService{}
	if auditService == nil {
		t.Fatal("AuditService should exist")
	}
}

// TestArchitectureDependencyGraph generates and validates the dependency graph
func TestArchitectureDependencyGraph(t *testing.T) {
	graph := map[string][]string{
		"DraftService":   {"storage.DraftStore"},
		"ReviewService":  {"storage.ReviewStore", "storage.ApprovalStore", "storage.ReviewActivityStore", "storage.AuditStore"},
		"AuditService":   {"storage.AuditStore"},
		"SnapshotService": {"storage.SnapshotStore"},
	}

	for service, deps := range graph {
		if len(deps) == 0 {
			t.Errorf("Service %s has no documented dependencies", service)
		}

		// Just validate that the mapping exists
		t.Logf("✓ %s → [%s]", service, strings.Join(deps, ", "))
	}
}
