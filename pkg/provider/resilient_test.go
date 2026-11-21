package provider

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
)

const canary = "CANARY-SECRET-VALUE-9f3a"

type scripted struct {
	mu    sync.Mutex
	calls int
	fn    func(ctx context.Context, n int, name string) (map[string]string, error)
}

func (s *scripted) Init(map[string]string) error { return nil }
func (s *scripted) GetSecret(name string) (map[string]string, error) {
	return s.GetSecretWithContext(context.Background(), name)
}
func (s *scripted) GetSecretWithContext(ctx context.Context, name string) (map[string]string, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	return s.fn(ctx, n, name)
}
func (s *scripted) WatchSecret(context.Context, string, time.Duration) (<-chan api.SecretUpdate, error) {
	return nil, nil
}
func (s *scripted) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }

// fakeTime is a deterministic clock whose Sleep advances time instead of
// blocking, and records every requested delay.
type fakeTime struct {
	mu     sync.Mutex
	now    time.Time
	delays []time.Duration
}

func newFakeTime() *fakeTime { return &fakeTime{now: time.Unix(1_000_000, 0)} }
func (f *fakeTime) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}
func (f *fakeTime) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.delays = append(f.delays, d)
	f.now = f.now.Add(d)
	f.mu.Unlock()
	return nil
}
func (f *fakeTime) advance(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() }

func cfgFor(ft *fakeTime, rnd float64) ResilienceConfig {
	c := DefaultResilience()
	c.Now, c.Sleep = ft.Now, ft.Sleep
	c.Rand = func() float64 { return rnd }
	return c
}

var (
	errTransient = errors.New("StatusCode: 503, ServiceUnavailable")
	errPermanent = errors.New("StatusCode: 403, AccessDeniedException")
	errThrottle  = errors.New("StatusCode: 400, ThrottlingException: Rate exceeded")
)

func failN(n int, err error) func(context.Context, int, string) (map[string]string, error) {
	return func(_ context.Context, i int, _ string) (map[string]string, error) {
		if i <= n {
			return nil, err
		}
		return map[string]string{"k": "v"}, nil
	}
}

func TestRetryTransientThenSuccess(t *testing.T) {
	ft := newFakeTime()
	p := &scripted{fn: failN(2, errTransient)}
	r := NewResilient(p, "aws", cfgFor(ft, 1.0)) // rand=1 => max of jitter range
	got, err := r.GetSecret("s")
	if err != nil || got["k"] != "v" {
		t.Fatalf("got %v %v", got, err)
	}
	if p.count() != 3 {
		t.Fatalf("calls=%d", p.count())
	}
	// attempt1 ceiling = 500ms, attempt2 ceiling = 1s; rand~1 => just below.
	if len(ft.delays) != 2 || ft.delays[0] > 500*time.Millisecond || ft.delays[1] > time.Second || ft.delays[1] <= ft.delays[0] {
		t.Fatalf("delays=%v", ft.delays)
	}
}

func TestPermanentNeverRetried(t *testing.T) {
	for _, e := range []error{
		errPermanent,
		errors.New("StatusCode: 400, ResourceNotFoundException"),
		errors.New("aws provider not initialized"),
	} {
		ft := newFakeTime()
		p := &scripted{fn: failN(99, e)}
		r := NewResilient(p, "aws", cfgFor(ft, 0.5))
		if _, err := r.GetSecret("s"); err == nil {
			t.Fatal("want error")
		}
		if p.count() != 1 || len(ft.delays) != 0 {
			t.Fatalf("%v: calls=%d delays=%v", e, p.count(), ft.delays)
		}
	}
}

func TestMaxAttemptsBound(t *testing.T) {
	ft := newFakeTime()
	p := &scripted{fn: failN(99, errTransient)}
	c := cfgFor(ft, 0.5)
	c.MaxAttempts = 4
	r := NewResilient(p, "aws", c)
	_, err := r.GetSecret("s")
	if err == nil || p.count() != 4 || len(ft.delays) != 3 {
		t.Fatalf("err=%v calls=%d delays=%v", err, p.count(), ft.delays)
	}
	if !strings.Contains(err.Error(), "4 attempt") || !strings.Contains(err.Error(), "ServiceUnavailable") {
		t.Fatalf("err=%v", err)
	}
}

func TestBackoffCappedAndFullJitterBounds(t *testing.T) {
	ft := newFakeTime()
	c := cfgFor(ft, 0)
	c.BaseBackoff, c.MaxBackoff, c.MaxAttempts, c.MaxElapsed = 100*time.Millisecond, 250*time.Millisecond, 6, time.Hour
	r := NewResilient(&scripted{fn: failN(99, errTransient)}, "x", c)
	for _, rnd := range []float64{0, 0.5, 0.999999} {
		c.Rand = func() float64 { return rnd }
		rr := NewResilient(&scripted{fn: failN(99, errTransient)}, "x", c)
		for a := 1; a <= 6; a++ {
			d := rr.backoff(a, Classification{Class: ClassTransient})
			ceil := 100 * time.Millisecond << (a - 1)
			if ceil > 250*time.Millisecond {
				ceil = 250 * time.Millisecond
			}
			if d < 0 || d > ceil {
				t.Fatalf("rnd=%v attempt=%d d=%v ceil=%v", rnd, a, d, ceil)
			}
		}
	}
	_ = r
}

func TestThrottleFloorAndRetryAfter(t *testing.T) {
	ft := newFakeTime()
	p := &scripted{fn: failN(1, errThrottle)}
	r := NewResilient(p, "aws", cfgFor(ft, 0)) // jitter 0 => floor decides
	if _, err := r.GetSecret("s"); err != nil {
		t.Fatal(err)
	}
	if len(ft.delays) != 1 || ft.delays[0] != time.Second {
		t.Fatalf("floor not applied: %v", ft.delays)
	}

	ft = newFakeTime()
	ra := errors.New("StatusCode: 429, Retry-After: 3, slow down")
	p = &scripted{fn: failN(1, ra)}
	r = NewResilient(p, "aws", cfgFor(ft, 0))
	if _, err := r.GetSecret("s"); err != nil {
		t.Fatal(err)
	}
	if len(ft.delays) != 1 || ft.delays[0] != 3*time.Second {
		t.Fatalf("Retry-After not honoured: %v", ft.delays)
	}
}

func TestRetryAfterBeyondBudgetStopsInsteadOfSleeping(t *testing.T) {
	ft := newFakeTime()
	ra := errors.New("StatusCode: 429, Retry-After: 600, slow down")
	p := &scripted{fn: failN(99, ra)}
	r := NewResilient(p, "aws", cfgFor(ft, 0))
	_, err := r.GetSecret("s")
	if err == nil || p.count() != 1 || len(ft.delays) != 0 {
		t.Fatalf("err=%v calls=%d delays=%v", err, p.count(), ft.delays)
	}
}

func TestElapsedBound(t *testing.T) {
	ft := newFakeTime()
	// each attempt "takes" 9s of fake time before failing
	p := &scripted{fn: func(_ context.Context, _ int, _ string) (map[string]string, error) {
		ft.advance(9 * time.Second)
		return nil, errTransient
	}}
	c := cfgFor(ft, 1)
	c.MaxAttempts, c.MaxElapsed = 10, 20*time.Second
	r := NewResilient(p, "aws", c)
	_, err := r.GetSecret("s")
	if err == nil {
		t.Fatal("want error")
	}
	var total time.Duration
	for _, d := range ft.delays {
		total += d
	}
	if elapsed := ft.Now().Sub(time.Unix(1_000_000, 0)); elapsed > 20*time.Second+9*time.Second {
		t.Fatalf("elapsed %v exceeds budget+one attempt", elapsed)
	}
	if p.count() >= 10 {
		t.Fatalf("attempts not curtailed by elapsed bound: %d", p.count())
	}
}

func TestPerAttemptTimeoutThenRetry(t *testing.T) {
	// Real clock for the per-attempt timeout (context-based).
	p := &scripted{fn: func(ctx context.Context, n int, _ string) (map[string]string, error) {
		if n == 1 {
			<-ctx.Done() // hang until the per-attempt deadline fires
			return nil, ctx.Err()
		}
		return map[string]string{"k": "v"}, nil
	}}
	c := DefaultResilience()
	c.AttemptTimeout = 30 * time.Millisecond
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	r := NewResilient(p, "x", c)
	got, err := r.GetSecret("s")
	if err != nil || got["k"] != "v" || p.count() != 2 {
		t.Fatalf("got=%v err=%v calls=%d", got, err, p.count())
	}
}

func TestCancellationMidBackoffReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inSleep := make(chan struct{})
	c := DefaultResilience()
	c.Sleep = func(c context.Context, d time.Duration) error { // real blocking sleep, observably entered
		close(inSleep)
		return sleepCtx(c, time.Hour)
	}
	p := &scripted{fn: failN(99, errTransient)}
	r := NewResilient(p, "x", c)
	done := make(chan error, 1)
	go func() { _, err := r.GetSecretWithContext(ctx, "s"); done <- err }()
	<-inSleep
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not return after cancellation")
	}
	if p.count() != 1 {
		t.Fatalf("retried after cancel: %d", p.count())
	}
	// cancellation is not a provider failure
	if r.fails != 0 {
		t.Fatalf("cancel counted by breaker: %d", r.fails)
	}
}

func TestPreCancelledContextDoesNoWork(t *testing.T) {
	p := &scripted{fn: failN(0, nil)}
	r := NewResilient(p, "x", DefaultResilience())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.GetSecretWithContext(ctx, "s"); !errors.Is(err, context.Canceled) || p.count() != 0 {
		t.Fatalf("err=%v calls=%d", err, p.count())
	}
}

func TestSecretValuesNeverInErrors(t *testing.T) {
	// Provider returns data alongside an error (a buggy/hostile plugin): the
	// wrapper must discard the data and never echo it.
	ft := newFakeTime()
	p := &scripted{fn: func(_ context.Context, _ int, _ string) (map[string]string, error) {
		return map[string]string{"password": canary}, errTransient
	}}
	r := NewResilient(p, "aws", cfgFor(ft, 0.5))
	got, err := r.GetSecret("db-pass")
	if got != nil || err == nil {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("canary leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "db-pass") || !strings.Contains(err.Error(), `"aws"`) {
		t.Fatalf("expected provider and key NAME in error: %v", err)
	}
	// breaker-open path too
	c := cfgFor(ft, 0.5)
	c.BreakerThreshold = 1
	r = NewResilient(p, "aws", c)
	_, _ = r.GetSecret("db-pass")
	_, err = r.GetSecret("db-pass")
	if !errors.Is(err, ErrCircuitOpen) || strings.Contains(err.Error(), canary) {
		t.Fatalf("err=%v", err)
	}
}

func TestSingleflightCoalescesConcurrentCallers(t *testing.T) {
	release := make(chan struct{})
	var inflight, maxInflight int32
	p := &scripted{fn: func(_ context.Context, _ int, _ string) (map[string]string, error) {
		n := atomic.AddInt32(&inflight, 1)
		for {
			m := atomic.LoadInt32(&maxInflight)
			if n <= m || atomic.CompareAndSwapInt32(&maxInflight, m, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&inflight, -1)
		return map[string]string{"k": "v"}, nil
	}}
	r := NewResilient(p, "x", DefaultResilience())
	const callers = 50
	var wg sync.WaitGroup
	results := make([]map[string]string, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = r.GetSecret("same")
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if p.count() != 1 || maxInflight != 1 {
		t.Fatalf("upstream calls=%d maxInflight=%d", p.count(), maxInflight)
	}
	// callers get independent copies
	results[0]["k"] = "mutated"
	if results[1]["k"] != "v" {
		t.Fatal("result map shared between callers")
	}
}

func TestFollowerNotPoisonedByLeaderCancellation(t *testing.T) {
	leaderIn := make(chan struct{})
	var once sync.Once
	p := &scripted{fn: func(ctx context.Context, n int, _ string) (map[string]string, error) {
		if n == 1 {
			once.Do(func() { close(leaderIn) })
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return map[string]string{"k": "v"}, nil
	}}
	r := NewResilient(p, "x", DefaultResilience())
	lctx, lcancel := context.WithCancel(context.Background())
	ldone := make(chan error, 1)
	go func() { _, err := r.GetSecretWithContext(lctx, "s"); ldone <- err }()
	<-leaderIn
	fdone := make(chan map[string]string, 1)
	go func() { d, _ := r.GetSecret("s"); fdone <- d }()
	time.Sleep(30 * time.Millisecond)
	lcancel()
	if err := <-ldone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader err=%v", err)
	}
	select {
	case d := <-fdone:
		if d["k"] != "v" {
			t.Fatalf("follower got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follower stuck")
	}
}

func TestBreakerOpenHalfOpenClose(t *testing.T) {
	ft := newFakeTime()
	var healthy atomic.Bool
	p := &scripted{fn: func(context.Context, int, string) (map[string]string, error) {
		if healthy.Load() {
			return map[string]string{"k": "v"}, nil
		}
		return nil, errTransient
	}}
	c := cfgFor(ft, 0)
	c.MaxAttempts, c.BreakerThreshold, c.BreakerCooldown = 1, 3, 30*time.Second
	r := NewResilient(p, "x", c)

	for i := 0; i < 3; i++ {
		if _, err := r.GetSecret("a"); err == nil || errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("i=%d err=%v", i, err)
		}
	}
	before := p.count()
	if _, err := r.GetSecret("a"); !errors.Is(err, ErrCircuitOpen) || p.count() != before {
		t.Fatalf("breaker should be open and short-circuit: err=%v calls=%d", err, p.count()-before)
	}
	// still open just before cool-down ends
	ft.advance(29 * time.Second)
	if _, err := r.GetSecret("a"); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err=%v", err)
	}
	// cool-down over: one probe goes through, fails, re-opens
	ft.advance(2 * time.Second)
	if _, err := r.GetSecret("a"); err == nil || errors.Is(err, ErrCircuitOpen) || p.count() != before+1 {
		t.Fatalf("probe should reach provider: err=%v", err)
	}
	if _, err := r.GetSecret("a"); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("failed probe must re-open: %v", err)
	}
	// next window: probe succeeds, breaker closes
	ft.advance(31 * time.Second)
	healthy.Store(true)
	if _, err := r.GetSecret("a"); err != nil {
		t.Fatalf("probe: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := r.GetSecret("a"); err != nil {
			t.Fatalf("closed breaker rejected call: %v", err)
		}
	}
}

func TestBreakerPermanentErrorsDoNotOpen(t *testing.T) {
	ft := newFakeTime()
	p := &scripted{fn: failN(99, errPermanent)}
	c := cfgFor(ft, 0)
	c.BreakerThreshold = 2
	r := NewResilient(p, "x", c)
	for i := 0; i < 10; i++ {
		if _, err := r.GetSecret("a"); errors.Is(err, ErrCircuitOpen) {
			t.Fatal("permanent errors must not trip the breaker")
		}
	}
	if p.count() != 10 {
		t.Fatalf("calls=%d", p.count())
	}
}

func TestBreakerHalfOpenAllowsSingleProbeUnderRace(t *testing.T) {
	ft := newFakeTime()
	release := make(chan struct{})
	var probes int32
	var open atomic.Bool
	p := &scripted{fn: func(context.Context, int, string) (map[string]string, error) {
		if !open.Load() {
			return nil, errTransient
		}
		atomic.AddInt32(&probes, 1)
		<-release
		return map[string]string{"k": "v"}, nil
	}}
	c := cfgFor(ft, 0)
	c.MaxAttempts, c.BreakerThreshold, c.BreakerCooldown = 1, 1, time.Second
	r := NewResilient(p, "x", c)
	_, _ = r.GetSecret("trip")
	ft.advance(2 * time.Second)
	open.Store(true)

	var wg sync.WaitGroup
	var rejected int32
	for i := 0; i < 20; i++ { // distinct keys: singleflight cannot mask the breaker
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := r.GetSecret(string(rune('a' + i))); errors.Is(err, ErrCircuitOpen) {
				atomic.AddInt32(&rejected, 1)
			}
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if probes != 1 || rejected != 19 {
		t.Fatalf("probes=%d rejected=%d", probes, rejected)
	}
}

func TestBreakerDisabled(t *testing.T) {
	ft := newFakeTime()
	p := &scripted{fn: failN(99, errTransient)}
	c := cfgFor(ft, 0)
	c.MaxAttempts, c.BreakerThreshold = 1, -1
	r := NewResilient(p, "x", c)
	for i := 0; i < 20; i++ {
		if _, err := r.GetSecret("a"); errors.Is(err, ErrCircuitOpen) {
			t.Fatal("breaker should be disabled")
		}
	}
}

func TestNoGoroutineLeakAfterCancel(t *testing.T) {
	before := goroutines()
	p := &scripted{fn: func(ctx context.Context, _ int, _ string) (map[string]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	r := NewResilient(p, "x", DefaultResilience())
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		_, _ = r.GetSecretWithContext(ctx, "s")
		cancel()
	}
	time.Sleep(50 * time.Millisecond)
	if after := goroutines(); after > before+2 {
		t.Fatalf("goroutines %d -> %d", before, after)
	}
}

// Error shape crossing a real net/rpc boundary must still classify: the
// plugin-side error is flattened to a string.
func TestClassificationSurvivesRealRPCBoundary(t *testing.T) {
	impl := &scripted{fn: func(_ context.Context, n int, _ string) (map[string]string, error) {
		if n == 1 {
			return nil, errThrottle
		}
		if n == 2 {
			return nil, errTransient
		}
		return map[string]string{"k": "v"}, nil
	}}
	rpcProv := newTestRPC(t, impl)
	ft := newFakeTime()
	r := NewResilient(rpcProv, "aws", cfgFor(ft, 0))
	got, err := r.GetSecret("s")
	if err != nil || got["k"] != "v" || impl.count() != 3 {
		t.Fatalf("got=%v err=%v calls=%d", got, err, impl.count())
	}

	impl2 := &scripted{fn: failN(99, errPermanent)}
	r2 := NewResilient(newTestRPC(t, impl2), "aws", cfgFor(ft, 0))
	if _, err := r2.GetSecret("s"); err == nil || impl2.count() != 1 {
		t.Fatalf("permanent over RPC retried: calls=%d err=%v", impl2.count(), err)
	}
}

func goroutines() int { return runtimeNumGoroutine() }
