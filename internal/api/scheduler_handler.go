package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/scheduler"
)

// SchedulerHandler handles scheduler API endpoints
type SchedulerHandler struct {
	scheduler *scheduler.Scheduler
}

// SchedulerResponse represents scheduler response data
type SchedulerResponse struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Enabled  bool              `json:"enabled"`
	Status   string            `json:"status"`
	NextRun  *int64            `json:"next_run"`
	LastRun  *int64            `json:"last_run"`
	Metadata map[string]string `json:"metadata"`
}

// JobExecutionResponse represents execution history
type JobExecutionResponse struct {
	ID      string `json:"id"`
	JobID   string `json:"job_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Started int64  `json:"started_at"`
}

// NewSchedulerHandler creates a new scheduler handler
func NewSchedulerHandler(sch *scheduler.Scheduler) *SchedulerHandler {
	return &SchedulerHandler{
		scheduler: sch,
	}
}

// ServeHTTP routes scheduler API requests
func (h *SchedulerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || user.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/scheduler/jobs" && r.Method == "GET":
		h.ListJobs(w, r)
	case strings.HasPrefix(path, "/api/scheduler/jobs/") && strings.HasSuffix(path, "/run") && r.Method == "POST":
		h.RunNow(w, r)
	case strings.HasPrefix(path, "/api/scheduler/jobs/") && strings.HasSuffix(path, "/pause") && r.Method == "POST":
		h.PauseJob(w, r)
	case strings.HasPrefix(path, "/api/scheduler/jobs/") && strings.HasSuffix(path, "/resume") && r.Method == "POST":
		h.ResumeJob(w, r)
	case strings.HasPrefix(path, "/api/scheduler/jobs/") && strings.HasSuffix(path, "/metrics") && r.Method == "GET":
		h.GetMetrics(w, r)
	case strings.HasPrefix(path, "/api/scheduler/jobs/") && strings.HasSuffix(path, "/history") && r.Method == "GET":
		h.GetHistory(w, r)
	case strings.HasPrefix(path, "/api/scheduler/jobs/") && r.Method == "GET":
		h.GetJob(w, r)
	case strings.HasPrefix(path, "/api/scheduler/jobs/") && r.Method == "DELETE":
		h.DeleteJob(w, r)
	case path == "/api/scheduler/metrics" && r.Method == "GET":
		h.GetSystemMetrics(w, r)
	default:
		http.NotFound(w, r)
	}
}

// extractJobIDFromPath extracts job ID from URL path
func extractJobIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		return ""
	}
	jobID := parts[4]
	// Remove any trailing path components (like /run, /pause, /metrics, etc.)
	if idx := strings.Index(jobID, "/"); idx != -1 {
		jobID = jobID[:idx]
	}
	return jobID
}

// ListJobs handles GET /api/scheduler/jobs
func (h *SchedulerHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := h.scheduler.List()
	responses := make([]SchedulerResponse, len(jobs))

	for i, job := range jobs {
		var nextRun, lastRun *int64
		if !job.NextRun.IsZero() {
			t := job.NextRun.Unix() * 1000
			nextRun = &t
		}
		if job.LastRun != nil {
			t := job.LastRun.Unix() * 1000
			lastRun = &t
		}

		responses[i] = SchedulerResponse{
			ID:       job.ID,
			Name:     job.Name,
			Type:     string(job.Type),
			Enabled:  job.Enabled,
			Status:   string(job.Status),
			NextRun:  nextRun,
			LastRun:  lastRun,
			Metadata: job.Metadata,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobs": responses,
	})
}

// GetJob handles GET /api/scheduler/jobs/:id
func (h *SchedulerHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobIDFromPath(r.URL.Path)
	job := h.scheduler.Get(jobID)
	if job == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	var nextRun, lastRun *int64
	if !job.NextRun.IsZero() {
		t := job.NextRun.Unix() * 1000
		nextRun = &t
	}
	if job.LastRun != nil {
		t := job.LastRun.Unix() * 1000
		lastRun = &t
	}

	resp := SchedulerResponse{
		ID:       job.ID,
		Name:     job.Name,
		Type:     string(job.Type),
		Enabled:  job.Enabled,
		Status:   string(job.Status),
		NextRun:  nextRun,
		LastRun:  lastRun,
		Metadata: job.Metadata,
	}

	metrics := h.scheduler.GetMetrics(jobID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job":     resp,
		"metrics": metrics,
	})
}

// RunNow handles POST /api/scheduler/jobs/:id/run
func (h *SchedulerHandler) RunNow(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobIDFromPath(r.URL.Path)
	if err := h.scheduler.RunNow(jobID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "job triggered",
		"job_id": jobID,
	})
}

// PauseJob handles POST /api/scheduler/jobs/:id/pause
func (h *SchedulerHandler) PauseJob(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobIDFromPath(r.URL.Path)
	if err := h.scheduler.Pause(jobID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "job paused",
		"job_id": jobID,
	})
}

// ResumeJob handles POST /api/scheduler/jobs/:id/resume
func (h *SchedulerHandler) ResumeJob(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobIDFromPath(r.URL.Path)
	if err := h.scheduler.Resume(jobID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "job resumed",
		"job_id": jobID,
	})
}

// DeleteJob handles DELETE /api/scheduler/jobs/:id
func (h *SchedulerHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobIDFromPath(r.URL.Path)
	if err := h.scheduler.Delete(jobID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "job deleted",
		"job_id": jobID,
	})
}

// GetMetrics handles GET /api/scheduler/jobs/:id/metrics
func (h *SchedulerHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobIDFromPath(r.URL.Path)
	metrics := h.scheduler.GetMetrics(jobID)
	if metrics == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// GetHistory handles GET /api/scheduler/jobs/:id/history
func (h *SchedulerHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	jobID := extractJobIDFromPath(r.URL.Path)
	limit := 50

	// Stub - would fetch from execution store
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"executions": []JobExecutionResponse{},
		"total":      0,
		"job_id":     jobID,
		"limit":      limit,
	})
}

// GetSystemMetrics handles GET /api/scheduler/metrics
func (h *SchedulerHandler) GetSystemMetrics(w http.ResponseWriter, r *http.Request) {
	allMetrics := h.scheduler.GetAllMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allMetrics)
}
