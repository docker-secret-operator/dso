// Package models defines the domain types for the DSO proxy architecture.
// It covers both the user-facing dso-compose.yml structure and the internal
// representations used during transformation.
package models

// PortMapping describes a single host:container port binding owned by the proxy.
// Example: "8080:3306" means the proxy listens on host port 8080 and forwards
// to the backing service's container port 3306.
type PortMapping struct {
	// Raw is the original string token from the YAML, e.g. "8080:3306" or "3000".
	Raw string `yaml:"raw,omitempty"`

	// Host is the port exposed on the Docker host (owned by the proxy).
	Host string

	// Container is the port the application container listens on internally.
	Container string
}

// ProxyTarget defines one service that the proxy section manages.
// Each entry maps a backing service name to the public port bindings the proxy
// should own on its behalf.
type ProxyTarget struct {
	// Name is the service key as it appears in the services block.
	Name string `yaml:"name"`

	// Ports is the list of host→container port mappings the proxy will own.
	Ports []string `yaml:"ports"`
}

// ProxyConfig represents the top-level `dso-proxy` stanza in dso-compose.yml.
// It declares which services participate in the proxy layer and what ports they
// expose to the outside world.
type ProxyConfig struct {
	// Containers holds one entry per proxied backing service.
	Containers []ProxyTarget `yaml:"containers"`
}

// Service mirrors a service entry in the dso-compose.yml services block.
// Fields map directly to their Docker Compose equivalents so that non-proxy
// services can be passed through the transformer without information loss.
type Service struct {
	// Image is the Docker image reference, e.g. "mysql:latest".
	Image string `yaml:"image,omitempty"`

	// Build is a path or build-context map used when no pre-built image exists.
	Build interface{} `yaml:"build,omitempty"`

	// Ports lists internal container ports (no host binding; host ports are
	// owned exclusively by the proxy). Format: "3306" or "3306/tcp".
	Ports []string `yaml:"ports,omitempty"`

	// Environment holds env vars as a list ("KEY=VALUE") or map.
	Environment interface{} `yaml:"environment,omitempty"`

	// Volumes lists volume mounts for the service.
	Volumes []string `yaml:"volumes,omitempty"`

	// DependsOn expresses startup ordering between services.
	DependsOn []string `yaml:"depends_on,omitempty"`

	// Restart is the restart policy, e.g. "unless-stopped".
	Restart string `yaml:"restart,omitempty"`

	// Labels are additional metadata labels to attach to the container.
	Labels map[string]string `yaml:"labels,omitempty"`

	// Networks explicitly lists networks this service should join.
	// The transformer will always inject "dso_mesh" in addition to any declared here.
	Networks []string `yaml:"networks,omitempty"`

	// Expose lists ports to expose to other services (not to the host).
	Expose []string `yaml:"expose,omitempty"`

	// HealthCheck defines the Docker HEALTHCHECK for this service.
	HealthCheck interface{} `yaml:"healthcheck,omitempty"`

	// Extra captures any unknown top-level keys so that un-modelled fields
	// are preserved transparently during YAML round-trips.
	Extra map[string]interface{} `yaml:",inline"`
}

// DSOConfig is the root struct for parsing a dso-compose.yml file.
// It separates the proxy declaration from the regular service definitions.
type DSOConfig struct {
	// Version is the Compose file format version, e.g. "3.9".
	Version string `yaml:"version"`

	// DSO is the optional `dso-proxy` stanza. When present it defines which
	// services are proxied and what port bindings the proxy owns.
	DSO ProxyConfig `yaml:"services>dso-proxy,omitempty"`

	// Services maps service names to their definitions.
	// The `dso-proxy` key is extracted before this map is used by the transformer.
	Services map[string]interface{} `yaml:"services"`
}
