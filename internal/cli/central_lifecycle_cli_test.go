package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// runCentral drives the real cli.Run entry point (central <args...> <db/key
// flags>) so exit statuses are the ones a shell would actually see -- the
// same pattern central_audit_verify_test.go uses for `central audit verify`.
func runCentral(e *centralEnv, args ...string) (stdout, stderr string, code int) {
	var out, errb bytes.Buffer
	full := append([]string{"central"}, args...)
	full = append(full, e.file()...)
	code = Run(full, &out, &errb)
	return out.String(), errb.String(), code
}

// decodeOneJSONDoc fails the test unless stdout is exactly one JSON document
// (no trailing garbage, no second document), as the --json contract in
// docs/cli-conventions.md requires.
func decodeOneJSONDoc(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("stdout is not JSON: %v\nstdout=%q", err, stdout)
	}
	if dec.More() {
		t.Fatalf("extra data after the first JSON document\nstdout=%q", stdout)
	}
	return v
}

func lifecycleFixture(t *testing.T) *centralEnv {
	t.Helper()
	e := newCentralEnv(t)
	if _, _, code := runCentral(e, "init"); code != 0 {
		t.Fatalf("init failed, code=%d", code)
	}
	if _, _, code := runCentral(e, "project", "create", "app", "--name", "App"); code != 0 {
		t.Fatalf("project create failed, code=%d", code)
	}
	if _, _, code := runCentral(e, "environment", "create", "app", "prod", "--name", "Prod"); code != 0 {
		t.Fatalf("environment create failed, code=%d", code)
	}
	return e
}

// ---- project rename ---------------------------------------------------------

func TestCentralProjectRename_CLI_Success(t *testing.T) {
	e := lifecycleFixture(t)

	out, errOut, code := runCentral(e, "project", "rename", "app", "app2", "--name", "App Two")
	if code != 0 {
		t.Fatalf("rename failed: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "app") || !strings.Contains(out, "app2") {
		t.Fatalf("expected human output to mention both slugs, got %q", out)
	}

	listOut, _, code := runCentral(e, "project", "list")
	if code != 0 {
		t.Fatalf("project list failed: code=%d", code)
	}
	if !strings.Contains(listOut, "app2") || !strings.Contains(listOut, "App Two") {
		t.Fatalf("expected renamed project in listing, got:\n%s", listOut)
	}

	// The old slug must be gone: environment commands against it fail.
	if _, _, code := runCentral(e, "environment", "list", "app"); code == 0 {
		t.Fatal("old project slug still resolves after rename")
	}
	// The new slug must work end-to-end (environment survives the rename).
	envOut, _, code := runCentral(e, "environment", "list", "app2")
	if code != 0 || !strings.Contains(envOut, "prod") {
		t.Fatalf("environment not reachable under the renamed project: code=%d out=%q", code, envOut)
	}
}

func TestCentralProjectRename_CLI_DefaultKeepsCurrentName(t *testing.T) {
	e := lifecycleFixture(t)

	if _, errOut, code := runCentral(e, "project", "rename", "app", "app3"); code != 0 {
		t.Fatalf("rename without --name failed: code=%d stderr=%s", code, errOut)
	}
	listOut, _, _ := runCentral(e, "project", "list")
	if !strings.Contains(listOut, "App") {
		t.Fatalf("expected the original display name 'App' to survive a rename with no --name, got:\n%s", listOut)
	}
}

func TestCentralProjectRename_CLI_SlugCollisionCleanError(t *testing.T) {
	e := lifecycleFixture(t)
	if _, _, code := runCentral(e, "project", "create", "other"); code != 0 {
		t.Fatal("project create other failed")
	}
	out, errOut, code := runCentral(e, "project", "rename", "app", "other")
	if code == 0 {
		t.Fatal("expected a slug collision to be rejected")
	}
	if out != "" {
		t.Fatalf("expected no stdout on a rejected rename, got %q", out)
	}
	for _, leak := range []string{"SQL", "sqlite3", ".go:", "goroutine", "SELECT", "panic"} {
		if strings.Contains(errOut, leak) {
			t.Fatalf("error leaked internal detail %q: %s", leak, errOut)
		}
	}
}

// ---- project delete ---------------------------------------------------------

func TestCentralProjectDelete_CLI_NonEmptyRejected(t *testing.T) {
	e := lifecycleFixture(t) // "app" has environment "prod"

	out, errOut, code := runCentral(e, "project", "delete", "app")
	if code == 0 {
		t.Fatalf("expected a non-empty project delete to be rejected")
	}
	if out != "" {
		t.Fatalf("expected no stdout on a rejected delete, got %q", out)
	}
	if !strings.Contains(errOut, "Error:") {
		t.Fatalf("expected one Error: line on stderr, got %q", errOut)
	}
	for _, leak := range []string{"SQL", "sqlite3", ".go:", "goroutine", "SELECT", "panic"} {
		if strings.Contains(errOut, leak) {
			t.Fatalf("error leaked internal detail %q: %s", leak, errOut)
		}
	}

	// The project (and its environment) must survive the rejected delete.
	listOut, _, _ := runCentral(e, "project", "list")
	if !strings.Contains(listOut, "app") {
		t.Fatalf("project must survive a rejected delete, got:\n%s", listOut)
	}
	envOut, _, code := runCentral(e, "environment", "list", "app")
	if code != 0 || !strings.Contains(envOut, "prod") {
		t.Fatalf("environment must survive a rejected project delete: code=%d out=%q", code, envOut)
	}
}

func TestCentralProjectDelete_CLI_JSONCarriesDocumentOnFailure(t *testing.T) {
	e := lifecycleFixture(t)

	out, errOut, code := runCentral(e, "project", "delete", "app", "--json")
	if code == 0 {
		t.Fatal("expected a non-empty project delete to be rejected")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Fatalf("expected one Error: line on stderr, got %q", errOut)
	}
	doc, ok := decodeOneJSONDoc(t, out).(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object, got %T: %v", doc, doc)
	}
	if deleted, _ := doc["deleted"].(bool); deleted {
		t.Fatalf("expected deleted=false for a rejected delete, got %v", doc)
	}
	if errMsg, _ := doc["error"].(string); errMsg == "" {
		t.Fatalf("expected a non-empty error field in the JSON document, got %v", doc)
	}
	if doc["slug"] != "app" {
		t.Fatalf("expected slug=app in the JSON document, got %v", doc)
	}
}

func TestCentralProjectDelete_CLI_EmptySucceeds(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, code := runCentral(e, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if _, _, code := runCentral(e, "project", "create", "lonely"); code != 0 {
		t.Fatal("project create failed")
	}
	out, errOut, code := runCentral(e, "project", "delete", "lonely")
	if code != 0 {
		t.Fatalf("expected an empty project delete to succeed: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "lonely") {
		t.Fatalf("expected confirmation output, got %q", out)
	}
	listOut, _, _ := runCentral(e, "project", "list")
	if strings.Contains(listOut, "lonely") {
		t.Fatalf("project still listed after delete:\n%s", listOut)
	}
}

// ---- environment rename -----------------------------------------------------

func TestCentralEnvironmentRename_CLI_Success(t *testing.T) {
	e := lifecycleFixture(t)

	out, errOut, code := runCentral(e, "environment", "rename", "app", "prod", "staging", "--name", "Staging")
	if code != 0 {
		t.Fatalf("rename failed: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "prod") || !strings.Contains(out, "staging") {
		t.Fatalf("expected human output to mention both slugs, got %q", out)
	}

	listOut, _, code := runCentral(e, "environment", "list", "app")
	if code != 0 {
		t.Fatalf("environment list failed: code=%d", code)
	}
	if !strings.Contains(listOut, "staging") || !strings.Contains(listOut, "Staging") {
		t.Fatalf("expected renamed environment in listing, got:\n%s", listOut)
	}
	if strings.Contains(listOut, "prod") {
		t.Fatalf("old environment slug still listed:\n%s", listOut)
	}
}

func TestCentralEnvironmentRename_CLI_SlugCollisionCleanError(t *testing.T) {
	e := lifecycleFixture(t)
	if _, _, code := runCentral(e, "environment", "create", "app", "other"); code != 0 {
		t.Fatal("environment create other failed")
	}
	out, errOut, code := runCentral(e, "environment", "rename", "app", "prod", "other")
	if code == 0 {
		t.Fatal("expected a slug collision to be rejected")
	}
	if out != "" {
		t.Fatalf("expected no stdout on a rejected rename, got %q", out)
	}
	for _, leak := range []string{"SQL", "sqlite3", ".go:", "goroutine", "SELECT", "panic"} {
		if strings.Contains(errOut, leak) {
			t.Fatalf("error leaked internal detail %q: %s", leak, errOut)
		}
	}
}

// ---- environment delete ------------------------------------------------------

func TestCentralEnvironmentDelete_CLI_EmptySucceeds(t *testing.T) {
	e := lifecycleFixture(t)

	out, errOut, code := runCentral(e, "environment", "delete", "app", "prod")
	if code != 0 {
		t.Fatalf("expected an empty environment delete to succeed: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "prod") {
		t.Fatalf("expected confirmation output, got %q", out)
	}
	listOut, _, code := runCentral(e, "environment", "list", "app")
	if code != 0 {
		t.Fatalf("environment list failed after delete: code=%d", code)
	}
	if strings.Contains(listOut, "prod") {
		t.Fatalf("environment still listed after delete:\n%s", listOut)
	}
}

func TestCentralEnvironmentDelete_CLI_NonEmptyRejected(t *testing.T) {
	e := lifecycleFixture(t)
	if _, _, err := e.run(t, "v1\n", append([]string{"secret", "create", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("secret create: %v", err)
	}

	out, errOut, code := runCentral(e, "environment", "delete", "app", "prod")
	if code == 0 {
		t.Fatal("expected a non-empty environment delete to be rejected")
	}
	if out != "" {
		t.Fatalf("expected no stdout on a rejected delete, got %q", out)
	}
	if !strings.Contains(errOut, "Error:") {
		t.Fatalf("expected one Error: line on stderr, got %q", errOut)
	}
}

// ---- secret rollback --json --------------------------------------------------

const lifecycleSecretValue = "LIFECYCLE-PLAINTEXT-secret-99"

func TestCentralSecretRollback_JSONOutput(t *testing.T) {
	e := lifecycleFixture(t)
	if _, _, err := e.run(t, lifecycleSecretValue+"\n", append([]string{"secret", "create", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("secret create: %v", err)
	}
	if _, _, err := e.run(t, "v2\n", append([]string{"secret", "add-version", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("add-version: %v", err)
	}

	out, errOut, code := runCentral(e, "secret", "rollback", "app", "prod", "k", "1", "--json", "--comment", "revert")
	if code != 0 {
		t.Fatalf("rollback --json failed: code=%d stderr=%s", code, errOut)
	}
	doc, ok := decodeOneJSONDoc(t, out).(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object, got %v", doc)
	}
	for _, k := range []string{"project", "environment", "name", "from_version", "new_version"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("rollback JSON missing key %q: %v", k, doc)
		}
	}
	if nv, _ := doc["new_version"].(float64); nv != 3 {
		t.Fatalf("expected new_version=3, got %v", doc["new_version"])
	}
	if fv, _ := doc["from_version"].(float64); fv != 1 {
		t.Fatalf("expected from_version=1, got %v", doc["from_version"])
	}
	if strings.Contains(out, lifecycleSecretValue) || strings.Contains(out, "v2") {
		t.Fatalf("secret value leaked into rollback --json output: %s", out)
	}
}

// ---- project list --json: retrofit proof ------------------------------------

func TestCentralProjectList_JSONOutput(t *testing.T) {
	e := lifecycleFixture(t)

	out, errOut, code := runCentral(e, "project", "list", "--json")
	if code != 0 {
		t.Fatalf("project list --json failed: code=%d stderr=%s", code, errOut)
	}
	decoded := decodeOneJSONDoc(t, out)
	arr, ok := decoded.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("expected a one-element JSON array, got %v", decoded)
	}
	entry, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected an object entry, got %v", arr[0])
	}
	if entry["slug"] != "app" || entry["name"] != "App" {
		t.Fatalf("unexpected project entry: %v", entry)
	}
	if strings.Contains(out, "Usage:") {
		t.Fatal("usage text on stdout")
	}
}

// ---- cross-cutting: no --json command ever leaks a secret value ------------

func TestCentralJSON_NeverLeaksSecretValue(t *testing.T) {
	const marker = "DO-NOT-LEAK-marker-value-55"
	e := lifecycleFixture(t)
	// marker is the secret VALUE only. The --comment field is documented as
	// free text that is deliberately not secret and is meant to be displayed
	// (`secret versions`), so it must never carry the marker here.
	if _, _, err := e.run(t, marker+"\n", append([]string{"secret", "create", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("secret create: %v", err)
	}
	if _, _, err := e.run(t, "v2\n", append([]string{"secret", "add-version", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("add-version: %v", err)
	}

	// Success-path commands: each must actually succeed (code 0, non-empty
	// stdout) and that stdout must be exactly one JSON document. Discarding
	// the exit code here would let a regression that makes a command fail
	// outright (empty stdout) silently pass this leak check as a no-op.
	successPath := [][]string{
		{"audit", "verify", "--json"}, // the 10th --json command, already using the shared helper
		{"project", "list", "--json"},
		{"environment", "list", "app", "--json"},
		{"secret", "list", "app", "prod", "--json"},
		{"secret", "versions", "app", "prod", "k", "--json"},
		{"secret", "rollback", "app", "prod", "k", "1", "--json"},
		{"project", "rename", "app", "app-renamed", "--json"},
		{"environment", "rename", "app-renamed", "prod", "prod-renamed", "--json"},
	}
	for _, args := range successPath {
		out, errOut, code := runCentral(e, args...)
		if code != 0 {
			t.Fatalf("%v expected to succeed, got code=%d stderr=%s", args, code, errOut)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%v produced no stdout", args)
		}
		decodeOneJSONDoc(t, out)
		if strings.Contains(out, marker) {
			t.Fatalf("%v leaked the secret marker into stdout JSON: %s", args, out)
		}
		if strings.Contains(errOut, marker) {
			t.Fatalf("%v leaked the secret marker into stderr: %s", args, errOut)
		}
	}

	// Rejected-delete commands (project/environment still non-empty): per
	// docs/cli-conventions.md, `project delete`/`environment delete --json`
	// are the two commands that still carry a JSON document on a non-zero
	// exit. Assert both the failure and the document, not just "no leak".
	rejectedDeletes := [][]string{
		{"environment", "delete", "app-renamed", "prod-renamed", "--json"}, // non-empty
		{"project", "delete", "app-renamed", "--json"},                     // non-empty
	}
	for _, args := range rejectedDeletes {
		out, errOut, code := runCentral(e, args...)
		if code == 0 {
			t.Fatalf("%v expected to be rejected (non-empty), got code=0", args)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%v: non-zero exit must still carry a JSON document on stdout, got empty stdout", args)
		}
		decodeOneJSONDoc(t, out)
		if strings.Contains(out, marker) {
			t.Fatalf("%v leaked the secret marker into stdout JSON: %s", args, out)
		}
		if strings.Contains(errOut, marker) {
			t.Fatalf("%v leaked the secret marker into stderr: %s", args, errOut)
		}
	}
}
