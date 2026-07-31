package analyzer

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestLooksExplicitlyNamed(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"happy_einstein", false},      // Docker's default auto-generated shape
		{"happy_einstein2", false},     // auto-generated with retry suffix
		{"db", true},                   // explicit --name
		{"my-app-postgres", true},      // explicit, contains hyphens
		{"myproject_postgres_1", true}, // Compose v1 default naming (three segments)
		{"myproject-postgres-1", true}, // Compose v2 default naming (hyphens)
		{"", false},                    // Docker never actually returns this, but must not panic/misclassify
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksExplicitlyNamed(tt.name)
			if got != tt.want {
				t.Errorf("looksExplicitlyNamed(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestAnalyzeContainer_HasContainerName(t *testing.T) {
	t.Run("auto-generated name does not count as explicit", func(t *testing.T) {
		c := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				Name:       "/happy_einstein",
				HostConfig: &container.HostConfig{},
			},
			Config: &container.Config{},
		}
		res := AnalyzeContainer(c)
		if res.HasContainerName {
			t.Error("expected HasContainerName=false for a Docker auto-generated name")
		}
	})

	t.Run("explicit name counts", func(t *testing.T) {
		c := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				Name:       "/production-db",
				HostConfig: &container.HostConfig{},
			},
			Config: &container.Config{},
		}
		res := AnalyzeContainer(c)
		if !res.HasContainerName {
			t.Error("expected HasContainerName=true for an explicitly-chosen name")
		}
	})
}

func TestAnalyzeContainer_StatefulDetection(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  bool
	}{
		{"postgres", "postgres:15", true},
		{"mysql", "mysql:8", true},
		{"redis previously missed", "redis:7", true},
		{"elasticsearch previously missed", "elasticsearch:8.11", true},
		{"cassandra previously missed", "cassandra:4", true},
		{"custom internal mirror not matched by name", "registry.internal/myapp:v1", false},
		{"stateless web app", "nginx:latest", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:       "/x",
					HostConfig: &container.HostConfig{},
				},
				Config: &container.Config{Image: tt.image},
			}
			res := AnalyzeContainer(c)
			if res.IsStateful != tt.want {
				t.Errorf("image %q: IsStateful = %v, want %v", tt.image, res.IsStateful, tt.want)
			}
		})
	}

	t.Run("mount path catches custom image mimicking a stateful workload", func(t *testing.T) {
		c := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				Name:       "/x",
				HostConfig: &container.HostConfig{},
			},
			Config: &container.Config{Image: "registry.internal/myapp-db:v2"},
			Mounts: []container.MountPoint{
				{Destination: "/bitnami/postgresql"},
			},
		}
		res := AnalyzeContainer(c)
		if !res.IsStateful {
			t.Error("expected /bitnami mount to be detected as stateful")
		}
	})
}
