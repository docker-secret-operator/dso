package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/setup"
)

func isolatedDoctorEnv(t *testing.T) {
	t.Helper()
	setupResolution(t, false, false, false)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "no-docker.sock"))
	t.Setenv("DSO_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	t.Setenv("DSO_MASTER_KEY", "synthetic-doctor-canary-key")
}

func TestDoctorJSON_SingleDocumentNoANSINoSecretsAndExitMatchesStatus(t *testing.T) {
	isolatedDoctorEnv(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"doctor", "--json", "--no-color"}, &out, &errOut)

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out.String())
	}
	if dec.More() {
		t.Error("extra data after the JSON document")
	}
	for _, k := range []string{"overall_status", "summary", "checks"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("doctor JSON lost key %q", k)
		}
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("ANSI escapes in JSON output")
	}
	if strings.Contains(out.String()+errOut.String(), "synthetic-doctor-canary-key") {
		t.Fatal("secret from the environment leaked into doctor output")
	}
	// A valid document describing an unhealthy system must still exit non-zero.
	if doc["overall_status"] == "FAIL" && code == 0 {
		t.Error("FAIL result exited 0")
	}
	if doc["overall_status"] != "FAIL" && code != 0 {
		t.Errorf("status %v but exit %d", doc["overall_status"], code)
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Error("usage text on stdout")
	}
}

func TestDoctorAndValidate_WriterFailuresArePropagated(t *testing.T) {
	isolatedDoctorEnv(t)
	for _, asJSON := range []bool{true, false} {
		if err := runDoctorTo(context.Background(), statusFailWriter{}, "default", asJSON); err == nil || !strings.Contains(err.Error(), "synthetic write failure") {
			// a FAIL result is also an error; the write failure must come first
			t.Errorf("doctor json=%v: writer failure not propagated, got %v", asJSON, err)
		}
		checks := []setup.DoctorCheck{{ID: "X", Category: "c", Status: setup.DoctorPass, Name: "n"}}
		if err := renderValidateResultTo(statusFailWriter{}, checks, asJSON); err == nil || !strings.Contains(err.Error(), "synthetic write failure") {
			t.Errorf("validate json=%v: writer failure not propagated, got %v", asJSON, err)
		}
	}
}

func TestValidateJSON_FailingResultKeepsDocumentAndNonZeroExit(t *testing.T) {
	checks := []setup.DoctorCheck{{ID: "X", Category: "c", Status: setup.DoctorFail, Name: "n"}}
	var out bytes.Buffer
	err := renderValidateResultTo(&out, checks, true)
	if err == nil {
		t.Fatal("failing validation must return an error")
	}
	if !json.Valid(out.Bytes()) {
		t.Errorf("document missing for failing result: %q", out.String())
	}
}

func TestConfigPathJSON_NoColorAndSchema(t *testing.T) {
	setupResolution(t, true, false, false)
	var out bytes.Buffer
	if code := Run([]string{"config", "path", "--json"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatal("exit", code)
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["source"] != "system" || m["exists"] != true {
		t.Errorf("unexpected %v", m)
	}
}
