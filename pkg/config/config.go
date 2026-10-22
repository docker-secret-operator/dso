package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type RestartStrategy struct {
	Type        string `yaml:"type"`
	GracePeriod string `yaml:"grace_period"`
}

type WatchConfig struct {
	Mode            string        `yaml:"mode"` // polling, event, hybrid
	PollingInterval string        `yaml:"polling_interval"`
	Webhook         WebhookConfig `yaml:"webhook"`
}

// Struct-tag vocabulary used by RedactYAML (see redact.go) to keep credentials
// out of `dso config show` and any other human-readable dump of the file:
//
//	dso:"secret"        every scalar under this field is a credential
//	dso:"url"           URL whose userinfo/path/query may carry a credential
//	dso:"free-form"     free-form map (provider settings, legacy config): every
//	                    value is redacted EXCEPT those under a short allowlist
//	                    of known non-sensitive keys (region, namespace, ...);
//	                    URL values keep only scheme://host. Fail-closed: an
//	                    unrecognised key is treated as sensitive.
//
// Any new field that holds a credential MUST carry one of these tags;
// TestSchemaCredentialFieldsAreTagged fails if a credential-looking field
// is added without one.
type WebhookConfig struct {
	Enabled bool `yaml:"enabled"`
	// AuthToken is the LEGACY shared bearer credential
	// ("Authorization: Bearer <auth_token>"). It has no body integrity and no
	// replay protection and is deprecated in favor of HMACSecret. It is still
	// honored when it is the ONLY webhook credential. When HMACSecret is also
	// set it is IGNORED unless AllowLegacyAuth is true, so configuring HMAC
	// cannot leave the weaker credential silently bypassing timestamp and
	// replay protection.
	AuthToken string `yaml:"auth_token" dso:"secret"`
	Endpoint  string `yaml:"endpoint,omitempty"`
	// HMACSecret is the shared key used to verify HMAC-SHA256 request
	// signatures (X-DSO-Timestamp / X-DSO-Signature). Minimum 16 bytes. It is
	// independent of DSO_AUTH_TOKEN and of AuthToken.
	HMACSecret string `yaml:"hmac_secret,omitempty" dso:"secret"`
	// TimestampTolerance is the maximum clock difference accepted between the
	// signed timestamp and the server clock, as a Go duration (default "5m",
	// maximum "1h"). It also bounds how long replay-protection state is kept.
	TimestampTolerance string `yaml:"timestamp_tolerance,omitempty"`
	// DisableLegacyAuth rejects the deprecated bearer-token form so only
	// signed requests are accepted.
	DisableLegacyAuth bool `yaml:"disable_legacy_auth,omitempty"`
	// AllowLegacyAuth is the explicit compatibility opt-in for accepting the
	// legacy bearer token while HMACSecret is configured (a migration window
	// for senders that cannot sign yet). It has no effect without HMACSecret
	// (legacy is already accepted then) and is overridden by
	// DisableLegacyAuth. While active, an unsigned request authenticated by
	// the bearer token has NO timestamp, body-integrity or replay protection.
	AllowLegacyAuth bool `yaml:"allow_legacy_auth,omitempty"`
}

// NotificationsConfig configures outbound rotation-event notifications
// (internal/notify). Distinct from WatchConfig.Webhook above, which is an
// INBOUND trigger endpoint. Disabled by default: an existing deployment
// must never start making outbound HTTP requests merely because a new DSO
// version was installed.
type NotificationsConfig struct {
	Enabled  bool                  `yaml:"enabled"`
	Webhooks []NotificationWebhook `yaml:"webhooks,omitempty"`
	// AlertCooldown bounds how often a repeated identical alert (same
	// dedup key: rotation_failed:<secret>, injection_failed:<container>,
	// degraded:<container>) may send another notification, e.g. "5m".
	// Empty/invalid -> internal/alert's 5-minute default. Does not affect
	// resolution notifications (service_recovered, rotation_succeeded),
	// which always fire exactly once per firing->resolved transition.
	AlertCooldown string `yaml:"alert_cooldown,omitempty"`
}

// NotificationWebhook is one outbound notification destination.
type NotificationWebhook struct {
	URL string `yaml:"url" dso:"url"`
	// Timeout per delivery attempt, e.g. "10s". Default 10s.
	Timeout string `yaml:"timeout,omitempty"`
	// MaxRetries for transient failures (network errors, HTTP 5xx).
	// Default 2; set -1 for no retries. HTTP 4xx is never retried.
	MaxRetries int `yaml:"max_retries,omitempty"`
	// Events filters which event types this destination receives
	// (rotation_succeeded, rotation_failed, recovery_succeeded,
	// recovery_failed). Empty = all.
	Events []string `yaml:"events,omitempty"`
	// AllowInsecureHTTP permits a plain-HTTP endpoint. Off by default.
	AllowInsecureHTTP bool `yaml:"allow_insecure_http,omitempty"`
}

// WebUIConfig configures the optional browser-facing dashboard. Disabled by
// default: an existing deployment must never start serving a web UI (or
// listening on a new address) merely because a new DSO version was
// installed. The web UI is session-authenticated separately from the
// DSO_AUTH_TOKEN mechanism that protects the underlying REST API (see
// internal/auth) -- both protections coexist.
type WebUIConfig struct {
	Enabled bool `yaml:"enabled"`
	// ListenAddress is the host:port the web UI is served on. If empty,
	// the web UI mounts onto the existing REST API listener (--api-addr)
	// instead of opening a second listener.
	ListenAddress string `yaml:"listen_address,omitempty"`
	// Username is the single operator identity allowed to log in.
	Username string `yaml:"username,omitempty"`
	// PasswordHash is a bcrypt hash of the operator's password. Never
	// store a plaintext password here.
	PasswordHash string `yaml:"password_hash,omitempty" dso:"secret"`
	// SessionIdleTimeout is how long an unused session stays valid, e.g.
	// "30m". Defaults to 30m if empty.
	SessionIdleTimeout string `yaml:"session_idle_timeout,omitempty"`
	// CookieSecure controls the Secure attribute on the WebUI session cookie.
	// "auto" (default/empty) infers it from the bind address (existing
	// behavior, preserved for backward compatibility). "always" forces
	// Secure=true -- use this when DSO is behind a TLS-terminating reverse
	// proxy (the browser talks HTTPS to the proxy even though DSO itself
	// speaks plain HTTP on a loopback or internal address). "never" forces
	// Secure=false -- local plain-HTTP development only.
	CookieSecure string `yaml:"cookie_secure,omitempty"`
	// SessionMaxLifetime is the absolute lifetime of a session regardless of
	// activity, as a Go duration (default "24h"). "0" disables the cap.
	SessionMaxLifetime string `yaml:"session_max_lifetime,omitempty"`
	// AllowedOrigins lists additional browser origins (e.g.
	// "https://dso.example.com") permitted to make cookie-authenticated
	// state-changing requests. Same-origin requests are always allowed. Needed
	// only when a reverse proxy rewrites the Host header. Their hosts are also
	// accepted by the Host check.
	AllowedOrigins []string `yaml:"allowed_origins,omitempty"`
	// AllowedHosts lists additional Host header values (hostnames, no port)
	// accepted by the REST API. Loopback names are always accepted. When the
	// API runs without any authentication (legacy loopback mode) a Host check
	// is always enforced to defeat DNS rebinding; list your reverse proxy's
	// public hostname here if you front that mode with one.
	AllowedHosts []string `yaml:"allowed_hosts,omitempty"`
	// TrustedProxies lists IPs/CIDRs of reverse proxies whose X-Forwarded-For
	// header is honored for per-client login throttling. Empty (default)
	// means forwarded headers are never trusted.
	TrustedProxies []string `yaml:"trusted_proxies,omitempty"`
}

type RotationConfigV2 struct {
	Enabled            bool   `yaml:"enabled"`
	Strategy           string `yaml:"strategy"` // restart, signal, none
	Signal             string `yaml:"signal,omitempty"`
	HealthCheckTimeout string `yaml:"health_check_timeout,omitempty"`
}

type AuthConfig struct {
	Method string            `yaml:"method"` // iam_role, access_key, token, env
	Params map[string]string `yaml:"params,omitempty" dso:"secret"`
}

type RetryConfig struct {
	Attempts int    `yaml:"attempts"`
	Backoff  string `yaml:"backoff"`
}

type ProviderConfig struct {
	Type   string            `yaml:"type"`
	Region string            `yaml:"region,omitempty"`
	Auth   AuthConfig        `yaml:"auth,omitempty"`
	Retry  RetryConfig       `yaml:"retry,omitempty"`
	Config map[string]string `yaml:"config,omitempty" dso:"free-form"`
}

type InjectionConfig struct {
	Type string `yaml:"type"` // env, file
	Path string `yaml:"path,omitempty"`
	UID  int    `yaml:"uid,omitempty"`
	GID  int    `yaml:"gid,omitempty"`
}

type TargetConfig struct {
	Containers []string          `yaml:"containers,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
}

type DefaultsConfig struct {
	Inject   InjectionConfig  `yaml:"inject,omitempty"`
	Rotation RotationConfigV2 `yaml:"rotation,omitempty"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`  // info, debug, error
	Format string `yaml:"format"` // json, text
}

type AgentConfig struct {
	Cache           bool             `yaml:"cache"`
	RefreshInterval string           `yaml:"refresh_interval"`
	AutoSync        bool             `yaml:"auto_sync"`
	RestartStrategy RestartStrategy  `yaml:"restart_strategy"`
	Watch           WatchConfig      `yaml:"watch"`
	Rotation        RotationConfigV2 `yaml:"rotation"`
	// OwnerID is this DSO agent instance's stable ownership identity
	// (SEC-P0 container-discovery isolation fix). It is written as the
	// dso.owner=<OwnerID> label on every container this agent creates or
	// manages, and required (in addition to dso.reloader=true / dso.secrets
	// matching) for this agent to register, rotate, or otherwise mutate a
	// container. It is generated once (see EnsureOwnerID) and persisted back
	// into this same config file so it survives agent restarts -- it is NOT
	// a secret and is safe to log, but treat it as an opaque identifier
	// rather than exposing it gratuitously (e.g. over the WebUI) since it
	// has no operator-facing meaning.
	OwnerID string `yaml:"owner_id,omitempty"`
}

// ProxyConfig configures the DSO proxy's host-port binding behavior.
type ProxyConfig struct {
	// AllowedHostPorts is an optional operator-supplied allow-list of host
	// ports the proxy may bind (SEC-7 full fix). Entries may be an exact
	// port ("8080") or an inclusive range ("3000-4000"). Any container can
	// set its own dso.host_ports label, so without this the proxy binds
	// whatever unprivileged port a container's label claims. Unset/empty
	// (the default) preserves prior behavior: any port >= 1024 is allowed.
	AllowedHostPorts []string `yaml:"allowed_host_ports,omitempty"`
}

type SecretMapping struct {
	Name     string            `yaml:"name"`
	Provider string            `yaml:"provider,omitempty"`
	Inject   InjectionConfig   `yaml:"inject"`
	Rotation RotationConfigV2  `yaml:"rotation"`
	Targets  TargetConfig      `yaml:"targets,omitempty"`
	Mappings map[string]string `yaml:"mappings"`
}

type Config struct {
	Providers     map[string]ProviderConfig `yaml:"providers"`
	Agent         AgentConfig               `yaml:"agent"`
	Defaults      DefaultsConfig            `yaml:"defaults,omitempty"`
	Logging       LoggingConfig             `yaml:"logging,omitempty"`
	Proxy         ProxyConfig               `yaml:"proxy,omitempty"`
	Notifications NotificationsConfig       `yaml:"notifications,omitempty"`
	WebUI         WebUIConfig               `yaml:"webui,omitempty"`
	Secrets       []SecretMapping           `yaml:"secrets"`

	// Legacy fields for backward compatibility detection
	LegacyProvider string            `yaml:"provider,omitempty"`
	LegacyConfig   map[string]string `yaml:"config,omitempty" dso:"free-form"`
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	// Internal type to avoid infinite recursion
	type alias Config
	var aux alias

	if err := value.Decode(&aux); err != nil {
		return err
	}

	*c = Config(aux)

	// Handle Legacy Top-Level Provider
	if c.LegacyProvider != "" {
		if c.Providers == nil {
			c.Providers = make(map[string]ProviderConfig)
		}
		if _, exists := c.Providers[c.LegacyProvider]; !exists {
			c.Providers[c.LegacyProvider] = ProviderConfig{
				Type:   c.LegacyProvider,
				Config: c.LegacyConfig,
			}
		}
	}

	// Handle Legacy Secret Mappings
	for range c.Secrets {
		// Detect if Inject was a simple string in raw YAML (v1)
		// Since we've already decoded into structured InjectionConfig,
		// we need to check if the 'type' is empty but was likely intended.
		// However, yaml.v3 handles this better if we use an intermediate map or custom unmarshaler for SecretMapping too.
	}

	return nil
}

// SecretMapping custom unmarshaler to handle legacy formats
func (s *SecretMapping) UnmarshalYAML(value *yaml.Node) error {
	// 1. Try to decode as the standard V2 structure first
	type v2Type SecretMapping
	var v2 v2Type
	if err := value.Decode(&v2); err != nil {
		// First-pass failure is expected for legacy (v1) format entries; fall through
		// to the raw-map path below which handles them. Log so operators can diagnose
		// accidental misconfiguration vs. intentional legacy format.
		fmt.Fprintf(os.Stderr, "[DSO] config: v2 decode of secret mapping failed (will attempt legacy fallback): %v\n", err)
	}
	*s = SecretMapping(v2)

	// 2. Handle legacy field disambiguation using a raw map representation
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return err
	}

	// Always ensure Name is populated from raw if it wasn't caught by V2 decode
	if name, ok := raw["name"].(string); ok && s.Name == "" {
		s.Name = name
	}

	// Always ensure Mappings are populated
	if mappings, ok := raw["mappings"].(map[string]interface{}); ok && len(s.Mappings) == 0 {
		s.Mappings = make(map[string]string)
		for k, v := range mappings {
			if strVal, ok := v.(string); ok {
				s.Mappings[k] = strVal
			}
		}
	}

	// Handle Legacy 'inject' as a string (v1 format: inject: env)
	if val, ok := raw["inject"].(string); ok {
		s.Inject.Type = val
	}

	// Handle Legacy 'rotation' as a bool (v1 format: rotation: true)
	if val, ok := raw["rotation"].(bool); ok {
		s.Rotation.Enabled = val
	}

	// Handle Legacy 'reload_strategy'
	if reloadRaw, ok := raw["reload_strategy"].(map[string]interface{}); ok {
		if reloadType, ok := reloadRaw["type"].(string); ok {
			s.Rotation.Strategy = reloadType
			s.Rotation.Enabled = true
		}
	}

	return nil
}

func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured")
	}

	// Validate providers
	validProviderTypes := map[string]bool{"vault": true, "aws": true, "azure": true, "huawei": true}
	for name, prov := range c.Providers {
		if prov.Type == "" {
			return fmt.Errorf("provider '%s' missing 'type'", name)
		}
		if !validProviderTypes[prov.Type] {
			return fmt.Errorf("provider '%s' has invalid type '%s'", name, prov.Type)
		}
		// Validate auth config if present
		if prov.Auth.Method != "" {
			validAuthMethods := map[string]bool{"iam_role": true, "access_key": true, "token": true, "env": true}
			if !validAuthMethods[prov.Auth.Method] {
				return fmt.Errorf("provider '%s' has invalid auth method '%s'", name, prov.Auth.Method)
			}
		}
	}

	// Validate secrets
	for i, sec := range c.Secrets {
		if sec.Name == "" {
			return fmt.Errorf("secret at index %d is missing 'name'", i)
		}

		// Merge Injection Defaults
		if sec.Inject.Type == "" {
			sec.Inject.Type = c.Defaults.Inject.Type
		}
		if sec.Inject.Type == "" {
			return fmt.Errorf("secret '%s' is missing injection type", sec.Name)
		}

		// Validate injection type
		validInjectTypes := map[string]bool{"env": true, "file": true}
		if !validInjectTypes[sec.Inject.Type] {
			return fmt.Errorf("secret '%s' has invalid injection type '%s'", sec.Name, sec.Inject.Type)
		}

		if sec.Inject.Path == "" {
			sec.Inject.Path = c.Defaults.Inject.Path
		}
		if sec.Inject.Type == "file" && sec.Inject.Path == "" {
			return fmt.Errorf("secret '%s' with type 'file' must have a 'path'", sec.Name)
		}

		// Validate file permissions
		if sec.Inject.Type == "file" && sec.Inject.UID == 0 && sec.Inject.GID == 0 {
			// If user/group not specified, apply safe defaults
			sec.Inject.UID = c.Defaults.Inject.UID
			sec.Inject.GID = c.Defaults.Inject.GID
		}
		// Ensure UID/GID is not root (0) for file injection
		if sec.Inject.Type == "file" && (sec.Inject.UID == 0 || sec.Inject.GID == 0) {
			return fmt.Errorf("secret '%s' file injection requires non-root UID/GID (got UID=%d, GID=%d)", sec.Name, sec.Inject.UID, sec.Inject.GID)
		}

		// Merge Rotation Defaults
		if !sec.Rotation.Enabled && c.Defaults.Rotation.Enabled {
			sec.Rotation.Enabled = true
		}
		if sec.Rotation.Strategy == "" {
			sec.Rotation.Strategy = c.Defaults.Rotation.Strategy
		}
		if sec.Rotation.Strategy == "" {
			sec.Rotation.Strategy = "restart" // Final fallback
		}

		// Validate rotation strategy
		validStrategies := map[string]bool{"restart": true, "signal": true, "auto": true, "rolling": true, "none": true}
		if !validStrategies[sec.Rotation.Strategy] {
			return fmt.Errorf("secret '%s' has invalid rotation strategy '%s'", sec.Name, sec.Rotation.Strategy)
		}

		// Validate provider reference
		providerName := sec.Provider
		if providerName == "" {
			if len(c.Providers) == 1 {
				for name := range c.Providers {
					providerName = name
				}
				sec.Provider = providerName
			} else {
				return fmt.Errorf("secret %q has no provider specified; set the 'provider' field (multiple providers are configured)", sec.Name)
			}
		}
		if _, exists := c.Providers[providerName]; !exists {
			return fmt.Errorf("secret '%s' references non-existent provider '%s'", sec.Name, providerName)
		}

		// Sync back
		c.Secrets[i] = sec
	}

	// Validate WebUI auth config: an enabled WebUI with no username or an
	// unparsable password hash would fail every login attempt at runtime
	// instead of at startup, so catch it here.
	if c.WebUI.Enabled {
		if c.WebUI.Username == "" {
			return fmt.Errorf("webui.enabled is true but webui.username is missing")
		}
		if c.WebUI.PasswordHash == "" {
			return fmt.Errorf("webui.enabled is true but webui.password_hash is missing")
		}
		if _, err := bcrypt.Cost([]byte(c.WebUI.PasswordHash)); err != nil {
			return fmt.Errorf("webui.password_hash is not a valid bcrypt hash: %w", err)
		}
	}

	// Validate Docker socket accessibility
	dockerSocket := os.Getenv("DOCKER_HOST")
	if dockerSocket == "" {
		dockerSocket = "/var/run/docker.sock"
	}
	// Only check file system paths, not network addresses
	if !strings.HasPrefix(dockerSocket, "unix://") && !strings.HasPrefix(dockerSocket, "tcp://") {
		// #nosec G703 -- dockerSocket comes from DOCKER_HOST, an operator-set
		// environment variable (standard Docker CLI/SDK convention), not
		// remote/attacker input; this is a read-only existence check (os.Stat),
		// not file content access
		if _, err := os.Stat(dockerSocket); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("docker socket not accessible at %s: file does not exist", dockerSocket)
			}
			return fmt.Errorf("docker socket validation failed at %s: %w", dockerSocket, err)
		}
	}

	return nil
}

// generateOwnerID returns a new cryptographically random ownership identity
// for the dso.owner container label (SEC-P0 discovery-isolation fix). 32
// bytes of crypto/rand hex-encoded (256 bits) -- unguessable, and not
// derived from PID, socket path, container ID, or Docker network, all of
// which are either reused across independent agents or predictable/spoofable
// by anything else on the same host.
func generateOwnerID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate owner id: %w", err)
	}
	return "dso-" + hex.EncodeToString(buf), nil
}

// EnsureOwnerID guarantees cfg.Agent.OwnerID is set, generating and
// persisting one into cfgFile if it is missing.
//
// Identity scope (see the "copied config" analysis in the P0 report):
// ownership here is CONFIG identity, not host identity or install identity.
// The owner ID lives inside dso.yaml itself and travels with that file. This
// is a deliberate choice: DSO's unit of "an agent's mandate" is already the
// config file (which secrets, which providers, which targets it manages) --
// tying ownership to the same artifact keeps a single mental model instead
// of inventing a second identity store (e.g. a dotfile beside the config,
// which would silently detach from the config on copy/rename and could
// itself be forgotten during backup/restore).
//
// Consequence an operator must know: copying dso.yaml from environment A to
// environment B copies the owner ID too. If both copies then run
// simultaneously against overlapping containers on the same host (or
// visible over the same Docker socket), they are, by this design,
// INDISTINGUISHABLE -- they will co-manage the same containers, which is the
// correct behavior for an intentional HA/failover pair sharing one config,
// but is a footgun for "I copied the config as a starting point for an
// unrelated environment." Mitigation shipped now: EnsureOwnerID never
// regenerates an ID that is already present, so an operator who wants
// distinct identity after copying a config must explicitly clear the
// owner_id field (or a future `dso rotate-owner` command, documented as a
// follow-up) to get a fresh one. This is called out explicitly in the
// report's Remaining Limitations section rather than silently assumed away.
//
// Persistence failure modes are handled fail-closed: if cfgFile cannot be
// safely rewritten (read-only, unwritable directory, path outside what
// IsSafePath allows), this returns an error instead of proceeding with an
// in-memory-only, unpersisted ID -- an unpersisted ID would silently change
// on every restart, which would make the agent lose track of (default-deny)
// every container it created in the prior run, a worse outcome than
// refusing to start.
func EnsureOwnerID(cfg *Config, cfgFile string) error {
	if cfg.Agent.OwnerID != "" {
		return nil
	}

	id, err := generateOwnerID()
	if err != nil {
		return err
	}

	if cfgFile == "" {
		cfgFile = "dso.yaml"
	}
	safePath, err := IsSafePath("", cfgFile)
	if err != nil {
		return fmt.Errorf("cannot persist generated owner id: invalid config path: %w", err)
	}

	info, statErr := os.Stat(safePath)
	if statErr != nil {
		return fmt.Errorf("cannot persist generated owner id: config file %s not accessible: %w", cfgFile, statErr)
	}
	if info.Mode().Perm()&0200 == 0 {
		return fmt.Errorf("cannot persist generated owner id: config file %s is not writable by this process; "+
			"set agent.owner_id manually in the config to a unique value and retry, or make the file writable", cfgFile)
	}

	raw, err := os.ReadFile(safePath) // #nosec G304 -- safePath constrained by IsSafePath, file already loaded once by the caller.
	if err != nil {
		return fmt.Errorf("cannot persist generated owner id: failed to re-read config file %s: %w", cfgFile, err)
	}

	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return fmt.Errorf("cannot persist generated owner id: failed to parse config file %s for update: %w", cfgFile, err)
	}
	if err := setOwnerIDNode(&node, id); err != nil {
		return fmt.Errorf("cannot persist generated owner id: %w", err)
	}

	out, err := yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("cannot persist generated owner id: failed to re-marshal config: %w", err)
	}

	// Write via a temp file + rename in the same directory so a crash or a
	// concurrent reader never observes a partially-written config file --
	// several processes may share this same dso.yaml path (e.g. CLI
	// commands run alongside a long-running agent).
	dir := filepath.Dir(safePath)
	tmp, err := os.CreateTemp(dir, ".dso-owner-id-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot persist generated owner id: failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot persist generated owner id: failed to write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot persist generated owner id: failed to close temp file: %w", err)
	}
	// Tighten-only (M1 config-permission policy, same bound as
	// PermissionAdvisory / DSO-DOCTOR-009): the rewritten file keeps the
	// owner/group read bits it already had but never carries group-write,
	// world access or execute bits. A 0600 file stays 0600; 0664 becomes 0640.
	if err := os.Chmod(tmpPath, info.Mode().Perm()&0o640); err != nil {
		return fmt.Errorf("cannot persist generated owner id: failed to set permissions on temp file: %w", err)
	}
	if err := os.Rename(tmpPath, safePath); err != nil {
		return fmt.Errorf("cannot persist generated owner id: failed to replace config file %s: %w", cfgFile, err)
	}

	cfg.Agent.OwnerID = id
	return nil
}

// setOwnerIDNode inserts/updates agent.owner_id in a parsed yaml.Node tree,
// preserving every other field, comment, and ordering in the file -- a
// naive re-marshal of the decoded Config struct would silently drop
// comments and reorder keys throughout the operator's dso.yaml.
func setOwnerIDNode(doc *yaml.Node, id string) error {
	if len(doc.Content) == 0 {
		return fmt.Errorf("empty or invalid config document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("config document root is not a mapping")
	}

	var agentNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "agent" {
			agentNode = root.Content[i+1]
			break
		}
	}
	if agentNode == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "agent"}
		agentNode = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, keyNode, agentNode)
	}
	if agentNode.Kind != yaml.MappingNode {
		return fmt.Errorf("config 'agent' section is not a mapping")
	}

	for i := 0; i+1 < len(agentNode.Content); i += 2 {
		if agentNode.Content[i].Value == "owner_id" {
			agentNode.Content[i+1].Value = id
			agentNode.Content[i+1].Tag = "!!str"
			return nil
		}
	}
	agentNode.Content = append(agentNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "owner_id"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: id},
	)
	return nil
}

// IsSafePath validates that a user-provided path does not escape the base directory.
func IsSafePath(baseDir, userPath string) (string, error) {
	clean := filepath.Clean(userPath)

	// List of allowed absolute system directories for DSO
	allowedSystemDirs := []string{
		"/etc/dso/",
		"/usr/local/lib/dso/",
		"/usr/local/bin/",
		"/var/run/dso/",
		"/run/dso/",
	}

	isSystemPath := false
	for _, dir := range allowedSystemDirs {
		if pathWithinDir(clean, dir) {
			isSystemPath = true
			break
		}
	}

	// Reject absolute paths in "anywhere" mode unless they are in allowed system directories.
	if filepath.IsAbs(clean) && !isSystemPath && baseDir == "" {
		return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
	}

	// If no baseDir is provided, at least reject ".." for security
	if baseDir == "" {
		if !filepath.IsLocal(clean) && !isSystemPath {
			return "", fmt.Errorf("path traversal attempt: %s", userPath)
		}
		return clean, nil
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	var fullPath string
	if filepath.IsAbs(clean) {
		fullPath = clean
	} else {
		fullPath = filepath.Join(baseAbs, clean)
	}
	fullAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Ensure no directory escape
	rel, err := filepath.Rel(baseAbs, fullAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal detected: %s", userPath)
	}

	// [SEC-4] The checks above are purely lexical (Clean/Abs/Rel): they
	// prevent literal ".." traversal but do not detect a symlink that
	// resolves outside baseDir. baseDir represents a genuine trust
	// boundary here (e.g. FileProvider's configured secrets directory) --
	// unlike the baseDir=="" callers above, which are all CLI-invoked by an
	// already-privileged operator and don't cross a privilege boundary by
	// following a symlink of their own choosing.
	if err := rejectEscapingSymlink(baseAbs, fullAbs); err != nil {
		return "", err
	}

	return fullAbs, nil
}

// rejectEscapingSymlink defends against two symlink-based ways fullAbs could
// resolve outside baseAbs despite passing the lexical containment check
// above [SEC-4]:
//  1. An intermediate directory component is a symlink pointing outside
//     baseAbs. Resolved via the parent directory rather than the full path,
//     since the full path's leaf component may not exist yet (e.g. an
//     export command creating a new output file) -- filepath.EvalSymlinks
//     requires every component to exist, so resolving just the parent
//     tolerates a not-yet-created leaf while still catching a malicious
//     intermediate symlink.
//  2. The leaf itself already exists and is a symlink (e.g. planted inside
//     an otherwise-legitimate secrets directory to redirect a read or write
//     to an unintended file). Detected via Lstat, matching the same
//     symlink-rejection convention pkg/provider/load.go's validatePluginPath
//     already uses for plugin binaries. A nonexistent leaf (Lstat failing
//     with "not exist") is not an error -- creating a new file is legitimate.
func rejectEscapingSymlink(baseAbs, fullAbs string) error {
	parent := filepath.Dir(fullAbs)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		resolvedFull := filepath.Join(resolvedParent, filepath.Base(fullAbs))
		rel, err := filepath.Rel(baseAbs, resolvedFull)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path traversal detected: resolved path escapes base directory via a symlinked parent directory")
		}
	}
	// If the parent doesn't exist (or EvalSymlinks otherwise fails), skip
	// this check silently -- the subsequent file operation (open/create)
	// will fail on its own with a clear "no such file or directory" error.

	if info, err := os.Lstat(fullAbs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is a symlink, refusing to follow: %s", fullAbs)
	}

	return nil
}

func pathWithinDir(path, dir string) bool {
	pathAbs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	dirAbs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dirAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func LoadConfig(cfgFile string) (*Config, error) {
	if cfgFile == "" {
		cfgFile = "dso.yaml"
	}

	// Standard validation for config file path
	safePath, err := IsSafePath("", cfgFile)
	if err != nil {
		return nil, fmt.Errorf("invalid config path: %w", err)
	}

	data, err := os.ReadFile(safePath) // #nosec G304 -- safePath is constrained by IsSafePath.
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", cfgFile, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// SEC-P0: every agent needs a stable dso.owner identity for container
	// discovery isolation (see EnsureOwnerID doc comment). Migrate an
	// existing config that predates this fix by generating and persisting
	// one now; fail closed rather than run with an unpersisted (and
	// therefore restart-unstable) identity.
	if err := EnsureOwnerID(&cfg, cfgFile); err != nil {
		return nil, fmt.Errorf("failed to ensure agent owner id: %w", err)
	}

	return &cfg, nil
}

// LoadConfigWithDecryption loads config and decrypts credentials using provided master key
// masterKey should be 32 bytes (for AES-256). If empty, credentials are not decrypted.
func LoadConfigWithDecryption(cfgFile string, masterKey []byte) (*Config, error) {
	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		return nil, err
	}

	// Only decrypt if master key is provided
	if len(masterKey) == 0 {
		return cfg, nil
	}

	cm, err := NewCryptoManager(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create crypto manager: %w", err)
	}

	if err := cm.DecryptProviderConfig(cfg); err != nil {
		return nil, fmt.Errorf("failed to decrypt config: %w", err)
	}

	return cfg, nil
}
