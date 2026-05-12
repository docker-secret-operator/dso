package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestConfig_LegacyUnmarshal_Full(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		yaml  string
		check func(*testing.T, *SecretMapping)
	}{
		{
			name: "legacy inject and rotation",
			yaml: `
inject: env
rotation: true
`,
			check: func(t *testing.T, s *SecretMapping) {
				if s.Inject.Type != "env" || !s.Rotation.Enabled {
					t.Errorf("Legacy inject/rotation failed: %+v", s)
				}
			},
		},
		{
			name: "legacy reload_strategy",
			yaml: `
reload_strategy:
  type: rolling
`,
			check: func(t *testing.T, s *SecretMapping) {
				if s.Rotation.Strategy != "rolling" || !s.Rotation.Enabled {
					t.Errorf("Legacy reload_strategy failed: %+v", s)
				}
			},
		},
	}

	for _, tt := range tests {
		var s SecretMapping
		if err := yaml.Unmarshal([]byte(tt.yaml), &s); err != nil {
			t.Errorf("%s: unmarshal failed: %v", tt.name, err)
			continue
		}
		tt.check(t, &s)
	}
}

func TestConfig_PathWithinDir_Edge(t *testing.T) {
	t.Parallel()
	if pathWithinDir("/tmp/a", "/tmp") == false {
		t.Error("Expected /tmp/a to be within /tmp")
	}
	if pathWithinDir("/etc/passwd", "/tmp") == true {
		t.Error("Expected /etc/passwd NOT to be within /tmp")
	}
}
