package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// VaultStub is a minimal Vault KV v2 read endpoint served on loopback. The REAL
// dso-provider-vault plugin (built from cmd/plugins/dso-provider-vault) talks to it,
// so the agent under test uses its production provider path: plugin process, RPC,
// hash-verified binary. The stub is also the rotation trigger: a secret that is
// absent answers 404 (the agent keeps retrying), and Set makes the next poll see
// it, which starts a rotation.
type VaultStub struct {
	srv   *httptest.Server
	token string

	mu    sync.Mutex
	data  map[string]map[string]string
	ver   map[string]int
	reads map[string]int
}

// NewVaultStub starts the stub; token is the only accepted X-Vault-Token.
func NewVaultStub(token string) *VaultStub {
	v := &VaultStub{token: token, data: map[string]map[string]string{}, ver: map[string]int{}, reads: map[string]int{}}
	v.srv = httptest.NewServer(http.HandlerFunc(v.serve))
	return v
}

// Addr is the base URL (http://127.0.0.1:port).
func (v *VaultStub) Addr() string { return v.srv.URL }

// Close stops the stub.
func (v *VaultStub) Close() { v.srv.Close() }

// Set publishes (or replaces) a secret.
func (v *VaultStub) Set(name string, kv map[string]string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cp := make(map[string]string, len(kv))
	for k, val := range kv {
		cp[k] = val
	}
	v.data[name] = cp
	v.ver[name]++
}

// Remove makes a secret absent again (404).
func (v *VaultStub) Remove(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.data, name)
}

// Reads is how many times a secret was successfully read.
func (v *VaultStub) Reads(name string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.reads[name]
}

func (v *VaultStub) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Vault-Token") != v.token {
		http.Error(w, `{"errors":["permission denied"]}`, http.StatusForbidden)
		return
	}
	const prefix = "/v1/secret/data/"
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, `{"errors":[]}`, http.StatusNotFound)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, prefix)
	v.mu.Lock()
	kv, ok := v.data[name]
	ver := v.ver[name]
	if ok {
		v.reads[name]++
	}
	v.mu.Unlock()
	if !ok {
		http.Error(w, `{"errors":[]}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"data": kv, "metadata": map[string]any{"version": ver}},
	})
}
