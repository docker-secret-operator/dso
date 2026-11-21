package cli

import (
	"os"
	"strings"
	"testing"
)

// M0-13 guard: local-mode `up` must not go back to writing the resolved
// (plaintext) compose file to disk. Source-level so it cannot be bypassed by a
// code path no test happens to execute.
func TestUp_LocalModeDoesNotWriteResolvedComposeToDisk(t *testing.T) {
	b, err := os.ReadFile("up.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, forbidden := range []string{"os.CreateTemp", "os.WriteFile", "ioutil.TempFile", `"-f", tmpFile`} {
		if strings.Contains(src, forbidden) {
			t.Errorf("up.go must not use %s: resolved secrets must be piped over stdin", forbidden)
		}
	}
	if !strings.Contains(src, "core.RunComposeUpStdin") {
		t.Error("up.go must run compose through core.RunComposeUpStdin")
	}
}
