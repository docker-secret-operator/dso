package harness

import (
	"fmt"
	"sort"
	"strings"
)

// SecretSpec is one secret of the generated agent configuration.
type SecretSpec struct {
	Name     string
	Mode     string            // "env" or "file"
	Path     string            // file mode: in-container directory
	Mapping  map[string]string // vault key -> env var name (env mode)
	Strategy Strategy
	Signal   string // file mode
	UID, GID int    // file mode: required non-root owner of the delivered files
}

// AgentConfig describes the dso.yaml of the agent under test. The generated file
// uses the production provider type ("vault", served by the real plugin) and
// per-secret rotation settings only; nothing here is a test-only config key.
type AgentConfig struct {
	OwnerID       string
	VaultAddr     string
	VaultToken    string
	PollInterval  string // only the initial poll matters in these tests
	HealthTimeout string
	Secrets       []SecretSpec
}

// YAML renders the configuration.
func (c AgentConfig) YAML() string {
	var b strings.Builder
	fmt.Fprintf(&b, "providers:\n  local:\n    type: vault\n    retry: {attempts: 1, backoff: 1s}\n    config:\n      address: %q\n      token: %q\n      mount: secret\n", c.VaultAddr, c.VaultToken)
	poll, health := c.PollInterval, c.HealthTimeout
	if poll == "" {
		poll = "1h"
	}
	if health == "" {
		health = "60s"
	}
	fmt.Fprintf(&b, "agent:\n  owner_id: %s\n  watch:\n    polling_interval: %s\n  rotation:\n    health_check_timeout: %s\n", c.OwnerID, poll, health)
	b.WriteString("secrets:\n")
	for _, s := range c.Secrets {
		fmt.Fprintf(&b, "  - name: %s\n    provider: local\n", s.Name)
		if s.Mode == "file" {
			fmt.Fprintf(&b, "    inject: {type: file, path: %s, uid: %d, gid: %d}\n", s.Path, s.UID, s.GID)
		} else {
			b.WriteString("    inject: {type: env}\n")
		}
		fmt.Fprintf(&b, "    rotation: {enabled: true, strategy: %s", s.Strategy)
		if s.Signal != "" {
			fmt.Fprintf(&b, ", signal: %s", s.Signal)
		}
		b.WriteString("}\n")
		if len(s.Mapping) > 0 {
			keys := make([]string, 0, len(s.Mapping))
			for k := range s.Mapping {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString("    mappings:\n")
			for _, k := range keys {
				fmt.Fprintf(&b, "      %s: %s\n", k, s.Mapping[k])
			}
		}
	}
	return b.String()
}
