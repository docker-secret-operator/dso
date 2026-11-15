package setup

import (
	"strings"
	"testing"
)

// The planner and repair unit templates declare the runtime directory with the
// same invariant as the shipped bootstrap unit, WITHOUT changing which binary
// they execute (the repair template's binary name is a separate, tracked
// defect and must not be altered here).
func TestServiceUnitTemplates_DeclareTheRuntimeDirectory(t *testing.T) {
	cases := []struct {
		name     string
		unit     string
		execLine string
	}{
		{"planner", string(dsoServiceUnit), "ExecStart=/usr/local/bin/dso agent\n"},
		{"repair", dsoAgentUnitContent("dso-agent"), "ExecStart=/usr/local/bin/dso-agent\n"},
	}
	for _, c := range cases {
		for _, want := range []string{
			"RuntimeDirectory=dso\n",
			"RuntimeDirectoryMode=0750\n",
			// Preserve keeps the event log across restarts, as these units
			// behaved before the directive existed.
			"RuntimeDirectoryPreserve=yes\n",
			c.execLine,
		} {
			if !strings.Contains(c.unit, want) {
				t.Errorf("%s unit lacks %q", c.name, strings.TrimSpace(want))
			}
		}
		if strings.Contains(c.unit, "RuntimeDirectoryMode=0775") {
			t.Errorf("%s unit widens the runtime directory", c.name)
		}
		if strings.Count(c.unit, "ExecStart=") != 1 {
			t.Errorf("%s unit must keep exactly one ExecStart", c.name)
		}
	}
}
