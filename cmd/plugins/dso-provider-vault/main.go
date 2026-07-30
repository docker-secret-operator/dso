package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/hashicorp/go-plugin"
	vault "github.com/hashicorp/vault/api"
)

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

type VaultProvider struct {
	client *vault.Client
	mount  string
}

func (p *VaultProvider) Init(config map[string]string) error {
	vaultAddr := config["address"]
	if vaultAddr == "" {
		vaultAddr = "http://127.0.0.1:8200"
	}

	// The Vault token is sent on every request as an X-Vault-Token header, so
	// cleartext HTTP to a remote host exposes it to anyone on the network
	// path. Loopback stays permitted so local development and the default
	// above keep working; anything else must be https.
	if err := requireSecureVaultAddr(vaultAddr); err != nil {
		return err
	}

	vaultToken := config["token"]
	if vaultToken == "" {
		return fmt.Errorf("vault token is required")
	}

	p.mount = config["mount"]
	if p.mount == "" {
		p.mount = "secret"
	}

	cfg := vault.DefaultConfig()
	cfg.Address = vaultAddr

	client, err := vault.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault client: %w", err)
	}

	client.SetToken(vaultToken)
	p.client = client

	return nil
}

func (p *VaultProvider) GetSecret(name string) (map[string]string, error) {
	// Every other provider guards this; without it a GetSecret call that
	// reaches the RPC server before Init nil-derefs and panics the plugin.
	if p.client == nil {
		return nil, fmt.Errorf("vault provider not initialized — Init() was not called")
	}

	// Support ?version= kv v2 version pinning
	version := ""
	cleanName := name
	if strings.Contains(name, "?version=") {
		parts := strings.SplitN(name, "?version=", 2)
		cleanName = parts[0]
		version = parts[1]
	}

	if err := validateVaultSecretName(cleanName); err != nil {
		return nil, err
	}

	// Vault KV v2 uses 'data' in the path
	prefix := p.mount + "/data/"
	vaultPath := prefix + cleanName

	// Defense in depth: even with the segment checks above, confirm the
	// normalized path still resolves inside the configured mount, so a secret
	// name can never reach an unrelated Vault path the token happens to be
	// authorized for (e.g. sys/, transit/, another team's mount).
	if cleaned := path.Clean(vaultPath); !strings.HasPrefix(cleaned, prefix) {
		return nil, fmt.Errorf(
			"vault secret name %q resolves outside the configured mount %q",
			name, p.mount,
		)
	}

	var secret *vault.Secret
	var err error
	if version != "" {
		secret, err = p.client.Logical().ReadWithData(vaultPath, map[string][]string{"version": {version}})
	} else {
		secret, err = p.client.Logical().Read(vaultPath)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read vault secret %s: %w", name, err)
	}

	if secret == nil {
		return nil, fmt.Errorf("vault secret %s not found", name)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid vault secret format for %s", name)
	}

	result := make(map[string]string)
	for k, v := range data {
		switch tv := v.(type) {
		case string:
			result[k] = tv
		case nil:
			result[k] = ""
		default:
			// Non-scalar KV values (nested objects/arrays) must be re-encoded
			// as JSON. fmt.Sprintf("%v", ...) would emit Go debug syntax like
			// "map[a:1]" and inject that as the secret value.
			encoded, err := json.Marshal(tv)
			if err != nil {
				return nil, fmt.Errorf("vault secret %s: cannot encode value for key %q: %w", name, k, err)
			}
			result[k] = string(encoded)
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf(
			"vault secret %s contains no key/value pairs\n  Fix: write at least one key under this path",
			name,
		)
	}

	return result, nil
}

// validateVaultSecretName rejects secret names that would escape the
// configured KV mount. Vault paths legitimately contain "/" (e.g.
// "app/db/password"), so only traversal segments and absolute paths are
// rejected -- not slashes in general.
func validateVaultSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("vault secret name cannot be empty")
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("vault secret name %q must be relative to the configured mount, not absolute", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return fmt.Errorf(
				"vault secret name %q must not contain '..' path segments (would escape the configured mount)",
				name,
			)
		}
	}
	return nil
}

// requireSecureVaultAddr permits cleartext HTTP only for loopback addresses.
// The Vault token travels in a request header, so plain HTTP to a remote host
// leaks it to the network path.
func requireSecureVaultAddr(addr string) error {
	u, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("invalid vault address %q: %w", addr, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf(
			"vault address %q uses cleartext http to a non-loopback host, which would expose the vault token on the network\n  Fix: use https://, or point at a local agent on 127.0.0.1",
			addr,
		)
	default:
		return fmt.Errorf("vault address %q must use http (loopback only) or https, got scheme %q", addr, u.Scheme)
	}
}

func (p *VaultProvider) WatchSecret(ctx context.Context, name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	ch := make(chan api.SecretUpdate)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				data, err := p.GetSecret(name)
				var errMsg string
				if err != nil {
					errMsg = err.Error()
				}
				select {
				case ch <- api.SecretUpdate{Name: name, Data: data, Error: errMsg}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func main() {
	// --version support: used by `docker dso system doctor` and
	// `docker dso system setup` to validate the plugin binary is functioning.
	// The other three provider plugins already implement this; without it
	// Vault's plugin health check cannot work the same way.
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("dso-provider-vault %s\n", version)
		os.Exit(0)
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: provider.Handshake,
		Plugins: map[string]plugin.Plugin{
			"provider": &provider.SecretProviderPlugin{Impl: &VaultProvider{}},
		},
	})
}
