//go:build !linux

package agent

// tightenUmask is a no-op on non-Linux platforms (macOS, Windows).
// The production target is Linux; on other platforms the caller's existing
// umask is used and os.Chmod still tightens permissions after Listen.
func tightenUmask() int { return 0 }

// restoreUmask is a no-op on non-Linux platforms.
func restoreUmask(_ int) {}
