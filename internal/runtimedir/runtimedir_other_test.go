//go:build !linux

package runtimedir

import (
	"errors"
	"os"
	"testing"
)

// On platforms without descriptor-based enforcement every operation must fail
// explicitly; it must never report an unenforced directory as enforced.
func TestUnsupportedPlatformFailsExplicitly(t *testing.T) {
	d, err := Ensure("/run/dso", CanonicalPolicyFor(0))
	if err == nil || d != nil {
		t.Fatalf("Ensure must fail on this platform, got dir=%v err=%v", d, err)
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
	var none Dir
	if _, err := none.OpenRegular("events.jsonl", os.O_RDONLY, 0, FilePolicy{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OpenRegular: want ErrUnsupported, got %v", err)
	}
	if err := none.Close(); err != nil {
		t.Fatal(err)
	}
}
