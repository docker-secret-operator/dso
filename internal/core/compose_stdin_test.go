package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeDocker = `#!/bin/sh
out="$FAKE_OUT"
printf '%s\n' "$@" > "$out/args"
pwd > "$out/cwd"
cat > "$out/stdin"
case "$FAKE_MODE" in
  fail) echo "compose: simulated failure" >&2; exit 7 ;;
  hang)
    trap 'echo term > "$out/got-term"; exit 143' TERM
    echo ready > "$out/ready"
    while :; do sleep 0.05; done ;;
esac
exit 0
`

func setupFake(t *testing.T) (bin, out, proj string) {
	t.Helper()
	d := t.TempDir()
	bin = filepath.Join(d, "docker")
	if err := os.WriteFile(bin, []byte(fakeDocker), 0o700); err != nil {
		t.Fatal(err)
	}
	out = filepath.Join(d, "out")
	proj = filepath.Join(d, "project")
	for _, p := range []string{out, proj} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func envFor(out, mode string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "FAKE_OUT=" + out, "FAKE_MODE=" + mode}
}

func tempComposeFiles(t *testing.T) int {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(os.TempDir(), "docker-compose-dso-*.yaml"))
	return len(m)
}

const syntheticSecret = "SYNTHETIC-RESOLVED-SECRET-5531"

func TestRunComposeUpStdin_PipesResolvedConfigAndLeavesNoFile(t *testing.T) {
	bin, out, proj := setupFake(t)
	before := tempComposeFiles(t)
	yamlData := []byte("services:\n  web:\n    environment:\n      DB_PASSWORD: " + syntheticSecret + "\n")
	var stderr bytes.Buffer
	err := RunComposeUpStdin(context.Background(), yamlData, ComposeStdinOptions{
		ProjectName: "myproj", ProjectDir: proj, Args: []string{"-d", "--build"},
		Env: envFor(out, "ok"), Stdout: &bytes.Buffer{}, Stderr: &stderr, DockerBin: bin,
	})
	if err != nil {
		t.Fatalf("%v (%s)", err, stderr.String())
	}
	args, _ := os.ReadFile(filepath.Join(out, "args"))
	want := "compose\n-p\nmyproj\n--project-directory\n" + proj + "\n-f\n-\nup\n-d\n--build\n"
	if string(args) != want {
		t.Fatalf("argv:\n%s\nwant:\n%s", args, want)
	}
	if strings.Contains(string(args), syntheticSecret) {
		t.Fatal("secret must never appear in process arguments")
	}
	if got, _ := os.ReadFile(filepath.Join(out, "stdin")); !bytes.Equal(got, yamlData) {
		t.Fatal("compose must receive the resolved YAML on stdin")
	}
	// Relative paths: compose runs in the original project directory, not /tmp.
	cwd, _ := os.ReadFile(filepath.Join(out, "cwd"))
	if strings.TrimSpace(string(cwd)) != proj {
		t.Fatalf("cwd %q, want %q", cwd, proj)
	}
	if after := tempComposeFiles(t); after != before {
		t.Fatalf("a docker-compose-dso-*.yaml temp file was created (%d -> %d)", before, after)
	}
	// Nothing in the project directory either.
	if es, _ := os.ReadDir(proj); len(es) != 0 {
		t.Fatalf("project directory polluted: %v", es)
	}
}

func TestRunComposeUpStdin_FailureIsPropagatedWithoutLeakingContent(t *testing.T) {
	bin, out, proj := setupFake(t)
	err := RunComposeUpStdin(context.Background(), []byte("secret: "+syntheticSecret), ComposeStdinOptions{
		ProjectName: "p", ProjectDir: proj, Env: envFor(out, "fail"), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, DockerBin: bin,
	})
	if err == nil {
		t.Fatal("a failing compose must fail")
	}
	if strings.Contains(err.Error(), syntheticSecret) {
		t.Fatalf("error leaked content: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("exit status not propagated: %v", err)
	}
}

func TestRunComposeUpStdin_CancellationTerminatesComposeGracefully(t *testing.T) {
	bin, out, proj := setupFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunComposeUpStdin(ctx, []byte("x: y"), ComposeStdinOptions{
			ProjectName: "p", ProjectDir: proj, Env: envFor(out, "hang"), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, DockerBin: bin,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(out, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake compose never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run must report an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not stop compose")
	}
	if _, err := os.Stat(filepath.Join(out, "got-term")); err != nil {
		t.Fatal("compose must receive SIGTERM (graceful), not just be killed")
	}
}

func TestRunComposeUpStdin_RejectsBadInput(t *testing.T) {
	for _, o := range []ComposeStdinOptions{
		{ProjectName: "", ProjectDir: "/tmp"},
		{ProjectName: "p", ProjectDir: ""},
		{ProjectName: "p", ProjectDir: "relative/dir"},
	} {
		if err := RunComposeUpStdin(context.Background(), nil, o); err == nil {
			t.Errorf("%+v must be rejected", o)
		}
	}
}
