package injector

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
	"github.com/docker/docker/client"
)

type mockTransport struct {
	reqFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.reqFunc(req)
}

func TestInjectOneFile_Success(t *testing.T) {
	execCreateResponse := types.IDResponse{ID: "mock-exec-id"}
	b1, _ := json.Marshal(execCreateResponse)

	execInspectResponse := container.ExecInspect{
		Running:  false,
		ExitCode: 0,
	}
	b2, _ := json.Marshal(execInspectResponse)

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/v1.41/containers/cid/exec" {
					return &http.Response{
						StatusCode: 201,
						Body:       io.NopCloser(bytes.NewReader(b1)),
					}, nil
				}
				if req.URL.Path == "/v1.41/exec/mock-exec-id/start" {
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewReader([]byte{})),
					}, nil
				}
				if req.URL.Path == "/v1.41/exec/mock-exec-id/json" {
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewReader(b2)),
					}, nil
				}
				return &http.Response{StatusCode: 404, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
			},
		},
	}

	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))

	err := injectOneFile(context.Background(), cli, "cid", "test.txt", "content", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInjectOneFile_Timeout(t *testing.T) {
	execCreateResponse := types.IDResponse{ID: "mock-exec-id"}
	b1, _ := json.Marshal(execCreateResponse)

	execInspectResponse := container.ExecInspect{
		Running:  true,
		ExitCode: 0,
	}
	b2, _ := json.Marshal(execInspectResponse)

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/v1.41/containers/cid/exec" {
					return &http.Response{
						StatusCode: 201,
						Body:       io.NopCloser(bytes.NewReader(b1)),
					}, nil
				}
				if req.URL.Path == "/v1.41/exec/mock-exec-id/start" {
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewReader([]byte{})),
					}, nil
				}
				if req.URL.Path == "/v1.41/exec/mock-exec-id/json" {
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewReader(b2)),
					}, nil
				}
				return &http.Response{StatusCode: 404, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
			},
		},
	}

	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := injectOneFile(ctx, cli, "cid", "test.txt", "content", 0, 0)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
