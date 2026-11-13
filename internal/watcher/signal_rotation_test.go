package watcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

const sigSentinel = "SYNTHETIC-SIGNAL-ROTATION-VALUE-7710"

type mapCache map[string]map[string]string

func (m mapCache) Get(k string) (map[string]string, bool) { v, ok := m[k]; return v, ok }

type opLog struct {
	mu     sync.Mutex
	events []string
}

func (l *opLog) add(e string) { l.mu.Lock(); l.events = append(l.events, e); l.mu.Unlock() }
func (l *opLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}
func (l *opLog) has(prefix string) bool {
	for _, e := range l.get() {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func fileSecret(name, path string) dsoConfig.SecretMapping {
	return dsoConfig.SecretMapping{Name: name, Provider: "vault", Inject: dsoConfig.InjectionConfig{Type: "file", Path: path, UID: 1000, GID: 1000}}
}

func newSignalController(t *testing.T, secrets ...dsoConfig.SecretMapping) *ReloaderController {
	t.Helper()
	cache := mapCache{}
	for _, s := range secrets {
		cache["vault:"+s.Name] = map[string]string{"password": sigSentinel + "-" + s.Name}
	}
	return &ReloaderController{
		Logger: zap.NewNop(),
		Config: &dsoConfig.Config{
			Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
			Agent:     dsoConfig.AgentConfig{OwnerID: "me"},
			Secrets:   secrets,
		},
		Cache: cache,
	}
}

func okOps(l *opLog) signalOps {
	return signalOps{
		inspect: func(ctx context.Context, id string) (container.InspectResponse, error) {
			l.add("inspect")
			return container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{Name: "/app", State: &container.State{Running: true}},
				Config:            &container.Config{Labels: map[string]string{"dso.owner": "me"}},
			}, nil
		},
		deliver: func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
			l.add("deliver:" + spec.Path)
			return nil
		},
		probe: func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
			l.add("probe:" + spec.Path)
			return nil
		},
		signal: func(ctx context.Context, id, sig string) error {
			l.add("signal:" + sig)
			return nil
		},
		ready: func(ctx context.Context, id string, to time.Duration) (rotation.Readiness, error) {
			l.add("ready")
			return rotation.ReadinessProcessStable, nil
		},
	}
}

func idx(ev []string, prefix string) int {
	for i, e := range ev {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

func noLeak(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), sigSentinel) {
		t.Fatalf("error leaked a secret value: %v", err)
	}
}

func TestSignalRotation_DeliversAndVerifiesBeforeSignalling(t *testing.T) {
	r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
	l := &opLog{}
	out, err := r.signalRotation(context.Background(), okOps(l), &TargetContainer{ID: "c1", Secrets: []string{"db"}}, "svc", "db")
	if err != nil {
		t.Fatal(err)
	}
	ev := l.get()
	d, p, s, rd := idx(ev, "deliver:"), idx(ev, "probe:"), idx(ev, "signal:"), idx(ev, "ready")
	if !(d >= 0 && d < p && p < s && s < rd) {
		t.Fatalf("order must be deliver < probe < signal < ready, got %v", ev)
	}
	if ev[s] != "signal:SIGHUP" {
		t.Fatalf("default reload signal is SIGHUP, got %s", ev[s])
	}
	if !out.Delivered || !out.ReloadRequested {
		t.Fatalf("outcome %+v", out)
	}
	if out.AdoptionVerified {
		t.Fatal("adoption must never be claimed: DSO cannot observe the application reloading")
	}
	if out.Readiness != rotation.ReadinessProcessStable || !strings.Contains(out.Limitation, "NOT verified") {
		t.Fatalf("process-only readiness and the limitation must be reported honestly: %+v", out)
	}
	if strings.Join(out.DeliveredFiles, ",") != "password" {
		t.Fatalf("delivered files %v", out.DeliveredFiles)
	}
	if _, bad := r.IsDegraded("svc"); bad {
		t.Fatal("a clean rotation must not degrade the service")
	}
}

func TestSignalRotation_FailureBeforePublishNeverSignals(t *testing.T) {
	r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
	l := &opLog{}
	ops := okOps(l)
	ops.deliver = func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
		l.add("deliver:" + spec.Path)
		return errors.New("secret write command exited 69 (integrity check failed)")
	}
	out, err := r.signalRotation(context.Background(), ops, &TargetContainer{ID: "c1", Secrets: []string{"db"}}, "svc", "db")
	noLeak(t, err)
	if err == nil {
		t.Fatal("failed delivery must fail the rotation")
	}
	var serr *SignalRotationError
	if !errors.As(err, &serr) || serr.State != SignalNothingPublished || out.State != SignalNothingPublished {
		t.Fatalf("a first-file failure publishes nothing: %v / %q", err, out.State)
	}
	if strings.Contains(err.Error(), "NOT restored") {
		t.Fatalf("nothing was published, so there is nothing to restore: %v", err)
	}
	if l.has("signal:") || l.has("probe:") || l.has("ready") {
		t.Fatalf("nothing may follow a failed delivery: %v", l.get())
	}
	if _, bad := r.IsDegraded("svc"); bad {
		t.Fatal("first-file failure published nothing; the previous secret is intact, service must not be marked degraded")
	}
}

func TestSignalRotation_FailureAfterPublishIsReportedAsUnrecoverable(t *testing.T) {
	cases := map[string]func(*signalOps, *opLog){
		"second secret fails after first was published": func(o *signalOps, l *opLog) {
			n := 0
			o.deliver = func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
				n++
				l.add("deliver:" + spec.Path)
				if n == 2 {
					return errors.New("disk full")
				}
				return nil
			}
		},
		"probe fails": func(o *signalOps, l *opLog) {
			o.probe = func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
				l.add("probe:" + spec.Path)
				return rotation.ErrUnhealthy
			}
		},
		"signal fails": func(o *signalOps, l *opLog) {
			o.signal = func(ctx context.Context, id, sig string) error {
				l.add("signal:" + sig)
				return errors.New("no such container")
			}
		},
		"readiness fails": func(o *signalOps, l *opLog) {
			o.ready = func(ctx context.Context, id string, to time.Duration) (rotation.Readiness, error) {
				l.add("ready")
				return rotation.ReadinessNone, fmt.Errorf("container became unhealthy: %w", rotation.ErrUnhealthy)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := newSignalController(t, fileSecret("a", "/run/secrets/a"), fileSecret("b", "/run/secrets/b"))
			l := &opLog{}
			ops := okOps(l)
			mutate(&ops, l)
			out, err := r.signalRotation(context.Background(), ops, &TargetContainer{ID: "c1", Secrets: []string{"a", "b"}}, "svc", "a")
			noLeak(t, err)
			if err == nil {
				t.Fatal("must not be reported as a successful rotation")
			}
			wantState := map[string]SignalState{
				"second secret fails after first was published": SignalPartiallyPublished,
				"probe fails":     SignalPublishedUnverified,
				"signal fails":    SignalPublishedNotSignalled,
				"readiness fails": SignalSentOutcomeUnknown,
			}[name]
			var serr *SignalRotationError
			if !errors.As(err, &serr) || serr.State != wantState || out.State != wantState {
				t.Fatalf("want state %q, got %v / outcome %q", wantState, err, out.State)
			}
			if strings.Contains(strings.ToLower(err.Error()), "irrecoverable") || strings.Contains(strings.ToLower(err.Error()), "unrecoverable") {
				t.Fatalf("the state is uncertain, not unrecoverable: %v", err)
			}
			if !strings.Contains(err.Error(), "NOT restored") || !strings.Contains(err.Error(), "manual recovery") {
				t.Fatalf("error must say the previous value was not restored and recovery is manual: %v", err)
			}
			if reason, bad := r.IsDegraded("svc"); !bad || !strings.Contains(reason, "manual recovery") {
				t.Fatalf("service must be flagged degraded, got %q %v", reason, bad)
			}
			if out.AdoptionVerified {
				t.Fatal("adoption can never be claimed")
			}
			if name == "probe fails" || name == "second secret fails after first was published" {
				if l.has("signal:") {
					t.Fatalf("signal must not follow a failed delivery/verification: %v", l.get())
				}
			}
		})
	}
}

func TestSignalRotation_EnvironmentSecretsAreRejectedWithoutSideEffects(t *testing.T) {
	envSecret := dsoConfig.SecretMapping{Name: "envsec", Provider: "vault", Inject: dsoConfig.InjectionConfig{Type: "env"}}
	for name, secrets := range map[string][]dsoConfig.SecretMapping{
		"env only":                             {envSecret},
		"file plus env (nothing is delivered)": {fileSecret("db", "/run/secrets/dso"), envSecret},
	} {
		t.Run(name, func(t *testing.T) {
			r := newSignalController(t, secrets...)
			l := &opLog{}
			_, err := r.signalRotation(context.Background(), okOps(l), &TargetContainer{ID: "c1", Secrets: []string{"db", "envsec"}}, "svc", "envsec")
			if !errors.Is(err, ErrSignalEnvSecret) {
				t.Fatalf("want ErrSignalEnvSecret, got %v", err)
			}
			if l.has("deliver:") || l.has("signal:") {
				t.Fatalf("a rejected rotation must change nothing: %v", l.get())
			}
		})
	}
	// Default inject type applies when a secret leaves it empty.
	r := newSignalController(t, dsoConfig.SecretMapping{Name: "db", Provider: "vault"})
	r.Config.Defaults.Inject.Type = "env"
	l := &opLog{}
	if _, err := r.signalRotation(context.Background(), okOps(l), &TargetContainer{ID: "c1", Secrets: []string{"db"}}, "svc", "db"); !errors.Is(err, ErrSignalEnvSecret) {
		t.Fatalf("defaults.inject env must be rejected too, got %v", err)
	}
}

func TestSignalRotation_RefusesUnownedUnhealthyOrIncompleteTargets(t *testing.T) {
	t.Run("not owned (missing label)", func(t *testing.T) {
		r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
		l := &opLog{}
		ops := okOps(l)
		ops.inspect = func(ctx context.Context, id string) (container.InspectResponse, error) {
			return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{Name: "/x", State: &container.State{Running: true}}, Config: &container.Config{Labels: map[string]string{}}}, nil
		}
		if _, err := r.signalRotation(context.Background(), ops, &TargetContainer{ID: "c9", Secrets: []string{"db"}}, "s", "db"); !errors.Is(err, ErrSignalNotOwned) {
			t.Fatalf("got %v", err)
		}
		if l.has("deliver:") || l.has("signal:") {
			t.Fatal("an unowned container must never be written to or signalled")
		}
	})
	t.Run("owned by another agent", func(t *testing.T) {
		r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
		l := &opLog{}
		ops := okOps(l)
		ops.inspect = func(ctx context.Context, id string) (container.InspectResponse, error) {
			return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{Name: "/x", State: &container.State{Running: true}}, Config: &container.Config{Labels: map[string]string{"dso.owner": "someone-else"}}}, nil
		}
		if _, err := r.signalRotation(context.Background(), ops, &TargetContainer{ID: "c9", Secrets: []string{"db"}}, "s", "db"); !errors.Is(err, ErrSignalNotOwned) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("not running", func(t *testing.T) {
		r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
		l := &opLog{}
		ops := okOps(l)
		ops.inspect = func(ctx context.Context, id string) (container.InspectResponse, error) {
			return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{Name: "/x", State: &container.State{Running: false}}, Config: &container.Config{Labels: map[string]string{"dso.owner": "me"}}}, nil
		}
		if _, err := r.signalRotation(context.Background(), ops, &TargetContainer{ID: "c", Secrets: []string{"db"}}, "s", "db"); err == nil || l.has("deliver:") {
			t.Fatalf("got %v %v", err, l.get())
		}
	})
	t.Run("inspect error", func(t *testing.T) {
		r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
		ops := okOps(&opLog{})
		ops.inspect = func(ctx context.Context, id string) (container.InspectResponse, error) {
			return container.InspectResponse{}, errors.New("daemon down")
		}
		if _, err := r.signalRotation(context.Background(), ops, &TargetContainer{ID: "c", Secrets: []string{"db"}}, "s", "db"); err == nil {
			t.Fatal("must fail")
		}
	})
	t.Run("provider value missing from cache", func(t *testing.T) {
		r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
		r.Cache = mapCache{}
		l := &opLog{}
		if _, err := r.signalRotation(context.Background(), okOps(l), &TargetContainer{ID: "c", Secrets: []string{"db"}}, "s", "db"); err == nil || l.has("deliver:") || l.has("signal:") {
			t.Fatalf("missing cache must fail before any change: %v %v", err, l.get())
		}
	})
	t.Run("no file secret mapped", func(t *testing.T) {
		r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
		if _, err := r.signalRotation(context.Background(), okOps(&opLog{}), &TargetContainer{ID: "c", Secrets: []string{"other"}}, "s", "other"); !errors.Is(err, ErrSignalNoFileSecret) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestSignalRotation_ConfiguredSignalAndAllowList(t *testing.T) {
	mk := func(sig string) *ReloaderController {
		s := fileSecret("db", "/run/secrets/dso")
		s.Rotation.Signal = sig
		return newSignalController(t, s)
	}
	for in, want := range map[string]string{"SIGUSR1": "signal:SIGUSR1", "usr2": "signal:SIGUSR2", "HUP": "signal:SIGHUP"} {
		l := &opLog{}
		if _, err := mk(in).signalRotation(context.Background(), okOps(l), &TargetContainer{ID: "c", Secrets: []string{"db"}}, "s", "db"); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if !l.has(want) {
			t.Fatalf("%s: want %s, got %v", in, want, l.get())
		}
	}
	for _, bad := range []string{"SIGKILL", "SIGTERM", "SIGINT", "9", "SIGQUIT", "bogus"} {
		l := &opLog{}
		_, err := mk(bad).signalRotation(context.Background(), okOps(l), &TargetContainer{ID: "c", Secrets: []string{"db"}}, "s", "db")
		if !errors.Is(err, ErrSignalBadSignal) {
			t.Fatalf("%s must be rejected, got %v", bad, err)
		}
		if l.has("deliver:") || l.has("signal:") {
			t.Fatalf("%s: nothing may happen: %v", bad, l.get())
		}
	}
	// Conflicting signals across one container's secrets.
	a, b := fileSecret("a", "/run/secrets/a"), fileSecret("b", "/run/secrets/b")
	a.Rotation.Signal, b.Rotation.Signal = "SIGHUP", "SIGUSR1"
	if _, err := newSignalController(t, a, b).signalRotation(context.Background(), okOps(&opLog{}), &TargetContainer{ID: "c", Secrets: []string{"a", "b"}}, "s", "a"); err == nil {
		t.Fatal("conflicting reload signals must be rejected")
	}
}

func TestSignalRotation_ConcurrentRotationsOfOneContainerNeverInterleave(t *testing.T) {
	r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
	l := &opLog{}
	ops := okOps(l)
	slow := ops.deliver
	ops.deliver = func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
		_ = slow(ctx, id, spec)
		time.Sleep(15 * time.Millisecond) // widen the window in which an unserialised peer would interleave
		return nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.signalRotation(context.Background(), ops, &TargetContainer{ID: "same", Secrets: []string{"db"}}, "svc", "db")
		}()
	}
	wg.Wait()
	ev := l.get()
	want := []string{"inspect", "deliver:/run/secrets/dso", "probe:/run/secrets/dso", "signal:SIGHUP", "ready"}
	if len(ev) != 8*len(want) {
		t.Fatalf("unexpected number of operations: %d", len(ev))
	}
	for i := 0; i < len(ev); i += len(want) {
		if strings.Join(ev[i:i+len(want)], ",") != strings.Join(want, ",") {
			t.Fatalf("rotations interleaved at %d: %v", i, ev[i:i+len(want)])
		}
	}
}

func TestSignalRotation_CancellationIsAFailureNotASuccess(t *testing.T) {
	r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
	l := &opLog{}
	ops := okOps(l)
	ctx, cancel := context.WithCancel(context.Background())
	ops.deliver = func(c context.Context, id string, spec rotation.FileSecretSpec) error {
		cancel()
		return c.Err()
	}
	_, err := r.signalRotation(ctx, ops, &TargetContainer{ID: "c", Secrets: []string{"db"}}, "s", "db")
	if err == nil || l.has("signal:") {
		t.Fatalf("cancelled rotation must fail and not signal: %v %v", err, l.get())
	}
}

func TestNormalizeSignal(t *testing.T) {
	for in, want := range map[string]string{"": "SIGHUP", "hup": "SIGHUP", " sigusr1 ": "SIGUSR1", "USR2": "SIGUSR2"} {
		got, err := normalizeSignal(in)
		if err != nil || got != want {
			t.Errorf("%q => %q, %v", in, got, err)
		}
	}
}

// A single secret mapped to several files: a failure part-way leaves a mixed set.
func TestSignalRotation_MultiFileSecretFailureIsPartialAndNeverSignals(t *testing.T) {
	sec := fileSecret("db", "/run/secrets/dso")
	r := newSignalController(t, sec)
	r.Cache = mapCache{"vault:db": map[string]string{"user": sigSentinel + "-u", "password": sigSentinel + "-p"}}
	l := &opLog{}
	ops := okOps(l)
	ops.deliver = func(ctx context.Context, id string, spec rotation.FileSecretSpec) error {
		l.add("deliver:" + spec.Path)
		return errors.New("write failed after the first file")
	}
	out, err := r.signalRotation(context.Background(), ops, &TargetContainer{ID: "c", Secrets: []string{"db"}}, "svc", "db")
	var serr *SignalRotationError
	if !errors.As(err, &serr) || serr.State != SignalPartiallyPublished || out.State != SignalPartiallyPublished {
		t.Fatalf("a multi-file failure may have published some files: %v / %q", err, out.State)
	}
	if l.has("signal:") || l.has("probe:") {
		t.Fatalf("no probe or signal after incomplete delivery: %v", l.get())
	}
	if out.Delivered {
		t.Fatal("the secret set must not be reported as delivered/applied")
	}
	if _, bad := r.IsDegraded("svc"); !bad {
		t.Fatal("a possibly inconsistent secret set must be flagged")
	}
}

func TestSignalRotation_SuccessStateAndLimitationDocumentDegradedStateIsInMemory(t *testing.T) {
	r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
	out, err := r.signalRotation(context.Background(), okOps(&opLog{}), &TargetContainer{ID: "c", Secrets: []string{"db"}}, "svc", "db")
	if err != nil || out.State != SignalReloadRequested {
		t.Fatalf("%v %q", err, out.State)
	}
}
