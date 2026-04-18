// Package transformer converts a parsed DSOConfig into a valid Docker Compose
// document that is ready to be used with `docker compose up`.
//
// Transformation rules applied:
//   - Application services: ports are removed; expose is added instead so
//     services can still talk to each other internally. The dso_mesh network
//     and a dso.service label are injected.
//   - For every proxied service a synthetic dso-proxy-<name> service is added.
//     It owns the host port bindings and forwards traffic to the backing service
//     via the shared dso_mesh network.
//   - No container_name is ever set (constraint from the architecture).
//   - A top-level networks block defining dso_mesh is always emitted.
package transformer

import (
	"fmt"
	"strings"

	"github.com/docker-secret-operator/dso/internal/models"
	"gopkg.in/yaml.v3"
)

const (
	// meshNetwork is the shared overlay network that connects all DSO-managed
	// services and their proxy counterparts.
	meshNetwork = "dso_mesh"

	// proxyImage is the lightweight TCP proxy image used by generated proxy
	// services. This is intentionally swappable — a future phase will allow
	// users to configure a custom proxy image.
	proxyImage = "alpine/socat:latest"
)

// composeOutput is the full docker-compose document structure that will be
// serialised to YAML by Transform.
type composeOutput struct {
	Version  string                            `yaml:"version"`
	Services map[string]map[string]interface{} `yaml:"services"`
	Networks map[string]interface{}            `yaml:"networks"`
}

// Transform takes a validated *models.DSOConfig and returns the bytes of a
// ready-to-use docker-compose.generated.yml file.
//
// It does NOT write anything to disk. The caller (cmd/dso or a CLI command)
// is responsible for persisting the output.
func Transform(cfg *models.DSOConfig) ([]byte, error) {
	out := composeOutput{
		Version:  cfg.Version,
		Services: make(map[string]map[string]interface{}),
		Networks: map[string]interface{}{
			meshNetwork: map[string]interface{}{
				"driver": "bridge",
			},
		},
	}

	// Build a lookup of proxied service names → their port mappings so we can
	// strip those ports from the backing service definitions.
	proxiedPorts := buildProxiedPortIndex(cfg.DSO.Containers)

	// --- Pass 1: transform backing services ---
	for name, rawSvc := range cfg.Services {
		svcMap, err := toStringMap(rawSvc)
		if err != nil {
			return nil, fmt.Errorf("transformer: service %q is not a valid map: %w", name, err)
		}

		transformed, err := transformBackingService(name, svcMap, proxiedPorts[name])
		if err != nil {
			return nil, fmt.Errorf("transformer: cannot transform service %q: %w", name, err)
		}

		out.Services[name] = transformed
	}

	// --- Pass 2: generate proxy services ---
	for _, target := range cfg.DSO.Containers {
		proxyName := "dso-proxy-" + target.Name
		proxySvc, err := buildProxyService(target)
		if err != nil {
			return nil, fmt.Errorf("transformer: cannot build proxy for %q: %w", target.Name, err)
		}
		out.Services[proxyName] = proxySvc
	}

	return yaml.Marshal(out)
}

// transformBackingService applies the DSO rules to a single application service:
//   - Removes container_name (not allowed in this architecture).
//   - Moves declared ports to the expose list.
//   - Adds the dso_mesh network.
//   - Injects dso.service and dso.managed labels.
func transformBackingService(
	name string,
	svc map[string]interface{},
	_ []string, // proxied port list reserved for future use
) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(svc)+4)

	for k, v := range svc {
		switch k {
		case "container_name":
			// Explicitly dropped — proxy architecture requires Docker to assign names.
			continue
		case "ports":
			// Ports on the host are owned by the proxy. Convert them to expose entries
			// so that intra-mesh communication still works without host bindings.
			expose := portsToExpose(v)
			if len(expose) > 0 {
				out["expose"] = expose
			}
		default:
			out[k] = v
		}
	}

	// Inject dso_mesh network.
	out["networks"] = mergeNetworks(out["networks"], meshNetwork)

	// Inject DSO labels.
	out["labels"] = mergeLabels(out["labels"], map[string]string{
		"dso.service": name,
		"dso.managed": "true",
	})

	return out, nil
}

// buildProxyService generates a synthetic dso-proxy-<name> service that owns
// the external port bindings for a proxied backing service.
//
// For now the proxy is a simple socat container that forwards TCP traffic.
// Phase 2 will replace this with a proper nginx/Envoy/HAProxy configuration.
func buildProxyService(target models.ProxyTarget) (map[string]interface{}, error) {
	// Build the socat command arguments: one socat invocation per port pair.
	// Each entry becomes a separate `command` argument in the generated YAML.
	if len(target.Ports) == 0 {
		return nil, fmt.Errorf("proxy target %q has no port mappings", target.Name)
	}

	// For multi-port proxies we'll use the first port for the primary socat
	// command and note that multi-port support will evolve in Phase 2 with a
	// proper reverse-proxy image.
	firstMapping := strings.TrimSpace(target.Ports[0])
	parts := strings.SplitN(firstMapping, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("proxy target %q: malformed port mapping %q", target.Name, firstMapping)
	}

	hostPort := strings.TrimSpace(parts[0])
	containerPort := strings.TrimSpace(parts[1])

	// socat command: listen on hostPort, forward to backing service's containerPort.
	socatCmd := fmt.Sprintf(
		"TCP-LISTEN:%s,fork,reuseaddr TCP:%s:%s",
		hostPort, target.Name, containerPort,
	)

	proxySvc := map[string]interface{}{
		"image":   proxyImage,
		"command": socatCmd,
		"ports":   target.Ports, // proxy owns ALL host port bindings
		"networks": []string{meshNetwork},
		"labels": map[string]string{
			"dso.proxy":   "true",
			"dso.service": target.Name,
			"dso.managed": "true",
		},
		"depends_on": []string{target.Name},
	}

	return proxySvc, nil
}

// buildProxiedPortIndex constructs a map of service name → list of host ports
// that are declared in the dso-proxy stanza. This is used to ensure those
// ports are stripped from the backing service's own port list.
func buildProxiedPortIndex(targets []models.ProxyTarget) map[string][]string {
	idx := make(map[string][]string, len(targets))
	for _, t := range targets {
		idx[t.Name] = append(idx[t.Name], t.Ports...)
	}
	return idx
}

// portsToExpose converts a raw `ports` field value (which can be a []interface{}
// or []string in Go after YAML unmarshalling) into a deduplicated list of
// plain container port strings suitable for the `expose` key.
func portsToExpose(raw interface{}) []string {
	if raw == nil {
		return nil
	}

	var expose []string

	switch v := raw.(type) {
	case []interface{}:
		for _, p := range v {
			s := fmt.Sprintf("%v", p)
			// Strip any host-side prefix (e.g. "8080:3306" → "3306").
			if idx := strings.LastIndex(s, ":"); idx >= 0 {
				s = s[idx+1:]
			}
			s = strings.TrimSpace(s)
			if s != "" {
				expose = append(expose, s)
			}
		}
	case []string:
		for _, p := range v {
			if idx := strings.LastIndex(p, ":"); idx >= 0 {
				p = p[idx+1:]
			}
			p = strings.TrimSpace(p)
			if p != "" {
				expose = append(expose, p)
			}
		}
	}

	return expose
}

// mergeNetworks merges an existing `networks` field value with a new network
// name, returning a deduplicated []string.
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
	case map[interface{}]interface{}, map[string]interface{}:
		// Named networks in map form — preserve the key names only.
		if m, ok := v.(map[string]interface{}); ok {
			for k := range m {
				if !seen[k] {
					seen[k] = true
					result = append(result, k)
				}
			}
		}
	}

	return result
}

// mergeLabels merges an existing `labels` field value (list or map) with
// additional label entries, returning a consolidated map[string]string.
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
	case []interface{}:
		// Label list format: "KEY=VALUE"
		for _, item := range v {
			parts := strings.SplitN(fmt.Sprintf("%v", item), "=", 2)
			if len(parts) == 2 {
				out[parts[0]] = parts[1]
			}
		}
	}

	for k, val := range add {
		out[k] = val
	}

	return out
}

// toStringMap coerces an interface{} (as returned by the YAML decoder into a
// generic map) into a map[string]interface{}, returning an error if it cannot.
func toStringMap(v interface{}) (map[string]interface{}, error) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, nil
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprintf("%v", k)] = val
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected map, got %T", v)
	}
}
