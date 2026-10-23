package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go/ast"

	"github.com/docker-secret-operator/dso/pkg/api"
)

// Only the process entrypoint may terminate the process; everything else must
// return errors so deferred cleanup runs and commands stay testable.
func TestNoOsExitOutsideEntrypoint(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "root.go" {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" && sel.Sel.Name == "Exit" {
						t.Errorf("%s: os.Exit call; return an error instead", fset.Position(c.Pos()))
					}
				}
			}
			return true
		})
	}
}

func TestConvertedCommands_FailOnceOnStderrWithEmptyStdout(t *testing.T) {
	setupResolution(t, false, false, false) // no config anywhere (system path redirected to a temp dir)
	t.Setenv("DSO_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	for _, args := range [][]string{
		{"fetch"},
		{"fetch", "x"},
		{"export"},
		{"diff"},
		{"inspect", "--bogus"},
	} {
		code, out, errOut := runCLI(args...)
		if code != 1 {
			t.Errorf("dso %v: exit %d, want 1", args, code)
		}
		if strings.Count(errOut, "Error:") != 1 {
			t.Errorf("dso %v: error must appear exactly once on stderr, got %q", args, errOut)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("dso %v: diagnostics leaked to stdout: %q", args, out)
		}
		if strings.Contains(errOut, "Usage:") {
			t.Errorf("dso %v: usage dumped for a runtime failure", args)
		}
	}
}

func TestDown_RejectsShellMetacharactersWithoutRunningDocker(t *testing.T) {
	code, _, errOut := runCLI("down", "a;b")
	if code != 1 || errOut == "" {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

func TestDown_HelpDescribesActualDestructiveness(t *testing.T) {
	c := NewDownCmd()
	if strings.Contains(c.Short, "images, and volumes") {
		t.Error("Short still claims images and volumes are removed by default")
	}
	for _, want := range []string{"ONLY", "--volumes", "DATA LOSS", "--rmi"} {
		if !strings.Contains(c.Long, want) {
			t.Errorf("down help missing %q", want)
		}
	}
}

func TestSecretGet_NotFoundIsReportedOnceAndGlobalDefaultIsAnnounced(t *testing.T) {
	isolatedVault(t)
	code, out, errOut := runCLI("secret", "get", "missing")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	if n := strings.Count(errOut, "secret not found"); n != 1 {
		t.Errorf("'secret not found' appears %d times: %q", n, errOut)
	}
	if !strings.Contains(errOut, `default project "global"`) {
		t.Errorf("implicit global default must be announced on stderr: %q", errOut)
	}
	if out != "" {
		t.Errorf("stdout must stay empty: %q", out)
	}

	// Explicit project: no note. Existing global secrets stay readable both ways.
	if err := runSecretSet(t, "legacy", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if got := runSecretGet(t, "global/legacy"); got != "v" {
		t.Errorf("implicit-set secret not readable as global/legacy: %q", got)
	}
	_, _, errOut = runCLI("secret", "get", "global/legacy")
	if strings.Contains(errOut, "default project") {
		t.Errorf("no note expected when project is explicit: %q", errOut)
	}
}

func TestNoColor_StripsANSIFromColouredOutput(t *testing.T) {
	old := noColor
	defer func() { noColor = old }()
	noColor = true
	if got := stripIfNoColor("\x1b[1;32m[OK]\x1b[0m ready"); got != "[OK] ready" {
		t.Errorf("got %q", got)
	}
	noColor = false
	t.Setenv("NO_COLOR", "1")
	if got := stripIfNoColor("\x1b[31mx\x1b[0m"); got != "x" {
		t.Errorf("NO_COLOR not honoured: %q", got)
	}
	t.Setenv("NO_COLOR", "")
	if got := stripIfNoColor("\x1b[31mx\x1b[0m"); !strings.Contains(got, "\x1b[") {
		t.Error("colour should be kept when not disabled")
	}
	if NewRootCmd().PersistentFlags().Lookup("no-color") == nil {
		t.Error("--no-color flag missing")
	}
}

func TestStatus_QuietPrintsNothingButKeepsExitCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DSO_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	code, out, errOut := runCLI("status", "-q")
	if code != 1 || out != "" {
		t.Errorf("unavailable: exit %d stdout %q", code, out)
	}
	if !strings.Contains(errOut, "unavailable") {
		t.Errorf("the single error line still belongs on stderr: %q", errOut)
	}
	startFakeAgent(t, api.StatusResponse{})
	if code, out, errOut := runCLI("status", "--quiet"); code != 0 || out != "" || errOut != "" {
		t.Errorf("healthy: exit %d stdout %q stderr %q", code, out, errOut)
	}
}

type statusFailWriter struct{}

func (statusFailWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic write failure") }

func TestStatus_JSONWriteFailureIsReturned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startFakeAgent(t, api.StatusResponse{})
	err := (&Status{JSON: true, Out: statusFailWriter{}}).Run()
	if err == nil || !strings.Contains(err.Error(), "synthetic write failure") {
		t.Fatalf("want write error propagated, got %v", err)
	}
}

// JSON-mode contract for every --json command: stdout is exactly one JSON
// document; anything else goes to stderr.
func TestJSONModes_StdoutIsPureJSON(t *testing.T) {
	setupResolution(t, false, false, false)
	t.Setenv("DSO_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	for _, args := range [][]string{{"status", "--json"}, {"config", "path", "--json"}} {
		var out, errOut bytes.Buffer
		Run(args, &out, &errOut)
		var v map[string]any
		dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
		if err := dec.Decode(&v); err != nil {
			t.Errorf("%v: stdout is not JSON: %v\n%s", args, err, out.String())
			continue
		}
		if dec.More() {
			t.Errorf("%v: trailing data after the JSON document", args)
		}
	}
}

// Schema pin: renaming or dropping these keys is a breaking change for scripts.
func TestStatusJSON_SchemaKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startFakeAgent(t, api.StatusResponse{Providers: []api.ProviderStatusInfo{{Name: "p", Known: true, Healthy: true}}})
	var out bytes.Buffer
	Run([]string{"status", "--json"}, &out, &bytes.Buffer{})
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"runtime", "agent_reached", "providers", "cache", "rotations", "degraded_services", "state", "health"} {
		if _, ok := m[k]; !ok {
			t.Errorf("status JSON lost key %q", k)
		}
	}
	var rt map[string]json.RawMessage
	_ = json.Unmarshal(m["runtime"], &rt)
	for _, k := range []string{"mode", "version"} {
		if _, ok := rt[k]; !ok {
			t.Errorf("runtime lost key %q", k)
		}
	}
	for _, k := range []string{"uptime", "start_time"} {
		if _, ok := rt[k]; ok {
			t.Errorf("unverifiable runtime key %q must be omitted", k)
		}
	}
	_ = os.Stdout
}
