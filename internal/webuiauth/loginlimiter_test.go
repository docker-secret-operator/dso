package webuiauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func loginRequest(remoteAddr string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = remoteAddr
	return r
}

func TestLoginFromRequest_Success(t *testing.T) {
	m, _ := testManager(t, 0)
	req := loginRequest("203.0.113.10:1234")

	sess, err := m.LoginFromRequest(req, "operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if sess.Token == "" {
		t.Fatal("expected a session token")
	}
}

func TestLoginFromRequest_Failure(t *testing.T) {
	m, _ := testManager(t, 0)
	req := loginRequest("203.0.113.11:1234")

	if _, err := m.LoginFromRequest(req, "operator", "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginFromRequest_UnknownUsernameAndWrongPasswordBehaveIdentically(t *testing.T) {
	m, _ := testManager(t, 0)

	_, errUnknownUser := m.LoginFromRequest(loginRequest("203.0.113.12:1"), "nobody", "correct-horse-battery-staple")
	_, errWrongPass := m.LoginFromRequest(loginRequest("203.0.113.13:1"), "operator", "wrong-password")

	if errUnknownUser != ErrInvalidCredentials || errWrongPass != ErrInvalidCredentials {
		t.Fatalf("expected identical ErrInvalidCredentials for both cases, got %v / %v", errUnknownUser, errWrongPass)
	}
}

func TestLoginFromRequest_RepeatedFailuresTriggerThrottling(t *testing.T) {
	m, _ := testManager(t, 0)
	req := loginRequest("203.0.113.20:1234")

	for i := 0; i < maxLoginFailures; i++ {
		if _, err := m.LoginFromRequest(req, "operator", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: expected ErrInvalidCredentials, got %v", i, err)
		}
	}

	// The next attempt, even with the CORRECT password, must be throttled --
	// proving bcrypt is skipped rather than merely mislabeling a failure.
	if _, err := m.LoginFromRequest(req, "operator", "correct-horse-battery-staple"); err != ErrTooManyLoginAttempts {
		t.Fatalf("expected ErrTooManyLoginAttempts, got %v", err)
	}
}

func TestLoginFromRequest_ThrottledDoesNotRevealUsernameValidity(t *testing.T) {
	m, _ := testManager(t, 0)
	reqUnknown := loginRequest("203.0.113.30:1234")
	reqKnown := loginRequest("203.0.113.31:1234")

	for i := 0; i < maxLoginFailures; i++ {
		if _, err := m.LoginFromRequest(reqUnknown, "nobody", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("unknown-user attempt %d: unexpected error %v", i, err)
		}
		if _, err := m.LoginFromRequest(reqKnown, "operator", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("known-user attempt %d: unexpected error %v", i, err)
		}
	}

	_, errUnknown := m.LoginFromRequest(reqUnknown, "nobody", "wrong")
	_, errKnown := m.LoginFromRequest(reqKnown, "operator", "wrong")

	if errUnknown != ErrTooManyLoginAttempts || errKnown != ErrTooManyLoginAttempts {
		t.Fatalf("expected identical throttled error regardless of username validity, got %v / %v", errUnknown, errKnown)
	}
}

func TestLoginFromRequest_SuccessResetsFailureCounter(t *testing.T) {
	m, _ := testManager(t, 0)
	req := loginRequest("203.0.113.40:1234")

	for i := 0; i < maxLoginFailures-1; i++ {
		if _, err := m.LoginFromRequest(req, "operator", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: unexpected error %v", i, err)
		}
	}

	if _, err := m.LoginFromRequest(req, "operator", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("expected successful login to reset counter, got %v", err)
	}

	// Failure counter should be back to zero -- another near-max run of
	// failures should not immediately throttle.
	for i := 0; i < maxLoginFailures-1; i++ {
		if _, err := m.LoginFromRequest(req, "operator", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("post-reset attempt %d: unexpected error %v", i, err)
		}
	}
	if _, err := m.LoginFromRequest(req, "operator", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("expected login to still be allowed after reset, got %v", err)
	}
}

func TestLoginFromRequest_RecoveryAfterWindowExpires(t *testing.T) {
	m, _ := testManager(t, 0)
	req := loginRequest("203.0.113.50:1234")

	frozen := m.now()
	m.now = func() time.Time { return frozen }

	for i := 0; i < maxLoginFailures; i++ {
		if _, err := m.LoginFromRequest(req, "operator", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: unexpected error %v", i, err)
		}
	}
	if _, err := m.LoginFromRequest(req, "operator", "correct-horse-battery-staple"); err != ErrTooManyLoginAttempts {
		t.Fatalf("expected throttling before window expiry, got %v", err)
	}

	// Advance the clock past the failure window.
	frozen = frozen.Add(loginFailureWindow + time.Second)

	if _, err := m.LoginFromRequest(req, "operator", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("expected login to succeed after window expiry, got %v", err)
	}
}

func TestLoginFromRequest_DifferentIPsTrackedIndependently(t *testing.T) {
	m, _ := testManager(t, 0)
	reqA := loginRequest("203.0.113.60:1234")
	reqB := loginRequest("203.0.113.61:1234")

	for i := 0; i < maxLoginFailures; i++ {
		if _, err := m.LoginFromRequest(reqA, "operator", "wrong"); err != ErrInvalidCredentials {
			t.Fatalf("IP A attempt %d: unexpected error %v", i, err)
		}
	}
	if _, err := m.LoginFromRequest(reqA, "operator", "correct-horse-battery-staple"); err != ErrTooManyLoginAttempts {
		t.Fatalf("expected IP A throttled, got %v", err)
	}

	// IP B must be unaffected by IP A's failures.
	if _, err := m.LoginFromRequest(reqB, "operator", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("expected IP B unaffected, got %v", err)
	}
}

func TestLoginFromRequest_Concurrent(t *testing.T) {
	m, _ := testManager(t, 0)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := loginRequest("203.0.113.70:1234")
			if i%2 == 0 {
				_, _ = m.LoginFromRequest(req, "operator", "correct-horse-battery-staple")
			} else {
				_, _ = m.LoginFromRequest(req, "operator", "wrong")
			}
		}(i)
	}
	wg.Wait()
}

func TestLoginFromRequest_ErrorDoesNotMentionPasswordOrHash(t *testing.T) {
	m, hash := testManager(t, 0)
	req := loginRequest("203.0.113.80:1234")

	_, err := m.LoginFromRequest(req, "operator", "wrong-password-value")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "wrong-password-value") || strings.Contains(err.Error(), hash) {
		t.Fatalf("error must not leak password or hash: %v", err)
	}
}
