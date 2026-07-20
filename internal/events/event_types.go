package events

import (
	"fmt"
	"time"
)

// Source represents the source of a secret change event
type Source int

const (
	SourceAWSSecretsManager Source = iota
	SourceAzureKeyVault
	SourceHashiCorpVault
	SourceLocalVault
	SourceDockerLabel
)

// String returns the string representation of a Source
func (s Source) String() string {
	switch s {
	case SourceAWSSecretsManager:
		return "AWSSecretsManager"
	case SourceAzureKeyVault:
		return "AzureKeyVault"
	case SourceHashiCorpVault:
		return "HashiCorpVault"
	case SourceLocalVault:
		return "LocalVault"
	case SourceDockerLabel:
		return "DockerLabel"
	default:
		return "Unknown"
	}
}

// Severity represents the severity level of a secret change
type Severity int

const (
	SeverityNormal Severity = iota
	SeverityHigh
	SeverityCritical
)

// String returns the string representation of a Severity
func (s Severity) String() string {
	switch s {
	case SeverityNormal:
		return "Normal"
	case SeverityHigh:
		return "High"
	case SeverityCritical:
		return "Critical"
	default:
		return "Unknown"
	}
}

// SecretChangeEvent represents a secret change event
type SecretChangeEvent struct {
	SecretName string
	Version    string
	Source     Source
	Severity   Severity
	Timestamp  time.Time
	Metadata   map[string]string
}

// String returns a formatted string representation of SecretChangeEvent
func (e *SecretChangeEvent) String() string {
	if e == nil {
		return "SecretChangeEvent(nil)"
	}
	return fmt.Sprintf("SecretChangeEvent{SecretName:%s, Version:%s, Source:%s, Severity:%s, Timestamp:%s, Metadata:%v}",
		e.SecretName, e.Version, e.Source.String(), e.Severity.String(), e.Timestamp.Format(time.RFC3339), e.Metadata)
}

// Action represents an action on a container label
type Action int

const (
	ActionLabelCreate Action = iota
	ActionLabelUpdate
	ActionLabelRemove
)

// String returns the string representation of an Action
func (a Action) String() string {
	switch a {
	case ActionLabelCreate:
		return "Create"
	case ActionLabelUpdate:
		return "Update"
	case ActionLabelRemove:
		return "Remove"
	default:
		return "Unknown"
	}
}

// ContainerLabelEvent represents a container label event
type ContainerLabelEvent struct {
	ContainerID string
	Labels      map[string]string
	Action      Action
	Timestamp   time.Time
}

// String returns a formatted string representation of ContainerLabelEvent
func (e *ContainerLabelEvent) String() string {
	if e == nil {
		return "ContainerLabelEvent(nil)"
	}
	return fmt.Sprintf("ContainerLabelEvent{ContainerID:%s, Labels:%v, Action:%s, Timestamp:%s}",
		e.ContainerID, e.Labels, e.Action.String(), e.Timestamp.Format(time.RFC3339))
}

// EventPriority represents the priority level of a queued event
type EventPriority int

const (
	PriorityLow      EventPriority = 1
	PriorityNormal   EventPriority = 2
	PriorityCritical EventPriority = 3
)

// Event is the interface that all events must implement
type Event interface {
	String() string
}

// QueuedEvent represents an event that has been queued for processing
type QueuedEvent struct {
	Event      Event
	Priority   EventPriority
	Sequence   int64
	EnqueuedAt time.Time
}

// Verify that SecretChangeEvent and ContainerLabelEvent implement Event
var _ Event = (*SecretChangeEvent)(nil)
var _ Event = (*ContainerLabelEvent)(nil)
