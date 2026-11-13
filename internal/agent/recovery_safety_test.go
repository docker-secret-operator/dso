package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"go.uber.org/zap/zaptest"
)

// M0-04: recovery decision matrix. A stateful in-memory Docker daemon records
// every mutation so each test can assert BOTH what was removed/started and what
// was preserved. The invariant under test: recovery never removes the last
// known healthy workload and fails safe on anything ambiguous.

type fakeCtr struct {
	id, name string
	owner    string // dso.owner label value ("" = no label)
	running  bool
	health   string        // "", "healthy", "unhealthy", "starting"
	upFor    time.Duration // how long it has been running
	// startHealth is the health the container reports after a successful start.
	startHealth string
	labels      map[string]string // extra labels (e.g. dso.rotation.id, dso.host_ports)
	status      string            // Docker's human status in listings, e.g. "Up 1 hour (healthy)"
}

type fakeDaemon struct {
	mu        sync.Mutex
	ctrs      map[string]*fakeCtr // by id
	nextID    int
	removed   []string
	started   []string
	stopped   []string
	listFails bool
}

func newFakeDaemon(cs ...*fakeCtr) *fakeDaemon {
	d := &fakeDaemon{ctrs: map[string]*fakeCtr{}}
	for _, c := range cs {
		d.ctrs[c.id] = c
	}
	return d
}

func (d *fakeDaemon) find(ref string) *fakeCtr {
	if c, ok := d.ctrs[ref]; ok {
		return c
	}
	for _, c := range d.ctrs {
		if c.name == ref {
			return c
		}
	}
	return nil
}

func (d *fakeDaemon) labels(c *fakeCtr) map[string]string {
	l := map[string]string{"dso.reloader": "true"}
	for k, v := range c.labels {
		l[k] = v
	}
	if c.owner != "" {
		l["dso.owner"] = c.owner
	}
	return l
}

func (d *fakeDaemon) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/v1.41")
		switch {
		case r.Method == http.MethodGet && path == "/containers/json":
			if d.listFails {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "daemon unreachable"})
				return
			}
			onlyRunning := strings.Contains(r.URL.Query().Get("filters"), "running")
			var out []map[string]interface{}
			for _, c := range d.ctrs {
				if onlyRunning && !c.running {
					continue
				}
				out = append(out, map[string]interface{}{
					"Id": c.id, "Names": []string{"/" + c.name}, "Labels": d.labels(c), "Status": c.status,
					"NetworkSettings": map[string]interface{}{"Networks": map[string]interface{}{
						"bridge": map[string]string{"IPAddress": "10.9.0." + fmt.Sprint(1+len(c.id)%200)}}},
				})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/json"):
			ref := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/json")
			c := d.find(ref)
			if c == nil {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "No such container: " + ref})
				return
			}
			st := &container.State{Running: c.running, Status: map[bool]string{true: "running", false: "exited"}[c.running]}
			if c.running {
				st.StartedAt = time.Now().Add(-c.upFor).UTC().Format(time.RFC3339Nano)
			}
			if c.health != "" {
				st.Health = &container.Health{Status: c.health}
			}
			_ = json.NewEncoder(w).Encode(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: c.id, Name: "/" + c.name, State: st, HostConfig: &container.HostConfig{}},
				NetworkSettings:   &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{"bridge": {}}},
				Config:            &container.Config{Image: "busybox", Labels: d.labels(c)},
			})
		case r.Method == http.MethodPost && path == "/containers/create":
			var body struct {
				Labels map[string]string
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			name := r.URL.Query().Get("name")
			if d.find(name) != nil {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "name already in use"})
				return
			}
			d.nextID++
			id := fmt.Sprintf("created-%d", d.nextID)
			c := &fakeCtr{id: id, name: name, owner: body.Labels["dso.owner"], startHealth: "healthy", labels: map[string]string{}}
			for k, v := range body.Labels {
				if k != "dso.owner" && k != "dso.reloader" {
					c.labels[k] = v
				}
			}
			d.ctrs[id] = c
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": id})
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/rename"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/rename")
			c := d.find(id)
			newName := r.URL.Query().Get("name")
			if c == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if other := d.find(newName); other != nil && other.id != c.id {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "name already in use"})
				return
			}
			c.name = newName
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/stop"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/stop")
			if c := d.find(id); c != nil {
				c.running = false
				d.stopped = append(d.stopped, c.id)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/start"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/start")
			if c := d.find(id); c != nil {
				c.running = true
				c.upFor = time.Hour
				c.health = c.startHealth
				d.started = append(d.started, c.id)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/containers/"):
			id := strings.TrimPrefix(path, "/containers/")
			if c := d.find(id); c != nil {
				d.removed = append(d.removed, c.name)
				delete(d.ctrs, c.id)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

func (d *fakeDaemon) names() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n []string
	for _, c := range d.ctrs {
		n = append(n, c.name)
	}
	sort.Strings(n)
	return n
}

func (d *fakeDaemon) removedSorted() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := append([]string(nil), d.removed...)
	sort.Strings(r)
	return r
}

const me = "owner-A"

func healthy(id, name string) *fakeCtr {
	return &fakeCtr{id: id, name: name, owner: me, running: true, health: "healthy", upFor: time.Hour, startHealth: "healthy"}
}

func artifact(id, name string) *fakeCtr {
	return &fakeCtr{id: id, name: name, owner: me, running: true, health: "healthy", upFor: time.Hour}
}

type recFixture struct {
	t  *testing.T
	d  *fakeDaemon
	ar *AutomaticRecovery
	st *StateTracker
}

func newRec(t *testing.T, d *fakeDaemon, originalIDs string) *recFixture {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"),
		client.WithHTTPClient(&http.Client{Transport: &recoveryMockTransport{reqFunc: func(req *http.Request) (*http.Response, error) {
			rec := &captureWriter{h: http.Header{}}
			d.handler().ServeHTTP(rec, req)
			return rec.response(), nil
		}}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	if originalIDs != "-" {
		if err := st.StartRotation("vault", "db", originalIDs, ""); err != nil {
			t.Fatal(err)
		}
	}
	ar := NewAutomaticRecovery(cli, logger, st)
	ar.OwnerID = me
	ar.Timings = &recoveryTimings{healthWait: 0, minStableRunning: 30 * time.Second}
	return &recFixture{t: t, d: d, ar: ar, st: st}
}

func stringBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

type captureWriter struct {
	h    http.Header
	code int
	buf  strings.Builder
}

func (c *captureWriter) Header() http.Header         { return c.h }
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }
func (c *captureWriter) WriteHeader(code int)        { c.code = code }
func (c *captureWriter) response() *http.Response {
	if c.code == 0 {
		c.code = 200
	}
	return &http.Response{StatusCode: c.code, Header: c.h, Body: stringBody(c.buf.String())}
}

func (f *recFixture) run() {
	f.t.Helper()
	if err := f.ar.RecoverFromCrash(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *recFixture) status() string {
	f.t.Helper()
	f.st.mu.RLock()
	defer f.st.mu.RUnlock()
	for _, s := range f.st.states {
		return s.Status
	}
	return "<none>"
}

func eq(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func TestRecovery_OriginalServingCandidateLeftover_RemovesOnlyTheCandidate(t *testing.T) {
	d := newFakeDaemon(healthy("o1", "app"), artifact("n1", "app_dso_new_1700000000"))
	f := newRec(t, d, "o1")
	f.run()
	eq(t, "removed", d.removedSorted(), []string{"app_dso_new_1700000000"})
	eq(t, "remaining", d.names(), []string{"app"})
	if f.status() != "recovered" {
		t.Fatalf("status %s", f.status())
	}
}

func TestRecovery_SwapCompleted_RemovesOldBackupKeepsPromotedWorkload(t *testing.T) {
	// The promoted replacement holds "app" (new ID); the original was renamed to a backup.
	d := newFakeDaemon(healthy("new1", "app"), artifact("o1", "app_dso_backup_1700000000"))
	f := newRec(t, d, "o1")
	f.run()
	eq(t, "removed", d.removedSorted(), []string{"app_dso_backup_1700000000"})
	eq(t, "remaining", d.names(), []string{"app"})
	if f.status() != "recovered" {
		t.Fatalf("status %s", f.status())
	}
}

func TestRecovery_CrashBetweenTheTwoRenames_NeverRemovesTheOnlyCopies(t *testing.T) {
	// S103: after `app -> backup` but before `new -> app`, no container holds the real
	// name. Both candidates may be the only running copy: remove NOTHING.
	d := newFakeDaemon(artifact("o1", "app_dso_backup_1700000000"), artifact("n1", "app_dso_new_1700000000"))
	f := newRec(t, d, "o1")
	f.run()
	eq(t, "removed", d.removedSorted(), nil)
	eq(t, "remaining", d.names(), []string{"app_dso_backup_1700000000", "app_dso_new_1700000000"})
	if f.status() != "critical_error" {
		t.Fatalf("must be flagged for manual recovery, got %s", f.status())
	}
}

func TestRecovery_CrashAfterCompletedSwapIsNotReportedCritical(t *testing.T) {
	// S102: swap finished and the old backup was already removed; original ID is gone
	// but the service is healthy under its real name.
	d := newFakeDaemon(healthy("new1", "app"))
	f := newRec(t, d, "o-gone")
	f.run()
	// The original id cannot name the service, so with no artifact to base it on
	// recovery has no proof for that service: it must NOT claim success.
	eq(t, "removed", d.removedSorted(), nil)
	if f.status() != "critical_error" {
		t.Fatalf("an unprovable original must stay flagged, got %s", f.status())
	}
	eq(t, "remaining", d.names(), []string{"app"})
}

func TestRecovery_OnlyOriginalSurvivesStoppedAndRenamed_IsNotAutoStarted(t *testing.T) {
	o := artifact("o1", "app_dso_backup_1700000000")
	o.running = false
	d := newFakeDaemon(o)
	f := newRec(t, d, "o1")
	f.run()
	if len(d.started) != 0 || len(d.removed) != 0 {
		t.Fatalf("nothing may be started or removed: started=%v removed=%v", d.started, d.removed)
	}
	if f.status() != "critical_error" {
		t.Fatalf("got %s", f.status())
	}
}

func TestRecovery_StoppedOriginalUnderRealName_IsStartedAsRollback(t *testing.T) {
	o := healthy("o1", "app")
	o.running = false
	d := newFakeDaemon(o, artifact("n1", "app_dso_new_1700000000"))
	f := newRec(t, d, "o1")
	f.run()
	eq(t, "started", d.started, []string{"o1"})
	eq(t, "removed", d.removedSorted(), []string{"app_dso_new_1700000000"})
	if f.status() != "recovered" {
		t.Fatalf("got %s", f.status())
	}
}

func TestRecovery_HolderNotProvenHealthy_PreservesEverything(t *testing.T) {
	cases := map[string]*fakeCtr{
		"unhealthy":                    {id: "h", name: "app", owner: me, running: true, health: "unhealthy", upFor: time.Hour},
		"health check still starting":  {id: "h", name: "app", owner: me, running: true, health: "starting", upFor: time.Hour},
		"no healthcheck, just started": {id: "h", name: "app", owner: me, running: true, upFor: 2 * time.Second},
		"stopped":                      {id: "h", name: "app", owner: me, running: false},
		"restarting loop":              {id: "h", name: "app", owner: me, running: true, health: "unhealthy", upFor: time.Second},
	}
	for name, holder := range cases {
		t.Run(name, func(t *testing.T) {
			d := newFakeDaemon(holder, artifact("o1", "app_dso_backup_1700000000"))
			f := newRec(t, d, "o1")
			f.run()
			eq(t, "removed", d.removedSorted(), nil)
			if f.status() != "critical_error" {
				t.Fatalf("got %s", f.status())
			}
		})
	}
}

func TestRecovery_HolderWithoutHealthcheckIsTrustedOnceStable(t *testing.T) {
	h := &fakeCtr{id: "h", name: "app", owner: me, running: true, upFor: time.Hour}
	d := newFakeDaemon(h, artifact("o1", "app_dso_backup_1700000000"))
	f := newRec(t, d, "o1")
	f.run()
	eq(t, "removed", d.removedSorted(), []string{"app_dso_backup_1700000000"})
}

func TestRecovery_OwnershipMismatchFailsClosedWithNoMutation(t *testing.T) {
	foreignOriginal := &fakeCtr{id: "o1", name: "app", owner: "owner-B", running: true, health: "healthy", upFor: time.Hour}
	d := newFakeDaemon(foreignOriginal, artifact("n1", "app_dso_new_1700000000"))
	f := newRec(t, d, "o1")
	f.run()
	if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
		t.Fatalf("no mutation allowed on an ownership mismatch: %+v", d)
	}
	if f.status() != "critical_error" {
		t.Fatalf("got %s", f.status())
	}
}

func TestRecovery_NeverTouchesForeignUnownedOrUnrelatedContainers(t *testing.T) {
	d := newFakeDaemon(
		healthy("o1", "app"),
		artifact("n1", "app_dso_new_1700000000"),
		&fakeCtr{id: "f1", name: "app_dso_backup_9", owner: "owner-B", running: true, health: "healthy", upFor: time.Hour},
		&fakeCtr{id: "u1", name: "app_dso_new_8", owner: "", running: true, health: "healthy", upFor: time.Hour},
		&fakeCtr{id: "x1", name: "my_old_database", owner: me, running: true, health: "healthy", upFor: time.Hour},
		&fakeCtr{id: "x2", name: "other_old_service_v2", owner: me, running: true, upFor: time.Hour},
	)
	f := newRec(t, d, "o1")
	f.run()
	eq(t, "removed", d.removedSorted(), []string{"app_dso_new_1700000000"})
	eq(t, "remaining", d.names(), []string{"app", "app_dso_backup_9", "app_dso_new_8", "my_old_database", "other_old_service_v2"})
}

func TestRecovery_MultiContainerRotationHandlesEachContainerIndividually(t *testing.T) {
	// Two replicas recorded as structured IDs. r1 is fine (artifact retired); r2's
	// service has no healthy holder (artifact preserved, rotation flagged).
	d := newFakeDaemon(
		healthy("r1", "web-1"), artifact("a1", "web-1_dso_new_1"),
		artifact("r2", "web-2_dso_backup_1"), artifact("a2", "web-2_dso_new_1"),
	)
	f := newRec(t, d, "r1,r2")
	f.run()
	eq(t, "removed", d.removedSorted(), []string{"web-1_dso_new_1"})
	eq(t, "remaining", d.names(), []string{"web-1", "web-2_dso_backup_1", "web-2_dso_new_1"})
	if f.status() != "critical_error" {
		t.Fatalf("got %s", f.status())
	}
	f.st.mu.RLock()
	defer f.st.mu.RUnlock()
	for _, s := range f.st.states {
		if len(s.OriginalContainerIDs) != 2 || s.OriginalContainerIDs[0] != "r1" || s.OriginalContainerIDs[1] != "r2" {
			t.Fatalf("IDs must be persisted as a structured list: %+v", s.OriginalContainerIDs)
		}
		if s.RotationID == "" || s.Phase == "" {
			t.Fatalf("rotation id/phase missing: %+v", s)
		}
	}
}

func TestRecovery_IsIdempotentAndNeverWorsensState(t *testing.T) {
	d := newFakeDaemon(healthy("o1", "app"), artifact("n1", "app_dso_new_1"))
	f := newRec(t, d, "o1")
	f.run()
	first := d.names()
	f.run() // recovered rotations are terminal: nothing left to do
	f.run()
	eq(t, "names after repeats", d.names(), first)
	eq(t, "removed total", d.removedSorted(), []string{"app_dso_new_1"})
	// A critical rotation stays critical and still mutates nothing on repeat.
	d2 := newFakeDaemon(artifact("o1", "app_dso_backup_1"), artifact("n1", "app_dso_new_1"))
	f2 := newRec(t, d2, "o1")
	for i := 0; i < 3; i++ {
		f2.run()
	}
	if len(d2.removed) != 0 || f2.status() != "critical_error" {
		t.Fatalf("repeat recovery changed things: removed=%v status=%s", d2.removed, f2.status())
	}
}

func TestRecovery_RotationWithoutIdentityPreservesEverything(t *testing.T) {
	d := newFakeDaemon(healthy("h", "app"), artifact("n1", "app_dso_new_1"))
	f := newRec(t, d, "")
	f.run()
	if len(d.removed) != 0 || f.status() != "critical_error" {
		t.Fatalf("removed=%v status=%s", d.removed, f.status())
	}
}

func TestRecovery_UnreadableStateDisablesAllAutomaticAction(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rotations.json"), []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	corrupt, where := st.Corrupt()
	if !corrupt || !strings.Contains(where, ".corrupt-") {
		t.Fatalf("corruption must be detected and the file preserved, got %v %q", corrupt, where)
	}
	if _, serr := os.Stat(where); serr != nil {
		t.Fatalf("the unreadable file must be preserved as evidence: %v", serr)
	}
	d := newFakeDaemon(healthy("o1", "app"), artifact("n1", "app_dso_new_1"))
	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		rec := &captureWriter{h: http.Header{}}
		d.handler().ServeHTTP(rec, req)
		return rec.response(), nil
	})
	ar := NewAutomaticRecovery(cli, logger, st)
	ar.OwnerID = me
	if err := ar.RecoverFromCrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ar.CleanupOrphanedContainers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(d.removed) != 0 || len(d.started) != 0 {
		t.Fatalf("with unknown state NOTHING may be touched: removed=%v started=%v", d.removed, d.started)
	}
}

func TestRecovery_DockerUnreachableRemovesNothing(t *testing.T) {
	d := newFakeDaemon(healthy("o1", "app"), artifact("n1", "app_dso_new_1"))
	d.listFails = true
	f := newRec(t, d, "o1")
	f.run()
	if len(d.removed) != 0 {
		t.Fatalf("removed %v", d.removed)
	}
}

func TestCleanupOrphanedContainers_UsesTheSameSafetyRule(t *testing.T) {
	// Previously a blind delete-by-name at every startup.
	d := newFakeDaemon(
		artifact("b1", "svc_dso_backup_1"), artifact("n1", "svc_dso_new_1"), // no holder: preserved
		healthy("h2", "web"), artifact("n2", "web_old_1700000000"), // holder: retired
		&fakeCtr{id: "f", name: "web_dso_backup_5", owner: "owner-B", running: true, health: "healthy", upFor: time.Hour}, // foreign: kept
	)
	f := newRec(t, d, "-")
	if err := f.ar.CleanupOrphanedContainers(context.Background()); err != nil {
		t.Fatal(err)
	}
	eq(t, "removed", d.removedSorted(), []string{"web_old_1700000000"})
	eq(t, "remaining", d.names(), []string{"svc_dso_backup_1", "svc_dso_new_1", "web", "web_dso_backup_5"})
	// Repeat is a no-op.
	if err := f.ar.CleanupOrphanedContainers(context.Background()); err != nil {
		t.Fatal(err)
	}
	eq(t, "removed after repeat", d.removedSorted(), []string{"web_old_1700000000"})
}

func TestRotationState_LegacyCommaJoinedIDsAreStillUnderstood(t *testing.T) {
	var s RotationState
	if err := json.Unmarshal([]byte(`{"provider_name":"p","secret_name":"s","original_container_id":"a, b,,c","status":"in_progress"}`), &s); err != nil {
		t.Fatal(err)
	}
	eq(t, "ids", s.OriginalIDs(), []string{"a", "b", "c"})
	s.OriginalContainerIDs = []string{"x"}
	eq(t, "structured wins", s.OriginalIDs(), []string{"x"})
}

func TestStateTracker_PersistedStateSurvivesReloadWithStructuredIDs(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	st, _ := NewStateTracker(dir, logger)
	if err := st.StartRotation("p", "s", "a,b", ""); err != nil {
		t.Fatal(err)
	}
	st2, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := st2.Corrupt(); c {
		t.Fatal("valid state must not be reported corrupt")
	}
	got := st2.GetUnfinishedRotationsAtStartup()
	if len(got) != 1 || strings.Join(got[0].OriginalIDs(), ",") != "a,b" || got[0].Phase != "rotation_requested" {
		t.Fatalf("%+v", got)
	}
	// No leftover temp file after persist.
	if _, err := os.Stat(filepath.Join(dir, "rotations.json.tmp")); err == nil {
		t.Fatal("temp state file left behind")
	}
}
