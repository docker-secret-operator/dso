package config

// Initialisation of agent.owner_id: concurrency (goroutines and processes),
// persistence failures, metadata preservation, and the guarantee that plain
// LoadConfig never writes the config.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const ownerHelperEnv = "DSO_OWNER_ID_TEST_HELPER_DIR"

// TestMain lets this test binary double as the "second DSO process" for the
// cross-process test (re-executed with ownerHelperEnv set).
func TestMain(m *testing.M) {
	if dir := os.Getenv(ownerHelperEnv); dir != "" {
		os.Exit(ownerIDHelper(dir))
	}
	os.Exit(m.Run())
}

// ownerIDHelper: announce readiness, wait for the parent's start signal on
// stdin (so every process starts at the same instant, with no sleeps), then
// initialise and print the id it got.
func ownerIDHelper(dir string) int {
	if err := os.Chdir(dir); err != nil {
		fmt.Println("ERR:", err)
		return 1
	}
	fmt.Println("ready")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	cfg, err := LoadConfigWithOwnerID("dso.yaml")
	if err != nil {
		fmt.Println("ERR:", err)
		return 1
	}
	fmt.Println("ID:" + cfg.Agent.OwnerID)
	return 0
}

func writeCfg(t *testing.T, content string, mode os.FileMode) (dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dso.yaml"), []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "dso.yaml"), mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

// minimalCfg passes Validate (the load-path tests need a loadable config).
const minimalCfg = `# keep this comment
providers:
  default:
    type: vault
secrets:
  - name: test
    provider: default
    inject:
      type: env
    mappings:
      key: VAL
agent:
  mode: local
`

func persistedID(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("dso.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var p ownerIDProbe
	if err := yamlUnmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p.Agent.OwnerID
}

func TestEnsureOwnerID_ConcurrentGoroutinesConverge(t *testing.T) {
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)

	const n = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	ids := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			c := &Config{}
			errs[i] = EnsureOwnerID(c, "dso.yaml")
			ids[i] = c.Agent.OwnerID
		}(i)
	}
	close(start)
	wg.Wait()

	want := persistedID(t)
	if want == "" {
		t.Fatal("no owner id persisted")
	}
	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if ids[i] != want {
			t.Fatalf("caller %d got %q, the persisted canonical id is %q", i, ids[i], want)
		}
	}
	if st, _ := os.Stat("dso.yaml"); st.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 0640", st.Mode().Perm())
	}
	if b, _ := os.ReadFile("dso.yaml"); !bytes.Contains(b, []byte("# keep this comment")) {
		t.Error("the config's comments were lost")
	}
}

func TestEnsureOwnerID_SeparateProcessesConverge(t *testing.T) {
	if runtimeIsNotUnix() {
		t.Skip("cross-process mutual exclusion needs flock(2); on this platform the lock is a documented no-op")
	}
	dir := writeCfg(t, minimalCfg, 0o640)

	const n = 8
	type proc struct {
		cmd *exec.Cmd
		in  *bufio.Writer
		out *bufio.Scanner
		w   interface{ Close() error }
	}
	procs := make([]proc, n)
	for i := range procs {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), ownerHelperEnv+"="+dir)
		stdin, _ := cmd.StdinPipe()
		stdout, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		procs[i] = proc{cmd: cmd, in: bufio.NewWriter(stdin), out: bufio.NewScanner(stdout), w: stdin}
	}
	for i, p := range procs { // wait until every process is up, then release them together
		if !p.out.Scan() || p.out.Text() != "ready" {
			t.Fatalf("process %d did not become ready: %q", i, p.out.Text())
		}
	}
	for _, p := range procs {
		_, _ = p.in.WriteString("\n")
		_ = p.in.Flush()
		_ = p.w.Close()
	}
	got := map[string]int{}
	for i, p := range procs {
		var id string
		for p.out.Scan() {
			if v, ok := strings.CutPrefix(p.out.Text(), "ID:"); ok {
				id = v
			} else if strings.HasPrefix(p.out.Text(), "ERR:") {
				t.Fatalf("process %d: %s", i, p.out.Text())
			}
		}
		if err := p.cmd.Wait(); err != nil {
			t.Fatalf("process %d: %v", i, err)
		}
		got[id]++
	}
	t.Chdir(dir)
	want := persistedID(t)
	if len(got) != 1 || got[want] != n {
		t.Fatalf("processes disagree: %v (persisted %q)", got, want)
	}
	if st, _ := os.Stat("dso.yaml"); st.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 0640", st.Mode().Perm())
	}
}

func TestEnsureOwnerID_ExistingIDIsNeverOverwritten(t *testing.T) {
	content := minimalCfg + "  owner_id: dso-established-id\n"
	dir := writeCfg(t, content, 0o640)
	t.Chdir(dir)

	// The caller's in-memory config is stale (no id), the file already has one.
	c := &Config{}
	if err := EnsureOwnerID(c, "dso.yaml"); err != nil {
		t.Fatal(err)
	}
	if c.Agent.OwnerID != "dso-established-id" {
		t.Errorf("adopted %q, want the established id", c.Agent.OwnerID)
	}
	if b, _ := os.ReadFile("dso.yaml"); string(b) != content {
		t.Errorf("the config was rewritten although an id existed:\n%s", b)
	}
}

func TestEnsureOwnerID_MalformedConfigIsLeftUntouched(t *testing.T) {
	for name, content := range map[string]string{
		"unterminated flow":   "agent: [unclosed\n",
		"owner_id not text":   "agent:\n  owner_id: [1, 2]\n",
		"agent not a map":     "agent: just-a-string\n",
		"top level is a list": "- a\n- b\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeCfg(t, content, 0o640)
			t.Chdir(dir)
			c := &Config{}
			if err := EnsureOwnerID(c, "dso.yaml"); err == nil {
				t.Fatal("expected an error")
			}
			if c.Agent.OwnerID != "" {
				t.Errorf("an id %q was returned for a config that could not be updated", c.Agent.OwnerID)
			}
			if b, _ := os.ReadFile("dso.yaml"); string(b) != content {
				t.Errorf("the malformed config was modified:\n%s", b)
			}
			assertNoTempFiles(t, dir)
		})
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dir, ".dso-owner-id-*.tmp"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// Persistence failure: no id may be reported as successful. Two independent
// failure injections, neither depends on running as non-root.
func TestEnsureOwnerID_PersistenceFailureNeverReturnsAnID(t *testing.T) {
	t.Run("lock unavailable", func(t *testing.T) {
		dir := writeCfg(t, minimalCfg, 0o640)
		t.Chdir(dir)
		if err := os.Mkdir(filepath.Join(dir, ownerIDLockFile), 0o700); err != nil { // lock path is a directory
			t.Fatal(err)
		}
		c := &Config{}
		if err := EnsureOwnerID(c, "dso.yaml"); err == nil || c.Agent.OwnerID != "" {
			t.Fatalf("err=%v id=%q: a failure must surface and leave the id empty", err, c.Agent.OwnerID)
		}
		if b, _ := os.ReadFile("dso.yaml"); string(b) != minimalCfg {
			t.Error("the config changed although persistence failed")
		}
		if _, err := LoadConfigWithOwnerID("dso.yaml"); err == nil {
			t.Error("LoadConfigWithOwnerID must fail when the id cannot be persisted")
		}
	})
	t.Run("atomic replace fails", func(t *testing.T) {
		dir := writeCfg(t, minimalCfg, 0o640)
		t.Chdir(dir)
		old := osRename
		osRename = func(string, string) error { return errors.New("injected rename failure") }
		t.Cleanup(func() { osRename = old })

		c := &Config{}
		if err := EnsureOwnerID(c, "dso.yaml"); err == nil || c.Agent.OwnerID != "" {
			t.Fatalf("err=%v id=%q", err, c.Agent.OwnerID)
		}
		if b, _ := os.ReadFile("dso.yaml"); string(b) != minimalCfg {
			t.Error("the original config was not left intact")
		}
		assertNoTempFiles(t, dir)
	})
}

func TestEnsureOwnerID_ModeIsTightenOnly(t *testing.T) {
	for _, tc := range []struct{ in, want os.FileMode }{{0o664, 0o640}, {0o644, 0o640}, {0o600, 0o600}, {0o640, 0o640}, {0o755, 0o640}} {
		dir := writeCfg(t, minimalCfg, tc.in)
		t.Chdir(dir)
		if err := EnsureOwnerID(&Config{}, "dso.yaml"); err != nil {
			t.Fatalf("%o: %v", tc.in, err)
		}
		if st, _ := os.Stat("dso.yaml"); st.Mode().Perm() != tc.want {
			t.Errorf("input %o: mode %o, want %o", tc.in, st.Mode().Perm(), tc.want)
		}
	}
}

// A symlinked config stays a symlink; the file it points at is updated.
func TestEnsureOwnerID_SymlinkedConfigStaysASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.yaml")
	if err := os.WriteFile(real, []byte(minimalCfg), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.yaml", filepath.Join(dir, "dso.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(dir)
	c := &Config{}
	if err := EnsureOwnerID(c, "dso.yaml"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat("dso.yaml"); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file")
	}
	if b, _ := os.ReadFile(real); !bytes.Contains(b, []byte(c.Agent.OwnerID)) {
		t.Error("the symlink target was not updated")
	}
}

// ---- LoadConfig is read-only ----------------------------------------------

// Root-independent proof: neither the rename hook nor the lock file is touched.
func TestLoadConfig_NeverWrites(t *testing.T) {
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)
	before, _ := os.Stat("dso.yaml")
	raw, _ := os.ReadFile("dso.yaml")

	old := osRename
	osRename = func(string, string) error { t.Error("LoadConfig attempted to replace the config"); return nil }
	t.Cleanup(func() { osRename = old })

	time.Sleep(10 * time.Millisecond) // so an mtime change would be visible
	cfg, err := LoadConfig("dso.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.OwnerID != "" {
		t.Errorf("a read-only load invented an owner id: %q", cfg.Agent.OwnerID)
	}
	after, _ := os.Stat("dso.yaml")
	if got, _ := os.ReadFile("dso.yaml"); !bytes.Equal(got, raw) {
		t.Error("config bytes changed")
	}
	if !after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
		t.Error("config mtime or inode changed")
	}
	if _, err := os.Stat(filepath.Join(dir, ownerIDLockFile)); err == nil {
		t.Error("a read-only load created the lock file")
	}
	assertNoTempFiles(t, dir)
}

// Real permission check, only meaningful for a non-root user (root bypasses
// mode bits); the root-independent test above covers that case.
func TestLoadConfig_WorksOnReadOnlyConfigAndDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file modes do not restrict writes, see TestLoadConfig_NeverWrites")
	}
	dir := writeCfg(t, minimalCfg, 0o440)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	t.Chdir(dir)

	if _, err := LoadConfig("dso.yaml"); err != nil {
		t.Fatalf("read-only load of an unwritable config failed: %v", err)
	}
	// The same file cannot be initialised: a clear, surfaced error.
	if _, err := LoadConfigWithOwnerID("dso.yaml"); err == nil || !strings.Contains(err.Error(), "agent.owner_id") {
		t.Fatalf("expected an actionable error, got %v", err)
	}
	if b, _ := os.ReadFile("dso.yaml"); string(b) != minimalCfg {
		t.Error("the unwritable config changed")
	}
}

func TestLoadConfigWithOwnerID_PersistsOnceAndIsStable(t *testing.T) {
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)
	a, err := LoadConfigWithOwnerID("dso.yaml")
	if err != nil || a.Agent.OwnerID == "" {
		t.Fatalf("first load: id=%q err=%v", a.Agent.OwnerID, err)
	}
	st1, _ := os.Stat("dso.yaml")
	time.Sleep(10 * time.Millisecond)
	b, err := LoadConfigWithOwnerID("dso.yaml")
	if err != nil || b.Agent.OwnerID != a.Agent.OwnerID {
		t.Fatalf("second load: id=%q err=%v, want %q", b.Agent.OwnerID, err, a.Agent.OwnerID)
	}
	if st2, _ := os.Stat("dso.yaml"); !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("the config was rewritten although the id already existed")
	}
	// A plain read afterwards sees the persisted id.
	if r, _ := LoadConfig("dso.yaml"); r.Agent.OwnerID != a.Agent.OwnerID {
		t.Errorf("LoadConfig sees %q, want %q", r.Agent.OwnerID, a.Agent.OwnerID)
	}
}

func TestLoadConfig_MissingFileStillFailsAndCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, load := range []func(string) (*Config, error){LoadConfig, LoadConfigWithOwnerID} {
		if _, err := load("dso.yaml"); err == nil {
			t.Fatal("a missing config must be an error")
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files were created for a missing config: %v", entries)
	}
}

func yamlUnmarshal(b []byte, v interface{}) error { return yaml.Unmarshal(b, v) }

func runtimeIsNotUnix() bool { return runtime.GOOS == "windows" || runtime.GOOS == "plan9" }
