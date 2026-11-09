package cli

import (
	"bytes"
	"encoding/json"
	"github.com/docker-secret-operator/dso/pkg/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// resolutionEnv isolates every input to config resolution: HOME, cwd, the
// system path and the -c flag state. Files contain only synthetic content.
type resolutionEnv struct{ sys, local, user string }

func setupResolution(t *testing.T, withSys, withLocal, withUser bool) resolutionEnv {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "cwd")
	for _, d := range []string{filepath.Join(home, ".dso"), cwd, filepath.Join(root, "etc")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	e := resolutionEnv{
		sys:   filepath.Join(root, "etc", "dso.yaml"),
		local: filepath.Join(cwd, "dso.yaml"),
		user:  filepath.Join(home, ".dso", "dso.yaml"),
	}
	for p, on := range map[string]bool{e.sys: withSys, e.local: withLocal, e.user: withUser} {
		if on {
			if err := os.WriteFile(p, []byte("version: v1\n# synthetic\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("HOME", home)
	t.Chdir(cwd)
	oldSys, oldCfg, oldExp := systemConfigPath, CfgFile, cfgFileExplicit
	systemConfigPath, CfgFile, cfgFileExplicit = e.sys, "dso.yaml", false
	t.Cleanup(func() { systemConfigPath, CfgFile, cfgFileExplicit = oldSys, oldCfg, oldExp })
	return e
}

func TestResolveConfig_PrecedenceIsBackwardCompatible(t *testing.T) {
	e := setupResolution(t, true, true, true)
	got := ResolveConfigSource()
	if got.Path != e.sys || got.Kind != ConfigFromSystem {
		t.Fatalf("system must still win over ./dso.yaml (compat), got %+v", got)
	}
	if len(got.Shadowed) != 2 {
		t.Errorf("expected local and user reported as shadowed, got %v", got.Shadowed)
	}
}

func TestResolveConfig_FallsThroughInOrder(t *testing.T) {
	cases := []struct {
		sys, local, user bool
		kind             string
	}{
		{false, true, true, ConfigFromLocal},
		{false, false, true, ConfigFromUser},
		{false, false, false, ConfigFromNone},
	}
	for _, c := range cases {
		setupResolution(t, c.sys, c.local, c.user)
		if got := ResolveConfigSource(); got.Kind != c.kind {
			t.Errorf("%+v: kind = %q, want %q", c, got.Kind, c.kind)
		}
	}
	setupResolution(t, false, false, false)
	if p := ResolveConfig(); p != "dso.yaml" {
		t.Errorf("missing-everywhere path = %q, want dso.yaml", p)
	}
}

func TestResolveConfig_ExplicitFlagAlwaysWins(t *testing.T) {
	e := setupResolution(t, true, true, true)
	CfgFile = "custom.yaml"
	if got := ResolveConfigSource(); got.Path != "custom.yaml" || got.Kind != ConfigFromFlag {
		t.Errorf("got %+v", got)
	}
	// The ambiguous case: -c dso.yaml passed explicitly must beat /etc.
	CfgFile, cfgFileExplicit = "dso.yaml", true
	got := ResolveConfigSource()
	if got.Path != "dso.yaml" || got.Kind != ConfigFromFlag {
		t.Errorf("explicit -c dso.yaml lost to automatic search: %+v", got)
	}
	for _, s := range got.Shadowed {
		if s == "dso.yaml" {
			t.Error("selected file must not be listed as shadowed")
		}
	}
	_ = e
}

func TestConfigPath_ReportsSourceWithoutContents(t *testing.T) {
	e := setupResolution(t, true, true, false)
	if err := os.WriteFile(e.sys, []byte("hmac_secret: SYNTHETIC-CONFIG-CANARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"config", "path"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if strings.TrimSpace(out.String()) != e.sys {
		t.Errorf("stdout should be only the path, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "source: system") || !strings.Contains(errOut.String(), "ignored (lower priority): dso.yaml") {
		t.Errorf("stderr should explain source and shadowed files: %q", errOut.String())
	}
	if strings.Contains(out.String()+errOut.String(), "SYNTHETIC-CONFIG-CANARY") {
		t.Fatal("config contents leaked")
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"config", "path", "--json"}, &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	var rep configPathReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Source != "system" || !rep.Exists || len(rep.Shadowed) != 1 {
		t.Errorf("unexpected report %+v", rep)
	}
}

func TestConfigPath_ExplicitMissingFileReported(t *testing.T) {
	setupResolution(t, true, false, false)
	var out, errOut bytes.Buffer
	if code := Run([]string{"-c", "nope.yaml", "config", "path"}, &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	if !strings.Contains(errOut.String(), "source: flag (file does not exist)") {
		t.Errorf("got %q", errOut.String())
	}
}

func TestConfigShow_MissingAndInvalidConfigFail(t *testing.T) {
	setupResolution(t, false, false, false)
	if code, _, _ := runCLI("config", "show"); code == 0 {
		t.Error("missing config must exit non-zero")
	}
	e := setupResolution(t, false, true, false)
	if err := os.WriteFile(e.local, []byte(":\n  - [unterminated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCLI("validate"); code == 0 {
		t.Error("invalid config must make validate fail")
	}
}

func TestDetectMode_ExplicitnessAndUserConfig(t *testing.T) {
	setupResolution(t, false, false, true) // only ~/.dso/dso.yaml
	if mode, reason := detectMode("", ResolveConfig()); mode != "cloud" || !strings.Contains(reason, "~/.dso/dso.yaml") {
		t.Errorf("user config should be auto-detected as cloud: %s / %s", mode, reason)
	}

	e := setupResolution(t, true, false, false) // only system file
	// A path that came from the automatic search must not be labelled explicit.
	if _, reason := detectMode("", e.sys); strings.Contains(reason, "explicit") {
		t.Errorf("auto-selected system config mislabelled as explicit: %q", reason)
	}
	CfgFile, cfgFileExplicit = "dso.yaml", true
	if _, reason := detectMode("", "dso.yaml"); strings.Contains(reason, "explicit") {
		t.Errorf("explicit but nonexistent file cannot select cloud as explicit: %q", reason)
	}
	if err := os.WriteFile("dso.yaml", []byte("version: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, reason := detectMode("", "dso.yaml"); !strings.Contains(reason, "explicit") {
		t.Errorf("-c path should be explicit: %q", reason)
	}
}

func TestDetectMode_MixedModeWarnsOnceOnStderr(t *testing.T) {
	e := setupResolution(t, false, true, false)
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".dso", "vault.enc"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mixedModeWarned = syncOnceReset()
	oldErr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	for i := 0; i < 3; i++ {
		if mode, _ := detectMode("", e.local); mode != "cloud" {
			t.Errorf("mixed config should default to cloud, got %s", mode)
		}
	}
	_ = w.Close()
	os.Stderr = oldErr
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if n := strings.Count(buf.String(), "Both local vault and cloud configuration"); n != 1 {
		t.Errorf("mixed-mode warning printed %d times, want 1: %q", n, buf.String())
	}
	if !strings.Contains(buf.String(), "--mode=local") {
		t.Error("warning should say how to choose local mode")
	}
	// explicit local override still wins and does not warn
	if mode, _ := detectMode("local", e.local); mode != "local" {
		t.Error("--mode=local must override")
	}
}

func syncOnceReset() (o sync.Once) { return }

func TestLoadConfig_UserConfigIsLoadableButOtherAbsolutePathsStillRejected(t *testing.T) {
	e := setupResolution(t, false, false, true)
	body := "version: v1\nagent:\n  mode: local\n"
	if err := os.WriteFile(e.user, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.IsSafeConfigPath(e.user); err != nil {
		t.Fatalf("user config path must be accepted: %v", err)
	}
	other := filepath.Join(filepath.Dir(e.user), "other.yaml")
	if _, err := config.IsSafeConfigPath(other); err == nil {
		t.Error("only the exact user config path is exempt; other absolute paths must stay rejected")
	}
	if _, err := config.IsSafeConfigPath(filepath.Join(filepath.Dir(e.user), "..", ".dso", "dso.yaml")); err != nil {
		// Clean() normalises to the exact path; acceptable either way, but must not escape.
		t.Logf("non-canonical spelling rejected: %v", err)
	}
}

func TestConfigShow_UnreadableFileFailsWithoutContents(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	e := setupResolution(t, false, true, false)
	if err := os.WriteFile(e.local, []byte("hmac_secret: SYNTHETIC-UNREADABLE-CANARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.local, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.local, 0o600) })
	code, out, errOut := runCLI("config", "show")
	if code == 0 || !strings.Contains(errOut, "failed to read config file") {
		t.Errorf("code %d stderr %q", code, errOut)
	}
	if strings.Contains(out+errOut, "SYNTHETIC-UNREADABLE-CANARY") {
		t.Error("contents leaked")
	}
}
