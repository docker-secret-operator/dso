package api

import (
	"context"
	"encoding/gob"
	"time"
)

func init() {
	gob.Register(SecretUpdate{})
}

type SecretUpdate struct {
	Name  string
	Data  map[string]string
	Error string
}

type SecretProvider interface {
	Init(config map[string]string) error
	GetSecret(name string) (map[string]string, error)
	WatchSecret(name string, interval time.Duration) (<-chan SecretUpdate, error)
}

// SecretProviderWithContext is an optional interface for providers that support context-aware operations.
// Providers that implement this interface will have their context-aware methods called instead of the
// non-context versions, allowing proper cancellation and timeout propagation.
type SecretProviderWithContext interface {
	GetSecretWithContext(ctx context.Context, name string) (map[string]string, error)
}
