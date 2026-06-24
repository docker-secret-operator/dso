// Package local implements a SecretProvider backed by the DSO encrypted local vault
// (~/.dso/vault.enc). Secrets are keyed as "project/path"; if no "/" is present the
// entire name is used as the path under the "global" project.
package local

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/vault"
)

// LocalVaultProvider satisfies api.SecretProvider using the encrypted local vault.
type LocalVaultProvider struct {
	v *vault.Vault
}

// Init loads (decrypts) the default vault. The config map is accepted for interface
// compatibility but currently unused — the vault location and master key are resolved
// via the standard DSO_MASTER_KEY env var / ~/.dso/master.key mechanism.
func (p *LocalVaultProvider) Init(_ map[string]string) error {
	v, err := vault.LoadDefault()
	if err != nil {
		return fmt.Errorf("local vault provider: failed to load vault: %w", err)
	}
	p.v = v
	return nil
}

// GetSecret retrieves a secret by name. Name must be "project/path" or just "path"
// (which is looked up under the "global" project).
func (p *LocalVaultProvider) GetSecret(name string) (map[string]string, error) {
	project, path, err := parseSecretName(name)
	if err != nil {
		return nil, fmt.Errorf("local vault provider: %w", err)
	}

	sec, err := p.v.Get(project, path)
	if err != nil {
		return nil, fmt.Errorf("local vault provider: %w", err)
	}

	result := map[string]string{"value": sec.Value}
	for k, v := range sec.Meta {
		result[k] = v
	}
	return result, nil
}

// WatchSecret polls the vault at the given interval and emits updates on the returned
// channel. The channel is closed when ctx is cancelled.
func (p *LocalVaultProvider) WatchSecret(ctx context.Context, name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
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

// parseSecretName splits "project/path" into project and path.
// A name without a slash is treated as path under the "global" project.
func parseSecretName(name string) (project, path string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("secret name cannot be empty")
	}
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 1 || parts[1] == "" {
		return "global", parts[0], nil
	}
	project = strings.TrimSpace(parts[0])
	path = strings.TrimSpace(parts[1])
	if project == "" || path == "" {
		return "", "", fmt.Errorf("invalid secret name %q: project and path cannot be empty", name)
	}
	return project, path, nil
}
