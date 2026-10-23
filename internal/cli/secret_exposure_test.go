package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/api"
)

const fetchCanary = "SYNTHETIC-FETCH-CANARY"

func writeFetchConfig(t *testing.T) {
	t.Helper()
	body := "version: v1\nagent:\n  mode: local\nproviders:\n  vault:\n    type: vault\n    config:\n      address: http://127.0.0.1:1\nsecrets:\n  - name: app-db\n    provider: vault\n    inject:\n      type: env\n    mappings:\n      PASSWORD: PASSWORD\n"
	if err := os.WriteFile("dso.yaml", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFetch_MasksByDefaultAndRevealsOnlyOnRequest(t *testing.T) {
	setupResolution(t, false, true, false) // creates ./dso.yaml in an isolated cwd
	writeFetchConfig(t)
	startFakeAgent(t, api.StatusResponse{})

	code, out, errOut := runCLI("fetch", "app-db")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.Contains(out+errOut, fetchCanary) {
		t.Fatal("fetch printed a secret value without --reveal")
	}
	if !strings.Contains(out, "PASSWORD: ***") {
		t.Errorf("expected masked value, got %q", out)
	}

	code, out, _ = runCLI("fetch", "app-db", "--reveal")
	if code != 0 || !strings.Contains(out, fetchCanary) {
		t.Errorf("--reveal is the documented way to print values: code %d out %q", code, out)
	}
}

func TestFetch_UnknownSecretErrorNamesNoValues(t *testing.T) {
	setupResolution(t, false, true, false)
	writeFetchConfig(t)
	startFakeAgent(t, api.StatusResponse{})
	code, out, errOut := runCLI("fetch", "nope")
	if !strings.Contains(errOut, "not found in config") {
		t.Errorf("must fail for the intended reason, got %q", errOut)
	}
	if code != 1 || strings.Contains(out+errOut, fetchCanary) {
		t.Errorf("code %d, leaked=%v", code, strings.Contains(out+errOut, fetchCanary))
	}
}

func TestExport_RefusesOverwriteAndWritesOwnerOnlyFile(t *testing.T) {
	setupResolution(t, false, true, false)
	writeFetchConfig(t)
	startFakeAgent(t, api.StatusResponse{})

	code, out, errOut := runCLI("export", "-o", ".env.test")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.Contains(out+errOut, fetchCanary) {
		t.Fatal("export echoed secret values to the terminal")
	}
	st, err := os.Stat(filepath.Join(".", ".env.test"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("export file: %v mode %v", err, st)
	}
	if code, _, _ := runCLI("export", "-o", ".env.test"); code == 0 {
		t.Error("second export must refuse to overwrite without --overwrite")
	}
}

func TestSecretCommandsDoNotPrintValuesOnFailure(t *testing.T) {
	isolatedVault(t)
	// list never prints values
	if err := runSecretSet(t, "p/k", []byte("SYNTHETIC-LIST-CANARY")); err != nil {
		t.Fatal(err)
	}
	_, out, errOut := runCLI("secret", "list", "p")
	if strings.Contains(out+errOut, "SYNTHETIC-LIST-CANARY") {
		t.Error("secret list printed a value")
	}
	// traversal rejected without echoing input value
	if err := runSecretSet(t, "p/../x", []byte("SYNTHETIC-LIST-CANARY")); err == nil || strings.Contains(err.Error(), "SYNTHETIC") {
		t.Errorf("want rejection without leaking, got %v", err)
	}
}
