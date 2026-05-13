package rotation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	bInspect, _ := json.Marshal(inspectResp)

	createResp := container.CreateResponse{
		ID: "new-container-id",
	}
	bCreate, _ := json.Marshal(createResp)

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/cid/json":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bInspect))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/cid/rename":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/create":
					return &http.Response{StatusCode: 201, Body: io.NopCloser(bytes.NewReader(bCreate))}, nil
				case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-container-id/start":
					return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
				case req.Method == "GET" && req.URL.Path == "/v1.41/containers/new-container-id/json":
					healthyInspect := inspectResp
					healthyInspect.State.Health = &container.Health{Status: "healthy"}
					bHealthy, _ := json.Marshal(healthyInspect)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bHealthy))}, nil
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
