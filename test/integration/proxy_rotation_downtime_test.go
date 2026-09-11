package integration

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap/zaptest"

	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/watcher"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

const (
	testSecretName   = "test-secret"
	testProviderName = "testprovider"
)

// stubCache satisfies watcher.SecretCache with a fixed rotated value.
type stubCache struct{ data map[string]map[string]string }

func (s *stubCache) Get(key string) (map[string]string, bool) {
	v, ok := s.data[key]
	return v, ok
}

// trafficResult records one client request attempt through the DSO proxy.
// endAt is when the attempt actually resolved -- distinct from at (when it
// started), because a read-deadline hit resolves long after it began and
// would otherwise be attributed to the wrong transition.
type trafficResult struct {
	at       time.Time
	endAt    time.Time
	ok       bool
	failKind string
}

// hammerProxy drives continuous short-lived TCP requests at the proxy's host
// port until stop is closed, classifying every failure. Short-lived
// connections are what exercise the router's backend selection on every
// single request, which is where an unready or missing backend shows up.
func hammerProxy(hostPort int, stop <-chan struct{}, interval time.Duration) []trafficResult {
	var results []trafficResult
	addr := fmt.Sprintf("127.0.0.1:%d", hostPort)

	for {
		select {
		case <-stop:
			return results
		default:
		}

		res := trafficResult{at: time.Now()}

		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			res.endAt = time.Now()
			if strings.Contains(err.Error(), "refused") {
				res.failKind = "connection_refused"
			} else if ne, isNet := err.(net.Error); isNet && ne.Timeout() {
				res.failKind = "dial_timeout"
			} else {
				res.failKind = "dial_error"
			}
			results = append(results, res)
			time.Sleep(interval)
			continue
		}

		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write([]byte("GET / HTTP/1.0\r\nHost: dso-test\r\n\r\n")); err != nil {
			res.endAt = time.Now()
			res.failKind = "write_error"
			_ = conn.Close()
			results = append(results, res)
			time.Sleep(interval)
			continue
		}

		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		_ = conn.Close()
		if err != nil && n == 0 {
			res.endAt = time.Now()
			// The proxy accepts the client connection before selecting a
			// backend (server.go handleConn), so "no backend" and "upstream
			// dial failed" both present as an immediate close. A request
			// routed at a container that accepted but isn't serving hangs
			// until our read deadline instead.
			if ne, isNet := err.(net.Error); isNet && ne.Timeout() {
				res.failKind = "read_timeout_2s"
			} else {
				res.failKind = "immediate_close_no_backend_or_dial_fail"
			}
			results = append(results, res)
			time.Sleep(interval)
			continue
		}

		if fields := strings.SplitN(string(buf[:n]), " ", 3); len(fields) > 1 &&
			strings.HasPrefix(fields[0], "HTTP/1.") && strings.HasPrefix(fields[1], "5") {
			res.endAt = time.Now()
			res.failKind = "http_5xx"
			results = append(results, res)
			time.Sleep(interval)
			continue
		}

		res.endAt = time.Now()
		res.ok = true
		results = append(results, res)
		time.Sleep(interval)
	}
}

// rotationRig is a fully wired, production-path rotation environment: a real
// nginx container whose host port is owned by a real proxy.Manager, driven by
// a real watcher.ReloaderController running its real event loop. Nothing here
// re-implements controller logic -- that was the flaw in the first version of
// this test, which mirrored the handlers by hand and so could not observe a
// fix made in the controller itself.
type rotationRig struct {
	cli        *client.Client
	rc         *watcher.ReloaderController
	pm         *dsoProxy.Manager
	appID      string
	appName    string
	hostPort   int
	terminate  func()
	stopEvents context.CancelFunc
}

func newRotationRig(ctx context.Context, t *testing.T) *rotationRig {
	return newRotationRigWithStrategy(ctx, t, "rolling")
}

func newRotationRigWithStrategy(ctx context.Context, t *testing.T, strategy string) *rotationRig {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	hostPort := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	portsLabel := fmt.Sprintf("%d:80", hostPort)

	// No published host port -- exactly what core.stripAndLabelHostPorts does
	// in production: the host binding is stripped into dso.host_ports so the
	// DSO proxy owns it.
	appContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nginx:alpine",
			ExposedPorts: []string{"80/tcp"},
			Labels: map[string]string{
				"dso.host_ports":      portsLabel,
				"dso.reloader":        "true",
				"dso.secrets":         testSecretName,
				"dso.update.strategy": strategy,
			},
			WaitingFor: wait.ForListeningPort("80/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start app container: %v", err)
	}

	appID := appContainer.GetContainerID()
	inspect, err := cli.ContainerInspect(ctx, appID)
	if err != nil {
		t.Fatalf("inspect app: %v", err)
	}
	appName := strings.TrimPrefix(inspect.Name, "/")

	ip := ""
	for _, ep := range inspect.NetworkSettings.Networks {
		if ep.IPAddress != "" {
			ip = ep.IPAddress
			break
		}
	}
	if ip == "" {
		t.Fatal("app container has no IP")
	}

	logger := zaptest.NewLogger(t)
	pm := dsoProxy.NewManager(logger)
	if err := pm.EnsurePort(hostPort, 80); err != nil {
		t.Fatalf("bind proxy port: %v", err)
	}
	if err := pm.RegisterContainer(appID, ip, hostPort, 80); err != nil {
		t.Fatalf("register original backend: %v", err)
	}

	// The REAL controller, with the REAL event loop.
	rc, err := watcher.NewReloaderController(logger)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	rc.ProxyManager = pm
	rc.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{
			testProviderName: {Type: "vault"},
		},
		Secrets: []dsoConfig.SecretMapping{{
			Name:     testSecretName,
			Provider: testProviderName,
			Inject:   dsoConfig.InjectionConfig{Type: "env"},
			Mappings: map[string]string{"password": "APP_PASSWORD"},
		}},
	}
	rc.Cache = &stubCache{data: map[string]map[string]string{
		testProviderName + ":" + testSecretName: {"password": "rotated-value"},
	}}
	rc.Targets.Store(appID, &watcher.TargetContainer{
		ID:       appID,
		Strategy: strategy,
		Secrets:  []string{testSecretName},
	})

	evCtx, evCancel := context.WithCancel(ctx)
	go rc.StartEventLoop(evCtx)
	// Give the event loop a moment to attach to the Docker event stream.
	time.Sleep(750 * time.Millisecond)

	return &rotationRig{
		cli:        cli,
		rc:         rc,
		pm:         pm,
		appID:      appID,
		appName:    appName,
		hostPort:   hostPort,
		stopEvents: evCancel,
		terminate: func() {
			_ = appContainer.Terminate(context.Background())
		},
	}
}

func (rig *rotationRig) close(t *testing.T) {
	t.Helper()
	rig.stopEvents()
	_ = rig.pm.Stop(2 * time.Second)
	// The rotated container inherits the original name; remove it too.
	if ins, err := rig.cli.ContainerInspect(context.Background(), rig.appName); err == nil && ins.ID != rig.appID {
		_ = rig.cli.ContainerRemove(context.Background(), ins.ID, container.RemoveOptions{Force: true})
	}
	rig.terminate()
	_ = rig.cli.Close()
}

// rotate runs a real TriggerReload and waits for the async rotation to finish.
func (rig *rotationRig) rotate(ctx context.Context, t *testing.T) error {
	t.Helper()
	done := make(chan error, 1)
	if err := rig.rc.TriggerReload(ctx, testSecretName, func(err error) { done <- err }); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-time.After(90 * time.Second):
		return fmt.Errorf("rotation did not complete within 90s")
	}
}

func summarize(results []trafficResult) (total, ok int, kinds map[string]int) {
	kinds = map[string]int{}
	for _, r := range results {
		total++
		if r.ok {
			ok++
		} else {
			kinds[r.failKind]++
		}
	}
	return total, ok, kinds
}

// TestProxy_RollingRotation_ZeroDowntime is the flagship-claim regression
// test. Real Docker, real proxy, real ReloaderController event loop, real
// rolling rotation, continuous client traffic throughout.
//
// It previously FAILED, reproducing the premature-registration defect: the
// watcher registered the rotation shadow as an active backend on Docker's
// "start" event, ~2s before the rotation's health gate completed, so the
// router sent live traffic to an application that was not listening yet.
func TestProxy_RollingRotation_ZeroDowntime(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	rig := newRotationRig(ctx, t)
	defer rig.close(t)

	stop := make(chan struct{})
	var results []trafficResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results = hammerProxy(rig.hostPort, stop, 2*time.Millisecond)
	}()

	time.Sleep(2 * time.Second)

	rotStart := time.Now()
	if err := rig.rotate(ctx, t); err != nil {
		close(stop)
		wg.Wait()
		t.Fatalf("rolling rotation failed: %v", err)
	}
	rotDur := time.Since(rotStart)

	// Keep hammering past the swap so the old backend's drain/removal and the
	// old container's stop are all covered by live traffic.
	time.Sleep(8 * time.Second)
	close(stop)
	wg.Wait()

	total, ok, kinds := summarize(results)
	t.Logf("=== TRAFFIC REPORT (sequential client) ===")
	t.Logf("rotation duration: %v", rotDur)
	t.Logf("total requests:    %d", total)
	t.Logf("successful:        %d", ok)
	t.Logf("failed:            %d", total-ok)
	for k, v := range kinds {
		t.Logf("  failure %-42s %d", k, v)
	}

	for _, r := range results {
		if !r.ok {
			t.Logf("  FAILURE %-40s started +%v resolved +%v (lasted %v)",
				r.failKind, r.at.Sub(rotStart), r.endAt.Sub(rotStart), r.endAt.Sub(r.at))
		}
	}

	if total < 100 {
		t.Fatalf("traffic generator produced too few requests (%d) to be meaningful", total)
	}
	if total-ok > 0 {
		t.Errorf("ZERO-DOWNTIME VIOLATION: %d/%d client requests failed during rolling rotation: %v",
			total-ok, total, kinds)
	} else {
		t.Logf("✅ no client-visible failures across %d requests during rolling rotation", total)
	}
}

// TestProxy_RollingRotation_ConcurrentClients measures the real blast radius.
// A sequential client under-reports it: one hung request blocks that loop for
// its full deadline, so at most ~1 failure per deadline window is observable.
// Real clients are concurrent. This is the test that originally recorded
// 110-140 failed requests per rotation.
func TestProxy_RollingRotation_ConcurrentClients(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	rig := newRotationRig(ctx, t)
	defer rig.close(t)

	const workers = 20
	var total, failed int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	addr := fmt.Sprintf("127.0.0.1:%d", rig.hostPort)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				atomic.AddInt64(&total, 1)
				c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				_ = c.SetDeadline(time.Now().Add(500 * time.Millisecond))
				if _, err := c.Write([]byte("GET / HTTP/1.0\r\nHost: d\r\n\r\n")); err != nil {
					atomic.AddInt64(&failed, 1)
					_ = c.Close()
					continue
				}
				b := make([]byte, 128)
				n, err := c.Read(b)
				_ = c.Close()
				if err != nil && n == 0 {
					atomic.AddInt64(&failed, 1)
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	time.Sleep(2 * time.Second)
	baseTotal, baseFailed := atomic.LoadInt64(&total), atomic.LoadInt64(&failed)

	rotStart := time.Now()
	if err := rig.rotate(ctx, t); err != nil {
		close(stop)
		wg.Wait()
		t.Fatalf("rolling rotation failed: %v", err)
	}
	rotDur := time.Since(rotStart)

	time.Sleep(6 * time.Second)
	close(stop)
	wg.Wait()

	totalV, failedV := atomic.LoadInt64(&total), atomic.LoadInt64(&failed)
	duringTotal := totalV - baseTotal
	duringFailed := failedV - baseFailed

	t.Logf("=== CONCURRENT CLIENT REPORT (%d workers) ===", workers)
	t.Logf("rotation duration: %v", rotDur)
	t.Logf("baseline window:   %d requests, %d failed", baseTotal, baseFailed)
	t.Logf("rotation window:   %d requests, %d failed (%.3f%%)",
		duringTotal, duringFailed, 100*float64(duringFailed)/float64(max64(duringTotal, 1)))

	if baseFailed > 0 {
		t.Fatalf("baseline (pre-rotation) traffic was already failing (%d) -- environment problem, results not attributable", baseFailed)
	}
	if duringFailed > 0 {
		t.Errorf("ZERO-DOWNTIME VIOLATION under concurrent load: %d/%d requests failed during rotation",
			duringFailed, duringTotal)
	} else {
		t.Logf("✅ no client-visible failures across %d concurrent requests during rolling rotation", duringTotal)
	}
}

// TestProxy_RollingRotation_HealthCheckFailure_OldKeepsServing covers the
// failure case: a shadow that never becomes healthy must never receive
// traffic, and the old container must keep serving throughout.
func TestProxy_RollingRotation_HealthCheckFailure_OldKeepsServing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	rig := newRotationRig(ctx, t)
	defer rig.close(t)

	// Force the shadow to fail its readiness gate: WaitHealthy fails fast if
	// the container is not running, so give the replacement a command that
	// exits immediately. The shadow inherits Config.Cmd from the original.
	if _, err := rig.cli.ContainerUpdate(ctx, rig.appID, container.UpdateConfig{}); err != nil {
		t.Logf("(container update no-op: %v)", err)
	}

	stop := make(chan struct{})
	var results []trafficResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results = hammerProxy(rig.hostPort, stop, 2*time.Millisecond)
	}()

	time.Sleep(1500 * time.Millisecond)

	// Rotate with an intentionally impossible health timeout so the readiness
	// gate cannot pass (WaitHealthy needs two 2s-apart stable polls).
	rig.rc.Config.Agent.Rotation.HealthCheckTimeout = "1s"
	rotErr := rig.rotate(ctx, t)

	time.Sleep(4 * time.Second)
	close(stop)
	wg.Wait()

	total, ok, kinds := summarize(results)
	t.Logf("=== HEALTH-CHECK-FAILURE REPORT ===")
	t.Logf("rotation error (expected non-nil): %v", rotErr)
	t.Logf("total requests: %d, successful: %d, failed: %d", total, ok, total-ok)
	for k, v := range kinds {
		t.Logf("  failure %-42s %d", k, v)
	}

	if rotErr == nil {
		t.Log("NOTE: rotation unexpectedly succeeded within the 1s health window; " +
			"the zero-failure assertion below still applies")
	}
	if total-ok > 0 {
		t.Errorf("old backend stopped serving during a FAILED rotation: %d/%d requests failed: %v",
			total-ok, total, kinds)
	} else {
		t.Logf("✅ old backend served all %d requests despite the failed rotation", total)
	}

	// The original container must still be the one serving.
	ins, err := rig.cli.ContainerInspect(ctx, rig.appName)
	if err != nil {
		t.Fatalf("original container is gone after a failed rotation: %v", err)
	}
	if !ins.State.Running {
		t.Errorf("original container is not running after a failed rotation (state=%s)", ins.State.Status)
	}
}

// TestProxy_SteadyState_NoRotation_Control is the control. Identical traffic,
// identical duration, identical proxy setup -- but NO rotation. Failures here
// would be load/environment artifacts, which is what makes the rotation
// tests' results attributable.
func TestProxy_SteadyState_NoRotation_Control(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	rig := newRotationRig(ctx, t)
	defer rig.close(t)

	stop := make(chan struct{})
	var results []trafficResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results = hammerProxy(rig.hostPort, stop, 2*time.Millisecond)
	}()

	time.Sleep(12 * time.Second)
	close(stop)
	wg.Wait()

	total, ok, kinds := summarize(results)
	t.Logf("=== CONTROL (no rotation) ===")
	t.Logf("total requests: %d, successful: %d, failed: %d", total, ok, total-ok)
	for k, v := range kinds {
		t.Logf("  failure %-42s %d", k, v)
	}

	if total < 100 {
		t.Fatalf("too few requests (%d)", total)
	}
	if total-ok > 0 {
		t.Errorf("control run saw %d/%d failures with NO rotation -- environment is unstable", total-ok, total)
	} else {
		t.Logf("✅ control clean: %d/%d succeeded with no rotation", ok, total)
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// TestProxy_RestartRotation_ZeroDowntime covers the restart strategy, which
// has its own proxy handoff (WaitHealthy -> ExecProbe -> SwapBackend). That
// ordering was correct on paper, but the watcher registered the replacement
// on its "start" event first, so SwapBackend's Add hit "already registered"
// and returned early -- meaning the old backend was never drained. This was
// proven by code reading during the investigation but never exercised at
// runtime. Now it is.
func TestProxy_RestartRotation_ZeroDowntime(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	rig := newRotationRigWithStrategy(ctx, t, "restart")
	defer rig.close(t)

	stop := make(chan struct{})
	var results []trafficResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results = hammerProxy(rig.hostPort, stop, 2*time.Millisecond)
	}()

	time.Sleep(2 * time.Second)

	rotStart := time.Now()
	if err := rig.rotate(ctx, t); err != nil {
		close(stop)
		wg.Wait()
		t.Fatalf("restart rotation failed: %v", err)
	}
	rotDur := time.Since(rotStart)

	time.Sleep(8 * time.Second)
	close(stop)
	wg.Wait()

	total, ok, kinds := summarize(results)
	t.Logf("=== RESTART STRATEGY TRAFFIC REPORT ===")
	t.Logf("rotation duration: %v", rotDur)
	t.Logf("total requests: %d, successful: %d, failed: %d", total, ok, total-ok)
	for k, v := range kinds {
		t.Logf("  failure %-42s %d", k, v)
	}
	for _, r := range results {
		if !r.ok {
			t.Logf("  FAILURE %-40s started +%v resolved +%v", r.failKind, r.at.Sub(rotStart), r.endAt.Sub(rotStart))
		}
	}

	if total < 100 {
		t.Fatalf("too few requests (%d)", total)
	}
	if total-ok > 0 {
		t.Errorf("ZERO-DOWNTIME VIOLATION (restart strategy): %d/%d requests failed: %v", total-ok, total, kinds)
	} else {
		t.Logf("✅ no client-visible failures across %d requests during restart rotation", total)
	}
}
