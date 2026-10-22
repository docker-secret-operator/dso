package rotation

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// ContainerCloner handles the metadata extraction for shadow instances
type ContainerCloner struct {
	cli *client.Client
}

func NewContainerCloner(cli *client.Client) *ContainerCloner {
	return &ContainerCloner{cli: cli}
}

// PrepareShadowConfig extracts and modifies the configuration of an existing container for rolling update.
// All config objects are defensively copied to prevent race conditions with concurrent operations.
//
// SEC-P0-3: managedEnvNames is every env var name this container's config
// maps ANY of its secrets to, independent of each secret's *current* inject
// type. config.Env starts as a verbatim deep copy of the original container's
// (below), and the newEnvs merge loop only ever adds/updates keys present in
// newEnvs -- it never removes one. Without stripping managedEnvNames entries
// that are no longer present in newEnvs first, a secret's value once written
// to Config.Env (while it was env-mode) would persist unchanged into every
// future shadow container even after that secret is switched to file-mode or
// dropped from config -- this is the exact mechanism already fixed for the
// rolling-fallback (executeSimpleRestart) and primary-restart paths; this is
// the third and, being the default *successful* rolling path, most commonly
// exercised instance of the same defect.
func (cc *ContainerCloner) PrepareShadowConfig(ctx context.Context, containerID string, newEnvs map[string]string, managedEnvNames map[string]bool) (*container.Config, *container.HostConfig, *network.NetworkingConfig, string, error) {
	inspect, err := cc.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("failed to inspect container: %w", err)
	}

	// DEFENSIVE COPY: Do not mutate the original config objects returned by Docker.
	// Create a shallow copy of Config
	config := *inspect.Config

	// DEEP COPY: Create a new Env slice before mutation to avoid race conditions
	// with concurrent access to the same config
	newEnv := make([]string, len(config.Env))
	copy(newEnv, config.Env)
	config.Env = newEnv

	// Drop the deprecated container-level MAC as well: on older API versions
	// this is the field that pins the address, and a shadow sharing the
	// original's MAC corrupts the bridge while both are running (see the
	// networking note below).
	config.MacAddress = "" //nolint:staticcheck // deprecated field, cleared deliberately

	// DEEP COPY: Labels
	if config.Labels != nil {
		newLabels := make(map[string]string)
		for k, v := range config.Labels {
			newLabels[k] = v
		}
		config.Labels = newLabels
	}

	// DEEP COPY: Volumes
	if config.Volumes != nil {
		newVolumes := make(map[string]struct{})
		for k, v := range config.Volumes {
			newVolumes[k] = v
		}
		config.Volumes = newVolumes
	}

	// DEFENSIVE COPY: Create a new HostConfig
	hostConfig := *inspect.HostConfig

	// DEEP COPY: Mounts
	if hostConfig.Mounts != nil {
		newMounts := make([]mount.Mount, len(hostConfig.Mounts))
		copy(newMounts, hostConfig.Mounts)
		hostConfig.Mounts = newMounts
	}

	// DEEP COPY: Networking configuration.
	//
	// Only the operator's INTENT is carried over (which networks to join, and
	// any aliases/links/driver options). Runtime identity assigned by Docker
	// to the original -- MacAddress, IPAddress, EndpointID, NetworkID,
	// gateways -- is deliberately dropped.
	//
	// Copying those verbatim is not cosmetic: the shadow runs ALONGSIDE the
	// original for the length of the health check, and Docker honours a
	// supplied MacAddress, so the clone joined the bridge with the same MAC as
	// the container still serving traffic. Two ports with one MAC corrupts the
	// bridge forwarding table, and frames for the original's IP start landing
	// on the wrong veth -- observed as "no route to host"/dial timeouts
	// against a container that was demonstrably healthy. IPAMConfig IS
	// preserved because it is explicit operator configuration (a pinned
	// address); if that genuinely collides, Docker rejects the create loudly
	// instead of silently poisoning the network.
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: SanitizedEndpointsConfig(inspect.NetworkSettings.Networks),
	}

	// Create Shadow Name
	originalName := strings.TrimPrefix(inspect.Name, "/")
	shadowName := originalName + "_dso_shadow"

	// SEC-P0-3: drop any inherited env entry this rotation manages but did
	// not supply a fresh value for (e.g. switched to file-mode, or dropped
	// from config) -- see the doc comment above for why this can't be
	// skipped.
	if len(managedEnvNames) > 0 {
		filtered := make([]string, 0, len(config.Env))
		for _, e := range config.Env {
			k, _, _ := strings.Cut(e, "=")
			if _, stillOverridden := newEnvs[k]; managedEnvNames[k] && !stillOverridden {
				continue
			}
			filtered = append(filtered, e)
		}
		config.Env = filtered
	}

	// Inject new environment variables into our copy (safe now)
	for k, v := range newEnvs {
		found := false
		for i, e := range config.Env {
			if strings.HasPrefix(e, k+"=") {
				config.Env[i] = fmt.Sprintf("%s=%s", k, v)
				found = true
				break
			}
		}
		if !found {
			config.Env = append(config.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return &config, &hostConfig, networkConfig, shadowName, nil
}

// SanitizedEndpointsConfig builds the network configuration for a replacement
// container: it joins exactly the same networks as the original and keeps the
// operator's intent (aliases, links, driver options, an explicitly pinned
// IPAMConfig), but drops every field Docker assigned to the original at
// runtime -- MacAddress, IPAddress, EndpointID, NetworkID, gateways.
//
// This matters because a replacement runs ALONGSIDE the original until the
// handoff completes. Docker honours a supplied MacAddress, so reusing the
// original's put two live containers on the bridge with one MAC, corrupting
// the forwarding table: traffic to the ORIGINAL, still-healthy container
// began failing with "no route to host"/dial timeouts. Every rotation path
// that creates a replacement must use this.
func SanitizedEndpointsConfig(networks map[string]*network.EndpointSettings) map[string]*network.EndpointSettings {
	out := make(map[string]*network.EndpointSettings, len(networks))
	for name, endpoint := range networks {
		if endpoint == nil {
			continue
		}
		out[name] = &network.EndpointSettings{
			IPAMConfig: endpoint.IPAMConfig,
			Aliases:    append([]string(nil), endpoint.Aliases...),
			Links:      append([]string(nil), endpoint.Links...),
			DriverOpts: endpoint.DriverOpts,
		}
	}
	return out
}
