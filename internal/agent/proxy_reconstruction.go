package agent

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Startup proxy reconstruction (M0-04 findings 3.1 and 3.2).
//
// The proxy is embedded in `dso agent`; its backend table is in-memory and is lost
// with the process. After a restart the ONLY routing the agent can establish is the
// table it rebuilds itself. The sequence is:
//
//	AttachProxy        gated scan: register only containers that are not rotation
//	                   candidates of an unsettled operation (3.2); hold the rest
//	recovery           AutomaticRecovery, whose RoutingVerifier reads this same
//	                   table (3.1): retire a workload only when the table positively
//	                   shows the replacement routed and the retiree not
//	settleProxy        RescanHeld: held containers whose operation recovery settled
//	                   become eligible and are registered
//
// Nothing here waits on anything else, so startup and recovery cannot deadlock.

// RoutingSource is what a RoutingVerifier needs from the proxy; *proxy.Manager
// satisfies it.
type RoutingSource interface {
	RoutingSnapshot() (active []string, established bool)
}

// ProxyRoutingVerifier answers AutomaticRecovery's routing questions from the live
// backend table of the SAME process's embedded proxy. It answers "unknown" unless
// the table was positively reconstructed from Docker in this process; a journalled
// handoff acknowledgement never makes it answer on its own, because that only
// proves a dead process once switched routing.
type ProxyRoutingVerifier struct{ Source RoutingSource }

// NewProxyRoutingVerifier returns a verifier over src. A nil src yields a verifier
// that always answers "unknown".
func NewProxyRoutingVerifier(src RoutingSource) *ProxyRoutingVerifier {
	return &ProxyRoutingVerifier{Source: src}
}

// ActiveBackends implements RoutingVerifier. Container IDs are globally unique, so
// the process-wide set of active backends is sufficient for the membership checks
// recovery makes about one service's holder and artifact.
func (v *ProxyRoutingVerifier) ActiveBackends(_ context.Context, _ string) ([]string, bool) {
	if v == nil || v.Source == nil {
		return nil, false
	}
	ids, established := v.Source.RoutingSnapshot()
	if !established {
		return nil, false
	}
	return ids, true
}

// ProxyEligibility returns the journal-aware traffic-eligibility gate for startup
// scans. A discovered container is HELD (not routed) when it is
//
//   - a DSO rotation artifact by name (<svc>_dso_new_*, <svc>_dso_backup_*,
//     <svc>_old_<ts>): it is not the real service workload, whatever its state;
//   - named as a candidate by an UNSETTLED journal record (by candidate ID, or by
//     the dso.rotation.id label when the crash preceded the ID being journalled),
//     unless the journal shows the swap committed, the container holds the real
//     service name and Docker reports it not unhealthy/still starting (then it is
//     the promoted workload the previous process was already routing to);
//   - rotation-labelled while the journal is unreadable or unavailable (nothing
//     can say its operation finished).
//
// A rotation-labelled container whose record is terminal (completed/recovered), or
// whose record has aged out of the journal, is the settled promoted workload and is
// eligible. Ownership is enforced separately by the proxy Manager before the gate.
func (st *StateTracker) ProxyEligibility() proxy.Eligibility {
	return func(c proxy.ScanCandidate) (bool, string) {
		name := ""
		for _, n := range c.Names {
			n = strings.TrimPrefix(n, "/")
			if name == "" {
				name = n
			}
			if IsDSORotationArtifactName(n) {
				return false, "rotation artifact " + n
			}
		}
		rid := c.Labels[rotation.LabelRotationID]
		if st == nil {
			if rid != "" {
				return false, "rotation candidate " + rid + ": no journal available"
			}
			return true, ""
		}
		if corrupt, _ := st.Corrupt(); corrupt {
			if rid != "" {
				return false, "rotation candidate " + rid + ": journal unreadable"
			}
			return true, ""
		}
		for _, r := range st.snapshotRecords() {
			listed := idIn(r.CandidateContainerIDs, c.ID) || (rid != "" && r.RotationID == rid)
			if !listed {
				continue
			}
			switch r.Status {
			case "completed", "recovered":
				continue
			}
			if promotedAndReady(r, name, c.Status) {
				continue
			}
			return false, "candidate of unsettled operation " + r.RotationID + " (" + r.Strategy + ", last " + r.Transition + ")"
		}
		return true, ""
	}
}

// promotedAndReady reports whether r shows the swap committed (written only after
// the candidate was health-verified and the handoff returned), the container holds
// the real service name, and Docker does not report it unhealthy or still starting.
func promotedAndReady(r *RotationState, name, dockerStatus string) bool {
	committed := r.Committed
	switch r.Transition {
	case rotation.TrSwapCommitted, rotation.TrIntentCleanup:
		committed = true
	}
	if !committed || r.ServiceID == "" || name != r.ServiceID {
		return false
	}
	s := strings.ToLower(dockerStatus)
	return !strings.Contains(s, "unhealthy") && !strings.Contains(s, "health: starting")
}

// AttachProxy wires the embedded proxy into the engine and performs the gated
// startup scan. It must run after the engine exists (the journal is needed for the
// gate) and before StartAll (recovery uses the resulting routing table). A nil pm
// or Docker client is a no-op.
func (t *TriggerEngine) AttachProxy(ctx context.Context, pm *proxy.Manager) {
	if pm == nil {
		return
	}
	t.proxyMgr = pm
	if t.Reloader != nil {
		t.Reloader.ProxyManager = pm
	}
	t.Routing = NewProxyRoutingVerifier(pm)
	if t.DockerClient == nil {
		return
	}
	rep := pm.ScanAndRegisterGated(ctx, t.DockerClient, t.StateTracker.ProxyEligibility())
	t.Logger.Info("Proxy routing reconstructed from running containers",
		zap.Int("registered", len(rep.Registered)), zap.Int("held", len(rep.Held)))
}

// settleProxy admits the containers the startup scan held once recovery has
// settled their operations. Containers whose operation is still unresolved stay
// held (and the service stays blocked by the operation guard).
func (t *TriggerEngine) settleProxy() {
	if t.proxyMgr == nil || t.DockerClient == nil {
		return
	}
	rep := t.proxyMgr.RescanHeld(t.ctx, t.DockerClient, t.StateTracker.ProxyEligibility())
	if len(rep.Registered) > 0 || len(rep.Held) > 0 {
		t.Logger.Info("Held proxy candidates re-evaluated after startup recovery",
			zap.Int("admitted", len(rep.Registered)), zap.Int("still_held", len(rep.Held)))
	}
}
