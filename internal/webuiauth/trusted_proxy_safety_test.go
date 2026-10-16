package webuiauth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// throttledOutOf20 sends 20 failed logins from one socket peer, each claiming a
// DIFFERENT client via X-Forwarded-For, and returns how many were throttled.
//
//	~15 of 20 => the attempts were counted against ONE identity (5 allowed, 15 blocked)
//	0  of 20  => each attempt got its own fresh identity, i.e. the header chose the identity
func throttledOutOf20(t *testing.T, trusted []string, peer string) int {
	t.Helper()
	return throttledWithHops(t, trusted, peer, forgedV4)
}

func forgedV4(i int) string { return fmt.Sprintf("198.51.100.%d", i+1) }
func forgedV6(i int) string { return fmt.Sprintf("2001:db8::%x", i+1) }

func throttledWithHops(t *testing.T, trusted []string, peer string, hop func(int) string) int {
	t.Helper()
	m, _ := hardeningManager(t, time.Hour)
	if err := m.SetTrustedProxies(trusted); err != nil {
		t.Fatalf("SetTrustedProxies(%v): %v", trusted, err)
	}
	n := 0
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", hop(i))
		if _, err := m.LoginFromRequest(r, "op", "wrong"); err == ErrTooManyLoginAttempts {
			n++
		}
	}
	return n
}

const (
	attackerPeer = "203.0.113.9:4000" // outside every range below
	insidePeer   = "10.9.9.9:4000"    // a direct CLIENT that happens to sit inside 10.0.0.0/8
	insidePeer6  = "[fd00::bad]:4000" // same, IPv6
)

// ---- clients cannot pick their own identity ---------------------------------

func TestTrustedProxy_DirectClientOutsideEveryTrustedRangeCannotChooseIdentity(t *testing.T) {
	for _, trusted := range [][]string{nil, {"10.0.0.1"}, {"10.0.0.0/24"}, {"192.168.0.0/16", "fd00::/64"}} {
		if got := throttledOutOf20(t, trusted, attackerPeer); got != 15 {
			t.Errorf("trusted=%v: throttled %d/20, want 15 (X-Forwarded-For from an untrusted peer must be ignored)", trusted, got)
		}
	}
}

func TestTrustedProxy_ExactProxyAddress_DirectClientNextToItCannotSpoof(t *testing.T) {
	if got := throttledOutOf20(t, []string{"10.0.0.1"}, insidePeer); got != 15 {
		t.Errorf("a client at 10.9.9.9 spoofing XFF with only 10.0.0.1 trusted: throttled %d/20, want 15", got)
	}
}

// ---- match-everything ranges -----------------------------------------------------

// REPRODUCED before the fix: a match-all range in ONE address family was a
// bypass. net.IPNet.Contains never matches an IPv4 address against an IPv6
// network (or the reverse), so with 0.0.0.0/0 a client put IPv6 hops in
// X-Forwarded-For, escaped the "trusted" set, and chose its own throttle
// identity (0 of 20 attempts throttled). The same held for ::/0 with IPv4 hops.
// Only listing BOTH families happened to fall back to the peer. Match-all
// entries are now ignored, so every combination is keyed on the connection.
func TestTrustedProxy_MatchAllRanges_AreIgnored_ForEveryAddressFamilyCombination(t *testing.T) {
	v4peer, v6peer := attackerPeer, "[2001:db8::bad]:4000"
	for _, trusted := range [][]string{{"0.0.0.0/0"}, {"::/0"}, {"0.0.0.0/0", "::/0"}} {
		for _, tc := range []struct {
			peer string
			hops func(int) string
			desc string
		}{
			{v4peer, forgedV4, "v4 peer, forged v4 hops"},
			{v4peer, forgedV6, "v4 peer, forged v6 hops"},
			{v6peer, forgedV4, "v6 peer, forged v4 hops"},
			{v6peer, forgedV6, "v6 peer, forged v6 hops"},
		} {
			if got := throttledWithHops(t, trusted, tc.peer, tc.hops); got != 15 {
				t.Errorf("trusted=%v %s: throttled %d/20, want 15 (a /0 entry must never let a client choose its identity)", trusted, tc.desc, got)
			}
		}
	}
}

// Ignoring a /0 entry must not disturb a legitimate proxy listed next to it.
func TestTrustedProxy_MatchAllEntryAlongsideRealProxy_StillThrottlesPerRealClient(t *testing.T) {
	m, _ := hardeningManager(t, time.Hour)
	if err := m.SetTrustedProxies([]string{"0.0.0.0/0", "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	from := func(peer, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", xff)
		return r
	}
	for i := 0; i < maxLoginFailures; i++ {
		_, _ = m.LoginFromRequest(from("10.0.0.1:1", "198.51.100.66"), "op", "wrong")
	}
	if _, err := m.LoginFromRequest(from("10.0.0.1:1", "198.51.100.66"), "op", "wrong"); err != ErrTooManyLoginAttempts {
		t.Fatalf("attacker behind the real proxy not throttled: %v", err)
	}
	if _, err := m.LoginFromRequest(from("10.0.0.1:2", "198.51.100.7"), "op", "pw-for-hardening-tests"); err != nil {
		t.Fatalf("operator behind the same proxy locked out: %v", err)
	}
	// And a direct client (not the proxy) still cannot pick an identity.
	if got := throttledWithHops(t, []string{"0.0.0.0/0", "10.0.0.1"}, "10.9.9.9:4000", forgedV6); got != 15 {
		t.Errorf("direct client throttled %d/20, want 15", got)
	}
}

// ---- warnings ------------------------------------------------------------------

func TestTrustedProxyWarnings(t *testing.T) {
	cases := []struct {
		entry    string
		wantWarn string // substring; "" = no warning expected
	}{
		{"10.0.0.1", ""},
		{"10.0.0.0/32", ""},
		{"10.0.0.0/24", ""},
		{"2001:db8::1", ""},
		{"fd00::/120", ""},
		{"10.0.0.0/23", "10.0.0.0/23"},
		{"172.16.0.0/12", "172.16.0.0/12"},
		{"10.0.0.0/8", "10.0.0.0/8"},
		{"fd00::/64", "fd00::/64"},
		{"0.0.0.0/0", "IGNORED"},
		{"::/0", "IGNORED"},
	}
	for _, tc := range cases {
		m, _ := hardeningManager(t, time.Hour)
		if err := m.SetTrustedProxies([]string{tc.entry}); err != nil {
			t.Fatalf("%s: %v", tc.entry, err)
		}
		w := strings.Join(m.TrustedProxyWarnings(), "\n")
		switch {
		case tc.wantWarn == "" && w != "":
			t.Errorf("%s: unexpected warning: %s", tc.entry, w)
		case tc.wantWarn != "" && !strings.Contains(w, tc.wantWarn):
			t.Errorf("%s: want a warning containing %q, got %q", tc.entry, tc.wantWarn, w)
		}
	}
}

func TestTrustedProxyWarnings_AreReplacedNotAccumulated(t *testing.T) {
	m, _ := hardeningManager(t, time.Hour)
	_ = m.SetTrustedProxies([]string{"10.0.0.0/8"})
	if len(m.TrustedProxyWarnings()) != 1 {
		t.Fatalf("want 1 warning, got %v", m.TrustedProxyWarnings())
	}
	_ = m.SetTrustedProxies([]string{"10.0.0.1"})
	if w := m.TrustedProxyWarnings(); len(w) != 0 {
		t.Errorf("warnings from a previous configuration survived: %v", w)
	}
	_ = m.SetTrustedProxies(nil)
	if w := m.TrustedProxyWarnings(); len(w) != 0 {
		t.Errorf("warnings with no trusted proxies: %v", w)
	}
}

// ---- known limitation, pinned on purpose ------------------------------------------

// A trusted range that contains direct clients trusts those clients' headers:
// any host inside a listed range can choose its own throttle identity. This is
// inherent to "trust everything in this range", so the mitigation is
// configuration hygiene (exact proxy addresses) plus the startup warning above.
// The test pins today's behaviour so that a future hardening (e.g. a per-peer
// cap on distinct forwarded identities) is a deliberate, reviewed change.
func TestTrustedProxy_KnownLimitation_HostInsideABroadRangeCanChooseItsIdentity(t *testing.T) {
	if got := throttledOutOf20(t, []string{"10.0.0.0/8"}, insidePeer); got != 0 {
		t.Errorf("behaviour changed: throttled %d/20 for a client inside 10.0.0.0/8 (documented limitation was 0). If this is a deliberate hardening, update docs/security-hardening.md and this test", got)
	}
	if got := throttledOutOf20(t, []string{"fd00::/64"}, insidePeer6); got != 0 {
		t.Errorf("behaviour changed for IPv6 range: throttled %d/20", got)
	}
}
