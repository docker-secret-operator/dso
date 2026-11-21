package proxy

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/util"
)

// Traffic eligibility at startup (M0-04 finding 3.2).
//
// Discovery ("this running container declares dso.host_ports") is not the same as
// eligibility ("it is safe to send client connections to it"). At agent start the
// scan used to turn every running, owned, labelled container into a live backend
// immediately, including a rotation candidate that was created, possibly only
// partially injected or never health-verified, by an operation the previous process
// died in the middle of. Startup recovery runs AFTER the scan, so until it settled
// the operation the router could already be sending real connections to that
// unverified workload.
//
// The scan is therefore gated. A candidate the gate does not clear is HELD: it is
// recorded (never forgotten), not bound and not registered, and is reconsidered by
// RescanHeld after recovery has settled the operation. There is no deadlock: the
// gate never blocks recovery, recovery never waits for the proxy, and a held
// container is released the moment its operation reaches a terminal state.

// ScanCandidate is what the gate may look at: identity and labels, no secrets.
type ScanCandidate struct {
	ID     string
	Names  []string // Docker names as listed (leading "/" included)
	Labels map[string]string
	Status string // Docker's human status, e.g. "Up 3 minutes (healthy)"
}

// Eligibility decides whether a discovered container may receive traffic now.
// A false result carries a human-readable reason that is logged and kept for
// inspection; the container is held, not discarded.
type Eligibility func(c ScanCandidate) (eligible bool, reason string)

// DefaultEligibility is the journal-less rule used by plain ScanAndRegister: a
// container stamped with a rotation identity (dso.rotation.id) is a rotation
// CANDIDATE, and without a journal there is nothing that says its operation
// finished, so it is held. The agent installs a journal-aware gate instead
// (internal/agent: ProxyEligibility) that also releases candidates whose operation
// is recorded as terminal.
func DefaultEligibility(c ScanCandidate) (bool, string) {
	if rid := c.Labels[rotation.LabelRotationID]; rid != "" {
		return false, "rotation candidate " + rid + ": operation not known to be settled"
	}
	return true, ""
}

// ScanReport summarises one scan pass.
type ScanReport struct {
	Listed     bool              // the Docker listing succeeded
	Registered []string          // container IDs newly registered by this pass
	Held       map[string]string // container ID -> reason, as of the end of the pass
	Failures   int               // eligible containers that could not be bound/registered (informational: they are simply not routed)
}

// ScanAndRegister scans running Docker containers for the dso.host_ports label
// and registers the ones DefaultEligibility clears. Called once at agent startup
// by callers that have no journal; the agent uses ScanAndRegisterGated.
func (m *Manager) ScanAndRegister(ctx context.Context, cli *client.Client) {
	m.ScanAndRegisterGated(ctx, cli, DefaultEligibility)
}

// ScanAndRegisterGated is ScanAndRegister with an explicit eligibility gate. A nil
// gate means DefaultEligibility. It records whether reconstruction completed (see
// RoutingSnapshot).
func (m *Manager) ScanAndRegisterGated(ctx context.Context, cli *client.Client, gate Eligibility) ScanReport {
	containers, err := cli.ContainerList(ctx, containerListOptions())
	if err != nil {
		m.log.Warn("proxy: failed to list containers for scan", zap.Error(err))
		m.rmu.Lock()
		m.reconstruct = reconstructionIncomplete
		m.rmu.Unlock()
		return ScanReport{}
	}
	return m.registerScannedGated(containers, gate)
}

// RescanHeld re-evaluates ONLY the containers a previous scan held, after startup
// recovery had the chance to settle their operations. Newly eligible ones are
// registered; still-ineligible ones stay held; ones that no longer run are dropped.
// It never touches a container the scan did not hold, so it cannot undo a live
// swap performed since.
func (m *Manager) RescanHeld(ctx context.Context, cli *client.Client, gate Eligibility) ScanReport {
	m.rmu.Lock()
	n := len(m.heldByScan)
	m.rmu.Unlock()
	if n == 0 {
		return ScanReport{Listed: true, Held: map[string]string{}}
	}
	containers, err := cli.ContainerList(ctx, containerListOptions())
	if err != nil {
		m.log.Warn("proxy: failed to list containers for held rescan; they stay held", zap.Error(err))
		return ScanReport{Held: m.HeldSnapshot()}
	}
	var heldNow []container.Summary
	running := map[string]bool{}
	for _, c := range containers {
		running[c.ID] = true
		if m.IsHeld(c.ID) {
			heldNow = append(heldNow, c)
		}
	}
	m.rmu.Lock()
	for id := range m.heldByScan {
		if !running[id] {
			delete(m.heldByScan, id)
		}
	}
	m.rmu.Unlock()
	return m.registerScannedGated(heldNow, gate)
}

// IsHeld reports whether the startup scan is holding id away from traffic. The
// watcher consults it so a Docker "start" event or a reconciliation pass cannot
// register a held candidate behind the gate's back.
func (m *Manager) IsHeld(id string) bool {
	m.rmu.Lock()
	defer m.rmu.Unlock()
	_, ok := m.heldByScan[id]
	return ok
}

// HeldSnapshot returns a copy of the held containers and the reason for each.
func (m *Manager) HeldSnapshot() map[string]string {
	m.rmu.Lock()
	defer m.rmu.Unlock()
	out := make(map[string]string, len(m.heldByScan))
	for k, v := range m.heldByScan {
		out[k] = v
	}
	return out
}

// registerScanned registers the listed containers with the default gate. Kept for
// callers (and tests) that scan an already-obtained listing.
func (m *Manager) registerScanned(containers []container.Summary) {
	m.registerScannedGated(containers, DefaultEligibility)
}

// registerScannedGated registers the eligible containers' dso.host_ports. It never
// writes labels, so repeated scans cannot claim or change another agent's
// ownership. It is idempotent: a container already registered is left alone.
func (m *Manager) registerScannedGated(containers []container.Summary, gate Eligibility) ScanReport {
	if gate == nil {
		gate = DefaultEligibility
	}
	rep := ScanReport{Listed: true}
	for _, c := range containers {
		portsLabel := c.Labels["dso.host_ports"]
		if portsLabel == "" {
			continue
		}
		if !m.OwnerAllowed(c.Labels) {
			m.log.Warn("proxy: container is owned by a different DSO agent instance; not registering",
				zap.String("id", util.ShortID(c.ID)))
			continue
		}
		if ok, why := gate(ScanCandidate{ID: c.ID, Names: c.Names, Labels: c.Labels, Status: c.Status}); !ok {
			m.rmu.Lock()
			if m.heldByScan == nil {
				m.heldByScan = map[string]string{}
			}
			m.heldByScan[c.ID] = why
			m.rmu.Unlock()
			m.log.Warn("proxy: container held back from traffic until startup recovery settles it",
				zap.String("id", util.ShortID(c.ID)), zap.String("reason", why))
			continue
		}
		m.rmu.Lock()
		delete(m.heldByScan, c.ID)
		m.rmu.Unlock()
		if _, already := m.registry.Get(c.ID); already {
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
			rep.Failures++
			continue
		}
		for _, pm := range portMaps {
			if err := m.EnsurePortOn(pm.HostIP, pm.HostPort, pm.ContainerPort); err != nil {
				m.log.Warn("proxy: failed to bind port",
					zap.Int("port", pm.HostPort), zap.Error(err))
				rep.Failures++
				continue
			}
			if err := m.RegisterContainer(c.ID, containerIP, pm.HostPort, pm.ContainerPort); err != nil {
				m.log.Warn("proxy: failed to register container",
					zap.String("id", util.ShortID(c.ID)), zap.Error(err))
				rep.Failures++
			}
		}
		rep.Registered = append(rep.Registered, c.ID)
		m.log.Info("proxy: registered existing container",
			zap.String("id", util.ShortID(c.ID)),
			zap.String("ports", portsLabel))
	}
	m.rmu.Lock()
	// The table is the truth of what THIS process routes: a container that failed
	// to bind/register is simply not routed (and so never reported as active). What
	// makes the table untrustworthy is not having listed Docker at all.
	m.reconstruct = reconstructionComplete
	rep.Held = make(map[string]string, len(m.heldByScan))
	for k, v := range m.heldByScan {
		rep.Held[k] = v
	}
	m.rmu.Unlock()
	return rep
}
