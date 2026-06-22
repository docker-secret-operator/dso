package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/apply"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// ConfigAuditLogger records config-change audit events. *services.AuditService
// satisfies it; tests supply a fake.
type ConfigAuditLogger interface {
	LogEvent(ctx context.Context, actorID, actorName, action, resource, resourceID, resourceType string) error
}

const backupPrefix = "dso.yaml.bak-"
const backupTimeFormat = "20060102T150405Z"

// ConfigEditor implements admin-only config editing: validate, apply (with
// backup + atomic write + audit + best-effort reconcile), list backups, and
// rollback. There is no live config reload in v1 — apply reports
// restart_required when providers/global settings change.
type ConfigEditor struct {
	logger     *zap.Logger
	reconciler apply.Reconciler  // optional; nil = saved-only
	audit      ConfigAuditLogger // optional

	mu              sync.Mutex
	restartRequired bool
}

// NewConfigEditor creates the editor. reconciler/audit may be nil.
func NewConfigEditor(logger *zap.Logger, reconciler apply.Reconciler, audit ConfigAuditLogger) *ConfigEditor {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ConfigEditor{logger: logger, reconciler: reconciler, audit: audit}
}

// ---- request/response types -------------------------------------------------

type rawConfigResponseV2 struct {
	Path            string `json:"path"`
	Yaml            string `json:"yaml"`
	ModifiedAt      string `json:"modified_at"`
	SHA256          string `json:"sha256"`
	RestartRequired bool   `json:"restart_required"`
}

type validateRequest struct {
	Yaml string `json:"yaml"`
}

type validateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type applyRequest struct {
	Yaml     string `json:"yaml"`
	BaseHash string `json:"base_hash"`
	DryRun   bool   `json:"dry_run"`
}

type applyResponse struct {
	Success         bool               `json:"success"`
	RestartRequired bool               `json:"restart_required"`
	BackupPath      string             `json:"backup_path,omitempty"`
	Plan            *apply.ApplyPlan   `json:"plan,omitempty"`
	Result          *apply.ApplyResult `json:"result,omitempty"`
}

type backupInfo struct {
	Timestamp string `json:"timestamp"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
}

type rollbackRequest struct {
	BackupPath string `json:"backup_path"`
}

// ---- handlers ---------------------------------------------------------------

// HandleGetRaw — GET /api/config/raw
func (e *ConfigEditor) HandleGetRaw(w http.ResponseWriter, r *http.Request) {
	path := resolveConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("failed to read config: %v", err)})
		return
	}
	info, _ := os.Stat(path)
	modified := ""
	if info != nil {
		modified = info.ModTime().UTC().Format(time.RFC3339)
	}
	e.mu.Lock()
	restart := e.restartRequired
	e.mu.Unlock()

	writeJSON(w, http.StatusOK, rawConfigResponseV2{
		Path:            path,
		Yaml:            string(data),
		ModifiedAt:      modified,
		SHA256:          sha256hex(data),
		RestartRequired: restart,
	})
}

// HandleValidate — POST /api/config/validate
func (e *ConfigEditor) HandleValidate(w http.ResponseWriter, r *http.Request) {
	var req validateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	cfg, err := parseConfig(req.Yaml)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid YAML: %v", err)})
		return
	}
	if err := cfg.Validate(); err != nil {
		writeJSON(w, http.StatusOK, validateResponse{Valid: false, Errors: []string{err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, validateResponse{Valid: true, Errors: []string{}})
}

// HandleApply — POST /api/config/apply
func (e *ConfigEditor) HandleApply(w http.ResponseWriter, r *http.Request) {
	var req applyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	desired, err := parseConfig(req.Yaml)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid YAML: %v", err)})
		return
	}
	if err := desired.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, validateResponse{Valid: false, Errors: []string{err.Error()}})
		return
	}

	path := resolveConfig()
	current, currentBytes := loadCurrent(path)

	// Dry run: compute the plan only, never write.
	if req.DryRun {
		writeJSON(w, http.StatusOK, applyResponse{
			Success: true,
			Plan:    apply.ComputePlan(current, desired),
		})
		return
	}

	// Concurrent-edit protection.
	if req.BaseHash != "" && currentBytes != nil && req.BaseHash != sha256hex(currentBytes) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "configuration changed since it was loaded; reload and retry",
		})
		return
	}

	// Backup current file (if any).
	backupPath := ""
	if currentBytes != nil {
		bp, err := writeBackup(path, currentBytes)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("backup failed: %v", err)})
			return
		}
		backupPath = bp
	}

	// Atomic write.
	if err := atomicWrite(path, []byte(req.Yaml)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("write failed: %v", err)})
		return
	}

	e.logAudit(r, "config.apply", path, fmt.Sprintf("backup=%s", backupPath))

	plan := apply.ComputePlan(current, desired)
	restartRequired := apply.RequiresRestart(current, desired)
	result, _ := apply.Execute(r.Context(), desired, plan, e.reconciler)

	e.setRestartRequired(restartRequired)

	writeJSON(w, http.StatusOK, applyResponse{
		Success:         true,
		RestartRequired: restartRequired,
		BackupPath:      backupPath,
		Plan:            plan,
		Result:          result,
	})
}

// HandleBackups — GET /api/config/backups
func (e *ConfigEditor) HandleBackups(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Dir(resolveConfig())
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, http.StatusOK, []backupInfo{})
		return
	}
	backups := make([]backupInfo, 0)
	for _, ent := range entries {
		name := ent.Name()
		if !strings.HasPrefix(name, backupPrefix) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupInfo{
			Timestamp: strings.TrimPrefix(name, backupPrefix),
			Path:      filepath.Join(dir, name),
			Size:      info.Size(),
		})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].Timestamp > backups[j].Timestamp })
	writeJSON(w, http.StatusOK, backups)
}

// HandleRollback — POST /api/config/rollback
func (e *ConfigEditor) HandleRollback(w http.ResponseWriter, r *http.Request) {
	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	path := resolveConfig()
	dir := filepath.Dir(path)
	if err := validateBackupPath(dir, req.BackupPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	restoreBytes, err := os.ReadFile(req.BackupPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "backup not found"})
		return
	}

	desired, err := parseConfig(string(restoreBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("backup is not valid YAML: %v", err)})
		return
	}
	if err := desired.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, validateResponse{Valid: false, Errors: []string{err.Error()}})
		return
	}

	current, currentBytes := loadCurrent(path)
	backupPath := ""
	if currentBytes != nil {
		bp, err := writeBackup(path, currentBytes)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("backup failed: %v", err)})
			return
		}
		backupPath = bp
	}

	if err := atomicWrite(path, restoreBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("write failed: %v", err)})
		return
	}

	e.logAudit(r, "config.rollback", path, fmt.Sprintf("restored=%s backup=%s", req.BackupPath, backupPath))

	plan := apply.ComputePlan(current, desired)
	restartRequired := apply.RequiresRestart(current, desired)
	result, _ := apply.Execute(r.Context(), desired, plan, e.reconciler)
	e.setRestartRequired(restartRequired)

	writeJSON(w, http.StatusOK, applyResponse{
		Success:         true,
		RestartRequired: restartRequired,
		BackupPath:      backupPath,
		Plan:            plan,
		Result:          result,
	})
}

// ---- helpers ----------------------------------------------------------------

func (e *ConfigEditor) setRestartRequired(v bool) {
	e.mu.Lock()
	if v {
		e.restartRequired = true
	}
	e.mu.Unlock()
}

func (e *ConfigEditor) logAudit(r *http.Request, action, resource, detail string) {
	actor := auth.CurrentUser(r.Context())
	actorID, actorName := "system", "system"
	if actor != nil {
		actorID, actorName = actor.ID, actor.Username
	}
	e.logger.Info("config change", zap.String("action", action), zap.String("actor", actorName), zap.String("detail", detail))
	if e.audit != nil {
		_ = e.audit.LogEvent(r.Context(), actorID, actorName, action, "config", resource, "configuration")
	}
}

func parseConfig(text string) (*config.Config, error) {
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadCurrent best-effort loads the current config + its raw bytes. Returns
// (nil, nil) when the file is absent or unparseable (treated as no prior state).
func loadCurrent(path string) (*config.Config, []byte) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	cfg, err := parseConfig(string(data))
	if err != nil {
		return nil, data
	}
	return cfg, data
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// atomicWrite writes data to path via a temp file in the same directory,
// fsync, then rename — so the target is never left partially written.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dso-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if rename succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func writeBackup(path string, data []byte) (string, error) {
	dir := filepath.Dir(path)
	name := backupPrefix + time.Now().UTC().Format(backupTimeFormat)
	backupPath := filepath.Join(dir, name)
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return "", err
	}
	return backupPath, nil
}

// validateBackupPath rejects anything that isn't a dso.yaml.bak-* file living
// directly in the config directory (no traversal, no arbitrary files).
func validateBackupPath(dir, candidate string) error {
	if candidate == "" {
		return fmt.Errorf("backup_path is required")
	}
	clean := filepath.Clean(candidate)
	if filepath.Dir(clean) != filepath.Clean(dir) {
		return fmt.Errorf("invalid backup path")
	}
	if !strings.HasPrefix(filepath.Base(clean), backupPrefix) {
		return fmt.Errorf("invalid backup target")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
