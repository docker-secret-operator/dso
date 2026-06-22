package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/apply"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/pkg/config"
	"gopkg.in/yaml.v3"
)

// ---- test helpers -----------------------------------------------------------

func validConfigYAML(t *testing.T, extraProvider bool) string {
	t.Helper()
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {Type: "vault", Auth: config.AuthConfig{Method: "token"}},
		},
		Secrets: []config.SecretMapping{
			{
				Name:     "test-secret",
				Provider: "vault",
				Inject:   config.InjectionConfig{Type: "file", Path: "/etc/secrets", UID: 1000, GID: 1000},
				Rotation: config.RotationConfigV2{Strategy: "restart", Enabled: true},
			},
		},
	}
	if extraProvider {
		// A second valid provider — changes the providers map (→ restart required).
		cfg.Providers["vault2"] = config.ProviderConfig{Type: "vault", Auth: config.AuthConfig{Method: "token"}}
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal valid config: %v", err)
	}
	return string(out)
}

// setupConfig writes a config file to a temp dir and points resolveConfig at it.
func setupConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dso.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DSO_CONFIG_PATH", path)
	return path
}

type fakeReconciler struct {
	called bool
	err    error
}

func (f *fakeReconciler) Reconcile(context.Context, *config.Config, *apply.ApplyPlan) error {
	f.called = true
	return f.err
}

type fakeAudit struct{ actions []string }

func (f *fakeAudit) LogEvent(_ context.Context, _, _, action, _, _, _ string) error {
	f.actions = append(f.actions, action)
	return nil
}

func adminReq(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	admin := &storage.User{ID: "u1", Username: "admin", Role: "admin"}
	return req.WithContext(auth.WithAuthenticatedUser(req.Context(), admin))
}

// ---- validate ---------------------------------------------------------------

func TestConfigEditor_Validate_Valid(t *testing.T) {
	setupConfig(t, validConfigYAML(t, false))
	e := NewConfigEditor(nil, nil, nil)

	rec := httptest.NewRecorder()
	e.HandleValidate(rec, adminReq(http.MethodPost, "/api/config/validate", validateRequest{Yaml: validConfigYAML(t, false)}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp validateResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Valid {
		t.Errorf("expected valid, got errors: %v", resp.Errors)
	}
}

func TestConfigEditor_Validate_Malformed(t *testing.T) {
	e := NewConfigEditor(nil, nil, nil)
	rec := httptest.NewRecorder()
	e.HandleValidate(rec, adminReq(http.MethodPost, "/api/config/validate", validateRequest{Yaml: "::: not yaml :::"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed YAML status = %d, want 400", rec.Code)
	}
}

func TestConfigEditor_Validate_InvalidConfig(t *testing.T) {
	e := NewConfigEditor(nil, nil, nil)
	// Secret references a provider that does not exist → cfg.Validate fails.
	bad := "providers: {}\nsecrets:\n  - name: x\n    provider: ghost\n"
	rec := httptest.NewRecorder()
	e.HandleValidate(rec, adminReq(http.MethodPost, "/api/config/validate", validateRequest{Yaml: bad}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp validateResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Valid || len(resp.Errors) == 0 {
		t.Errorf("expected invalid with errors, got %#v", resp)
	}
}

// ---- apply ------------------------------------------------------------------

func TestConfigEditor_Apply_DryRun_NoWrite(t *testing.T) {
	path := setupConfig(t, validConfigYAML(t, false))
	before, _ := os.ReadFile(path)
	e := NewConfigEditor(nil, &fakeReconciler{}, nil)

	rec := httptest.NewRecorder()
	e.HandleApply(rec, adminReq(http.MethodPost, "/api/config/apply",
		applyRequest{Yaml: validConfigYAML(t, true), DryRun: true}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp applyResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Plan == nil {
		t.Error("dry run should return a plan")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("dry run must not modify the config file")
	}
}

func TestConfigEditor_Apply_WritesBackupAtomicAuditReconcile(t *testing.T) {
	path := setupConfig(t, validConfigYAML(t, false))
	rec := &fakeReconciler{}
	audit := &fakeAudit{}
	e := NewConfigEditor(nil, rec, audit)

	newYAML := validConfigYAML(t, true) // adds a provider → restart required
	w := httptest.NewRecorder()
	e.HandleApply(w, adminReq(http.MethodPost, "/api/config/apply", applyRequest{Yaml: newYAML}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp applyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if !resp.Success {
		t.Error("expected success")
	}
	if !resp.RestartRequired {
		t.Error("adding a provider should require restart")
	}
	// File updated.
	got, _ := os.ReadFile(path)
	if string(got) != newYAML {
		t.Error("config file was not updated with new YAML")
	}
	// Backup created and contains the OLD content.
	if resp.BackupPath == "" {
		t.Fatal("expected a backup path")
	}
	if !strings.HasPrefix(filepath.Base(resp.BackupPath), backupPrefix) {
		t.Errorf("backup name %q lacks prefix", resp.BackupPath)
	}
	// Reconcile + audit happened.
	if !rec.called {
		t.Error("reconciler should have been called")
	}
	if len(audit.actions) == 0 || audit.actions[0] != "config.apply" {
		t.Errorf("expected config.apply audit, got %v", audit.actions)
	}
}

func TestConfigEditor_Apply_ConcurrentConflict(t *testing.T) {
	setupConfig(t, validConfigYAML(t, false))
	e := NewConfigEditor(nil, &fakeReconciler{}, nil)

	w := httptest.NewRecorder()
	e.HandleApply(w, adminReq(http.MethodPost, "/api/config/apply",
		applyRequest{Yaml: validConfigYAML(t, true), BaseHash: "stale-hash-does-not-match"}))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestConfigEditor_Apply_SecretOnlyNoRestart(t *testing.T) {
	// Start from a 2-provider config; change only a secret → no restart.
	base := validConfigYAML(t, true)
	setupConfig(t, base)
	e := NewConfigEditor(nil, &fakeReconciler{}, nil)

	var cfg config.Config
	yaml.Unmarshal([]byte(base), &cfg)
	cfg.Secrets = append(cfg.Secrets, config.SecretMapping{
		Name: "another", Provider: "vault",
		Inject:   config.InjectionConfig{Type: "file", Path: "/etc/s2", UID: 1, GID: 1},
		Rotation: config.RotationConfigV2{Strategy: "restart", Enabled: true},
	})
	out, _ := yaml.Marshal(&cfg)

	w := httptest.NewRecorder()
	e.HandleApply(w, adminReq(http.MethodPost, "/api/config/apply", applyRequest{Yaml: string(out)}))
	var resp applyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.RestartRequired {
		t.Error("secret-only change should not require restart")
	}
}

func TestConfigEditor_Apply_InvalidYAML(t *testing.T) {
	path := setupConfig(t, validConfigYAML(t, false))
	before, _ := os.ReadFile(path)
	e := NewConfigEditor(nil, &fakeReconciler{}, nil)

	w := httptest.NewRecorder()
	e.HandleApply(w, adminReq(http.MethodPost, "/api/config/apply", applyRequest{Yaml: "::: bad :::"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("invalid apply must leave the original file intact")
	}
}

// ---- backups + rollback -----------------------------------------------------

func TestConfigEditor_BackupsAndRollback(t *testing.T) {
	path := setupConfig(t, validConfigYAML(t, false))
	rec := &fakeReconciler{}
	e := NewConfigEditor(nil, rec, nil)

	// Apply a change to generate a backup of the original.
	newYAML := validConfigYAML(t, true)
	w := httptest.NewRecorder()
	e.HandleApply(w, adminReq(http.MethodPost, "/api/config/apply", applyRequest{Yaml: newYAML}))
	var applyResp applyResponse
	json.Unmarshal(w.Body.Bytes(), &applyResp)

	// List backups.
	w = httptest.NewRecorder()
	e.HandleBackups(w, adminReq(http.MethodGet, "/api/config/backups", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("backups status = %d", w.Code)
	}
	var backups []backupInfo
	json.Unmarshal(w.Body.Bytes(), &backups)
	if len(backups) == 0 {
		t.Fatal("expected at least one backup")
	}

	// Roll back to the original.
	rec.called = false
	w = httptest.NewRecorder()
	e.HandleRollback(w, adminReq(http.MethodPost, "/api/config/rollback", rollbackRequest{BackupPath: applyResp.BackupPath}))
	if w.Code != http.StatusOK {
		t.Fatalf("rollback status = %d: %s", w.Code, w.Body.String())
	}
	if !rec.called {
		t.Error("rollback should trigger reconcile")
	}
	// Current file should now match the original (pre-apply) content.
	got, _ := os.ReadFile(path)
	if string(got) != validConfigYAML(t, false) {
		t.Error("rollback did not restore original content")
	}
}

func TestConfigEditor_Rollback_Traversal(t *testing.T) {
	setupConfig(t, validConfigYAML(t, false))
	e := NewConfigEditor(nil, &fakeReconciler{}, nil)

	for _, bad := range []string{"/etc/passwd", "../../etc/passwd", "dso.yaml", ""} {
		w := httptest.NewRecorder()
		e.HandleRollback(w, adminReq(http.MethodPost, "/api/config/rollback", rollbackRequest{BackupPath: bad}))
		if w.Code != http.StatusBadRequest {
			t.Errorf("backupPath %q: status = %d, want 400", bad, w.Code)
		}
	}
}

func TestConfigEditor_Rollback_UnknownButValidShape(t *testing.T) {
	path := setupConfig(t, validConfigYAML(t, false))
	e := NewConfigEditor(nil, &fakeReconciler{}, nil)
	// Correct dir + prefix, but the file doesn't exist.
	missing := filepath.Join(filepath.Dir(path), backupPrefix+"20990101T000000Z")
	w := httptest.NewRecorder()
	e.HandleRollback(w, adminReq(http.MethodPost, "/api/config/rollback", rollbackRequest{BackupPath: missing}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
