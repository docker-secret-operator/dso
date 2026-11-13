//go:build dsofault

// This file exists only in binaries built with `-tags dsofault` (the crash-recovery
// test binary). A release build never contains it.
package faultinject

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
)

// Enabled is true: this binary can be told to kill itself at a named point.
const Enabled = true

// Environment contract of the tagged binary (read once, at first use).
//
//	DSO_FAULT_POINT   exact point name to die at; empty/unset disables the hook
//	DSO_FAULT_NTH     die on the Nth hit of that point (default 1)
//	DSO_FAULT_MARKER  file written (point, pid, nth) immediately before dying, so the
//	                  harness can verify WHICH process died at WHICH point
//	DSO_FAULT_STATE_DIR / DSO_FAULT_LOCK_DIR / DSO_FAULT_PLUGIN_DIR  per-test state,
//	                  lock and provider-plugin directories (the production values are
//	                  fixed system paths)
const (
	envPoint    = "DSO_FAULT_POINT"
	envNth      = "DSO_FAULT_NTH"
	envMarker   = "DSO_FAULT_MARKER"
	envStateDir = "DSO_FAULT_STATE_DIR"
	envLockDir  = "DSO_FAULT_LOCK_DIR"
	envPlugDir  = "DSO_FAULT_PLUGIN_DIR"
)

type config struct {
	point  string
	nth    int64
	marker string
}

var (
	once   sync.Once
	cfg    config
	hits   atomic.Int64
	getenv = os.Getenv

	// die terminates the process. Tests replace it; production of the tagged binary
	// uses SIGKILL on ITSELF only: no deferred cleanup, no graceful shutdown, exactly
	// what an unexpected process death looks like.
	die = func() {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {} // SIGKILL cannot be handled; never run past this point
	}
)

func load() {
	cfg = config{point: getenv(envPoint), nth: 1, marker: getenv(envMarker)}
	if n, err := strconv.ParseInt(getenv(envNth), 10, 64); err == nil && n > 0 {
		cfg.nth = n
	}
}

// reset re-reads the environment (tests only).
func reset() {
	once = sync.Once{}
	hits.Store(0)
}

// Hit dies when point EXACTLY equals the configured point and this is the
// configured hit number. It returns true only when die has been replaced (tests);
// the real die never returns. Any other point is a no-op.
func Hit(point string) bool {
	once.Do(load)
	if cfg.point == "" || point != cfg.point {
		return false
	}
	if hits.Add(1) != cfg.nth {
		return false
	}
	if cfg.marker != "" {
		_ = os.WriteFile(cfg.marker, []byte("point="+point+"\npid="+strconv.Itoa(os.Getpid())+"\nnth="+strconv.FormatInt(cfg.nth, 10)+"\n"), 0o600)
	}
	die()
	return true
}

// Param returns the override for a whitelisted setting name.
func Param(name string) (string, bool) {
	var key string
	switch name {
	case "state_dir":
		key = envStateDir
	case "lock_dir":
		key = envLockDir
	case "plugin_dir":
		key = envPlugDir
	default:
		return "", false
	}
	v := getenv(key)
	return v, v != ""
}
