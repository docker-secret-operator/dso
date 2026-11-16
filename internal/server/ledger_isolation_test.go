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
// reloader only through the engine (the single legacy fallback is the
// engine-less struct-literal server).
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

func TestManualRotate_ReloaderOnlyInEngineLessFallback(t *testing.T) {
	src, err := os.ReadFile("rest.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "s.Reloader.TriggerReload("); n != 1 {
		t.Fatalf("rest.go calls Reloader.TriggerReload %d times; the only allowed call is the engine-less fallback of startManualRotation", n)
	}
	if !strings.Contains(string(src), "s.TriggerEngine.RotateSecretManually(") {
		t.Fatal("the manual rotation does not go through TriggerEngine.RotateSecretManually")
	}
}
