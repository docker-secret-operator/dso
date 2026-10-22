package rotation

// SEC-P0 Test 7: replacement-container ownership preservation. Every
// container-recreation path (rolling via PrepareShadowConfig, restart/
// fallback via watcher.cloneContainerConfig -- which mirrors the same
// verbatim-Labels-copy documented on PrepareShadowConfig) must carry the
// original container's dso.owner label forward onto its replacement, or a
// successful rotation would silently turn an owned container into an
// unowned ("legacy") one that no agent would ever discover or manage again.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func TestPrepareShadowConfig_PreservesOwnerLabel(t *testing.T) {
	inspectResp := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			Name:       "/my-container",
			HostConfig: &container.HostConfig{},
			State:      &container.State{Running: true},
		},
		Config: &container.Config{
			Image: "nginx",
			Env:   []string{"EXISTING=true"},
			Labels: map[string]string{
				"dso.reloader":        "true",
				"dso.secrets":         "test_secret_a",
				"dso.owner":           "owner-A",
				"dso.update.strategy": "rolling",
			},
		},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{"bridge": {}},
		},
	}
	body, _ := json.Marshal(inspectResp)

	httpClient := &http.Client{
		Transport: &mockTransport{
			reqFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
			},
		},
	}
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"), client.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("failed to build docker client: %v", err)
	}

	cloner := NewContainerCloner(cli)
	newConfig, _, _, _, err := cloner.PrepareShadowConfig(context.Background(), "cid", map[string]string{"NEW": "val"}, nil)
	if err != nil {
		t.Fatalf("PrepareShadowConfig failed: %v", err)
	}

	if got := newConfig.Labels["dso.owner"]; got != "owner-A" {
		t.Errorf("replacement container must preserve dso.owner label, got %q want %q", got, "owner-A")
	}
	if got := newConfig.Labels["dso.reloader"]; got != "true" {
		t.Errorf("replacement container must preserve dso.reloader label, got %q", got)
	}
}
