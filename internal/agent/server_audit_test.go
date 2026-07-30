package agent

import (
	"bytes"
	"context"
	"net"
	"net/rpc"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/audit"
	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// captureAuditLog redirects internal/audit's package-level logger to a
// buffer for the duration of the test and returns it.
func captureAuditLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)
	audit.InitAuditLogger(zap.New(core))
	return buf
}

func newTestAgentServer(t *testing.T) *AgentServer {
	t.Helper()
	logger := zap.NewNop()
	return &AgentServer{
		Cache:  NewSecretCache(time.Hour),
		Store:  providers.NewSecretStoreManager(logger),
		Logger: logger,
		Config: &config.Config{Providers: map[string]config.ProviderConfig{}},
	}
}

// TestGetSecret_AuditLogsOnCacheHit is a regression test for wiring
// internal/audit into AgentServer.GetSecret. Before this change, audit.Log
// was fully built, SEC-1-hardened, and unit-tested, but had zero production
// callers -- every secret fetch served by the agent (the vast majority of
// production traffic goes through this cache-hit path) produced Prometheus
// counters but no compliance audit trail at all.
func TestGetSecret_AuditLogsOnCacheHit(t *testing.T) {
	buf := captureAuditLog(t)
	s := newTestAgentServer(t)
	s.Cache.Set("vault:db_password", map[string]string{"value": "hunter2"})

	req := &api.AgentRequest{Provider: "vault", Secret: "db_password"}
	resp := &api.AgentResponse{}
	if err := s.GetSecret(req, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		`"event":"secret_fetch"`,
		`"secret_name":"db_password"`,
		`"provider":"vault"`,
		`"status":"success"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected audit log to contain %s, got: %s", want, out)
		}
	}
	// The fetched secret's plaintext value must never appear in the audit
	// trail -- audit.Log only ever receives the secret's name, never its data.
	if strings.Contains(out, "hunter2") {
		t.Errorf("audit log must never contain the secret's plaintext value: %s", out)
	}
}

// TestGetSecret_AuditLogsOnProviderFailure confirms a failed provider lookup
// (an unknown/unconfigured provider type) still produces a "failed" audit
// record, not just a silent RPC error returned to the caller. Retry.Attempts
// is pinned to 1 so the test doesn't pay the multi-second exponential
// backoff between retry attempts for what is deliberately a permanent
// failure (no such plugin binary exists).
func TestGetSecret_AuditLogsOnProviderFailure(t *testing.T) {
	buf := captureAuditLog(t)
	s := newTestAgentServer(t)
	s.Config.Providers["nonexistent-provider-type"] = config.ProviderConfig{
		Type:  "nonexistent-provider-type",
		Retry: config.RetryConfig{Attempts: 1},
	}

	req := &api.AgentRequest{Provider: "nonexistent-provider-type", Secret: "whatever"}
	resp := &api.AgentResponse{}
	if err := s.GetSecret(req, resp); err == nil {
		t.Fatal("expected an error for an unknown provider type")
	}

	out := buf.String()
	if !strings.Contains(out, `"event":"secret_fetch"`) {
		t.Errorf("expected audit log to contain secret_fetch event, got: %s", out)
	}
	if !strings.Contains(out, `"status":"failed"`) {
		t.Errorf("expected audit log to record failed status, got: %s", out)
	}
}

// TestGetSecret_HTTPDriverPathAttributesDistinctUser confirms the exported
// AgentServer.GetSecret (reachable directly from StartDriverServer's HTTP
// path, which has no SO_PEERCRED/peer-identity concept of its own) attributes
// audit entries to "docker-secret-driver" -- a real, identifiable caller,
// distinct from the Unix-socket RPC path's resolved peer identity -- rather
// than the old undifferentiated "agent" placeholder both paths used to share.
func TestGetSecret_HTTPDriverPathAttributesDistinctUser(t *testing.T) {
	buf := captureAuditLog(t)
	s := newTestAgentServer(t)
	s.Cache.Set("vault:db_password", map[string]string{"value": "hunter2"})

	req := &api.AgentRequest{Provider: "vault", Secret: "db_password"}
	resp := &api.AgentResponse{}
	if err := s.GetSecret(req, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"user":"docker-secret-driver"`) {
		t.Errorf(`expected audit log to record user "docker-secret-driver", got: %s`, out)
	}
}

// TestStartSocketServer_AuditLogsRealPeerIdentity is the regression test for
// threading the real, SO_PEERCRED-authenticated peer identity into the audit
// log instead of the fixed "agent" placeholder. It exercises the actual
// production path end to end: starts a real Unix-socket agent server, dials
// it as a real OS process (so the kernel reports this test binary's own
// credentials via SO_PEERCRED, exactly as it would for any real client), and
// makes a real net/rpc call over that connection.
func TestStartSocketServer_AuditLogsRealPeerIdentity(t *testing.T) {
	buf := captureAuditLog(t)

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "agent.sock")

	cache := NewSecretCache(time.Hour)
	cache.Set("vault:db_password", map[string]string{"value": "hunter2"})
	store := providers.NewSecretStoreManager(zap.NewNop())
	cfg := &config.Config{Providers: map[string]config.ProviderConfig{}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, shutdown, err := StartSocketServer(ctx, socketPath, cache, store, zap.NewNop(), cfg)
	if err != nil {
		t.Fatalf("StartSocketServer failed: %v", err)
	}
	defer shutdown()

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to dial agent socket: %v", err)
	}
	defer conn.Close()

	client := rpc.NewClient(conn)
	defer client.Close()

	req := &api.AgentRequest{Provider: "vault", Secret: "db_password"}
	resp := &api.AgentResponse{}
	if err := client.Call("Agent.GetSecret", req, resp); err != nil {
		t.Fatalf("RPC call failed: %v", err)
	}

	// The dialing process (this test binary) is its own SO_PEERCRED peer, so
	// the expected audit identity is whatever this process's own UID resolves
	// to -- proving the value came from the real, per-connection peer
	// credential, not a hardcoded string.
	wantUser := "uid:" + strconv.Itoa(os.Getuid())
	if u, lookupErr := user.LookupId(strconv.Itoa(os.Getuid())); lookupErr == nil {
		wantUser = u.Username
	}

	out := buf.String()
	if strings.Contains(out, `"user":"agent"`) {
		t.Errorf("audit log still uses the old fixed \"agent\" placeholder: %s", out)
	}
	if !strings.Contains(out, `"user":"`+wantUser+`"`) {
		t.Errorf("expected audit log to attribute the real peer identity %q, got: %s", wantUser, out)
	}
}
