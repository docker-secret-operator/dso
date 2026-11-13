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

// While tightenUmask..restoreUmask is held, the umask observed by ANY goroutine
// must be 077 and the process umask must be restored to its true original value
// afterwards. Run with -race.
func TestUmaskTightenRestorePairsAreSerialised(t *testing.T) {
	orig := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(orig) })

	var violations atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				old := tightenUmask()
				if old != 0o022 { // a previous holder's 077 leaked into "the original"
					violations.Add(1)
				}
				if cur := unix.Umask(0o077); cur != 0o077 { // someone restored under us
					violations.Add(1)
				}
				restoreUmask(old)
			}
		}()
	}
	wg.Wait()
	if v := violations.Load(); v != 0 {
		t.Fatalf("%d umask violations: concurrent socket creation could run under a wider umask", v)
	}
	if got := unix.Umask(0o022); got != 0o022 {
		t.Fatalf("process umask was left at %#o, want the original 022", got)
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
