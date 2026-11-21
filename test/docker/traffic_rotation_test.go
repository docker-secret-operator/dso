//go:build dockerint

package docker

// Traffic-across-rotation scenarios (M0-11 migration of
// test/integration/proxy_rotation_downtime_test.go).
//
// What these tests can and cannot show:
//   - Each test generates TCP/HTTP traffic BEFORE, DURING and AFTER the
//     transition through the in-process DSO proxy, and records request count,
//     failure kinds, HTTP status distribution, latency percentiles, the backend
//     identity every response came from, and time spent in each phase.
//   - Clients here open a NEW connection per request. They measure availability
//     of new connections. They do NOT measure preservation of existing
//     long-lived connections; nothing in this file claims that they survive.
//   - A passing run is evidence about this image, this daemon and this load. It
//     is not a general zero-downtime guarantee.
//
// The workload is busybox httpd serving a random per-start identity string, so a
// response proves WHICH container instance answered. The synthetic rotated value
// is only ever the literal "rotated-value".
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap/zaptest"

	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/watcher"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

const (
	trafficSecret   = "traffic_secret"
	trafficProvider = "trafficprovider"
	// rotatedValue is what the shadow's environment will hold after rotation.
	rotatedValue = "rotated-value"

	// The identity file is regenerated on every container start. When the
	// shadow receives APP_PASSWORD=rotated-value AND FAIL_READINESS is set, it
	// marks itself unhealthy so the readiness gate must refuse it.
	trafficScript = `mkdir -p /www; head -c8 /dev/urandom | od -An -tx1 | tr -d ' \n' > /www/index.html; ` +
		`if [ -n "${FAIL_READINESS:-}" ] && [ "${APP_PASSWORD:-}" = rotated-value ]; then touch /tmp/bad; fi; ` +
		`exec httpd -f -p 80 -h /www`
)

type phase string

const (
	phasePre    phase = "pre"
	phaseDuring phase = "during"
	phasePost   phase = "post"
)

type sample struct {
	start, end time.Time
	ok         bool
	failKind   string // connection_refused, dial_timeout, dial_error, write_error, read_timeout, closed_no_response, http_5xx, http_other
	status     int
	backend    string
	latency    time.Duration
}

// generator runs concurrent short-connection HTTP/1.0 clients until stopped.
type generator struct {
	addr    string
	workers int
	mu      sync.Mutex
	samples []sample
	stop    chan struct{}
	wg      sync.WaitGroup
}

func startTraffic(addr string, workers int) *generator {
	g := &generator{addr: addr, workers: workers, stop: make(chan struct{})}
	for i := 0; i < workers; i++ {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			for {
				select {
				case <-g.stop:
					return
				default:
				}
				s := oneRequest(g.addr)
				g.mu.Lock()
				g.samples = append(g.samples, s)
				g.mu.Unlock()
				time.Sleep(3 * time.Millisecond)
			}
		}()
	}
	return g
}

func (g *generator) finish() []sample {
	close(g.stop)
	g.wg.Wait()
	return g.samples
}

func oneRequest(addr string) sample {
	s := sample{start: time.Now()}
	done := func() sample { s.end = time.Now(); s.latency = s.end.Sub(s.start); return s }

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		switch ne, isNet := err.(net.Error); {
		case strings.Contains(err.Error(), "refused"):
			s.failKind = "connection_refused"
		case isNet && ne.Timeout():
			s.failKind = "dial_timeout"
		default:
			s.failKind = "dial_error"
		}
		return done()
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("GET / HTTP/1.0\r\nHost: dso-it\r\n\r\n")); err != nil {
		s.failKind = "write_error"
		return done()
	}
	raw, err := io.ReadAll(conn)
	if len(raw) == 0 {
		if ne, isNet := err.(net.Error); isNet && ne.Timeout() {
			s.failKind = "read_timeout"
		} else {
			s.failKind = "closed_no_response"
		}
		return done()
	}
	text := string(raw)
	head, body, _ := strings.Cut(text, "\r\n\r\n")
	if fields := strings.SplitN(head, " ", 3); len(fields) > 1 {
		_, _ = fmt.Sscanf(fields[1], "%d", &s.status)
	}
	s.backend = strings.TrimSpace(body)
	switch {
	case s.status >= 500:
		s.failKind = "http_5xx"
	case s.status != http.StatusOK:
		s.failKind = "http_other"
	default:
		s.ok = true
	}
	return done()
}

// window classifies a sample by when it STARTED relative to the transition.
func window(s sample, rotStart, rotEnd time.Time) phase {
	switch {
	case s.start.Before(rotStart):
		return phasePre
	case !s.start.After(rotEnd):
		return phaseDuring
	default:
		return phasePost
	}
}

type phaseStats struct {
	total, ok    int
	failKinds    map[string]int
	statuses     map[int]int
	backends     map[string]int
	lat          []time.Duration
	connFailures int
	timeouts     int
}

func collect(samples []sample, rotStart, rotEnd time.Time) map[phase]*phaseStats {
	out := map[phase]*phaseStats{}
	for _, p := range []phase{phasePre, phaseDuring, phasePost} {
		out[p] = &phaseStats{failKinds: map[string]int{}, statuses: map[int]int{}, backends: map[string]int{}}
	}
	for _, s := range samples {
		ps := out[window(s, rotStart, rotEnd)]
		ps.total++
		ps.lat = append(ps.lat, s.latency)
		if s.status != 0 {
			ps.statuses[s.status]++
		}
		if s.backend != "" {
			ps.backends[s.backend]++
		}
		if s.ok {
			ps.ok++
			continue
		}
		ps.failKinds[s.failKind]++
		switch s.failKind {
		case "connection_refused", "dial_error", "closed_no_response":
			ps.connFailures++
		case "dial_timeout", "read_timeout":
			ps.timeouts++
		}
	}
	return out
}

func pct(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	c := append([]time.Duration(nil), d...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	return c[int(float64(len(c)-1)*p)]
}

func report(t *testing.T, name string, stats map[phase]*phaseStats, rotStart, rotEnd time.Time, extra string) {
	t.Helper()
	t.Logf("=== TRAFFIC REPORT: %s ===", name)
	t.Logf("rotation phase duration: %v (%s)", rotEnd.Sub(rotStart), extra)
	for _, p := range []phase{phasePre, phaseDuring, phasePost} {
		s := stats[p]
		t.Logf("[%s] requests=%d ok=%d failed=%d connection_failures=%d timeouts=%d", p, s.total, s.ok, s.total-s.ok, s.connFailures, s.timeouts)
		t.Logf("[%s] failure kinds=%v http_status=%v", p, s.failKinds, s.statuses)
		t.Logf("[%s] backends observed=%d %v", p, len(s.backends), s.backends)
		t.Logf("[%s] latency p50=%v p95=%v p99=%v max=%v", p, pct(s.lat, .50), pct(s.lat, .95), pct(s.lat, .99), pct(s.lat, 1))
	}
}

// trafficRig is a real workload fronted by a real proxy.Manager and driven by
// the real ReloaderController. Everything it creates carries the guard labels.
type trafficRig struct {
	cli      *client.Client
	rc       *watcher.ReloaderController
	pm       *dsoProxy.Manager
	appID    string
	appName  string
	hostPort int
}

type rigOpts struct {
	strategy      string
	failReadiness bool // shadow becomes unhealthy after rotation
	directPublish bool // publish the port with Docker instead of the DSO proxy
	healthTimeout string
}

func newTrafficRig(t *testing.T, o rigOpts) *trafficRig {
	t.Helper()
	cli := dockerClient(t)
	if o.strategy == "" {
		o.strategy = "rolling"
	}
	if o.healthTimeout == "" {
		o.healthTimeout = "60s"
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	hostPort := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	name := uniqueName(t, "dso-it-traffic-"+o.strategy)
	args := []string{"run", "-d", "--name", name}
	args = append(args, dockerguard.Current(t).LabelArgs()...)
	args = append(args,
		"--label", "dso.reloader=true",
		"--label", "dso.owner="+ownerID,
		"--label", "dso.secrets="+trafficSecret,
		"--label", "dso.update.strategy="+o.strategy,
		"--health-cmd", "test ! -f /tmp/bad",
		"--health-interval", "1s", "--health-retries", "1",
	)
	if o.failReadiness {
		args = append(args, "-e", "FAIL_READINESS=1")
	}
	if o.directPublish {
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:80", hostPort))
	} else {
		// Exactly what the compose transformation does in production: the host
		// binding is stripped into a label so the DSO proxy owns it.
		args = append(args, "--label", fmt.Sprintf("dso.host_ports=%d:80", hostPort))
	}
	args = append(args, testImage, "sh", "-c", trafficScript)
	id := dockerCmd(t, args...)
	cleanupByLabel(t, cli, dockerguard.LabelRun, dockerguard.Current(t).Config.RunID)

	in, err := cli.ContainerInspect(context.Background(), id)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	ip := ""
	for _, ep := range in.NetworkSettings.Networks {
		if ep.IPAddress != "" {
			ip = ep.IPAddress
			break
		}
	}
	if ip == "" {
		t.Fatal("workload has no IP")
	}

	logger := zaptest.NewLogger(t)
	pm := dsoProxy.NewManager(logger)
	if !o.directPublish {
		if err := pm.EnsurePort(hostPort, 80); err != nil {
			t.Fatalf("bind proxy port: %v", err)
		}
		if err := pm.RegisterContainer(id, ip, hostPort, 80); err != nil {
			t.Fatalf("register original backend: %v", err)
		}
		t.Cleanup(func() { _ = pm.Stop(2 * time.Second) })
	}

	rc, err := watcher.NewReloaderController(logger)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	rc.ProxyManager = pm
	cfg := &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{trafficProvider: {Type: "vault"}},
		Secrets: []dsoConfig.SecretMapping{{
			Name:     trafficSecret,
			Provider: trafficProvider,
			Inject:   dsoConfig.InjectionConfig{Type: "env"},
			Mappings: map[string]string{"password": "APP_PASSWORD"},
		}},
	}
	cfg.Agent.OwnerID = ownerID
	cfg.Agent.Rotation.HealthCheckTimeout = o.healthTimeout
	rc.Config = cfg
	rc.Cache = &fakeCache{data: map[string]map[string]string{
		trafficProvider + ":" + trafficSecret: {"password": rotatedValue},
	}}
	rc.Targets.Store(id, &watcher.TargetContainer{ID: id, Strategy: o.strategy, Secrets: []string{trafficSecret}})

	evCtx, evCancel := context.WithCancel(context.Background())
	t.Cleanup(evCancel)
	go rc.StartEventLoop(evCtx)
	time.Sleep(750 * time.Millisecond) // attach to the event stream

	return &trafficRig{cli: cli, rc: rc, pm: pm, appID: id, appName: name, hostPort: hostPort}
}

func (r *trafficRig) addr() string { return fmt.Sprintf("127.0.0.1:%d", r.hostPort) }

// rotate runs a real TriggerReload; the returned times bound the transition.
func (r *trafficRig) rotate() (start, end time.Time, err error) {
	done := make(chan error, 1)
	start = time.Now()
	if err = r.rc.TriggerReload(context.Background(), trafficSecret, func(e error) { done <- e }); err != nil {
		return start, time.Now(), err
	}
	select {
	case err = <-done:
	case <-time.After(90 * time.Second):
		err = fmt.Errorf("rotation did not complete within 90s")
	}
	return start, time.Now(), err
}

// settle is how long traffic continues before and after the transition.
const (
	settlePre  = 3 * time.Second
	settlePost = 8 * time.Second
)

// runWithTraffic wraps one rotation in continuous traffic and returns the
// per-phase statistics plus the rotation outcome.
func (r *trafficRig) runWithTraffic(t *testing.T, workers int) (map[phase]*phaseStats, []sample, time.Time, time.Time, error) {
	t.Helper()
	g := startTraffic(r.addr(), workers)
	time.Sleep(settlePre)
	rs, re, err := r.rotate()
	time.Sleep(settlePost)
	samples := g.finish()
	return collect(samples, rs, re), samples, rs, re, err
}

func requireBaselineClean(t *testing.T, st map[phase]*phaseStats) {
	t.Helper()
	if st[phasePre].total < 50 {
		t.Fatalf("pre-rotation traffic too small to be meaningful (%d requests)", st[phasePre].total)
	}
	if f := st[phasePre].total - st[phasePre].ok; f > 0 {
		t.Fatalf("baseline traffic was already failing (%d): environment problem, results not attributable: %v", f, st[phasePre].failKinds)
	}
}

// TestTraffic_SteadyStateControl runs the same traffic with NO rotation, so
// failures in the rotation tests are attributable to the rotation.
func TestTraffic_SteadyStateControl(t *testing.T) {
	rig := newTrafficRig(t, rigOpts{})
	g := startTraffic(rig.addr(), 8)
	time.Sleep(12 * time.Second)
	samples := g.finish()
	now := time.Now()
	st := collect(samples, now, now) // everything is "pre"
	report(t, "control (no rotation)", st, now, now, "no rotation performed")
	if st[phasePre].total < 100 {
		t.Fatalf("too few requests (%d)", st[phasePre].total)
	}
	if st[phasePre].ok != st[phasePre].total {
		t.Errorf("control saw failures with no rotation: %v", st[phasePre].failKinds)
	}
	if n := len(st[phasePre].backends); n != 1 {
		t.Errorf("control observed %d backend identities, want exactly 1", n)
	}
}

func assertRotatedAndServed(t *testing.T, st map[phase]*phaseStats) {
	t.Helper()
	pre, post := st[phasePre].backends, st[phasePost].backends
	if len(pre) != 1 {
		t.Errorf("expected exactly one backend identity before rotation, saw %v", pre)
	}
	if st[phasePost].total == 0 {
		t.Fatal("no post-rotation traffic")
	}
	for id := range post {
		if _, was := pre[id]; was {
			t.Errorf("post-rotation traffic is still served by the pre-rotation instance %s", id)
		}
	}
	if len(post) == 0 {
		t.Error("post-rotation traffic reached no identifiable backend")
	}
}

func assertNoFailures(t *testing.T, label string, st map[phase]*phaseStats) {
	t.Helper()
	for _, p := range []phase{phasePre, phaseDuring, phasePost} {
		if f := st[p].total - st[p].ok; f > 0 {
			t.Errorf("%s: %d/%d new-connection requests failed in %s phase: %v", label, f, st[p].total, p, st[p].failKinds)
		}
	}
}

// TestTraffic_RollingRotation: normal rolling rotation under concurrent load.
func TestTraffic_RollingRotation(t *testing.T) {
	rig := newTrafficRig(t, rigOpts{strategy: "rolling"})
	st, _, rs, re, err := rig.runWithTraffic(t, 20)
	report(t, "rolling rotation (20 concurrent new-connection clients)", st, rs, re, "rolling")
	if err != nil {
		t.Fatalf("rolling rotation failed: %v", err)
	}
	requireBaselineClean(t, st)
	assertRotatedAndServed(t, st)
	assertNoFailures(t, "rolling", st)
}

// TestTraffic_RestartRotation: the restart strategy. It is NOT presented as
// zero-downtime by design; the assertion is the observed result, and a failure
// here documents the actual downtime rather than contradicting a promise.
func TestTraffic_RestartRotation(t *testing.T) {
	rig := newTrafficRig(t, rigOpts{strategy: "restart"})
	st, _, rs, re, err := rig.runWithTraffic(t, 8)
	report(t, "restart rotation (8 concurrent new-connection clients)", st, rs, re, "restart")
	if err != nil {
		t.Fatalf("restart rotation failed: %v", err)
	}
	requireBaselineClean(t, st)
	assertRotatedAndServed(t, st)
	assertNoFailures(t, "restart", st)
}

// TestTraffic_CandidateReadinessFailure: the shadow never becomes healthy. It
// must never receive traffic and the original must keep serving.
func TestTraffic_CandidateReadinessFailure(t *testing.T) {
	rig := newTrafficRig(t, rigOpts{strategy: "rolling", failReadiness: true, healthTimeout: "20s"})
	st, _, rs, re, err := rig.runWithTraffic(t, 8)
	report(t, "candidate readiness failure", st, rs, re, fmt.Sprintf("rotation error=%v", err))
	if err == nil {
		t.Error("rotation succeeded although the candidate marks itself unhealthy")
	}
	requireBaselineClean(t, st)
	assertNoFailures(t, "readiness-failure", st)
	// Only the original instance may ever have answered.
	all := map[string]int{}
	for _, p := range []phase{phasePre, phaseDuring, phasePost} {
		for b, n := range st[p].backends {
			all[b] += n
		}
	}
	if len(all) != 1 {
		t.Errorf("traffic reached %d backend identities during a failed rotation, want exactly the original: %v", len(all), all)
	}
	in, ierr := rig.cli.ContainerInspect(context.Background(), rig.appName)
	if ierr != nil || in.ID != rig.appID || !in.State.Running {
		t.Errorf("original is not the running holder of %s after a failed rotation (err=%v)", rig.appName, ierr)
	}
	assertOnlyOwnedByRun(t, rig)
}

// TestTraffic_UnsupportedDirectPublishing: a rolling strategy on a service whose
// ports Docker publishes directly must be rejected BEFORE any mutation: the
// original keeps its ID, keeps running and keeps answering. No candidate may be
// created. The strategy is not silently switched to restart.
func TestTraffic_UnsupportedDirectPublishing(t *testing.T) {
	rig := newTrafficRig(t, rigOpts{strategy: "rolling", directPublish: true})
	before := containersWithLabel(t, rig, "dso.owner", ownerID)
	st, _, rs, re, err := rig.runWithTraffic(t, 4)
	report(t, "rolling rejected for directly published port", st, rs, re, fmt.Sprintf("rotation error=%v", err))
	if err == nil {
		t.Fatal("rolling rotation of a directly published service must be rejected before mutation")
	}
	requireBaselineClean(t, st)
	assertNoFailures(t, "rejected-rolling", st)
	if after := containersWithLabel(t, rig, "dso.owner", ownerID); len(after) != len(before) {
		t.Errorf("container set changed on a rejected rotation: before=%d after=%d", len(before), len(after))
	}
	in, ierr := rig.cli.ContainerInspect(context.Background(), rig.appName)
	if ierr != nil || in.ID != rig.appID || !in.State.Running {
		t.Errorf("original was disturbed by a rejected rotation (err=%v)", ierr)
	}
	if len(st[phasePost].backends) != 1 {
		t.Errorf("a rejected rotation changed the serving instance: %v", st[phasePost].backends)
	}
}

func containersWithLabel(t *testing.T, rig *trafficRig, k, v string) []string {
	t.Helper()
	out := dockerCmd(t, "ps", "-a", "-q", "--filter", "label="+k+"="+v,
		"--filter", "label="+dockerguard.LabelRun+"="+dockerguard.Current(t).Config.RunID)
	return strings.Fields(out)
}

// assertOnlyOwnedByRun checks that nothing carrying this run's labels besides
// the original remains running as a leftover candidate after a failed rotation.
func assertOnlyOwnedByRun(t *testing.T, rig *trafficRig) {
	t.Helper()
	list, err := rig.cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	run := dockerguard.Current(t).Config.RunID
	for _, c := range list {
		if c.Labels[dockerguard.LabelRun] != run || c.ID == rig.appID {
			continue
		}
		if c.State == "running" {
			t.Errorf("leftover running candidate %s after a failed rotation", c.ID[:12])
		}
	}
}
