package rotation

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"

	"github.com/docker-secret-operator/dso/internal/injector"
)

// Managed delivery lifecycle for a GATED candidate (docs/design/n19-managed-delivery.md):
//
//	created -> started (gate armed, application not running)
//	  -> VERIFY identity + mounts -> gate_started
//	  -> intent_deliver_secrets -> inject every file (hardened exec injector)
//	  -> probe every file -> secrets_delivered (keyed versions) [durable BEFORE release]
//	  -> intent_release_gate -> write release flag -> gate_released
//
// The caller then waits for application readiness and records candidate_ready. No
// phase is interchangeable with another: only secrets_delivered is delivery evidence,
// only gate_released lets the application run, only candidate_ready (after both) is a
// verified candidate.

// DeliveryError is a failed managed delivery. Partial is true when at least one file
// is known to have been published. Phase names the step that failed.
type DeliveryError struct {
	Phase   string
	Partial bool
	Err     error
}

func (e *DeliveryError) Error() string {
	p := ""
	if e.Partial {
		p = " (partially delivered)"
	}
	return fmt.Sprintf("managed file delivery failed at %s%s: %v", e.Phase, p, e.Err)
}

func (e *DeliveryError) Unwrap() error { return e.Err }

// DeliveryOps are the side effects of delivery, injectable so the lifecycle can be
// exercised without a Docker daemon.
type DeliveryOps struct {
	Inspect func(ctx context.Context, id string) (container.InspectResponse, error)
	// Inject publishes every file of spec atomically (RequireTmpfs).
	Inject func(ctx context.Context, id string, spec FileSecretSpec) error
	// Probe verifies every file of spec exists and is non-empty.
	Probe func(ctx context.Context, id string, spec FileSecretSpec) error
	// Release writes the gate release flag into dir.
	Release func(ctx context.Context, id, dir string, uid, gid int) error
}

// DefaultDeliveryOps are the real operations: the hardened exec injector (atomic
// write, tmpfs required, container user) and the existing file probe.
func DefaultDeliveryOps(cli *client.Client) DeliveryOps {
	return DeliveryOps{
		Inspect: func(ctx context.Context, id string) (container.InspectResponse, error) {
			return cli.ContainerInspect(ctx, id)
		},
		Inject: func(ctx context.Context, id string, spec FileSecretSpec) error {
			files := make(map[string]string, len(spec.Data))
			for k, v := range spec.Data {
				files[k] = v
			}
			return injector.InjectFilesWithOptions(ctx, cli, id, files, spec.UID, spec.GID,
				injector.Options{Dir: spec.Path, RequireTmpfs: true})
		},
		Probe: func(ctx context.Context, id string, spec FileSecretSpec) error {
			return ExecProbeFiles(ctx, cli, id, spec.Path, secretFileNames(spec.Data), 15*time.Second, 3)
		},
		Release: func(ctx context.Context, id, dir string, uid, gid int) error {
			return injector.InjectFilesWithOptions(ctx, cli, id, map[string]string{GateFlagName: "released\n"}, uid, gid,
				injector.Options{Dir: dir, RequireTmpfs: true})
		},
	}
}

// Delivery runs the managed delivery lifecycle for one candidate.
type Delivery struct {
	Ops DeliveryOps
	// Intent persists a transition BEFORE the guarded action; an error aborts it.
	Intent func(ctx context.Context, e JournalEvent) error
	// Ack persists an informational/acknowledging transition (failure is logged by the
	// owner of the journal; the preceding intent is already durable).
	Ack func(ctx context.Context, e JournalEvent)
	// Fault is the test fault hook (named crash points); may be nil.
	Fault func(point string)
	// RotationID, when set, must equal the candidate's dso.rotation.id label.
	RotationID string
}

// Evidence is what a successful delivery proved.
type Evidence struct {
	Files    []string // delivered file paths (no content)
	Versions []string // keyed version identities of the delivered values
}

func (d *Delivery) fault(p string) {
	if d.Fault != nil {
		d.Fault(p)
	}
}

func (d *Delivery) intent(ctx context.Context, e JournalEvent) error {
	if d.Intent == nil {
		return nil
	}
	return d.Intent(ctx, e)
}

func (d *Delivery) ack(ctx context.Context, e JournalEvent) {
	if d.Ack != nil {
		d.Ack(ctx, e)
	}
}

// VerifyRunningGate inspects the started candidate and proves it is the container the
// operation created, running the gate (not the application), with a tmpfs covering
// every destination. A mismatch refuses delivery: no secret byte is written.
func VerifyRunningGate(insp container.InspectResponse, id, rotationID string, specs []FileSecretSpec) error {
	if insp.ContainerJSONBase == nil || insp.Config == nil || insp.State == nil {
		return fmt.Errorf("%w: container inspection is incomplete", ErrDeliveryRefused)
	}
	if insp.ID != id && (len(id) < 12 || !strings.HasPrefix(insp.ID, id)) {
		return fmt.Errorf("%w: container identity mismatch (expected %.12s, inspected %.12s)", ErrDeliveryRefused, id, insp.ID)
	}
	if rotationID != "" && insp.Config.Labels[LabelRotationID] != rotationID {
		return fmt.Errorf("%w: container does not carry this operation's rotation id", ErrDeliveryRefused)
	}
	if insp.Config.Labels[GateLabel] == "" || !IsGated(insp.Config) {
		return fmt.Errorf("%w: container is not gated (the application could already be running)", ErrDeliveryRefused)
	}
	if insp.Path != gateShell {
		return fmt.Errorf("%w: container process is %q, not the gate shell", ErrDeliveryRefused, insp.Path)
	}
	if !insp.State.Running || insp.State.Restarting {
		return fmt.Errorf("%w: container is not running (gate exited or restarting)", ErrDeliveryRefused)
	}
	if want := GateFlagPath(specs); len(insp.Config.Entrypoint) > gatePrefixLen+1 && insp.Config.Entrypoint[gatePrefixLen+1] != want {
		return fmt.Errorf("%w: the gate waits for a different release flag than this delivery will write", ErrDeliveryRefused)
	}
	// Every file the gate waits for must be one this delivery writes: a gate armed for a
	// different file set (configuration changed since the container was created) would
	// never open, or open on files this delivery did not verify.
	writes := map[string]bool{}
	for _, f := range gateFiles(specs) {
		writes[f] = true
	}
	for _, f := range gateExpectedFiles(insp.Config.Entrypoint) {
		if !writes[f] {
			return fmt.Errorf("%w: the gate waits for %s, which this delivery does not write (the container was armed for a different secret file set)", ErrDeliveryRefused, f)
		}
	}
	for _, dir := range specDirs(specs) {
		best, bestType, bestRW := -1, mount.Type(""), true
		for _, m := range insp.Mounts {
			if covers(path.Clean(m.Destination), dir) && len(m.Destination) > best {
				best, bestType, bestRW = len(m.Destination), m.Type, m.RW
			}
		}
		if best < 0 {
			// Not listed as a mount: accept only a declared tmpfs with no more specific
			// mount; otherwise the destination would be the writable layer.
			if insp.HostConfig == nil || VerifyDeclaredTmpfs(insp.Config, insp.HostConfig, []FileSecretSpec{{Path: dir, Data: map[string]string{"x": "x"}}}) != nil {
				return fmt.Errorf("%w: %s is not on a mount of the running container", ErrDeliveryRefused, dir)
			}
			continue
		}
		if bestType != mount.TypeTmpfs {
			return fmt.Errorf("%w: %s is on a %s mount of the running container, not a tmpfs", ErrDeliveryRefused, dir, bestType)
		}
		if !bestRW {
			return fmt.Errorf("%w: the tmpfs at %s is read-only", ErrDeliveryRefused, dir)
		}
	}
	return nil
}

// Execute delivers specs into the started, gated candidate id and releases the gate.
// On success the application may start; the caller must still verify readiness. On
// any error the gate is still closed (or was never opened by DSO): the caller discards
// the candidate. Journal order: gate_started, intent_deliver_secrets, [delivery_partial],
// secrets_delivered, intent_release_gate, gate_released.
func (d *Delivery) Execute(ctx context.Context, id string, specs []FileSecretSpec) (*Evidence, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: nothing to deliver", ErrDeliveryRefused)
	}
	insp, err := d.Ops.Inspect(ctx, id)
	if err != nil {
		return nil, &DeliveryError{Phase: "inspect", Err: err}
	}
	if err := VerifyRunningGate(insp, id, d.RotationID, specs); err != nil {
		return nil, &DeliveryError{Phase: "verify_gate", Err: err}
	}
	d.ack(ctx, JournalEvent{Transition: TrGateStarted, CandidateIDs: []string{id}, Detail: "candidate running the startup gate; application not started"})
	d.fault("after_gate_started")

	if err := d.intent(ctx, JournalEvent{Transition: TrIntentDeliverSecrets, CandidateIDs: []string{id}, Detail: fmt.Sprintf("%d secret director(ies)", len(specDirs(specs)))}); err != nil {
		return nil, &DeliveryError{Phase: "journal", Err: err}
	}
	d.fault("after_intent_deliver")

	ev := &Evidence{}
	delivered := 0
	for _, spec := range specs {
		if err := d.Ops.Inject(ctx, id, spec); err != nil {
			partial := delivered > 0
			if partial {
				d.ack(ctx, JournalEvent{Transition: TrDeliveryPartial, CandidateIDs: []string{id}, State: "delivery_partial",
					Detail: fmt.Sprintf("%d of %d secret director(ies) delivered before the failure", delivered, len(specs))})
			}
			return nil, &DeliveryError{Phase: "inject", Partial: partial, Err: err}
		}
		delivered++
		d.fault("after_spec_delivered")
	}
	d.fault("after_delivery_before_probe")
	for _, spec := range specs {
		if err := d.Ops.Probe(ctx, id, spec); err != nil {
			return nil, &DeliveryError{Phase: "probe", Partial: true, Err: err}
		}
		for _, n := range secretFileNames(spec.Data) {
			ev.Files = append(ev.Files, path.Join(spec.Path, n))
		}
		if spec.Version != "" {
			ev.Versions = append(ev.Versions, spec.Version)
		}
	}
	d.fault("after_probe_before_ack")

	// Delivery evidence is durable BEFORE the gate may be released: a failed write
	// aborts here and the application never starts.
	if err := d.intent(ctx, JournalEvent{Transition: TrSecretsDelivered, CandidateIDs: []string{id}, DeliveredVersions: ev.Versions,
		Detail: fmt.Sprintf("%d file(s) delivered and probed; names and values are not journalled", len(ev.Files))}); err != nil {
		return nil, &DeliveryError{Phase: "journal", Partial: true, Err: err}
	}
	d.fault("after_secrets_delivered")

	if err := d.intent(ctx, JournalEvent{Transition: TrIntentReleaseGate, CandidateIDs: []string{id}}); err != nil {
		return nil, &DeliveryError{Phase: "journal", Partial: true, Err: err}
	}
	d.fault("after_intent_release")
	dir := specDirs(specs)[0]
	owner := specs[0]
	for _, s := range specs {
		if s.Path == dir {
			owner = s
			break
		}
	}
	if err := d.Ops.Release(ctx, id, dir, owner.UID, owner.GID); err != nil {
		return nil, &DeliveryError{Phase: "release", Partial: true, Err: err}
	}
	d.fault("after_release_before_ack")
	d.ack(ctx, JournalEvent{Transition: TrGateReleased, CandidateIDs: []string{id}})
	d.fault("after_gate_released")
	return ev, nil
}

// IsDeliveryRefusal reports whether err is a deterministic refusal (nothing was
// changed and no strategy fallback can help).
func IsDeliveryRefusal(err error) bool {
	return errors.Is(err, ErrDeliveryRefused)
}
