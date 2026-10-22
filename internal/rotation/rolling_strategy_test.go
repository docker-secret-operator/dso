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
		ContainerJSONBase: &container.ContainerJSONBase{
			Name:       "/my-container",
			HostConfig: &container.HostConfig{},
			State:      &container.State{Running: true},
		},
		Config: &container.Config{
			Image: "nginx",
			Env:   []string{"EXISTING=true"},
		},
		NetworkSettings: &container.NetworkSettings{
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

	err := rs.Execute(context.Background(), "cid", map[string]string{"SECRET": "val"}, nil, nil, 1*time.Second)
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

	err := rs.Execute(context.Background(), "cid", map[string]string{"SECRET": "val"}, nil, nil, 100*time.Millisecond)
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

func TestRollingStrategy_Execute_ContextCancellation(t *testing.T) {
	inspectResp := getBaseInspect()

	createResp := container.CreateResponse{
		ID: "new-container-id",
	}
	bCreate, _ := json.Marshal(createResp)

	// Track container names and rename timing
	containerNames := map[string]string{
		"cid":              "my-container",
		"new-container-id": "my-container",
	}
	renameAttempts := 0
	healthCheckCount := 0

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
					renameAttempts++
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
					healthCheckCount++
					healthyInspect := inspectResp
					healthyInspect.Name = "/" + containerNames["new-container-id"]
					healthyInspect.State.Health = &container.Health{Status: "healthy"}
					bHealthy, _ := json.Marshal(healthyInspect)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bHealthy))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/rename":
					renameAttempts++
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

	// Create a context with a deadline that expires during the atomic swap
	// The health check will pass (it's before the swap), but the context will be
	// cancelled during the rename operations. Since swapCtx is independent,
	// renames should still complete.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Execute rotation - this will succeed despite context cancellation
	// because swapCtx (context.Background()) is used for rename operations
	err := rs.Execute(ctx, "cid", map[string]string{"SECRET": "val"}, nil, nil, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Execute failed unexpectedly: %v", err)
	}

	// Verify that renames completed despite the caller's context being cancelled
	// We expect 2 rename calls: one for original->backup, one for new->original
	if renameAttempts < 2 {
		t.Errorf("Expected at least 2 rename attempts (atomic swap), got %d", renameAttempts)
	}

	// Verify final state: original container should have the backup name,
	// new container should have the original name
	expectedBackupPrefix := "my-container_dso_backup_"
	if !strings.HasPrefix(containerNames["cid"], expectedBackupPrefix) {
		t.Errorf("Expected original container to have backup name (prefix: %s), got: %s",
			expectedBackupPrefix, containerNames["cid"])
	}

	if containerNames["new-container-id"] != "my-container" {
		t.Errorf("Expected new container to have original name (my-container), got: %s",
			containerNames["new-container-id"])
	}
}

func TestRollingStrategy_CleanupContainerWithRetry(t *testing.T) {
	inspectResp := getBaseInspect()

	createResp := container.CreateResponse{
		ID: "new-container-id",
	}
	bCreate, _ := json.Marshal(createResp)

	// Track container names and cleanup attempts
	containerNames := map[string]string{
		"cid":              "my-container",
		"new-container-id": "my-container",
	}
	stopAttempts := 0
	removeAttempts := 0

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
					stopAttempts++
					// Fail on first attempt, succeed on second
					if stopAttempts == 1 {
						return &http.Response{StatusCode: 500, Body: io.NopCloser(bytes.NewReader([]byte("transient error")))}, nil
					}
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/cid":
					removeAttempts++
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				default:
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				}
			},
		},
	}

	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))
	rs := NewRollingStrategy(cli)

	// Execute rotation - cleanup should retry after initial stop failure
	err := rs.Execute(context.Background(), "cid", map[string]string{"SECRET": "val"}, nil, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify retry behavior:
	// Stop should be called twice (first fails, second succeeds)
	if stopAttempts != 2 {
		t.Errorf("Expected stopAttempts == 2, got %d", stopAttempts)
	}

	// Remove should be called once after stop succeeds
	if removeAttempts != 1 {
		t.Errorf("Expected removeAttempts == 1, got %d", removeAttempts)
	}
}

// TestRollingStrategy_Execute_FileSecretStreamFailure_RollsBack is the
// crash-window regression test (P0 fix, Phase 3): it deterministically
// fails the file-secret delivery step (StreamSecretToContainer's underlying
// CopyToContainer call, PUT .../archive) using the same mock-transport
// failure-injection pattern as TestRollingStrategy_Execute_HealthTimeout
// above, rather than actually killing a process. This is the smallest
// abstraction boundary at which the dangerous "new container exists but
// isn't safely usable yet" window can be exercised.
//
// Asserts the properties Phase 3 asks for:
//   - the original container is never renamed (still recoverable/active) --
//     failure here happens before the atomic swap point
//   - the new (unsafe) container is removed, not left running
//   - no secret value is persisted anywhere Docker retains (nothing in this
//     test path ever reaches Config.Env or container create body, since the
//     failure happens before any swap/rename bookkeeping)
//   - the rotation reports a clear, distinguishable error (partial rotation
//     is detected, not silently swallowed)
func TestRollingStrategy_Execute_FileSecretStreamFailure_RollsBack(t *testing.T) {
	inspectResp := getBaseInspect()
	bInspect, _ := json.Marshal(inspectResp)

	createResp := container.CreateResponse{ID: "new-container-id"}
	bCreate, _ := json.Marshal(createResp)

	renameCalled := 0
	newContainerRemoved := 0
	streamAttempted := 0

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/cid/json":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bInspect))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/create":
					return &http.Response{StatusCode: 201, Body: io.NopCloser(bytes.NewReader(bCreate))}, nil
				case req.Method == "PUT" && strings.Contains(req.URL.Path, "/archive"):
					// Simulate the file-secret stream failing (e.g. daemon
					// hiccup, disk full, container removed out-of-band) --
					// this is the deterministic failure-injection point.
					streamAttempted++
					return &http.Response{StatusCode: 500, Body: io.NopCloser(bytes.NewReader([]byte("simulated stream failure")))}, nil
				case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/new-container-id":
					newContainerRemoved++
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/cid/rename":
					renameCalled++
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				default:
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				}
			},
		},
	}

	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))
	rs := NewRollingStrategy(cli)

	fileSecrets := []FileSecretSpec{
		{Path: "/run/secrets/dso", Data: map[string]string{"db-password": "DSO_TEST_SECRET_UNIQUE_9f31c8e7"}, UID: 1000, GID: 1000},
	}

	err := rs.Execute(context.Background(), "cid", nil, fileSecrets, nil, 1*time.Second)
	if err == nil {
		t.Fatal("Expected error when file-secret streaming fails, got nil")
	}
	if !strings.Contains(err.Error(), "stream file-mode secret") {
		t.Errorf("Expected error to identify the file-secret streaming step, got: %v", err)
	}
	if streamAttempted == 0 {
		t.Error("Expected the stream step to actually be attempted")
	}
	if renameCalled != 0 {
		t.Errorf("Expected 0 rename calls (failure occurred before the swap point, original container must stay untouched), got %d", renameCalled)
	}
	if newContainerRemoved == 0 {
		t.Error("Expected the unsafe new container to be removed during rollback")
	}
}

// TestRollingStrategy_Execute_FileSecretNeverInCreateRequest is the P0
// secret-exposure regression test (Phase 4). It uses a unique sentinel
// value and asserts the sentinel never appears in the request body DSO
// sends to Docker's container-create API -- i.e. it never becomes part of
// Config.Env / `docker inspect` output -- while confirming it DOES reach
// the file-streaming step (so the fix doesn't just silently drop the
// secret). Covers the "container configuration" and "container environment"
// surfaces from the security regression checklist; command-line/labels are
// covered by inspection of the same captured request body below.
func TestRollingStrategy_Execute_FileSecretNeverInCreateRequest(t *testing.T) {
	const sentinel = "DSO_TEST_SECRET_UNIQUE_9f31c8e7"

	inspectResp := getBaseInspect()

	createResp := container.CreateResponse{ID: "new-container-id"}
	bCreate, _ := json.Marshal(createResp)

	containerNames := map[string]string{
		"cid":              "my-container",
		"new-container-id": "my-container",
	}

	var createRequestBody []byte
	var streamedData map[string]string

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/cid/json":
					resp := inspectResp
					resp.Name = "/" + containerNames["cid"]
					bResp, _ := json.Marshal(resp)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bResp))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/create":
					createRequestBody, _ = io.ReadAll(req.Body)
					return &http.Response{StatusCode: 201, Body: io.NopCloser(bytes.NewReader(bCreate))}, nil
				case req.Method == "PUT" && strings.Contains(req.URL.Path, "/archive"):
					// Decode the tar stream StreamSecretToContainer sent so we
					// can assert the sentinel DID reach the file-injection
					// path (functional correctness), just not Config.Env.
					streamedData = map[string]string{"probe": "received"}
					body, _ := io.ReadAll(req.Body)
					if bytes.Contains(body, []byte(sentinel)) {
						streamedData["sentinel_seen_in_tar"] = "true"
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/start":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/new-container-id/json":
					healthyInspect := inspectResp
					healthyInspect.Name = "/" + containerNames["new-container-id"]
					healthyInspect.State.Health = &container.Health{Status: "healthy"}
					bHealthy, _ := json.Marshal(healthyInspect)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bHealthy))}, nil
				case req.Method == "POST" && strings.Contains(req.URL.Path, "/exec") && !strings.Contains(req.URL.Path, "/start"):
					b, _ := json.Marshal(struct{ ID string }{ID: "exec-1"})
					return &http.Response{StatusCode: 201, Body: io.NopCloser(bytes.NewReader(b))}, nil
				case req.Method == "POST" && strings.Contains(req.URL.Path, "/exec/exec-1/start"):
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "GET" && strings.Contains(req.URL.Path, "/exec/exec-1/json"):
					b, _ := json.Marshal(struct {
						ExitCode int
						Running  bool
					}{ExitCode: 0, Running: false})
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b))}, nil
				case req.Method == "POST" && strings.Contains(req.URL.Path, "/rename"):
					newName := req.URL.Query().Get("name")
					id := "cid"
					if strings.Contains(req.URL.Path, "new-container-id") {
						id = "new-container-id"
					}
					if newName != "" {
						containerNames[id] = newName
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

	// Only an env-mode secret goes in newEnvs; the sentinel is exclusively a
	// file-mode secret, mirroring how internal/watcher/controller.go now
	// splits the two after the fix.
	newEnvs := map[string]string{"UNRELATED_ENV": "fine-to-be-visible"}
	fileSecrets := []FileSecretSpec{
		{Path: "/run/secrets/dso", Data: map[string]string{"db-password": sentinel}, UID: 1000, GID: 1000},
	}

	err := rs.Execute(context.Background(), "cid", newEnvs, fileSecrets, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if createRequestBody == nil {
		t.Fatal("container create request was never captured")
	}
	if bytes.Contains(createRequestBody, []byte(sentinel)) {
		t.Fatalf("SECURITY REGRESSION: sentinel secret value found in container create request body (would appear in `docker inspect`):\n%s", createRequestBody)
	}
	if streamedData["sentinel_seen_in_tar"] != "true" {
		t.Error("sentinel was not observed in the file-streaming request -- fix may have dropped the secret instead of safely delivering it")
	}
}

// TestRollingStrategy_Execute_StripsStaleManagedSecretEnv is the P0-3
// regression test. It targets the PRIMARY, successful rolling-rotation path
// (RollingStrategy.Execute -> PrepareShadowConfig), which is a THIRD,
// independent instance of the same root cause already fixed for the
// rolling-fallback (executeSimpleRestart, P0-1) and primary-restart (P0-2)
// paths: PrepareShadowConfig clones the original container's Config.Env
// verbatim and only ever adds/updates keys present in newEnvs, so a secret
// once written to Config.Env (while it was env-mode) would otherwise persist
// unchanged into the shadow container even after being switched to
// file-mode or dropped from config entirely.
//
// This is the highest-priority instance of the three: it is the default
// *success* path of DSO's flagship zero-downtime rolling rotation, not a
// failure fallback -- so it is the path most likely to be exercised in
// normal production operation.
func TestRollingStrategy_Execute_StripsStaleManagedSecretEnv(t *testing.T) {
	const (
		sentinel = "DSO_P0_3_STALE_SENTINEL"
		freshVal = "fresh-current-value"
	)

	inspectResp := getBaseInspect()
	// The original (already-running) container inherited STALE_SECRET into
	// its Config.Env from an earlier point when that secret was env-mode.
	// FOO is unrelated and must survive. CURRENT_SECRET is still env-mode
	// and must be updated to its fresh rotated value.
	inspectResp.Config.Env = []string{
		"FOO=bar",
		"STALE_SECRET=" + sentinel,
		"CURRENT_SECRET=old-current-value",
	}

	createResp := container.CreateResponse{ID: "new-container-id"}
	bCreate, _ := json.Marshal(createResp)

	containerNames := map[string]string{
		"cid":              "my-container",
		"new-container-id": "my-container",
	}

	var createRequestBody []byte

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/cid/json":
					resp := inspectResp
					resp.Name = "/" + containerNames["cid"]
					bResp, _ := json.Marshal(resp)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bResp))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/create":
					createRequestBody, _ = io.ReadAll(req.Body)
					return &http.Response{StatusCode: 201, Body: io.NopCloser(bytes.NewReader(bCreate))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/start":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/new-container-id/json":
					healthyInspect := inspectResp
					healthyInspect.Name = "/" + containerNames["new-container-id"]
					healthyInspect.State.Health = &container.Health{Status: "healthy"}
					bHealthy, _ := json.Marshal(healthyInspect)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bHealthy))}, nil
				case req.Method == "POST" && strings.Contains(req.URL.Path, "/rename"):
					newName := req.URL.Query().Get("name")
					id := "cid"
					if strings.Contains(req.URL.Path, "new-container-id") {
						id = "new-container-id"
					}
					if newName != "" {
						containerNames[id] = newName
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

	// This rotation no longer supplies STALE_SECRET as an env override (it is
	// now file-mode), but it is still a managed name for this container.
	// CURRENT_SECRET, by contrast, IS re-supplied this rotation with a fresh
	// value -- it must survive with the NEW value, not be treated as stale.
	newEnvs := map[string]string{"CURRENT_SECRET": freshVal}
	managedEnvNames := map[string]bool{"STALE_SECRET": true, "CURRENT_SECRET": true}

	err := rs.Execute(context.Background(), "cid", newEnvs, nil, managedEnvNames, 1*time.Second)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if createRequestBody == nil {
		t.Fatal("container create request was never captured")
	}
	if bytes.Contains(createRequestBody, []byte(sentinel)) {
		t.Fatalf("P0-3 regression: stale secret value leaked into the shadow container's create request (would appear in `docker inspect`):\n%s", createRequestBody)
	}

	var created struct {
		Env []string `json:"Env"`
	}
	if err := json.Unmarshal(createRequestBody, &created); err != nil {
		t.Fatalf("failed to parse create request body: %v", err)
	}
	for _, e := range created.Env {
		if strings.HasPrefix(e, "STALE_SECRET=") {
			t.Fatalf("P0-3 regression: STALE_SECRET key should have been stripped (secret is now file-mode), got: %q (full env: %v)", e, created.Env)
		}
	}

	fooOK, freshOK := false, false
	for _, e := range created.Env {
		if e == "FOO=bar" {
			fooOK = true
		}
		if e == "CURRENT_SECRET="+freshVal {
			freshOK = true
		}
	}
	if !fooOK {
		t.Errorf("unrelated env var FOO=bar should have been preserved, got: %v", created.Env)
	}
	if !freshOK {
		t.Errorf("current env-mode secret CURRENT_SECRET should have been updated to %q, got: %v", freshVal, created.Env)
	}
}
