package ledger

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Repository reference guard (design section 9, owner decision O-9).
//
// Rules:
//  1. No Go file outside internal/ledger imports this package, except the files
//     in ledgerImportAllowlist (the S4b bridge and the S6 trigger wiring).
//  2. An allowlisted importer must not reference the KEEP-UNWIRED or retired
//     surface (forbiddenOutsideNames).
//  3. Inside the package, the whole-document mutators update and
//     updateMemoryOnly are called directly only by unexported functions or by
//     the allowlisted KEEP-UNWIRED (*Store).RequestRedelivery: no exported
//     wrapper re-opens them.

// ledgerImportPath is this package's import path.
const ledgerImportPath = "github.com/docker-secret-operator/dso/internal/ledger"

// ledgerImportAllowlist lists, relative to the repository root, the only files
// outside internal/ledger that may import it. Extend it explicitly when the
// wiring slice lands; nothing else may.
var ledgerImportAllowlist = map[string]bool{
	"internal/agent/ledger_bridge.go":      true, // S4b bridge (design 12.5 step 4): journal -> ledger evidence
	"internal/agent/ledger_bridge_test.go": true, // S4b bridge tests (real Store + real StateTracker)
	"internal/agent/ledger_wiring.go":      true, // S6 trigger-engine wiring (design 12.5 step 6): the only production user
	"internal/agent/ledger_wiring_test.go": true, // S6 engine tests (real Store + real StateTracker, Docker-free)
}

// forbiddenOutsideNames may not be referenced (as a selector) by any file
// outside the package, including allowlisted importers.
var forbiddenOutsideNames = []string{
	"RequestRedelivery", "CancelReasonRestart", "EntryPartial", "Update", "UpdateMemoryOnly",
	"BeginRecovery", "CompleteRecovery", "RecoveryRecords", "Rotation", "EntryInProgress", "Intent",
}

// forbiddenOutsidePrefixes covers the Redelivery* vocabulary.
var forbiddenOutsidePrefixes = []string{"Redelivery"}

// directMutatorAllowlist names the exported functions allowed to call update or
// updateMemoryOnly directly.
var directMutatorAllowlist = map[string]bool{"Store.RequestRedelivery": true}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller information")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// outsideViolations returns the violations of rules 1 and 2 for one parsed
// file at repository-relative path rel.
func outsideViolations(rel string, f *ast.File) []string {
	var out []string
	local := ""
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != ledgerImportPath {
			continue
		}
		local = "ledger"
		if imp.Name != nil {
			local = imp.Name.Name
		}
		if !ledgerImportAllowlist[rel] {
			out = append(out, rel+": imports internal/ledger (not allowlisted; only the bridge and the S6 wiring may)")
		}
	}
	if local == "" {
		return out
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		bad := false
		for _, fn := range forbiddenOutsideNames {
			if name == fn {
				bad = true
			}
		}
		for _, p := range forbiddenOutsidePrefixes {
			if strings.HasPrefix(name, p) {
				bad = true
			}
		}
		if bad {
			out = append(out, rel+": references "+name)
		}
		return true
	})
	return out
}

func TestRepositoryReferenceGuard_NoImportersOutsideAllowlist(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	var violations []string
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == "internal/ledger", strings.HasPrefix(d.Name(), ".") && rel != ".", d.Name() == "vendor", d.Name() == "node_modules", d.Name() == "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if perr != nil {
			return nil // unparsable files are not ours to judge here
		}
		files++
		imports := false
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == ledgerImportPath {
				imports = true
			}
		}
		if imports {
			full, ferr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if ferr != nil {
				violations = append(violations, rel+": imports internal/ledger and does not parse")
				return nil
			}
			violations = append(violations, outsideViolations(rel, full)...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 50 {
		t.Fatalf("only %d Go files scanned: the guard would be vacuous", files)
	}
	sort.Strings(violations)
	for _, v := range violations {
		t.Error(v)
	}
}

// The scanner itself detects what it must (positive controls).
func TestRepositoryReferenceGuard_DetectsViolations(t *testing.T) {
	fset := token.NewFileSet()
	src := `package x
import l "` + ledgerImportPath + `"
func f(s *l.Store) { _, _ = s.RequestRedelivery(l.Key{}, "a", "b"); _ = l.RedeliveryRequested; _ = l.EntryPartial }
`
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := outsideViolations("internal/agent/other.go", f)
	if len(got) != 4 {
		t.Fatalf("want 4 violations (import, RequestRedelivery, RedeliveryRequested, EntryPartial), got %v", got)
	}
	got = outsideViolations("internal/agent/ledger_bridge.go", f)
	if len(got) != 3 {
		t.Fatalf("an allowlisted importer may import but not reference the unwired surface, got %v", got)
	}
	clean := `package x
import "` + ledgerImportPath + `"
var _ ledger.Key
`
	f, _ = parser.ParseFile(fset, "y.go", clean, parser.SkipObjectResolution)
	if got := outsideViolations("internal/agent/ledger_bridge.go", f); len(got) != 0 {
		t.Fatalf("a clean allowlisted importer passes: %v", got)
	}
}

// Rule 3: no exported wrapper around the whole-document mutators.
func TestRepositoryReferenceGuard_NoExportedMutatorWrapper(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	fset := token.NewFileSet()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	callers := 0
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				rt := fd.Recv.List[0].Type
				if se, ok := rt.(*ast.StarExpr); ok {
					rt = se.X
				}
				if id, ok := rt.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			calls := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if s, ok := c.Fun.(*ast.SelectorExpr); ok && (s.Sel.Name == "update" || s.Sel.Name == "updateMemoryOnly") {
						calls = true
					}
				}
				return true
			})
			if !calls {
				continue
			}
			callers++
			if fd.Name.IsExported() && !directMutatorAllowlist[name] {
				t.Errorf("%s: exported function %s calls update/updateMemoryOnly directly", n, name)
			}
		}
	}
	if callers == 0 {
		t.Fatal("no caller of update found: the guard would be vacuous")
	}
}
