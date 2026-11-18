//go:build dsofault

package faultinject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withEnv(t *testing.T, kv map[string]string) (died *int) {
	t.Helper()
	n := 0
	oldGet, oldDie := getenv, die
	getenv = func(k string) string { return kv[k] }
	die = func() { n++ }
	reset()
	t.Cleanup(func() { getenv, die = oldGet, oldDie; reset() })
	return &n
}

func TestTaggedBuildFiresOnlyOnTheExactPoint(t *testing.T) {
	died := withEnv(t, map[string]string{"DSO_FAULT_POINT": "rolling:after_intent_swap"})
	if !Enabled {
		t.Fatal("Enabled must be true with the dsofault tag")
	}
	for _, p := range []string{"", "rolling:after_intent_swa", "rolling:after_intent_swap ", "ROLLING:AFTER_INTENT_SWAP", "after_intent_swap", "rolling:after_intent_cleanup"} {
		if Hit(p) {
			t.Fatalf("fired on non-matching point %q", p)
		}
	}
	if *died != 0 {
		t.Fatal("die called for a non-matching point")
	}
	if !Hit("rolling:after_intent_swap") || *died != 1 {
		t.Fatalf("did not fire on the exact point (died=%d)", *died)
	}
}

func TestTaggedBuildUnsetPointNeverFires(t *testing.T) {
	died := withEnv(t, map[string]string{})
	if Hit("rolling:after_intent_swap") || Hit("") || *died != 0 {
		t.Fatal("fired with no point configured")
	}
}

func TestTaggedBuildNthHitAndMarker(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "m")
	died := withEnv(t, map[string]string{"DSO_FAULT_POINT": "state:after_persist_rename", "DSO_FAULT_NTH": "3", "DSO_FAULT_MARKER": marker})
	if Hit("state:after_persist_rename") || Hit("state:after_persist_rename") || *died != 0 {
		t.Fatal("fired before the configured hit number")
	}
	if !Hit("state:after_persist_rename") || *died != 1 {
		t.Fatal("did not fire on hit 3")
	}
	b, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(b), "point=state:after_persist_rename") || !strings.Contains(string(b), "pid=") {
		t.Fatalf("marker missing/incomplete: %q %v", b, err)
	}
	if Hit("state:after_persist_rename") && *died != 1 {
		t.Fatal("fired a second time")
	}
}

func TestTaggedParamIsWhitelisted(t *testing.T) {
	withEnv(t, map[string]string{"DSO_FAULT_STATE_DIR": "/x/state", "DSO_FAULT_LOCK_DIR": "/x/lock", "DSO_FAULT_LEDGER_DIR": "/x/ledger", "DSO_FAULT_PLUGIN_DIR": "/x/plug", "DSO_FAULT_OTHER": "/x/o"})
	if v, ok := Param("state_dir"); !ok || v != "/x/state" {
		t.Fatalf("state_dir=%q %v", v, ok)
	}
	if v, ok := Param("lock_dir"); !ok || v != "/x/lock" {
		t.Fatalf("lock_dir=%q %v", v, ok)
	}
	if v, ok := Param("ledger_dir"); !ok || v != "/x/ledger" {
		t.Fatalf("ledger_dir=%q %v", v, ok)
	}
	if v, ok := Param("plugin_dir"); !ok || v != "/x/plug" {
		t.Fatalf("plugin_dir=%q %v", v, ok)
	}
	if _, ok := Param("other"); ok {
		t.Fatal("non-whitelisted name accepted")
	}
}
