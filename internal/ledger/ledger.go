// Package ledger is DSO's applied-version ledger: the document model, its
// validation, strict (de)serialization, the canonical HMAC identity encoder
// (S1), the persistence store (S2), host-local key management (S3) and the
// narrow transition facade (S4a, transitions*.go).
//
// INTEGRATION STATUS: UNWIRED LIBRARY. No production package imports this
// package; nothing in the agent, the trigger engine, the REST server or any CLI
// instantiates a Store. The only intended future importer is the bridge file
// internal/agent/ledger_bridge.go (see TestRepositoryReferenceGuard).
//
// ROTATION LIFECYCLE AND CRASH RECOVERY ARE NOT OWNED HERE. The M0 journal
// (StateTracker, journalGuard, AutomaticRecovery) is the only authority for
// rotation operation identity, lifecycle, pending intent, recovery and
// blocking (docs/design/m1-ledger-reconciliation.md section 0). The ledger
// records verified facts the journal has established: the observed desired
// version, the last version whose delivery the journal verified, and per
// replica delivery and adoption records. It never starts, authorises, gates or
// sequences a Docker mutation and never calls or bypasses the operation guard
// (invariant X1). Schema version 3 therefore has no rotation intent, no entry
// in_progress state and no recovery journal.
//
// Production code mutates the document only through the Versions facade. The
// whole-document store primitives are unexported (Store.update,
// Store.updateMemoryOnly) and a repository guard test enforces that.
//
// UNWIRED but kept: the manual redelivery request (Store.RequestRedelivery and
// the Redelivery* states), the advisory Store.Gate, and EntryPartial (not
// produced by the v1 facade: the journal cannot attribute per-replica outcomes).
//
// Durability: an update is published in memory only after its durable write
// succeeded, so a failed update never changes the published state. A failed
// update does not necessarily leave the previous document on disk: after a
// failed directory fsync that follows a successful rename, the file may already
// hold the complete candidate, and a restart may find either complete document
// (see Store.update).
//
//   - the ledger records, per (project, service, provider, secret), the desired
//     and applied version identities and per-replica delivery records;
//   - a version identity is either a provider-native id or a keyed HMAC-SHA256
//     of the secret. The ledger never stores a plaintext secret value and never
//     an unkeyed hash of one;
//   - a container ID is a replica attribute, never part of an entry's key.
//
// Types that look similar are kept distinct on purpose: a delivery request is
// not a verified delivery, a delivery is not adoption by the application, and
// a manual request is not a desired-state change.
package ledger

import "time"

// SchemaVersion is the only document schema version this package reads and
// writes. Version 3 (S2.2, docs/design/m1-ledger-reconciliation.md section
// 1.3) adds per-replica rotation_id, delivery and adoption and removes the
// rotation intent, the in_progress entry state and the recovery journal
// (the M0 journal is the only rotation authority). No version-1 or version-2
// ledger exists on disk and no migration is defined: a document older than
// this version is rejected as invalid (the store then preserves it aside and
// starts a fresh, all-UNKNOWN ledger), and a version greater than this one is
// reported as a *SchemaVersionError (never as corruption; the file is left
// untouched and the store runs memory-only).
const SchemaVersion = 3

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
	// EntryAdopted: observed, applied UNKNOWN (applied is nil).
	EntryAdopted EntryState = "adopted"
	EntryIdle    EntryState = "idle"
	EntryPending EntryState = "pending"
	// EntryPartial is valid in the schema but not produced by the v1 facade.
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

// DeliveryState is the per-replica delivery record (schema v3).
type DeliveryState string

const (
	// DeliveryNone: nothing is claimed about delivery.
	DeliveryNone DeliveryState = "none"
	// DeliveryRequested is a correlation hint written after the journal
	// recorded the operation. It never proves delivery.
	DeliveryRequested DeliveryState = "requested"
	// DeliveryVerified requires journal evidence (design section 4.2).
	DeliveryVerified DeliveryState = "verified"
)

// AdoptionState says what is known about the application holding a version.
type AdoptionState string

const (
	// AdoptionUnknown: existing workload or interrupted operation.
	AdoptionUnknown AdoptionState = "unknown"
	// AdoptionUnverified: delivered, but nothing proves the application loaded
	// it (signal reload, file-mode secrets while N19 is open).
	AdoptionUnverified AdoptionState = "unverified"
	// AdoptionVerified: a fresh process was started with the version.
	AdoptionVerified AdoptionState = "verified"
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
	BodySHA256 string  `json:"body_sha256"`
	Entries    []Entry `json:"entries"`
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
	Created string `json:"created"`
	// Applied is the last version whose delivery the journal verified for this
	// replica (nil: unknown). It is never set at adoption.
	Applied *Version       `json:"applied"`
	Outcome ReplicaOutcome `json:"outcome"`
	// Reason is one of the closed OutcomeReason values, or "".
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
	// RotationID is "" or the M0 journal operation id that last wrote this
	// record (1 to 64 characters of [A-Za-z0-9_-]). The ledger never mints one.
	RotationID string `json:"rotation_id"`
	// Delivery is none, requested (a correlation hint, subordinate to the
	// journal) or verified (requires journal evidence).
	Delivery DeliveryState `json:"delivery"`
	// Adoption says whether the application is known to hold the version:
	// unknown, unverified (delivered, nothing proves it was loaded) or verified
	// (a fresh process was started with the version).
	Adoption AdoptionState `json:"adoption"`
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
	// Applied is the service-level applied version: the last version whose
	// delivery the journal verified for every targeted replica (nil: unknown).
	// It advances only through Versions.RecordDeliveryVerified / RecordApplied.
	Applied     *Version     `json:"applied"`
	State       EntryState   `json:"state"`
	Replicas    []Replica    `json:"replicas"`
	Request     *Redelivery  `json:"request"`
	LastOutcome *LastOutcome `json:"last_outcome"`
}
