package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// No-Docker smoke test of the harness against the REAL dsofault-tagged agent and
// the REAL vault plugin: boot, provider plugin load from a per-test directory, the
// "now running" readiness marker, the per-test state directory, graceful SIGTERM,
// and the whole self-kill chain (armed point -> SIGKILL -> marker -> VerifyFault).
// The agent is pointed at an unreachable TCP endpoint, so it can never reach any
// Docker daemon: this proves the orchestration, not rotation behaviour.
func TestRealAgentBootAndSelfKillWithoutDocker(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the agent and the vault plugin; run without -short")
	}
	const noDocker = "tcp://127.0.0.1:1" // nothing listens here
	bin := t.TempDir()
	dso, plugDir := filepath.Join(bin, "dso-fault"), filepath.Join(bin, "plugins")
	if err := os.MkdirAll(plugDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goBuild(t, dso, "dsofault")
	goBuildPkg(t, filepath.Join(plugDir, "dso-provider-vault"), "./cmd/plugins/dso-provider-vault")
	pb, _ := os.ReadFile(filepath.Join(plugDir, "dso-provider-vault"))
	sum := sha256.Sum256(pb)
	manifest := filepath.Join(bin, "hashes.txt")
	if err := os.WriteFile(manifest, []byte("dso-provider-vault="+hex.EncodeToString(sum[:])+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	vault := NewVaultStub("tok")
	defer vault.Close()
	vault.Set("app_token", map[string]string{"password": "not-a-real-secret-value"})

	run := func(t *testing.T, n int, root Root, extra ...string) *Proc {
		env, err := SanitizedEnv(os.Environ(), noDocker, map[string]string{
			"HOME": root.Home, "DSO_PLUGIN_DIR": plugDir, "DSO_PLUGIN_HASH_MANIFEST": manifest, "DSO_FAULT_PLUGIN_DIR": plugDir,
			"DSO_FAULT_STATE_DIR": root.State, "DSO_FAULT_LOCK_DIR": root.Lock, "DSO_AUTH_TOKEN": "at-0123456789abcdef0123456789abcdef",
		})
		if err != nil {
			t.Fatal(err)
		}
		env = append(env, extra...)
		p, err := Start(Spec{Path: dso, Args: root.AgentArgs("127.0.0.1:0", "127.0.0.1:0"), Dir: root.Cfg, Env: env, LogPath: root.AgentLog(n)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.TerminateGroup(5 * time.Second) })
		return p
	}
	cfg := func(root Root) {
		if err := root.WriteConfig(AgentConfig{OwnerID: "dso-crash-0123abcd", VaultAddr: vault.Addr(), VaultToken: "tok",
			Secrets: []SecretSpec{{Name: "app_token", Mode: "env", Strategy: Rolling, Mapping: map[string]string{"password": "APP_PASSWORD"}}}}); err != nil {
			t.Fatal(err)
		}
	}
	logHas := func(root Root, n int, s string) bool {
		b, _ := os.ReadFile(root.AgentLog(n))
		return strings.Contains(string(b), s)
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		for i := 0; i < 300; i++ {
			if ok() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	t.Run("boots, loads the plugin, shuts down gracefully", func(t *testing.T) {
		root := shortRoot(t)
		cfg(root)
		p := run(t, 1, root)
		wait("readiness marker", func() bool { return logHas(root, 1, "DSO Agent is now running") })
		wait("the real plugin to read the secret from the stub", func() bool { return vault.Reads("app_token") > 0 })
		if _, err := os.Stat(filepath.Join(root.State, "rotations.json")); err != nil {
			t.Errorf("the agent did not use the per-test state directory: %v", err)
		}
		if err := p.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		info, err := p.WaitExit(30 * time.Second)
		if err != nil || !info.Exited || info.Code != 0 {
			t.Fatalf("graceful stop: %v %v\n%s", info, err, readTail(root.AgentLog(1)))
		}
		_ = p.KillStragglers()
	})

	t.Run("armed point kills the agent by SIGKILL and the marker names its pid", func(t *testing.T) {
		root := shortRoot(t)
		cfg(root)
		fe, err := FaultEnv("state:after_persist_rename", 1, root.MarkerPath(1))
		if err != nil {
			t.Fatal(err)
		}
		p := run(t, 1, root, fe...)
		info, err := p.WaitExit(60 * time.Second)
		if err != nil {
			t.Fatalf("%v\n%s", err, readTail(root.AgentLog(1)))
		}
		marker, err := os.ReadFile(root.MarkerPath(1))
		if err != nil {
			t.Fatalf("no marker: %v (%s)", err, info)
		}
		if err := VerifyFault("state:after_persist_rename", p.PID(), info, marker); err != nil {
			t.Fatal(err)
		}
		_ = p.KillStragglers()
	})

	t.Run("an unarmed run is not killed by an unrelated point", func(t *testing.T) {
		root := shortRoot(t)
		cfg(root)
		fe, _ := FaultEnv("rolling:after_intent_swap", 1, root.MarkerPath(1)) // never reached without containers
		p := run(t, 1, root, fe...)
		wait("readiness marker", func() bool { return logHas(root, 1, "DSO Agent is now running") })
		time.Sleep(1500 * time.Millisecond)
		if _, done := p.Exit(); done {
			t.Fatalf("the agent died at a point it never reached\n%s", readTail(root.AgentLog(1)))
		}
		if _, err := os.Stat(root.MarkerPath(1)); err == nil {
			t.Fatal("marker written although the armed point was never reached")
		}
		_ = p.Signal(syscall.SIGTERM)
		_, _ = p.WaitExit(30 * time.Second)
		_ = p.KillStragglers()
	})
}

func readTail(path string) string {
	b, _ := os.ReadFile(path)
	s := string(b)
	if len(s) > 3000 {
		s = s[len(s)-3000:]
	}
	return s
}

// shortRoot makes a Root under a SHORT directory: unix socket paths are limited to
// ~104 bytes and t.TempDir() embeds the test name.
func shortRoot(t *testing.T) Root {
	t.Helper()
	base, err := os.MkdirTemp("", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	root, err := NewRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
