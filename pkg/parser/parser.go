// Package parser reads and validates dso-compose.yml files.
// It is responsible for decoding the raw YAML, splitting the special
// `dso-proxy` service stanza away from the regular service definitions,
// and asserting that every proxy target references a real service.
package parser

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker-secret-operator/dso/internal/models"
	"gopkg.in/yaml.v3"
)

// rawDSOConfig is used solely during the initial unmarshal step so that we can
// grab the raw `services` map (including the `dso-proxy` key) before we
// separate concerns into the public DSOConfig struct.
type rawDSOConfig struct {
	Version  string                     `yaml:"version"`
	Services map[string]yaml.Node `yaml:"services"`
}

// rawProxyService matches the shape of the `dso-proxy` stanza inside services.
type rawProxyService struct {
	Containers []models.ProxyTarget `yaml:"containers"`
}

// ParseFile reads the dso-compose.yml at the given path, validates it, and
// returns a fully populated *models.DSOConfig on success.
//
// Validation rules:
//   - The file must be readable and contain valid YAML.
//   - At least one non-proxy service must be declared.
//   - Every proxy target must reference a service that exists in the services map.
//   - Every proxy target must declare at least one port mapping.
func ParseFile(path string) (*models.DSOConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("parser: cannot read %q: %w", path, err)
	}

	return Parse(data)
}

// Parse accepts raw YAML bytes and returns a validated *models.DSOConfig.
// This variant is useful for testing without a real file on disk.
func Parse(data []byte) (*models.DSOConfig, error) {
	// Step 1: decode into the raw intermediate struct so we can inspect every
	// top-level service key before splitting off the proxy stanza.
	var raw rawDSOConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parser: invalid YAML: %w", err)
	}

	if len(raw.Services) == 0 {
		return nil, fmt.Errorf("parser: dso-compose.yml must declare at least one service")
	}

	// Step 2: extract and decode the `dso-proxy` stanza if present.
	var proxyConfig models.ProxyConfig

	proxyNode, hasProxy := raw.Services["dso-proxy"]
	if hasProxy {
		var rp rawProxyService
		if err := proxyNode.Decode(&rp); err != nil {
			return nil, fmt.Errorf("parser: cannot decode dso-proxy stanza: %w", err)
		}
		proxyConfig.Containers = rp.Containers
	}

	// Step 3: build the backing service map (everything except dso-proxy).
	backingServices := make(map[string]interface{}, len(raw.Services))
	for name, node := range raw.Services {
		if name == "dso-proxy" {
			continue
		}

		// Decode each service node into a generic map so the transformer can
		// iterate over arbitrary fields without bespoke per-field handling.
		var svcMap map[string]interface{}
		if err := node.Decode(&svcMap); err != nil {
			return nil, fmt.Errorf("parser: cannot decode service %q: %w", name, err)
		}
		backingServices[name] = svcMap
	}

	if len(backingServices) == 0 {
		return nil, fmt.Errorf("parser: dso-compose.yml has no backing services (only dso-proxy)")
	}

	// Step 4: validate proxy references.
	if hasProxy {
		if err := validateProxyTargets(proxyConfig.Containers, backingServices); err != nil {
			return nil, err
		}
	}

	cfg := &models.DSOConfig{
		Version:  raw.Version,
		DSO:      proxyConfig,
		Services: backingServices,
	}

	return cfg, nil
}

// validateProxyTargets checks that:
//  1. Every proxy container references a declared backing service.
//  2. Each proxy target has at least one port mapping.
//  3. Each port mapping is well-formed (non-empty, contains ":").
func validateProxyTargets(targets []models.ProxyTarget, services map[string]interface{}) error {
	for _, target := range targets {
		if target.Name == "" {
			return fmt.Errorf("parser: proxy container entry is missing a 'name' field")
		}

		if _, exists := services[target.Name]; !exists {
			return fmt.Errorf(
				"parser: proxy references unknown service %q — make sure it is declared in the services block",
				target.Name,
			)
		}

		if len(target.Ports) == 0 {
			return fmt.Errorf(
				"parser: proxy target %q must declare at least one port mapping",
				target.Name,
			)
		}

		for _, p := range target.Ports {
			trimmed := strings.TrimSpace(p)
			if trimmed == "" {
				return fmt.Errorf(
					"parser: proxy target %q has an empty port mapping entry",
					target.Name,
				)
			}
			if !strings.Contains(trimmed, ":") {
				return fmt.Errorf(
					"parser: proxy target %q has invalid port mapping %q — expected format 'hostPort:containerPort'",
					target.Name, trimmed,
				)
			}
		}
	}
	return nil
}
