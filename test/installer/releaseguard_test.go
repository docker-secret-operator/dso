package installer

// [M0-05] Release-build guard tests: a release must be unable to inherit
// -tags=dsofault, and any artifact that contains the fault hook must be rejected.
// Only `go build` is used (no Docker).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func goBuild(t *testing.T, out string, env ...string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, "./test/installer/testdata/faultprobe")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), env...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

// Builds with the fault tag injected through GOFLAGS must be rejected by the artifact
// check; an ordinary build must pass it.
func TestReleaseGuard_ArtifactInspection(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	faulty, clean := filepath.Join(dir, "dso-faulty"), filepath.Join(dir, "dso-clean")
	goBuild(t, faulty, "GOFLAGS=-tags=dsofault")
	goBuild(t, clean, "GOFLAGS=")
	script := filepath.Join(root, "scripts/check-no-fault-hook.sh")

	out, code := run(t, root, os.Environ(), "sh", script, faulty)
	if code == 0 || !strings.Contains(out, "fault hook") {
		t.Fatalf("a dsofault build must be rejected (exit %d): %s", code, out)
	}
	out, code = run(t, root, os.Environ(), "sh", script, clean)
	if code != 0 {
		t.Fatalf("a normal build must pass (exit %d): %s", code, out)
	}
	// one bad artifact among good ones fails the whole check; a missing one fails too
	if _, code = run(t, root, os.Environ(), "sh", script, clean, faulty); code == 0 {
		t.Fatal("must fail if ANY artifact contains the hook")
	}
	if _, code = run(t, root, os.Environ(), "sh", script, clean, filepath.Join(dir, "missing")); code == 0 {
		t.Fatal("a missing artifact cannot be inspected and must fail")
	}
	// the same check must also work with busybox's grep (the Alpine builder image)
	if bb, err := exec.LookPath("busybox"); err == nil {
		if _, code = run(t, root, os.Environ(), bb, "sh", script, faulty); code == 0 {
			t.Fatal("busybox: a dsofault build must be rejected")
		}
		if _, code = run(t, root, os.Environ(), bb, "sh", script, clean); code != 0 {
			t.Fatal("busybox: a normal build must pass")
		}
	}
}

// The environment guard must catch -tags arriving through GOFLAGS or the `go env -w`
// file (an empty GOFLAGS variable does NOT override the file, so it is asserted, not
// merely reset).
func TestReleaseGuard_EnvironmentAssertion(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts/check-release-env.sh")
	envFile := filepath.Join(t.TempDir(), "goenv")
	if err := os.WriteFile(envFile, []byte("GOFLAGS=-tags=dsofault\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := func(extra ...string) []string {
		var e []string
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "GOFLAGS=") || strings.HasPrefix(kv, "GOENV=") {
				continue
			}
			e = append(e, kv)
		}
		return append(e, extra...)
	}
	cases := []struct {
		name string
		env  []string
		ok   bool
	}{
		{"clean environment", base(), true},
		{"GOFLAGS=-tags=dsofault in the environment", base("GOFLAGS=-tags=dsofault"), false},
		{"GOFLAGS=-tags dsofault (space form)", base("GOFLAGS=-tags dsofault"), false},
		{"go env -w file carries -tags=dsofault", base("GOENV=" + envFile), false},
		{"empty GOFLAGS does not override the go env file", base("GOFLAGS=", "GOENV="+envFile), false},
		{"GOENV=off neutralises the file", base("GOFLAGS=", "GOENV=off"), true},
		{"unrelated flag is still refused in a release", base("GOFLAGS=-mod=mod"), false},
	}
	for _, c := range cases {
		out, code := run(t, root, c.env, "bash", script)
		if (code == 0) != c.ok {
			t.Errorf("%s: exit %d, want ok=%v\n%s", c.name, code, c.ok, out)
		}
	}
	// scripts/build-release.sh must refuse before building anything.
	out, code := run(t, root, base("GOFLAGS=-tags=dsofault"), "bash", filepath.Join(root, "scripts/build-release.sh"), "v0.0.0-test")
	if code == 0 || !strings.Contains(out, "GOFLAGS is set") {
		t.Errorf("build-release.sh must refuse an inherited GOFLAGS (exit %d): %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "build", "dso-v0.0.0-test")); err == nil {
		t.Error("build-release.sh created build output despite the guard")
	}
	out, code = run(t, root, base("GOENV="+envFile), "bash", filepath.Join(root, "scripts/build-release.sh"), "v0.0.0-test")
	if code == 0 || !strings.Contains(out, "go env GOFLAGS") {
		t.Errorf("build-release.sh must refuse flags supplied by the go env file (exit %d): %s", code, out)
	}
}
