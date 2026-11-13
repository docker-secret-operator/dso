package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/vault/api"

	"github.com/docker-secret-operator/dso/pkg/config"
)

// The generated configuration must be accepted by the REAL loader (including the
// owner-id validation the agent performs), for every strategy.
func TestGeneratedConfigLoadsWithTheProductionLoader(t *testing.T) {
	for name, cfg := range map[string]AgentConfig{
		"rolling": {OwnerID: "dso-crash-0123abcd", VaultAddr: "http://127.0.0.1:8200", VaultToken: "t", Secrets: []SecretSpec{
			{Name: "app_token", Mode: "env", Strategy: Rolling, Mapping: map[string]string{"password": "APP_PASSWORD"}}}},
		"restart": {OwnerID: "dso-crash-0123abcd", VaultAddr: "http://127.0.0.1:8200", VaultToken: "t", Secrets: []SecretSpec{
			{Name: "app_token", Mode: "env", Strategy: Restart, Mapping: map[string]string{"password": "APP_PASSWORD"}}}},
		"signal": {OwnerID: "dso-crash-0123abcd", VaultAddr: "http://127.0.0.1:8200", VaultToken: "t", Secrets: []SecretSpec{
			{Name: "sec_a", Mode: "file", Path: "/run/secrets/a", UID: 10001, GID: 10001, Strategy: Signal, Signal: "SIGHUP"},
			{Name: "sec_b", Mode: "file", Path: "/run/secrets/b", UID: 10001, GID: 10001, Strategy: Signal, Signal: "SIGHUP"}}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "dso.yaml"), []byte(cfg.YAML()), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir) // the agent runs with the config directory as its cwd (IsSafePath rejects absolute paths outside /etc/dso)
			got, err := config.LoadConfigWithOwnerID("dso.yaml")
			if err != nil {
				t.Fatalf("production loader rejected the generated config: %v\n%s", err, cfg.YAML())
			}
			if got.Agent.OwnerID != cfg.OwnerID || len(got.Secrets) != len(cfg.Secrets) || got.Providers["local"].Type != "vault" {
				t.Fatalf("%+v", got)
			}
			for i, s := range got.Secrets {
				if !s.Rotation.Enabled || s.Rotation.Strategy != string(cfg.Secrets[i].Strategy) {
					t.Fatalf("secret %d rotation %+v", i, s.Rotation)
				}
			}
		})
	}
}

// The stub is wire-compatible with the vault client the real plugin uses, and an
// absent secret is the "not armed" state.
func TestVaultStubServesKVv2AndTokenIsEnforced(t *testing.T) {
	v := NewVaultStub("tok")
	defer v.Close()
	mk := func(token string) *api.Client {
		cfg := api.DefaultConfig()
		cfg.Address = v.Addr()
		c, err := api.NewClient(cfg)
		if err != nil {
			t.Fatal(err)
		}
		c.SetToken(token)
		return c
	}
	c := mk("tok")
	if s, err := c.Logical().Read("secret/data/app_token"); err != nil || s != nil {
		t.Fatalf("absent secret must read as nil/no error (agent retries): %v %v", s, err)
	}
	v.Set("app_token", map[string]string{"password": "p1"})
	s, err := c.Logical().Read("secret/data/app_token")
	if err != nil || s == nil {
		t.Fatalf("%v %v", s, err)
	}
	data := s.Data["data"].(map[string]any)
	if data["password"] != "p1" || v.Reads("app_token") != 1 {
		t.Fatalf("%v reads=%d", data, v.Reads("app_token"))
	}
	v.Remove("app_token")
	if s, _ := c.Logical().Read("secret/data/app_token"); s != nil {
		t.Fatal("removed secret still served")
	}
	if _, err := mk("wrong").Logical().Read("secret/data/app_token"); err == nil {
		t.Fatal("wrong token accepted")
	}
}
