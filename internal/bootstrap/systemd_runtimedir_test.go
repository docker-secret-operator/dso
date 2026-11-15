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
