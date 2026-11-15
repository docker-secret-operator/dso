package ledger

import (
	"errors"
	"io"
	"time"
)

// This file holds the public vocabulary of the S2 persistence store: errors,
// modes, key-provider boundary, events and health (design §7, §8; owner
// decisions D-S2-1, D-S2-6, D-S2-7).

// MaxFileSize is the largest ledger file the store reads or writes (16 MiB,
// owner decision M-S2-1).
const MaxFileSize = 16 << 20

// fileSizeLimit is the limit actually applied; tests lower it to exercise the
// write-side refusal without building a 16 MiB document.
var fileSizeLimit int64 = MaxFileSize

var (
	// ErrPersistenceUnavailable is returned by a durable update in a memory-only
	// mode and by operations that need healthy durable persistence (Unblock).
	// It never stops dispatch: the ledger is advisory (owner decision O-3).
	ErrPersistenceUnavailable = errors.New("ledger: persistence unavailable")
	// ErrPersistenceFailed wraps the reason a durable write did not complete.
	ErrPersistenceFailed = errors.New("ledger: durable write failed")
	// ErrTooLarge means a document exceeds MaxFileSize.
	ErrTooLarge = errors.New("ledger: document exceeds the maximum file size")
	// ErrClosed is returned after Close.
	ErrClosed = errors.New("ledger: store is closed")
	// ErrNoChange may be returned by an update closure to end the update without
	// a write, a generation bump or a health change.
	ErrNoChange = errors.New("ledger: no change")
	// ErrNotMemoryOnly is returned by updateMemoryOnly outside a memory-only mode.
	ErrNotMemoryOnly = errors.New("ledger: store is not in a memory-only mode")
	// ErrLocked means another process holds the ledger lock.
	ErrLocked = errors.New("ledger: the state directory is locked by another process")
	// ErrNotFound means the named entry or record does not exist.
	ErrNotFound = errors.New("ledger: not found")
	// ErrNoDesired means the entry has no observed desired version.
	ErrNoDesired = errors.New("ledger: entry has no desired version")
	// ErrUnsupportedPlatform means an operation needs a platform facility that is
	// not available; the store then fails closed.
	ErrUnsupportedPlatform = errors.New("ledger: operation not supported on this platform")
	// ErrPreserveFailed means an existing file could not be preserved safely.
	ErrPreserveFailed = errors.New("ledger: cannot preserve the existing file")
	// ErrPreserveAmbiguous means an interrupted preservation could not be shown
	// to have happened or not to have happened.
	ErrPreserveAmbiguous = errors.New("ledger: outcome of an interrupted preservation cannot be established")
	// ErrPreserveExhausted means every bounded collision retry was used.
	ErrPreserveExhausted = errors.New("ledger: no unused preserved file name found")
)

// Mode is how the store persists.
type Mode string

const (
	// ModeDurable persists every update (it may be degraded, see Health).
	ModeDurable Mode = "durable"
	// ModeMemoryOnly never touches the disk: persistence is disabled for the run.
	ModeMemoryOnly Mode = "memory_only"
)

// Reason says why the store started in a recovery or memory-only condition.
type Reason string

const (
	ReasonNone                Reason = ""
	ReasonUnsupportedPlatform Reason = "unsupported_platform"
	ReasonUnsafeDir           Reason = "unsafe_directory"
	ReasonLocked              Reason = "locked"
	ReasonLedgerUnreadable    Reason = "ledger_unreadable"
	ReasonTooLarge            Reason = "ledger_too_large"
	ReasonKeyUnsafe           Reason = "key_unsafe"
	ReasonKeyMalformed        Reason = "key_malformed"
	ReasonNewerSchema         Reason = "newer_schema"
	ReasonPreserveFailed      Reason = "preserve_failed"
	ReasonKeyGenerateFailed   Reason = "key_generate_failed"
	// Recoveries that keep the store durable.
	ReasonCorrupt     Reason = "corrupt"
	ReasonKeyMissing  Reason = "key_missing"
	ReasonKeyMismatch Reason = "key_mismatch"
	ReasonKeyAdopted  Reason = "key_adopted"
)

// KeyState is the classification of the key file by the KeyProvider.
type KeyState int

const (
	// KeyAbsent: no key exists.
	KeyAbsent KeyState = iota
	// KeyValid: a usable key exists; KeyID is its id.
	KeyValid
	// KeyUnsafe: a key object exists but fails an ownership, mode, type or size
	// check. The store fails closed and never generates a key.
	KeyUnsafe
	// KeyMalformed: a key file exists and is safe but is not a valid key.
	KeyMalformed
)

// KeyInspection is the result of KeyProvider.Inspect.
type KeyInspection struct {
	State KeyState
	// KeyID is the id derived from the key bytes (KeyValid only).
	KeyID string
	// Reason names the failed check (KeyUnsafe and KeyMalformed only).
	Reason string
}

// KeyProvider is the S2/S3 boundary (owner decision D-S2-1). S3 implements the
// production provider; S2 only inspects the state it reports, runs the §7
// recovery actions and never calls Generate for an unsafe or malformed key.
type KeyProvider interface {
	Inspect() KeyInspection
	// Generate creates and installs a new key and returns its id.
	Generate() (keyID string, err error)
}

// EventKind classifies an Event.
type EventKind string

const (
	EventRecovery            EventKind = "recovery"
	EventPreserved           EventKind = "preserved"
	EventStaleTempRemoved    EventKind = "stale_temp_removed"
	EventPersistenceDegraded EventKind = "persistence_degraded"
	EventPersistenceRestored EventKind = "persistence_restored"
	EventProbe               EventKind = "probe"
	EventRequestSuperseded   EventKind = "request_superseded"
	EventRequestCancelled    EventKind = "request_cancelled"
)

// Event is delivered to Config.Events, always outside the store mutex. It
// never carries secret material.
type Event struct {
	Kind   EventKind
	Reason Reason
	Detail string
	Err    error
	At     time.Time
}

// Config configures a Store. Keys is required; every other field has a default.
type Config struct {
	Keys KeyProvider
	// Now defaults to time.Now; the store converts it to UTC.
	Now func() time.Time
	// Random defaults to crypto/rand.Reader.
	Random io.Reader
	// After returns a channel that fires after d. It defaults to time.After and
	// exists so tests control retry and probe delays.
	After func(d time.Duration) <-chan time.Time
	// Events receives events outside the store mutex. A panic in it is
	// recovered and never rolls back an update.
	Events func(Event)
	// Flush is the pluggable flush hook run first by the background probe. It
	// returns flushed=true when it persisted a held record (its own update then
	// restored health) and flushed=false when nothing was held, in which case
	// the probe makes a harmless write.
	Flush func() (flushed bool, err error)

	ops *fileOps // tests only
}

// OpenResult reports how Open classified the state directory.
type OpenResult struct {
	Mode   Mode
	Reason Reason
	Detail string
	// Preserved is the path a corrupt or key-mismatched ledger was moved to.
	Preserved string
	// KeyID is the key id the in-memory ledger is bound to.
	KeyID string
	// CancelledRequests counts undispatched manual requests cancelled at start.
	CancelledRequests int
	// RemovedTemps lists stale temporary files removed at start.
	RemovedTemps []string
}

// Snapshot is a consistent copy of the published ledger.
type Snapshot struct {
	Ledger *Ledger
	// Durable acknowledges the published document: it is true when that document
	// was written durably (file and directory fsync succeeded) and false in a
	// memory-only mode and while the published document has never been
	// persisted. It does not prove the file currently holds exactly the
	// published generation: after a failed post-rename directory fsync the file
	// may hold a newer complete candidate that was never published (see
	// Store.update).
	Durable bool
	Mode    Mode
}

// Health is a point-in-time view of persistence.
type Health struct {
	Mode                Mode
	Reason              Reason
	Healthy             bool
	Durable             bool
	ConsecutiveFailures int
	LastError           string
	LastSuccess         time.Time
	Generation          uint64
	ProbeRunning        bool
}
