package server

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No independent ledger commit logic in the REST layer: internal/server never
// imports the applied-version ledger, and the rotation handler reaches the
// reloader only through the engine.
func TestServer_DoesNotImportTheLedger(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			if strings.Contains(imp.Path.Value, "/internal/ledger") {
				t.Errorf("%s imports %s: the REST layer must not commit ledger state", f, imp.Path.Value)
			}
		}
	}
}

// The REST layer never reaches the reloader itself: rotation executes only through
// the RotationEngine, and there is no engine-less fallback.
func TestManualRotate_NoReloaderRotationFromRESTLayer(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{".TriggerReload(", "Reloader.Trigger"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s contains %q: rotation must go only through the RotationEngine", f, banned)
			}
		}
	}
	src, err := os.ReadFile("rest.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "eng.RotateSecretManually(") {
		t.Fatal("the manual rotation does not go through RotationEngine.RotateSecretManually")
	}
}
