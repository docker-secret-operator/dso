package webuiauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func hardeningManager(t *testing.T, idle time.Duration) (*Manager, *time.Time) {
	t.Helper()
	hash, err := HashPassword("pw-for-hardening-tests")
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager("op", hash, idle)
	clock := time.Unix(2_000_000, 0)
	m.now = func() time.Time { return clock }
	return m, &clock
}

func TestAbsoluteLifetime_ExpiresDespiteActivity(t *testing.T) {
	m, clock := hardeningManager(t, 30*time.Minute)
	m.SetMaxLifetime(2 * time.Hour)
	sess, err := m.Login("op", "pw-for-hardening-tests")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ { // active every 20 minutes for 100 minutes
		*clock = clock.Add(20 * time.Minute)
		if _, err := m.Validate(sess.Token); err != nil {
			t.Fatalf("active session rejected at +%dm: %v", (i+1)*20, err)
		}
	}
	*clock = clock.Add(25 * time.Minute) // 125m total, only 25m idle
	if _, err := m.Validate(sess.Token); err != ErrSessionExpired {
		t.Fatalf("session past absolute lifetime = %v, want ErrSessionExpired", err)
	}
	if _, err := m.Validate(sess.Token); err != ErrNoSession {
		t.Fatalf("expired session must be deleted, got %v", err)
	}
}

func TestAbsoluteLifetime_DefaultAndDisable(t *testing.T) {
	m, clock := hardeningManager(t, 48*time.Hour)
	if m.maxLifetime != DefaultMaxLifetime {
		t.Fatalf("default max lifetime = %v", m.maxLifetime)
	}
	m.SetMaxLifetime(0) // disabled
	sess, _ := m.Login("op", "pw-for-hardening-tests")
	*clock = clock.Add(30 * time.Hour)
	if _, err := m.Validate(sess.Token); err != nil {
		t.Fatalf("with the cap disabled a 30h session should be fine: %v", err)
	}
	m.SetMaxLifetime(-time.Hour)
	if m.maxLifetime != 0 {
		t.Error("negative lifetime must clamp to disabled")
	}
}

func TestSessionCookie_LifetimeMatchesServerTimers(t *testing.T) {
	m, clock := hardeningManager(t, 30*time.Minute)
	m.SetMaxLifetime(time.Hour)
	sess, _ := m.Login("op", "pw-for-hardening-tests")

	if got := m.SessionCookie(sess).MaxAge; got != 30*60 {
		t.Errorf("fresh cookie Max-Age = %d, want idle timeout 1800", got)
	}
	*clock = clock.Add(45 * time.Minute) // 15m of absolute lifetime left
	if got := m.SessionCookie(sess).MaxAge; got != 15*60 {
		t.Errorf("cookie Max-Age near absolute cap = %d, want 900", got)
	}
	*clock = clock.Add(30 * time.Minute) // beyond the cap
	if got := m.SessionCookie(sess).MaxAge; got != -1 {
		t.Errorf("exhausted cookie Max-Age = %d, want -1 (delete)", got)
	}
	c := m.SessionCookie(sess)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || !c.Secure || c.Path != "/" {
		t.Errorf("cookie attributes regressed: %+v", c)
	}
}

func TestLogoutAndLogoutOthers_Invalidate(t *testing.T) {
	m, _ := hardeningManager(t, time.Hour)
	a, _ := m.Login("op", "pw-for-hardening-tests")
	b, _ := m.Login("op", "pw-for-hardening-tests")
	c, _ := m.Login("op", "pw-for-hardening-tests")
	if n := m.LogoutOthers(a.Token); n != 2 {
		t.Fatalf("LogoutOthers removed %d, want 2", n)
	}
	for _, s := range []*Session{b, c} {
		if _, err := m.Validate(s.Token); err != ErrNoSession {
			t.Errorf("revoked session still valid: %v", err)
		}
	}
	m.Logout(a.Token)
	if _, err := m.Validate(a.Token); err != ErrNoSession {
		t.Errorf("logged-out session still valid: %v", err)
	}
	m.Logout("never-existed") // must not panic
}

// ---- trusted proxies -------------------------------------------------------

func reqFrom(peer string, xff ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = peer
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientKey_ForwardedHeadersIgnoredUnlessProxyTrusted(t *testing.T) {
	m, _ := hardeningManager(t, time.Hour)
	// Default: nothing trusted, a spoofed header changes nothing.
	if got := m.clientKey(reqFrom("203.0.113.9:5555", "198.51.100.1")); got != "203.0.113.9" {
		t.Errorf("untrusted peer with spoofed XFF -> %q, want socket peer", got)
	}
	if err := m.SetTrustedProxies([]string{"10.0.0.0/8", "192.168.1.1", "fd00::/8"}); err != nil {
		t.Fatal(err)
	}
	// Peer not in the trusted set: still ignored even though a list exists.
	if got := m.clientKey(reqFrom("203.0.113.9:5555", "198.51.100.1")); got != "203.0.113.9" {
		t.Errorf("non-proxy peer -> %q, want socket peer", got)
	}
	// Trusted proxy: client address comes from XFF.
	if got := m.clientKey(reqFrom("10.1.2.3:4444", "198.51.100.1")); got != "198.51.100.1" {
		t.Errorf("trusted proxy -> %q, want 198.51.100.1", got)
	}
	// Client-supplied leftmost junk cannot override what the proxy appended.
	if got := m.clientKey(reqFrom("10.1.2.3:4444", "1.2.3.4, 198.51.100.1")); got != "198.51.100.1" {
		t.Errorf("spoofed leftmost entry -> %q, want rightmost untrusted", got)
	}
	// Chain of two trusted hops.
	if got := m.clientKey(reqFrom("10.1.2.3:4444", "198.51.100.1", "192.168.1.1")); got != "198.51.100.1" {
		t.Errorf("multi-hop -> %q", got)
	}
	// Malformed / missing header falls back to the peer, never to attacker text.
	if got := m.clientKey(reqFrom("10.1.2.3:4444", "not-an-ip")); got != "10.1.2.3" {
		t.Errorf("malformed XFF -> %q, want peer", got)
	}
	if got := m.clientKey(reqFrom("10.1.2.3:4444")); got != "10.1.2.3" {
		t.Errorf("absent XFF -> %q, want peer", got)
	}
	if got := m.clientKey(reqFrom("[fd00::5]:80", "2001:db8::7")); got != "2001:db8::/64" {
		t.Errorf("ipv6 proxy -> %q", got)
	}
}

func TestSetTrustedProxies_RejectsGarbage(t *testing.T) {
	m, _ := hardeningManager(t, time.Hour)
	for _, bad := range []string{"not-an-ip", "10.0.0.0/99", "10.0.0/8", "*"} {
		if err := m.SetTrustedProxies([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := m.SetTrustedProxies(nil); err != nil {
		t.Errorf("clearing: %v", err)
	}
}

func TestLoginThrottle_BehindProxy_IsPerRealClient(t *testing.T) {
	m, _ := hardeningManager(t, time.Hour)
	if err := m.SetTrustedProxies([]string{"10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	attacker := func() *http.Request { return reqFrom("10.0.0.1:1000", "198.51.100.66") }
	operator := func() *http.Request { return reqFrom("10.0.0.1:1001", "198.51.100.7") }

	for i := 0; i < maxLoginFailures; i++ {
		if _, err := m.LoginFromRequest(attacker(), "op", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := m.LoginFromRequest(attacker(), "op", "wrong"); err != ErrTooManyLoginAttempts {
		t.Fatalf("attacker not throttled: %v", err)
	}
	// The real operator, same proxy IP, different forwarded client, is unaffected.
	if _, err := m.LoginFromRequest(operator(), "op", "pw-for-hardening-tests"); err != nil {
		t.Fatalf("operator locked out by attacker behind the same proxy: %v", err)
	}
}

func TestLoginThrottle_WithoutTrustedProxy_SpoofedHeaderCannotEvadeLimit(t *testing.T) {
	m, _ := hardeningManager(t, time.Hour)
	for i := 0; i < maxLoginFailures; i++ {
		_, _ = m.LoginFromRequest(reqFrom("203.0.113.9:1", "198.51.100."+string(rune('1'+i))), "op", "wrong")
	}
	if _, err := m.LoginFromRequest(reqFrom("203.0.113.9:1", "198.51.100.99"), "op", "wrong"); err != ErrTooManyLoginAttempts {
		t.Fatalf("rotating X-Forwarded-For evaded the throttle: %v", err)
	}
}
