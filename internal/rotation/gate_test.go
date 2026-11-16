package rotation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

func gateSpecs(dir string) []FileSecretSpec {
	return []FileSecretSpec{{Path: dir, Data: map[string]string{"a": "x", "b": "y"}, UID: 1000, GID: 1000}}
}

// Original-contract table: the effective argv the container runs is always
// (original ENTRYPOINT ++ CMD), through the gate, in every image shape.
func TestArmGate_PreservesOriginalContract(t *testing.T) {
	cases := []struct {
		name         string
		ep, cmd      []string
		wantEP, want []string
		refused      bool
	}{
		{"cmd only", nil, []string{"nginx", "-g", "daemon off;"}, nil, []string{"nginx", "-g", "daemon off;"}, false},
		{"entrypoint and cmd", []string{"/entry.sh"}, []string{"--a", "b c"}, []string{"/entry.sh"}, []string{"--a", "b c"}, false},
		{"entrypoint only", []string{"/bin/app", "--x"}, nil, []string{"/bin/app", "--x"}, nil, false},
		{"compose override of both", []string{"/o", "1"}, []string{"2", ""}, []string{"/o", "1"}, []string{"2", ""}, false},
		{"awkward argument boundaries", []string{"/e"}, []string{"a b", "c'd", `e"f`, "g\nh", "$HOME", ";rm -rf /", "--", "-x"}, []string{"/e"}, []string{"a b", "c'd", `e"f`, "g\nh", "$HOME", ";rm -rf /", "--", "-x"}, false},
		{"neither", nil, nil, nil, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &container.Config{Entrypoint: tc.ep, Cmd: tc.cmd, User: "1000", WorkingDir: "/w", Env: []string{"E=1"}}
			origEP := append([]string(nil), tc.ep...)
			err := ArmGate(cfg, gateSpecs("/run/secrets/dso"), 30*time.Second)
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "neither an ENTRYPOINT nor a CMD") {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ep, cmd := OriginalCommand(cfg)
			if strings.Join(ep, "\x00") != strings.Join(tc.wantEP, "\x00") || strings.Join(cmd, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("contract changed: ep=%q cmd=%q", ep, cmd)
			}
			if cfg.User != "1000" || cfg.WorkingDir != "/w" || len(cfg.Env) != 1 {
				t.Error("user/workdir/env changed")
			}
			if len(tc.ep) != len(origEP) {
				t.Error("input slice mutated")
			}
			for _, a := range cfg.Entrypoint[:gatePrefixLen+3] {
				if strings.Contains(a, "SECRET") {
					t.Error("secret in the gate arguments")
				}
			}
		})
	}
}

func TestArmGate_IdempotentNeverNests(t *testing.T) {
	cfg := &container.Config{Entrypoint: []string{"/e"}, Cmd: []string{"c"}}
	if err := ArmGate(cfg, gateSpecs("/run/secrets/dso"), time.Minute); err != nil {
		t.Fatal(err)
	}
	// a clone of a promoted (already gated) container is re-armed for the new file set
	clone := &container.Config{Entrypoint: append([]string(nil), cfg.Entrypoint...), Cmd: cfg.Cmd}
	specs := []FileSecretSpec{{Path: "/run/secrets/dso", Data: map[string]string{"only": "v"}, UID: 1, GID: 1}}
	if err := ArmGate(clone, specs, time.Minute); err != nil {
		t.Fatal(err)
	}
	ep, _ := OriginalCommand(clone)
	if len(ep) != 1 || ep[0] != "/e" {
		t.Fatalf("nested wrapper: original = %q", ep)
	}
	n := 0
	for _, a := range clone.Entrypoint {
		if a == "dso-gate" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("gate nested %d times", n)
	}
	if clone.Entrypoint[gatePrefixLen+2] != "1" {
		t.Fatalf("file count not refreshed: %q", clone.Entrypoint[gatePrefixLen+2])
	}
}

func runGate(t *testing.T, ep []string, extra ...string) *exec.Cmd {
	t.Helper()
	// Run the real gate program with the local sh; the "container" argv is the
	// Entrypoint (already containing /bin/sh -c SCRIPT ...) followed by CMD.
	argv := append(append([]string(nil), ep...), extra...)
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh") // explicit skip (reported)
	}
	argv[0] = sh
	return exec.Command(argv[0], argv[1:]...)
}

func gateArgv(t *testing.T, dir string, timeout time.Duration, ep, cmd []string) []string {
	t.Helper()
	cfg := &container.Config{Entrypoint: ep, Cmd: cmd}
	if err := ArmGate(cfg, gateSpecs(dir), timeout); err != nil {
		t.Fatal(err)
	}
	return append([]string(nil), cfg.Entrypoint...)
}

func TestGateScript_WaitsForFlagAndFilesThenExecsOriginal(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(t.TempDir(), "out")
	argv := gateArgv(t, dir, 20*time.Second, []string{"/bin/sh", "-c", `printf '%s|' "$0" "$@" > ` + out, "ep"}, []string{"c 1", "c;2"})
	cmd := runGate(t, argv, "c 1", "c;2")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	time.Sleep(400 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("the gate released with nothing delivered")
	default:
	}
	// files without the flag: still gated
	for _, n := range []string{"a", "b"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o400)
	}
	time.Sleep(400 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("the gate released on files alone (the release flag is the agent's decision)")
	default:
	}
	// flag then the gate execs the original
	_ = os.WriteFile(filepath.Join(dir, GateFlagName), []byte("released\n"), 0o400)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("original exited with %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("gate did not release after flag + files")
	}
	b, _ := os.ReadFile(out)
	if string(b) != "ep|c 1|c;2|" {
		t.Fatalf("original argv not preserved exactly: %q", b)
	}
}

func TestGateScript_FlagWithoutFilesStaysGated(t *testing.T) {
	dir := t.TempDir()
	argv := gateArgv(t, dir, 20*time.Second, []string{"/bin/true"}, nil)
	cmd := runGate(t, argv)
	_ = os.WriteFile(filepath.Join(dir, GateFlagName), []byte("x"), 0o400)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Fatal("released with a flag but without the secret files")
	case <-time.After(600 * time.Millisecond):
	}
	_ = cmd.Process.Kill()
	<-done
}

func TestGateScript_TermExits143_TimeoutExits75(t *testing.T) {
	dir := t.TempDir()
	argv := gateArgv(t, dir, 20*time.Second, []string{"/bin/true"}, nil)
	cmd := runGate(t, argv)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 143 {
			t.Fatalf("TERM: err = %v, want exit 143", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the gate ignored SIGTERM (docker stop would wait for SIGKILL)")
	}

	argv = gateArgv(t, dir, time.Second, []string{"/bin/true"}, nil)
	cmd = runGate(t, argv)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != GateExitTimeout {
		t.Fatalf("timeout: err = %v, want exit %d", err, GateExitTimeout)
	}
	if !strings.Contains(stderr.String(), "did not complete in time") {
		t.Errorf("no timeout message: %q", stderr.String())
	}
}

func TestGateScript_ContainsNoSecretOrUserValue(t *testing.T) {
	sc := GateScript()
	for _, bad := range []string{"SECRET", "password", "/run/secrets", "echo $"} {
		if strings.Contains(sc, bad) {
			t.Errorf("gate script contains %q", bad)
		}
	}
	// only the fixed constant is ever the script; values travel as positional parameters
	a := &container.Config{Entrypoint: []string{"/e"}}
	b := &container.Config{Entrypoint: []string{"/other"}}
	_ = ArmGate(a, gateSpecs("/run/secrets/dso"), time.Minute)
	_ = ArmGate(b, []FileSecretSpec{{Path: "/x/y", Data: map[string]string{"zz": "v"}, UID: 1, GID: 1}}, time.Minute)
	if a.Entrypoint[2] != b.Entrypoint[2] || a.Entrypoint[2] != sc {
		t.Fatal("the script text varies with its inputs")
	}
}

// The delivery runner with fake operations: ordering, and journal-before-action.
func TestDelivery_JournalBeforeEveryGuardedAction(t *testing.T) {
	rec := &n19Rec{}
	j := &memJournal{}
	var order []string
	ops := rec.ops()
	base := *ops
	ops.Inject = func(c context.Context, id string, s FileSecretSpec) error {
		order = append(order, "inject after "+j.last())
		return base.Inject(c, id, s)
	}
	ops.Release = func(c context.Context, id, dir string, u, g int) error {
		order = append(order, "release after "+j.last())
		return base.Release(c, id, dir, u, g)
	}
	cfg := &container.Config{Entrypoint: []string{"/e"}, Labels: map[string]string{LabelRotationID: "rot-1"}}
	hc := &container.HostConfig{Tmpfs: map[string]string{"/run/secrets/dso": ""}}
	specs := gateSpecs("/run/secrets/dso")
	if err := PrepareGatedCandidate(cfg, hc, specs, time.Minute); err != nil {
		t.Fatal(err)
	}
	body := `{"Entrypoint":` + jsonStrings(cfg.Entrypoint) + `,"Labels":{"dso.rotation.id":"rot-1","dso.gate":"` + cfg.Labels[GateLabel] + `"},"HostConfig":{"Tmpfs":{"/run/secrets/dso":""}}}`
	rec.capture([]byte(body))
	d := &Delivery{Ops: *ops, RotationID: "rot-1",
		Intent: func(c context.Context, e JournalEvent) error { return j.Record(c, e) },
		Ack:    func(c context.Context, e JournalEvent) { _ = j.Record(c, e) }}
	ev, err := d.Execute(context.Background(), "cand", specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Files) != 2 {
		t.Fatalf("evidence files = %v", ev.Files)
	}
	if order[0] != "inject after intent_deliver_secrets" || order[len(order)-1] != "release after intent_release_gate" {
		t.Fatalf("an action ran before its journal intent: %v", order)
	}
}

func jsonStrings(s []string) string {
	var b strings.Builder
	b.WriteString("[")
	for i, x := range s {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconvQuote(x))
	}
	b.WriteString("]")
	return b.String()
}

func strconvQuote(s string) string {
	b, _ := jsonMarshal(s)
	return string(b)
}

var jsonMarshal = func(v any) ([]byte, error) { return json.Marshal(v) }
