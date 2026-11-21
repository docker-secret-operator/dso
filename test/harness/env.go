// Package harness is the Docker-free orchestration logic of the real-process
// crash-recovery tests (test/docker, build tag dockerint): environment
// sanitising, child-process launch and PID-scoped termination, fault-point
// selection, sentinel scanning, the invariant checks over observed state, the
// traffic generator and availability analysis, and report assembly.
//
// Everything here runs in `go test -short ./...` without a Docker daemon. It
// never talks to Docker and never signals anything it did not start itself.
package harness

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// buildPassthrough is the allow-list copied from the parent environment for
// every subprocess. Nothing else is inherited: in particular no DOCKER_* (set
// explicitly below), no DSO_* (so the parent's integration-test variables such as
// the daemon ID never reach the code under test) and no credentials.
var passthrough = []string{
	"PATH", "LANG", "LC_ALL", "TZ", "TMPDIR",
	// needed by `go build` only; harmless for the agent
	"GOCACHE", "GOPATH", "GOMODCACHE", "GOFLAGS", "GOPROXY", "GOSUMDB", "GONOSUMDB",
	"GOTOOLCHAIN", "GOROOT", "CGO_ENABLED", "GOARCH", "GOOS", "HOME",
}

// SanitizedEnv builds the explicit environment of a subprocess: the allow-listed
// parent variables, DOCKER_HOST set to the guard endpoint, then extra. Any
// DOCKER_* key other than DOCKER_HOST in extra is an error, and DOCKER_HOST in
// extra must equal dockerHost: a subprocess can only ever see the verified
// endpoint.
func SanitizedEnv(parent []string, dockerHost string, extra map[string]string) ([]string, error) {
	u, err := url.Parse(dockerHost)
	if err != nil || (u.Scheme != "unix" && u.Scheme != "tcp") {
		return nil, fmt.Errorf("dockerHost must be a unix:// or tcp:// endpoint, got %q", dockerHost)
	}
	allowed := map[string]bool{}
	for _, k := range passthrough {
		allowed[k] = true
	}
	out := map[string]string{}
	for _, kv := range parent {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !allowed[k] {
			continue
		}
		out[k] = v
	}
	for k, v := range extra {
		if strings.HasPrefix(k, "DOCKER_") && (k != "DOCKER_HOST" || v != dockerHost) {
			return nil, fmt.Errorf("refusing to pass %s to a subprocess: only DOCKER_HOST=<guard endpoint> is allowed", k)
		}
		if strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return nil, fmt.Errorf("invalid environment entry %q", k)
		}
		out[k] = v
	}
	out["DOCKER_HOST"] = dockerHost
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+out[k])
	}
	return env, nil
}

// DockerVars returns every DOCKER_* entry of env (used by tests and by the
// harness' pre-flight assertion that exactly DOCKER_HOST is present).
func DockerVars(env []string) []string {
	var d []string
	for _, kv := range env {
		if strings.HasPrefix(kv, "DOCKER_") {
			d = append(d, kv)
		}
	}
	return d
}
