package providers

import (
	"testing"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/pkg/config"
	pkgprovider "github.com/docker-secret-operator/dso/pkg/provider"
)

// Native in-process backends have no transient failure mode and must not be
// wrapped (the wrapper is for network-backed plugin providers).
func TestNativeProvidersAreNotWrapped(t *testing.T) {
	s := NewSecretStoreManager(zap.NewNop())
	prov, err := s.GetProvider("local", config.ProviderConfig{Type: "file", Config: map[string]string{"path": t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := prov.(*pkgprovider.Resilient); wrapped {
		t.Fatal("native file provider must not be wrapped in Resilient")
	}
}
