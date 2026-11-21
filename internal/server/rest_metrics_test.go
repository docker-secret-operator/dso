package server

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRESTMetrics_AuthAndSessionCounters(t *testing.T) {
	s := newWebUIServer(t, actorTestToken)
	delta := func(before float64, got float64) float64 { return got - before }

	unauth := testutil.ToFloat64(observability.RESTAuthFailuresTotal.WithLabelValues("unauthenticated"))
	cross := testutil.ToFloat64(observability.RESTAuthFailuresTotal.WithLabelValues("cross_origin"))
	lf := testutil.ToFloat64(observability.RESTLoginAttemptsTotal.WithLabelValues("failure"))
	ls := testutil.ToFloat64(observability.RESTLoginAttemptsTotal.WithLabelValues("success"))
	rev := testutil.ToFloat64(observability.RESTSessionsRevokedTotal)

	do(s, "GET", "/api/drift", nil)
	if d := delta(unauth, testutil.ToFloat64(observability.RESTAuthFailuresTotal.WithLabelValues("unauthenticated"))); d != 1 {
		t.Fatalf("unauthenticated delta %v", d)
	}
	postLogin(s, testOperator, "wrong")
	c := login(t, s)
	do(s, "POST", "/api/sessions/revoke-others", map[string]string{"Origin": "https://evil.example"}, c)
	if d := delta(cross, testutil.ToFloat64(observability.RESTAuthFailuresTotal.WithLabelValues("cross_origin"))); d != 1 {
		t.Fatalf("cross_origin delta %v", d)
	}
	if d := delta(lf, testutil.ToFloat64(observability.RESTLoginAttemptsTotal.WithLabelValues("failure"))); d != 1 {
		t.Fatalf("login failure delta %v", d)
	}
	if d := delta(ls, testutil.ToFloat64(observability.RESTLoginAttemptsTotal.WithLabelValues("success"))); d != 1 {
		t.Fatalf("login success delta %v", d)
	}
	login(t, s) // second session
	do(s, "POST", "/api/sessions/revoke-others", map[string]string{"Origin": "http://example.com"}, c)
	if d := delta(rev, testutil.ToFloat64(observability.RESTSessionsRevokedTotal)); d != 1 {
		t.Fatalf("revoked delta %v", d)
	}
}

func TestRESTMetrics_RotationRequestCounter(t *testing.T) {
	_, send := actorServer(t, "token")
	// This fixture has no managed rotation engine, so the request is refused (503)
	// and must be counted under status="failure", never "success".
	f := testutil.ToFloat64(observability.RESTManualRotationRequestsTotal.WithLabelValues("failure"))
	ok := testutil.ToFloat64(observability.RESTManualRotationRequestsTotal.WithLabelValues("success"))
	w := send(rotatePath, map[string]string{"Authorization": "Bearer " + actorTestToken})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("fixture changed: code %d, want 503", w.Code)
	}
	if d := testutil.ToFloat64(observability.RESTManualRotationRequestsTotal.WithLabelValues("failure")) - f; d != 1 {
		t.Fatalf("failure delta %v", d)
	}
	if d := testutil.ToFloat64(observability.RESTManualRotationRequestsTotal.WithLabelValues("success")) - ok; d != 0 {
		t.Fatalf("success delta %v", d)
	}
}

// Every REST metric must be in the catalogue (docs/metrics.md).
func TestRESTMetrics_CataloguedAndLabelsBounded(t *testing.T) {
	b, err := os.ReadFile("../../docs/metrics.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dso_rest_auth_failures_total", "dso_rest_login_attempts_total",
		"dso_rest_sessions_revoked_total", "dso_rest_manual_rotation_requests_total", "dso_rest_websocket_clients", "dso_rest_active_sessions"} {
		if !strings.Contains(string(b), "`"+name+"`") {
			t.Errorf("%s missing from docs/metrics.md", name)
		}
	}
}
