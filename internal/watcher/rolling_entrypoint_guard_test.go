package watcher

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Entry-point guard (M0-02): every route into rolling replacement (agent poll,
// REST rotate, CLI sync, provider events, manual rotation) reaches
// ReloaderController.TriggerReload. This test makes the safety checks impossible to
// bypass by accident: the rolling strategy may be constructed ONLY in the controller,
// and there only after the eligibility and journal checks.
func TestRollingStrategyIsOnlyReachableThroughTheEligibilityCheckedPath(t *testing.T) {
	root := "../.."
	var users []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "/node_modules/") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(b), "NewRollingStrategy(") || strings.Contains(string(b), "NewRollingStrategyWithLogger(") {
			if !strings.HasSuffix(filepath.ToSlash(p), "internal/rotation/rolling_strategy.go") {
				users = append(users, filepath.ToSlash(p))
			}
		}
		return nil
	})
	if len(users) != 1 || !strings.HasSuffix(users[0], "internal/watcher/controller.go") {
		t.Fatalf("the rolling strategy must be constructed only by the controller, found: %v", users)
	}
	b, err := os.ReadFile("controller.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	ctor := strings.Index(src, "NewRollingStrategyWithLogger(")
	for _, must := range []string{"rollingEligibility(inspect)", "r.RequireJournal && r.Journal == nil"} {
		i := strings.Index(src, must)
		if i < 0 || i > ctor {
			t.Fatalf("%q must run before the rolling strategy is constructed", must)
		}
	}
	if strings.Count(src, "NewRollingStrategyWithLogger(") != 1 {
		t.Fatal("exactly one construction site is allowed")
	}
}
