package events

import (
	"strings"
	"testing"
	"time"
)

// TestSecretChangeEvent_String verifies String() formats correctly
func TestSecretChangeEvent_String(t *testing.T) {
	now := time.Now()
	metadata := map[string]string{"key1": "value1", "key2": "value2"}
	event := &SecretChangeEvent{
		SecretName: "db-password",
		Version:    "1.0.0",
		Source:     SourceAWSSecretsManager,
		Severity:   SeverityCritical,
		Timestamp:  now,
		Metadata:   metadata,
	}

	result := event.String()

	// Verify that the String() method returns a formatted string containing key information
	if !strings.Contains(result, "SecretChangeEvent{") {
		t.Errorf("expected 'SecretChangeEvent{' in output, got: %s", result)
	}
	if !strings.Contains(result, "db-password") {
		t.Errorf("expected 'db-password' in output, got: %s", result)
	}
	if !strings.Contains(result, "1.0.0") {
		t.Errorf("expected '1.0.0' in output, got: %s", result)
	}
	if !strings.Contains(result, "AWSSecretsManager") {
		t.Errorf("expected 'AWSSecretsManager' in output, got: %s", result)
	}
	if !strings.Contains(result, "Critical") {
		t.Errorf("expected 'Critical' in output, got: %s", result)
	}

	// Test nil pointer
	var nilEvent *SecretChangeEvent
	nilResult := nilEvent.String()
	if nilResult != "SecretChangeEvent(nil)" {
		t.Errorf("expected 'SecretChangeEvent(nil)' for nil pointer, got: %s", nilResult)
	}
}

// TestContainerLabelEvent_String verifies String() formats correctly
func TestContainerLabelEvent_String(t *testing.T) {
	now := time.Now()
	labels := map[string]string{"app": "myapp", "version": "2.0"}
	event := &ContainerLabelEvent{
		ContainerID: "abc123def456",
		Labels:      labels,
		Action:      ActionLabelUpdate,
		Timestamp:   now,
	}

	result := event.String()

	// Verify that the String() method returns a formatted string containing key information
	if !strings.Contains(result, "ContainerLabelEvent{") {
		t.Errorf("expected 'ContainerLabelEvent{' in output, got: %s", result)
	}
	if !strings.Contains(result, "abc123def456") {
		t.Errorf("expected 'abc123def456' in output, got: %s", result)
	}
	if !strings.Contains(result, "Update") {
		t.Errorf("expected 'Update' in output, got: %s", result)
	}

	// Test nil pointer
	var nilEvent *ContainerLabelEvent
	nilResult := nilEvent.String()
	if nilResult != "ContainerLabelEvent(nil)" {
		t.Errorf("expected 'ContainerLabelEvent(nil)' for nil pointer, got: %s", nilResult)
	}
}

// TestEventInterface verifies both event types implement Event interface
func TestEventInterface(t *testing.T) {
	now := time.Now()

	// Create a SecretChangeEvent
	secretEvent := &SecretChangeEvent{
		SecretName: "api-key",
		Version:    "2.0",
		Source:     SourceHashiCorpVault,
		Severity:   SeverityHigh,
		Timestamp:  now,
		Metadata:   map[string]string{},
	}

	// Create a ContainerLabelEvent
	containerEvent := &ContainerLabelEvent{
		ContainerID: "xyz789",
		Labels:      map[string]string{"env": "prod"},
		Action:      ActionLabelCreate,
		Timestamp:   now,
	}

	// Verify that both implement Event interface
	var _ Event = secretEvent
	var _ Event = containerEvent

	// Verify String() method works for both
	secretStr := secretEvent.String()
	if secretStr == "" {
		t.Errorf("SecretChangeEvent.String() returned empty string")
	}

	containerStr := containerEvent.String()
	if containerStr == "" {
		t.Errorf("ContainerLabelEvent.String() returned empty string")
	}

	// Verify they are different types
	if secretStr == containerStr {
		t.Errorf("expected different String() outputs for different event types")
	}
}
