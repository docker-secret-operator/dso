//go:build linux

package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// Regression for the legacy TestRace_SocketPermission_Concurrent (which asserted
// nothing). The real contract it pointed at: agent sockets must never be
// reachable with a wider mode than intended, even when several are created
// concurrently, because the process umask they are created under is global state.

// While withUmask is active, the umask observed inside the scope must be exactly
// the requested one for every concurrent caller, and the process umask must be
// the true original afterwards. Run with -race (the race detector alone does not
// prove umask safety; the assertions below do).
func TestWithUmaskScopesAreSerialised(t *testing.T) {
	orig := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(orig) })

	var violations atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				withUmask(0o077, func() {
					if cur := unix.Umask(0o077); cur != 0o077 { // someone else's scope leaked or restored under us
						violations.Add(1)
					}
				})
			}
		}()
	}
	wg.Wait()
	if v := violations.Load(); v != 0 {
		t.Fatalf("%d umask violations: concurrent socket creation could run under a wrong umask", v)
	}
	if got := unix.Umask(0o022); got != 0o022 {
		t.Fatalf("process umask was left at %#o, want the original 022", got)
	}
}

// A panic inside the scope must still restore the umask and release the lock:
// afterwards a normal scope must not deadlock and the original mask is back.
func TestWithUmaskRestoresAndUnlocksOnPanic(t *testing.T) {
	orig := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(orig) })

	func() {
		defer func() { _ = recover() }()
		withUmask(0o077, func() { panic("boom") })
	}()
	done := make(chan struct{})
	go func() { withUmask(0o077, func() {}); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("umask lock was not released after a panic in the scope (deadlock)")
	}
	if got := unix.Umask(0o022); got != 0o022 {
		t.Fatalf("umask after panic = %#o, want the original 022", got)
	}
}

// The socket file must be BORN restricted: with a deliberately wide process umask
// (000) the raw mode right after Listen, before any chmod, has no group/other bits.
func TestListenUnixRestrictedCreatesRestrictedFile(t *testing.T) {
	orig := unix.Umask(0o000)
	t.Cleanup(func() { unix.Umask(orig) })

	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := filepath.Join(dir, "s"+string(rune('a'+i))+".sock")
			l, err := listenUnixRestricted(p)
			if err != nil {
				t.Errorf("listen: %v", err)
				return
			}
			defer func() { _ = l.Close() }()
			fi, err := os.Stat(p)
			if err != nil {
				t.Errorf("stat: %v", err)
				return
			}
			if fi.Mode().Perm()&0o077 != 0 {
				t.Errorf("socket born with group/other bits: %v", fi.Mode().Perm())
			}
		}(i)
	}
	wg.Wait()
	if got := unix.Umask(0o000); got != 0o000 {
		t.Fatalf("umask after listens = %#o, want original 000", got)
	}
}

// Concurrent StartSocketServer calls: no socket is ever observable with any 'other'
// permission or group-execute (owner bits are irrelevant; the intended ceiling is 0660), and the final
// modes are exactly 0600 (no dso group) or 0660.
func TestConcurrentSocketCreationNeverExposesWiderMode(t *testing.T) {
	orig := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(orig) })

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	const n = 8
	paths := make([]string, n)
	for i := range paths {
		paths[i] = filepath.Join(dir, "s"+string(rune('a'+i)), "agent.sock")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wider atomic.Value // first offending observation
	stop := make(chan struct{})
	var watchWG sync.WaitGroup
	watchWG.Add(1)
	go func() {
		defer watchWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, p := range paths {
				if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSocket != 0 {
					if m := fi.Mode().Perm(); m&0o017 != 0 { // any 'other' bit or group-execute
						wider.CompareAndSwap(nil, p+" "+m.String())
					}
				}
			}
		}
	}()

	var wg sync.WaitGroup
	var shutdowns sync.Map
	for _, p := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			_, shutdown, err := StartSocketServer(ctx, p, nil, nil, zap.NewNop(), nil)
			if err != nil {
				t.Errorf("StartSocketServer(%s): %v", p, err)
				return
			}
			shutdowns.Store(p, shutdown)
		}(p)
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond) // let the watcher see the final modes too
	close(stop)
	watchWG.Wait()

	if w := wider.Load(); w != nil {
		t.Fatalf("a socket was observable with a wider mode than intended: %v", w)
	}
	for _, p := range paths {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("socket %s missing: %v", p, err)
		}
		if m := fi.Mode().Perm(); m != 0o600 && m != 0o660 {
			t.Errorf("socket %s final mode %v, want 0600 (or 0660 with a dso group)", p, m)
		}
	}
	shutdowns.Range(func(_, v any) bool { v.(func())(); return true })
	if got := unix.Umask(0o022); got != 0o022 {
		t.Fatalf("process umask was left at %#o, want 022", got)
	}
}
