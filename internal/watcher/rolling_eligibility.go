package watcher

import (
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// rollingEligibility decides, BEFORE anything is created, stopped or renamed,
// whether a container can be replaced by the rolling (parallel candidate)
// strategy. Rolling needs the candidate to run next to the live container, which
// is only possible when no host address/port/protocol is bound by Docker itself:
//
//   - a Docker-published host port (any HostConfig.PortBindings entry: fixed,
//     UDP, range, auto-assigned, privileged, address-specific) cannot be bound by
//     two containers at once; the candidate would fail to start and the old
//     fallback would then STOP the healthy container;
//   - PublishAllPorts assigns different host ports to the candidate, changing the
//     endpoint;
//   - host networking shares the host's ports.
//
// Containers whose ports were handed to the DSO proxy carry no PortBindings (the
// compose rewrite removed them) and are eligible. A service with mixed
// proxy-managed and Docker-published ports is NOT: every mapping must be
// accounted for. The caller must reject (not silently switch strategy) when this
// returns an error: the operator's choice is respected and nothing is mutated.
func rollingEligibility(inspect container.InspectResponse) error {
	// An empty inspect means the daemon could not describe the container; the
	// strategy's own first step inspects it again and fails before any mutation, so
	// there is nothing to reject here.
	if inspect.ContainerJSONBase == nil || inspect.HostConfig == nil {
		return nil
	}
	hc := inspect.HostConfig
	var reasons []string
	if hc.NetworkMode.IsHost() {
		reasons = append(reasons, "it uses host networking (the candidate would share the host's ports)")
	}
	if hc.PublishAllPorts {
		reasons = append(reasons, "it publishes all ports (the candidate would be assigned different host ports)")
	}
	if len(hc.PortBindings) > 0 {
		var ports []string
		for p, b := range hc.PortBindings {
			desc := string(p)
			for _, hb := range b {
				switch {
				case hb.HostPort == "":
					desc += " (auto-assigned host port)"
				case hb.HostIP != "":
					desc += fmt.Sprintf(" (published on %s:%s)", hb.HostIP, hb.HostPort)
				default:
					desc += fmt.Sprintf(" (published on host port %s)", hb.HostPort)
				}
			}
			ports = append(ports, desc)
		}
		sort.Strings(ports)
		reasons = append(reasons, "Docker publishes host ports itself: "+strings.Join(ports, ", "))
		if inspect.Config != nil && inspect.Config.Labels["dso.host_ports"] != "" {
			reasons = append(reasons, "the service mixes proxy-managed and Docker-published ports")
		}
	}
	if len(reasons) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrRollingNotSupported, strings.Join(reasons, "; "))
}

// ErrRollingNotSupported means the rolling strategy cannot safely replace the
// container; nothing was changed. Use strategy "restart" (brief downtime), or have
// the DSO proxy own the service's plain-TCP ports.
var ErrRollingNotSupported = errorString("rolling rotation is not supported for this container (nothing was changed; use strategy \"restart\" or let the DSO proxy own its TCP ports)")

type errorString string

func (e errorString) Error() string { return string(e) }
