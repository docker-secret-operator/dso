//go:build dockerint

package docker

// SECTION 2: GATED-DELIVERY FUNCTIONAL (see the section map in n19_diag_helpers_test.go).
//
// N19 diagnostic scenarios K, L, M: the REAL managed rotation (the production
// ReloaderController: label discovery, ownership gate, rolling / restart
// strategies, health gate, file probes) of a FILE-mode secret.
//
// What the production path does now, and so what a real run is expected to show IF the
// design's premise holds (all of it is RUNTIME-UNVERIFIED until this has been run against
// an approved isolated daemon; compiling is not executing):
//
//	candidate created with its ENTRYPOINT wrapped by the startup gate, tmpfs declared
//	-> started; the gate (not the application) is PID 1 and the application has NOT run
//	-> inspect verifies identity, rotation label, gate, tmpfs at every destination
//	-> secret delivered through the exec injector (stdin, atomic, RequireTmpfs), probed
//	-> delivery evidence journalled, THEN the release flag is written, THEN the application
//	   starts and readiness/handoff proceed. A candidate never runs without its secret.
//	Expected evidence: the canary is in the RUNNING tmpfs of the replacement and of the
//	promoted container, never in the writable layer (that is scenario I's question).
//
// How they are driven (same as test/docker/scenarios_test.go S3 and
// signal_rotation_test.go): a plain labelled container (dso.reloader, dso.owner,
// dso.secrets, dso.update.strategy) with a declared tmpfs at /run/secrets/dso,
// a real ReloaderController started in-process against the guarded daemon, its
// cache supplied through fakeCache, and TriggerReload. No agent, Vault or Compose
// is needed to drive the code paths:
//   K: strategy "rolling"  -> rotation.RollingStrategy.Execute (gated candidate, step
//      delivery via rotation.Delivery, post-readiness probe of every delivered file).
//   L: strategy "restart"  -> the controller's recreation branch (gated replacement,
//      deliverGatedRestart, probeSpecs after start).
//   M: readiness gate with an application that never reads the file.
// LIMITATION (documented, not hidden): executeSimpleRestart (the rolling FALLBACK) is
// unexported and is reached only as the FALLBACK of a rolling failure that is NOT
// health-class or delivery-refusal, so it is not driven here; the Compose-project
// recreation (ComposePath != "") and the root agent are not driven either: that needs a
// compose file plus the agent process. Re-delivery after a daemon restart, the
// agent-start reconciliation, crash points inside the gated phases and dispatch-vs-delivered
// identity have NO scenario here (sections 4-6 of the map): they are Docker-free tests only.
//
// While the rotation runs, a poller observes every NEW container of the scenario
// (the shadow / replacement) directly: its file state inside the running tmpfs, its
// application's own view, its declared tmpfs and health status. That is what makes
// the outcome observable even when the rotation rolls the shadow back.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

func n19FileConfig() *dsoConfig.Config {
	cfg := &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{providerName: {Type: "vault"}},
		Secrets: []dsoConfig.SecretMapping{{
			Name:     secretName,
			Provider: providerName,
			Inject:   dsoConfig.InjectionConfig{Type: "file", Path: n19Dir, UID: n19UID, GID: n19UID},
		}},
	}
	cfg.Agent.OwnerID = ownerID
	cfg.Agent.Rotation.HealthCheckTimeout = "30s"
	return cfg
}

// n19Rot is what one managed rotation produced.
type n19Rot struct {
	mu sync.Mutex

	origID, origName, newID string
	rotOutcome              string // "success" | "error: ..." | "timeout"
	swapped                 bool   // the name now belongs to a different container
	origRunningAfter        bool

	shadowSeen    bool
	shadowID      string
	shadowTmpfs   map[string]string
	shadowState   string
	everMatch     bool // exec sha256 matched at some tick
	everAbsent    bool
	everMismatch  bool
	sawHealthy    bool
	healthStatus  string
	appMatch      bool
	appAbsent     bool
	appSeen       bool
	appDetail     string
	lastExecError string
	ticks         int

	postIntegrity string // for the container holding the name after a swap
}

func (r *n19Rot) shadowIntegrity() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case !r.shadowSeen:
		return "unknown"
	case r.everMatch:
		return "match"
	case r.everMismatch:
		return "mismatch"
	case r.everAbsent:
		return "absent"
	}
	return "unknown"
}

func (r *n19Rot) appIntegrity() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.appMatch:
		return "match"
	case r.appAbsent:
		return "absent"
	}
	return "unknown"
}

// n19Poll observes every container of this scenario other than the original until stop closes.
func n19Poll(t *testing.T, lv, want string, rot *n19Rot, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	dc := dockerClient(t)
	sess := dockerguard.Current(t)
	for {
		select {
		case <-stop:
			return
		case <-time.After(300 * time.Millisecond):
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		list, err := dc.ContainerList(ctx, container.ListOptions{All: true, Filters: n19LabelFilters(t, lv)})
		cancel()
		if err != nil {
			continue
		}
		for _, c := range list {
			if c.ID == rot.origID || c.State != "running" {
				continue
			}
			ictx, icancel := context.WithTimeout(context.Background(), 10*time.Second)
			in, ierr := dc.ContainerInspect(ictx, c.ID)
			icancel()
			if ierr != nil || in.State == nil || !in.State.Running {
				continue
			}
			got, d := n19ExecHash(sess, t, c.ID, n19File)
			ap, _, ash, aok := n19AppLast(dc, c.ID)
			rot.mu.Lock()
			rot.ticks++
			rot.shadowSeen, rot.shadowID = true, c.ID
			if in.HostConfig != nil {
				rot.shadowTmpfs = in.HostConfig.Tmpfs
			}
			rot.shadowState = in.State.Status
			if in.State.Health != nil {
				rot.healthStatus = in.State.Health.Status
				if in.State.Health.Status == "healthy" {
					rot.sawHealthy = true
				}
			}
			switch n19Integrity(got, want) {
			case "match":
				rot.everMatch = true
			case "absent":
				rot.everAbsent = true
			case "mismatch":
				rot.everMismatch = true
			}
			if d != "" {
				rot.lastExecError = d
			}
			if aok {
				rot.appSeen = true
				switch {
				case ap && n19Integrity(ash, want) == "match":
					rot.appMatch = true
				case !ap:
					rot.appAbsent = true
				}
				rot.appDetail = fmt.Sprintf("last app line present=%v", ap)
			}
			rot.mu.Unlock()
		}
	}
}

var n19HealthFlags = []string{"--health-cmd", "true", "--health-interval", "1s", "--health-retries", "1"}

// n19ManagedRotation runs one real managed file-mode rotation and returns what was observed.
func n19ManagedRotation(t *testing.T, sc, lv, strategy, app string, healthcheck bool) *n19Rot {
	t.Helper()
	v1, _ := n19Canary(sc)
	v2, want2 := n19Canary(sc)
	cache := &fakeCache{data: map[string]map[string]string{}}
	cache.set(v1)

	name := uniqueName(t, "dso-it-n19m")
	args := []string{"run", "-d", "--name", name}
	args = append(args, dockerguard.Current(t).LabelArgs()...)
	args = append(args,
		"--label", "dso.reloader=true",
		"--label", "dso.owner="+ownerID,
		"--label", "dso.secrets="+secretName,
		"--label", "dso.update.strategy="+strategy,
		"--label", "dso.it.run="+lv,
		"--label", n19Label+"="+lv,
		"--tmpfs", n19Dir)
	if healthcheck {
		args = append(args, n19HealthFlags...)
	}
	args = append(args, testImage, "sh", "-c", app)
	origID := n19LastToken(dockerCmd(t, args...))
	if !n19IDRe.MatchString(origID) {
		t.Fatalf("docker run did not return a container id")
	}
	rot := &n19Rot{origID: origID, origName: name}
	if !n19WaitRunning(t, origID) {
		t.Fatalf("original did not start")
	}
	if err := n19SeedRunning(t, origID, v1); err != nil {
		t.Fatalf("%v", err)
	}

	ctrl := startControllerCfg(t, cache, n19FileConfig())
	waitTarget(t, ctrl, origID)

	stop, done := make(chan struct{}), make(chan struct{})
	go n19Poll(t, lv, want2, rot, stop, done)

	cache.set(v2)
	outcome := make(chan error, 1)
	if err := ctrl.TriggerReload(context.Background(), secretName, func(err error) { outcome <- err }); err != nil {
		close(stop)
		<-done
		rot.rotOutcome = "error: TriggerReload: " + n19Clip(injRedact(err.Error()), 200)
	} else {
		select {
		case err := <-outcome:
			rot.rotOutcome = "success"
			if err != nil {
				rot.rotOutcome = "error: " + n19Clip(injRedact(err.Error()), 300)
			}
		case <-time.After(rotateWait):
			rot.rotOutcome = "timeout"
		}
		// Give the poller one more tick to catch a still-running replacement.
		time.Sleep(600 * time.Millisecond)
		close(stop)
		<-done
	}

	dc := dockerClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if in, err := dc.ContainerInspect(ctx, name); err == nil {
		rot.newID = in.ID
		rot.swapped = in.ID != origID
		if rot.swapped && in.State != nil && in.State.Running {
			got, _ := n19ExecHash(dockerguard.Current(t), t, in.ID, n19File)
			rot.postIntegrity = n19Integrity(got, want2)
		}
	}
	if in, err := dc.ContainerInspect(ctx, origID); err == nil && in.State != nil {
		rot.origRunningAfter = in.State.Running
	}
	return rot
}

func (r *n19Rot) emitManaged(t *testing.T, sc, strategy string) {
	t.Helper()
	o := n19Base(t, sc, r.shadowID)
	o.Container = n19Short(r.shadowID)
	o.Observation, o.Operation, o.Location = "rotation_outcome", "real ReloaderController.TriggerReload, strategy="+strategy+", file-mode secret at "+n19Dir, "none"
	o.LifecycleState = "n/a"
	r.mu.Lock()
	o.Detail = fmt.Sprintf("outcome=%s; swapped=%v; original_running_after=%v; shadow_observed=%v", r.rotOutcome, r.swapped, r.origRunningAfter, r.shadowSeen)
	tmpfs := r.shadowTmpfs
	r.mu.Unlock()
	n19Emit(t, o)

	// The replacement as observed while it ran (location shadow_tmpfs).
	o = n19Obs{Scenario: sc, Container: n19Short(r.shadowID), Observation: "replacement_file_state", Operation: "poll: exec sha256 of the file inside the replacement's running tmpfs", Location: "shadow_tmpfs"}
	o.DeclaredTmpfs = tmpfs
	o.LifecycleState = r.shadowState
	o.CanaryIntegrity = r.shadowIntegrity()
	o.FileVisibility = n19Visibility(o.CanaryIntegrity)
	r.mu.Lock()
	o.Detail = fmt.Sprintf("ticks=%d health=%s saw_healthy=%v replacement_declares_tmpfs=%v exec_err=%s", r.ticks, r.healthStatus, r.sawHealthy, len(tmpfs) > 0, r.lastExecError)
	r.mu.Unlock()
	n19Emit(t, o)

	// The application's own view in the replacement (location app_view).
	o = n19Obs{Scenario: sc, Container: n19Short(r.shadowID), Observation: "replacement_application_view", Operation: "poll: the replacement application's own N19-APP log line", Location: "app_view"}
	o.CanaryIntegrity = r.appIntegrity()
	o.FileVisibility = n19Visibility(o.CanaryIntegrity)
	r.mu.Lock()
	o.Detail = fmt.Sprintf("app_lines_seen=%v %s", r.appSeen, r.appDetail)
	r.mu.Unlock()
	n19Emit(t, o)

	// The container now holding the service name, only when a swap happened.
	if r.swapped {
		o = n19Base(t, sc, r.newID)
		o.Observation, o.Operation, o.Location = "post_swap_service_container", "exec sha256 of the file in the container now holding the service name", "running_tmpfs"
		o.CanaryIntegrity = r.postIntegrity
		o.FileVisibility = n19Visibility(o.CanaryIntegrity)
		o.Detail = "swap committed"
		n19Emit(t, o)
	} else {
		o = n19Base(t, sc, r.origID)
		o.Observation, o.Operation, o.Location = "original_after_rotation", "state of the original after the rotation (no swap happened)", "original_not_rotated"
		o.Detail = fmt.Sprintf("original_running=%v (not a rotation result: the original still holds the previous secret)", r.origRunningAfter)
		n19Emit(t, o)
	}
}

// K: managed ROLLING rotation of a file-mode secret: does the secret exist in the
// shadow's tmpfs, and what does the rotation report?
func TestN19Diag_K_ManagedRollingFileRotation(t *testing.T) {
	lv := n19Begin(t, "K")
	rot := n19ManagedRotation(t, "K", lv, "rolling", n19App, false)
	rot.emitManaged(t, "K", "rolling")
}

// L: managed RECREATE (strategy "restart") rotation of a file-mode secret: the
// controller's own gated create -> start -> verify -> exec delivery -> release -> probe path.
func TestN19Diag_L_ManagedRecreateFileRotation(t *testing.T) {
	lv := n19Begin(t, "L")
	rot := n19ManagedRotation(t, "L", lv, "restart", n19App, false)
	rot.emitManaged(t, "L", "restart")
}

// M: candidate readiness and failure handling. The application is healthy
// REGARDLESS of the file (it never reads it) and has a HEALTHCHECK that always
// passes, so the health gate cannot catch an absent secret. Records whether the
// gate passed (replacement seen healthy), whether the rotation reports success,
// and whether the secret is readable in the replacement.
func TestN19Diag_M_ReadinessGateWithAbsentFile(t *testing.T) {
	lv := n19Begin(t, "M")
	rot := n19ManagedRotation(t, "M", lv, "rolling", n19IdleApp, true)
	rot.emitManaged(t, "M", "rolling")

	o := n19Obs{Scenario: "M", Container: n19Short(rot.shadowID), Observation: "gate_vs_secret_presence", Operation: "healthy-regardless application + always-passing HEALTHCHECK, rolling strategy", Location: "none"}
	rot.mu.Lock()
	o.Detail = fmt.Sprintf("health_gate_passed(saw healthy)=%v; rotation_outcome=%s; secret_in_replacement=%s; interpretation: gate_passed=true with outcome=success and secret absent would be a SILENT failure; outcome=error with the file probe naming the path is fail-closed",
		rot.sawHealthy, rot.rotOutcome, rot.shadowIntegrityLocked())
	rot.mu.Unlock()
	n19Emit(t, o)
}

// shadowIntegrityLocked is shadowIntegrity for callers already holding mu.
func (r *n19Rot) shadowIntegrityLocked() string {
	switch {
	case !r.shadowSeen:
		return "unknown"
	case r.everMatch:
		return "match"
	case r.everMismatch:
		return "mismatch"
	case r.everAbsent:
		return "absent"
	}
	return "unknown"
}
