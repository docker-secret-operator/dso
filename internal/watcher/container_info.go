package watcher

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// SetDockerClient wires an explicit Docker client into an existing
// ReloaderController. Exported solely so other packages' tests can point a
// ReloaderController at a mock Docker daemon (see
// internal/server/containers_test.go) without a real Docker socket;
// production code should construct via NewReloaderController instead.
func (r *ReloaderController) SetDockerClient(cli *client.Client) {
	r.cli = cli
}

// DockerContainerInfo is the safe, operator-facing subset of Docker
// metadata for one container. Deliberately excludes everything a full
// `docker inspect` exposes -- Config.Env, mounts, labels, network
// internals -- keeping only what an operator needs to recognize a
// container and judge its runtime state. Never carries secret material.
type DockerContainerInfo struct {
	Name   string
	Image  string
	Status string   // Docker lifecycle state: running/exited/created/paused/restarting/dead
	Health string   // "healthy"/"unhealthy"/"starting", or "" when no healthcheck is configured
	Ports  []string // published ports only, formatted "hostPort->containerPort/proto"
}

// healthStatusPattern matches the health annotation Docker appends to a
// container's human-readable Status string, e.g. "Up 2 minutes (healthy)"
// or "Up 5 seconds (health: starting)". Parsing it here avoids a second,
// per-container Inspect call just to learn health -- ContainerList already
// returns it embedded in Status.
var healthStatusPattern = regexp.MustCompile(`\(health: (\w+)\)|\((healthy|unhealthy)\)`)

func parseHealthFromStatus(status string) string {
	m := healthStatusPattern.FindStringSubmatch(status)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// ListDockerContainerInfo returns safe Docker metadata for every container
// Docker currently knows about (running or not), keyed by container ID.
//
// Performance: this issues exactly ONE Docker ContainerList call no matter
// how many containers exist or how many are DSO-managed targets -- there
// is no per-container ContainerInspect here. Health is derived from the
// same Status string ContainerList already returns, not a second Inspect.
// This keeps GET /api/containers at O(1) Docker API calls.
func (r *ReloaderController) ListDockerContainerInfo(ctx context.Context) (map[string]DockerContainerInfo, error) {
	if r.cli == nil {
		// No Docker client wired (e.g. a test-only ReloaderController) --
		// return an honest empty map rather than panicking on a nil client.
		return map[string]DockerContainerInfo{}, nil
	}

	list, err := r.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	out := make(map[string]DockerContainerInfo, len(list))
	for _, c := range list {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		ports := make([]string, 0, len(c.Ports))
		for _, p := range c.Ports {
			if p.PublicPort == 0 {
				// Not published to the host -- container-internal only,
				// and not useful/safe operator-facing information here.
				continue
			}
			ports = append(ports, fmt.Sprintf("%d->%d/%s", p.PublicPort, p.PrivatePort, p.Type))
		}

		out[c.ID] = DockerContainerInfo{
			Name:   name,
			Image:  c.Image,
			Status: c.State,
			Health: parseHealthFromStatus(c.Status),
			Ports:  ports,
		}
	}
	return out, nil
}
