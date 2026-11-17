//go:build dockerint

package docker

// Helpers for the `docker dso inject` security scenarios (inject_security_test.go).
// Everything goes through the isolation guard: the verified endpoint only, the
// run's labels on every resource, unique names, synthetic secrets only.

import (
	"bytes"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/cli"
	"github.com/docker-secret-operator/dso/test/dockerguard"
	"github.com/docker/docker/api/types/container"
)

const (
	injDir     = "/run/secrets/dso" // DSO's tmpfs secret directory and the default --mount
	injIdleCmd = `trap 'exit 0' TERM; while true; do sleep 1 & wait $!; done`
)

// injSpec describes a throwaway container for an inject scenario.
type injSpec struct {
	labels map[string]string
	tmpfs  []string // --tmpfs values, e.g. "/run/secrets/dso:rw,mode=0755"
	mounts []string // -v values (bind or named volume)
	user   string
}

// injStart runs a labelled idle container and removes it when the test ends.
func injStart(t *testing.T, sp injSpec) (id, name string) {
	t.Helper()
	name = uniqueName(t, "dso-it-inj")
	args := []string{"run", "-d", "--name", name}
	args = append(args, dockerguard.Current(t).LabelArgs()...)
	for k, v := range sp.labels {
		args = append(args, "--label", k+"="+v)
	}
	for _, v := range sp.tmpfs {
		args = append(args, "--tmpfs", v)
	}
	for _, v := range sp.mounts {
		args = append(args, "-v", v)
	}
	if sp.user != "" {
		args = append(args, "--user", sp.user)
	}
	args = append(args, testImage, "sh", "-c", injIdleCmd)
	id = dockerCmd(t, args...)
	dc := dockerClient(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = dc.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
	})
	return id, name
}

// injVolume creates a labelled named volume and returns its name.
func injVolume(t *testing.T) string {
	t.Helper()
	name := uniqueName(t, "dso-it-injvol")
	args := append([]string{"volume", "create"}, dockerguard.Current(t).LabelArgs()...)
	dockerCmd(t, append(args, name)...)
	return name
}

// injRun executes `docker dso inject` in-process against the verified endpoint
// (dockerguard.Main exported DOCKER_HOST) with the value on stdin, and returns
// everything it printed (stdout + stderr) and its error.
func injRun(t *testing.T, value string, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()

	cmd := cli.NewInjectCmd()
	var eb bytes.Buffer
	cmd.SetIn(strings.NewReader(value))
	cmd.SetOut(&eb)
	cmd.SetErr(&eb)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs(append(args, "--value-stdin"))
	runErr := cmd.Execute()
	if runErr != nil {
		eb.WriteString(runErr.Error())
	}
	_ = w.Close()
	os.Stdout = old
	return <-done + eb.String(), runErr
}

// injExec runs a command in the container (non-fatal).
func injExec(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	return dockerguard.Current(t).Docker(t, append([]string{"exec", name}, args...)...)
}

func injExecOK(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := injExec(t, name, args...)
	if err != nil {
		t.Fatalf("docker exec %s %s: %v\n%s", name, strings.Join(args, " "), err, injRedact(out))
	}
	return out
}

func injRedact(s string) string { return strings.ReplaceAll(s, sentinel, "<redacted>") }

// injNoLeak fails when the synthetic secret appears in text.
func injNoLeak(t *testing.T, what, text string) {
	t.Helper()
	if strings.Contains(text, sentinel) {
		t.Fatalf("%s contains the synthetic secret", what)
	}
}

// injFileMissing asserts path does not exist inside the container.
func injFileMissing(t *testing.T, name, path string) {
	t.Helper()
	if _, err := injExec(t, name, "test", "-e", path); err == nil {
		t.Fatalf("%s exists in the container but must not", path)
	}
}

// injContent reads the file (as root) and compares without printing the value.
func injAssertContent(t *testing.T, name, path, want string) {
	t.Helper()
	got := injExecOK(t, name, "cat", path)
	if got != strings.TrimSpace(want) {
		t.Fatalf("%s content mismatch (len got=%d want=%d)", path, len(got), len(want))
	}
}

// injFSType returns the filesystem type backing dir inside the container,
// using the longest matching /proc/mounts entry.
func injFSType(t *testing.T, name, dir string) string {
	t.Helper()
	return injExecOK(t, name, "sh", "-c",
		`awk -v d="$1" 'BEGIN{b=-1} { m=$2; if ((d==m || index(d, m "/")==1 || m=="/") && length(m)>b) { b=length(m); t=$3 } } END{print t}' /proc/mounts`,
		"sh", dir)
}

// injDiffEntries returns `docker diff` lines (writable-layer changes).
func injDiffEntries(t *testing.T, id string) []string {
	t.Helper()
	out := dockerCmd(t, "diff", id)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func injAssertNoDiffMention(t *testing.T, id string, needles ...string) {
	t.Helper()
	for _, l := range injDiffEntries(t, id) {
		for _, n := range needles {
			if strings.Contains(l, n) {
				t.Fatalf("writable layer changed by injection: %q", l)
			}
		}
	}
}

// injSemver parses "29.5.1" / "25.0.17-ce" into numeric parts (missing parts 0).
func injSemver(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func injAtLeast(v, min [3]int) bool {
	for i := range v {
		if v[i] != min[i] {
			return v[i] > min[i]
		}
	}
	return true
}

func injLabelArgs(t *testing.T) []string { return dockerguard.Current(t).LabelArgs() }

// injExecAs runs `docker exec --user USER NAME CMD...` (non-fatal).
func injExecAs(t *testing.T, name, user string, args ...string) (string, error) {
	t.Helper()
	return dockerguard.Current(t).Docker(t, append([]string{"exec", "--user", user, name}, args...)...)
}

// injExecEnv runs `docker exec -e ENV NAME CMD...` (non-fatal); the env value
// is visible only in that process, never in a container-wide setting.
func injExecEnv(t *testing.T, name, env string, args ...string) (string, error) {
	t.Helper()
	return dockerguard.Current(t).Docker(t, append([]string{"exec", "-e", env, name}, args...)...)
}
