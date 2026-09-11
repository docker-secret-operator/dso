package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/injector"
	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// RotationChecks covers DSO-DOCTOR-018 through DSO-DOCTOR-022 (Phase 7 /
// P2-5): operational visibility into rotation state, degraded services,
// orphaned rotation artifacts, proxy host-port conflicts, and secret-
// destination reachability. Before this existed, `dso doctor` could report
// a fully healthy result while DSO itself had an active operational
// problem in exactly these areas -- the Phase 6 audit's core P2-5 finding.
//
// This deliberately does NOT duplicate rotation/proxy logic: rotation-state
// and degraded-service checks query the LIVE running agent process over
// its existing Unix-socket RPC (the same mechanism `docker dso status`
// already uses), since that state is in-memory-only and has no other safe
// external observation point. Orphan-container detection reuses
// agent.IsDSORotationArtifactName -- the exact same naming-pattern
// predicate internal/agent/recovery.go's own cleanup logic uses -- rather
// than re-implementing it here and risking drift on a security-sensitive
// check. Host-port-conflict detection reads only Docker container labels;
// it cannot and does not reach into the live agent's in-memory proxy
// registry, which has no safe external diagnostic surface today (see
// checkProxyHostPortConflicts's doc comment).
type RotationChecks struct {
	configPath string

	// getAgentStatus queries the live agent process. Injected so tests
	// never require a real running agent/socket.
	getAgentStatus func() (*api.StatusResponse, error)

	// listDSOContainers lists Docker containers on the host (name + the raw
	// dso.host_ports label value, if any). Injected so tests never require
	// a real Docker daemon.
	listDSOContainers func(ctx context.Context) ([]dsoContainerInfo, error)

	loadConfig func(path string) (*config.Config, error)
	statDir    func(path string) (os.FileInfo, error)
}

// dsoContainerInfo is the minimal, safe Docker metadata this check needs --
// deliberately not a raw Docker inspect/summary struct.
type dsoContainerInfo struct {
	Name      string
	HostPorts string // raw dso.host_ports label value; "" if absent
}

func newRotationChecks(socketPath, configPath string) *RotationChecks {
	return &RotationChecks{
		configPath: configPath,
		getAgentStatus: func() (*api.StatusResponse, error) {
			client, err := injector.NewAgentClientWithTimeout(socketPath, 3*time.Second)
			if err != nil {
				return nil, err
			}
			defer func() { _ = client.Close() }()
			return client.GetStatus()
		},
		listDSOContainers: defaultListDSOContainers,
		loadConfig:        config.LoadConfig,
		statDir:           os.Stat,
	}
}

// defaultListDSOContainers shells out to `docker ps`, matching the existing
// exec-based, dependency-light convention this package's other checks
// already use (see doctor_docker.go) rather than pulling in the full Docker
// SDK client just for this diagnostic. Lists ALL containers, not only
// DSO-labeled ones -- both downstream checks (orphan-name matching,
// non-empty host-ports label) already filter correctly, so over-listing
// here is harmless and keeps this a single, simple call.
func defaultListDSOContainers(ctx context.Context) ([]dsoContainerInfo, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", `{{.Names}}\t{{.Label "dso.host_ports"}}`).Output()
	if err != nil {
		return nil, err
	}
	var containers []dsoContainerInfo
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		info := dsoContainerInfo{Name: parts[0]}
		if len(parts) > 1 {
			info.HostPorts = parts[1]
		}
		containers = append(containers, info)
	}
	return containers, nil
}

func (rc *RotationChecks) run(ctx context.Context) []DoctorCheck {
	status, statusErr := rc.getAgentStatus()
	return []DoctorCheck{
		rc.checkRotationState(status, statusErr),
		rc.checkDegradedServices(status, statusErr),
		rc.checkOrphanContainers(ctx),
		rc.checkProxyHostPortConflicts(ctx),
		rc.checkSecretDestinations(),
	}
}

// DSO-DOCTOR-018: no rotation stuck awaiting recovery/operator review.
//
// status.PendingRotations already excludes historical, successfully
// completed/recovered rotations (see internal/agent.StateTracker.
// GetPendingRotations) -- only rollback_required, critical_error, or a
// genuinely still-in-progress rotation counts here, so this distinguishes
// "active failure" from "historical failure" by construction, not by any
// additional logic in doctor itself.
func (rc *RotationChecks) checkRotationState(status *api.StatusResponse, statusErr error) DoctorCheck {
	const id = "DSO-DOCTOR-018"
	const name = "Rotation state"
	const desc = "The running agent must have no rotations stuck awaiting recovery or operator review"

	if statusErr != nil {
		return infoCheck(id, name, desc,
			"could not reach the running DSO agent to check rotation state: "+statusErr.Error(),
			DoctorCatRotation,
		)
	}
	if status.PendingRotations == 0 {
		return passCheck(id, name, desc, "no pending rotations", DoctorCatRotation)
	}
	return warnCheck(id, name, desc,
		fmt.Sprintf("%d rotation(s) pending recovery or manual review", status.PendingRotations),
		"A rotation did not complete cleanly (e.g. an agent crash mid-rotation) and is awaiting automatic recovery or operator intervention",
		DoctorCatRotation,
		"Run `docker dso status` for detail on the affected secret/container",
		"Check agent logs for the automatic-recovery outcome",
	)
}

// DSO-DOCTOR-019: no service currently tracked as DSO-degraded. This is
// DSO's own operational concept (a rotation failed and the previous
// container was deliberately kept serving traffic) and is explicitly NOT
// inferred from Docker container health -- those are different concepts,
// and this check never conflates them.
func (rc *RotationChecks) checkDegradedServices(status *api.StatusResponse, statusErr error) DoctorCheck {
	const id = "DSO-DOCTOR-019"
	const name = "Degraded services"
	const desc = "No service should be currently marked DSO-degraded (distinct from Docker container health)"

	if statusErr != nil {
		return infoCheck(id, name, desc,
			"could not reach the running DSO agent to check degraded-service state: "+statusErr.Error(),
			DoctorCatRotation,
		)
	}
	if len(status.DegradedServices) == 0 {
		return passCheck(id, name, desc, "no services currently marked degraded", DoctorCatRotation)
	}

	// Include each service's own reason (already a safe, redaction-passed
	// string -- see internal/watcher/controller.go's markDegraded call
	// sites and internal/notify/event.go's error redaction) so the
	// operator gets actionable detail directly from doctor, not just a
	// pointer to go look elsewhere.
	details := make([]string, 0, len(status.DegradedServices))
	for _, d := range status.DegradedServices {
		details = append(details, fmt.Sprintf("%s (%s)", d.Service, d.Reason))
	}
	return failCheck(id, name, desc,
		fmt.Sprintf("%d service(s) degraded: %s", len(status.DegradedServices), strings.Join(details, "; ")),
		"A rotation failed and DSO kept the previous container serving traffic rather than risk downtime",
		DoctorCritical, DoctorCatRotation,
		"Investigate and fix the underlying provider/secret issue for the listed service(s)",
		"Trigger a fresh rotation once the underlying issue is fixed to clear the degraded state",
	)
}

// DSO-DOCTOR-020: no leftover DSO-created rotation containers from an
// interrupted rotation. Diagnostic only -- never deletes anything; the
// existing agent-restart automatic-recovery path (internal/agent/
// recovery.go) remains the actual cleanup action.
func (rc *RotationChecks) checkOrphanContainers(ctx context.Context) DoctorCheck {
	const id = "DSO-DOCTOR-020"
	const name = "Orphaned rotation containers"
	const desc = "No leftover DSO rotation containers (_dso_backup_, _dso_new_, or _old_<timestamp>) should remain on the host"

	containers, err := rc.listDSOContainers(ctx)
	if err != nil {
		return infoCheck(id, name, desc,
			"could not list Docker containers to check for orphans: "+err.Error(),
			DoctorCatRotation,
		)
	}

	var orphans []string
	for _, c := range containers {
		if agent.IsDSORotationArtifactName(c.Name) {
			orphans = append(orphans, c.Name)
		}
	}
	if len(orphans) == 0 {
		return passCheck(id, name, desc, "no orphaned rotation containers found", DoctorCatRotation)
	}
	return warnCheck(id, name, desc,
		fmt.Sprintf("%d orphaned rotation container(s) found: %s", len(orphans), strings.Join(orphans, ", ")),
		"A previous rotation was interrupted (e.g. an agent crash) before its own cleanup completed",
		DoctorCatRotation,
		"Restart the DSO agent -- automatic recovery cleans these up on startup",
		"Or remove manually after confirming they are not in use: docker rm -f <container>",
	)
}

// DSO-DOCTOR-021: no two containers claim the same DSO proxy host port.
//
// This reads only Docker container labels (dso.host_ports), never the live
// agent's in-memory proxy registry -- that registry only exists inside the
// running agent process and exposing it externally would require new
// architecture (a diagnostic RPC/HTTP surface) this phase deliberately does
// not add. Label-based conflict detection still catches the single most
// common real-world proxy misconfiguration (two services declaring the
// same host port) without that coupling.
func (rc *RotationChecks) checkProxyHostPortConflicts(ctx context.Context) DoctorCheck {
	const id = "DSO-DOCTOR-021"
	const name = "Proxy host-port conflicts"
	const desc = "Containers declaring a DSO proxy host port (dso.host_ports label) must not claim the same port as another container"

	containers, err := rc.listDSOContainers(ctx)
	if err != nil {
		return infoCheck(id, name, desc,
			"could not list Docker containers to check host-port claims: "+err.Error(),
			DoctorCatRotation,
		)
	}

	portOwners := make(map[int][]string)
	for _, c := range containers {
		if c.HostPorts == "" {
			continue
		}
		for _, pm := range dsoProxy.ParseHostPorts(c.HostPorts) {
			portOwners[pm.HostPort] = append(portOwners[pm.HostPort], c.Name)
		}
	}

	var conflicts []string
	for port, owners := range portOwners {
		if len(owners) > 1 {
			conflicts = append(conflicts, fmt.Sprintf("port %d: %s", port, strings.Join(owners, ", ")))
		}
	}

	if len(conflicts) == 0 {
		if len(portOwners) == 0 {
			return infoCheck(id, name, desc, "no containers declare dso.host_ports; nothing to check", DoctorCatRotation)
		}
		return passCheck(id, name, desc,
			fmt.Sprintf("%d declared host port(s), no conflicts found", len(portOwners)), DoctorCatRotation)
	}
	return failCheck(id, name, desc,
		strings.Join(conflicts, "; "),
		"Two or more containers declare the same DSO proxy host port -- only one can actually be bound",
		DoctorHigh, DoctorCatRotation,
		"Review the dso.host_ports label on the listed containers and ensure each host port is unique",
	)
}

// DSO-DOCTOR-022: file-mode secret injection destinations are reachable.
// Validates infrastructure (does the destination directory exist) only --
// never reads, prints, or otherwise touches a secret value.
func (rc *RotationChecks) checkSecretDestinations() DoctorCheck {
	const id = "DSO-DOCTOR-022"
	const name = "Secret destination reachability"
	const desc = "File-mode secret injection destination directories must exist -- this never inspects secret values"

	cfg, err := rc.loadConfig(rc.configPath)
	if err != nil {
		return infoCheck(id, name, desc,
			"could not load DSO config to check injection destinations: "+err.Error(),
			DoctorCatRotation,
		)
	}
	if cfg == nil {
		return infoCheck(id, name, desc, "no configuration loaded; skipping", DoctorCatRotation)
	}

	type dest struct {
		secret string
		dir    string
	}
	var dests []dest
	for _, sec := range cfg.Secrets {
		injectConfig := sec.Inject
		if injectConfig.Type == "" {
			injectConfig = cfg.Defaults.Inject
		}
		if injectConfig.Type == "file" && injectConfig.Path != "" {
			dests = append(dests, dest{secret: sec.Name, dir: filepath.Dir(injectConfig.Path)})
		}
	}

	if len(dests) == 0 {
		return infoCheck(id, name, desc, "no file-mode secret injections configured", DoctorCatRotation)
	}

	var unreachable []string
	for _, d := range dests {
		if _, statErr := rc.statDir(d.dir); statErr != nil {
			unreachable = append(unreachable, fmt.Sprintf("%s (destination dir %s): %v", d.secret, d.dir, statErr))
		}
	}

	if len(unreachable) == 0 {
		return passCheck(id, name, desc,
			fmt.Sprintf("%d file-mode injection destination(s) reachable", len(dests)), DoctorCatRotation)
	}
	return warnCheck(id, name, desc,
		strings.Join(unreachable, "; "),
		"A configured file-mode secret's destination directory does not exist or could not be statted -- injection will fail at the next rotation",
		DoctorCatRotation,
		"Ensure the destination directory exists on the target container's mount path before the next rotation",
	)
}
