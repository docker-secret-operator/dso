package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/services"
)

type BackupHandler struct {
	backupService *services.BackupService
}

func NewBackupHandler(backupService *services.BackupService) *BackupHandler {
	return &BackupHandler{backupService: backupService}
}

type BackupResponse struct {
	ID          string     `json:"id"`
	Filename    string     `json:"filename"`
	SizeBytes   int64      `json:"size_bytes"`
	Checksum    string     `json:"checksum"`
	BackupType  string     `json:"backup_type"`
	Status      string     `json:"status"`
	DurationMs  int        `json:"duration_ms"`
	ErrorMsg    *string    `json:"error_msg,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type BackupStatusResponse struct {
	LatestBackupAge    *int64  `json:"latest_backup_age_minutes,omitempty"`
	TotalBackups       int     `json:"total_backups"`
	FailedBackups      int     `json:"failed_backups"`
	CompletedBackups   int     `json:"completed_backups"`
	WorkerRunning      bool    `json:"worker_running"`
	LastBackupSize     *int64  `json:"last_backup_size_bytes,omitempty"`
	RetentionDays      int     `json:"retention_days"`
}

func (h *BackupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/backups") && r.Method == "GET":
		h.handleGetBackups(w, r, user)
	case strings.HasSuffix(r.URL.Path, "/backups") && r.Method == "POST":
		if user.Role != "admin" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
			return
		}
		h.handleCreateBackup(w, r)
	case strings.HasSuffix(r.URL.Path, "/backups/status") && r.Method == "GET":
		if user.Role != "admin" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
			return
		}
		h.handleGetStatus(w, r)
	case strings.Contains(r.URL.Path, "/backups/") && strings.HasSuffix(r.URL.Path, "/restore") && r.Method == "POST":
		if user.Role != "admin" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
			return
		}
		h.handleRestore(w, r)
	case strings.Contains(r.URL.Path, "/backups/") && strings.HasSuffix(r.URL.Path, "/download") && r.Method == "GET":
		h.handleDownload(w, r, user)
	case strings.Contains(r.URL.Path, "/backups/") && !strings.Contains(r.URL.Path[strings.LastIndex(r.URL.Path, "/"):], "/") && r.Method == "DELETE":
		if user.Role != "admin" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
			return
		}
		h.handleDeleteBackup(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
	}
}

func (h *BackupHandler) handleGetBackups(w http.ResponseWriter, r *http.Request, user any) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			if l > 100 {
				l = 100
			}
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	backups, err := h.backupService.GetBackups(r.Context(), limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch backups"})
		return
	}

	response := make([]BackupResponse, 0, len(backups))
	for _, b := range backups {
		response = append(response, BackupResponse{
			ID:          b.ID,
			Filename:    b.Filename,
			SizeBytes:   b.SizeBytes,
			Checksum:    b.Checksum,
			BackupType:  b.BackupType,
			Status:      b.Status,
			DurationMs:  b.DurationMs,
			ErrorMsg:    b.ErrorMsg,
			CreatedAt:   b.CreatedAt,
			CompletedAt: b.CompletedAt,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *BackupHandler) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := h.backupService.CreateBackup(context.Background(), "manual"); err != nil {
			// Error already logged by service
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"message": "backup creation started"})
}

func (h *BackupHandler) handleRestore(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	backupID := ""
	for i, p := range parts {
		if p == "backups" && i+1 < len(parts) {
			backupID = parts[i+1]
			break
		}
	}

	if backupID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid backup id"})
		return
	}

	if err := h.backupService.RestoreBackup(r.Context(), backupID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "restore initiated"})
}

func (h *BackupHandler) handleDownload(w http.ResponseWriter, r *http.Request, user any) {
	parts := strings.Split(r.URL.Path, "/")
	backupID := ""
	for i, p := range parts {
		if p == "backups" && i+1 < len(parts) {
			backupID = parts[i+1]
			break
		}
	}

	if backupID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid backup id"})
		return
	}

	backup, err := h.backupService.GetBackup(r.Context(), backupID)
	if err != nil || backup == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "backup not found"})
		return
	}

	backupPath := filepath.Join("data/backups", filepath.Base(backup.Filename))
	if _, err := os.Stat(backupPath); err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "backup file not found"})
		return
	}

	safeFilename := strings.ReplaceAll(filepath.Base(backup.Filename), "\"", "")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+safeFilename+"\"")
	http.ServeFile(w, r, backupPath)
}

func (h *BackupHandler) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	backupID := ""
	for i, p := range parts {
		if p == "backups" && i+1 < len(parts) {
			backupID = parts[i+1]
			break
		}
	}

	if backupID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid backup id"})
		return
	}

	if err := h.backupService.DeleteBackup(r.Context(), backupID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "backup deleted"})
}

func (h *BackupHandler) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	backups, err := h.backupService.GetBackups(r.Context(), 1000, 0)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch backup status"})
		return
	}

	status := &BackupStatusResponse{
		WorkerRunning:    true,
		RetentionDays:    30,
		TotalBackups:     len(backups),
		CompletedBackups: 0,
		FailedBackups:    0,
	}

	var latestBackup *time.Time
	for _, b := range backups {
		if b.Status == "completed" {
			status.CompletedBackups++
			if latestBackup == nil || b.CreatedAt.After(*latestBackup) {
				latestBackup = &b.CreatedAt
				status.LastBackupSize = &b.SizeBytes
			}
		} else if b.Status == "failed" {
			status.FailedBackups++
		}
	}

	if latestBackup != nil {
		age := int64(time.Since(*latestBackup).Minutes())
		status.LatestBackupAge = &age
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}
