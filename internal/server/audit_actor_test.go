package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/audit"
	"github.com/docker-secret-operator/dso/internal/auth"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	actorTestToken = "ACTOR-TEST-TOKEN-7f3a9c-SECRET"
	rotatePath     = "/api/secrets/db_password/rotate"
	notifyPath     = "/api/integrations/notifications/test"
)

// captureAudit routes the global audit logger into an observer for the test.
func captureAudit(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	audit.InitAuditLogger(zap.New(core))
	t.Cleanup(func() { audit.InitAuditLogger(nil) })
	return logs
}

func auditEntries(logs *observer.ObservedLogs) []observer.LoggedEntry {
	var out []observer.LoggedEntry
	for _, e := range logs.All() {
		if e.Message == "audit_event" {
			out = append(out, e)
		}
	}
	return out
}

func auditUser(e observer.LoggedEntry) string {
	for _, f := range e.Context {
		if f.Key == "user" {
			return f.String
		}
	}
	return ""
}

func dumpEntries(logs *observer.ObservedLogs) string {
	var b strings.Builder
	for _, e := range logs.All() {
		b.WriteString(e.Message)
		for _, f := range e.Context {
			fmt.Fprintf(&b, " %s=%s|%v", f.Key, f.String, f.Interface)
		}
		b.WriteString("\n")
	}
	return b.String()
}

var actorEndpoints = []struct{ name, path, event string }{
	{"rotate", rotatePath, "manual_rotation_triggered"},
	{"notify-test", notifyPath, "test_notification_sent"},
}

func actorServer(t *testing.T, mode string) (*RESTServer, func(path string, hdr map[string]string) *httptest.ResponseRecorder) {
	t.Helper()
	var s *RESTServer
	switch mode {
	case "session":
		s = newWebUIServer(t, "")
	case "session+token":
		s = newWebUIServer(t, actorTestToken)
	case "token":
		t.Setenv("DSO_AUTH_TOKEN", actorTestToken)
		s = createTestRESTServer()
		s.Auth = auth.NewAuthenticator()
	case "open":
		t.Setenv("DSO_AUTH_TOKEN", "")
		s = createTestRESTServer()
	default:
		t.Fatalf("mode %s", mode)
	}
	s.Config.Secrets = restServerWithSecret("db_password").Config.Secrets
	s.Reloader = restServerWithSecret("db_password").Reloader
	var cookie *http.Cookie
	if strings.HasPrefix(mode, "session") {
		cookie = login(t, s)
	}
	return s, func(path string, hdr map[string]string) *httptest.ResponseRecorder {
		h := map[string]string{"Origin": "http://example.com"}
		for k, v := range hdr {
			h[k] = v
		}
		_, noCookie := h["no-cookie"]
		delete(h, "no-cookie") // test-only sentinel, never sent
		if cookie != nil && !noCookie {
			return do(s, "POST", path, h, cookie)
		}
		return do(s, "POST", path, h)
	}
}

func TestAuditActor_PerAuthenticationMode(t *testing.T) {
	fp := tokenFingerprint(actorTestToken)
	cases := []struct {
		mode string
		hdr  map[string]string
		want string
	}{
		{"session", nil, "webui:user:operator"},
		{"session+token", map[string]string{"Authorization": "Bearer " + actorTestToken}, "api-bearer:" + fp},
		{"token", map[string]string{"Authorization": "Bearer " + actorTestToken}, "api-bearer:" + fp},
		{"open", nil, "anonymous-legacy-open"},
	}
	for _, ep := range actorEndpoints {
		for _, c := range cases {
			t.Run(ep.name+"/"+c.mode, func(t *testing.T) {
				logs := captureAudit(t)
				_, send := actorServer(t, c.mode)
				hdr := c.hdr
				if c.mode == "session+token" {
					hdr = map[string]string{"Authorization": "Bearer " + actorTestToken, "no-cookie": "1"}
				}
				w := send(ep.path, hdr)
				if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
					t.Fatalf("unexpected denial %d", w.Code)
				}
				es := auditEntries(logs)
				if len(es) != 1 || auditUser(es[0]) != c.want {
					t.Fatalf("audit entries = %d, user = %q, want 1 / %q", len(es), func() string {
						if len(es) > 0 {
							return auditUser(es[0])
						}
						return ""
					}(), c.want)
				}
				if es[0].Context[0].Key != "event" || es[0].Context[0].String != ep.event {
					t.Errorf("event = %q, want %q", es[0].Context[0].String, ep.event)
				}
			})
		}
	}
}

func TestAuditActor_WebhookIsMethodOnly(t *testing.T) {
	a := webhookActor()
	if a.Name != "webhook" || !a.Authenticated {
		t.Fatalf("webhook actor = %+v", a)
	}
	// The webhook route is exempt from the bearer gate, so actorFromContext must
	// never claim it for an arbitrary request.
	s := createTestRESTServer()
	s.allowAnonymous = false
	req := httptest.NewRequest("POST", webhookPath, nil)
	if got := s.actorFromContext(req); got.Authenticated || got.Name != "unauthenticated" {
		t.Fatalf("unsigned webhook-path request actor = %+v", got)
	}
}

func TestAuditActor_DeniedRequestsAttributeNoPrincipal(t *testing.T) {
	denials := []struct {
		mode string
		hdr  map[string]string
		want int
	}{
		{"token", map[string]string{"Authorization": "Bearer wrong-token"}, 401},
		{"token", nil, 401},
		{"session", map[string]string{"no-cookie": "1"}, 401},
		{"session", map[string]string{"Origin": "https://evil.example"}, 403},
		{"session+token", map[string]string{"no-cookie": "1", "Authorization": "Bearer nope"}, 401},
	}
	for _, ep := range actorEndpoints {
		for i, d := range denials {
			t.Run(fmt.Sprintf("%s/%s/%d", ep.name, d.mode, i), func(t *testing.T) {
				logs := captureAudit(t)
				s, send := actorServer(t, d.mode)
				w := send(ep.path, d.hdr)
				if w.Code != d.want {
					t.Fatalf("status = %d, want %d", w.Code, d.want)
				}
				if es := auditEntries(logs); len(es) != 0 {
					t.Fatalf("denied request produced %d audit events: %s", len(es), dumpEntries(logs))
				}
				req := httptest.NewRequest("POST", ep.path, nil)
				req.Header.Set("Authorization", "Bearer wrong-token")
				if a := s.actorFromContext(req); a.Authenticated && d.mode != "open" {
					t.Fatalf("bad credential resolved to authenticated actor %+v", a)
				}
			})
		}
	}
}

func TestAuditActor_SpoofedIdentityAndAddressIgnored(t *testing.T) {
	spoof := map[string]string{
		"X-Actor": "admin", "X-User": "admin", "X-Forwarded-User": "admin", "X-Remote-User": "admin",
		"X-Forwarded-For": "203.0.113.99", "X-Real-IP": "203.0.113.99", "Forwarded": "for=203.0.113.99",
	}
	for _, ep := range actorEndpoints {
		for _, mode := range []string{"session", "token", "open"} {
			t.Run(ep.name+"/"+mode, func(t *testing.T) {
				logs := captureAudit(t)
				_, send := actorServer(t, mode)
				hdr := map[string]string{}
				for k, v := range spoof {
					hdr[k] = v
				}
				if mode == "token" {
					hdr["Authorization"] = "Bearer " + actorTestToken
				}
				w := send(ep.path+"?actor=admin&user=admin", hdr)
				if w.Code == 401 || w.Code == 403 {
					t.Fatalf("unexpected denial %d", w.Code)
				}
				es := auditEntries(logs)
				if len(es) < 1 {
					t.Fatal("no audit event")
				}
				for _, e := range es {
					u := auditUser(e)
					if strings.Contains(u, "admin") {
						t.Fatalf("spoofed identity leaked into actor %q", u)
					}
				}
				// No audit field carries a client address, so a spoofed
				// X-Forwarded-For cannot change one.
				if d := dumpEntries(logs); strings.Contains(d, "203.0.113.99") {
					t.Fatalf("spoofed address recorded: %s", d)
				}
			})
		}
	}
}

func TestAuditActor_NoSecretMaterialInAuditLogsOrResponse(t *testing.T) {
	for _, ep := range actorEndpoints {
		for _, mode := range []string{"session+token", "token"} {
			t.Run(ep.name+"/"+mode, func(t *testing.T) {
				logs := captureAudit(t)
				_, send := actorServer(t, mode)
				hdr := map[string]string{"Authorization": "Bearer " + actorTestToken}
				if strings.HasPrefix(mode, "session") {
					hdr["no-cookie"] = "1"
				}
				w := send(ep.path, hdr)
				whole := w.Result().Status + w.Body.String() + strings.Join(w.Result().Header.Values("Set-Cookie"), "\n")
				secrets := []string{actorTestToken, testPassword}
				all := whole + dumpEntries(logs)
				for _, sec := range secrets {
					if strings.Contains(all, sec) {
						t.Fatalf("secret value leaked")
					}
				}
			})
		}
	}
}

func TestAuditActor_SessionCookieValueNeverRecorded(t *testing.T) {
	logs := captureAudit(t)
	core, srvLogs := observer.New(zapcore.DebugLevel)
	s := newWebUIServer(t, "")
	s.Logger = zap.New(core)
	s.Config.Secrets = restServerWithSecret("db_password").Config.Secrets
	s.Reloader = restServerWithSecret("db_password").Reloader
	c := login(t, s)
	w := do(s, "POST", rotatePath, map[string]string{"Origin": "http://example.com"}, c)
	all := w.Body.String() + dumpEntries(logs) + dumpEntries(srvLogs)
	if strings.Contains(all, c.Value) || len(c.Value) < 8 {
		t.Fatalf("session cookie value found in audit/log/response output (or cookie empty)")
	}
	if es := auditEntries(logs); len(es) != 1 || auditUser(es[0]) != "webui:user:operator" {
		t.Fatalf("audit = %s", dumpEntries(logs))
	}
}

func TestTokenFingerprint_StableDistinctNonReversible(t *testing.T) {
	a, b := tokenFingerprint("token-A"), tokenFingerprint("token-B")
	if a == b || a != tokenFingerprint("token-A") || len(a) != 12 {
		t.Fatalf("fingerprint properties violated: %s %s", a, b)
	}
	if strings.Contains(a, "token") {
		t.Fatal("fingerprint contains token text")
	}
}

func TestSanitizeActorPart(t *testing.T) {
	got := sanitizeActorPart("bob\n\"event\":\"x\"\x00" + strings.Repeat("a", 200))
	if strings.ContainsAny(got, "\n\x00") || len(got) > maxActorUsernameLen {
		t.Fatalf("sanitize = %q", got)
	}
}
