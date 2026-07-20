package rotation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type mockTransport struct {
	reqFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.reqFunc(req)
}

func getBaseInspect() container.InspectResponse {
	return container.InspectResponse{
		ContainerJSONBase: &types.ContainerJSONBase{
			Name:       "/my-container",
			HostConfig: &container.HostConfig{},
			State:      &container.State{Running: true},
		},
		Config: &container.Config{
			Image: "nginx",
			Env:   []string{"EXISTING=true"},
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {},
			},
		},
	}
}

func TestRollingStrategy_Execute_Success(t *testing.T) {
	inspectResp := getBaseInspect()

	createResp := container.CreateResponse{
		ID: "new-container-id",
	}
	bCreate, _ := json.Marshal(createResp)

	// Track container names for verification after rename
	containerNames := map[string]string{
		"cid":              "my-container",
		"new-container-id": "my-container",
	}

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/cid/json":
					resp := inspectResp
					resp.Name = "/" + containerNames["cid"]
					bResp, _ := json.Marshal(resp)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bResp))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/cid/rename":
					// Extract the new name from query parameter
					newName := req.URL.Query().Get("name")
					if newName != "" {
						containerNames["cid"] = newName
					}
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/create":
					return &http.Response{StatusCode: 201, Body: io.NopCloser(bytes.NewReader(bCreate))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/start":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/new-container-id/json":
					healthyInspect := inspectResp
					healthyInspect.Name = "/" + containerNames["new-container-id"]
					healthyInspect.State.Health = &container.Health{Status: "healthy"}
					bHealthy, _ := json.Marshal(healthyInspect)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bHealthy))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/rename":
					// Extract the new name from query parameter
					newName := req.URL.Query().Get("name")
					if newName != "" {
						containerNames["new-container-id"] = newName
					}
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/cid/stop":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/cid":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				default:
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				}
			},
		},
	}

	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))
	rs := NewRollingStrategy(cli)

	err := rs.Execute(context.Background(), "cid", map[string]string{"SECRET": "val"}, 1*time.Second)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestRollingStrategy_Execute_HealthTimeout(t *testing.T) {
	inspectResp := getBaseInspect()
	bInspect, _ := json.Marshal(inspectResp)

	createResp := container.CreateResponse{
		ID: "new-container-id",
	}
	bCreate, _ := json.Marshal(createResp)

	renameCalled := 0
	removeCalled := 0
	stopCalled := 0

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/cid/json":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bInspect))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/cid/rename":
					renameCalled++
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/create":
					return &http.Response{StatusCode: 201, Body: io.NopCloser(bytes.NewReader(bCreate))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/start":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/new-container-id/json":
					unhealthyInspect := inspectResp
					unhealthyInspect.State.Health = &container.Health{Status: "unhealthy"}
					bUnhealthy, _ := json.Marshal(unhealthyInspect)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bUnhealthy))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/stop":
					stopCalled++
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/new-container-id":
					removeCalled++
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				default:
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				}
			},
		},
	}

	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))
	rs := NewRollingStrategy(cli)

	err := rs.Execute(context.Background(), "cid", map[string]string{"SECRET": "val"}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("Expected error due to health timeout, got nil")
	}

	// Verify rollback behavior with new atomic strategy:
	// On health check failure, the original container is never renamed,
	// and the new container is simply stopped and removed.
	// No rename operations should occur before the atomic swap point.
	if renameCalled != 0 {
		t.Errorf("Expected 0 rename calls (health failed before swap), got %d", renameCalled)
	}
	if stopCalled == 0 {
		t.Error("Expected stop call for unhealthy new container during rollback")
	}
	if removeCalled == 0 {
		t.Error("Expected remove call for unhealthy new container during rollback")
	}
}

func TestRollingStrategy_RenameTimeout(t *testing.T) {
	// Test 1: Verify default timeout is 30 seconds
	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
			},
		},
	}

	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))
	rs := NewRollingStrategy(cli)

	if rs.renameTimeout != 30*time.Second {
		t.Errorf("Expected default renameTimeout to be 30 seconds, got %v", rs.renameTimeout)
	}

	// Test 2: Verify timeout is actually enforced by checking context composition
	// Create a custom transport that respects context cancellation
	blockingTransport := &mockTransport{
		reqFunc: func(req *http.Request) (*http.Response, error) {
			// For rename requests, block and check context
			if req.Method == "POST" && strings.Contains(req.URL.Path, "/rename") {
				// Use a channel to simulate blocking operation
				done := make(chan struct{})
				go func() {
					time.Sleep(2 * time.Second)
					close(done)
				}()

				// Wait for either operation to complete or context to be cancelled
				select {
				case <-done:
					// Operation completed (this shouldn't happen with timeout)
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case <-req.Context().Done():
					// Context was cancelled (timeout occurred)
					return nil, req.Context().Err()
				}
			}
			return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
		},
	}

	blockingCli, _ := client.NewClientWithOpts(
		client.WithVersion("1.41"),
		client.WithHost("tcp://127.0.0.1:2375"),
		client.WithHTTPClient(&http.Client{Transport: blockingTransport}),
	)
	rsWithTimeout := NewRollingStrategy(blockingCli)

	// Override the timeout to 1 second for testing
	rsWithTimeout.renameTimeout = 1 * time.Second

	// Measure elapsed time and verify timeout occurs before blocking completes
	start := time.Now()
	err := rsWithTimeout.renameWithTimeout(context.Background(), "test-id", "test-name")
	elapsed := time.Since(start)

	// Should timeout after ~1 second, not wait 2 seconds
	if elapsed >= 2*time.Second {
		t.Errorf("Expected timeout after ~1 second, but took %v - timeout not enforced", elapsed)
	}

	// Verify context deadline exceeded error occurred
	if err == nil {
		t.Error("Expected error due to timeout, got nil")
	}

	// Test 3: Verify context deadline is properly composed (caller's context is respected)
	shortDeadlineCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	composedStart := time.Now()
	err = rsWithTimeout.renameWithTimeout(shortDeadlineCtx, "test-id", "test-name")
	composedElapsed := time.Since(composedStart)

	// Should respect the caller's shorter deadline (500ms) instead of the 1s rename timeout
	if composedElapsed >= 1*time.Second {
		t.Errorf("Expected caller's 500ms deadline to be respected, but took %v", composedElapsed)
	}

	if err == nil {
		t.Error("Expected error due to caller's deadline, got nil")
	}
}
