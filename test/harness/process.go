package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Spec describes one child process.
type Spec struct {
	Path    string
	Args    []string
	Dir     string
	Env     []string // explicit; the parent environment is never inherited
	LogPath string   // stdout+stderr (appended)
}

// ExitInfo is how a child ended.
type ExitInfo struct {
	Exited   bool
	Code     int
	Signaled bool
	Signal   syscall.Signal
	Err      error
}

func (e ExitInfo) String() string {
	switch {
	case e.Signaled:
		return "killed by signal " + e.Signal.String()
	case e.Exited:
		return "exited with status " + strconv.Itoa(e.Code)
	case e.Err != nil:
		return "wait error: " + e.Err.Error()
	}
	return "unknown"
}

// Proc is a child started by this package. Every signal it sends goes to the PID
// (or process group) it started and is preceded by an identity check, so a
// recycled PID can never be signalled. Nothing here looks a process up by name.
type Proc struct {
	cmd        *exec.Cmd
	pid, pgid  int
	startTicks uint64 // /proc/<pid>/stat field 22 at launch
	logf       *os.File

	done chan struct{}
	mu   sync.Mutex
	exit ExitInfo
	// stragglers records whether group members outlived the leader at the moment it
	// was reaped; only then may the group be signalled after exit (see KillStragglers).
	stragglers bool
}

// Start launches spec in its own process group (Setpgid, so pgid == pid).
func Start(spec Spec) (*Proc, error) {
	if spec.Path == "" {
		return nil, errors.New("empty path")
	}
	cmd := exec.Command(spec.Path, spec.Args...) // #nosec G204 -- test harness: path is the binary the harness itself built
	cmd.Dir = spec.Dir
	cmd.Env = append([]string(nil), spec.Env...)
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var logf *os.File
	if spec.LogPath != "" {
		f, err := os.OpenFile(spec.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- harness-owned temp path
		if err != nil {
			return nil, err
		}
		logf = f
		cmd.Stdout, cmd.Stderr = f, f
	}
	if err := cmd.Start(); err != nil {
		if logf != nil {
			_ = logf.Close()
		}
		return nil, err
	}
	p := &Proc{cmd: cmd, pid: cmd.Process.Pid, logf: logf, done: make(chan struct{})}
	p.pgid = p.pid
	if st, err := readStat(p.pid); err == nil {
		p.startTicks = st.startTicks
		if st.pgrp != p.pgid {
			// Setpgid raced or failed: refuse to ever signal a group we do not lead.
			p.pgid = 0
		}
	}
	go p.wait()
	return p, nil
}

func (p *Proc) wait() {
	err := p.cmd.Wait()
	info := ExitInfo{Err: err}
	if ps := p.cmd.ProcessState; ps != nil {
		info.Err = nil
		if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			info.Signaled, info.Signal = true, ws.Signal()
		} else {
			info.Exited, info.Code = true, ps.ExitCode()
		}
	}
	p.mu.Lock()
	p.exit = info
	if p.pgid > 0 {
		// Members of the group still alive right now necessarily belong to it: a
		// process-group ID with live members cannot be recycled.
		p.stragglers = syscall.Kill(-p.pgid, 0) == nil
	}
	p.mu.Unlock()
	if p.logf != nil {
		_ = p.logf.Close()
	}
	close(p.done)
}

// PID is the process ID the harness started.
func (p *Proc) PID() int { return p.pid }

// Done is closed when the child has been reaped.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Exit returns the exit information once the child has ended.
func (p *Proc) Exit() (ExitInfo, bool) {
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.exit, true
	default:
		return ExitInfo{}, false
	}
}

// WaitExit waits for the child to end.
func (p *Proc) WaitExit(timeout time.Duration) (ExitInfo, error) {
	select {
	case <-p.done:
		info, _ := p.Exit()
		return info, nil
	case <-time.After(timeout):
		return ExitInfo{}, fmt.Errorf("pid %d did not exit within %v", p.pid, timeout)
	}
}

// VerifyIdentity proves that the live process with p.PID() is the one that was
// started (same start time since boot). It fails when the child has ended, when
// the PID now belongs to something else, or when identity cannot be read.
func (p *Proc) VerifyIdentity() error {
	if p.startTicks == 0 {
		return errors.New("start time was not recorded: identity cannot be verified, refusing to signal")
	}
	select {
	case <-p.done:
		return fmt.Errorf("pid %d has already been reaped", p.pid)
	default:
	}
	st, err := readStat(p.pid)
	if err != nil {
		return fmt.Errorf("pid %d: %w", p.pid, err)
	}
	if st.startTicks != p.startTicks {
		return fmt.Errorf("pid %d is no longer the process that was started (start time changed)", p.pid)
	}
	return nil
}

// Signal sends sig to the child ONLY, after verifying its identity.
func (p *Proc) Signal(sig syscall.Signal) error {
	if err := p.VerifyIdentity(); err != nil {
		return err
	}
	return syscall.Kill(p.pid, sig)
}

// TerminateGroup stops the child and its whole process group: SIGTERM, then
// SIGKILL after grace. It signals only the group this package created
// (pgid == the PID it started) and only while the leader's identity verifies; once
// the leader has been reaped it acts only on stragglers observed at reap time.
func (p *Proc) TerminateGroup(grace time.Duration) error {
	if p.pgid <= 0 {
		return errors.New("not the leader of its own process group: refusing group signalling")
	}
	select {
	case <-p.done: // already reaped: only stragglers can remain
		return p.KillStragglers()
	default:
	}
	if err := p.VerifyIdentity(); err != nil {
		return fmt.Errorf("refusing to signal: %w", err)
	}
	{
		_ = syscall.Kill(-p.pgid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(grace):
			if err := p.VerifyIdentity(); err == nil {
				_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
			}
		}
		if _, err := p.WaitExit(10 * time.Second); err != nil {
			return err
		}
	}
	return p.KillStragglers()
}

// KillStragglers SIGKILLs group members that outlived the leader (for example a
// provider plugin orphaned when the agent was SIGKILLed). It acts only if members
// were observed alive at the instant the leader was reaped.
func (p *Proc) KillStragglers() error {
	<-p.done
	p.mu.Lock()
	had := p.stragglers
	p.mu.Unlock()
	if !had || p.pgid <= 0 {
		return nil
	}
	if err := syscall.Kill(-p.pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

type procStat struct {
	ppid, pgrp int
	startTicks uint64
}

// readStat parses /proc/<pid>/stat (Linux). comm may contain spaces and parens, so
// parsing starts after the LAST ')'.
func readStat(pid int) (procStat, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, err
	}
	return parseStat(string(b))
}

func parseStat(s string) (procStat, error) {
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return procStat{}, errors.New("malformed /proc stat")
	}
	f := strings.Fields(s[i+2:]) // f[0] = state (field 3)
	if len(f) < 20 {
		return procStat{}, errors.New("short /proc stat")
	}
	ppid, e1 := strconv.Atoi(f[1])
	pgrp, e2 := strconv.Atoi(f[2])
	start, e3 := strconv.ParseUint(f[19], 10, 64) // field 22
	if e1 != nil || e2 != nil || e3 != nil {
		return procStat{}, errors.New("unparsable /proc stat")
	}
	return procStat{ppid: ppid, pgrp: pgrp, startTicks: start}, nil
}
