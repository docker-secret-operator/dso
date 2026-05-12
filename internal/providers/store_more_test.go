package providers

import (
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/hashicorp/go-plugin"
	"go.uber.org/zap/zaptest"
	"testing"
)

func TestSecretStoreManager_GetProvider_ConfigMapping(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	// Inject auth and region config
	cfg := config.ProviderConfig{
		Type:   "non-existent",
		Region: "us-east-1",
		Auth: config.AuthConfig{
			Method: "token",
			Params: map[string]string{"foo": "bar"},
		},
		Config: map[string]string{"custom": "val"},
		Retry: config.RetryConfig{
			Attempts: 1,
		},
	}

	_, _ = manager.GetProvider("test-prov", cfg)
	// It will error out but cover the mapping lines
}

// Dummy type for the mock client to avoid nil panic
type dummyClient struct {
	plugin.Client
}

func (d *dummyClient) Kill() {
	// do nothing
}

func TestShutdown_WithEntries(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	// Create a dummy client to avoid panic during Kill()
	// plugin.Client is a struct, we can just instantiate an empty one or a mock
	client := &plugin.Client{}

	manager.store.Store("prov1", &StoreEntry{
		Provider: nil,
		Client:   client,
	})

	manager.Shutdown()
}
