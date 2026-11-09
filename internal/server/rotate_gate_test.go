package server

import (
	"net/http"
	"testing"
)

// The manual-rotation and notification-test endpoints are state-changing POSTs
// reachable with the ambient session cookie, so they must sit behind the same
// authentication gate and Origin (CSRF) check as every other protected route.
func TestRotateAndNotifyTest_BehindAuthAndOriginGate(t *testing.T) {
	s := newWebUIServer(t, "")
	targets := []string{"/api/secrets/db_password/rotate", "/api/integrations/notifications/test"}
	for _, target := range targets {
		if w := do(s, "POST", target, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s anonymous = %d, want 401", target, w.Code)
		}
		c := login(t, s)
		for name, hdr := range map[string]map[string]string{
			"cross-site origin":   {"Origin": "https://evil.example"},
			"null origin":         {"Origin": "null"},
			"cross-site metadata": {"Sec-Fetch-Site": "cross-site"},
		} {
			if w := do(s, "POST", target, hdr, c); w.Code != http.StatusForbidden {
				t.Errorf("%s %s = %d, want 403", target, name, w.Code)
			}
		}
		// Same origin passes the gate (the handler may still answer 4xx/5xx
		// for its own reasons, but never 401/403).
		if w := do(s, "POST", target, map[string]string{"Origin": "http://example.com"}, c); w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			t.Errorf("%s same-origin = %d, must pass the gate", target, w.Code)
		}
	}
}
