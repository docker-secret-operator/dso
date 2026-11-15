package setup

import (
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
	"github.com/docker-secret-operator/dso/internal/runtimedir"
)

// serviceSection returns the key=value lines of the unit's [Service] section.
func serviceSection(t *testing.T, unit string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	in := false
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			in = line == "[Service]"
		case in && line != "" && !strings.HasPrefix(line, "#"):
			k, v, ok := strings.Cut(line, "=")
			if ok {
				out[k] = append(out[k], v)
			}
		}
	}
	return out
}

// The three writers of dso-agent.service must agree on the runtime directory.
func TestAllUnitWriters_ProduceIdenticalRuntimeDirectorySettings(t *testing.T) {
	units := map[string]string{
		"bootstrap": bootstrap.NewSystemdManager(nil, true).GenerateServiceFile(),
		"planner":   string(dsoServiceUnit),
		"repair":    dsoAgentUnitContent("dso-agent"),
	}
	want := map[string][]string{
		"RuntimeDirectory":         {"dso"},
		"RuntimeDirectoryMode":     {"0750"},
		"RuntimeDirectoryPreserve": {"restart"},
	}
	for name, unit := range units {
		sec := serviceSection(t, unit)
		for k, v := range want {
			if strings.Join(sec[k], ",") != strings.Join(v, ",") {
				t.Errorf("%s: %s = %v, want %v", name, k, sec[k], v)
			}
		}
		for _, line := range sec["ExecStartPre"] {
			if strings.Contains(line, "/run/dso") {
				t.Errorf("%s: redundant ExecStartPre for /run/dso: %q", name, line)
			}
		}
		if strings.Contains(unit, "{{") {
			t.Errorf("%s: unexpanded placeholder", name)
		}
		if !strings.Contains(unit, runtimedir.UnitDirectives) {
			t.Errorf("%s: does not embed runtimedir.UnitDirectives verbatim", name)
		}
	}
}

// The three writers must emit identical sandbox settings, and none may
// switch the service identity (the agent stays root: see SECURITY.md).
func TestAllUnitWriters_ProduceIdenticalHardening(t *testing.T) {
	units := map[string]string{
		"bootstrap": bootstrap.NewSystemdManager(nil, true).GenerateServiceFile(),
		"planner":   string(dsoServiceUnit),
		"repair":    dsoAgentUnitContent("dso-agent"),
	}
	want := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(runtimedir.HardeningDirectives), "\n") {
		k, v, _ := strings.Cut(line, "=")
		want[k] = []string{v}
	}
	for name, unit := range units {
		sec := serviceSection(t, unit)
		if !strings.Contains(unit, runtimedir.HardeningDirectives) {
			t.Errorf("%s: does not embed runtimedir.HardeningDirectives verbatim", name)
		}
		for k, v := range want {
			if strings.Join(sec[k], ",") != strings.Join(v, ",") {
				t.Errorf("%s: %s = %v, want %v", name, k, sec[k], v)
			}
		}
		// Never switch identity or add directives that break required behaviour.
		for _, k := range []string{"User", "Group"} {
			for _, v := range sec[k] {
				if v != "root" {
					t.Errorf("%s: %s=%s; the agent must stay root", name, k, v)
				}
			}
		}
		for _, banned := range []string{"MemoryDenyWriteExecute", "UMask", "CapabilityBoundingSet", "PrivateNetwork", "ProtectProc", "SystemCallFilter"} {
			if _, ok := sec[banned]; ok {
				t.Errorf("%s: %s is unverified without a live systemd host (deferred)", name, banned)
			}
		}
		if strings.Contains(unit, "{{") {
			t.Errorf("%s: unexpanded placeholder", name)
		}
	}
}
