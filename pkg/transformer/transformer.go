// Package transformer converts a validated *models.DSOConfig into a canonical
// docker-compose.generated.yml that is ready for `docker compose up`.
//
// For each eligible service (IsEligible == true):
//   - `ports` are removed from the service and converted to `expose`.
//   - A proxy service `dso-proxy-<name>` takes ownership of the host ports.
//   - `dso_mesh` network and DSO labels are injected.
//
// For ineligible services (databases, explicitly disabled, no ports):
//   - All fields including `ports` are passed through unchanged.
//   - `container_name` is still stripped (architecture constraint).
//
// The function also returns a human-readable summary slice suitable for
// printing as a "diff" to the user before the generated file is written.
package transformer

import (
	"fmt"
	"strings"

	"github.com/docker-secret-operator/dso/internal/models"
	"gopkg.in/yaml.v3"
)

const (
	// meshNetwork is the shared bridge network joining all DSO-managed services
	// and their proxy counterparts.
	meshNetwork = "dso_mesh"

	// proxyImage is the DSO Go-based TCP reverse proxy that replaced socat.
	// Build with: docker build -t docker-secret-operator/dso-proxy:latest
	//             -f Dockerfile.proxy .
	proxyImage = "docker-secret-operator/dso-proxy:latest"

	// proxyAPIPort is the HTTP control API port exposed on the dso_mesh network.
	// External traffic cannot reach this port; it is only accessible to other
	// services on the mesh (e.g. the DSO agent for dynamic backend management).
	proxyAPIPort = 9900
)

// composeOutput is the full document structure written to docker-compose.generated.yml.
type composeOutput struct {
	Version  string                            `yaml:"version"`
	Services map[string]map[string]interface{} `yaml:"services"`
	Networks map[string]interface{}            `yaml:"networks,omitempty"`
	Volumes  interface{}                       `yaml:"volumes,omitempty"`
}

// Transform converts a parsed *models.DSOConfig into the bytes of a valid
// docker-compose.generated.yml.
//
// Returns:
//   - out: the YAML bytes to write to disk.
//   - summary: human-readable lines describing every transformation applied.
//   - err: non-nil on any fatal transformation failure.
func Transform(cfg *models.DSOConfig) (out []byte, summary []string, err error) {
	doc := composeOutput{
		Version:  cfg.Version,
		Services: make(map[string]map[string]interface{}),
		Networks: buildNetworks(cfg.RawNetworks),
		Volumes:  cfg.RawVolumes,
	}

	// Surface the deprecation warning in the transform summary as well.
	if cfg.DeprecatedProxy != nil {
		summary = append(summary,
			"⚠  dso-proxy block detected — migrating to auto-detection (deprecated, will be removed in v4)")
	}

	for _, svc := range cfg.Services {
		if !svc.IsEligible {
			// Pass-through: preserve everything including the original ports.
			// container_name was already stripped by the parser.
			passThrough := copyMap(svc.RawFields)
			if len(svc.Ports) > 0 {
				passThrough["ports"] = toInterfaceSlice(svc.Ports)
			}
			doc.Services[svc.Name] = passThrough
			continue
		}

		// ── Eligible: transform the backing service ───────────────────────────
		backing, err := transformBacking(svc)
		if err != nil {
			return nil, summary, fmt.Errorf("transformer: service %q: %w", svc.Name, err)
		}
		doc.Services[svc.Name] = backing
		summary = append(summary,
			fmt.Sprintf("DSO: Enabling zero-downtime for service '%s'", svc.Name))

		// ── Generate the proxy service ────────────────────────────────────────
		proxySvc, err := buildProxy(svc)
		if err != nil {
			return nil, summary, fmt.Errorf("transformer: proxy for %q: %w", svc.Name, err)
		}
		doc.Services["dso-proxy-"+svc.Name] = proxySvc

		for _, p := range svc.Ports {
			hp := hostPort(p)
			summary = append(summary,
				fmt.Sprintf("DSO: Injecting proxy for port %s → %s", hp, svc.Name))
		}
	}

	b, err := yaml.Marshal(doc)
	return b, summary, err
}

// transformBacking applies the DSO rules to a single eligible service:
//   - Moves ports to expose.
//   - Injects dso_mesh network.
//   - Injects dso.service and dso.managed labels (+ dso.strategy if set).
func transformBacking(svc models.ParsedService) (map[string]interface{}, error) {
	out := copyMap(svc.RawFields)

	// Convert ports to expose entries (container-side only, no host binding).
	if expose := portsToExpose(svc.Ports); len(expose) > 0 {
		out["expose"] = expose
	}

	// Inject dso_mesh network (merge with any existing network declarations).
	out["networks"] = mergeNetworks(out["networks"], meshNetwork)

	// Build the DSO label set.
	extraLabels := map[string]string{
		"dso.service": svc.Name,
		"dso.managed": "true",
	}
	if svc.DSO.Strategy != "" {
		extraLabels["dso.strategy"] = svc.DSO.Strategy
	}
	out["labels"] = mergeLabels(out["labels"], extraLabels)

	return out, nil
}

// buildProxy generates the synthetic dso-proxy-<name> service that owns all
// host port bindings for the proxied backing service.
//
// The proxy is configured entirely via environment variables:
//
//	 DSO_PROXY_BINDS    — one entry per port mapping ("listenPort:service:targetPort")
//	 DSO_PROXY_BACKENDS — initial backend to register ("id:service:host:port")
//	 DSO_PROXY_API_PORT — HTTP control API port (always proxyAPIPort)
//
// Multi-port services are handled natively: every port mapping in svc.Ports
// produces one entry in DSO_PROXY_BINDS. The Go proxy opens one TCP listener
// per entry, replacing the previous single-port socat command.
func buildProxy(svc models.ParsedService) (map[string]interface{}, error) {
	if len(svc.Ports) == 0 {
		return nil, fmt.Errorf("service %q has no ports to proxy", svc.Name)
	}

	// Build DSO_PROXY_BINDS: "listenPort:service:targetPort,..."
	bindSpecs := make([]string, 0, len(svc.Ports))
	for _, p := range svc.Ports {
		p = strings.TrimSpace(p)
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf(
				"service %q: malformed port mapping %q (expected 'hostPort:containerPort')",
				svc.Name, p)
		}
		hostP := strings.TrimSpace(parts[0])
		containerP := strings.TrimSpace(parts[1])
		bindSpecs = append(bindSpecs, fmt.Sprintf("%s:%s:%s", hostP, svc.Name, containerP))
	}

	// Build DSO_PROXY_BACKENDS: the initial backend entry.
	// host = service DNS name (Docker resolves this on the dso_mesh network).
	// port = 0 → proxy uses binding.TargetPort when dialling, which is set per
	// listener from DSO_PROXY_BINDS and supports multi-port correctly.
	initialBackend := fmt.Sprintf("%s-default:%s:%s:0", svc.Name, svc.Name, svc.Name)

	return map[string]interface{}{
		"image":      proxyImage,
		"ports":      toInterfaceSlice(svc.Ports),
		"networks":   []string{meshNetwork},
		"depends_on": []string{svc.Name},
		"expose":     []string{fmt.Sprintf("%d", proxyAPIPort)},
		"environment": map[string]string{
			"DSO_PROXY_BINDS":    strings.Join(bindSpecs, ","),
			"DSO_PROXY_BACKENDS": initialBackend,
			"DSO_PROXY_API_PORT": fmt.Sprintf("%d", proxyAPIPort),
		},
		"labels": map[string]string{
			"dso.proxy":   "true",
			"dso.service": svc.Name,
			"dso.managed": "true",
		},
	}, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// buildNetworks constructs the top-level networks map, always including
// dso_mesh and merging in any user-defined networks from the original file.
func buildNetworks(existing interface{}) map[string]interface{} {
	networks := map[string]interface{}{
		meshNetwork: map[string]interface{}{"driver": "bridge"},
	}
	switch v := existing.(type) {
	case map[string]interface{}:
		for k, val := range v {
			networks[k] = val
		}
	case map[interface{}]interface{}:
		for k, val := range v {
			networks[fmt.Sprintf("%v", k)] = val
		}
	}
	return networks
}

// mergeNetworks returns a deduplicated []string of network names, always
// placing meshNetwork first.
func mergeNetworks(existing interface{}, add string) []string {
	seen := map[string]bool{add: true}
	result := []string{add}

	switch v := existing.(type) {
	case []interface{}:
		for _, n := range v {
			s := fmt.Sprintf("%v", n)
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	case []string:
		for _, s := range v {
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	case map[string]interface{}:
		for k := range v {
			if !seen[k] {
				seen[k] = true
				result = append(result, k)
			}
		}
	}
	return result
}

// mergeLabels consolidates an existing labels value (list or map form) with
// additional label entries, returning a unified map[string]string.
func mergeLabels(existing interface{}, add map[string]string) map[string]string {
	out := make(map[string]string)
	switch v := existing.(type) {
	case map[interface{}]interface{}:
		for k, val := range v {
			out[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", val)
		}
	case map[string]interface{}:
		for k, val := range v {
			out[k] = fmt.Sprintf("%v", val)
		}
	case map[string]string:
		for k, val := range v {
			out[k] = val
		}
	case []interface{}:
		// "KEY=VALUE" list form.
		for _, item := range v {
			if p := strings.SplitN(fmt.Sprintf("%v", item), "=", 2); len(p) == 2 {
				out[p[0]] = p[1]
			}
		}
	}
	for k, val := range add {
		out[k] = val
	}
	return out
}

// portsToExpose converts a list of port-mapping strings (e.g. "8080:3306" or
// "3000") into a list of container-side-only port strings for the expose key.
func portsToExpose(ports []string) []string {
	var expose []string
	for _, p := range ports {
		container := p
		// Strip host prefix: "8080:3306" → "3306"
		if idx := strings.LastIndex(p, ":"); idx >= 0 {
			container = p[idx+1:]
		}
		// Strip protocol suffix: "3306/tcp" → "3306"
		if idx := strings.Index(container, "/"); idx >= 0 {
			container = container[:idx]
		}
		if s := strings.TrimSpace(container); s != "" {
			expose = append(expose, s)
		}
	}
	return expose
}

// hostPort extracts the host-side portion of a port-mapping string.
// "8080:3306" → "8080", "3000" → "3000".
func hostPort(mapping string) string {
	parts := strings.SplitN(mapping, ":", 2)
	return strings.TrimSpace(parts[0])
}

// copyMap returns a shallow copy of a map[string]interface{}.
func copyMap(src map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// toInterfaceSlice converts []string to []interface{} for YAML marshalling
// compatibility with Docker Compose list fields.
func toInterfaceSlice(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
