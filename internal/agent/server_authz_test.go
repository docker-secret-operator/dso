package agent

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// Synthetic placeholder values only -- never real secret material.
const (
	syntheticAllowed = "synthetic-allowed-value"
	syntheticOther   = "synthetic-other-dir-value"
)

func writeSyntheticSecret(t *testing.T, dir, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"value":"`+value+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newAuthzTestServer returns an AgentServer whose dso.yaml defines one file
// provider "files" rooted at a temp dir and maps only the secret "allowed".
func newAuthzTestServer(t *testing.T) (*AgentServer, string) {
	t.Helper()
	dir := t.TempDir()
	writeSyntheticSecret(t, dir, "allowed", syntheticAllowed)
	writeSyntheticSecret(t, dir, "unlisted", "synthetic-unlisted-value")
	logger := zap.NewNop()
	return &AgentServer{
		Cache:  NewSecretCache(time.Hour),
		Store:  providers.NewSecretStoreManager(logger),
		Logger: logger,
		Config: &config.Config{
			Providers: map[string]config.ProviderConfig{
				"files": {Type: "file", Config: map[string]string{"path": dir}},
			},
			Secrets: []config.SecretMapping{{Name: "allowed", Provider: "files"}},
		},
	}, dir
}

func assertDenied(t *testing.T, err error, resp *api.AgentResponse) {
	t.Helper()
	if !errors.Is(err, errIPCPermissionDenied) {
		t.Fatalf("expected permission denied, got err=%v", err)
	}
	if !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("expected resp.Error to report permission denied, got %q", resp.Error)
	}
	if resp.Data != nil {
		t.Fatal("denied request must not return data")
	}
}

// PE-28 regression: the legacy one-off branch let any IPC caller name an
// unconfigured provider and supply its config, making the root agent load the
// in-process file backend rooted at a caller-chosen directory.
func TestGetSecret_UnconfiguredProviderWithCallerConfig_DeniedWithoutLoading(t *testing.T) {
	s, _ := newAuthzTestServer(t)
	outside := t.TempDir()
	writeSyntheticSecret(t, outside, "target", syntheticOther)

	resp := &api.AgentResponse{}
	err := s.GetSecret(&api.AgentRequest{Provider: "file", Config: map[string]string{"path": outside}, Secret: "target"}, resp)
	assertDenied(t, err, resp)

	if _, known := s.Store.Health("file"); known {
		t.Fatal("a denied request must never initialise a provider")
	}
	if _, found := s.Cache.Get("file:target"); found {
		t.Fatal("a denied request must never write the cache")
	}
}

// PE-28: the env backend (agent process environment) is not reachable over IPC.
func TestGetSecret_EnvBackend_Denied(t *testing.T) {
	s, _ := newAuthzTestServer(t)
	resp := &api.AgentResponse{}
	err := s.GetSecret(&api.AgentRequest{Provider: "env", Secret: "PATH"}, resp)
	assertDenied(t, err, resp)
}

// PE-11: only secret names mapped in dso.yaml may be fetched, even from a
// configured provider whose credentials could read more.
func TestGetSecret_SecretNotInMappings_Denied(t *testing.T) {
	s, _ := newAuthzTestServer(t)
	resp := &api.AgentResponse{}
	err := s.GetSecret(&api.AgentRequest{Provider: "files", Secret: "unlisted"}, resp)
	assertDenied(t, err, resp)

	resp = &api.AgentResponse{}
	err = s.GetSecret(&api.AgentRequest{Provider: "files", Secret: "../files/unlisted"}, resp)
	assertDenied(t, err, resp)
}

// Configured pairs keep working, and caller-supplied Config is ignored: the
// agent uses only its own provider definition.
func TestGetSecret_ConfiguredPair_UsesAgentConfigNotCallerConfig(t *testing.T) {
	s, _ := newAuthzTestServer(t)
	outside := t.TempDir()
	writeSyntheticSecret(t, outside, "allowed", syntheticOther)

	resp := &api.AgentResponse{}
	err := s.GetSecret(&api.AgentRequest{Provider: "files", Config: map[string]string{"path": outside}, Secret: "allowed"}, resp)
	if err != nil {
		t.Fatalf("configured pair should be served: %v", err)
	}
	if resp.Data["value"] != syntheticAllowed {
		t.Fatalf("expected value from the agent's own provider config, got %q", resp.Data["value"])
	}
}

// An empty provider name is resolved from the secret's own mapping.
func TestGetSecret_EmptyProvider_ResolvedFromMapping(t *testing.T) {
	s, _ := newAuthzTestServer(t)
	s.Config.Providers["other"] = config.ProviderConfig{Type: "env"}

	resp := &api.AgentResponse{}
	if err := s.GetSecret(&api.AgentRequest{Secret: "allowed"}, resp); err != nil {
		t.Fatalf("expected resolution via mapping: %v", err)
	}
	if resp.Data["value"] != syntheticAllowed {
		t.Fatalf("unexpected value %q", resp.Data["value"])
	}

	// Not mapped anywhere and two providers: ambiguous => denied.
	resp = &api.AgentResponse{}
	assertDenied(t, s.GetSecret(&api.AgentRequest{Secret: "unlisted"}, resp), resp)
}

func TestPeerTier(t *testing.T) {
	self := 1234
	cases := []struct {
		peer peerIdentity
		want ipcTier
	}{
		{peerIdentity{uid: 0}, tierPrivileged},
		{peerIdentity{uid: 1234}, tierPrivileged},
		{peerIdentity{uid: 1001, gid: 999}, tierRestricted},
	}
	for _, c := range cases {
		if got := peerTier(c.peer, self); got != c.want {
			t.Errorf("peerTier(uid=%d) = %v, want %v", c.peer.uid, got, c.want)
		}
	}
}

// PE-11 method tiers: CheckProviderConnectivity is root-only; the rest are
// available to the dso group.
func TestAgentConn_MethodTiers(t *testing.T) {
	s, _ := newAuthzTestServer(t)
	s.Trigger = nil

	restricted := &agentConn{srv: s, auditUser: "dso-member", tier: tierRestricted}
	privileged := &agentConn{srv: s, auditUser: "root", tier: tierPrivileged}
	none := &agentConn{srv: s, auditUser: "nobody", tier: tierNone}

	checkReq := &api.ProviderCheckRequest{ProviderName: "probe", Type: "env"}

	var checkResp api.ProviderCheckResponse
	if err := restricted.CheckProviderConnectivity(checkReq, &checkResp); !errors.Is(err, errIPCPermissionDenied) {
		t.Fatalf("restricted peer must be denied CheckProviderConnectivity, got %v", err)
	}
	if checkResp.Reachable {
		t.Fatal("denied check must not report reachable")
	}

	checkResp = api.ProviderCheckResponse{}
	if err := privileged.CheckProviderConnectivity(checkReq, &checkResp); err != nil {
		t.Fatalf("privileged peer must be allowed: %v", err)
	}
	if !checkResp.Reachable {
		t.Fatalf("expected env probe to be reachable, got error %q", checkResp.Error)
	}
	// PE-11: the check must never populate the shared provider cache.
	if _, known := s.Store.Health("probe"); known {
		t.Fatal("connectivity check populated the shared provider cache")
	}

	// Restricted methods are available to the dso group.
	if err := restricted.GetEvents(&api.AgentRequest{}, &api.AgentResponse{}); err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if err := restricted.GetStatus(&api.StatusRequest{}, &api.StatusResponse{}); err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	resp := &api.AgentResponse{}
	if err := restricted.GetSecret(&api.AgentRequest{Provider: "files", Secret: "allowed"}, resp); err != nil {
		t.Fatalf("GetSecret for a configured pair: %v", err)
	}
	// TriggerReconciliation is authorized; it then fails only because this
	// test server has no trigger engine.
	err := restricted.TriggerReconciliation(&api.ReconcileRequest{}, &api.ReconcileResponse{})
	if err == nil || errors.Is(err, errIPCPermissionDenied) {
		t.Fatalf("expected authorized-but-unavailable error, got %v", err)
	}

	// A peer with no tier gets nothing.
	if err := none.GetEvents(&api.AgentRequest{}, &api.AgentResponse{}); !errors.Is(err, errIPCPermissionDenied) {
		t.Fatalf("tierNone must be denied, got %v", err)
	}
}

// Default deny for the IPC surface: every method net/rpc would register on
// agentConn must appear in ipcMethodTiers (and vice versa), so adding a method
// without classifying it fails here instead of silently exposing it.
func TestAgentConn_EveryRPCMethodHasATier(t *testing.T) {
	errType := reflect.TypeOf((*error)(nil)).Elem()
	typ := reflect.TypeOf(&agentConn{})
	exposed := map[string]bool{}
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		mt := m.Type // receiver, args, reply
		if mt.NumIn() != 3 || mt.NumOut() != 1 || mt.Out(0) != errType || mt.In(2).Kind() != reflect.Pointer {
			continue
		}
		exposed[m.Name] = true
		if _, ok := ipcMethodTiers[m.Name]; !ok {
			t.Errorf("RPC method %s has no entry in ipcMethodTiers", m.Name)
		}
	}
	for name := range ipcMethodTiers {
		if !exposed[name] {
			t.Errorf("ipcMethodTiers lists %s but agentConn does not expose it", name)
		}
	}
}

// End to end over a real Unix socket: the legacy caller-config request is
// refused by the running server.
func TestStartSocketServer_DeniesCallerSuppliedProvider(t *testing.T) {
	// Short path: macOS limits sun_path to 104 bytes and t.TempDir() is long.
	dir, err := os.MkdirTemp("/tmp", "dso-authz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "a.sock")

	s, _ := newAuthzTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, shutdown, err := StartSocketServer(ctx, socketPath, s.Cache, s.Store, zap.NewNop(), s.Config)
	if err != nil {
		t.Fatalf("StartSocketServer: %v", err)
	}
	defer shutdown()

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client := rpc.NewClient(conn)
	defer func() { _ = client.Close() }()

	outside := t.TempDir()
	writeSyntheticSecret(t, outside, "target", syntheticOther)
	var resp api.AgentResponse
	err = client.Call("Agent.GetSecret", &api.AgentRequest{Provider: "file", Config: map[string]string{"path": outside}, Secret: "target"}, &resp)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied over the socket, got err=%v", err)
	}
	if resp.Data != nil {
		t.Fatal("denied request returned data")
	}

	resp = api.AgentResponse{}
	if err := client.Call("Agent.GetSecret", &api.AgentRequest{Provider: "files", Secret: "allowed"}, &resp); err != nil {
		t.Fatalf("configured pair over the socket: %v", err)
	}
	if resp.Data["value"] != syntheticAllowed {
		t.Fatalf("unexpected value %q", resp.Data["value"])
	}
}
