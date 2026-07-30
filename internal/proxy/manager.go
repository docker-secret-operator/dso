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

	// ctx/cancel bound the lifetime of background goroutines spawned by
	// SwapBackend (the deferred-removal drain wait) to the Manager's own
	// lifetime, so Stop can guarantee none of them outlive it.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// allowedHostPorts is an optional operator-supplied allow-list (SEC-7
	// full fix). nil means "no restriction beyond the privileged-port floor
	// ParseHostPorts already enforces" — the default, so existing
	// deployments are unaffected until an operator opts in via
	// SetAllowedHostPorts.
	allowedHostPorts *portAllowList
}

// NewManager creates a Manager and starts the underlying TCP proxy server.
func NewManager(log *zap.Logger) *Manager {
	reg := NewRegistry()
	router := NewRouter(reg)
	srv := NewServer(router, log)
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		registry: reg,
		router:   router,
		server:   srv,
		log:      log,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// EnsurePort opens a host-port listener if one is not already open. Rejects
// hostPort if it falls outside an operator-configured allow-list (SEC-7).
func (m *Manager) EnsurePort(hostPort, targetPort int) error {
	m.mu.Lock()
	al := m.allowedHostPorts
	m.mu.Unlock()

	if !al.allows(hostPort) {
		return fmt.Errorf("proxy: host port %d is not permitted by the proxy.allowed_host_ports allow-list", hostPort)
	}
	return m.server.Bind(PortBinding{ListenPort: hostPort, TargetPort: targetPort})
}

// SetAllowedHostPorts configures an optional allow-list of host ports the
// proxy is permitted to bind (SEC-7 full fix, following up on the
// privileged-port floor in ParseHostPorts). Entries may be an exact port
// ("8080") or an inclusive range ("3000-4000"). Passing nil or an empty
// slice clears the allow-list, restoring the default "allow any port >=
// 1024" behavior. Must be called before EnsurePort for the ports it should
// govern — typically once at startup, before ScanAndRegister.
func (m *Manager) SetAllowedHostPorts(entries []string) error {
	al, err := newPortAllowList(entries)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.allowedHostPorts = al
	m.mu.Unlock()
	return nil
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
	// We do this in a goroutine so TriggerReload is not blocked. The wait races
	// against m.ctx so Stop can cancel it rather than leaving it to run past
	// Manager shutdown.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// A short fixed drain window covers the vast majority of in-flight requests.
		// The active-connections WaitGroup in Server guarantees actual in-flight
		// connections are not dropped; this just ensures we don't remove the backend
		// entry while something is still dialing it.
		select {
		case <-time.After(5 * time.Second):
		case <-m.ctx.Done():
			// Manager is shutting down; the whole registry is being torn down
			// with it, so there is nothing useful left to remove.
			return
		}
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

// Stop gracefully shuts down all proxy listeners. It also cancels and waits
// for any in-flight SwapBackend deferred-removal goroutines, so no
// background goroutine outlives Stop returning.
func (m *Manager) Stop(drainTimeout time.Duration) error {
	m.cancel()
	m.wg.Wait()
	return m.server.CloseGraceful(drainTimeout)
}

// minUnprivilegedHostPort is the lowest host port ParseHostPorts will bind.
// dso.host_ports is attacker-influenceable (any container can set its own
// labels), so this is the single choke point all three callers (manager.go's
// ScanAndRegister, controller.go's event handlers) go through — without a
// floor here, a container could ask DSO's daemon (typically root in its
// container, per SEC-3) to bind well-known/privileged host ports such as 22
// or 53 (SEC-7). DSO's own compose.go, which generates this label for
// legitimate use, only ever writes back the port pairs already present in the
// operator's own compose file — those are virtually always >= 1024 for
// application services, so this floor does not restrict the intended use.
const minUnprivilegedHostPort = 1024

// ParseHostPorts parses a "dso.host_ports" label value like "3306:3306,8080:80"
// into a slice of PortMap. Entries with a malformed pair, an out-of-range
// port, or a host port below minUnprivilegedHostPort are silently skipped
// (SEC-7), consistent with how malformed entries are already skipped here.
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
		if hp < minUnprivilegedHostPort || hp > 65535 {
			continue
		}
		if cp < 1 || cp > 65535 {
			continue
		}
		out = append(out, PortMap{HostPort: hp, ContainerPort: cp})
	}
	return out
}

// portAllowList is an optional operator-supplied allow-list of host ports
// (SEC-7 full fix). A nil *portAllowList always allows (checked via the
// nil-safe allows method), which is the zero-value/default behavior.
type portAllowList struct {
	ports  map[int]bool
	ranges []portRange
}

type portRange struct {
	lo, hi int
}

// newPortAllowList parses allow-list entries like "8080" or "3000-4000".
// An empty slice returns a nil *portAllowList (no restriction).
func newPortAllowList(entries []string) (*portAllowList, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	al := &portAllowList{ports: make(map[int]bool)}
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if lo, hi, isRange := strings.Cut(e, "-"); isRange {
			loN, err1 := strconv.Atoi(strings.TrimSpace(lo))
			hiN, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 != nil || err2 != nil || loN < 1 || hiN > 65535 || loN > hiN {
				return nil, fmt.Errorf("proxy: invalid allowed_host_ports range %q", raw)
			}
			al.ranges = append(al.ranges, portRange{lo: loN, hi: hiN})
			continue
		}
		p, err := strconv.Atoi(e)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("proxy: invalid allowed_host_ports entry %q", raw)
		}
		al.ports[p] = true
	}
	return al, nil
}

// allows reports whether port is permitted. A nil receiver (no allow-list
// configured) always allows, preserving the pre-SEC-7-full-fix default.
func (al *portAllowList) allows(port int) bool {
	if al == nil {
		return true
	}
	if al.ports[port] {
		return true
	}
	for _, r := range al.ranges {
		if port >= r.lo && port <= r.hi {
			return true
		}
	}
	return false
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
