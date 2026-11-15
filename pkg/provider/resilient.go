package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
)

// ErrCircuitOpen is returned (wrapped) while a provider's breaker is open.
var ErrCircuitOpen = errors.New("provider circuit open")

// ResilienceConfig bounds GetSecret retries. The zero value of any field
// selects the default shown; use DefaultResilience for the full set.
type ResilienceConfig struct {
	MaxAttempts    int           // total attempts incl. the first (default 3)
	AttemptTimeout time.Duration // per-attempt deadline (default 8s)
	MaxElapsed     time.Duration // overall budget across attempts+sleeps (default 20s)
	BaseBackoff    time.Duration // first backoff ceiling (default 500ms)
	MaxBackoff     time.Duration // backoff ceiling cap (default 5s)
	ThrottleFloor  time.Duration // minimum delay after a throttle w/o Retry-After (default 1s)

	// Circuit breaker, per provider instance. BreakerThreshold consecutive
	// operations that ended in a transient failure open it for
	// BreakerCooldown; then exactly one probe is allowed.
	BreakerThreshold int           // default 5; negative disables the breaker
	BreakerCooldown  time.Duration // default 30s

	// Test seams. nil selects the real implementation.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	Rand  func() float64 // uniform in [0,1)
}

// DefaultResilience returns the production defaults.
//
// Why these: the plugin SDKs are configured NOT to retry themselves (see
// cmd/plugins), so this wrapper is the single owner of retries and worst-case
// latency is exactly MaxElapsed (20s), under the 30s agent fetch timeout in
// internal/agent/server.go. 3 attempts with full-jitter backoff ceilings of
// 0.5s/1s rides out a blip without hammering a struggling provider.
func DefaultResilience() ResilienceConfig {
	return ResilienceConfig{
		MaxAttempts:      3,
		AttemptTimeout:   8 * time.Second,
		MaxElapsed:       20 * time.Second,
		BaseBackoff:      500 * time.Millisecond,
		MaxBackoff:       5 * time.Second,
		ThrottleFloor:    time.Second,
		BreakerThreshold: 5,
		BreakerCooldown:  30 * time.Second,
	}
}

func (c ResilienceConfig) withDefaults() ResilienceConfig {
	d := DefaultResilience()
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = d.MaxAttempts
	}
	if c.AttemptTimeout <= 0 {
		c.AttemptTimeout = d.AttemptTimeout
	}
	if c.MaxElapsed <= 0 {
		c.MaxElapsed = d.MaxElapsed
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = d.BaseBackoff
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = d.MaxBackoff
	}
	if c.ThrottleFloor <= 0 {
		c.ThrottleFloor = d.ThrottleFloor
	}
	if c.BreakerThreshold == 0 {
		c.BreakerThreshold = d.BreakerThreshold
	}
	if c.BreakerCooldown <= 0 {
		c.BreakerCooldown = d.BreakerCooldown
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Sleep == nil {
		c.Sleep = sleepCtx
	}
	if c.Rand == nil {
		c.Rand = rand.Float64
	}
	return c
}

// sleepCtx sleeps for d or until ctx is done, whichever is first. It never
// sleeps past cancellation and leaves no timer behind.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Resilient wraps a SecretProvider so GetSecret is bounded, retried only for
// transient failures, de-duplicated per key, and short-circuited during an
// outage. Only the read-only GetSecret is wrapped; Init and WatchSecret pass
// straight through.
//
// It spawns no goroutines: the first caller for a key (the "leader") performs
// the work on its own goroutine and concurrent callers for the same key wait
// on it, so total in-flight upstream work is bounded by the number of distinct
// keys, not callers.
type Resilient struct {
	inner api.SecretProvider
	name  string
	cfg   ResilienceConfig

	mu       sync.Mutex
	flights  map[string]*flight
	fails    int       // consecutive transient-failed operations
	openTill time.Time // breaker open until (zero = closed)
	probing  bool      // a half-open probe is in flight
}

type flight struct {
	done chan struct{}
	data map[string]string
	err  error
	// leaderCtxErr is set when the leader gave up because ITS context ended,
	// so followers (whose contexts are fine) retry as a new leader instead of
	// inheriting somebody else's cancellation.
	leaderCtxErr bool
}

// NewResilient wraps inner. name is the provider's configured name, used only
// in error text.
func NewResilient(inner api.SecretProvider, name string, cfg ResilienceConfig) *Resilient {
	return &Resilient{inner: inner, name: name, cfg: cfg.withDefaults(), flights: map[string]*flight{}}
}

var (
	_ api.SecretProvider            = (*Resilient)(nil)
	_ api.SecretProviderWithContext = (*Resilient)(nil)
)

func (r *Resilient) Init(config map[string]string) error { return r.inner.Init(config) }

func (r *Resilient) WatchSecret(ctx context.Context, name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	return r.inner.WatchSecret(ctx, name, interval)
}

// GetSecret is GetSecretWithContext under context.Background(); the overall
// MaxElapsed budget still bounds it.
func (r *Resilient) GetSecret(name string) (map[string]string, error) {
	return r.GetSecretWithContext(context.Background(), name)
}

func (r *Resilient) GetSecretWithContext(ctx context.Context, name string) (map[string]string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.mu.Lock()
		if f, ok := r.flights[name]; ok {
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.done:
			}
			if f.leaderCtxErr {
				continue // become (or follow) a fresh leader
			}
			return cloneData(f.data), f.err
		}
		f := &flight{done: make(chan struct{})}
		r.flights[name] = f
		r.mu.Unlock()

		data, err := r.run(ctx, name)
		f.data, f.err = data, err
		f.leaderCtxErr = ctx.Err() != nil
		r.mu.Lock()
		delete(r.flights, name)
		r.mu.Unlock()
		close(f.done)
		if f.leaderCtxErr {
			return nil, err
		}
		return cloneData(data), err
	}
}

func cloneData(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// run executes one bounded retry loop.
func (r *Resilient) run(ctx context.Context, name string) (map[string]string, error) {
	probe, berr := r.breakerAllow()
	if berr != nil {
		return nil, fmt.Errorf("provider %q: secret %q: %w", r.name, name, berr)
	}

	cfg := r.cfg
	start := cfg.Now()
	opCtx, cancel := context.WithTimeout(ctx, cfg.MaxElapsed)
	defer cancel()

	var lastErr error
	var lastClass Classification
	attempts := 0
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		attempts = attempt
		data, err := r.attempt(opCtx, name)
		if err == nil {
			r.breakerRecord(true, probe)
			return data, nil
		}
		lastErr = err

		// Caller cancelled: not a provider failure, don't count it, don't retry.
		if ctx.Err() != nil {
			r.breakerAbort(probe)
			return nil, ctx.Err()
		}

		lastClass = Classify(err, name)
		if !lastClass.Retryable() {
			// The provider answered; it is reachable. Not a breaker failure.
			r.breakerRecord(true, probe)
			return nil, err
		}
		if attempt == cfg.MaxAttempts {
			break
		}

		delay := r.backoff(attempt, lastClass)
		// Never sleep past the overall budget: give up with the real error
		// rather than waiting just to be cut off.
		if cfg.Now().Add(delay).Sub(start) >= cfg.MaxElapsed {
			break
		}
		if serr := cfg.Sleep(opCtx, delay); serr != nil {
			if ctx.Err() != nil {
				r.breakerAbort(probe)
				return nil, ctx.Err()
			}
			break // overall budget exhausted during backoff
		}
	}

	r.breakerRecord(false, probe)
	return nil, fmt.Errorf("provider %q: secret %q: giving up after %d attempt(s) (%s): %w",
		r.name, name, attempts, lastClass.Class, lastErr)
}

func (r *Resilient) attempt(opCtx context.Context, name string) (map[string]string, error) {
	actx, cancel := context.WithTimeout(opCtx, r.cfg.AttemptTimeout)
	defer cancel()
	var data map[string]string
	var err error
	if wc, ok := r.inner.(api.SecretProviderWithContext); ok {
		data, err = wc.GetSecretWithContext(actx, name)
	} else {
		// Legacy provider without context support: cannot be interrupted.
		data, err = r.inner.GetSecret(name)
	}
	if err != nil {
		return nil, err // discard any partial data returned alongside an error
	}
	return data, nil
}

// backoff returns the delay before attempt+1: full jitter over an exponential
// ceiling, floored for throttles and raised to Retry-After when present.
func (r *Resilient) backoff(attempt int, c Classification) time.Duration {
	ceil := r.cfg.BaseBackoff
	for i := 1; i < attempt && ceil < r.cfg.MaxBackoff; i++ {
		ceil *= 2
	}
	if ceil > r.cfg.MaxBackoff {
		ceil = r.cfg.MaxBackoff
	}
	d := time.Duration(r.cfg.Rand() * float64(ceil))
	if c.Class == ClassThrottled && d < r.cfg.ThrottleFloor {
		d = r.cfg.ThrottleFloor
	}
	if c.RetryAfter > d {
		d = c.RetryAfter
	}
	return d
}

// --- circuit breaker -------------------------------------------------------

func (r *Resilient) breakerEnabled() bool { return r.cfg.BreakerThreshold > 0 }

// breakerAllow admits or rejects an operation. probe is true for the single
// half-open probe, whose outcome alone decides close vs. re-open.
func (r *Resilient) breakerAllow() (probe bool, err error) {
	if !r.breakerEnabled() {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.openTill.IsZero() {
		return false, nil
	}
	now := r.cfg.Now()
	if now.Before(r.openTill) || r.probing {
		return false, fmt.Errorf("%w (cooling down, next probe in %s)", ErrCircuitOpen,
			maxDur(r.openTill.Sub(now), 0).Round(time.Second))
	}
	r.probing = true // half-open: this caller is the single probe
	return true, nil
}

// breakerRecord records an operation outcome: ok=false only for operations
// that ended in a transient failure.
func (r *Resilient) breakerRecord(ok, probe bool) {
	if !r.breakerEnabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if probe {
		r.probing = false
	}
	if ok {
		r.fails = 0
		r.openTill = time.Time{}
		return
	}
	r.fails++
	if probe || r.fails >= r.cfg.BreakerThreshold {
		r.openTill = r.cfg.Now().Add(r.cfg.BreakerCooldown)
	}
}

// breakerAbort releases a half-open probe slot without judging the provider
// (caller cancelled mid-probe).
func (r *Resilient) breakerAbort(probe bool) {
	if !probe || !r.breakerEnabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probing = false
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
