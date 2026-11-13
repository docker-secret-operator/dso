package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller info")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(f)))
}

// sourcePoints extracts every fault point name from the production call sites.
func sourcePoints(t *testing.T) map[string]bool {
	t.Helper()
	root := repoRoot(t)
	found := map[string]bool{}
	grab := func(rel string, re *regexp.Regexp, prefix string) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			found[prefix+m[1]] = true
		}
	}
	grab("internal/rotation/rolling_strategy.go", regexp.MustCompile(`rs\.fault\("([a-z_]+)"\)`), "rolling:")
	for _, f := range []string{"internal/watcher/controller.go", "internal/watcher/signal_rotation.go"} {
		grab(f, regexp.MustCompile(`r\.fault\("([a-z]+:[a-z_]+)"\)`), "")
		grab(f, regexp.MustCompile(`r\.fault\(fmt\.Sprintf\("(signal:after_spec_)%d(_published)"`), "") // handled below
	}
	for _, f := range []string{"internal/agent/state_tracker.go", "internal/agent/recovery.go"} {
		grab(f, regexp.MustCompile(`faultinject\.Hit\("([a-z]+:[a-z_]+)"\)`), "")
	}
	// The signal strategy publishes spec N: the family is instrumented with the index.
	for k := range found {
		if k == "signal:after_spec_" || k == "after_spec_" {
			delete(found, k)
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, "internal/watcher/signal_rotation.go"))
	if strings.Contains(string(b), `"signal:after_spec_%d_published"`) {
		found["signal:after_spec_0_published"] = true
		found["signal:after_spec_1_published"] = true
	}
	return found
}

func TestRegistryMatchesSource(t *testing.T) {
	src := sourcePoints(t)
	if len(src) < 25 {
		t.Fatalf("source scan found only %d points: the scanner is broken", len(src))
	}
	reg := map[string]bool{}
	for _, p := range Points {
		if err := ValidatePointName(p.Name); err != nil {
			t.Error(err)
		}
		reg[p.Name] = true
	}
	var missingFromSource, missingFromRegistry []string
	for n := range reg {
		if !src[n] {
			missingFromSource = append(missingFromSource, n)
		}
	}
	for n := range src {
		if !reg[n] {
			missingFromRegistry = append(missingFromRegistry, n)
		}
	}
	sort.Strings(missingFromSource)
	sort.Strings(missingFromRegistry)
	if len(missingFromSource) > 0 {
		t.Errorf("registered points that do not exist in the production code: %v", missingFromSource)
	}
	if len(missingFromRegistry) > 0 {
		t.Errorf("instrumented points missing from the matrix registry: %v", missingFromRegistry)
	}
}

func TestMatrixShape(t *testing.T) {
	cases := Matrix()
	seen := map[string]bool{}
	per := map[Strategy]int{}
	for _, c := range cases {
		if seen[c.ID()] {
			t.Errorf("duplicate case id %s", c.ID())
		}
		seen[c.ID()] = true
		per[c.Strategy]++
		if c.Point.Scenario == ScenarioCrashDuringRecovery {
			if c.Point.Base == "" {
				t.Errorf("%s: recovery-crash case without a base crash point", c.ID())
			}
			found := false
			for _, p := range Points {
				if p.Name == c.Point.Base {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: base %q is not a registered point", c.ID(), c.Point.Base)
			}
		}
		// a point must never be registered for a strategy whose code cannot reach it
		switch {
		case strings.HasPrefix(c.Point.Name, "rolling:") && c.Strategy != Rolling,
			strings.HasPrefix(c.Point.Name, "signal:") && c.Strategy != Signal,
			strings.HasPrefix(c.Point.Name, "restart:") && c.Strategy != Restart:
			t.Errorf("%s: point belongs to a different strategy", c.ID())
		}
	}
	for _, s := range []Strategy{Rolling, Signal, Restart} {
		if per[s] < 10 {
			t.Errorf("strategy %s has only %d cases", s, per[s])
		}
	}
}

func TestFilterCases(t *testing.T) {
	all := Matrix()
	got, err := FilterCases(all, `^signal/`)
	if err != nil || len(got) == 0 {
		t.Fatalf("%v %d", err, len(got))
	}
	for _, c := range got {
		if c.Strategy != Signal {
			t.Fatalf("filter leaked %s", c.ID())
		}
	}
	if g, _ := FilterCases(all, ""); len(g) != len(all) {
		t.Fatal("empty filter must keep all")
	}
	if _, err := FilterCases(all, "("); err == nil {
		t.Fatal("bad regexp accepted")
	}
}

func TestFaultEnvAndNameValidation(t *testing.T) {
	env, err := FaultEnv("rolling:after_intent_swap", 1, "/m")
	if err != nil || len(env) != 2 || env[0] != "DSO_FAULT_POINT=rolling:after_intent_swap" {
		t.Fatalf("%v %v", env, err)
	}
	env, _ = FaultEnv("state:after_persist_rename", 4, "/m")
	if env[len(env)-1] != "DSO_FAULT_NTH=4" {
		t.Fatalf("%v", env)
	}
	for _, bad := range []string{"", "x", "rolling:", "Rolling:x", "a:b c", "a:b;rm", "../x"} {
		if _, err := FaultEnv(bad, 1, "/m"); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestVerifyFault(t *testing.T) {
	killed := ExitInfo{Signaled: true, Signal: syscall.SIGKILL}
	marker := []byte("point=rolling:after_intent_swap\npid=4242\nnth=1\n")
	if err := VerifyFault("rolling:after_intent_swap", 4242, killed, marker); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		point string
		pid   int
		exit  ExitInfo
		m     []byte
	}{
		"other point": {"rolling:after_intent_cleanup", 4242, killed, marker},
		"other pid":   {"rolling:after_intent_swap", 1, killed, marker},
		"clean exit":  {"rolling:after_intent_swap", 4242, ExitInfo{Exited: true}, marker},
		"term only":   {"rolling:after_intent_swap", 4242, ExitInfo{Signaled: true, Signal: syscall.SIGTERM}, marker},
		"no marker":   {"rolling:after_intent_swap", 4242, killed, nil},
		"junk marker": {"rolling:after_intent_swap", 4242, killed, []byte("nonsense")},
	}
	for name, c := range cases {
		if err := VerifyFault(c.point, c.pid, c.exit, c.m); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
