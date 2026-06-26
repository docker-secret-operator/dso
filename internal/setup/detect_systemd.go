package setup

import (
	"context"
	"os/exec"
	"strings"
)

// detectSystemd checks whether systemd is available and retrieves its version.
func detectSystemd(ctx context.Context, cfg DetectorConfig) SystemdInfo {
	info := SystemdInfo{}

	path, err := cfg.LookPath("systemctl")
	if err != nil {
		return info
	}
	info.BinaryPath = path
	info.Available = true

	vCtx, cancel := context.WithTimeout(ctx, cfg.SystemdTimeout)
	defer cancel()

	out, err := exec.CommandContext(vCtx, info.BinaryPath, "--version").Output()
	if err != nil {
		return info
	}

	// First line format: "systemd 252 (252.19-1~deb12u1)"
	first, _, _ := strings.Cut(string(out), "\n")
	fields := strings.Fields(first)
	if len(fields) >= 2 {
		info.Version = fields[1]
	}

	return info
}
