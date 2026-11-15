// Package ledger is the foundation (S1) of DSO's applied-version ledger: the
// document model, its validation, strict (de)serialization and the canonical
// HMAC identity encoder.
//
// Stage S2 adds the persistence store (store.go and its companions): the
// single-writer durable Update, recovery modes, the KeyProvider boundary, retry
// and health, and generic request and recovery-journal operations. It still
// contains no key storage (S3), no rotation transitions (S4), no reconciliation
// and no logging or metrics (S6); S2 reports through events and snapshots only.
//
// INTEGRATION STATUS: UNWIRED LIBRARY. No production package imports this
// package; nothing in the agent, the trigger engine, the REST server or any CLI
// instantiates a Store. It is integrated on the M1 branch as a library only.
//
// ROTATION LIFECYCLE AND CRASH RECOVERY ARE NOT OWNED HERE. The ledger is not
// an independent authority for either. The following APIs and types are
// UNWIRED - pending reconciliation with the M0 StateTracker/journal (the M0
// journal remains authoritative for rotation lifecycle and crash recovery):
//
//   - the write-ahead rotation intent: Entry.Rotation (*Intent) and the Intent
//     type, together with the entry states that depend on it (EntryInProgress,
//     EntryPartial);
//   - the startup-recovery journal: Ledger.Recoveries, RecoveryRecord,
//     RecoveryScope, RecoveryTrigger (including TriggerStateTracker and
//     TriggerLedgerIntent), RecoveryState, Action, ActionOp, ActionResult,
//     ActionOutcome, and the Store methods BeginRecovery, CompleteRecovery and
//     RecoveryRecords;
//   - the manual redelivery request (Store.RequestRedelivery and the
//     Redelivery* states), which depends on the same unreconciled lifecycle.
//
// No CLI command or API exposes any of these, and none may be exposed until the
// reconciliation with the M0 journal has been designed and approved.
//
// Durability: an update is published in memory only after its durable write
// succeeded, so a failed update never changes the published state. A failed
// update does not necessarily leave the previous document on disk: after a
// failed directory fsync that follows a successful rename, the file may already
// hold the complete candidate, and a restart may find either complete document
// (see Store.Update).
//
//   - the ledger records, per (project, service, provider, secret), the desired
//     and applied version identities, per-replica delivery records, a
//     write-ahead rotation intent, a manual redelivery request and (at the
//     document level) a startup-recovery journal;
//   - a version identity is either a provider-native id or a keyed HMAC-SHA256
//     of the secret. The ledger never stores a plaintext secret value and never
//     an unkeyed hash of one;
//   - a container ID is a replica attribute, never part of an entry's key.
//
// Types that look similar are kept distinct on purpose: a rotation intent is
// not an applied delivery, a manual request is not a desired-state change, and
// a recovery record is not a rotation intent.
package ledger

import "time"

// SchemaVersion is the only document schema version this package reads and
// writes. Version 2 is the format of BL-02 design revision 6.3 (§28, §43): it
// adds the skipped_mismatch recovery-action outcome and relaxes the partial
// state rule. No version-1 ledger exists and no migration from version 1 is
// defined; a version-1 document is rejected as invalid, and a version greater
// than this one is reported as a *SchemaVersionError (never as corruption).
const SchemaVersion = 2

// VersionKind says how a Version identifies a secret value.
type VersionKind string

const (
	// KindProvider is an opaque, provider-native version id.
	KindProvider VersionKind = "provider"
	// KindHMAC is a host-local HMAC-SHA256 identity bound to a key id.
	KindHMAC VersionKind = "hmac"
)

// EntryState is the stored state of an Entry. "Pending work" and
// "awaiting replicas" are derived, never stored.
type EntryState string

const (
	EntryAdopted    EntryState = "adopted"
	EntryIdle       EntryState = "idle"
	EntryPending    EntryState = "pending"
	EntryInProgress EntryState = "in_progress"
	EntryPartial    EntryState = "partial"
	EntryFailed     EntryState = "failed"
	EntryRolledBack EntryState = "rolled_back"
	EntryBlocked    EntryState = "blocked"
)

// ReplicaOutcome is the delivery outcome recorded for one replica.
type ReplicaOutcome string

const (
	ReplicaAdopted    ReplicaOutcome = "adopted"
	ReplicaSucceeded  ReplicaOutcome = "succeeded"
	ReplicaRolledBack ReplicaOutcome = "rolled_back"
	ReplicaFailed     ReplicaOutcome = "failed"
	ReplicaBlocked    ReplicaOutcome = "blocked"
	ReplicaPending    ReplicaOutcome = "pending"
)

// RedeliveryState is the state of a manual redelivery request.
type RedeliveryState string

const (
	RedeliveryRequested  RedeliveryState = "requested"
	RedeliveryDispatched RedeliveryState = "dispatched"
	RedeliverySucceeded  RedeliveryState = "succeeded"
	RedeliveryFailed     RedeliveryState = "failed"
	RedeliverySuperseded RedeliveryState = "superseded"
	RedeliveryCancelled  RedeliveryState = "cancelled"
)

// RecoveryTrigger says why a startup-recovery record was created.
type RecoveryTrigger string

const (
	TriggerStateTracker RecoveryTrigger = "state_tracker"
	TriggerLedgerIntent RecoveryTrigger = "ledger_intent"
	TriggerOrphanScan   RecoveryTrigger = "orphan_scan"
)

// RecoveryState is the state of a startup-recovery record.
type RecoveryState string

const (
	RecoveryPlanned            RecoveryState = "planned"
	RecoveryInProgress         RecoveryState = "in_progress"
	RecoveryDone               RecoveryState = "done"
	RecoveryFailed             RecoveryState = "failed"
	RecoveryBlockedPersistence RecoveryState = "blocked_persistence"
)

// ActionOp is one planned recovery operation.
type ActionOp string

const (
	ActionStop   ActionOp = "stop"
	ActionRemove ActionOp = "remove"
	ActionStart  ActionOp = "start"
)

// ActionOutcome is the result of one executed recovery action.
type ActionOutcome string

const (
	ActionOK          ActionOutcome = "ok"
	ActionAlreadyDone ActionOutcome = "already_done"
	ActionFailedOp    ActionOutcome = "failed"
	// ActionSkippedMismatch records that the planned container no longer matches
	// the plan (id, created, name or owner changed). No Docker mutation is made
	// for such an action. It requires a non-empty reason and counts as a
	// successful-completion result when judging a done record (design §18.1,
	// §18.2).
	ActionSkippedMismatch ActionOutcome = "skipped_mismatch"
)

// Ledger is the whole document.
type Ledger struct {
	SchemaVersion int `json:"schema_version"`
	// LedgerID is a random 128-bit id (32 lowercase hex characters).
	LedgerID string `json:"ledger_id"`
	// Generation is a diagnostic counter increased on every durable write.
	Generation uint64 `json:"generation"`
	// HMACKeyID is the id of the key behind every "hmac" Version in the file.
	HMACKeyID string    `json:"hmac_key_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// BodySHA256 is the SHA-256 of the canonical body (see CanonicalBody). It
	// detects truncation and bit rot of ledger metadata, not tampering. It is
	// computed over identity metadata only, never over a secret value.
	BodySHA256 string           `json:"body_sha256"`
	Entries    []Entry          `json:"entries"`
	Recoveries []RecoveryRecord `json:"recoveries"`
}

// Key identifies one entry. It deliberately contains no container id.
//
// Project is the Compose project ("" for a non-Compose target). Service is the
// Compose service, or the container name for a non-Compose target. Provider is
// the resolved configured provider name and Secret the configured secret name.
type Key struct {
	Project  string `json:"project"`
	Service  string `json:"service"`
	Provider string `json:"provider"`
	Secret   string `json:"secret"`
}

// Version is the identity of one secret value. It never contains the value.
type Version struct {
	Kind VersionKind `json:"kind"`
	// ID is an opaque provider id (KindProvider) or base64url(HMAC-SHA256)
	// without padding (KindHMAC).
	ID string `json:"id"`
	// KeyID is set for KindHMAC only and names the key the HMAC was made with.
	KeyID string `json:"key_id"`
	// Source is a keyed digest of the provider endpoint identity, or "". It is
	// always "" for KindHMAC.
	Source string `json:"source"`
}

// Replica is the per-replica delivery record. ContainerID is a replica
// attribute and a reconciliation signal only.
type Replica struct {
	// Number is the Compose container number ("" for a non-Compose target).
	Number        string `json:"number"`
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	// Created is the container's creation timestamp as reported by Docker.
	Created string         `json:"created"`
	Applied *Version       `json:"applied"`
	Outcome ReplicaOutcome `json:"outcome"`
	Reason  string         `json:"reason"`
	At      time.Time      `json:"at"`
}

// Intent is the write-ahead record of a rotation that has begun and has not
// reached a durable terminal outcome. It is not an applied delivery.
type Intent struct {
	// RotationID is a random 128-bit id (32 lowercase hex characters).
	RotationID string    `json:"rotation_id"`
	StartedAt  time.Time `json:"started_at"`
	// Desired is the version this rotation delivers; it may be older than the
	// entry's Desired when a newer version arrived meanwhile.
	Desired  Version `json:"desired"`
	Strategy string  `json:"strategy"`
	// PriorReplicas are the replicas that existed before the rotation began.
	PriorReplicas []Replica `json:"prior_replicas"`
	// Supersedes is the RotationID of an unresolved earlier intent this one
	// replaces, or "".
	Supersedes string `json:"supersedes"`
}

// Redelivery is a manual redelivery request for the entry's current desired
// version. It is not a change of the desired state.
type Redelivery struct {
	// RequestID is a random 128-bit id (32 lowercase hex characters).
	RequestID   string    `json:"request_id"`
	RequestedAt time.Time `json:"requested_at"`
	// Actor is an authenticated actor name, never a credential.
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
	// TargetSeq is the desired_seq the operator asked to redeliver.
	TargetSeq uint64          `json:"target_seq"`
	State     RedeliveryState `json:"state"`
	// RotationID is the rotation serving the request, once dispatched.
	RotationID string `json:"rotation_id"`
	// SupersededBy is the id of the request or rotation that replaced it.
	SupersededBy string `json:"superseded_by"`
}

// LastOutcome summarizes the latest terminal outcome of an entry. It never
// contains secret material.
type LastOutcome struct {
	Kind     string    `json:"kind"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
	Strategy string    `json:"strategy"`
}

// Entry is the per-secret record for one service: independent desired and
// applied state per (project, service, provider, secret).
type Entry struct {
	Key Key `json:"key"`
	// Desired is the latest observed value identity and is never discarded.
	Desired *Version `json:"desired"`
	// DesiredSeq increases only when Desired changes; 0 while Desired is nil.
	DesiredSeq        uint64    `json:"desired_seq"`
	DesiredObservedAt time.Time `json:"desired_observed_at,omitzero"`
	// Applied is the service-level applied version. It must advance only when
	// every targeted replica succeeded; that rule is enforced by later stages.
	Applied     *Version     `json:"applied"`
	State       EntryState   `json:"state"`
	Replicas    []Replica    `json:"replicas"`
	Rotation    *Intent      `json:"rotation"`
	Request     *Redelivery  `json:"request"`
	LastOutcome *LastOutcome `json:"last_outcome"`
}

// RecoveryScope names what a recovery record is about: a service group, or an
// artifact container by name when no group applies.
type RecoveryScope struct {
	Project       string `json:"project"`
	Service       string `json:"service"`
	ContainerName string `json:"container_name"`
}

// Action is one recovery operation, fixed before any Docker mutation.
type Action struct {
	Op            ActionOp `json:"op"`
	ContainerID   string   `json:"container_id"`
	ContainerName string   `json:"container_name"`
	Created       string   `json:"created"`
}

// ActionResult is the outcome of the planned action at the same index.
type ActionResult struct {
	Outcome ActionOutcome `json:"outcome"`
	Reason  string        `json:"reason"`
}

// RecoveryRecord is one startup-recovery journal record. It is separate from
// a rotation Intent and never describes secret delivery.
type RecoveryRecord struct {
	// RecoveryID is a random 128-bit id (32 lowercase hex characters).
	RecoveryID string          `json:"recovery_id"`
	Scope      RecoveryScope   `json:"scope"`
	Trigger    RecoveryTrigger `json:"trigger"`
	Planned    []Action        `json:"planned"`
	Done       []ActionResult  `json:"done"`
	State      RecoveryState   `json:"state"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at,omitzero"`
}
