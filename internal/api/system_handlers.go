package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// SystemHandler provides system-level endpoints
type SystemHandler struct {
	storageProvider storage.StorageProvider
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(storageProvider storage.StorageProvider) *SystemHandler {
	return &SystemHandler{
		storageProvider: storageProvider,
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status      string    `json:"status"`
	Timestamp   time.Time `json:"timestamp"`
	Persistence struct {
		Enabled       bool      `json:"enabled"`
		Driver        string    `json:"driver"`
		Status        string    `json:"status"`
		MigrationVer  string    `json:"migration_version,omitempty"`
		DatabaseSize  string    `json:"database_size,omitempty"`
		WALMode       bool      `json:"wal_mode,omitempty"`
		LastCheckTime time.Time `json:"last_check_time"`
	} `json:"persistence"`
	Uptime string  `json:"uptime"`
	Checks []Check `json:"checks"`
}

// Check represents an individual health check
type Check struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // "ok", "warning", "error"
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

var startTime = time.Now()

// HandleHealth handles GET /api/system/health
func (h *SystemHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Uptime:    time.Since(startTime).String(),
		Checks:    []Check{},
	}

	// Check if persistence is available
	if h.storageProvider != nil {
		response.Persistence.Enabled = true
		response.Persistence.Driver = "sqlite"
		response.Persistence.LastCheckTime = time.Now()

		if err := h.storageProvider.Health(ctx); err != nil {
			response.Persistence.Status = "error"
			response.Status = "degraded"
			response.Checks = append(response.Checks, Check{
				Name:      "persistence_health",
				Status:    "error",
				Message:   err.Error(),
				Timestamp: time.Now(),
			})
		} else {
			response.Persistence.Status = "healthy"
			response.Persistence.WALMode = true
			response.Checks = append(response.Checks, Check{
				Name:      "persistence_health",
				Status:    "ok",
				Message:   "Database is healthy and WAL mode is enabled",
				Timestamp: time.Now(),
			})
		}
	} else {
		response.Persistence.Enabled = false
		response.Persistence.Status = "disabled"
		response.Checks = append(response.Checks, Check{
			Name:      "persistence_health",
			Status:    "ok",
			Message:   "Persistence disabled (ephemeral mode)",
			Timestamp: time.Now(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleHealthSimple handles GET /api/system/health/ready (simple readiness check)
func (h *SystemHandler) HandleHealthSimple(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Quick health check
	if h.storageProvider != nil {
		if err := h.storageProvider.Health(ctx); err != nil {
			http.Error(w, fmt.Sprintf(`{"ready": false, "error": "%v"}`, err), http.StatusServiceUnavailable)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ready": true}`))
}

// HandleStorageStatus handles GET /api/system/storage
func (h *SystemHandler) HandleStorageStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.storageProvider == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": false,
			"status":  "disabled",
			"message": "Persistence not enabled",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status := map[string]interface{}{
		"enabled": true,
		"driver":  "sqlite",
		"checked": time.Now(),
	}

	if err := h.storageProvider.Health(ctx); err != nil {
		status["status"] = "error"
		status["error"] = err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		status["status"] = "healthy"
		status["wal_mode"] = true
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
