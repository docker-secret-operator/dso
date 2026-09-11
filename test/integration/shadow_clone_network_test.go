package integration

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap/zaptest"

	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/rotation"
)

// TestShadowClone_DoesNotDuplicateNetworkIdentity is a regression test for a
// proven cause of client-visible failures during rolling rotation.
//
// PrepareShadowConfig used to copy the original container's EndpointSettings
// verbatim, including its MacAddress. Because the shadow runs ALONGSIDE the
// original for the duration of the health check, that put two containers on
// the bridge with the same MAC, corrupting the bridge forwarding table:
// traffic to the ORIGINAL (healthy, still-serving) container started failing
// with "no route to host"/dial timeouts.
//
// The test starts a real container, clones it exactly the way rotation does,
// runs the clone alongside it, and asserts both that the identity is not
// duplicated and that traffic to the original is undisturbed.
func TestShadowClone_DoesNotDuplicateNetworkIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	hostPort := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	app, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nginx:alpine",
			ExposedPorts: []string{"80/tcp"},
			WaitingFor:   wait.ForListeningPort("80/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start app: %v", err)
	}
	defer func() { _ = app.Terminate(ctx) }()

	origID := app.GetContainerID()
	origInspect, err := cli.ContainerInspect(ctx, origID)
	if err != nil {
		t.Fatalf("inspect original: %v", err)
	}

	origIP, origMAC := "", ""
	for _, ep := range origInspect.NetworkSettings.Networks {
		if ep.IPAddress != "" {
			origIP, origMAC = ep.IPAddress, ep.MacAddress
			break
		}
	}
	if origIP == "" {
		t.Fatal("original container has no IP")
	}

	pm := dsoProxy.NewManager(zaptest.NewLogger(t))
	defer func() { _ = pm.Stop(2 * time.Second) }()
	if err := pm.EnsurePort(hostPort, 80); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := pm.RegisterContainer(origID, origIP, hostPort, 80); err != nil {
		t.Fatalf("register: %v", err)
	}

	stop := make(chan struct{})
	var results []trafficResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results = hammerProxy(hostPort, stop, 2*time.Millisecond)
	}()
	time.Sleep(2 * time.Second)

	// Clone exactly the way the rolling strategy does.
	cloner := rotation.NewContainerCloner(cli)
	cfg, hostCfg, netCfg, _, err := cloner.PrepareShadowConfig(ctx, origID, map[string]string{"ROTATED": "1"})
	if err != nil {
		t.Fatalf("PrepareShadowConfig: %v", err)
	}

	for name, ep := range netCfg.EndpointsConfig {
		if ep.MacAddress != "" {
			t.Errorf("network %q: shadow config carries the original's MacAddress %q -- "+
				"two live containers would share a MAC and corrupt the bridge", name, ep.MacAddress)
		}
		if ep.IPAddress != "" {
			t.Errorf("network %q: shadow config carries a fixed IPAddress %q", name, ep.IPAddress)
		}
		if ep.EndpointID != "" {
			t.Errorf("network %q: shadow config carries the original's EndpointID", name)
		}
	}
	if cfg.MacAddress != "" { //nolint:staticcheck // deprecated field, asserted deliberately
		t.Errorf("shadow Config carries the deprecated container-level MacAddress %q", cfg.MacAddress) //nolint:staticcheck
	}

	created, err := cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, "dso_shadow_identity_probe")
	if err != nil {
		t.Fatalf("create clone: %v", err)
	}
	defer func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	}()
	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start clone: %v", err)
	}

	cloneInspect, err := cli.ContainerInspect(ctx, created.ID)
	if err != nil {
		t.Fatalf("inspect clone: %v", err)
	}
	for name, ep := range cloneInspect.NetworkSettings.Networks {
		t.Logf("original network=%s ip=%s mac=%s", name, origIP, origMAC)
		t.Logf("clone    network=%s ip=%s mac=%s", name, ep.IPAddress, ep.MacAddress)
		if ep.MacAddress != "" && ep.MacAddress == origMAC {
			t.Errorf("clone shares the original's MAC %s on network %q", origMAC, name)
		}
	}

	// The original must keep serving undisturbed while the clone runs.
	time.Sleep(6 * time.Second)
	close(stop)
	wg.Wait()

	total, ok, kinds := summarize(results)
	t.Logf("traffic to ORIGINAL while clone runs alongside: total=%d ok=%d failed=%d", total, ok, total-ok)
	for k, v := range kinds {
		t.Logf("  failure %-42s %d", k, v)
	}
	if total < 100 {
		t.Fatalf("too few requests (%d)", total)
	}
	if total-ok > 0 {
		t.Errorf("running a shadow clone alongside the original disturbed %d/%d requests to the original",
			total-ok, total)
	}
}
