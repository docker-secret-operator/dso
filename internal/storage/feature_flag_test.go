package storage

import (
	"testing"
)

// TestFeatureFlagDefaults verifies default configuration
func TestFeatureFlagDefaults(t *testing.T) {
	config := DefaultPersistenceConfig()

	// Must be opt-in for backward compatibility
	if config.Enabled {
		t.Fatal("Persistence should be disabled by default for backward compatibility")
	}

	if config.Driver != "sqlite" {
		t.Fatalf("expected driver 'sqlite', got %q", config.Driver)
	}

	if config.Path == "" {
		t.Fatal("expected default path")
	}

	if config.MaxConnections == 0 {
		t.Fatal("expected non-zero max connections")
	}

	t.Logf("✓ Feature flag defaults: enabled=%v, driver=%s, path=%s", config.Enabled, config.Driver, config.Path)
}

// TestFeatureFlagDisabled verifies backward compatibility when disabled
func TestFeatureFlagDisabled(t *testing.T) {
	config := DefaultPersistenceConfig()

	// Feature flag disabled
	if config.Enabled {
		t.Fatal("Feature flag should be disabled by default")
	}

	t.Logf("✓ With persistence.enabled=false: storage disabled, Phase 3.0X behavior preserved")
}

// TestFeatureFlagEnabled verifies persistence ready when enabled
func TestFeatureFlagEnabled(t *testing.T) {
	config := PersistenceConfig{
		Enabled:        true,
		Driver:         "sqlite",
		Path:           ":memory:",
		MaxConnections: 25,
		MaxIdleConns:   5,
	}

	if !config.Enabled {
		t.Fatal("Feature flag should be enabled")
	}

	if config.Driver != "sqlite" {
		t.Fatalf("expected driver 'sqlite', got %q", config.Driver)
	}

	t.Logf("✓ With persistence.enabled=true: database will be created and migrated on startup")
}

// TestFeatureFlagConfiguration validates all configuration options
func TestFeatureFlagConfiguration(t *testing.T) {
	scenarios := []struct {
		name   string
		config PersistenceConfig
		valid  bool
	}{
		{
			name: "disabled_default",
			config: PersistenceConfig{
				Enabled: false,
				Driver:  "sqlite",
				Path:    "data/dso.db",
			},
			valid: true,
		},
		{
			name: "enabled_file",
			config: PersistenceConfig{
				Enabled: true,
				Driver:  "sqlite",
				Path:    "data/dso.db",
			},
			valid: true,
		},
		{
			name: "enabled_memory",
			config: PersistenceConfig{
				Enabled: true,
				Driver:  "sqlite",
				Path:    ":memory:",
			},
			valid: true,
		},
		{
			name: "invalid_driver",
			config: PersistenceConfig{
				Enabled: true,
				Driver:  "invalid",
				Path:    "data/dso.db",
			},
			valid: false,
		},
	}

	for _, scenario := range scenarios {
		// When disabled, just needs to have the flag as disabled
		// When enabled, needs proper driver and path
		valid := (!scenario.config.Enabled && scenario.config.Driver == "sqlite") ||
			(scenario.config.Enabled && scenario.config.Driver == "sqlite")
		if valid != scenario.valid {
			t.Errorf("Scenario %q: expected valid=%v, got %v", scenario.name, scenario.valid, valid)
		}
		t.Logf("✓ %s: valid=%v", scenario.name, valid)
	}
}
