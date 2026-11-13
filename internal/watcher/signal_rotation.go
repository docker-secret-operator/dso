package watcher

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/audit"
	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/util"
)

// Signal rotation (strategy "signal") re-delivers the secret FILES of a running
// container and then asks the application to reload them. It exists only for
// file-mode secrets: a signal cannot change the environment of a running process,
// so env-mode secrets are rejected rather than silently "rotated".
//
// What a successful result means (and does not):
//
//	Delivered         the new files were written atomically (injector) and probed
//	ReloadRequested   the configured signal was delivered to the container
//	Readiness         Docker HEALTHCHECK healthy, or only "process still running"
//	AdoptionVerified  ALWAYS false: DSO has no application-level acknowledgement
//	                  that the new secret was loaded (an app holding the old file
//	                  open keeps reading the old inode until it reopens it)
//
// Failure after the new file is published cannot be rolled back automatically:
// no plaintext backup of the previous value is kept, and the application's state
// cannot be established. The result says so and the service is flagged degraded.

var (
	// ErrSignalEnvSecret rejects signal rotation of env-mode secrets.
	ErrSignalEnvSecret = errors.New("signal rotation cannot update environment-variable secrets (a running process's environment never changes); use strategy restart or rolling")
	// ErrSignalNotOwned: the container is not owned by this agent.
	ErrSignalNotOwned = errors.New("signal rotation refused: container is not owned by this DSO agent")
	// ErrSignalNoFileSecret: nothing to deliver.
	ErrSignalNoFileSecret = errors.New("signal rotation needs at least one file-mode secret mapped to this container")
	// ErrSignalBadSignal: configured signal is not an allowed reload signal.
	ErrSignalBadSignal = errors.New("unsupported reload signal")
)

// SignalState is the precise condition a signal rotation ended in. It exists so an
// operator is never told "unrecoverable" when the truth is "uncertain", nor "rolled
// back" when nothing was rolled back.
type SignalState string

const (
	// SignalNothingPublished: delivery failed before any secret file was replaced;
	// the previous value is intact. No signal was sent.
	SignalNothingPublished SignalState = "nothing_published"
	// SignalPartiallyPublished: some, but not all, secret files were replaced. The
	// secret SET is inconsistent. No signal was sent.
	SignalPartiallyPublished SignalState = "partially_published"
	// SignalPublishedUnverified: every file was delivered but verification failed;
	// no signal was sent.
	SignalPublishedUnverified SignalState = "published_verification_failed"
	// SignalPublishedNotSignalled: files delivered and verified, the reload signal
	// could not be delivered.
	SignalPublishedNotSignalled SignalState = "published_signal_not_sent"
	// SignalSentOutcomeUnknown: the signal was delivered but readiness afterwards
	// failed or could not be established; whether the application reloaded is unknown.
	SignalSentOutcomeUnknown SignalState = "signal_sent_outcome_unknown"
	// SignalReloadRequested: delivered, verified, signal sent, readiness evidence
	// obtained. Adoption is still NOT verified.
	SignalReloadRequested SignalState = "reload_requested_adoption_unverified"
)

// SignalRotationError carries the precise end state. A state other than
// SignalNothingPublished means secret files may differ from what the running
// application holds; DSO does NOT restore the previous value (no plaintext backup is
// kept, and the previous version may have been revoked at its provider). No
// rollback occurred; manual recovery is the supported path: re-deliver or restart.
type SignalRotationError struct {
	State SignalState
	Stage string
	Cause error
}

func (e *SignalRotationError) Error() string {
	msg := fmt.Sprintf("signal rotation failed at %s [%s]: %v", e.Stage, e.State, e.Cause)
	if e.State != SignalNothingPublished {
		msg += "; no rollback was performed and the previous value was NOT restored; the application's state is uncertain, manual recovery (reload or restart) is required"
	}
	return msg
}

func (e *SignalRotationError) Unwrap() error { return e.Cause }

// SignalOutcome states exactly which lifecycle steps are evidenced.
type SignalOutcome struct {
	DeliveredFiles   []string // file names delivered and probed
	Delivered        bool
	ReloadRequested  bool
	Readiness        rotation.Readiness
	AdoptionVerified bool // always false: no application acknowledgement mechanism exists
	Limitation       string
	State            SignalState
}

const signalLimitation = "the application's adoption of the new secret is NOT verified: DSO cannot observe whether the process reopened the file (a process holding the old file open keeps reading the old inode)"

// allowedReloadSignals are the only signals DSO will send for a reload. Never
// TERM/KILL/INT/QUIT: those stop the workload instead of reloading it.
var allowedReloadSignals = map[string]bool{"SIGHUP": true, "SIGUSR1": true, "SIGUSR2": true}

func normalizeSignal(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "SIGHUP", nil
	}
	if !strings.HasPrefix(s, "SIG") {
		s = "SIG" + s
	}
	if !allowedReloadSignals[s] {
		return "", fmt.Errorf("%w %q (allowed: SIGHUP, SIGUSR1, SIGUSR2)", ErrSignalBadSignal, s)
	}
	return s, nil
}

// signalOps are the side effects, injectable so the ordering and failure
// semantics can be unit-tested without a Docker daemon.
type signalOps struct {
	inspect func(ctx context.Context, id string) (container.InspectResponse, error)
	deliver func(ctx context.Context, id string, spec rotation.FileSecretSpec) error
	probe   func(ctx context.Context, id string, spec rotation.FileSecretSpec) error
	signal  func(ctx context.Context, id, sig string) error
	ready   func(ctx context.Context, id string, timeout time.Duration) (rotation.Readiness, error)
}

func (r *ReloaderController) defaultSignalOps() signalOps {
	return signalOps{
		inspect: func(ctx context.Context, id string) (container.InspectResponse, error) {
			return r.cli.ContainerInspect(ctx, id)
		},
		deliver: func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
			// Live replacement uses the atomic exec-based injector, NOT the tar
			// stream (which is pre-start only). tmpfs is required so plaintext
			// never lands on a host-backed layer or bind mount.
			files := make(map[string]string, len(spec.Data))
			for k, v := range spec.Data {
				files[k] = v
			}
			return injector.InjectFilesWithOptions(ctx, r.cli, id, files, spec.UID, spec.GID,
				injector.Options{Dir: spec.Path, RequireTmpfs: true})
		},
		probe: func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
			return rotation.ExecProbeFiles(ctx, r.cli, id, spec.Path, fileNames(spec.Data), 15*time.Second, 3)
		},
		signal: func(ctx context.Context, id, sig string) error {
			return r.cli.ContainerKill(ctx, id, sig)
		},
		ready: func(ctx context.Context, id string, timeout time.Duration) (rotation.Readiness, error) {
			return rotation.WaitReady(ctx, r.cli, id, timeout)
		},
	}
}

func fileNames(data map[string]string) []string {
	n := make([]string, 0, len(data))
	for k := range data {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// jctx carries the rotation identity used for every journal record of one operation.
type jstate struct {
	rid      string
	strategy string
}

func (r *ReloaderController) newJState(ctx context.Context, strategy string) *jstate {
	rid := rotation.RotationIDFrom(ctx)
	if rid == "" {
		rid = rotation.NewRotationID()
	}
	return &jstate{rid: rid, strategy: strategy}
}

// jIntent persists an INTENT before the guarded action. With no journal it is a
// no-op unless the agent made one mandatory (RequireJournal), in which case it
// refuses. A failed write means the caller must not perform the action.
func (r *ReloaderController) jIntent(ctx context.Context, j *jstate, e rotation.JournalEvent) error {
	if r.Journal == nil {
		if r.RequireJournal {
			return errors.New("the rotation journal is unavailable: refusing a destructive step that could not be recovered after a crash")
		}
		return nil
	}
	e.RotationID, e.OwnerID, e.Strategy = j.rid, r.ownerID(), j.strategy
	if err := r.Journal.Record(ctx, e); err != nil {
		return fmt.Errorf("rotation journal could not record %q, refusing the next step: %w", e.Transition, err)
	}
	return nil
}

// jAck persists an acknowledgement; failure is logged only (the intent is durable).
func (r *ReloaderController) jAck(ctx context.Context, j *jstate, e rotation.JournalEvent) {
	if r.Journal == nil {
		return
	}
	e.RotationID, e.OwnerID, e.Strategy = j.rid, r.ownerID(), j.strategy
	if err := r.Journal.Record(ctx, e); err != nil {
		r.Logger.Warn("rotation journal could not record an acknowledgement; the intent remains durable",
			zap.String("transition", e.Transition), zap.Error(err))
	}
}

// MarkDegradedFromJournal restores, after an agent restart, the degraded flag of a
// service whose interrupted rotation left an uncertain or manual-recovery state.
func (r *ReloaderController) MarkDegradedFromJournal(serviceName, reason string) {
	r.markDegraded(serviceName, reason)
}

// signalMu serialises signal rotations per container so two concurrent
// deliveries can never interleave "deliver, deliver, signal, signal".
var signalMu sync.Map // containerID -> *sync.Mutex

// resolveSignalPlan maps the secrets a target depends on to file specs. It
// rejects env-mode secrets and missing cache entries before anything is changed.
func (r *ReloaderController) resolveSignalPlan(target *TargetContainer) (specs []rotation.FileSecretSpec, sig string, err error) {
	if r.Config == nil || r.Cache == nil {
		return nil, "", errors.New("signal rotation: controller has no configuration or secret cache")
	}
	sig = ""
	var envSecrets []string
	for _, sec := range r.Config.Secrets {
		uses := false
		for _, ts := range target.Secrets {
			if ts == sec.Name {
				uses = true
				break
			}
		}
		if !uses {
			continue
		}
		inj := sec.Inject
		if inj.Type == "" {
			inj = r.Config.Defaults.Inject
		}
		if inj.Type != "file" {
			envSecrets = append(envSecrets, sec.Name)
			continue
		}
		if sec.Rotation.Signal != "" {
			s, serr := normalizeSignal(sec.Rotation.Signal)
			if serr != nil {
				return nil, "", serr
			}
			if sig != "" && sig != s {
				return nil, "", fmt.Errorf("signal rotation: secrets of one container configure different reload signals (%s, %s)", sig, s)
			}
			sig = s
		}
		pName := sec.Provider
		if pName == "" {
			for name := range r.Config.Providers {
				pName = name
				break
			}
		}
		data, found := r.Cache.Get(fmt.Sprintf("%s:%s", pName, sec.Name))
		if !found {
			return nil, "", fmt.Errorf("signal rotation: no cached value for secret %q; nothing delivered, no signal sent", sec.Name)
		}
		if inj.Path == "" {
			return nil, "", fmt.Errorf("signal rotation: secret %q has no inject.path", sec.Name)
		}
		specs = append(specs, rotation.FileSecretSpec{Path: inj.Path, Data: data, UID: inj.UID, GID: inj.GID})
	}
	if len(envSecrets) > 0 {
		sort.Strings(envSecrets)
		return nil, "", fmt.Errorf("%w (env-mode secrets: %s)", ErrSignalEnvSecret, strings.Join(envSecrets, ", "))
	}
	if len(specs) == 0 {
		return nil, "", ErrSignalNoFileSecret
	}
	if sig == "" {
		sig = "SIGHUP"
	}
	return specs, sig, nil
}

// signalRotation runs the full lifecycle for one container whose service lock the
// caller already holds. Order is fixed: ownership -> plan (rejects env-mode,
// missing cache) -> deliver -> probe -> signal -> readiness. The signal is NEVER
// sent unless every file was delivered and probed. The returned error is nil only
// when delivery, probe, signal and readiness all succeeded; even then
// AdoptionVerified is false and Limitation says why.
func (r *ReloaderController) signalRotation(ctx context.Context, ops signalOps, target *TargetContainer, serviceName, secretName string) (SignalOutcome, error) {
	var out SignalOutcome
	out.Limitation = signalLimitation

	mu, _ := signalMu.LoadOrStore(target.ID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	inspect, err := ops.inspect(ctx, target.ID)
	if err != nil {
		return out, fmt.Errorf("signal rotation: inspecting container: %w", err)
	}
	var labels map[string]string
	name := ""
	if inspect.Config != nil {
		labels = inspect.Config.Labels
	}
	name = strings.TrimPrefix(inspect.Name, "/")
	if !r.checkOwnership(target.ID, name, labels) {
		return out, ErrSignalNotOwned
	}
	if inspect.State == nil || !inspect.State.Running {
		return out, errors.New("signal rotation: container is not running")
	}

	specs, sig, err := r.resolveSignalPlan(target)
	if err != nil {
		return out, err
	}

	js := r.newJState(ctx, "signal")
	totalFiles := 0
	for _, sp := range specs {
		totalFiles += len(sp.Data)
	}
	// Durable intent BEFORE any file is replaced. If it cannot be recorded nothing
	// has been published, so this is a clean refusal.
	if err := r.jIntent(ctx, js, rotation.JournalEvent{
		Transition: rotation.TrIntentSignalPublish, ServiceID: serviceName, OriginalIDs: []string{target.ID},
		State:  "publishing",
		Detail: fmt.Sprintf("%d file(s) in %d secret(s); publication may be partial if interrupted", totalFiles, len(specs)),
	}); err != nil {
		return out, &SignalRotationError{State: SignalNothingPublished, Stage: "journal", Cause: err}
	}
	r.fault("signal:after_intent_publish")

	fail := func(state SignalState, stage string, cause error) (SignalOutcome, error) {
		out.State = state
		serr := &SignalRotationError{State: state, Stage: stage, Cause: cause}
		r.jAck(ctx, js, rotation.JournalEvent{Transition: rotation.TrSignalFailed, State: string(state), ManualRecovery: state != SignalNothingPublished, Detail: stage})
		if state != SignalNothingPublished && serviceName != "" {
			r.markDegraded(serviceName, serr.Error())
		}
		return out, serr
	}

	filesBefore := 0
	for specIdx, spec := range specs {
		if err := ops.deliver(ctx, target.ID, spec); err != nil {
			// Delivery is atomic PER FILE. If any file could already have been
			// replaced (an earlier spec, or an earlier file of a multi-file spec),
			// the set is partially published; otherwise the previous value is intact.
			if filesBefore == 0 && len(spec.Data) <= 1 {
				return fail(SignalNothingPublished, "secret delivery", err)
			}
			return fail(SignalPartiallyPublished, "secret delivery", err)
		}
		filesBefore += len(spec.Data)
		r.fault(fmt.Sprintf("signal:after_spec_%d_published", specIdx))
	}
	for _, spec := range specs {
		if err := ops.probe(ctx, target.ID, spec); err != nil {
			return fail(SignalPublishedUnverified, "delivery verification", err)
		}
		out.DeliveredFiles = append(out.DeliveredFiles, fileNames(spec.Data)...)
	}
	out.Delivered = true
	r.fault("signal:after_publish_before_ack")
	r.jAck(ctx, js, rotation.JournalEvent{Transition: rotation.TrSignalPublished, State: "published_reload_not_requested"})

	if err := r.jIntent(ctx, js, rotation.JournalEvent{Transition: rotation.TrIntentSignalReload, State: "reload_pending", Detail: sig}); err != nil {
		return fail(SignalPublishedNotSignalled, "journal before reload signal", err)
	}
	r.fault("signal:after_intent_reload")
	if err := ops.signal(ctx, target.ID, sig); err != nil {
		return fail(SignalPublishedNotSignalled, "reload signal delivery", err)
	}
	out.ReloadRequested = true
	r.fault("signal:after_signal_before_ack")
	r.jAck(ctx, js, rotation.JournalEvent{Transition: rotation.TrSignalReloadSent, State: "signal_sent_outcome_unknown"})

	readyTimeout := 30 * time.Second
	if r.Config != nil && r.Config.Agent.Rotation.HealthCheckTimeout != "" {
		if d, perr := time.ParseDuration(r.Config.Agent.Rotation.HealthCheckTimeout); perr == nil && d > 0 {
			readyTimeout = d
		}
	}
	r.fault("signal:during_readiness")
	ev, err := ops.ready(ctx, target.ID, readyTimeout)
	if err != nil {
		return fail(SignalSentOutcomeUnknown, "post-reload readiness", err)
	}
	out.Readiness = ev
	out.State = SignalReloadRequested
	r.fault("signal:before_final_ack")
	r.jAck(ctx, js, rotation.JournalEvent{Transition: rotation.TrSignalDone, State: string(SignalReloadRequested),
		Detail: "readiness: " + ev.String() + "; application adoption NOT verified"})
	return out, nil
}

// runSignalRotation is the TriggerReload entry point: it runs signalRotation with
// the production ops, audits and logs the REAL outcome, and returns the error to
// record. It never claims adoption.
func (r *ReloaderController) runSignalRotation(ctx context.Context, target *TargetContainer, serviceName, secretName string) error {
	provider := r.resolveSecretProvider(secretName)
	out, err := r.signalRotation(ctx, r.defaultSignalOps(), target, serviceName, secretName)
	if err != nil {
		audit.Log(ctx, "rotate", "system", provider, secretName, target.ID, "failed")
		r.Logger.Error("Signal rotation failed",
			zap.String("id", util.ShortID(target.ID)), zap.Error(err))
		return err
	}
	audit.Log(ctx, "rotate", "system", provider, secretName, target.ID, "reload_requested")
	r.Logger.Info("Signal rotation: files delivered and reload requested; application adoption NOT verified",
		zap.String("id", util.ShortID(target.ID)),
		zap.Strings("files", out.DeliveredFiles),
		zap.String("readiness_evidence", out.Readiness.String()))
	return nil
}
