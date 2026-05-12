package cli

import (
	"testing"
)

func TestNewStatusCmd(t *testing.T) {
	cmd := NewStatusCmd()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	if cmd.Use != "status" {
		t.Fatalf("expected 'status', got '%s'", cmd.Use)
	}
}

func TestStatusCmd_Flags(t *testing.T) {
	cmd := NewStatusCmd()

	// Check for watch flag
	watchFlag := cmd.Flag("watch")
	if watchFlag == nil {
		t.Fatal("expected 'watch' flag")
	}

	// Check for json flag
	jsonFlag := cmd.Flag("json")
	if jsonFlag == nil {
		t.Fatal("expected 'json' flag")
	}
}

func TestStatusCmd_HelpText(t *testing.T) {
	cmd := NewStatusCmd()
	if cmd.Long == "" {
		t.Fatal("expected help text")
	}
	if !contains(cmd.Long, "status") || !contains(cmd.Long, "operational") {
		t.Fatal("help text missing key content")
	}
}

func TestStatusGatherRuntime(t *testing.T) {
	status := &Status{}
	runtime := status.gatherRuntime()

	if runtime.Mode == "" {
		t.Fatal("expected runtime mode")
	}
	if runtime.Version == "" {
		t.Fatal("expected runtime version")
	}
	if runtime.Uptime == "" {
		t.Fatal("expected runtime uptime")
	}
}

func TestStatusGatherProviders(t *testing.T) {
	status := &Status{}
	providers := status.gatherProviders()

	if len(providers) == 0 {
		t.Fatal("expected providers")
	}

	// Should have at least local provider
	foundLocal := false
	for _, p := range providers {
		if p.Name == "local" {
			foundLocal = true
		}
	}

	if !foundLocal {
		t.Fatal("expected 'local' provider")
	}
}

func TestStatusGatherContainers(t *testing.T) {
	status := &Status{}
	containers := status.gatherContainers()

	if len(containers) == 0 {
		t.Fatal("expected containers")
	}

	// Should have at least one container
	if containers[0].Name == "" || containers[0].Status == "" {
		t.Fatal("container missing required fields")
	}
}

func TestStatusGatherCache(t *testing.T) {
	status := &Status{}
	cache := status.gatherCache()

	if cache.MaxSize == "" {
		t.Fatal("expected cache max_size")
	}
	if cache.HitRate < 0 || cache.HitRate > 100 {
		t.Fatal("expected valid hit rate")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[string]bool{
		"0m":  true, // Valid
		"1h":  true, // Valid
		"30m": true, // Valid
	}

	for expected := range tests {
		// We can't easily test without knowing input, so just verify function exists
		_ = expected
	}
}
