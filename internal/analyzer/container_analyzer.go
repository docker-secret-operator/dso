package analyzer

import (
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// dockerAutoNamePattern matches Docker's daemon-generated container names:
// two lowercase words joined by an underscore (moby's namesgenerator draws
// from a fixed adjective/surname wordlist), optionally with a numeric suffix
// appended when the daemon retries after a name collision (e.g.
// "happy_einstein", "happy_einstein2"). A name that does NOT match this
// shape was very likely set explicitly, either via `docker run --name` or
// Compose's `container_name:`.
//
// This is a heuristic, not a guarantee: an operator could coincidentally
// choose a two-lowercase-word underscored name and have it misclassified as
// auto-generated. That is an accepted, documented limitation — there is no
// field on container.InspectResponse that distinguishes "the daemon picked
// this" from "the user picked this", so a pattern match against the known
// generator shape is the best available signal.
var dockerAutoNamePattern = regexp.MustCompile(`^[a-z]+_[a-z]+[0-9]*$`)

// looksExplicitlyNamed reports whether name was very likely set on purpose
// (via --name or compose's container_name:) rather than left to Docker's
// auto-generated default. An explicit name matters for rotation strategy
// because other containers may resolve it via Docker's embedded DNS —
// replacing it with a differently-named container during a "rolling"
// (parallel-instance) rotation could break service discovery for peers.
func looksExplicitlyNamed(name string) bool {
	return name != "" && !dockerAutoNamePattern.MatchString(name)
}

type AnalysisResult struct {
	ContainerName       string
	HasFixedPortBinding bool
	HasContainerName    bool
	HasRestartAlways    bool
	IsStateful          bool
	SupportsScaling     bool
	HasHealthCheck      bool
	NetworkMode         string
	FixedPorts          []string
}

func AnalyzeContainer(container container.InspectResponse) AnalysisResult {
	res := AnalysisResult{
		ContainerName: strings.TrimPrefix(container.Name, "/"),
		NetworkMode:   string(container.HostConfig.NetworkMode),
		FixedPorts:    []string{},
	}

	// 1. Container Name — only an explicitly-chosen name counts. Docker's
	// InspectResponse.Name is never empty (the daemon always assigns one),
	// so treating any non-empty value as "has a container name" flagged
	// virtually every container, regardless of whether the operator (or
	// Compose) actually pinned a name other services might depend on.
	if looksExplicitlyNamed(res.ContainerName) {
		res.HasContainerName = true
	}

	// 2. Restart Policy
	if container.HostConfig.RestartPolicy.Name == "always" {
		res.HasRestartAlways = true
	}

	// 3. Health Check
	if container.Config.Healthcheck != nil && len(container.Config.Healthcheck.Test) > 0 {
		res.HasHealthCheck = true
	}

	// 4. Fixed Port Binding
	for _, bindings := range container.HostConfig.PortBindings {
		for _, binding := range bindings {
			if binding.HostPort != "" && binding.HostPort != "0" {
				res.HasFixedPortBinding = true
				res.FixedPorts = append(res.FixedPorts, binding.HostPort)
				break
			}
		}
	}

	// 5. Stateful Detection. Image-name matching is inherently incomplete —
	// any custom-tagged or internally-mirrored database image won't contain
	// one of these substrings — so the mount-path check is the primary
	// signal and the image list is a fast-path shortcut for common images.
	img := strings.ToLower(container.Config.Image)
	statefulImageHints := []string{
		"mysql", "postgres", "mariadb", "mongo", "redis", "memcached",
		"elasticsearch", "opensearch", "cassandra", "rabbitmq", "clickhouse",
		"etcd", "zookeeper", "kafka", "influxdb", "cockroach", "couchdb",
		"neo4j", "sqlserver", "dynamodb", "timescale",
	}
	for _, hint := range statefulImageHints {
		if strings.Contains(img, hint) {
			res.IsStateful = true
			break
		}
	}
	if !res.IsStateful && container.Mounts != nil {
		statefulMountPrefixes := []string{"/var/lib/", "/data", "/bitnami", "/var/opt/"}
		for _, m := range container.Mounts {
			for _, prefix := range statefulMountPrefixes {
				if strings.HasPrefix(m.Destination, prefix) {
					res.IsStateful = true
					break
				}
			}
			if res.IsStateful {
				break
			}
		}
	}

	// 6. Scaling capability
	res.SupportsScaling = !res.HasContainerName && !res.HasFixedPortBinding

	return res
}
