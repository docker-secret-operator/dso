package proxy

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

func TestExtractContainerIP(t *testing.T) {
	t.Run("nil NetworkSettingsSummary returns empty string", func(t *testing.T) {
		if got := extractContainerIP(nil); got != "" {
			t.Errorf("expected empty string for nil input, got %q", got)
		}
	})

	t.Run("no networks returns empty string", func(t *testing.T) {
		ns := &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{},
		}
		if got := extractContainerIP(ns); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("finds IP from a single network", func(t *testing.T) {
		ns := &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: "172.17.0.2"},
			},
		}
		if got := extractContainerIP(ns); got != "172.17.0.2" {
			t.Errorf("expected 172.17.0.2, got %q", got)
		}
	})

	t.Run("skips nil endpoint entries", func(t *testing.T) {
		ns := &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"bad":  nil,
				"good": {IPAddress: "10.0.0.5"},
			},
		}
		if got := extractContainerIP(ns); got != "10.0.0.5" {
			t.Errorf("expected 10.0.0.5, got %q", got)
		}
	})

	t.Run("skips entries with empty IPAddress", func(t *testing.T) {
		ns := &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"empty": {IPAddress: ""},
				"real":  {IPAddress: "10.0.0.9"},
			},
		}
		if got := extractContainerIP(ns); got != "10.0.0.9" {
			t.Errorf("expected 10.0.0.9, got %q", got)
		}
	})
}

func TestContainerListOptions(t *testing.T) {
	opts := containerListOptions()
	if !opts.Filters.Contains("status") {
		t.Fatal("expected a 'status' filter to be set")
	}
	if !opts.Filters.ExactMatch("status", "running") {
		t.Error("expected filter status=running")
	}
}
