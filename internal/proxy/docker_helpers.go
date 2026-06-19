package proxy

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// shortID returns up to the first 12 characters of a container ID,
// safe for empty or short strings (e.g. test fixtures, future Docker versions).
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func containerListOptions() container.ListOptions {
	f := filters.NewArgs()
	f.Add("status", "running")
	return container.ListOptions{Filters: f}
}

func extractContainerIP(ns *container.NetworkSettingsSummary) string {
	if ns == nil {
		return ""
	}
	for _, ep := range ns.Networks {
		if ep != nil && ep.IPAddress != "" {
			return ep.IPAddress
		}
	}
	return ""
}
