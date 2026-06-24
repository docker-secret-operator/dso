package proxy

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/util"
)

// PortMap is a single host-port → container-port mapping parsed from the
// dso.host_ports label, e.g. "3306:3306".
type PortMap struct {
	HostPort      int
	ContainerPort int
}

// Manager owns all DSO-managed TCP proxies. One proxy server is shared across
// all port bindings; each port gets its own listener. The registry/router are
// shared so the zero-downtime swap (drain old → add new) is atomic.
//
// Zero-downtime swap sequence (called by TriggerReload after new container is healthy):
//  1. Add new backend → router immediately routes new connections there
//  2. SetDraining(old) → router stops routing new connections to old
//  3. Remove(old)      → deregistered (in-flight connections already finished)
type Manager struct {
	mu       sync.Mutex
	registry *Registry
	router   *Router
	server   *Server
	log      *zap.Logger

	// containerToBackendID maps containerID → backendID (same value here) so we
	// can look up a backend when we know only the container being replaced.
	containerToBackendID sync.Map
}

// NewManager creates a Manager and starts the underlying TCP proxy server.
func NewManager(log *zap.Logger) *Manager {
	reg := NewRegistry()
	router := NewRouter(reg)
	srv := NewServer(router, log)
	return &Manager{
		registry: reg,
		router:   router,
		server:   srv,
		log:      log,
	}
}

// EnsurePort opens a host-port listener if one is not already open.
func (m *Manager) EnsurePort(hostPort, targetPort int) error {
	return m.server.Bind(PortBinding{ListenPort: hostPort, TargetPort: targetPort})
}

// RegisterContainer adds a container as a backend for the given host port.
// containerIP is the container's Docker-network IP address.
func (m *Manager) RegisterContainer(containerID, containerIP string, hostPort, containerPort int) error {
	backendID := containerID
	addr := fmt.Sprintf("%s:%d", containerIP, containerPort)

	if err := m.registry.Add(Backend{ID: backendID, Addr: addr}); err != nil {
		return fmt.Errorf("proxy: register container %s: %w", util.ShortID(containerID), err)
	}
	m.containerToBackendID.Store(containerID, backendID)
	m.log.Info("proxy: container registered as backend",
		zap.String("container", util.ShortID(containerID)),
		zap.String("addr", addr),
		zap.Int("host_port", hostPort))
	return nil
}

// SwapBackend performs the zero-downtime backend swap:
//  1. Register newContainerID as an active backend (new connections go here immediately)
//  2. Drain oldContainerID (no new connections; in-flight complete naturally)
//  3. After drainWait, remove oldContainerID from registry
//
// drainWait should be long enough for the longest expected in-flight request to complete.
func (m *Manager) SwapBackend(oldContainerID, newContainerID, newContainerIP string, containerPort, hostPort int) error {
	newAddr := fmt.Sprintf("%s:%d", newContainerIP, containerPort)

	// Step 1: add new backend — router starts sending new connections immediately
	if err := m.registry.Add(Backend{ID: newContainerID, Addr: newAddr}); err != nil {
		return fmt.Errorf("proxy: add new backend: %w", err)
	}
	m.containerToBackendID.Store(newContainerID, newContainerID)

	m.log.Info("proxy: new backend active, draining old",
		zap.String("old", util.ShortID(oldContainerID)),
		zap.String("new", util.ShortID(newContainerID)),
		zap.String("new_addr", newAddr))

	// Step 2: mark old backend draining — router stops routing new connections to it
	if err := m.registry.SetDraining(oldContainerID); err != nil {
		m.log.Warn("proxy: could not drain old backend (may already be removed)",
			zap.String("old", util.ShortID(oldContainerID)),
			zap.Error(err))
	}

	// Step 3: wait for in-flight connections on old backend to complete, then remove.
	// We do this in a goroutine so TriggerReload is not blocked.
	go func() {
		// A short fixed drain window covers the vast majority of in-flight requests.
		// The active-connections WaitGroup in Server guarantees actual in-flight
		// connections are not dropped; this just ensures we don't remove the backend
		// entry while something is still dialing it.
		time.Sleep(5 * time.Second)
		if err := m.registry.Remove(oldContainerID); err != nil {
			m.log.Debug("proxy: remove old backend (already gone)",
				zap.String("old", util.ShortID(oldContainerID)))
		}
		m.containerToBackendID.Delete(oldContainerID)
		m.log.Info("proxy: old backend removed",
			zap.String("old", util.ShortID(oldContainerID)))
	}()

	return nil
}

// DeregisterContainer removes a container's backend immediately (e.g. on container stop).
func (m *Manager) DeregisterContainer(containerID string) {
	_ = m.registry.SetDraining(containerID)
	_ = m.registry.Remove(containerID)
	m.containerToBackendID.Delete(containerID)
}

// Stop gracefully shuts down all proxy listeners.
func (m *Manager) Stop(drainTimeout time.Duration) error {
	return m.server.CloseGraceful(drainTimeout)
}

// ParseHostPorts parses a "dso.host_ports" label value like "3306:3306,8080:80"
// into a slice of PortMap.
func ParseHostPorts(label string) []PortMap {
	if label == "" {
		return nil
	}
	var out []PortMap
	for _, part := range strings.Split(label, ",") {
		part = strings.TrimSpace(part)
		sides := strings.SplitN(part, ":", 2)
		if len(sides) != 2 {
			continue
		}
		hp, e1 := strconv.Atoi(sides[0])
		cp, e2 := strconv.Atoi(sides[1])
		if e1 != nil || e2 != nil {
			continue
		}
		out = append(out, PortMap{HostPort: hp, ContainerPort: cp})
	}
	return out
}

// ScanAndRegister scans running Docker containers for the dso.host_ports label
// and registers them with the proxy. Called once at agent startup.
func (m *Manager) ScanAndRegister(ctx context.Context, cli *client.Client) {
	containers, err := cli.ContainerList(ctx, containerListOptions())
	if err != nil {
		m.log.Warn("proxy: failed to list containers for scan", zap.Error(err))
		return
	}

	for _, c := range containers {
		portsLabel := c.Labels["dso.host_ports"]
		if portsLabel == "" {
			continue
		}
		portMaps := ParseHostPorts(portsLabel)
		if len(portMaps) == 0 {
			continue
		}
		containerIP := extractContainerIP(c.NetworkSettings)
		if containerIP == "" {
			m.log.Warn("proxy: container has no IP, skipping",
				zap.String("id", util.ShortID(c.ID)))
			continue
		}
		for _, pm := range portMaps {
			if err := m.EnsurePort(pm.HostPort, pm.ContainerPort); err != nil {
				m.log.Warn("proxy: failed to bind port",
					zap.Int("port", pm.HostPort), zap.Error(err))
				continue
			}
			if err := m.RegisterContainer(c.ID, containerIP, pm.HostPort, pm.ContainerPort); err != nil {
				m.log.Warn("proxy: failed to register container",
					zap.String("id", util.ShortID(c.ID)), zap.Error(err))
			}
		}
		m.log.Info("proxy: registered existing container",
			zap.String("id", util.ShortID(c.ID)),
			zap.String("ports", portsLabel))
	}
}
