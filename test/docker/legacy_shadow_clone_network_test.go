//go:build dockerint

package docker

// Guarded migration of test/integration/shadow_clone_network_test.go (M0-11).
//
// Regression for a proven cause of client-visible failures during rolling
// rotation: PrepareShadowConfig used to copy the original's EndpointSettings
// (MacAddress, IP, EndpointID), so the shadow ran beside the original with the
// SAME MAC and corrupted the bridge forwarding table. The test clones exactly the
// way rotation does, runs the clone alongside the original while traffic flows
// through the in-process DSO proxy, and asserts identity is not duplicated and the
// original is undisturbed.
//
// Workload: busybox httpd (trafficScript) instead of nginx:alpine; nginx was only a
// "something listening on 80" stand-in. Traffic uses the shared generator from
// traffic_rotation_test.go.
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap/zaptest"

	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

func TestShadowClone_DoesNotDuplicateNetworkIdentity(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	hostPort := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	name := uniqueName(t, "dso-it-shadow-orig")
	args := []string{"run", "-d", "--name", name}
	args = append(args, dockerguard.Current(t).LabelArgs()...)
	args = append(args, "--label", "dso.it.run="+runLabel, testImage, "sh", "-c", trafficScript)
	origID := dockerCmd(t, args...)

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

	gen := startTraffic(fmt.Sprintf("127.0.0.1:%d", hostPort), 2)
	time.Sleep(2 * time.Second)

	// Clone exactly the way the rolling strategy does.
	cloner := rotation.NewContainerCloner(cli)
	cfg, hostCfg, netCfg, _, err := cloner.PrepareShadowConfig(ctx, origID, map[string]string{"ROTATED": "1"}, nil)
	if err != nil {
		gen.finish()
		t.Fatalf("PrepareShadowConfig: %v", err)
	}

	for netName, ep := range netCfg.EndpointsConfig {
		if ep.MacAddress != "" {
			t.Errorf("network %q: shadow config carries the original's MacAddress %q -- "+
				"two live containers would share a MAC and corrupt the bridge", netName, ep.MacAddress)
		}
		if ep.IPAddress != "" {
			t.Errorf("network %q: shadow config carries a fixed IPAddress %q", netName, ep.IPAddress)
		}
		if ep.EndpointID != "" {
			t.Errorf("network %q: shadow config carries the original's EndpointID", netName)
		}
	}
	if cfg.MacAddress != "" { //nolint:staticcheck // deprecated field, asserted deliberately
		t.Errorf("shadow Config carries the deprecated container-level MacAddress %q", cfg.MacAddress) //nolint:staticcheck
	}

	created, err := cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, uniqueName(t, "dso-it-shadow-probe"))
	if err != nil {
		gen.finish()
		t.Fatalf("create clone: %v", err)
	}
	// The clone inherits the original's labels (guard + run label), so label-scoped
	// cleanup covers it; this removal is an extra, id-scoped safety.
	defer func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	}()
	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		gen.finish()
		t.Fatalf("start clone: %v", err)
	}

	cloneInspect, err := cli.ContainerInspect(ctx, created.ID)
	if err != nil {
		gen.finish()
		t.Fatalf("inspect clone: %v", err)
	}
	for netName, ep := range cloneInspect.NetworkSettings.Networks {
		t.Logf("original network=%s ip=%s mac=%s", netName, origIP, origMAC)
		t.Logf("clone    network=%s ip=%s mac=%s", netName, ep.IPAddress, ep.MacAddress)
		if ep.MacAddress != "" && ep.MacAddress == origMAC {
			t.Errorf("clone shares the original's MAC %s on network %q", origMAC, netName)
		}
		if ep.IPAddress != "" && ep.IPAddress == origIP {
			t.Errorf("clone shares the original's IP %s on network %q", origIP, netName)
		}
	}

	// The original must keep serving undisturbed while the clone runs.
	time.Sleep(6 * time.Second)
	samples := gen.finish()

	total, ok := len(samples), 0
	kinds := map[string]int{}
	for _, s := range samples {
		if s.ok {
			ok++
		} else {
			kinds[s.failKind]++
		}
	}
	t.Logf("traffic to ORIGINAL while clone runs alongside: total=%d ok=%d failed=%d failure kinds=%v", total, ok, total-ok, kinds)
	if total < 100 {
		t.Fatalf("too few requests (%d)", total)
	}
	if total-ok > 0 {
		t.Errorf("running a shadow clone alongside the original disturbed %d/%d requests to the original", total-ok, total)
	}
}
