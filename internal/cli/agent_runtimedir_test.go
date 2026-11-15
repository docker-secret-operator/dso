package cli

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/runtimedir"
)

// None of these tests touches the real /run/dso: the enforcement function is
// replaced by a recorder.

type ensureCall struct {
	path   string
	policy runtimedir.Policy
}

func stubEnsure(t *testing.T, ret error) *[]ensureCall {
	t.Helper()
	var calls []ensureCall
	old := ensureRuntimeDirectory
	ensureRuntimeDirectory = func(path string, p runtimedir.Policy) (*runtimedir.Dir, error) {
		calls = append(calls, ensureCall{path, p})
		return nil, ret
	}
	t.Cleanup(func() { ensureRuntimeDirectory = old })
	return &calls
}

func TestEstablishAgentRuntimeDir_RootEstablishesTheInvariantOnTheCanonicalDirectory(t *testing.T) {
	calls := stubEnsure(t, nil)
	core, _ := observer.New(zap.DebugLevel)
	if err := establishAgentRuntimeDir(zap.New(core), 0); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("want exactly one enforcement call, got %d", len(*calls))
	}
	c := (*calls)[0]
	if c.path != "/run/dso" {
		t.Fatalf("enforced %q, want /run/dso", c.path)
	}
	p := c.policy
	if !p.Enforce || !p.Create || p.OwnerUID != 0 || p.GroupName != "dso" || p.Mode != 0o750 || !p.RequireNoGroupOtherWrite {
		t.Fatalf("a root agent must establish root:dso 0750, got %+v", p)
	}
}

func TestEstablishAgentRuntimeDir_RootFailsStartupWhenTheInvariantCannotBeEstablished(t *testing.T) {
	cause := &runtimedir.UnsafeError{Path: "/run/dso", Reason: "is a symbolic link"}
	stubEnsure(t, cause)
	err := establishAgentRuntimeDir(zap.NewNop(), 0)
	if err == nil {
		t.Fatal("startup must fail when the runtime directory is unsafe")
	}
	if !errors.Is(err, runtimedir.ErrUnsafe) {
		t.Fatalf("the cause must stay in the chain: %v", err)
	}
	if !strings.Contains(err.Error(), "/run/dso") || !strings.Contains(err.Error(), "0750") {
		t.Fatalf("the message should name the directory and the invariant: %v", err)
	}
}

func TestEstablishAgentRuntimeDir_UnsupportedPlatformWarnsInsteadOfClaimingSuccess(t *testing.T) {
	stubEnsure(t, runtimedir.ErrUnsupported)
	core, logs := observer.New(zap.DebugLevel)
	if err := establishAgentRuntimeDir(zap.New(core), 0); err != nil {
		t.Fatalf("an unsupported platform must not abort a development agent: %v", err)
	}
	warned := false
	for _, e := range logs.All() {
		if e.Level == zap.WarnLevel && strings.Contains(e.Message, "cannot be enforced") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("the lack of enforcement must be logged, not silent")
	}
}

func TestEstablishAgentRuntimeDir_NonRootNeverTouchesTheCanonicalDirectory(t *testing.T) {
	calls := stubEnsure(t, nil)
	core, logs := observer.New(zap.DebugLevel)
	if err := establishAgentRuntimeDir(zap.New(core), 1000); err != nil {
		t.Fatalf("a non-root development agent must still start: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a non-root process must not enforce or modify the canonical directory (%d calls)", len(*calls))
	}
	if logs.FilterLevelExact(zap.WarnLevel).Len() == 0 {
		t.Fatal("running as non-root must be reported")
	}
}

// ---- the enforcement is reached before anything uses the directory ----

func funcCalls(t *testing.T, file, fn string) map[string]token.Pos {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var decl *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == fn {
			decl = fd
		}
	}
	if decl == nil {
		t.Fatalf("%s not found in %s", fn, file)
	}
	first := map[string]token.Pos{}
	ast.Inspect(decl, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			if x, ok := fun.X.(*ast.Ident); ok {
				name = x.Name + "." + fun.Sel.Name
			}
		}
		if name != "" {
			if p, seen := first[name]; !seen || call.Pos() < p {
				first[name] = call.Pos()
			}
		}
		return true
	})
	return first
}

func TestAgentRuntimeDirIsEstablishedBeforeAnythingUsesIt(t *testing.T) {
	calls := funcCalls(t, "agent.go", "NewAgentCmd")
	enforce, ok := calls["establishAgentRuntimeDir"]
	if !ok {
		t.Fatal("NewAgentCmd never calls establishAgentRuntimeDir")
	}
	// trigger engine, IPC socket, Docker driver socket, and (through the REST
	// server) the event store.
	for _, name := range []string{
		"agent.NewTriggerEngine",
		"agent.StartSocketServer",
		"agent.StartDriverServer",
		"server.StartRESTServerSupervised",
	} {
		pos, found := calls[name]
		if !found {
			t.Fatalf("%s is no longer called from NewAgentCmd: update this test deliberately", name)
		}
		if enforce > pos {
			t.Errorf("establishAgentRuntimeDir must run before %s", name)
		}
	}
}

// Every production path that can create the socket, the trigger engine or the
// event store must be reached only through the one place that enforces the
// runtime directory. A new caller anywhere else would bypass it, so this
// fails when one appears.
func TestAllAgentStartupPathsGoThroughTheEnforcement(t *testing.T) {
	targets := map[string]bool{
		"StartSocketServer":         true,
		"StartDriverServer":         true,
		"StartRESTServer":           true,
		"StartRESTServerSupervised": true,
		"NewTriggerEngine":          true,
		"NewEventStore":             true,
	}
	root := filepath.Join("..", "..")
	found := map[string]map[string]bool{}

	for _, dir := range []string{"internal", "cmd", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				ast.Inspect(fd, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					var name string
					switch fun := call.Fun.(type) {
					case *ast.Ident:
						name = fun.Name
					case *ast.SelectorExpr:
						name = fun.Sel.Name
					}
					if targets[name] {
						if found[name] == nil {
							found[name] = map[string]bool{}
						}
						found[name][rel+":"+fd.Name.Name] = true
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	want := map[string][]string{
		"StartSocketServer":         {"internal/cli/agent.go:NewAgentCmd"},
		"StartDriverServer":         {"internal/cli/agent.go:NewAgentCmd"},
		"NewTriggerEngine":          {"internal/cli/agent.go:NewAgentCmd"},
		"StartRESTServerSupervised": {"internal/cli/agent.go:NewAgentCmd", "internal/server/rest.go:StartRESTServer"},
		"NewEventStore":             {"internal/server/rest.go:StartRESTServerSupervised"},
		"StartRESTServer":           nil,
	}
	for name := range targets {
		var got []string
		for k := range found[name] {
			got = append(got, k)
		}
		sort.Strings(got)
		exp := append([]string(nil), want[name]...)
		sort.Strings(exp)
		if strings.Join(got, ",") != strings.Join(exp, ",") {
			t.Errorf("%s is called from %v, expected only %v: a new startup path may bypass the runtime-directory enforcement",
				name, got, exp)
		}
	}
}
