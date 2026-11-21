package agent

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// (13) Standalone `docker dso inject` (admin injection into an arbitrary container) must
// not touch managed ledger state. It runs in the operator's own process, talks to
// Docker directly and never reaches the agent's engine, journal or ledger: the
// ledger is owned (flock) by the agent process and only the engine's managed
// operations (ExecuteRotationDecision, RotateSecretManually) and the startup
// reconcile write it. This pins that structurally: neither the CLI command nor the
// injector package imports the ledger or this package, or names the managed entry
// points.
func TestStandaloneInject_NeverTouchesManagedLedgerState(t *testing.T) {
	var files []string
	for _, pat := range []string{"../cli/inject.go", "../cli/inject_*.go", "../injector/*.go"} {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 2 {
		t.Fatalf("expected the inject sources, found %v", files)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		checked++
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasSuffix(p, "/internal/ledger") || strings.HasSuffix(p, "/internal/agent") {
				t.Errorf("%s imports %s: standalone injection must not reach managed state", f, p)
			}
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"RotateSecretManually", "ExecuteRotation", "RecordRotationOutcome", "ReconcileLedger", "StartRotationForDispatch"} {
			if strings.Contains(string(src), name) {
				t.Errorf("%s references %s", f, name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test inject sources checked")
	}
}
