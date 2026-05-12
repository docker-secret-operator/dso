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
func (cc *ContainerCloner) PrepareShadowConfig(ctx context.Context, containerID string, newEnvs map[string]string) (*container.Config, *container.HostConfig, *network.NetworkingConfig, string, error) {
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

	// DEEP COPY: Networking configuration
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: make(map[string]*network.EndpointSettings),
	}
	for name, endpoint := range inspect.NetworkSettings.Networks {
		// Endpoint settings may be shared; create a copy
		epCopy := *endpoint
		networkConfig.EndpointsConfig[name] = &epCopy
	}

	// Create Shadow Name
	originalName := strings.TrimPrefix(inspect.Name, "/")
	shadowName := originalName + "_dso_shadow"

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
