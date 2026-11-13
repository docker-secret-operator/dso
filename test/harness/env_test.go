package harness

import (
	"strings"
	"testing"
)

func TestSanitizedEnvOnlyEverCarriesTheGuardEndpoint(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin", "HOME=/home/x", "GOCACHE=/c",
		"DOCKER_HOST=unix:///var/run/docker.sock", // the dangerous default daemon
		"DOCKER_CONTEXT=prod", "DOCKER_CONFIG=/root/.docker", "DOCKER_TLS_VERIFY=1", "DOCKER_CERT_PATH=/x",
		"DSO_TEST_DAEMON_ID=abc", "DSO_INTEGRATION_TESTS=1", "AWS_SECRET_ACCESS_KEY=hunter2", "VAULT_TOKEN=s.x", "TESTCONTAINERS_HOST_OVERRIDE=x",
	}
	const host = "unix:///run/dso-test/docker.sock"
	env, err := SanitizedEnv(parent, host, map[string]string{"DSO_FAULT_POINT": "rolling:after_intent_swap"})
	if err != nil {
		t.Fatal(err)
	}
	if d := DockerVars(env); len(d) != 1 || d[0] != "DOCKER_HOST="+host {
		t.Fatalf("DOCKER_* in the child env: %v", d)
	}
	joined := strings.Join(env, "\n")
	for _, leaked := range []string{"hunter2", "s.x", "DSO_TEST_DAEMON_ID", "DSO_INTEGRATION_TESTS", "TESTCONTAINERS", "prod"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("ambient value %q leaked into the child environment", leaked)
		}
	}
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "DSO_FAULT_POINT=rolling:after_intent_swap") {
		t.Fatalf("expected entries missing: %v", env)
	}
}

func TestSanitizedEnvRejectsDockerOverrides(t *testing.T) {
	const host = "unix:///run/dso-test/docker.sock"
	for _, extra := range []map[string]string{
		{"DOCKER_HOST": "unix:///var/run/docker.sock"},
		{"DOCKER_CONTEXT": "x"},
		{"DOCKER_CONFIG": "/root"},
		{"A=B": "c"},
	} {
		if _, err := SanitizedEnv(nil, host, extra); err == nil {
			t.Errorf("accepted %v", extra)
		}
	}
	if _, err := SanitizedEnv(nil, host, map[string]string{"DOCKER_HOST": host}); err != nil {
		t.Fatalf("restating the guard endpoint is harmless: %v", err)
	}
	for _, bad := range []string{"", "/var/run/docker.sock", "http://x", "ssh://h"} {
		if _, err := SanitizedEnv(nil, bad, nil); err == nil {
			t.Errorf("accepted endpoint %q", bad)
		}
	}
}
