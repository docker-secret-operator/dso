package ledger

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// ErrInvalid is wrapped by every validation and decoding failure of a ledger
// document. Callers classify a document as corrupt with errors.Is(err,
// ErrInvalid). Messages name the field path and never echo secret material.
var ErrInvalid = errors.New("ledger: invalid document")

// invalid builds an ErrInvalid-wrapping error for the field at path.
func invalid(path, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, path, fmt.Sprintf(format, args...))
}

// quote renders an untrusted string for an error message, shortened.
func quote(s string) string {
	const max = 40
	if len(s) > max {
		return fmt.Sprintf("%q...", s[:max])
	}
	return fmt.Sprintf("%q", s)
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isID(s string) bool { return isHex(s, 32) }

// hasControl reports whether s contains a control character or is not valid
// UTF-8.
func hasControl(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func isUTC(t time.Time) bool {
	_, off := t.Zone()
	return off == 0
}

func checkTime(path string, t time.Time, required bool) error {
	if t.IsZero() {
		if required {
			return invalid(path, "timestamp is required")
		}
		return nil
	}
	if !isUTC(t) {
		return invalid(path, "timestamp must be UTC")
	}
	return nil
}

func checkText(path, s string, required bool) error {
	if s == "" {
		if required {
			return invalid(path, "must not be empty")
		}
		return nil
	}
	if hasControl(s) {
		return invalid(path, "must not contain control characters")
	}
	return nil
}

// Validate checks the whole document. It reports the first problem found.
//
// Beyond field shapes it enforces the structural invariants that follow from
// the schema: unique entry keys, every HMAC identity bound to the ledger's
// hmac_key_id, desired/seq consistency, the state/intent/replica relations of
// the state table, and the recovery-record rules. It does not know a previous
// document, so "applied advances only when every replica succeeded" is a rule
// for the later stages that perform transitions, not something checkable here.
func (l *Ledger) Validate() error {
	if l == nil {
		return invalid("ledger", "document is nil")
	}
	if l.SchemaVersion != SchemaVersion {
		return invalid("schema_version", "unsupported schema version %d (this package reads %d)", l.SchemaVersion, SchemaVersion)
	}
	if !isID(l.LedgerID) {
		return invalid("ledger_id", "must be 32 lowercase hex characters")
	}
	if l.HMACKeyID != "" && !ValidKeyID(l.HMACKeyID) {
		return invalid("hmac_key_id", "must be empty or of the form k-<16 hex>")
	}
	if err := checkTime("created_at", l.CreatedAt, true); err != nil {
		return err
	}
	if err := checkTime("updated_at", l.UpdatedAt, true); err != nil {
		return err
	}
	if l.BodySHA256 != "" && !isHex(l.BodySHA256, 64) {
		return invalid("body_sha256", "must be empty or 64 lowercase hex characters")
	}

	seen := make(map[Key]int, len(l.Entries))
	for i := range l.Entries {
		path := fmt.Sprintf("entries[%d]", i)
		e := &l.Entries[i]
		if prev, dup := seen[e.Key]; dup {
			return invalid(path+".key", "duplicate of entries[%d] (project, service, provider and secret must be unique)", prev)
		}
		seen[e.Key] = i
		if err := e.validate(path, l.HMACKeyID); err != nil {
			return err
		}
	}

	ids := make(map[string]struct{}, len(l.Recoveries))
	open := make(map[RecoveryScope]int)
	for i := range l.Recoveries {
		path := fmt.Sprintf("recoveries[%d]", i)
		r := &l.Recoveries[i]
		if err := r.validate(path); err != nil {
			return err
		}
		if _, dup := ids[r.RecoveryID]; dup {
			return invalid(path+".recovery_id", "duplicate recovery id")
		}
		ids[r.RecoveryID] = struct{}{}
		if r.State == RecoveryPlanned || r.State == RecoveryInProgress {
			if prev, dup := open[r.Scope]; dup {
				return invalid(path+".scope", "second non-terminal record for the same scope (recoveries[%d] is already open)", prev)
			}
			open[r.Scope] = i
		}
	}
	return nil
}

func (k Key) validate(path string) error {
	if err := checkText(path+".project", k.Project, false); err != nil {
		return err
	}
	if err := checkText(path+".service", k.Service, true); err != nil {
		return err
	}
	if err := checkText(path+".provider", k.Provider, true); err != nil {
		return err
	}
	return checkText(path+".secret", k.Secret, true)
}

func validEntryState(s EntryState) bool {
	switch s {
	case EntryAdopted, EntryIdle, EntryPending, EntryInProgress, EntryPartial,
		EntryFailed, EntryRolledBack, EntryBlocked:
		return true
	}
	return false
}

func (e *Entry) validate(path, keyID string) error {
	if err := e.Key.validate(path + ".key"); err != nil {
		return err
	}

	if e.Desired == nil {
		if e.DesiredSeq != 0 {
			return invalid(path+".desired_seq", "must be 0 while desired is null")
		}
		if !e.DesiredObservedAt.IsZero() {
			return invalid(path+".desired_observed_at", "must be absent while desired is null")
		}
	} else {
		if err := e.Desired.validate(path+".desired", keyID); err != nil {
			return err
		}
		if e.DesiredSeq < 1 {
			return invalid(path+".desired_seq", "must be at least 1 once desired is set")
		}
		if err := checkTime(path+".desired_observed_at", e.DesiredObservedAt, false); err != nil {
			return err
		}
	}
	if e.Applied != nil {
		if err := e.Applied.validate(path+".applied", keyID); err != nil {
			return err
		}
	}
	if !validEntryState(e.State) {
		return invalid(path+".state", "unknown entry state %s", quote(string(e.State)))
	}

	replicaIDs := make(map[string]struct{}, len(e.Replicas))
	succeeded, notDelivered := 0, 0
	for i := range e.Replicas {
		rp := fmt.Sprintf("%s.replicas[%d]", path, i)
		r := &e.Replicas[i]
		if err := r.validate(rp, keyID); err != nil {
			return err
		}
		if _, dup := replicaIDs[r.ContainerID]; dup {
			return invalid(rp+".container_id", "duplicate container id within the entry")
		}
		replicaIDs[r.ContainerID] = struct{}{}
		switch r.Outcome {
		case ReplicaSucceeded:
			succeeded++
		case ReplicaFailed, ReplicaRolledBack, ReplicaPending:
			// Targeted replicas that did not receive the version. blocked and
			// adopted replicas are non-targeted: they neither satisfy nor
			// prevent a partial entry (design §27.3).
			notDelivered++
		}
		if e.State == EntryAdopted && r.Outcome != ReplicaAdopted {
			return invalid(rp+".outcome", "every replica of an adopted entry must have outcome adopted")
		}
	}

	if e.Rotation != nil {
		if err := e.Rotation.validate(path+".rotation", keyID); err != nil {
			return err
		}
	}
	if (e.State == EntryInProgress) != (e.Rotation != nil) {
		return invalid(path+".state", "state in_progress and a rotation intent must be present together")
	}
	if e.State == EntryPartial && (succeeded == 0 || notDelivered == 0) {
		return invalid(path+".state", "a partial entry needs at least one succeeded replica and one failed, rolled_back or pending replica")
	}

	if e.Request != nil {
		if e.Desired == nil {
			return invalid(path+".request", "a redelivery request needs a desired version")
		}
		if err := e.Request.validate(path+".request", e.DesiredSeq); err != nil {
			return err
		}
	}
	if e.LastOutcome != nil {
		if err := e.LastOutcome.validate(path + ".last_outcome"); err != nil {
			return err
		}
	}
	return nil
}

func validReplicaOutcome(o ReplicaOutcome) bool {
	switch o {
	case ReplicaAdopted, ReplicaSucceeded, ReplicaRolledBack, ReplicaFailed, ReplicaBlocked, ReplicaPending:
		return true
	}
	return false
}

func (r *Replica) validate(path, keyID string) error {
	if r.Number != "" {
		for i := 0; i < len(r.Number); i++ {
			if r.Number[i] < '0' || r.Number[i] > '9' {
				return invalid(path+".number", "must be empty or a decimal container number")
			}
		}
	}
	if err := checkText(path+".container_id", r.ContainerID, true); err != nil {
		return err
	}
	if err := checkText(path+".container_name", r.ContainerName, true); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, r.Created); err != nil {
		return invalid(path+".created", "must be an RFC 3339 timestamp")
	}
	if r.Applied != nil {
		if err := r.Applied.validate(path+".applied", keyID); err != nil {
			return err
		}
	}
	if !validReplicaOutcome(r.Outcome) {
		return invalid(path+".outcome", "unknown replica outcome %s", quote(string(r.Outcome)))
	}
	if (r.Outcome == ReplicaSucceeded || r.Outcome == ReplicaAdopted) && r.Applied == nil {
		return invalid(path+".applied", "a %s replica must record the version it holds", r.Outcome)
	}
	if err := checkText(path+".reason", r.Reason, false); err != nil {
		return err
	}
	return checkTime(path+".at", r.At, true)
}

func (in *Intent) validate(path, keyID string) error {
	if !isID(in.RotationID) {
		return invalid(path+".rotation_id", "must be 32 lowercase hex characters")
	}
	if err := checkTime(path+".started_at", in.StartedAt, true); err != nil {
		return err
	}
	if err := in.Desired.validate(path+".desired", keyID); err != nil {
		return err
	}
	if err := checkText(path+".strategy", in.Strategy, true); err != nil {
		return err
	}
	ids := make(map[string]struct{}, len(in.PriorReplicas))
	for i := range in.PriorReplicas {
		rp := fmt.Sprintf("%s.prior_replicas[%d]", path, i)
		r := &in.PriorReplicas[i]
		if err := r.validate(rp, keyID); err != nil {
			return err
		}
		if _, dup := ids[r.ContainerID]; dup {
			return invalid(rp+".container_id", "duplicate container id within the intent")
		}
		ids[r.ContainerID] = struct{}{}
	}
	if in.Supersedes != "" {
		if !isID(in.Supersedes) {
			return invalid(path+".supersedes", "must be empty or 32 lowercase hex characters")
		}
		if in.Supersedes == in.RotationID {
			return invalid(path+".supersedes", "an intent cannot supersede itself")
		}
	}
	return nil
}

func validRedeliveryState(s RedeliveryState) bool {
	switch s {
	case RedeliveryRequested, RedeliveryDispatched, RedeliverySucceeded,
		RedeliveryFailed, RedeliverySuperseded, RedeliveryCancelled:
		return true
	}
	return false
}

func (r *Redelivery) validate(path string, desiredSeq uint64) error {
	if !isID(r.RequestID) {
		return invalid(path+".request_id", "must be 32 lowercase hex characters")
	}
	if err := checkTime(path+".requested_at", r.RequestedAt, true); err != nil {
		return err
	}
	if err := checkText(path+".actor", r.Actor, true); err != nil {
		return err
	}
	if err := checkText(path+".reason", r.Reason, true); err != nil {
		return err
	}
	if r.TargetSeq == 0 || r.TargetSeq > desiredSeq {
		return invalid(path+".target_seq", "must be between 1 and the entry's desired_seq")
	}
	if !validRedeliveryState(r.State) {
		return invalid(path+".state", "unknown redelivery state %s", quote(string(r.State)))
	}
	if r.RotationID != "" && !isID(r.RotationID) {
		return invalid(path+".rotation_id", "must be empty or 32 lowercase hex characters")
	}
	if r.SupersededBy != "" && !isID(r.SupersededBy) {
		return invalid(path+".superseded_by", "must be empty or 32 lowercase hex characters")
	}
	if (r.State == RedeliveryDispatched || r.State == RedeliverySucceeded) && r.RotationID == "" {
		return invalid(path+".rotation_id", "a %s request must name the rotation serving it", r.State)
	}
	if r.State == RedeliverySuperseded && r.SupersededBy == "" {
		return invalid(path+".superseded_by", "a superseded request must say what replaced it")
	}
	return nil
}

func (o *LastOutcome) validate(path string) error {
	if err := checkText(path+".kind", o.Kind, true); err != nil {
		return err
	}
	if err := checkText(path+".reason", o.Reason, false); err != nil {
		return err
	}
	if err := checkTime(path+".at", o.At, true); err != nil {
		return err
	}
	return checkText(path+".strategy", o.Strategy, false)
}

func validRecoveryState(s RecoveryState) bool {
	switch s {
	case RecoveryPlanned, RecoveryInProgress, RecoveryDone, RecoveryFailed, RecoveryBlockedPersistence:
		return true
	}
	return false
}

func validTrigger(t RecoveryTrigger) bool {
	switch t {
	case TriggerStateTracker, TriggerLedgerIntent, TriggerOrphanScan:
		return true
	}
	return false
}

func validOp(o ActionOp) bool {
	switch o {
	case ActionStop, ActionRemove, ActionStart:
		return true
	}
	return false
}

func validActionOutcome(o ActionOutcome) bool {
	switch o {
	case ActionOK, ActionAlreadyDone, ActionFailedOp, ActionSkippedMismatch:
		return true
	}
	return false
}

func (r *RecoveryRecord) validate(path string) error {
	if !isID(r.RecoveryID) {
		return invalid(path+".recovery_id", "must be 32 lowercase hex characters")
	}
	if err := r.Scope.validate(path + ".scope"); err != nil {
		return err
	}
	if !validTrigger(r.Trigger) {
		return invalid(path+".trigger", "unknown recovery trigger %s", quote(string(r.Trigger)))
	}
	if !validRecoveryState(r.State) {
		return invalid(path+".state", "unknown recovery state %s", quote(string(r.State)))
	}
	for i := range r.Planned {
		ap := fmt.Sprintf("%s.planned[%d]", path, i)
		a := &r.Planned[i]
		if !validOp(a.Op) {
			return invalid(ap+".op", "unknown action %s", quote(string(a.Op)))
		}
		if err := checkText(ap+".container_id", a.ContainerID, true); err != nil {
			return err
		}
		if err := checkText(ap+".container_name", a.ContainerName, true); err != nil {
			return err
		}
		if _, err := time.Parse(time.RFC3339Nano, a.Created); err != nil {
			return invalid(ap+".created", "must be an RFC 3339 timestamp")
		}
	}
	if len(r.Done) > len(r.Planned) {
		return invalid(path+".done", "more results than planned actions")
	}
	allOK := true
	for i := range r.Done {
		dp := fmt.Sprintf("%s.done[%d]", path, i)
		d := &r.Done[i]
		if !validActionOutcome(d.Outcome) {
			return invalid(dp+".outcome", "unknown action outcome %s", quote(string(d.Outcome)))
		}
		// skipped_mismatch names the mismatching attribute: the reason is
		// required (design §18.1).
		if err := checkText(dp+".reason", d.Reason, d.Outcome == ActionSkippedMismatch); err != nil {
			return err
		}
		if d.Outcome == ActionFailedOp {
			allOK = false
			// Execution is sequential and halts at the first failed action, so
			// a failed result can only be the last element of the prefix and a
			// retry overwrites it in place (design §18.4 rule 3, §18.5).
			if i != len(r.Done)-1 {
				return invalid(dp+".outcome", "a failed result may only be the last result of a recovery record")
			}
		}
	}
	if r.State == RecoveryPlanned && len(r.Done) != 0 {
		return invalid(path+".done", "a planned record has no results yet")
	}
	if r.State == RecoveryDone && (len(r.Done) != len(r.Planned) || !allOK) {
		return invalid(path+".state", "a done record needs a successful result for every planned action")
	}
	if err := checkTime(path+".started_at", r.StartedAt, true); err != nil {
		return err
	}
	terminal := r.State == RecoveryDone || r.State == RecoveryFailed
	if err := checkTime(path+".finished_at", r.FinishedAt, terminal); err != nil {
		return err
	}
	if !terminal && !r.FinishedAt.IsZero() {
		return invalid(path+".finished_at", "only a done or failed record has a finish time")
	}
	return nil
}

func (s RecoveryScope) validate(path string) error {
	group := s.Service != ""
	artifact := s.ContainerName != ""
	switch {
	case group == artifact:
		return invalid(path, "a scope names either a service group or an artifact container, not both and not neither")
	case group:
		if err := checkText(path+".project", s.Project, false); err != nil {
			return err
		}
		return checkText(path+".service", s.Service, true)
	default:
		if s.Project != "" {
			return invalid(path+".project", "an artifact scope has no project")
		}
		return checkText(path+".container_name", s.ContainerName, true)
	}
}
