package harness

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func goBuild(t *testing.T, out string, tags string) {
	t.Helper()
	goBuildPkgTags(t, out, tags, "./cmd/dso")
}

func goBuildPkg(t *testing.T, out, pkg string) { t.Helper(); goBuildPkgTags(t, out, "", pkg) }

func goBuildPkgTags(t *testing.T, out, tags, pkg string) {
	t.Helper()
	env, err := SanitizedEnv(os.Environ(), "unix:///nonexistent/go-build-only.sock", nil)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"build"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "-o", out, pkg)
	cmd := exec.Command("go", args...) // #nosec G204 -- fixed tool, test-controlled args
	cmd.Dir = repoRoot(t)
	cmd.Env = env
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, b)
	}
}

// The release binary must contain NO fault hook: not the environment-variable
// names, not the marker contract. The dsofault binary must contain them (otherwise
// the harness would be testing a binary that cannot die on cue).
func TestUntaggedBinaryHasNoFaultHookAndTaggedHasIt(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries (~1 min cold); run without -short")
	}
	dir := t.TempDir()
	plain, tagged := filepath.Join(dir, "dso"), filepath.Join(dir, "dso-fault")
	goBuild(t, plain, "")
	goBuild(t, tagged, "dsofault")
	pb, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := os.ReadFile(tagged)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"DSO_FAULT_POINT", "DSO_FAULT_MARKER", "DSO_FAULT_STATE_DIR", "DSO_FAULT_LOCK_DIR", "DSO_FAULT_PLUGIN_DIR", "DSO_FAULT_NTH"} {
		if bytes.Contains(pb, []byte(s)) {
			t.Errorf("the production binary contains %q", s)
		}
		if !bytes.Contains(tb, []byte(s)) {
			t.Errorf("the dsofault binary lacks %q", s)
		}
	}
}
