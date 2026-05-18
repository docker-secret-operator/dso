package proxy

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

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
