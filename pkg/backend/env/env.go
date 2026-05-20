package env

import (
	"context"
	"os"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
)

type EnvProvider struct{}

func (p *EnvProvider) Init(config map[string]string) error {
	return nil
}

func (p *EnvProvider) GetSecret(name string) (map[string]string, error) {
	val := os.Getenv(name)
	if val == "" {
		return map[string]string{}, nil
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
				data, _ := p.GetSecret(name)
				select {
				case ch <- api.SecretUpdate{Name: name, Data: data}:
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
