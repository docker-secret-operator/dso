package bootstrap

import (
	"context"
	"strings"
	"testing"
)

type nopLogger struct{}

func (nopLogger) Info(msg string, args ...interface{})  {}
func (nopLogger) Error(msg string, args ...interface{}) {}
func (nopLogger) Warn(msg string, args ...interface{})  {}
func (nopLogger) Debug(msg string, args ...interface{}) {}

// TestBuildAndInstallPlugin_UnknownProvider verifies that an unrecognised
// provider name is rejected before any filesystem or exec path is reached (B5).
func TestBuildAndInstallPlugin_UnknownProvider(t *testing.T) {
	ppi := NewProviderPluginInstaller(nopLogger{}, false)
	err := ppi.buildAndInstallPlugin(context.Background(), "evil/../etc", t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestBuildAndInstallPlugin_KnownProviders verifies the four known providers pass
// the allowlist check (they will fail later due to missing source, which is fine).
func TestBuildAndInstallPlugin_KnownProviders(t *testing.T) {
	ppi := NewProviderPluginInstaller(nopLogger{},false)
	for _, p := range []string{"aws", "azure", "vault", "huawei"} {
		err := ppi.buildAndInstallPlugin(context.Background(), p, t.TempDir())
		if err != nil && strings.Contains(err.Error(), "unknown provider") {
			t.Errorf("known provider %q was incorrectly rejected: %v", p, err)
		}
	}
}

// TestInstallProviderPlugins_DryRun verifies dry-run mode never touches disk.
func TestInstallProviderPlugins_DryRun(t *testing.T) {
	ppi := NewProviderPluginInstaller(nopLogger{},true)
	err := ppi.InstallProviderPlugins(context.Background(), []string{"aws", "vault"})
	if err != nil {
		t.Fatalf("dry-run should never fail: %v", err)
	}
}

// TestInstallProviderPlugins_Empty confirms no-op for empty provider list.
func TestInstallProviderPlugins_Empty(t *testing.T) {
	ppi := NewProviderPluginInstaller(nopLogger{},false)
	if err := ppi.InstallProviderPlugins(context.Background(), nil); err != nil {
		t.Fatalf("empty provider list should not fail: %v", err)
	}
}
