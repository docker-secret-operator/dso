package events

import "context"

// RotationTrigger is a function type that triggers a rotation
type RotationTrigger func(ctx context.Context, secretName string, priority EventPriority) error

// EventReactor defines the interface for processing and reacting to events
type EventReactor interface {
	// ProcessSecretEvent processes a secret change event
	ProcessSecretEvent(ctx context.Context, event SecretChangeEvent) error

	// ProcessContainerEvent processes a container label event
	ProcessContainerEvent(ctx context.Context, event ContainerLabelEvent) error

	// Start starts the event reactor
	Start(ctx context.Context) error

	// Stop stops the event reactor
	Stop(ctx context.Context) error

	// IsHealthy returns whether the event reactor is in a healthy state
	IsHealthy() bool
}
