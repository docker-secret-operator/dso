// Package notify delivers rotation/recovery outcome events to external
// destinations (generic webhooks today). It is strictly an observer of
// rotation: nothing in this package is ever consulted by rotation logic,
// and no delivery outcome can alter rotation state. The fundamental
// invariant is that DSO must continue rotating secrets correctly even if
// every notification destination is completely unavailable.
package notify

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/docker-secret-operator/dso/pkg/security"
)

// EventType identifies the operational outcome an event describes. Only
// outcomes with real operational semantics are modeled -- there is
// deliberately no "rotation_started" (operators act on outcomes, and a
// started-event would double delivery volume for no actionable signal).
type EventType string

const (
	RotationSucceeded EventType = "rotation_succeeded"
	RotationFailed    EventType = "rotation_failed"
	RecoverySucceeded EventType = "recovery_succeeded"
	RecoveryFailed    EventType = "recovery_failed"
)

// RotationEvent is the provider-neutral notification payload. Every field
// is metadata about a rotation; none may ever carry secret material.
// ErrorMessage is redacted at construction (NewEvent), not trusted to be
// clean by callers -- provider errors can embed credentials or URLs
// containing tokens.
type RotationEvent struct {
	EventID    string    `json:"event_id"`
	Type       EventType `json:"event_type"`
	Timestamp  time.Time `json:"timestamp"`
	Provider   string    `json:"provider"`
	SecretName string    `json:"secret_name"`
	// Containers holds the affected container IDs (short form), when known.
	Containers []string `json:"affected_containers,omitempty"`
	// DurationSeconds is the rotation's wall-clock duration, when the
	// emitting site measured one; zero (omitted) otherwise.
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	// ErrorMessage is present only on *_failed events, and has been passed
	// through pkg/security's redaction patterns.
	ErrorMessage string `json:"error_message,omitempty"`
}

// redactor is shared across events; RedactionPatterns is a set of
// precompiled regexps and is safe for concurrent readers.
var redactor = security.NewRedactionPatterns()

// NewEvent builds a RotationEvent with a fresh event ID and the error (if
// any) redacted. Delivery may retry, so consumers should treat EventID as
// the idempotency key: at-least-once delivery is the contract, never
// exactly-once.
func NewEvent(t EventType, provider, secretName string, containers []string, duration time.Duration, err error) RotationEvent {
	e := RotationEvent{
		EventID:    newEventID(),
		Type:       t,
		Timestamp:  time.Now().UTC(),
		Provider:   provider,
		SecretName: secretName,
		Containers: containers,
	}
	if duration > 0 {
		e.DurationSeconds = duration.Seconds()
	}
	if err != nil {
		e.ErrorMessage = redactor.RedactError(err)
	}
	return e
}

func newEventID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Timestamp fallback keeps events flowing if the entropy source is
		// unavailable; uniqueness degrades but nothing secret-bearing is at
		// stake in an event ID.
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b)
}
