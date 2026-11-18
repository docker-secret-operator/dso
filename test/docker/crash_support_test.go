//go:build dockerint

package docker

// Support for the real-process crash-recovery harness (crash_recovery_test.go).
// See docs/testing/REAL-PROCESS-CRASH-TESTS.md.
//
// Isolation: every Docker call goes through dockerguard.Current (verified test
// daemon only); every container the harness creates carries the guard labels plus
// a per-case label; every subprocess gets an explicit sanitised environment whose
// only DOCKER_* variable is DOCKER_HOST set to the guard endpoint; the agent runs
// in its own process group and is only ever signalled by the PID the harness
// started.
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"

	"github.com/docker-secret-operator/dso/test/dockerguard"
	"github.com/docker-secret-operator/dso/test/harness"
)

const (
	crashSecret    = "app_token"
	crashSecretA   = "sec_a"
	crashSecretB   = "sec_b"
	crashAppUID    = 10001
	crashFaultWait = 150 * time.Second
	crashReadyWait = 120 * time.Second
	readyMarker    = "DSO Agent is now running"

	// busybox httpd serving a random per-start identity, so a response proves WHICH
	// instance answered. With FAIL_READINESS set, an instance that receives
	// APP_PASSWORD (i.e. a rotated candidate) marks itself unhealthy; the original
	// starts without APP_PASSWORD and stays healthy.
	crashHTTPScript = `mkdir -p /www; head -c8 /dev/urandom | od -An -tx1 | tr -d ' \n' > /www/index.html; ` +
		`if [ -n "${FAIL_READINESS:-}" ] && [ -n "${APP_PASSWORD:-}" ]; then touch /tmp/bad; fi; ` +
		`exec httpd -f -p 80 -h /www`

	// Signal workload: loads both secret files at start and again on SIGHUP, counting
	// reloads.
	crashSignalScript = `a=/run/secrets/a/value; b=/run/secrets/b/value
load() { cat $a > /tmp/la.new && mv /tmp/la.new /tmp/la; cat $b > /tmp/lb.new && mv /tmp/lb.new /tmp/lb; }
trap 'exit 0' TERM; trap 'load; echo r >> /tmp/reloads' HUP
until [ -s $a ] && [ -s $b ]; do sleep 0.2; done
load; echo started > /tmp/started
while true; do sleep 1 & wait $!; done`
)

func repoRoot() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(f)))
}

type crashArtifacts struct {
	dso       string // dsofault-tagged agent binary
	pluginDir string
	manifest  string
}

// buildCrashArtifacts builds the dsofault-tagged dso binary and the real vault
// provider plugin into dir, and writes the plugin hash manifest.
func buildCrashArtifacts(t *testing.T, sess *dockerguard.Session, dir string) crashArtifacts {
	t.Helper()
	env, err := harness.SanitizedEnv(os.Environ(), sess.Config.Host, nil)
	if err != nil {
		t.Fatal(err)
	}
	build := func(out string, args ...string) {
		cmd := exec.Command("go", append([]string{"build", "-o", out}, args...)...) // #nosec G204 -- fixed tool
		cmd.Dir, cmd.Env = repoRoot(), env
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %v: %v\n%s", args, err, b)
		}
	}
	// t.TempDir creates numbered subdirectories as 0777 &^ umask. The agent's plugin
	// trust check (G012) rejects a group-writable directory chain, so the harness must not
	// depend on the caller's umask (0002 is the default on many systems).
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	a := crashArtifacts{dso: filepath.Join(dir, "dso-fault"), pluginDir: filepath.Join(dir, "plugins")}
	if err := os.MkdirAll(a.pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(a.pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	build(a.dso, "-tags", "dsofault", "./cmd/dso")
	plugin := filepath.Join(a.pluginDir, "dso-provider-vault")
	build(plugin, "./cmd/plugins/dso-provider-vault")
	// go build honours the umask; the trust check rejects a group-writable plugin binary.
	for _, f := range []string{a.dso, plugin} {
		if err := os.Chmod(f, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(plugin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	a.manifest = filepath.Join(dir, "hashes.txt")
	if err := os.WriteFile(a.manifest, []byte("dso-provider-vault="+hex.EncodeToString(sum[:])+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return a
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// crashCase is one deployment under test: a workload, the real agent (restartable
// on the same state directory), a Vault stub that doubles as the rotation trigger,
// and bystanders that must never be touched.
type crashCase struct {
	t        *testing.T
	sess     *dockerguard.Session
	arts     crashArtifacts
	strategy harness.Strategy
	scenario harness.Scenario

	root     harness.Root
	vault    *harness.VaultStub
	token    string
	owner    string
	service  string
	caseTag  string
	proxyPrt int
	apiPrt   int
	metrPrt  int

	origID     string
	bystander  string
	foreign    string
	agents     []*crashAgent
	generator  *harness.Generator
	secretKeys []string // vault secret names of this case
}

type crashAgent struct {
	n      int
	proc   *harness.Proc
	log    string
	marker string
	start  time.Time
}

func (c *crashCase) proxied() bool { return c.strategy == harness.Rolling }

func newCrashCase(t *testing.T, sess *dockerguard.Session, arts crashArtifacts, strategy harness.Strategy, scenario harness.Scenario) *crashCase {
	t.Helper()
	c := &crashCase{t: t, sess: sess, arts: arts, strategy: strategy, scenario: scenario}
	root, err := harness.NewRoot(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.root = root
	c.token = "vt-" + randHex(8)
	c.vault = harness.NewVaultStub(c.token)
	c.owner = "dso-crash-" + randHex(4)
	c.service = uniqueName(t, "dso-it-crash-"+string(strategy))
	c.caseTag = randHex(6)
	c.proxyPrt, c.apiPrt, c.metrPrt = freePort(t), freePort(t), freePort(t)

	t.Cleanup(func() {
		for _, a := range c.agents {
			_ = a.proc.TerminateGroup(5 * time.Second)
		}
		if c.generator != nil {
			c.generator.Finish()
		}
		c.vault.Close()
		c.removeContainers()
		_ = os.RemoveAll(c.root.Dir) // the harness' own unique temp directory
	})

	// PollInterval is short because the agent adopts the FIRST observation of a secret without
	// rotating (M1 applied-version ledger): arm() publishes a baseline, waits for adoption, then
	// publishes the rotated value, which only a later poll can see.
	cfg := harness.AgentConfig{OwnerID: c.owner, VaultAddr: c.vault.Addr(), VaultToken: c.token, HealthTimeout: "45s", PollInterval: "3s"}
	switch strategy {
	case harness.Signal:
		cfg.Secrets = []harness.SecretSpec{
			{Name: crashSecretA, Mode: "file", Path: "/run/secrets/a", UID: crashAppUID, GID: crashAppUID, Strategy: harness.Signal, Signal: "SIGHUP"},
			{Name: crashSecretB, Mode: "file", Path: "/run/secrets/b", UID: crashAppUID, GID: crashAppUID, Strategy: harness.Signal, Signal: "SIGHUP"},
		}
		c.secretKeys = []string{crashSecretA, crashSecretB}
	default:
		cfg.Secrets = []harness.SecretSpec{{Name: crashSecret, Mode: "env", Strategy: strategy, Mapping: map[string]string{"password": "APP_PASSWORD"}}}
		c.secretKeys = []string{crashSecret}
	}
	if err := c.root.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}
	c.startWorkloads()
	return c
}

// ---- Docker side (guard session only) ------------------------------------------------

func (c *crashCase) labels(extra ...string) []string {
	args := append([]string{}, c.sess.LabelArgs()...)
	args = append(args, "--label", "dso.it.case="+c.caseTag)
	return append(args, extra...)
}

func (c *crashCase) docker(args ...string) string {
	c.t.Helper()
	out, err := c.sess.Docker(c.t, args...)
	if err != nil {
		redact := func(s string) string { return strings.ReplaceAll(s, sentinel, "<redacted>") }
		c.t.Fatalf("docker %s: %v\n%s", redact(strings.Join(args, " ")), err, redact(out))
	}
	return out
}

func (c *crashCase) dockerTry(args ...string) (string, error) {
	return c.sess.Docker(c.t, args...)
}

func (c *crashCase) startWorkloads() {
	t := c.t
	managed := []string{"--label", "dso.reloader=true", "--label", "dso.owner=" + c.owner, "--label", "dso.update.strategy=" + string(c.strategy)}
	health := []string{"--health-cmd", "test ! -f /tmp/bad", "--health-interval", "1s", "--health-retries", "1"}

	switch c.strategy {
	case harness.Signal:
		args := []string{"run", "-d", "--name", c.service}
		args = append(args, c.labels(append(managed, "--label", "dso.secrets="+crashSecretA+","+crashSecretB)...)...)
		args = append(args, "--user", fmt.Sprintf("%d:%d", crashAppUID, crashAppUID),
			"--tmpfs", fmt.Sprintf("/run/secrets/a:rw,uid=%d,gid=%d,mode=0750", crashAppUID, crashAppUID),
			"--tmpfs", fmt.Sprintf("/run/secrets/b:rw,uid=%d,gid=%d,mode=0750", crashAppUID, crashAppUID),
			testImage, "sh", "-c", crashSignalScript)
		c.origID = c.docker(args...)
		// the application's initial (old) secret files, as the first delivery would leave them
		for _, p := range []string{"a", "b"} {
			c.docker("exec", c.origID, "sh", "-c", fmt.Sprintf("umask 077; printf %%s old-%s > /run/secrets/%s/value", p, p))
		}
		waitFor(t, "signal app started", 30*time.Second, func() bool { _, err := c.dockerTry("exec", c.origID, "cat", "/tmp/started"); return err == nil })
	default:
		extra := append([]string{}, managed...)
		extra = append(extra, "--label", "dso.secrets="+crashSecret)
		if c.proxied() {
			// exactly what the compose transformation does in production: the host
			// binding is stripped into a label so the DSO proxy owns it
			extra = append(extra, "--label", fmt.Sprintf("dso.host_ports=%d:80", c.proxyPrt))
		}
		args := []string{"run", "-d", "--name", c.service}
		args = append(args, c.labels(extra...)...)
		args = append(args, health...)
		if c.scenario == harness.ScenarioCandidateUnhealthy {
			args = append(args, "-e", "FAIL_READINESS=1")
		}
		args = append(args, testImage, "sh", "-c", crashHTTPScript)
		c.origID = c.docker(args...)
		waitFor(t, "workload healthy", 60*time.Second, func() bool {
			out, err := c.dockerTry("inspect", "-f", "{{.State.Health.Status}}", c.origID)
			return err == nil && out == "healthy"
		})
	}

	// An unrelated container (guard labels only: it is a test resource, but not DSO's).
	c.bystander = c.docker(append(append([]string{"run", "-d", "--name", c.service + "-bystander"}, c.labels()...),
		testImage, "sh", "-c", "trap 'exit 0' TERM; while true; do sleep 1 & wait $!; done")...)
	// A FOREIGN-owned look-alike: DSO labels of a DIFFERENT agent and a rotation-artifact
	// name of this service. Recovery must neither remove nor stop it.
	c.foreign = c.docker(append(append([]string{"run", "-d", "--name", c.service + "_dso_backup_999"},
		c.labels("--label", "dso.reloader=true", "--label", "dso.owner=someone-else-"+randHex(3), "--label", "dso.secrets="+c.secretKeys[0], "--label", "dso.update.strategy="+string(c.strategy))...),
		testImage, "sh", "-c", "trap 'exit 0' TERM; while true; do sleep 1 & wait $!; done")...)
}

// removeContainers removes this case's containers: every container labelled with the
// case tag, and any container named after this case's unique service name (rotation
// artifacts created by the agent keep the case label because the clone copies labels,
// but the name match covers a clone that did not).
func (c *crashCase) removeContainers() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cli := c.sess.Client
	f := filters.NewArgs()
	f.Add("label", "dso.it.case="+c.caseTag)
	byLabel, _ := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	all, _ := cli.ContainerList(ctx, container.ListOptions{All: true})
	seen := map[string]bool{}
	rm := func(id string) {
		if !seen[id] {
			seen[id] = true
			_ = cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: true})
		}
	}
	for _, x := range byLabel {
		rm(x.ID)
	}
	for _, x := range all {
		for _, n := range x.Names {
			n = strings.TrimPrefix(n, "/")
			if n == c.service || strings.HasPrefix(n, c.service+"_") || strings.HasPrefix(n, c.service+"-") {
				rm(x.ID)
			}
		}
	}
}

// snapshot reads the case's containers and journal. withRouting also measures which
// containers answer real traffic through the proxy.
func (c *crashCase) snapshot(withRouting bool) harness.Snapshot {
	t := c.t
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	list, err := c.sess.Client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	var snap harness.Snapshot
	idents := map[string]string{} // identity -> container ID
	for _, x := range list {
		name := ""
		if len(x.Names) > 0 {
			name = strings.TrimPrefix(x.Names[0], "/")
		}
		if x.Labels["dso.it.case"] != c.caseTag && !(name == c.service || strings.HasPrefix(name, c.service+"_") || strings.HasPrefix(name, c.service+"-")) {
			continue
		}
		in, err := c.sess.Client.ContainerInspect(ctx, x.ID)
		if err != nil {
			continue // vanished between list and inspect
		}
		v := harness.CView{ID: x.ID, Name: name, Running: in.State != nil && in.State.Running, Owner: x.Labels["dso.owner"], RotationID: x.Labels["dso.rotation.id"]}
		if in.State != nil && in.State.Health != nil {
			v.Health = in.State.Health.Status
		}
		snap.Containers = append(snap.Containers, v)
		if withRouting && v.Running && c.proxied() {
			if id, err := c.dockerTry("exec", x.ID, "cat", "/www/index.html"); err == nil {
				idents[strings.TrimSpace(id)] = x.ID
			}
		}
	}
	sort.Slice(snap.Containers, func(i, j int) bool { return snap.Containers[i].ID < snap.Containers[j].ID })
	recs, err := harness.LoadJournal(c.root.State)
	if err == nil {
		snap.Journal = recs
	}
	if withRouting && c.proxied() {
		seen := map[string]bool{}
		for i := 0; i < 40; i++ {
			s := harness.OneRequest(c.proxyAddr())
			if s.OK {
				id, ok := idents[s.Backend]
				if !ok {
					id = "unknown-identity:" + s.Backend
				}
				if !seen[id] {
					seen[id] = true
					snap.RoutedTo = append(snap.RoutedTo, id)
				}
			}
			time.Sleep(25 * time.Millisecond)
		}
		sort.Strings(snap.RoutedTo)
	}
	return snap
}

func (c *crashCase) proxyAddr() string { return fmt.Sprintf("127.0.0.1:%d", c.proxyPrt) }

// ---- the agent process --------------------------------------------------------------

// startAgent launches the real agent. point "" runs it without a fault armed.
func (c *crashCase) startAgent(point string, nth int) *crashAgent {
	t := c.t
	t.Helper()
	n := len(c.agents) + 1
	extra := map[string]string{
		"HOME":                     c.root.Home,
		"DSO_PLUGIN_DIR":           c.arts.pluginDir,
		"DSO_PLUGIN_HASH_MANIFEST": c.arts.manifest,
		"DSO_FAULT_PLUGIN_DIR":     c.arts.pluginDir,
		"DSO_FAULT_STATE_DIR":      c.root.State,
		"DSO_FAULT_LOCK_DIR":       c.root.Lock,
		"DSO_FAULT_LEDGER_DIR":     c.root.Ledger,
		"DSO_AUTH_TOKEN":           "at-" + randHex(16),
	}
	env, err := harness.SanitizedEnv(os.Environ(), c.sess.Config.Host, extra)
	if err != nil {
		t.Fatal(err)
	}
	marker := c.root.MarkerPath(n)
	if point != "" {
		fe, err := harness.FaultEnv(point, nth, marker)
		if err != nil {
			t.Fatal(err)
		}
		env = append(env, fe...)
	}
	if d := harness.DockerVars(env); len(d) != 1 || d[0] != "DOCKER_HOST="+c.sess.Config.Host {
		t.Fatalf("agent environment carries unexpected DOCKER_* variables: %v", d)
	}
	p, err := harness.Start(harness.Spec{
		Path: c.arts.dso, Args: c.root.AgentArgs(fmt.Sprintf("127.0.0.1:%d", c.apiPrt), fmt.Sprintf("127.0.0.1:%d", c.metrPrt)),
		Dir: c.root.Cfg, Env: env, LogPath: c.root.AgentLog(n),
	})
	if err != nil {
		t.Fatalf("start agent: %v", err)
	}
	a := &crashAgent{n: n, proc: p, log: c.root.AgentLog(n), marker: marker, start: time.Now()}
	c.agents = append(c.agents, a)
	t.Logf("agent #%d started: pid=%d armed=%q", n, p.PID(), point)
	// A failed test must carry the agent's own account of why (the log is removed with the
	// temp dir). Secret sentinels are redacted by logTail.
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("--- agent #%d log tail (on failure) ---\n%s", a.n, logTail(a.log, 40))
			saveFailureEvidence(t, a, c.root)
		}
	})
	return a
}

// saveFailureEvidence copies the failed run's COMPLETE agent log (a 40-line tail is not
// enough to attribute a container removal to a code path) and the rotation journal into
// $DSO_IT_ARTIFACTS/crash/<test>/, which the CI job uploads. Secret sentinels are
// redacted; the journal holds identifiers and transitions only. Best effort: evidence
// collection never changes a test's outcome.
func saveFailureEvidence(t *testing.T, a *crashAgent, root harness.Root) {
	dir := os.Getenv("DSO_IT_ARTIFACTS")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) { // go test runs in the package directory; the script's path is repo-root relative
		wd, err := os.Getwd()
		if err != nil {
			return
		}
		for r := wd; ; r = filepath.Dir(r) {
			if _, err := os.Stat(filepath.Join(r, "go.mod")); err == nil {
				dir = filepath.Join(r, dir)
				break
			}
			if filepath.Dir(r) == r {
				return
			}
		}
	}
	out := filepath.Join(dir, "crash", strings.NewReplacer("/", "_", "#", "_").Replace(t.Name()))
	if err := os.MkdirAll(out, 0o750); err != nil {
		return
	}
	if b, err := os.ReadFile(a.log); err == nil { // #nosec G304 -- harness-owned log
		_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("agent-%d.log", a.n)), []byte(strings.ReplaceAll(string(b), sentinel, "<redacted>")), 0o600)
	}
	if b, err := os.ReadFile(filepath.Join(root.State, "rotations.json")); err == nil { // #nosec G304 -- harness-owned journal
		_ = os.WriteFile(filepath.Join(out, "rotations.json"), b, 0o600)
	}
}

func logContains(path, sub string) bool {
	b, err := os.ReadFile(path) // #nosec G304 -- harness-owned log
	return err == nil && strings.Contains(string(b), sub)
}

func logTail(path string, n int) string {
	b, _ := os.ReadFile(path) // #nosec G304 -- harness-owned log
	lines := strings.Split(string(b), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.ReplaceAll(strings.Join(lines, "\n"), sentinel, "<redacted>")
}

// waitReady waits for "DSO Agent is now running": printed only after startup
// recovery (StartAll) has finished. It fails if the agent dies first.
func (c *crashCase) waitReady(a *crashAgent) time.Time {
	t := c.t
	t.Helper()
	deadline := time.Now().Add(crashReadyWait)
	for time.Now().Before(deadline) {
		if logContains(a.log, readyMarker) {
			return time.Now()
		}
		select {
		case <-a.proc.Done():
			info, _ := a.proc.Exit()
			t.Fatalf("agent #%d ended before it was ready (%s)\n%s", a.n, info, logTail(a.log, 40))
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatalf("agent #%d not ready within %v\n%s", a.n, crashReadyWait, logTail(a.log, 40))
	return time.Time{}
}

// waitProxyServing waits until the proxy answers a request (rolling only).
func (c *crashCase) waitProxyServing() {
	waitFor(c.t, "proxy serving the workload", 60*time.Second, func() bool { return harness.OneRequest(c.proxyAddr()).OK })
}

// awaitFault waits for the agent to die at point and verifies that the process the
// harness started is the one that died, by SIGKILL, at exactly that point. It returns
// the instant the death was observed, whether the verification held, and a non-empty
// reason when the point was never reached.
func (c *crashCase) awaitFault(a *crashAgent, point string) (death time.Time, verified bool, notReached string) {
	t := c.t
	t.Helper()
	select {
	case <-a.proc.Done():
	case <-time.After(crashFaultWait):
		return time.Time{}, false, fmt.Sprintf("point %s not reached within %v (agent still running)", point, crashFaultWait)
	}
	death = time.Now()
	info, _ := a.proc.Exit()
	marker, err := os.ReadFile(a.marker) // #nosec G304 -- harness-owned marker
	if err != nil {
		return death, false, fmt.Sprintf("agent #%d ended (%s) without reaching %s\n%s", a.n, info, point, logTail(a.log, 30))
	}
	verified = true
	if err := harness.VerifyFault(point, a.proc.PID(), info, marker); err != nil {
		verified = false
		t.Errorf("fault verification: %v", err)
	}
	// An orphaned provider plugin of the dead agent: same process group, harness-started.
	if err := a.proc.KillStragglers(); err != nil {
		t.Logf("straggler cleanup: %v", err)
	}
	return death, verified, ""
}

// stopGracefully SIGTERMs the agent it started (identity verified) and waits.
func (c *crashCase) stopGracefully(a *crashAgent) {
	t := c.t
	t.Helper()
	if err := a.proc.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal agent #%d: %v", a.n, err)
	}
	if _, err := a.proc.WaitExit(90 * time.Second); err != nil {
		t.Fatalf("%v\n%s", err, logTail(a.log, 30))
	}
	_ = a.proc.KillStragglers()
}

// ---- triggers and observations -------------------------------------------------------

// arm publishes the rotated secret(s) in the Vault stub: the agent's next poll sees
// them and starts a rotation. For the signal strategy the two secrets are armed in
// order, the second only after the first was read AND given time to enter the agent's
// cache, so the single rotation that follows sees both.
func (c *crashCase) arm() {
	t := c.t
	if c.strategy == harness.Signal {
		// baseline: the first observation of each secret is adopted, not rotated
		c.vault.Set(crashSecretA, map[string]string{"value": syntheticValue("a1")})
		c.vault.Set(crashSecretB, map[string]string{"value": syntheticValue("b1")})
		waitFor(t, "agent to adopt the baseline signal secrets", 30*time.Second, func() bool {
			return c.vault.Reads(crashSecretA) > 0 && c.vault.Reads(crashSecretB) > 0
		})
		time.Sleep(5 * time.Second)
		c.vault.Set(crashSecretB, map[string]string{"value": syntheticValue("b2")})
		waitFor(t, "agent to read the first signal secret", 30*time.Second, func() bool { return c.vault.Reads(crashSecretB) > 1 })
		time.Sleep(2 * time.Second)
		c.vault.Set(crashSecretA, map[string]string{"value": syntheticValue("a2")})
		return
	}
	c.vault.Set(crashSecret, map[string]string{"password": syntheticValue("v1")})
	waitFor(t, "agent to adopt the baseline secret", 30*time.Second, func() bool { return c.vault.Reads(crashSecret) > 0 })
	time.Sleep(5 * time.Second)
	c.vault.Set(crashSecret, map[string]string{"password": syntheticValue("v2")})
}

// disarm makes every secret absent again so a restarted agent's poll fails (404) and
// cannot start a NEW rotation while recovery is being observed.
func (c *crashCase) disarm() {
	for _, n := range c.secretKeys {
		c.vault.Remove(n)
	}
}

func (c *crashCase) signalEvidence() (files map[string]string, reloads int) {
	files = map[string]string{}
	for _, p := range []string{"a", "b"} {
		if out, err := c.dockerTry("exec", c.origID, "cat", "/run/secrets/"+p+"/value"); err == nil {
			files[p] = out
		}
	}
	reloads = -1
	if out, err := c.dockerTry("exec", c.origID, "sh", "-c", "wc -l < /tmp/reloads 2>/dev/null || echo 0"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			reloads = n
		}
	}
	return
}

func (c *crashCase) world() harness.World {
	return harness.World{Service: c.service, OwnerID: c.owner, Strategy: c.strategy, Proxied: c.proxied(), OriginalID: c.origID,
		Bystanders: []string{c.bystander}, Foreign: []string{c.foreign}}
}

// leakScan greps every file the agents wrote (logs, journal, locks, config, sockets
// directory) for the synthetic secret sentinel.
func (c *crashCase) leakScan() []harness.Finding {
	f, err := harness.ScanTree(c.root.Dir, sentinel, nil)
	if err != nil {
		c.t.Errorf("leak scan could not read everything (unscanned is not clean): %v", err)
	}
	return f
}
