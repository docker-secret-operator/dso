package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"gopkg.in/yaml.v3"
)

var debugMode bool

func SetDebug(b bool) {
	debugMode = b
}

type ComposeFile struct {
	Version  string                 `yaml:"version,omitempty"`
	Services map[string]interface{} `yaml:"services,omitempty"`
	Secrets  map[string]interface{} `yaml:"secrets,omitempty"`
	Other    map[string]interface{} `yaml:",inline"`
}

// serviceConsumesSecret returns true if the compose service declares at least one
// environment variable that maps to a DSO secret (by env name or dso:// prefix).
func serviceConsumesSecret(svc map[string]interface{}, sec config.SecretMapping) bool {
	envNames := make(map[string]bool, len(sec.Mappings))
	for _, envName := range sec.Mappings {
		envNames[envName] = true
	}
	switch env := svc["environment"].(type) {
	case map[string]interface{}:
		for k, v := range env {
			if envNames[k] {
				return true
			}
			if s, ok := v.(string); ok && (strings.HasPrefix(s, "dso://") || strings.HasPrefix(s, "dsofile://")) {
				return true
			}
		}
	case []interface{}:
		for _, item := range env {
			parts := strings.SplitN(fmt.Sprintf("%v", item), "=", 2)
			if envNames[parts[0]] {
				return true
			}
			if len(parts) == 2 && (strings.HasPrefix(parts[1], "dso://") || strings.HasPrefix(parts[1], "dsofile://")) {
				return true
			}
		}
	}
	return false
}

// serviceIsTarget returns true if the compose service should be managed by DSO for the
// given secret. Explicit container names beat label matching, which beats auto-detection.
// File-mode secrets fall back to broad targeting (inject.type=file cannot be auto-detected
// from env keys alone).
func serviceIsTarget(name string, svc map[string]interface{}, sec config.SecretMapping) bool {
	if len(sec.Targets.Containers) > 0 {
		for _, t := range sec.Targets.Containers {
			if t == name {
				return true
			}
		}
		return false
	}
	if len(sec.Targets.Labels) > 0 {
		svcLabels := make(map[string]string)
		if labelsRaw, ok := svc["labels"].(map[string]interface{}); ok {
			for k, v := range labelsRaw {
				svcLabels[k] = fmt.Sprintf("%v", v)
			}
		}
		for k, v := range sec.Targets.Labels {
			if svcLabels[k] != v {
				return false
			}
		}
		return true
	}
	// File-mode: can't auto-detect from env keys — preserve broad behavior
	if sec.Inject.Type == "file" {
		return true
	}
	return serviceConsumesSecret(svc, sec)
}

// RunComposeUpWithEnv parses the compose file, fetches DSO custom secrets for env overrides, merges them with dso.yaml configurations, and dynamically runs docker compose up via stdin.
// If preInjected is non-nil, those secrets are used directly instead of connecting back to the agent (used during rotation to avoid self-call deadlock).
func RunComposeUpWithEnv(filename string, extraArgs []string, configPath string, dryRun bool, preInjected ...map[string]string) error {
	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Resolve config path if empty
	targetConfig := configPath
	if targetConfig == "" {
		if _, err := os.Stat("dso.yaml"); err == nil {
			targetConfig = "dso.yaml"
		} else if _, err := os.Stat("/etc/dso/dso.yaml"); err == nil {
			targetConfig = "/etc/dso/dso.yaml"
		}
	}

	// Always load config (needed for label injection below, regardless of how secrets are fetched)
	cfg, err := config.LoadConfigWithOwnerID(targetConfig)
	if err != nil {
		return fmt.Errorf("failed to load DSO config %s: %w", targetConfig, err)
	}

	var injectedSecrets map[string]string

	// If the caller pre-injected secrets (e.g. during rotation), use those directly.
	// This avoids a self-call deadlock where the agent tries to talk to itself.
	if len(preInjected) > 0 && preInjected[0] != nil {
		injectedSecrets = preInjected[0]
		for k, v := range injectedSecrets {
			envMap[k] = v
		}
	} else if cfg != nil {
		socketPath := "/run/dso/dso.sock"
		if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
			socketPath = custom
		}
		client, err := injector.NewAgentClient(socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Agent connection failed (%v). Proceeding without dynamic env injection.\n", err)
		} else {
			defer func() { _ = client.Close() }()
			injectedSecrets, err = client.FetchAllEnvs(cfg)
			if err != nil {
				return fmt.Errorf("injection failed: %w", err)
			}
			for k, v := range injectedSecrets {
				envMap[k] = v
			}
		}
	}

	// G304: Ensure the filename is safe and does not escape the workspace
	safePath, err := config.IsSafePath("", filename)
	if err != nil {
		return fmt.Errorf("invalid compose file path: %w", err)
	}

	data, err := os.ReadFile(safePath) // #nosec G304 -- safePath is constrained by config.IsSafePath.
	if err != nil {
		return fmt.Errorf("failed to read compose file %s: %w", filename, err)
	}

	var parsed ComposeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	// Step 1: Pre-compute which services are managed by DSO so that non-DSO
	// services receive no labels, port-stripping, or env injection.
	absPath, err := filepath.Abs(filename)
	if err != nil {
		// QUAL-2: previously ignored, silently corrupting the
		// dso.compose.path label and project name derived from absPath below.
		return fmt.Errorf("failed to resolve absolute path for compose file %s: %w", filename, err)
	}
	managedServices := make(map[string]bool)
	if cfg != nil {
		for name, svcRaw := range parsed.Services {
			svc, ok := svcRaw.(map[string]interface{})
			if !ok {
				continue
			}
			for _, sec := range cfg.Secrets {
				if serviceIsTarget(name, svc, sec) {
					managedServices[name] = true
					break
				}
			}
		}
	}

	// Step 2: Inject rotation management labels into managed services only
	for name, svcRaw := range parsed.Services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}

		if !managedServices[name] {
			parsed.Services[name] = svc
			continue
		}

		var tmpfsMounts []string

		// Strip host port bindings and record them in a label so the DSO agent
		// proxy can own those ports and achieve zero-downtime rotation.
		// Only managed services participate in the TCP proxy.
		var portNotes []string
		svc, portNotes = stripAndLabelHostPorts(svc)
		for _, n := range portNotes {
			fmt.Fprintf(os.Stderr, "DSO: service %q: %s\n", name, n)
		}

		if cfg != nil {
			for _, sec := range cfg.Secrets {
				if !serviceIsTarget(name, svc, sec) {
					continue
				}

				// Inject labels for the agent to discover
				labels := make(map[string]interface{})
				if existingLabels, ok := svc["labels"].(map[string]interface{}); ok {
					labels = existingLabels
				}
				labels["dso.reloader"] = "true"
				labels["dso.compose.path"] = absPath
				// SEC-P0: stamp this agent's ownership identity so discovery
				// (internal/watcher.ReloaderController.checkOwnership) can
				// tell this container apart from one created by a different,
				// independently-configured DSO agent on the same host. A
				// container created without this (e.g. by a pre-fix DSO
				// version) is a "legacy" container: existing discovery code
				// defaults-deny it rather than silently matching it.
				if cfg.Agent.OwnerID != "" {
					labels["dso.owner"] = cfg.Agent.OwnerID
				}
				// Propagate the rotation strategy from dso.yaml to the container label so
				// the rotation agent uses the configured strategy (rolling/restart/signal)
				// instead of always defaulting to restart.
				if sec.Rotation.Strategy != "" {
					labels["dso.update.strategy"] = sec.Rotation.Strategy
				}

				// Track secrets for this service
				existingSecrets := ""
				if s, ok := labels["dso.secrets"].(string); ok {
					existingSecrets = s
				}
				if existingSecrets == "" {
					labels["dso.secrets"] = sec.Name
				} else if !strings.Contains(existingSecrets, sec.Name) {
					labels["dso.secrets"] = existingSecrets + "," + sec.Name
				}
				svc["labels"] = labels

				// Inject values based on mode (env/file)
				injectConfig := sec.Inject
				if injectConfig.Type == "" {
					injectConfig = cfg.Defaults.Inject
				}

				if injectConfig.Type == "file" && injectConfig.Path != "" {
					tmpfsMounts = append(tmpfsMounts, injectConfig.Path)
				} else {
					// Standard ENV mode — normalise list-style environment to map first
					var envSection map[string]interface{}
					if existingEnvMap, ok := svc["environment"].(map[string]interface{}); ok {
						envSection = existingEnvMap
					} else if existingEnvSlice, ok := svc["environment"].([]interface{}); ok {
						envSection = make(map[string]interface{})
						for _, item := range existingEnvSlice {
							strItem := fmt.Sprintf("%v", item)
							parts := strings.SplitN(strItem, "=", 2)
							if len(parts) == 2 {
								envSection[parts[0]] = parts[1]
							} else if len(parts) == 1 {
								envSection[parts[0]] = nil
							}
						}
					} else {
						envSection = make(map[string]interface{})
					}

					for keyInProvider, envName := range sec.Mappings {
						if val, ok := injectedSecrets[envName]; ok {
							envSection[envName] = val
						} else if val, ok := injectedSecrets[keyInProvider]; ok {
							envSection[envName] = val
						}
					}
					svc["environment"] = envSection
				}
			}
		}

		if len(tmpfsMounts) > 0 {
			svc["tmpfs"] = tmpfsMounts
		}
		parsed.Services[name] = svc
	}

	// Always use the transformed YAML to ensure labels and secrets are injected
	transformedData, err := yaml.Marshal(&parsed)
	if err != nil {
		return fmt.Errorf("failed to marshal transformed compose file: %w", err)
	}

	// [DIAGNOSTIC] Print injection summary to terminal
	for name, svcRaw := range parsed.Services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if labels, ok := svc["labels"].(map[string]interface{}); ok {
			if _, managed := labels["dso.reloader"]; managed {
				fmt.Printf(" [DSO] Linked service '%s' to secrets: %v\n", name, labels["dso.secrets"])
			}
		}
	}

	// Step 2: Run docker compose via stdin
	projectName := filepath.Base(filepath.Dir(absPath))

	// We use '-f -' to read the transformed YAML from stdin, avoiding disk leakage.
	args := append([]string{"compose", "-p", projectName, "-f", "-", "up"}, extraArgs...)
	cmd := exec.Command("docker", args...) // #nosec G204 -- docker arguments are intentionally forwarded without shell expansion.

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(string(transformedData))

	// Rebuild process environment for the docker command
	var finalEnvs []string
	for k, v := range envMap {
		finalEnvs = append(finalEnvs, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = finalEnvs
	cmd.Dir = filepath.Dir(absPath) // Set context to project folder for relative path resolution

	if dryRun {
		fmt.Printf("DRY RUN: DSO would securely inject the following secrets into %s (in-memory transformation):\n", filename)
		for k := range injectedSecrets {
			fmt.Printf("  - [Service Environment] %s=********\n", k)
		}
		fmt.Println("DRY RUN completed successfully. Use without --dry-run to deploy.")
		return nil
	}

	if debugMode {
		fmt.Println("DEBUG: Transformed Compose File (Redacted):")
		PrintRedactedCompose(&parsed)
	}

	fmt.Printf("DSO securely injecting secrets for %s (via in-memory pipe)...\n", filename)
	return cmd.Run()
}

// minProxyHostPort mirrors proxy.minUnprivilegedHostPort: the proxy refuses to
// bind lower host ports, so a mapping below it can NOT be proxied.
const minProxyHostPort = 1024

// publishedPort is one Compose port mapping, normalised from short or long syntax.
type publishedPort struct {
	hostIP    string // literal IP, "" = all interfaces
	host      string // published host port ("" when none / auto-assigned)
	container string // target port
	proto     string // "tcp", "udp", "sctp"
	// proxiable is true only when the DSO TCP proxy can reproduce this mapping
	// EXACTLY: one TCP port pair, a literal bind address, host port >= 1024.
	proxiable bool
	// why explains a non-proxiable mapping (empty when proxiable or unpublished).
	why string
}

// stripAndLabelHostPorts lets the DSO proxy own a managed service's host ports so
// rotation can swap backends without dropping the listener. It is deliberately
// conservative:
//
//   - Exposure must never widen. A mapping with a host address
//     ("127.0.0.1:5432:5432", "[::1]:8443:443", long-syntax host_ip) keeps that
//     address in the dso.host_ports label ("ip:host:container", IPv6 in brackets)
//     and the proxy binds exactly that address.
//   - Nothing is silently dropped or reinterpreted. The proxy represents only
//     single TCP pairs with host port >= 1024. If ANY published mapping of the
//     service cannot be reproduced exactly (UDP/SCTP, port ranges, auto-assigned
//     host ports, host ports < 1024, host-mode/unknown long-syntax options,
//     two addresses for one host port), the service's `ports:` are left
//     completely untouched and Docker publishes them directly. No label is
//     added, so the service gets the non-proxy rotation path (stop, then start)
//     rather than a proxy that would change what is exposed. A note is returned.
//   - Container-internal ports of proxied services are kept via `expose:`.
//
// It returns the (possibly modified) service and human-readable notes.
func stripAndLabelHostPorts(svc map[string]interface{}) (map[string]interface{}, []string) {
	rawPorts, ok := svc["ports"].([]interface{})
	if !ok {
		return svc, nil
	}

	var published []publishedPort
	var exposeOnly []string
	addExpose := func(p string) {
		for _, e := range exposeOnly {
			if e == p {
				return
			}
		}
		exposeOnly = append(exposeOnly, p)
	}
	for _, entry := range rawPorts {
		pp, publishes := parsePortSpec(entry)
		if !publishes {
			if pp.container != "" {
				addExpose(pp.container)
			}
			continue
		}
		published = append(published, pp)
	}
	if len(published) == 0 {
		return svc, nil
	}

	// One listener per host port: two different addresses for the same port are
	// ambiguous for the proxy, so they are not proxied.
	byPort := map[string]string{}
	for i := range published {
		pp := &published[i]
		if !pp.proxiable {
			continue
		}
		if prev, dup := byPort[pp.host]; dup && prev != pp.hostIP {
			pp.proxiable, pp.why = false, "the same host port is published on two different addresses"
		}
		byPort[pp.host] = pp.hostIP
	}
	var notes []string
	for _, pp := range published {
		if !pp.proxiable {
			notes = append(notes, fmt.Sprintf("port mapping %s is not proxiable (%s)", describePort(pp), pp.why))
		}
	}
	if len(notes) > 0 {
		notes = append(notes, "ports for this service are left as published by Docker (no DSO proxy, no zero-downtime proxy swap)")
		return svc, notes
	}

	var labelEntries []string
	for _, pp := range published {
		labelEntries = append(labelEntries, labelEntry(pp))
		addExpose(pp.container)
	}

	delete(svc, "ports")
	var exposeList []interface{}
	if el, ok := svc["expose"].([]interface{}); ok {
		exposeList = el
	}
	for _, p := range exposeOnly {
		exposeList = append(exposeList, p)
	}
	svc["expose"] = exposeList

	labels := make(map[string]interface{})
	if existingLabels, ok := svc["labels"].(map[string]interface{}); ok {
		for k, v := range existingLabels {
			labels[k] = v
		}
	}
	labels["dso.host_ports"] = strings.Join(labelEntries, ",")
	svc["labels"] = labels
	return svc, nil
}

func describePort(pp publishedPort) string {
	h := pp.host
	if h == "" {
		h = "<auto>"
	}
	ip := ""
	if pp.hostIP != "" {
		ip = pp.hostIP + ":"
	}
	return fmt.Sprintf("%s%s:%s/%s", ip, h, pp.container, pp.proto)
}

// labelEntry renders a proxiable mapping for the dso.host_ports label.
func labelEntry(pp publishedPort) string {
	switch {
	case pp.hostIP == "":
		return pp.host + ":" + pp.container
	case strings.Contains(pp.hostIP, ":"):
		return "[" + pp.hostIP + "]:" + pp.host + ":" + pp.container
	default:
		return pp.hostIP + ":" + pp.host + ":" + pp.container
	}
}

// parsePortSpec normalises one `ports:` entry (short string/int or long map).
// publishes is false for entries with no host publication at all (container
// port only), which stay expose-only.
func parsePortSpec(entry interface{}) (pp publishedPort, publishes bool) {
	switch v := entry.(type) {
	case map[string]interface{}:
		return parseLongPort(v)
	default:
		return parseShortPort(fmt.Sprintf("%v", v))
	}
}

func parseShortPort(s string) (pp publishedPort, publishes bool) {
	pp.proto = "tcp"
	if i := strings.LastIndex(s, "/"); i >= 0 {
		pp.proto = strings.ToLower(s[i+1:])
		s = s[:i]
	}
	var ip, rest string
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 || len(s) <= end+1 || s[end+1] != ':' {
			pp.why = "malformed IPv6 address"
			return pp, true
		}
		ip, rest = s[1:end], s[end+2:]
		if !validBindIP(ip, true) {
			pp.why = "invalid IPv6 address"
			return pp, true
		}
	} else {
		parts := strings.Split(s, ":")
		switch len(parts) {
		case 1: // container port only
			pp.container = parts[0]
			return pp, false
		case 2:
			rest = s
		case 3:
			ip, rest = parts[0], parts[1]+":"+parts[2]
			if !validBindIP(ip, false) {
				pp.why = "bind address must be a literal IPv4 address (use [addr] for IPv6)"
				return pp, true
			}
		default:
			pp.why = "unrecognised port syntax"
			return pp, true
		}
	}
	hc := strings.SplitN(rest, ":", 2)
	if len(hc) != 2 {
		pp.why = "unrecognised port syntax"
		return pp, true
	}
	pp.hostIP = canonicalIP(ip)
	pp.host, pp.container = hc[0], hc[1]
	classifyPort(&pp)
	return pp, true
}

func parseLongPort(m map[string]interface{}) (pp publishedPort, publishes bool) {
	pp.proto = "tcp"
	known := map[string]bool{"target": true, "published": true, "host_ip": true, "protocol": true, "mode": true, "name": true, "app_protocol": true}
	str := func(k string) string {
		if v, ok := m[k]; ok && v != nil {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
	pp.container = str("target")
	pp.host = str("published")
	if p := str("protocol"); p != "" {
		pp.proto = strings.ToLower(p)
	}
	ip := str("host_ip")
	if pp.host == "" && ip == "" {
		return pp, false // no publication requested
	}
	for k := range m {
		if !known[k] {
			pp.why = fmt.Sprintf("unsupported long-syntax option %q", k)
			return pp, true
		}
	}
	if mode := str("mode"); mode != "" && mode != "host" {
		pp.why = fmt.Sprintf("unsupported mode %q", mode)
		return pp, true
	}
	if ip != "" {
		isV6 := strings.Contains(ip, ":")
		if !validBindIP(ip, isV6) {
			pp.why = "host_ip must be a literal IP address"
			return pp, true
		}
		pp.hostIP = canonicalIP(ip)
	}
	classifyPort(&pp)
	return pp, true
}

// classifyPort decides proxiability of a normalised mapping (set pp.why when not).
func classifyPort(pp *publishedPort) {
	switch {
	case pp.proto != "tcp":
		pp.why = pp.proto + " is not carried by the TCP proxy (never translated to TCP)"
	case pp.host == "":
		pp.why = "host port is auto-assigned by Docker"
	case strings.ContainsAny(pp.host+pp.container, "-,"):
		pp.why = "port ranges are not supported by the proxy"
	default:
		hp, e1 := strconv.Atoi(pp.host)
		cp, e2 := strconv.Atoi(pp.container)
		switch {
		case e1 != nil || e2 != nil || hp < 1 || hp > 65535 || cp < 1 || cp > 65535:
			pp.why = "not a plain numeric port pair"
		case hp < minProxyHostPort:
			pp.why = fmt.Sprintf("host ports below %d cannot be bound by the proxy", minProxyHostPort)
		default:
			pp.proxiable = true
		}
	}
}

func validBindIP(ip string, wantV6 bool) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil || a.Zone() != "" || a.Is4In6() {
		return false
	}
	return a.Is6() == wantV6
}

func canonicalIP(ip string) string {
	if ip == "" {
		return ""
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	return a.String()
}

func PrintRedactedCompose(p *ComposeFile) {
	// Deep copy and redact
	redacted := *p
	redacted.Services = make(map[string]interface{})
	for name, svcRaw := range p.Services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			redacted.Services[name] = svcRaw
			continue
		}

		newSvc := make(map[string]interface{})
		for k, v := range svc {
			if k == "environment" {
				env, ok := v.(map[string]interface{})
				if ok {
					redactedEnv := make(map[string]interface{})
					for ek := range env {
						redactedEnv[ek] = observability.Redact(fmt.Sprintf("%v", env[ek]))
					}
					newSvc[k] = redactedEnv
				} else {
					newSvc[k] = v
				}
			} else {
				newSvc[k] = v
			}
		}
		redacted.Services[name] = newSvc
	}

	d, err := yaml.Marshal(&redacted)
	if err != nil {
		// QUAL-3: previously ignored, silently printing nothing instead of
		// surfacing a compose-parsing problem during --debug.
		fmt.Fprintf(os.Stderr, "\033[31m[DSO] Failed to render redacted compose for debug output: %v\033[0m\n", err)
		return
	}
	fmt.Println(string(d))
}

// ComposeStdinOptions configures RunComposeUpStdin.
type ComposeStdinOptions struct {
	ProjectName string
	ProjectDir  string   // directory relative paths, .env and env_file resolve against
	Args        []string // forwarded after `up`
	Env         []string // nil = inherit the current environment
	Stdout      io.Writer
	Stderr      io.Writer
	DockerBin   string // "" = "docker" resolved via PATH (tests substitute a fake)
}

// RunComposeUpStdin runs `docker compose -p NAME --project-directory DIR -f - up ARGS`
// feeding yamlData (which may contain resolved plaintext secrets) on STDIN. The
// resolved configuration therefore never touches the filesystem and never
// appears in a process argument. Cancelling ctx sends SIGTERM to compose (and
// SIGKILL after 30s) instead of orphaning it.
//
// Not covered, and documented in the CLI docs: the YAML exists in this process's
// memory and in the pipe; environment-variable secrets become container
// environment, which any principal with Docker daemon access can read via
// `docker inspect`; a compose validation error may echo parts of the config on
// stderr (passed through unchanged, never logged by DSO itself).
func RunComposeUpStdin(ctx context.Context, yamlData []byte, o ComposeStdinOptions) error {
	if o.ProjectName == "" {
		return errors.New("compose: project name is required")
	}
	if o.ProjectDir == "" || !filepath.IsAbs(o.ProjectDir) {
		return errors.New("compose: an absolute project directory is required")
	}
	bin := o.DockerBin
	if bin == "" {
		bin = "docker"
	}
	args := append([]string{"compose", "-p", o.ProjectName, "--project-directory", o.ProjectDir, "-f", "-", "up"}, o.Args...)
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- docker arguments are intentionally forwarded without shell expansion.
	cmd.Stdin = bytes.NewReader(yamlData)
	cmd.Stdout, cmd.Stderr = o.Stdout, o.Stderr
	cmd.Dir = o.ProjectDir
	if o.Env != nil {
		cmd.Env = o.Env
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 30 * time.Second
	return cmd.Run()
}
