package env

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
)

type EnvProvider struct{}

func (p *EnvProvider) Init(config map[string]string) error {
	return nil
}

func (p *EnvProvider) GetSecret(name string) (map[string]string, error) {
	// An unset variable must be an error, not an empty secret. Returning
	// (empty, nil) here made a typo'd or unexported variable name
	// indistinguishable from a successful fetch: the agent counts it as
	// "success", caches it, and injects a blank value into containers --
	// potentially overwriting a previously-good cached secret on rotation.
	// Every other provider errors on a missing secret; this one now matches.
	//
	// LookupEnv (not Getenv) is used so that a variable deliberately set to
	// the empty string is still honored as an explicit operator choice,
	// while genuinely-unset is rejected.
	val, ok := os.LookupEnv(name)
	if !ok {
		return nil, fmt.Errorf(
			"environment variable %q is not set\n  Fix: export %s before starting DSO, or correct the secret name in dso.yaml",
			name, name,
		)
	}
	// Return the environment variable as a single mapping
	return map[string]string{"value": val}, nil
}

func (p *EnvProvider) WatchSecret(ctx context.Context, name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	ch := make(chan api.SecretUpdate)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Context cancelled, clean up goroutine
				return
			case <-ticker.C:
				// Propagate the error via SecretUpdate.Error rather than
				// silently emitting an update with nil Data, which consumers
				// cannot distinguish from a legitimately empty secret.
				data, err := p.GetSecret(name)
				update := api.SecretUpdate{Name: name, Data: data}
				if err != nil {
					update = api.SecretUpdate{Name: name, Error: err.Error()}
				}
				select {
				case ch <- update:
					// Message sent
				case <-ctx.Done():
					// Context cancelled while sending
					return
				}
			}
		}
	}()
	return ch, nil
}
