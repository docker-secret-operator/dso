package rotation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

func fastProbeTimers(t *testing.T) {
	t.Helper()
	pp, rp, ma, hp := probePollInterval, probeRetryPause, probeMinAttempt, healthPollInterval
	probePollInterval, probeRetryPause, probeMinAttempt, healthPollInterval = time.Millisecond, time.Millisecond, 20*time.Millisecond, time.Millisecond
	t.Cleanup(func() { probePollInterval, probeRetryPause, probeMinAttempt, healthPollInterval = pp, rp, ma, hp })
}

// probeServer scripts the Docker exec endpoints. inspect decides each exec/json
// answer from the call index.
type probeServer struct {
	mu       sync.Mutex
	creates  []container.ExecOptions
	starts   []container.ExecStartOptions
	inspects int32
	inspect  func(n int) (running bool, exit int, fail bool)
}

func (ps *probeServer) client(t *testing.T) (c interface{ Close() error }, handler func(*http.Request) *http.Response) {
	return nil, func(req *http.Request) *http.Response {
		p := req.URL.Path
		switch {
		case strings.HasSuffix(p, "/containers/c/exec"):
			var o container.ExecOptions
			b, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(b, &o)
			ps.mu.Lock()
			ps.creates = append(ps.creates, o)
			ps.mu.Unlock()
			return jsonResp(http.StatusCreated, map[string]string{"Id": "exec-1"})
		case strings.HasSuffix(p, "/exec/exec-1/start"):
			var o container.ExecStartOptions
			b, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(b, &o)
			ps.mu.Lock()
			ps.starts = append(ps.starts, o)
			ps.mu.Unlock()
			return jsonResp(http.StatusOK, nil)
		case strings.HasSuffix(p, "/exec/exec-1/json"):
			n := int(atomic.AddInt32(&ps.inspects, 1))
			running, exit, fail := ps.inspect(n)
			if fail {
				return apiErrResp()
			}
			return jsonResp(http.StatusOK, map[string]interface{}{"ExitCode": exit, "Running": running})
		}
		return jsonResp(http.StatusNoContent, nil)
	}
}

func TestExecProbe_DoesNotSucceedBeforeTheCommandHasFinished(t *testing.T) {
	fastProbeTimers(t)
	// Regression for S104: the old probe read ExitCode right after ExecStart. While
	// an exec is still running the daemon reports ExitCode 0, which was treated as
	// success. Here the command is still "running" for 3 inspections and then
	// FAILS (exit 1); the probe must wait and report the failure.
	ps := &probeServer{inspect: func(n int) (bool, int, bool) {
		if n <= 3 {
			return true, 0, false
		}
		return false, 1, false
	}}
	_, h := ps.client(t)
	err := ExecProbe(context.Background(), fakeClient(t, h), "c", "/run/secrets/dso/k", 3*time.Second, 1)
	if err == nil || !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("want ErrUnhealthy (command failed after running), got %v", err)
	}
	if atomic.LoadInt32(&ps.inspects) < 4 {
		t.Fatalf("probe returned before the exec finished (inspects=%d)", ps.inspects)
	}
}

func TestExecProbe_SucceedsOnlyAfterCompletionWithExitZero(t *testing.T) {
	fastProbeTimers(t)
	ps := &probeServer{inspect: func(n int) (bool, int, bool) { return n < 3, 0, false }}
	_, h := ps.client(t)
	if err := ExecProbe(context.Background(), fakeClient(t, h), "c", "/run/secrets/dso/k", time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&ps.inspects) < 3 {
		t.Fatalf("returned success before completion (inspects=%d)", ps.inspects)
	}
}

func TestExecProbe_RequestShape(t *testing.T) {
	fastProbeTimers(t)
	ps := &probeServer{inspect: func(int) (bool, int, bool) { return false, 0, false }}
	_, h := ps.client(t)
	if err := ExecProbe(context.Background(), fakeClient(t, h), "c", "/run/secrets/dso/k", time.Second, 1); err != nil {
		t.Fatal(err)
	}
	c := ps.creates[0]
	if c.AttachStdout || c.AttachStderr {
		t.Fatal("probe output must not be attached (no file content may reach logs)")
	}
	if len(c.Cmd) != 5 || c.Cmd[0] != "/bin/sh" || c.Cmd[3] != "dso-probe" || c.Cmd[4] != "/run/secrets/dso/k" {
		t.Fatalf("unexpected argv %v", c.Cmd)
	}
	if !ps.starts[0].Detach {
		t.Fatal("exec must be started detached and then polled")
	}
}

func TestExecProbe_FailureModes(t *testing.T) {
	fastProbeTimers(t)
	cases := map[string]struct {
		inspect     func(n int) (bool, int, bool)
		wantUnheal  bool
		wantIsCause error
	}{
		"nonzero exit":                      {func(int) (bool, int, bool) { return false, 1, false }, true, nil},
		"container died during the probe":   {func(n int) (bool, int, bool) { return false, 137, false }, true, nil},
		"probe never finishes (slow)":       {func(int) (bool, int, bool) { return true, 0, false }, true, nil},
		"docker API error while inspecting": {func(int) (bool, int, bool) { return false, 0, true }, false, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ps := &probeServer{inspect: c.inspect}
			_, h := ps.client(t)
			err := ExecProbe(context.Background(), fakeClient(t, h), "c", "/p", 40*time.Millisecond, 2)
			if err == nil {
				t.Fatal("must fail")
			}
			if got := errors.Is(err, ErrUnhealthy); got != c.wantUnheal {
				t.Fatalf("errors.Is(ErrUnhealthy)=%v want %v (%v)", got, c.wantUnheal, err)
			}
		})
	}
}

func TestExecProbe_Cancellation(t *testing.T) {
	fastProbeTimers(t)
	ctx, cancel := context.WithCancel(context.Background())
	ps := &probeServer{inspect: func(n int) (bool, int, bool) {
		if n == 2 {
			cancel()
		}
		return true, 0, false
	}}
	_, h := ps.client(t)
	err := ExecProbe(ctx, fakeClient(t, h), "c", "/p", 5*time.Second, 3)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnhealthy) {
		t.Fatalf("want context.Canceled and not ErrUnhealthy, got %v", err)
	}
}

func TestExecProbeFiles_ProbesEachDeliveredFile(t *testing.T) {
	fastProbeTimers(t)
	ps := &probeServer{inspect: func(int) (bool, int, bool) { return false, 0, false }}
	_, h := ps.client(t)
	if err := ExecProbeFiles(context.Background(), fakeClient(t, h), "c", "/run/secrets/dso", []string{"b", "a", "../evil"}, time.Second, 1); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range ps.creates {
		got = append(got, c.Cmd[4])
	}
	want := []string{"/run/secrets/dso/b", "/run/secrets/dso/a", "/run/secrets/dso/evil"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("probed %v, want %v", got, want)
	}
}

// The probe script is executed for real against a local directory.
func TestProbeScript_RealExecution(t *testing.T) {
	for _, tool := range []string{"sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("sh unavailable")
		}
	}
	run := func(p string) int {
		err := exec.Command("/bin/sh", "-c", probeScript, "dso-probe", p).Run()
		if err == nil {
			return 0
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatal(err)
		return -1
	}
	d := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, []byte(content), 0o400); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if run(write("ok", "v")) != 0 {
		t.Fatal("non-empty regular file must pass")
	}
	if run(write("empty", "")) == 0 {
		t.Fatal("empty file must fail")
	}
	if run(filepath.Join(d, "missing")) == 0 {
		t.Fatal("missing path must fail")
	}
	// A directory is NOT a secret file. The old `test -s <dir>` accepted it.
	only := filepath.Join(d, "onlysub")
	_ = os.MkdirAll(filepath.Join(only, "child"), 0o700)
	if run(only) == 0 {
		t.Fatal("a directory with no secret files must fail")
	}
	if fi, _ := os.Stat(only); fi.Size() == 0 {
		t.Log("note: directory reports size 0 here; old probe would still differ on other filesystems")
	}
	emptyDir := filepath.Join(d, "emptydir")
	_ = os.MkdirAll(emptyDir, 0o700)
	if run(emptyDir) == 0 {
		t.Fatal("an empty directory must fail")
	}
	good := filepath.Join(d, "gooddir")
	_ = os.MkdirAll(good, 0o700)
	_ = os.WriteFile(filepath.Join(good, "a"), []byte("x"), 0o400)
	if run(good) != 0 {
		t.Fatal("a directory with non-empty secret files must pass")
	}
	_ = os.WriteFile(filepath.Join(good, "b"), nil, 0o400)
	if run(good) == 0 {
		t.Fatal("a directory containing an empty secret file must fail")
	}
	_ = os.Remove(filepath.Join(good, "b"))
	_ = os.WriteFile(filepath.Join(good, ".inject.AbC123"), []byte("partial"), 0o400)
	if run(good) == 0 {
		t.Fatal("a leftover temporary file must fail")
	}
}

func TestWaitReady_ReportsEvidenceLevel(t *testing.T) {
	fastProbeTimers(t)
	mk := func(st *container.State) func(*http.Request) *http.Response {
		return func(*http.Request) *http.Response { return inspectWithState(st) }
	}
	r, err := WaitReady(context.Background(), fakeClient(t, mk(&container.State{Running: true, Health: &container.Health{Status: "healthy"}})), "c", time.Second)
	if err != nil || r != ReadinessHealthcheck {
		t.Fatalf("got %v, %v", r, err)
	}
	r, err = WaitReady(context.Background(), fakeClient(t, mk(&container.State{Running: true})), "c", time.Second)
	if err != nil || r != ReadinessProcessStable {
		t.Fatalf("got %v, %v", r, err)
	}
	if !strings.Contains(ReadinessProcessStable.String(), "no HEALTHCHECK") {
		t.Fatal("process-only evidence must be described honestly")
	}
	r, err = WaitReady(context.Background(), fakeClient(t, mk(&container.State{Running: false})), "c", time.Second)
	if r != ReadinessNone || !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("exited container: %v %v", r, err)
	}
}
