package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests re-execute this test binary as a tiny helper child. Modes:
//
//	sleep           sleep for a minute
//	exit:N          exit with status N
//	selfkill        SIGKILL itself
//	spawn           start a grandchild sleeper in the SAME process group, print its
//	                PID to the file named by HELPER_PIDFILE, then sleep
//	ignoreterm      ignore SIGTERM and sleep (forces SIGKILL escalation)
func TestMain(m *testing.M) {
	if mode := os.Getenv("HARNESS_HELPER"); mode != "" {
		helperMain(mode)
		return
	}
	os.Exit(m.Run())
}

func helperMain(mode string) {
	switch {
	case mode == "sleep":
		time.Sleep(time.Minute)
	case strings.HasPrefix(mode, "exit:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(mode, "exit:"))
		os.Exit(n)
	case mode == "selfkill":
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	case mode == "spawn":
		c := exec.Command(os.Args[0])
		c.Env = []string{"HARNESS_HELPER=sleep"}
		if err := c.Start(); err != nil {
			os.Exit(3)
		}
		_ = os.WriteFile(os.Getenv("HELPER_PIDFILE"), []byte(strconv.Itoa(c.Process.Pid)), 0o600)
		time.Sleep(time.Minute)
	case mode == "ignoreterm":
		ignoreTerm()
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func startHelper(t *testing.T, mode string, extra ...string) *Proc {
	t.Helper()
	p, err := Start(Spec{Path: os.Args[0], Env: append([]string{"HARNESS_HELPER=" + mode}, extra...), LogPath: filepath.Join(t.TempDir(), "log")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.TerminateGroup(time.Second) })
	return p
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestExitStatusAndSignalAreReportedExactly(t *testing.T) {
	p := startHelper(t, "exit:7")
	info, err := p.WaitExit(10 * time.Second)
	if err != nil || !info.Exited || info.Code != 7 || info.Signaled {
		t.Fatalf("got %+v (%v)", info, err)
	}
	k := startHelper(t, "selfkill")
	info, err = k.WaitExit(10 * time.Second)
	if err != nil || !info.Signaled || info.Signal != syscall.SIGKILL || info.Exited {
		t.Fatalf("got %+v (%v): a SIGKILLed child must be reported as signalled, not exited", info, err)
	}
}

func TestChildRunsInItsOwnProcessGroup(t *testing.T) {
	p := startHelper(t, "sleep")
	st, err := readStat(p.PID())
	if err != nil {
		t.Fatal(err)
	}
	if st.pgrp != p.PID() || st.pgrp == syscall.Getpgrp() {
		t.Fatalf("child pgrp=%d pid=%d parent pgrp=%d: not in its own group", st.pgrp, p.PID(), syscall.Getpgrp())
	}
	if err := p.VerifyIdentity(); err != nil {
		t.Fatal(err)
	}
}

func TestTerminateGroupKillsChildAndGrandchildOnly(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "gc")
	p := startHelper(t, "spawn", "HELPER_PIDFILE="+pidfile)
	var gc int
	waitFor(t, func() bool {
		b, err := os.ReadFile(pidfile)
		if err != nil {
			return false
		}
		gc, err = strconv.Atoi(string(b))
		return err == nil
	})
	// An unrelated process in ANOTHER group (a bystander) must survive.
	by := exec.Command("sleep", "60")
	by.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := by.Start(); err != nil {
		t.Skipf("no sleep binary: %v", err)
	}
	t.Cleanup(func() { _ = by.Process.Kill(); _, _ = by.Process.Wait() })

	if err := p.TerminateGroup(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !alive(gc) })
	if !alive(by.Process.Pid) {
		t.Fatal("an unrelated process was killed")
	}
}

func TestTerminateGroupEscalatesToKill(t *testing.T) {
	p := startHelper(t, "ignoreterm")
	time.Sleep(300 * time.Millisecond) // let the helper install its handler
	start := time.Now()
	if err := p.TerminateGroup(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	info, _ := p.Exit()
	if !info.Signaled || info.Signal != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL escalation, got %+v", info)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatal("escalation took too long")
	}
}

// A recycled PID must never be signalled: with a forged recorded start time the
// identity check fails and nothing is sent.
func TestIdentityMismatchRefusesToSignal(t *testing.T) {
	p := startHelper(t, "sleep")
	p.startTicks++ // simulate "the PID now belongs to a different process"
	if err := p.VerifyIdentity(); err == nil {
		t.Fatal("identity verified despite a start-time mismatch")
	}
	if err := p.Signal(syscall.SIGKILL); err == nil {
		t.Fatal("signalled a process whose identity did not verify")
	}
	if err := p.TerminateGroup(time.Second); err == nil {
		t.Fatal("TerminateGroup must report that it refused to signal")
	}
	if !alive(p.PID()) {
		t.Fatal("process was killed although identity did not verify")
	}
	p.startTicks-- // restore so the cleanup can stop it
}

func TestVerifyIdentityFailsAfterReap(t *testing.T) {
	p := startHelper(t, "exit:0")
	if _, err := p.WaitExit(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyIdentity(); err == nil {
		t.Fatal("identity verified for a reaped child")
	}
	if err := p.Signal(syscall.SIGTERM); err == nil {
		t.Fatal("signalled a reaped child")
	}
}

func TestParseStatHandlesParensAndSpacesInComm(t *testing.T) {
	line := "1234 (a b) c) S 1 4321 1 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 98765 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0"
	st, err := parseStat(line)
	if err != nil {
		t.Fatal(err)
	}
	if st.ppid != 1 || st.pgrp != 4321 || st.startTicks != 98765 {
		t.Fatalf("%+v", st)
	}
	if _, err := parseStat("garbage"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met")
}
