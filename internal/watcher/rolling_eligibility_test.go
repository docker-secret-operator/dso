package watcher

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
)

func inspectWithPorts(pb nat.PortMap, mutate func(*container.InspectResponse)) container.InspectResponse {
	in := minimalInspect("app", "c1")
	in.HostConfig.PortBindings = pb
	in.Config.Labels = map[string]string{}
	if mutate != nil {
		mutate(&in)
	}
	return in
}

func TestRollingEligibility(t *testing.T) {
	cases := []struct {
		name   string
		in     container.InspectResponse
		reject bool
		why    string
	}{
		{"no ports at all", inspectWithPorts(nil, nil), false, ""},
		{"proxy-managed only (compose stripped the bindings)", inspectWithPorts(nil, func(i *container.InspectResponse) { i.Config.Labels["dso.host_ports"] = "127.0.0.1:5432:5432" }), false, ""},
		{"direct fixed tcp port", inspectWithPorts(nat.PortMap{"80/tcp": {{HostPort: "8080"}}}, nil), true, "host port 8080"},
		{"direct loopback port", inspectWithPorts(nat.PortMap{"5432/tcp": {{HostIP: "127.0.0.1", HostPort: "5432"}}}, nil), true, "127.0.0.1:5432"},
		{"udp", inspectWithPorts(nat.PortMap{"53/udp": {{HostPort: "5353"}}}, nil), true, "53/udp"},
		{"auto-assigned host port", inspectWithPorts(nat.PortMap{"80/tcp": {{HostPort: ""}}}, nil), true, "auto-assigned"},
		{"privileged host port", inspectWithPorts(nat.PortMap{"80/tcp": {{HostPort: "80"}}}, nil), true, "host port 80"},
		{"mixed proxy-managed and direct", inspectWithPorts(nat.PortMap{"53/udp": {{HostPort: "5353"}}}, func(i *container.InspectResponse) { i.Config.Labels["dso.host_ports"] = "8080:80" }), true, "mixes proxy-managed"},
		{"mixed tcp and udp direct", inspectWithPorts(nat.PortMap{"80/tcp": {{HostPort: "8080"}}, "53/udp": {{HostPort: "5353"}}}, nil), true, "53/udp"},
		{"publish all ports", inspectWithPorts(nil, func(i *container.InspectResponse) { i.HostConfig.PublishAllPorts = true }), true, "all ports"},
		{"host networking", inspectWithPorts(nil, func(i *container.InspectResponse) { i.HostConfig.NetworkMode = "host" }), true, "host networking"},
		{"nil host config", container.InspectResponse{}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := rollingEligibility(c.in)
			if c.reject {
				if !errors.Is(err, ErrRollingNotSupported) || !strings.Contains(err.Error(), c.why) {
					t.Fatalf("want ErrRollingNotSupported mentioning %q, got %v", c.why, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("must be eligible: %v", err)
			}
		})
	}
}

// End to end through TriggerReload: a rolling rotation of a container with a
// Docker-published port is rejected BEFORE any create/stop/rename/kill/remove, the
// outcome is an error, and the strategy is not silently switched.
func TestTriggerReload_RollingRejectedBeforeMutationForDirectPublishedPorts(t *testing.T) {
	var mu sync.Mutex
	var mutations []string
	in := inspectWithPorts(nat.PortMap{"5432/tcp": {{HostIP: "127.0.0.1", HostPort: "5432"}}}, nil)
	body, _ := json.Marshal(in)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/c1/json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			mu.Lock()
			mutations = append(mutations, r.Method+" "+r.URL.Path)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r := newMockController(t, h)
	r.Targets.Store("c1", &TargetContainer{ID: "c1", Strategy: "rolling", Secrets: []string{"db"}})
	err := waitOutcome(t, r, "db")
	if !errors.Is(err, ErrRollingNotSupported) {
		t.Fatalf("want ErrRollingNotSupported, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(mutations) != 0 {
		t.Fatalf("a rejected rotation must not mutate anything, saw %v", mutations)
	}
	if _, bad := r.IsDegraded("app"); bad {
		t.Fatal("nothing was changed, the service must not be marked degraded")
	}
}
