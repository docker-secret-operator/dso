// Package parser reads standard docker-compose.yml files and produces a
// validated *models.DSOConfig ready for the transformer.
//
// Auto-detection rules (applied per service, in priority order):
//  1. x-dso.enabled = false  → never eligible (explicit opt-out)
//  2. x-dso.enabled = true   → always eligible (explicit opt-in, even DBs)
//  3. Has ports + not a known database image → eligible
//  4. Everything else → pass-through, unchanged
//
// The legacy dso-proxy service block is still accepted for backward
// compatibility but triggers a deprecation warning.
package parser

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/docker-secret-operator/dso/internal/models"
	"gopkg.in/yaml.v3"
)

// knownDatabaseImages lists image base-names (without tags or registries) that
// should NOT be proxied by default. Users can override with x-dso.enabled=true.
var knownDatabaseImages = map[string]bool{
	"mysql":         true,
	"postgres":      true,
	"postgresql":    true,
	"mariadb":       true,
	"mongo":         true,
	"mongodb":       true,
	"redis":         true,
	"elasticsearch": true,
	"cassandra":     true,
	"rabbitmq":      true,
	"memcached":     true,
	"couchdb":       true,
	"influxdb":      true,
	"neo4j":         true,
	"kafka":         true,
	"zookeeper":     true,
	"etcd":          true,
}

// rawCompose is the top-level docker-compose.yml structure used during the
// initial unmarshalling pass. Using yaml.Node for services lets us decode each
// service independently without losing unknown fields.
type rawCompose struct {
	Version  string                  `yaml:"version"`
	Services map[string]yaml.Node    `yaml:"services"`
	Networks interface{}             `yaml:"networks"`
	Volumes  interface{}             `yaml:"volumes"`
}

// rawLegacyProxy matches the shape of the old dso-proxy service stanza.
type rawLegacyProxy struct {
	Containers []models.ProxyTarget `yaml:"containers"`
}

// ParseFile reads the docker-compose.yml at path and returns a validated
// *models.DSOConfig, any non-fatal warnings (e.g. deprecation notices), and
// any fatal error.
func ParseFile(path string) (*models.DSOConfig, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("parser: cannot read %q: %w", path, err)
	}
	return Parse(data)
}

// Parse accepts raw YAML bytes and returns a validated *models.DSOConfig.
// This variant is useful for unit tests that don't need a real file on disk.
func Parse(data []byte) (*models.DSOConfig, []string, error) {
	var raw rawCompose
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parser: invalid YAML: %w", err)
	}

	if len(raw.Services) == 0 {
		return nil, nil, fmt.Errorf("parser: no services found in compose file")
	}

	var warnings []string
	cfg := &models.DSOConfig{
		Version:     raw.Version,
		RawNetworks: raw.Networks,
		RawVolumes:  raw.Volumes,
	}

	// ── Backward compatibility: detect legacy dso-proxy block ────────────────
	if legacyNode, hasLegacy := raw.Services["dso-proxy"]; hasLegacy {
		warnings = append(warnings,
			"WARNING: The dso-proxy block is deprecated. "+
				"Use a standard docker-compose.yml with optional x-dso extension fields instead. "+
				"Support will be removed in a future major release.")

		var lp rawLegacyProxy
		if err := legacyNode.Decode(&lp); err != nil {
			return nil, warnings, fmt.Errorf("parser: cannot decode legacy dso-proxy block: %w", err)
		}
		cfg.DeprecatedProxy = &models.LegacyProxyConfig{Containers: lp.Containers}
	}

	// Build a lookup of services the legacy proxy explicitly targets so we can
	// force-enable eligibility even when auto-detection would say no.
	legacyTargets := buildLegacyTargetIndex(cfg.DeprecatedProxy)

	// ── Parse backing services in alphabetical order (deterministic output) ──
	names := make([]string, 0, len(raw.Services))
	for name := range raw.Services {
		if name != "dso-proxy" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		node := raw.Services[name]
		svc, err := parseService(name, &node)
		if err != nil {
			return nil, warnings, fmt.Errorf("parser: service %q: %w", name, err)
		}

		// Legacy proxy block forces eligibility for its declared targets,
		// overriding the auto-detection result.
		if legacyPorts, isLegacyTarget := legacyTargets[name]; isLegacyTarget {
			svc.IsEligible = true
			// Patch ports from the legacy target if the service didn't declare any.
			if len(svc.Ports) == 0 {
				svc.Ports = legacyPorts
			}
		}

		cfg.Services = append(cfg.Services, svc)
	}

	return cfg, warnings, nil
}

// parseService decodes a single service YAML node into a ParsedService.
func parseService(name string, node *yaml.Node) (models.ParsedService, error) {
	// Decode into a generic map so we preserve every field without bespoke
	// per-field handling — unknown keys are kept as-is for pass-through.
	var raw map[string]interface{}
	if err := node.Decode(&raw); err != nil {
		return models.ParsedService{}, fmt.Errorf("cannot decode service: %w", err)
	}

	// Extract x-dso options (may be absent).
	var dsoOpts models.DSOOptions
	if xdso, ok := raw["x-dso"]; ok {
		if err := remarshal(xdso, &dsoOpts); err != nil {
			return models.ParsedService{}, fmt.Errorf("cannot decode x-dso block: %w", err)
		}
	}

	// Extract image (needed for database detection).
	image, _ := raw["image"].(string)

	// Extract ports into a clean []string.
	ports := extractPorts(raw["ports"])

	// Build RawFields: everything except the DSO-managed keys.
	rawFields := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		switch k {
		case "ports", "x-dso", "container_name":
			// ports   → managed by DSO (moved to proxy or restored unchanged)
			// x-dso   → consumed by parser, not passed to Compose
			// container_name → forbidden in DSO proxy architecture
			continue
		default:
			rawFields[k] = v
		}
	}

	return models.ParsedService{
		Name:       name,
		RawFields:  rawFields,
		Ports:      ports,
		DSO:        dsoOpts,
		IsEligible: determineEligibility(dsoOpts, ports, image),
	}, nil
}

// determineEligibility applies the four auto-detection rules in priority order.
func determineEligibility(opts models.DSOOptions, ports []string, image string) bool {
	// Rule 1: explicit opt-out always wins.
	if opts.Enabled != nil && !*opts.Enabled {
		return false
	}
	// Rule 2: explicit opt-in always wins (even for database images).
	if opts.Enabled != nil && *opts.Enabled {
		return true
	}
	// Rule 3: auto — must have at least one port AND not a known database image.
	return len(ports) > 0 && !isKnownDatabase(image)
}

// isKnownDatabase returns true if the image base-name matches a known stateful
// service that should not be traffic-proxied by default.
func isKnownDatabase(image string) bool {
	if image == "" {
		return false
	}
	// Strip registry prefix: "docker.io/library/mysql:8.0" → "mysql:8.0"
	base := image
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	// Strip tag: "mysql:8.0" → "mysql"
	if idx := strings.Index(base, ":"); idx >= 0 {
		base = base[:idx]
	}
	return knownDatabaseImages[strings.ToLower(base)]
}

// extractPorts coerces the raw `ports` field value (which can be
// []interface{} with string or int elements after yaml unmarshalling) into a
// clean []string of port-mapping tokens.
func extractPorts(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	var ports []string
	switch v := raw.(type) {
	case []interface{}:
		for _, p := range v {
			var s string
			switch pv := p.(type) {
			case string:
				s = strings.TrimSpace(pv)
			case int:
				s = fmt.Sprintf("%d", pv)
			default:
				s = strings.TrimSpace(fmt.Sprintf("%v", pv))
			}
			if s != "" {
				ports = append(ports, s)
			}
		}
	case []string:
		for _, s := range v {
			if s = strings.TrimSpace(s); s != "" {
				ports = append(ports, s)
			}
		}
	}
	return ports
}

// remarshal round-trips a value through YAML marshal+unmarshal into dest.
// Used to safely decode the x-dso field (which arrives as map[string]interface{})
// into a typed struct.
func remarshal(src interface{}, dest interface{}) error {
	b, err := yaml.Marshal(src)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, dest)
}

// ── Legacy target index ──────────────────────────────────────────────────────

// legacyTargetIndex maps service name → port strings from the legacy dso-proxy block.
type legacyTargetIndex map[string][]string

func buildLegacyTargetIndex(lp *models.LegacyProxyConfig) legacyTargetIndex {
	idx := make(legacyTargetIndex)
	if lp == nil {
		return idx
	}
	for _, t := range lp.Containers {
		idx[t.Name] = t.Ports
	}
	return idx
}
