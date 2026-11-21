package bootstrap

import (
	"strings"
	"testing"
)

// The shipped unit already declares the runtime directory; this pins it. The
// directives are an extra: the agent enforces the same invariant itself.
func TestGenerateServiceFile_DeclaresTheRuntimeDirectory(t *testing.T) {
	unit := NewSystemdManager(&MockLogger{}, true).GenerateServiceFile()
	for _, want := range []string{
		"RuntimeDirectory=dso\n",
		"RuntimeDirectoryMode=0750\n",
		"RuntimeDirectoryPreserve=restart\n",
		"User=root\n",
		"ExecStartPre=/usr/bin/install -d -m 0755 /run/docker/plugins\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("the shipped unit lacks %q", strings.TrimSpace(want))
		}
	}
	// systemd creates /run/dso and the agent enforces it; no redundant install.
	if strings.Contains(unit, "install -d -m 0750 /run/dso") || strings.Contains(unit, "{{RUNTIME_DIRECTORY}}") {
		t.Error("the unit keeps the redundant /run/dso install or an unexpanded placeholder")
	}
	if strings.Contains(unit, "RuntimeDirectoryMode=0775") || strings.Contains(unit, "install -d -m 0775 /run/dso") {
		t.Error("the unit widens the runtime directory")
	}
}

// The shipped unit's filesystem sandbox must keep every path the agent
// writes, and must not change the service identity.
func TestGenerateServiceFile_SandboxKeepsRequiredPaths(t *testing.T) {
	unit := NewSystemdManager(&MockLogger{}, true).GenerateServiceFile()
	var rw string
	for _, line := range strings.Split(unit, "\n") {
		if v, ok := strings.CutPrefix(line, "ReadWritePaths="); ok {
			rw = v
		}
	}
	fields := strings.Fields(rw)
	for _, need := range []string{"/etc/dso", "/var/lib/dso", "/var/log/dso", "/run/dso", "/run/docker/plugins"} {
		found := false
		for _, f := range fields {
			if f == need {
				found = true
			}
		}
		if !found {
			t.Errorf("ReadWritePaths %q lacks required path %s", rw, need)
		}
	}
	for _, want := range []string{"User=root\n", "Group=root\n", "ProtectSystem=strict\n", "NoNewPrivileges=true\n", "SystemCallArchitectures=native\n"} {
		if !strings.Contains(unit, want) {
			t.Errorf("the shipped unit lacks %q", strings.TrimSpace(want))
		}
	}
	for _, banned := range []string{"MemoryDenyWriteExecute", "UMask=", "CapabilityBoundingSet", "User=dso", "ProtectSystem=full"} {
		if strings.Contains(unit, banned) {
			t.Errorf("the shipped unit contains %q", banned)
		}
	}
	if strings.Contains(unit, "{{") {
		t.Error("unexpanded placeholder")
	}
}
