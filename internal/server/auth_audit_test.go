package server

import (
	"encoding/json"
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

func TestSessionRevokeByID(t *testing.T) {
	s := newWebUIServer(t, "")
	a, b := login(t, s), login(t, s)
	hdr := map[string]string{"Origin": "http://example.com"}

	listed := func(c *http.Cookie) (ids []string, cur string) {
		w := do(s, "GET", "/api/sessions", nil, c)
		var v []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		for _, e := range v {
			ids = append(ids, e.ID)
			if e.Current {
				cur = e.ID
			}
		}
		return
	}
	ids, curA := listed(a)
	if len(ids) != 2 || curA == "" || strings.Contains(strings.Join(ids, ""), a.Value) {
		t.Fatalf("ids=%v cur=%q", ids, curA)
	}
	if again, _ := listed(a); strings.Join(again, ",") != strings.Join(ids, ",") {
		t.Fatalf("ids not stable: %v vs %v", ids, again)
	}
	post := func(id string) int {
		req := httptest.NewRequest("POST", "/api/sessions/revoke", strings.NewReader(`{"id":"`+id+`"}`))
		req.Header.Set("Origin", hdr["Origin"])
		req.AddCookie(a)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w.Code
	}
	if c := post(curA); c != http.StatusConflict {
		t.Fatalf("own session: %d", c)
	}
	if c := post("sess-0000000000000000"); c != http.StatusNotFound {
		t.Fatalf("unknown id: %d", c)
	}
	var other string
	for _, id := range ids {
		if id != curA {
			other = id
		}
	}
	users, _, _ := eventsNamed(t, s, "session_revoke", func() {
		if c := post(other); c != http.StatusOK {
			t.Fatalf("revoke other: %d", c)
		}
		post("sess-0000000000000000")
	})
	if len(users) != 1 || users[0] != "webui:user:"+testOperator {
		t.Fatalf("audit users=%v", users)
	}
	if w := do(s, "GET", "/api/auth/session", nil, b); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still valid: %d", w.Code)
	}
	if w := do(s, "GET", "/api/auth/session", nil, a); w.Code != http.StatusOK {
		t.Fatalf("own session lost: %d", w.Code)
	}
	// foreign origin with a cookie must be refused (CSRF)
	req := httptest.NewRequest("POST", "/api/sessions/revoke", strings.NewReader(`{"id":"x"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(a)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", w.Code)
	}
}
