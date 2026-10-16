package webuiauth

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const goodPass = "correct-horse-battery-staple"

// countingCompare replaces bcrypt verification for the test and counts how many
// verifications START. The sleep widens the window in which the old
// check-then-record logic let every concurrent request through.
func countingCompare(t *testing.T, delay time.Duration) *int32 {
	t.Helper()
	var n int32
	orig := compareHash
	compareHash = func(h, p []byte) error {
		atomic.AddInt32(&n, 1)
		time.Sleep(delay)
		return orig(h, p)
	}
	t.Cleanup(func() { compareHash = orig })
	return &n
}

// Regression for review finding M0-1: 40 concurrent wrong guesses from one
// client previously let 29 reach bcrypt against a budget of 5.
func TestReservation_ConcurrentWrongAttemptsCannotExceedBudget(t *testing.T) {
	m, _ := testManager(t, 0)
	calls := countingCompare(t, 20*time.Millisecond)

	const attackers = 40
	var ready, wg sync.WaitGroup
	start := make(chan struct{})
	var throttled int32
	ready.Add(attackers)
	for i := 0; i < attackers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			if _, err := m.LoginFromRequest(loginRequest("203.0.113.200:1"), "operator", "wrong-guess"); err == ErrTooManyLoginAttempts {
				atomic.AddInt32(&throttled, 1)
			}
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(calls); got != maxLoginFailures {
		t.Fatalf("%d password verifications started; the budget is exactly %d", got, maxLoginFailures)
	}
	if got := atomic.LoadInt32(&throttled); got != attackers-maxLoginFailures {
		t.Fatalf("throttled %d, want %d", got, attackers-maxLoginFailures)
	}
}

func TestReservation_InflightAttemptsCountAgainstTheBudget(t *testing.T) {
	l := newLoginLimiter()
	var held []*reservation
	for i := 0; i < maxLoginFailures; i++ {
		r := l.reserve("k")
		if r == nil {
			t.Fatalf("reservation %d denied", i)
		}
		held = append(held, r)
	}
	if l.reserve("k") != nil {
		t.Fatal("reservation beyond budget granted while others are in flight")
	}
	held[0].finish(outcomeSuccess) // a correct login clears the key
	if r := l.reserve("k"); r == nil {
		t.Fatal("key still blocked after a successful login freed its failures")
	} else {
		r.finish(outcomeNeutral)
	}
}

func TestReservation_SuccessResetsFailuresAndRefundsGlobalToken(t *testing.T) {
	m, _ := testManager(t, 0)
	frozen := m.now()
	m.now = func() time.Time { return frozen }
	req := func(i int) string { return fmt.Sprintf("198.51.100.%d:1", i) }

	// Use all but one global token on failures from distinct clients...
	for i := 1; i < globalLoginBurst; i++ {
		if _, err := m.LoginFromRequest(loginRequest(req(i)), "operator", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("%d: %v", i, err)
		}
	}
	// ...then a successful login takes the last token and must give it back.
	if _, err := m.LoginFromRequest(loginRequest(req(100)), "operator", goodPass); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := m.LoginFromRequest(loginRequest(req(101)), "operator", goodPass); err != nil {
		t.Fatalf("the successful login did not refund its global token: %v", err)
	}
}

func TestReservation_NeutralOutcomeCountsNothing(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < 3*maxLoginFailures; i++ {
		r := l.reserve("k")
		if r == nil {
			t.Fatalf("neutral releases consumed budget (attempt %d)", i)
		}
		r.finish(outcomeNeutral)
		r.finish(outcomeFailure) // double finish must be a no-op, not a second charge
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.attempts) != 0 {
		t.Fatalf("idle key not cleaned up: %+v", l.attempts)
	}
}

func TestReservation_PanicDuringVerificationDoesNotLeakAReservation(t *testing.T) {
	m, _ := testManager(t, 0)
	orig := compareHash
	compareHash = func(h, p []byte) error { panic("verifier exploded") }
	for i := 0; i < 3*maxLoginFailures; i++ {
		func() {
			defer func() { _ = recover() }()
			_, _ = m.LoginFromRequest(loginRequest("203.0.113.210:1"), "operator", "x")
		}()
	}
	compareHash = orig
	// The key must be neither blocked nor charged by the panics.
	if _, err := m.LoginFromRequest(loginRequest("203.0.113.210:1"), "operator", goodPass); err != nil {
		t.Fatalf("panics left the key throttled: %v", err)
	}
}

func TestReservation_WindowExpiryAppliesToCountedFailures(t *testing.T) {
	m, _ := testManager(t, 0)
	frozen := m.now()
	m.now = func() time.Time { return frozen }
	for i := 0; i < maxLoginFailures; i++ {
		m.LoginFromRequest(loginRequest("203.0.113.220:1"), "operator", "wrong")
	}
	if _, err := m.LoginFromRequest(loginRequest("203.0.113.220:1"), "operator", goodPass); err != ErrTooManyLoginAttempts {
		t.Fatalf("expected throttling, got %v", err)
	}
	frozen = frozen.Add(loginFailureWindow + time.Second)
	if _, err := m.LoginFromRequest(loginRequest("203.0.113.220:1"), "operator", goodPass); err != nil {
		t.Fatalf("not recovered after the window: %v", err)
	}
}

// ---- A2: IPv6 aggregation ------------------------------------------------------

// Regression for review finding M0-2: 50 addresses in one /64 got 250 guesses.
func TestThrottle_AddressesInOneIPv6PrefixShareOneBudget(t *testing.T) {
	m, _ := testManager(t, 0)
	reached := 0
	for i := 0; i < 50; i++ {
		addr := fmt.Sprintf("[2001:db8:1:2:%x::%x]:4000", i+1, i+7)
		for k := 0; k < 5; k++ {
			if _, err := m.LoginFromRequest(loginRequest(addr), "operator", "bad"); err != ErrTooManyLoginAttempts {
				reached++
			}
		}
	}
	if reached > maxLoginFailures {
		t.Fatalf("%d guesses reached bcrypt from one /64; budget is %d", reached, maxLoginFailures)
	}
}

func TestThrottle_KeyDerivation(t *testing.T) {
	cases := map[string]string{
		"203.0.113.9":                      "203.0.113.9",       // IPv4: per address
		"203.0.113.10":                     "203.0.113.10",      // neighbour is a different key
		"::ffff:203.0.113.9":               "203.0.113.9",       // IPv4-mapped is IPv4
		"2001:db8:1:2:aaaa:bbbb:cccc:dddd": "2001:db8:1:2::/64", // IPv6: /64
		"2001:db8:1:2::1":                  "2001:db8:1:2::/64", // same /64
		"2001:db8:1:3::1":                  "2001:db8:1:3::/64", // different /64
		"not-an-ip":                        "not-an-ip",         // malformed: verbatim, no panic
		"":                                 "",
	}
	for in, want := range cases {
		if got := throttleKey(in); got != want {
			t.Errorf("throttleKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestThrottle_DifferentIPv6PrefixesAndIPv4NeighboursAreIndependent(t *testing.T) {
	m, _ := testManager(t, 0)
	for i := 0; i < maxLoginFailures; i++ {
		m.LoginFromRequest(loginRequest("[2001:db8:1:2::1]:1"), "operator", "bad")
	}
	if _, err := m.LoginFromRequest(loginRequest("[2001:db8:1:2::ffff]:1"), "operator", goodPass); err != ErrTooManyLoginAttempts {
		t.Fatalf("same /64 not throttled: %v", err)
	}
	if _, err := m.LoginFromRequest(loginRequest("[2001:db8:1:3::1]:1"), "operator", goodPass); err != nil {
		t.Fatalf("different /64 wrongly throttled: %v", err)
	}
	for i := 0; i < maxLoginFailures; i++ {
		m.LoginFromRequest(loginRequest("192.0.2.1:1"), "operator", "bad")
	}
	if _, err := m.LoginFromRequest(loginRequest("192.0.2.2:1"), "operator", goodPass); err != nil {
		t.Fatalf("IPv4 neighbour wrongly throttled: %v", err)
	}
}

func TestThrottle_MalformedRemoteAddrDoesNotPanicOrBypass(t *testing.T) {
	m, _ := testManager(t, 0)
	for i := 0; i < maxLoginFailures; i++ {
		m.LoginFromRequest(loginRequest("@"), "operator", "bad")
	}
	if _, err := m.LoginFromRequest(loginRequest("@"), "operator", goodPass); err != ErrTooManyLoginAttempts {
		t.Fatalf("malformed peer address escaped throttling: %v", err)
	}
}

func TestThrottle_TrustedProxyClientsAreAggregatedByPrefixToo(t *testing.T) {
	m, _ := testManager(t, 0)
	if err := m.SetTrustedProxies([]string{"10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLoginFailures; i++ {
		r := loginRequest("10.0.0.1:5555")
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("2001:db8:9:9:%x::1", i+1))
		m.LoginFromRequest(r, "operator", "bad")
	}
	r := loginRequest("10.0.0.1:5555")
	r.Header.Set("X-Forwarded-For", "2001:db8:9:9:ffff::1")
	if _, err := m.LoginFromRequest(r, "operator", goodPass); err != ErrTooManyLoginAttempts {
		t.Fatalf("rotating addresses behind a trusted proxy evaded the /64 budget: %v", err)
	}
	// Untrusted peers cannot choose their key via XFF.
	r2 := loginRequest("203.0.113.5:1")
	r2.Header.Set("X-Forwarded-For", "2001:db8:9:9:ffff::1")
	if got := m.clientKey(r2); got != "203.0.113.5" {
		t.Fatalf("untrusted peer chose its key: %q", got)
	}
}

// ---- global backstop -----------------------------------------------------------

func TestGlobalBackstop_RotatingPrefixesCannotGuessWithoutLimit(t *testing.T) {
	m, _ := testManager(t, 0)
	frozen := m.now()
	m.now = func() time.Time { return frozen }
	reached := 0
	for i := 0; i < 500; i++ {
		addr := fmt.Sprintf("[2001:db8:%x:%x::1]:1", i, i) // 500 different /64s, one guess each
		if _, err := m.LoginFromRequest(loginRequest(addr), "operator", "bad"); err != ErrTooManyLoginAttempts {
			reached++
		}
	}
	if reached != globalLoginBurst {
		t.Fatalf("%d guesses started at one instant across 500 prefixes; global burst is %d", reached, globalLoginBurst)
	}
	// Tokens return over time (no permanent lockout).
	frozen = frozen.Add(globalLoginInterval + 100*time.Millisecond)
	if _, err := m.LoginFromRequest(loginRequest("[2001:db8:ffff::1]:1"), "operator", goodPass); err != nil {
		t.Fatalf("global bucket did not refill: %v", err)
	}
}

func TestGlobalBackstop_DeniedRequestsDoNotConsumePerKeyBudget(t *testing.T) {
	l := newLoginLimiter()
	frozen := time.Unix(1_800_000_000, 0)
	l.now = func() time.Time { return frozen }
	for i := 0; i < globalLoginBurst; i++ {
		if r := l.reserve(fmt.Sprintf("k%d", i)); r == nil {
			t.Fatal("burst denied early")
		}
	}
	if l.reserve("victim") != nil {
		t.Fatal("global bucket not enforced")
	}
	l.mu.Lock()
	_, tracked := l.attempts["victim"]
	l.mu.Unlock()
	if tracked {
		t.Fatal("a globally denied request left per-key state behind")
	}
}
