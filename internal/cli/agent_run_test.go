package cli

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// agentTestEnv gives the agent a synthetic config and a Docker endpoint that
// cannot exist, so nothing real is contacted.
func agentTestEnv(t *testing.T) (cfgPath, dir string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(dir, "no-docker.sock"))
	t.Chdir(dir) // config paths outside DSO dirs must be relative
	cfgPath = "dso.yaml"
	if err := os.WriteFile(filepath.Join(dir, cfgPath), []byte("version: v1\nagent:\n  mode: local\nproviders:\n  vault:\n    type: vault\n    config:\n      address: http://127.0.0.1:1\nsecrets: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, dir
}

func agentArgs(dir string, extra ...string) []string {
	return append([]string{
		"agent",
		"--socket", filepath.Join(dir, "dso.sock"),
		"--driver-socket", filepath.Join(dir, "driver.sock"),
		"--api-addr", "127.0.0.1:0",
		"--metrics-addr", "127.0.0.1:0",
	}, extra...)
}

func TestAgent_MissingConfigIsAnErrorNotAProcessExit(t *testing.T) {
	_, dir := agentTestEnv(t)
	code, out, errOut := runCLI(append([]string{"-c", "does-not-exist.yaml"}, agentArgs(dir)...)...)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (and, importantly, the test process survived)", code)
	}
	if strings.Count(errOut, "Error:") != 1 || !strings.Contains(errOut, "failed to load configuration") {
		t.Errorf("want one clear config error, got %q", errOut)
	}
	if strings.Contains(out, "DSO Agent stopped") {
		t.Error("agent must not report a clean stop on startup failure")
	}
}

func TestAgent_RESTStartFailureUnwindsAndReturns(t *testing.T) {
	cfg, dir := agentTestEnv(t)
	args := agentArgs(dir)
	t.Setenv("DSO_AUTH_TOKEN", "")
	args[6] = "0.0.0.0:0" // non-loopback without auth is refused, after socket+driver servers are up

	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"-c", cfg}, args...))
	// Safety net: if startup unexpectedly succeeds, stop it instead of hanging the suite.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := root.ExecuteContext(ctx)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("want a startup error before the timeout, got %v (ctx: %v)", err, ctx.Err())
	}
	if !strings.Contains(err.Error(), "failed to start REST API server") {
		t.Errorf("unexpected error: %v", err)
	}
	// Earlier stages must have been torn down: socket files removed.
	if _, statErr := os.Stat(filepath.Join(dir, "dso.sock")); statErr == nil {
		t.Error("IPC socket left behind after failed startup")
	}
	if strings.Contains(out.String(), "DSO Agent stopped") {
		t.Error("must not report a clean stop")
	}
}

func TestAgent_StartsAndShutsDownCleanlyOnCancel(t *testing.T) {
	cfg, dir := agentTestEnv(t)
	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"-c", cfg}, agentArgs(dir)...))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	sock := filepath.Join(dir, "dso.sock")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("agent exited during startup: %v / %s", err, errOut.String())
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("agent socket never appeared")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("clean shutdown returned %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("agent did not shut down after cancellation")
	}
	if !strings.Contains(out.String(), "DSO Agent stopped.") {
		t.Errorf("stdout: %q", out.String())
	}
	if _, err := os.Stat(sock); err == nil {
		t.Error("IPC socket left behind after shutdown")
	}
}

// A REST API bind failure used to be logged from a goroutine while the agent
// kept running without its API. It must now abort startup and unwind.
func TestAgent_RESTAddressInUseAbortsStartup(t *testing.T) {
	cfg, dir := agentTestEnv(t)
	t.Setenv("DSO_AUTH_TOKEN", "")
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	args := agentArgs(dir)
	args[6] = busy.Addr().String()

	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"-c", cfg}, args...))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = root.ExecuteContext(ctx)
	if err == nil || ctx.Err() != nil || !strings.Contains(err.Error(), "failed to start REST API server") {
		t.Fatalf("want startup abort, got %v (ctx %v)", err, ctx.Err())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dso.sock")); statErr == nil {
		t.Error("IPC socket left behind after aborted startup")
	}
}
