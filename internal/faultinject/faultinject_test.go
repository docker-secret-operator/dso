//go:build !dsofault

package faultinject

import (
	"os"
	"testing"
)

// The untagged (production) build has no hook: Hit never fires even when every
// fault environment variable is set, and Param never overrides a directory.
func TestUntaggedBuildHasNoHook(t *testing.T) {
	t.Setenv("DSO_FAULT_POINT", "rolling:after_intent_swap")
	t.Setenv("DSO_FAULT_STATE_DIR", t.TempDir())
	if Enabled {
		t.Fatal("Enabled must be false without the dsofault tag")
	}
	if Hit("rolling:after_intent_swap") {
		t.Fatal("stub Hit fired")
	}
	if v, ok := Param("state_dir"); ok || v != "" {
		t.Fatalf("stub Param overrode state_dir: %q %v", v, ok)
	}
	_ = os.Getenv
}
