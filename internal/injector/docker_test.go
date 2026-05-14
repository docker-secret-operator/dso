package injector

import (
	"context"
	"go.uber.org/zap/zaptest"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDockerInjector_LogInjectionEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	injector, err := NewDockerInjector(logger)
	if err != nil {
		t.Fatalf("Failed to create injector: %v", err)
	}
	defer injector.Close()

	// Should not panic or block
	injector.LogInjectionEvent("my-secret", "container-1", "update", "success", "")
	injector.LogInjectionEvent("my-secret", "container-1", "update", "failure", "some error")
}

func TestDockerInjector_SignalContainers_DockerMissing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	injector, err := NewDockerInjector(logger)
	if err != nil {
		t.Fatalf("Failed to create injector: %v", err)
	}
	defer injector.Close()

	// Set invalid DOCKER_HOST to force ContainerList or NewClient to fail fast
	os.Setenv("DOCKER_HOST", "tcp://127.0.0.1:12345")
	defer os.Unsetenv("DOCKER_HOST")

	ctx := context.Background()
	_ = injector.SignalContainers(ctx, "my-secret")
}

func TestDockerInjector_SignalContainers_MockServer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	injector, err := NewDockerInjector(logger)
	if err != nil {
		t.Fatalf("Failed to create injector: %v", err)
	}
	defer injector.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" || r.URL.Path == "/v1.41/containers/json" {
			w.WriteHeader(http.StatusOK)
			// Return one container
			w.Write([]byte(`[{"Id": "mock-container-1", "Names": ["/mock"]}]`))
			return
		}
		if r.URL.Path == "/containers/mock-container-1/kill" || r.URL.Path == "/v1.41/containers/mock-container-1/kill" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Catch-all
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	os.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())
	os.Setenv("DOCKER_API_VERSION", "1.41")
	defer os.Unsetenv("DOCKER_HOST")
	defer os.Unsetenv("DOCKER_API_VERSION")

	ctx := context.Background()
	_ = injector.SignalContainers(ctx, "my-secret")
}

func TestDockerInjector_SignalContainers_MockServer_KillFail(t *testing.T) {
	logger := zaptest.NewLogger(t)
	injector, err := NewDockerInjector(logger)
	if err != nil {
		t.Fatalf("Failed to create injector: %v", err)
	}
	defer injector.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" || r.URL.Path == "/v1.41/containers/json" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"Id": "mock-fail", "Names": ["/mock"]}]`))
			return
		}
		if r.URL.Path == "/containers/mock-fail/kill" || r.URL.Path == "/v1.41/containers/mock-fail/kill" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	os.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())
	os.Setenv("DOCKER_API_VERSION", "1.41")
	defer os.Unsetenv("DOCKER_HOST")
	defer os.Unsetenv("DOCKER_API_VERSION")

	ctx := context.Background()
	_ = injector.SignalContainers(ctx, "my-secret")
}
