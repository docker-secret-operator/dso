package agent

import (
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// TestTriggerReconciliation_NoTriggerEngine_ReturnsError proves the CLI-3 fix
// degrades gracefully — a nil Trigger (e.g. a server constructed without one
// in a test, or a future code path that forgets to wire it) must produce a
// clear error rather than a nil-pointer panic when `docker dso sync` calls in.
func TestTriggerReconciliation_NoTriggerEngine_ReturnsError(t *testing.T) {
	s := newTestAgentServer(t)

	var resp api.ReconcileResponse
	err := s.TriggerReconciliation(&api.ReconcileRequest{}, &resp)
	if err == nil {
		t.Fatal("expected an error when Trigger is nil")
	}
}

// TestTriggerReconciliation_UnknownSecret_ChecksNothing proves that
// requesting reconciliation for a secret name that doesn't exist in the
// agent's config checks zero secrets, rather than silently claiming success
// the way the old fabricated implementation always did.
func TestTriggerReconciliation_UnknownSecret_ChecksNothing(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{},
		Secrets:   []config.SecretMapping{{Name: "db_password", Provider: "vault"}},
	}
	s := &AgentServer{
		Cache:  NewSecretCache(time.Hour),
		Store:  providers.NewSecretStoreManager(logger),
		Logger: logger,
		Config: cfg,
	}
	s.Trigger = NewTriggerEngineForTest(t, s.Cache, s.Store, nil, logger, cfg, nil)

	var resp api.ReconcileResponse
	if err := s.TriggerReconciliation(&api.ReconcileRequest{Secret: "does-not-exist"}, &resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SecretsChecked != 0 {
		t.Errorf("expected 0 secrets checked for an unknown secret name, got %d", resp.SecretsChecked)
	}
}

// TestTriggerReconciliation_MissingProvider_ReportsError proves a secret
// whose configured provider doesn't exist in the config is reported as a
// per-secret error rather than silently skipped or crashing the whole call.
func TestTriggerReconciliation_MissingProvider_ReportsError(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{},
		Secrets:   []config.SecretMapping{{Name: "db_password", Provider: "nonexistent-provider"}},
	}
	s := &AgentServer{
		Cache:  NewSecretCache(time.Hour),
		Store:  providers.NewSecretStoreManager(logger),
		Logger: logger,
		Config: cfg,
	}
	s.Trigger = NewTriggerEngineForTest(t, s.Cache, s.Store, nil, logger, cfg, nil)

	var resp api.ReconcileResponse
	if err := s.TriggerReconciliation(&api.ReconcileRequest{}, &resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Error == "" {
		t.Fatalf("expected 1 result with a populated error, got %+v", resp.Results)
	}
	if resp.SecretsChecked != 0 {
		t.Errorf("a secret whose provider doesn't exist should not count as checked, got %d", resp.SecretsChecked)
	}
}

// TestGetStatus_ReportsRealCacheAndPendingRotations proves GetStatus reflects
// the actual cache/state-tracker contents instead of the old hardcoded
// example numbers (5 entries, 1234 hits, etc. regardless of real state).
func TestGetStatus_ReportsRealCacheAndPendingRotations(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Providers: map[string]config.ProviderConfig{}}
	s := &AgentServer{
		Cache:  NewSecretCache(time.Hour),
		Store:  providers.NewSecretStoreManager(logger),
		Logger: logger,
		Config: cfg,
	}
	s.Trigger = NewTriggerEngineForTest(t, s.Cache, s.Store, nil, logger, cfg, nil)

	// Empty cache to start.
	var resp api.StatusResponse
	if err := s.GetStatus(&api.StatusRequest{}, &resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CacheEntries != 0 {
		t.Errorf("expected 0 cache entries on a fresh cache, got %d", resp.CacheEntries)
	}

	// Add two entries and confirm the count tracks reality.
	s.Cache.Set("vault:db_password", map[string]string{"value": "x"})
	s.Cache.Set("vault:api_key", map[string]string{"value": "y"})

	resp = api.StatusResponse{}
	if err := s.GetStatus(&api.StatusRequest{}, &resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CacheEntries != 2 {
		t.Errorf("expected 2 cache entries after Set x2, got %d", resp.CacheEntries)
	}
}

// TestGetStatus_UnknownProvider_ReportsUnknownNotFabricatedHealth proves a
// configured-but-never-contacted provider is reported as "not yet
// contacted" (known=false) rather than a fabricated healthy/unhealthy
// status, since the agent genuinely doesn't know its state yet.
func TestGetStatus_UnknownProvider_ReportsUnknownNotFabricatedHealth(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {Type: "vault"},
		},
	}
	s := &AgentServer{
		Cache:  NewSecretCache(time.Hour),
		Store:  providers.NewSecretStoreManager(logger),
		Logger: logger,
		Config: cfg,
	}
	s.Trigger = NewTriggerEngineForTest(t, s.Cache, s.Store, nil, logger, cfg, nil)

	var resp api.StatusResponse
	if err := s.GetStatus(&api.StatusRequest{}, &resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("expected 1 provider in status, got %d", len(resp.Providers))
	}
	if resp.Providers[0].Known {
		t.Error("expected Known=false for a provider the agent has never contacted")
	}
	if resp.Providers[0].Healthy {
		t.Error("expected Healthy=false for an unknown provider (must not fabricate healthy)")
	}
}

// TestCheckProviderConnectivity_UnreachableProvider_ReportsNotReachable
// proves the pre-flight check used by `docker dso apply` actually attempts a
// connection and reports failure honestly, rather than declaring any
// syntactically-valid provider type "reachable" without contacting it.
func TestCheckProviderConnectivity_UnreachableProvider_ReportsNotReachable(t *testing.T) {
	logger := zap.NewNop()
	s := &AgentServer{
		Cache:  NewSecretCache(time.Hour),
		Store:  providers.NewSecretStoreManager(logger),
		Logger: logger,
		Config: &config.Config{Providers: map[string]config.ProviderConfig{}},
	}

	req := &api.ProviderCheckRequest{
		ProviderName: "vault",
		Type:         "vault",
		Config: map[string]string{
			// Deliberately invalid/unreachable target so the plugin load or
			// handshake fails quickly rather than hanging on a real network call.
			"address": "http://127.0.0.1:1", // port 1 refuses connections
		},
	}
	var resp api.ProviderCheckResponse
	if err := s.CheckProviderConnectivity(req, &resp); err != nil {
		t.Fatalf("unexpected RPC-level error: %v", err)
	}
	if resp.Reachable {
		t.Error("expected Reachable=false for an unreachable provider target")
	}
	if resp.Error == "" {
		t.Error("expected a non-empty Error explaining why the provider is unreachable")
	}
}
