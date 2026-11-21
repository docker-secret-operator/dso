//go:build unix

package sqlite

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// InstallStagingSignalCleanup arranges that, on SIGINT or SIGTERM, every
// staging directory this process created is removed and the signal is then
// re-delivered with its default action restored, so the process still
// terminates exactly as it would have without the handler (the signal is not
// swallowed and the exit status is the shell's usual 128+signal). It returns a
// function that uninstalls the handler; call it when the command finishes.
//
// It cannot help after SIGKILL, an out-of-memory kill, a machine failure or
// power loss.
//
// Known, deliberately unfixed edge: a signal that arrives in the instant
// between the command finishing and the returned stop function running can be
// consumed without effect (the command exits normally instead of dying by the
// signal). Draining the channel in stop cannot close this: signals are
// dispatched to the channel asynchronously by the Go runtime, and one already
// caught but not yet dispatched is dropped by signal.Stop. A fix would need a
// different mechanism (for example blocking signals) that would change
// unrelated behaviour. The consequence is benign: the command had already
// completed and its staging directories were already removed by the normal
// path. I did not prove the runtime dispatch behaviour empirically; the
// decision rests on the semantics documented for os/signal.
func InstallStagingSignalCleanup() (stop func()) {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-ch:
			removeRegisteredStagingDirs()
			signal.Reset(sig)
			if s, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(syscall.Getpid(), s)
			}
		case <-done:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(ch)
			close(done)
		})
	}
}
