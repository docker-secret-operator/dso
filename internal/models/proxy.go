// Package models defines the domain types for the DSO proxy architecture.
//
// V2 replaces the hand-rolled dso-proxy DSL with a standard-compose-first
// model: every service is analysed individually, and eligibility for proxy
// injection is determined by auto-detection rules plus an optional x-dso
// extension field that the user may add to any service.
package models

// DSOOptions represents the optional `x-dso` extension field on a Compose service.
// All fields are optional; absent values fall back to auto-detection rules.
//
// Example dso-compose.yml snippet:
//
//	services:
//	  app:
//	    image: myapp
//	    ports:
//	      - "3000:3000"
//	    x-dso:
//	      enabled: true
//	      strategy: rolling
type DSOOptions struct {
	// Enabled explicitly opts a service in (true) or out (false) of DSO proxy
	// injection. When nil the parser applies auto-detection: services with ports
	// that are not known database images are eligible.
	Enabled *bool `yaml:"enabled,omitempty"`

	// Strategy names the deployment strategy to use when rotating this service.
	// Supported values (Phase 2+): "rolling", "canary". Defaults to "rolling".
	Strategy string `yaml:"strategy,omitempty"`
}

// ParsedService is the result of fully analysing one Compose service definition.
// The parser populates this struct; the transformer reads it.
type ParsedService struct {
	// Name is the service key from the Compose file, e.g. "api" or "mysql-db".
	Name string

	// RawFields holds every field from the original service definition except
	// `ports`, `x-dso`, and `container_name`, which are either managed by DSO
	// or explicitly disallowed. These fields are passed through verbatim.
	RawFields map[string]interface{}

	// Ports is the list of port-mapping strings declared on the service,
	// e.g. ["3000:3000", "5000:5000"]. Populated even for ineligible services
	// so the transformer can restore them for pass-through services.
	Ports []string

	// DSO holds the parsed x-dso options for this service. Zero-value when no
	// x-dso block was present (fields are safe to read without nil-checking).
	DSO DSOOptions

	// IsEligible is true when the transformer should inject a proxy service and
	// move this service's ports to the proxy. False means pass-through.
	IsEligible bool
}

// DSOConfig is the root result produced by the parser. It is the single
// input type consumed by the transformer.
type DSOConfig struct {
	// Version is the Compose file format version string, e.g. "3.9".
	Version string

	// Services is the ordered list of parsed service definitions.
	// Ordering is alphabetical by service name for deterministic output.
	Services []ParsedService

	// RawNetworks is the top-level `networks:` block from the original Compose
	// file, passed through verbatim and merged with the dso_mesh network.
	RawNetworks interface{}

	// RawVolumes is the top-level `volumes:` block from the original Compose
	// file, passed through verbatim.
	RawVolumes interface{}

	// DeprecatedProxy is populated only when a legacy dso-proxy block was found
	// in the services map. Non-nil value signals the transformer to emit a
	// deprecation warning. Support will be removed in a future major version.
	DeprecatedProxy *LegacyProxyConfig
}

// LegacyProxyConfig holds the content of the former dso-proxy service stanza.
// It is used only for backward compatibility.
//
// Deprecated: add x-dso extension fields directly to each service, or rely on
// auto-detection. The dso-proxy block will be removed in a future release.
type LegacyProxyConfig struct {
	Containers []ProxyTarget
}

// ProxyTarget is a single entry in the legacy dso-proxy.containers list.
//
// Deprecated: see LegacyProxyConfig.
type ProxyTarget struct {
	// Name is the backing service key this proxy target references.
	Name string `yaml:"name"`

	// Ports is the list of host:container port mappings this target owns.
	Ports []string `yaml:"ports"`
}

// PortMapping is a parsed host:container port pair.
// Used internally by helpers that need to inspect individual port components.
type PortMapping struct {
	// Raw is the original mapping string, e.g. "8080:3306".
	Raw string

	// Host is the port exposed on the Docker host.
	Host string

	// Container is the port the application container listens on.
	Container string
}
