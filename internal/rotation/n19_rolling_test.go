package rotation

// N19 managed file delivery through RollingStrategy.Execute, against the stateful
// fake daemon and the real gate/verification/journal code. No Docker daemon.
// The scenarios named in the N19 test matrix: candidate creation with tmpfs, gate
// start failure, missing/incorrect tmpfs, identity mismatch, unsafe destination,
// partial delivery, failed verification, every crash point, failed application start,
// failed readiness, failed routing handoff, successful commit.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

const (
	n19Sentinel = "DSO_N19_SENTINEL_VALUE_5c2e"
	n19Ver      = "hmac:k1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

type n19Rig struct {
	rs   *RollingStrategy
	d    *n19Daemon
	rec  *n19Rec
	j    *memJournal
	spec []FileSecretSpec
}

func n19Original() n19Ctr {
	return n19Ctr{
		id: "cid", name: "my-container",
		cfg: container.Config{
			Image: "app:1", User: "1000:1000", WorkingDir: "/srv",
			Entrypoint: []string{"/app/entry", "--flag=a b"},
			Cmd:        []string{"serve", "--name", "x;y $Z", "line\nbreak"},
			Env:        []string{"EXISTING=true"},
		},
		hc: &container.HostConfig{Tmpfs: map[string]string{"/run/secrets/dso": "rw,noexec,nosuid,size=1m"}, ReadonlyRootfs: true},
	}
}

func newN19Rig(t *testing.T, mutate func(o *n19Ctr)) *n19Rig {
	t.Helper()
	rec := &n19Rec{}
	o := n19Original()
	if mutate != nil {
		mutate(&o)
	}
	d, cli := newN19Daemon(t, rec, o)
	j := &memJournal{}
	rs := NewRollingStrategy(cli)
	rs.Journal, rs.OwnerID = j, "agent-1"
	rs.DeliveryOps = rec.ops()
	rs.renameTimeout = 5 * time.Second
	return &n19Rig{rs: rs, d: d, rec: rec, j: j, spec: []FileSecretSpec{{
		Path: "/run/secrets/dso", Data: map[string]string{"db-password": n19Sentinel}, UID: 1000, GID: 1000, Version: n19Ver,
	}}}
}

func (r *n19Rig) run() error {
	return r.rs.Execute(context.Background(), "cid", map[string]string{"PLAIN": "v"}, r.spec, nil, 2*time.Second)
}

func idx(events []string, want string) int {
	for i, e := range events {
		if e == want {
			return i
		}
	}
	return -1
}

func (r *n19Rig) assertOriginalUntouched(t *testing.T) {
	t.Helper()
	if r.d.n("rename:cid->my-container_dso_backup_") != 0 {
		t.Fatal("unexpected backup rename")
	}
	for _, l := range r.d.log {
		if strings.HasPrefix(l, "rename:cid->") || l == "remove:cid" || l == "stop:cid" {
			t.Fatalf("the original container was touched: %s (log %v)", l, r.d.log)
		}
	}
}

func (r *n19Rig) assertCandidateDiscarded(t *testing.T) {
	t.Helper()
	if r.d.n("remove:cand-1") == 0 {
		t.Fatalf("the failed candidate was not removed (log %v)", r.d.log)
	}
}

// Candidate creation: tmpfs declared, gate armed, original contract preserved, no
// secret in the create request, no archive call; success ordering and journal.
func TestN19Rolling_Success_GateArmedDeliveryOrderedAndJournalled(t *testing.T) {
	r := newN19Rig(t, nil)
	if err := r.run(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	c := r.d.ctrs["cand-1"]
	if c == nil {
		t.Fatal("no candidate was created")
	}
	// original contract preserved
	ep, cmd := OriginalCommand(&c.cfg)
	if strings.Join(ep, "|") != "/app/entry|--flag=a b" {
		t.Errorf("original ENTRYPOINT not preserved: %q", ep)
	}
	if strings.Join(cmd, "|") != "serve|--name|x;y $Z|line\nbreak" {
		t.Errorf("CMD changed: %q", cmd)
	}
	if c.cfg.User != "1000:1000" || c.cfg.WorkingDir != "/srv" || !c.hc.ReadonlyRootfs || c.hc.Tmpfs["/run/secrets/dso"] == "" {
		t.Errorf("runtime settings changed: user=%q wd=%q ro=%v tmpfs=%v", c.cfg.User, c.cfg.WorkingDir, c.hc.ReadonlyRootfs, c.hc.Tmpfs)
	}
	if !IsGated(&c.cfg) || c.cfg.Labels[GateLabel] == "" {
		t.Error("candidate was not created with the startup gate")
	}
	// no secret anywhere the daemon keeps (config, labels, env, entrypoint)
	for _, b := range r.rec.created {
		if strings.Contains(string(b), n19Sentinel) {
			t.Fatal("secret value reached the container create request")
		}
	}
	if strings.Contains(GateScript(), n19Sentinel) || r.d.archiveCalls != 0 {
		t.Fatal("secret in gate script or archive endpoint used")
	}
	ev := r.rec.events
	order := []string{"docker:create", "docker:start:cand-1", "inject:/run/secrets/dso", "probe:/run/secrets/dso", "release:/run/secrets/dso", "docker:rename:cand-1->my-container"}
	last := -1
	for _, want := range order {
		i := idx(ev, want)
		if i < 0 || i < last {
			t.Fatalf("%s missing or out of order in %v", want, ev)
		}
		last = i
	}
	// journal: delivery phases in order, ready only after delivery evidence
	tr := r.j.transitions()
	want := []string{TrIntentCreateCandidate, TrCandidateCreated, TrGateStarted, TrIntentDeliverSecrets, TrSecretsDelivered, TrIntentReleaseGate, TrGateReleased, TrCandidateReady}
	last = -1
	for _, w := range want {
		i := idx(tr, w)
		if i < 0 || i < last {
			t.Fatalf("journal %v lacks %s in order", tr, w)
		}
		last = i
	}
	if e := r.j.event(TrCandidateCreated); e == nil || !e.Gated {
		t.Error("candidate_created must record Gated")
	}
	if e := r.j.event(TrSecretsDelivered); e == nil || len(e.DeliveredVersions) != 1 || e.DeliveredVersions[0] != n19Ver {
		t.Errorf("secrets_delivered must carry the keyed version, got %+v", e)
	}
	if r.j.last() != TrCompleted {
		t.Errorf("operation not committed/completed: %v", tr)
	}
	for _, e := range r.j.events {
		if strings.Contains(e.Detail, n19Sentinel) || strings.Contains(e.State, n19Sentinel) {
			t.Fatal("secret in journal")
		}
	}
}

func TestN19Rolling_RefusalsHappenBeforeAnyChange(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(o *n19Ctr)
		spec   func(s *[]FileSecretSpec)
		want   string
	}{
		{"missing tmpfs", func(o *n19Ctr) { o.hc.Tmpfs = nil }, nil, "not covered by a tmpfs"},
		{"bind mount instead of tmpfs", func(o *n19Ctr) {
			o.hc.Tmpfs = nil
			o.hc.Binds = []string{"/host/secrets:/run/secrets/dso:rw"}
		}, nil, "bind mount or volume"},
		{"volume shadows the tmpfs", func(o *n19Ctr) {
			o.hc.Mounts = []mount.Mount{{Type: mount.TypeVolume, Target: "/run/secrets/dso/sub"}}
		}, func(s *[]FileSecretSpec) { (*s)[0].Path = "/run/secrets/dso/sub" }, "bind mount or volume"},
		{"read-only tmpfs", func(o *n19Ctr) { o.hc.Tmpfs = map[string]string{"/run/secrets/dso": "ro"} }, nil, "read-only"},
		{"unsafe relative destination", nil, func(s *[]FileSecretSpec) { (*s)[0].Path = "run/secrets" }, "unsafe destination"},
		{"destination under /proc", nil, func(s *[]FileSecretSpec) { (*s)[0].Path = "/proc/x" }, "pseudo filesystem"},
		{"traversal in destination", nil, func(s *[]FileSecretSpec) { (*s)[0].Path = "/run/secrets/../x" }, "unsafe destination"},
		{"unsafe file name", nil, func(s *[]FileSecretSpec) { (*s)[0].Data = map[string]string{"a/b": "v"} }, "invalid secret file name"},
		{"reserved gate flag name", nil, func(s *[]FileSecretSpec) { (*s)[0].Data = map[string]string{GateFlagName: "v"} }, "reserved"},
		{"no ENTRYPOINT and no CMD", func(o *n19Ctr) { o.cfg.Entrypoint, o.cfg.Cmd = nil, nil }, nil, "neither an ENTRYPOINT nor a CMD"},
		{"owner the exec cannot produce", func(o *n19Ctr) { o.cfg.User = "2000" }, nil, "cannot chown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newN19Rig(t, tc.mutate)
			if tc.spec != nil {
				tc.spec(&r.spec)
			}
			err := r.run()
			if err == nil || !errors.Is(err, ErrDeliveryRefused) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a delivery refusal containing %q", err, tc.want)
			}
			if r.d.n("create") != 0 {
				t.Fatal("a candidate was created although delivery cannot be done safely")
			}
			if len(r.j.events) != 0 {
				t.Fatalf("a refusal must leave no journal trace: %v", r.j.transitions())
			}
			if r.rec.has("inject:/run/secrets/dso") {
				t.Fatal("secret material was written")
			}
			r.assertOriginalUntouched(t)
		})
	}
}

func TestN19Rolling_GateStartFailure_NothingDelivered(t *testing.T) {
	r := newN19Rig(t, nil)
	r.d.startErr = true
	if err := r.run(); err == nil || !strings.Contains(err.Error(), "failed to start new container") {
		t.Fatalf("err = %v", err)
	}
	if r.rec.has("inject:/run/secrets/dso") || r.rec.has("release:/run/secrets/dso") {
		t.Fatal("delivery must not run when the gated candidate failed to start")
	}
	r.assertCandidateDiscarded(t)
	r.assertOriginalUntouched(t)
	if r.j.event(TrSecretsDelivered) != nil || r.j.event(TrCandidateReady) != nil {
		t.Fatalf("journal claims delivery/readiness: %v", r.j.transitions())
	}
}

func TestN19Rolling_IdentityAndMountVerificationFailClosed(t *testing.T) {
	cases := []struct {
		name string
		edit func(o *n19Rec, in *container.InspectResponse)
		want string
	}{
		{"identity mismatch", func(_ *n19Rec, in *container.InspectResponse) { in.ID = "someone-else" }, "identity mismatch"},
		{"not the gate process", func(_ *n19Rec, in *container.InspectResponse) { in.Path = "/app/entry" }, "not the gate shell"},
		{"gate removed from entrypoint", func(_ *n19Rec, in *container.InspectResponse) {
			in.Config.Entrypoint = []string{"/app/entry"}
		}, "not gated"},
		{"wrong rotation id", func(_ *n19Rec, in *container.InspectResponse) { in.Config.Labels[LabelRotationID] = "rot-other" }, "rotation id"},
		{"destination is a bind mount at runtime", func(_ *n19Rec, in *container.InspectResponse) {
			in.Mounts = []container.MountPoint{{Type: mount.TypeBind, Destination: "/run/secrets/dso", RW: true}}
		}, "not a tmpfs"},
		{"destination is a volume at runtime", func(_ *n19Rec, in *container.InspectResponse) {
			in.Mounts = []container.MountPoint{{Type: mount.TypeVolume, Destination: "/run/secrets", RW: true}}
		}, "not a tmpfs"},
		{"read-only tmpfs at runtime", func(_ *n19Rec, in *container.InspectResponse) {
			in.Mounts = []container.MountPoint{{Type: mount.TypeTmpfs, Destination: "/run/secrets/dso", RW: false}}
		}, "read-only"},
		{"gate already exited", func(_ *n19Rec, in *container.InspectResponse) { in.State.Running = false }, "not running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newN19Rig(t, nil)
			base := r.rec.ops()
			ops := *base
			ops.Inspect = func(ctx context.Context, id string) (container.InspectResponse, error) {
				in, err := base.Inspect(ctx, id)
				if in.Config != nil {
					lbls := map[string]string{}
					for k, v := range in.Config.Labels {
						lbls[k] = v
					}
					in.Config.Labels = lbls
				}
				tc.edit(r.rec, &in)
				return in, err
			}
			r.rs.DeliveryOps = &ops
			err := r.run()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if r.rec.has("inject:/run/secrets/dso") {
				t.Fatal("secret material was written despite a failed identity/mount verification")
			}
			if r.j.event(TrSecretsDelivered) != nil {
				t.Fatal("delivery evidence journalled")
			}
			r.assertCandidateDiscarded(t)
			r.assertOriginalUntouched(t)
		})
	}
}

func TestN19Rolling_PartialDelivery_JournalledAndNeverReleased(t *testing.T) {
	r := newN19Rig(t, func(o *n19Ctr) {
		o.hc.Tmpfs["/run/secrets/b"] = ""
	})
	r.spec = append(r.spec, FileSecretSpec{Path: "/run/secrets/b", Data: map[string]string{"k2": "v2"}, UID: 1000, GID: 1000, Version: n19Ver})
	base := r.rec.ops()
	ops := *base
	ops.Inject = func(ctx context.Context, id string, s FileSecretSpec) error {
		if s.Path == "/run/secrets/b" {
			return errN19Sim
		}
		return base.Inject(ctx, id, s)
	}
	r.rs.DeliveryOps = &ops
	err := r.run()
	var de *DeliveryError
	if !errors.As(err, &de) || !de.Partial || de.Phase != "inject" {
		t.Fatalf("err = %v, want a partial inject DeliveryError", err)
	}
	if r.j.event(TrDeliveryPartial) == nil {
		t.Fatalf("partial delivery not journalled: %v", r.j.transitions())
	}
	if r.j.event(TrSecretsDelivered) != nil || r.rec.has("release:/run/secrets/b") || r.rec.has("release:/run/secrets/dso") {
		t.Fatal("a partially delivered candidate must never be marked delivered or released")
	}
	r.assertCandidateDiscarded(t)
	r.assertOriginalUntouched(t)
}

func TestN19Rolling_FailedVerificationJournalAndRelease(t *testing.T) {
	t.Run("probe fails", func(t *testing.T) {
		r := newN19Rig(t, nil)
		r.rec.probeErr = errN19Sim
		if err := r.run(); err == nil {
			t.Fatal("expected failure")
		}
		if r.j.event(TrSecretsDelivered) != nil || r.rec.has("release:/run/secrets/dso") {
			t.Fatal("unverified delivery was journalled or the gate released")
		}
		r.assertCandidateDiscarded(t)
	})
	t.Run("delivery evidence cannot be journalled", func(t *testing.T) {
		r := newN19Rig(t, nil)
		r.j.failOn = TrSecretsDelivered
		if err := r.run(); err == nil {
			t.Fatal("expected failure")
		}
		if r.rec.has("release:/run/secrets/dso") {
			t.Fatal("the gate was released although the delivery evidence could not be made durable")
		}
		r.assertCandidateDiscarded(t)
	})
	t.Run("release intent cannot be journalled", func(t *testing.T) {
		r := newN19Rig(t, nil)
		r.j.failOn = TrIntentReleaseGate
		if err := r.run(); err == nil || r.rec.has("release:/run/secrets/dso") {
			t.Fatalf("gate released without a durable intent (err %v)", err)
		}
	})
	t.Run("release fails", func(t *testing.T) {
		r := newN19Rig(t, nil)
		r.rec.relErr = errN19Sim
		if err := r.run(); err == nil {
			t.Fatal("expected failure")
		}
		if r.j.event(TrGateReleased) != nil || r.j.event(TrCandidateReady) != nil {
			t.Fatal("journal claims a release that failed")
		}
		r.assertCandidateDiscarded(t)
	})
}

func TestN19Rolling_FailedApplicationStartAndReadiness(t *testing.T) {
	t.Run("application exits after release", func(t *testing.T) {
		r := newN19Rig(t, nil)
		r.d.candExited = true
		if err := r.run(); err == nil || !errors.Is(err, ErrUnhealthy) {
			t.Fatalf("err = %v, want an unhealthy rejection", err)
		}
		if !r.j.has(TrGateReleased) || r.j.event(TrCandidateReady) != nil {
			t.Fatalf("journal %v: released but never ready", r.j.transitions())
		}
		r.assertCandidateDiscarded(t)
		r.assertOriginalUntouched(t)
	})
	t.Run("never healthy", func(t *testing.T) {
		r := newN19Rig(t, nil)
		r.d.candHealth = "unhealthy"
		if err := r.run(); err == nil || !errors.Is(err, ErrUnhealthy) {
			t.Fatalf("err = %v", err)
		}
		if r.j.event(TrCandidateReady) != nil {
			t.Fatal("ready journalled for an unhealthy candidate")
		}
		r.assertCandidateDiscarded(t)
		r.assertOriginalUntouched(t)
	})
}

func (j *memJournal) has(tr string) bool { return j.event(tr) != nil }

func TestN19Rolling_FailedRoutingHandoff_DiscardsAfterReady(t *testing.T) {
	r := newN19Rig(t, nil)
	r.rs.OnReady = func(context.Context, string) error { return errN19Sim }
	if err := r.run(); err == nil || !strings.Contains(err.Error(), "proxy handoff failed") {
		t.Fatalf("err = %v", err)
	}
	if !r.j.has(TrCandidateReady) || !r.j.has(TrIntentHandoff) || r.j.has(TrHandoffDone) || r.j.has(TrSwapCommitted) {
		t.Fatalf("journal %v", r.j.transitions())
	}
	r.assertCandidateDiscarded(t)
	r.assertOriginalUntouched(t)
}

// Crash at every delivery point: the process dies (panic) with NO cleanup. What stays
// behind is exactly what recovery sees. secrets_delivered is the only delivery
// evidence; the application never ran before the gate was released; candidate_ready is
// never journalled by a crash before readiness.
func TestN19Rolling_CrashAtEveryPoint(t *testing.T) {
	type exp struct {
		last     string // last durable transition
		injected bool   // a file was written into the candidate's tmpfs
		released bool   // the release flag was written
	}
	cases := []struct {
		point string
		want  exp
	}{
		{"after_candidate_create_before_ack", exp{TrIntentCreateCandidate, false, false}},
		{"after_secret_delivery", exp{TrCandidateCreated, false, false}}, // created, never started
		{"after_gate_started", exp{TrGateStarted, false, false}},         // crash before delivery
		{"after_intent_deliver", exp{TrIntentDeliverSecrets, false, false}},
		{"after_spec_delivered", exp{TrIntentDeliverSecrets, true, false}}, // crash during delivery
		{"after_delivery_before_probe", exp{TrIntentDeliverSecrets, true, false}},
		{"after_probe_before_ack", exp{TrIntentDeliverSecrets, true, false}}, // delivered, unacknowledged
		{"after_secrets_delivered", exp{TrSecretsDelivered, true, false}},    // acknowledged, gate closed
		{"after_intent_release", exp{TrIntentReleaseGate, true, false}},
		{"after_release_before_ack", exp{TrIntentReleaseGate, true, true}}, // flag written, unacknowledged
		{"after_gate_released", exp{TrGateReleased, true, true}},           // crash after release, before readiness
		{"after_start_before_verify", exp{TrGateReleased, true, true}},
		{"after_candidate_verified", exp{TrGateReleased, true, true}}, // reached only after readiness passed, before the ack
	}
	for _, tc := range cases {
		t.Run(tc.point, func(t *testing.T) {
			r := newN19Rig(t, nil)
			reached := false
			r.rs.FaultHook = func(p string) {
				if p == tc.point {
					reached = true
					panic("crash:" + p)
				}
			}
			func() {
				defer func() { _ = recover() }()
				_ = r.run()
			}()
			if !reached {
				t.Fatalf("fault point %s was never reached", tc.point)
			}
			if got := r.j.last(); got != tc.want.last {
				t.Errorf("last durable transition = %s, want %s (all: %v)", got, tc.want.last, r.j.transitions())
			}
			if got := r.rec.has("inject:/run/secrets/dso"); got != tc.want.injected {
				t.Errorf("injected = %v, want %v", got, tc.want.injected)
			}
			if got := r.rec.has("release:/run/secrets/dso"); got != tc.want.released {
				t.Errorf("released = %v, want %v", got, tc.want.released)
			}
			if r.j.has(TrCandidateReady) {
				t.Error("candidate_ready journalled by a crash before readiness was acknowledged")
			}
			// "released" implies delivery evidence was durable first
			if r.rec.has("release:/run/secrets/dso") && !r.j.has(TrSecretsDelivered) {
				t.Error("the gate was released before delivery evidence was durable")
			}
			// a crash performs no cleanup: original untouched, candidate left for recovery
			r.assertOriginalUntouched(t)
			if c := r.d.ctrs["cand-1"]; tc.point != "after_candidate_create_before_ack" && (c == nil || c.removed) {
				t.Error("a crash must leave the candidate for recovery")
			}
		})
	}
}
