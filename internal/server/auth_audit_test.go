package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// M3 slice 2: the session/auth lifecycle is audited with a server-derived
// actor. These tests drive ServeHTTP (the real gate and handlers).

func eventsNamed(t *testing.T, s *RESTServer, name string, run func()) (users, statuses []string, dump string) {
	t.Helper()
	logs := captureAudit(t)
	run()
	for _, e := range allAuditEntries(logs) {
		isEv, status := false, ""
		for _, f := range e.Context {
			if f.Key == "event" && f.String == name {
				isEv = true
			}
			if f.Key == "status" {
				status = f.String
			}
		}
		if isEv {
			users = append(users, auditUser(e))
			statuses = append(statuses, status)
		}
	}
	return users, statuses, dumpEntries(logs)
}

func postLogin(s *RESTServer, user, pass string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"`+user+`","password":"`+pass+`"}`))
	req.RemoteAddr = "192.0.2.10:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestAuthAudit_LoginSuccessAttributedToOperator(t *testing.T) {
	s := newWebUIServer(t, "")
	users, st, _ := eventsNamed(t, s, "auth_login", func() { login(t, s) })
	if len(users) != 1 || st[0] != "success" || users[0] != "webui:user:"+testOperator {
		t.Fatalf("got users=%v statuses=%v", users, st)
	}
}

func TestAuthAudit_LoginFailureNeverRecordsAttemptedCredentials(t *testing.T) {
	s := newWebUIServer(t, "")
	const typedPassword = "hunter2-typed-in-username-field"
	users, st, dump := eventsNamed(t, s, "auth_login", func() {
		if w := postLogin(s, typedPassword, "wrong"); w.Code != http.StatusUnauthorized {
			t.Fatalf("code %d", w.Code)
		}
	})
	if len(users) != 1 || st[0] != "failure" || users[0] != actorUnauthenticated {
		t.Fatalf("got users=%v statuses=%v", users, st)
	}
	for _, bad := range []string{typedPassword, "wrong", "192.0.2.10"} {
		if strings.Contains(dump, bad) {
			t.Fatalf("audit output leaked %q: %s", bad, dump)
		}
	}
}

func TestAuthAudit_RateLimitTripIsRecorded(t *testing.T) {
	s := newWebUIServer(t, "")
	_, st, _ := eventsNamed(t, s, "auth_login", func() {
		for i := 0; i < 60; i++ {
			if postLogin(s, testOperator, "wrong").Code == http.StatusTooManyRequests {
				return
			}
		}
		t.Fatal("login limiter never tripped")
	})
	if st[len(st)-1] != "rate_limited" {
		t.Fatalf("last status %v, want rate_limited", st)
	}
}

func TestAuthAudit_LogoutAttributedOnlyWhenSessionExists(t *testing.T) {
	s := newWebUIServer(t, "")
	c := login(t, s)
	users, st, _ := eventsNamed(t, s, "auth_logout", func() {
		do(s, "POST", "/api/auth/logout", nil)    // anonymous: not recorded
		do(s, "POST", "/api/auth/logout", nil, c) // real session: recorded
	})
	if len(users) != 1 || st[0] != "success" || users[0] != "webui:user:"+testOperator {
		t.Fatalf("got users=%v statuses=%v", users, st)
	}
	if w := do(s, "GET", "/api/auth/session", nil, c); w.Code != http.StatusUnauthorized {
		t.Fatalf("session still valid after logout: %d", w.Code)
	}
}

func TestAuthAudit_RevokeOthersAttributedPerCredential(t *testing.T) {
	s := newWebUIServer(t, actorTestToken)
	c := login(t, s)
	users, _, _ := eventsNamed(t, s, "sessions_revoke_others", func() {
		do(s, "POST", "/api/sessions/revoke-others", map[string]string{"Origin": "http://example.com"}, c)
		do(s, "POST", "/api/sessions/revoke-others", map[string]string{"Authorization": "Bearer " + actorTestToken})
		do(s, "POST", "/api/sessions/revoke-others", nil) // denied: no event
	})
	if len(users) != 2 || users[0] != "webui:user:"+testOperator || !strings.HasPrefix(users[1], "api-bearer:") {
		t.Fatalf("users=%v", users)
	}
	if strings.Contains(strings.Join(users, ""), actorTestToken) {
		t.Fatal("bearer token leaked into actor")
	}
}
