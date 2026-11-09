package rotation

// M1: WaitHealthy and ExecProbe classify readiness rejections with
// ErrUnhealthy, and only those -- Docker API errors and context cancellation
// stay unclassified. RollingStrategy.Execute must keep the classification
// through its own wrap so internal/watcher can skip the fallback restart for
// a rejected secret without string matching.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func jsonResp(status int, v interface{}) *http.Response {
	var b []byte
	if v != nil {
		b, _ = json.Marshal(v)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{"Content-Type": {"application/json"}}}
}

func apiErrResp() *http.Response {
	return jsonResp(http.StatusInternalServerError, map[string]string{"message": "simulated daemon error"})
}

func fakeClient(t *testing.T, fn func(req *http.Request) *http.Response) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"),
		client.WithHTTPClient(&http.Client{Transport: &mockTransport{reqFunc: func(req *http.Request) (*http.Response, error) {
			return fn(req), nil
		}}}))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return cli
}

// inspectWithState answers a container inspect with the given state.
func inspectWithState(st *container.State) *http.Response {
	in := getBaseInspect()
	in.State = st
	return jsonResp(http.StatusOK, in)
}

func TestWaitHealthy_ReadinessRejection_IsErrUnhealthy(t *testing.T) {
	cases := []struct {
		name    string
		state   *container.State
		timeout time.Duration
	}{
		{"unhealthy", &container.State{Running: true, Health: &container.Health{Status: "unhealthy"}}, time.Second},
		{"restarting", &container.State{Running: true, Restarting: true}, time.Second},
		{"exited", &container.State{Running: false}, time.Second},
		// A zero timeout skips the poll loop: these exercise the two
		// timeout returns without sleeping.
		{"timeout_no_healthcheck", &container.State{Running: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cli := fakeClient(t, func(*http.Request) *http.Response { return inspectWithState(tc.state) })
			err := WaitHealthy(context.Background(), cli, "c", tc.timeout)
			if !errors.Is(err, ErrUnhealthy) {
				t.Fatalf("WaitHealthy err = %v, want errors.Is(err, ErrUnhealthy)", err)
			}
		})
	}
}

// The health-check-defined timeout path: "starting" for the whole window.
func TestWaitHealthy_NeverHealthyTimeout_IsErrUnhealthy(t *testing.T) {
	cli := fakeClient(t, func(*http.Request) *http.Response {
		return inspectWithState(&container.State{Running: true, Health: &container.Health{Status: "starting"}})
	})
	err := WaitHealthy(context.Background(), cli, "c", 10*time.Millisecond)
	if !errors.Is(err, ErrUnhealthy) || !strings.Contains(err.Error(), "never reached healthy state") {
		t.Fatalf("WaitHealthy err = %v, want health-check timeout classified ErrUnhealthy", err)
	}
}

func TestWaitHealthy_NonHealthFailures_AreNotErrUnhealthy(t *testing.T) {
	t.Run("inspect_api_error", func(t *testing.T) {
		cli := fakeClient(t, func(*http.Request) *http.Response { return apiErrResp() })
		err := WaitHealthy(context.Background(), cli, "c", time.Second)
		if err == nil || errors.Is(err, ErrUnhealthy) {
			t.Fatalf("WaitHealthy err = %v, want a non-ErrUnhealthy failure for a Docker API error", err)
		}
	})
	t.Run("context_cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cli := fakeClient(t, func(*http.Request) *http.Response {
			cancel() // cancelled while the container is still "starting"
			return inspectWithState(&container.State{Running: true, Health: &container.Health{Status: "starting"}})
		})
		err := WaitHealthy(ctx, cli, "c", time.Minute)
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnhealthy) {
			t.Fatalf("WaitHealthy err = %v, want context.Canceled and not ErrUnhealthy", err)
		}
	})
}

// execFake answers ExecProbe's exec create/start/inspect; createErr makes the
// create call fail with a daemon error, otherwise the probe exits exitCode.
func execFake(t *testing.T, createErr bool, exitCode int) *client.Client {
	return fakeClient(t, func(req *http.Request) *http.Response {
		p := req.URL.Path
		switch {
		case strings.HasSuffix(p, "/containers/c/exec"):
			if createErr {
				return apiErrResp()
			}
			return jsonResp(http.StatusCreated, map[string]string{"Id": "exec-1"})
		case strings.HasSuffix(p, "/exec/exec-1/start"):
			return jsonResp(http.StatusOK, nil)
		case strings.HasSuffix(p, "/exec/exec-1/json"):
			return jsonResp(http.StatusOK, map[string]interface{}{"ExitCode": exitCode, "Running": false})
		default:
			return jsonResp(http.StatusNoContent, nil)
		}
	})
}

func TestExecProbe_SecretNeverPresent_IsErrUnhealthy(t *testing.T) {
	err := ExecProbe(context.Background(), execFake(t, false, 1), "c", "/run/secrets/k", time.Millisecond, 1)
	if !errors.Is(err, ErrUnhealthy) || !strings.Contains(err.Error(), "exec probe failed for /run/secrets/k after 1 retries") {
		t.Fatalf("ExecProbe err = %v, want probe rejection classified ErrUnhealthy with its diagnostic text", err)
	}
}

func TestExecProbe_DockerAPIError_IsNotErrUnhealthy(t *testing.T) {
	err := ExecProbe(context.Background(), execFake(t, true, 0), "c", "/run/secrets/k", time.Millisecond, 1)
	if err == nil || errors.Is(err, ErrUnhealthy) {
		t.Fatalf("ExecProbe err = %v, want a non-ErrUnhealthy failure for a Docker API error", err)
	}
}

// executeFake drives RollingStrategy.Execute for original "cid" and shadow
// "new-container-id": the shadow reports shadowHealth, or its start fails.
func executeFake(t *testing.T, shadowHealth string, startErr bool) *client.Client {
	return fakeClient(t, func(req *http.Request) *http.Response {
		p := req.URL.Path
		switch {
		case req.Method == http.MethodGet && strings.HasSuffix(p, "/containers/cid/json"):
			return jsonResp(http.StatusOK, getBaseInspect())
		case strings.HasSuffix(p, "/containers/create"):
			return jsonResp(http.StatusCreated, container.CreateResponse{ID: "new-container-id"})
		case strings.HasSuffix(p, "/containers/new-container-id/start") && startErr:
			return apiErrResp()
		case req.Method == http.MethodGet && strings.HasSuffix(p, "/containers/new-container-id/json"):
			return inspectWithState(&container.State{Running: true, Health: &container.Health{Status: shadowHealth}})
		default:
			return jsonResp(http.StatusNoContent, nil)
		}
	})
}

func TestRollingStrategy_Execute_HealthRejection_PreservesErrUnhealthy(t *testing.T) {
	rs := NewRollingStrategy(executeFake(t, "unhealthy", false))
	err := rs.Execute(context.Background(), "cid", map[string]string{"K": "v"}, nil, nil, time.Second)
	if !errors.Is(err, ErrUnhealthy) || !strings.Contains(err.Error(), "health verification failed") {
		t.Fatalf("Execute err = %v, want the health rejection to keep ErrUnhealthy through Execute's wrap", err)
	}
}

func TestRollingStrategy_Execute_StartFailure_IsNotErrUnhealthy(t *testing.T) {
	rs := NewRollingStrategy(executeFake(t, "healthy", true))
	err := rs.Execute(context.Background(), "cid", map[string]string{"K": "v"}, nil, nil, time.Second)
	if err == nil || errors.Is(err, ErrUnhealthy) || !strings.Contains(err.Error(), "failed to start new container") {
		t.Fatalf("Execute err = %v, want an unclassified start failure", err)
	}
}
