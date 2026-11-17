package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/audit"
)

func exportReq(s *RESTServer) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/audit/export", nil)
	req.Header.Set("Authorization", "Bearer "+hardeningToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestAuditExport_DisabledIs503AndAnonymousIs401(t *testing.T) {
	audit.SetDurable(nil)
	s := newWebUIServer(t, hardeningToken)
	if w := exportReq(s); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no sink: %d", w.Code)
	}
	anon := httptest.NewRecorder()
	s.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/api/audit/export", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", anon.Code)
	}
}

func TestAuditExport_StreamsVerifiedChainAndRefusesTamper(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	d, err := audit.OpenDurable(p)
	if err != nil {
		t.Fatal(err)
	}
	audit.SetDurable(d)
	t.Cleanup(func() { audit.SetDurable(nil); _ = d.Close() })
	_ = d.Append("manual_rotation_triggered", "token:ab12cd34", "", "db", "", "success")
	_ = d.Append("auth_login", "operator", "", "", "", "failure")
	s := newWebUIServer(t, hardeningToken)

	w := exportReq(s)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("export: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if lines := strings.Count(strings.TrimSpace(w.Body.String()), "\n") + 1; lines != 2 {
		t.Fatalf("lines = %d", lines)
	}

	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), `"status":"success"`, `"status":"failure"`, 1)), 0o600)
	w = exportReq(s)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "manual_rotation") {
		t.Fatalf("tampered export must be refused without content: %d %q", w.Code, w.Body.String())
	}
}
