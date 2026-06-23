package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// SecretHistoryHandler serves GET /api/secrets/:name/history|timeline|diff
type SecretHistoryHandler struct {
	versions   *sqlite.SecretVersionStore
	audit      storage.AuditStore
	driftStore drift.Store
}

// NewSecretHistoryHandler creates the handler. All dependencies are optional;
// missing ones cause the related sections to return empty slices.
func NewSecretHistoryHandler(
	versions *sqlite.SecretVersionStore,
	audit storage.AuditStore,
	driftStore drift.Store,
) *SecretHistoryHandler {
	return &SecretHistoryHandler{
		versions:   versions,
		audit:      audit,
		driftStore: driftStore,
	}
}

// ServeHTTP routes /api/secrets/:name/history|timeline|diff
func (h *SecretHistoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Path is expected to start with the secret name portion, e.g. "mySecret/history"
	// The router strips "/api/secrets/" before handing off.
	path := strings.TrimPrefix(r.URL.Path, "/api/secrets/")

	var secretName, action string
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		secretName = path[:idx]
		action = path[idx+1:]
	} else {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	switch action {
	case "history":
		h.handleHistory(w, r, secretName)
	case "timeline":
		h.handleTimeline(w, r, secretName)
	case "diff":
		h.handleDiff(w, r, secretName)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// ── History ───────────────────────────────────────────────────────────────────

type versionResponse struct {
	Version        int       `json:"version"`
	CreatedAt      time.Time `json:"createdAt"`
	RotatedBy      string    `json:"rotatedBy"`
	RotationSource string    `json:"rotationSource"`
	Provider       string    `json:"provider"`
	ExecutionID    string    `json:"executionId,omitempty"`
}

func (h *SecretHistoryHandler) handleHistory(w http.ResponseWriter, r *http.Request, name string) {
	if h.versions == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"currentVersion": 0,
			"versions":       []versionResponse{},
		})
		return
	}

	versions, err := h.versions.ListBySecret(r.Context(), name)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	current := 0
	rows := make([]versionResponse, 0, len(versions))
	for _, v := range versions {
		if v.Version > current {
			current = v.Version
		}
		rows = append(rows, versionResponse{
			Version:        v.Version,
			CreatedAt:      v.CreatedAt,
			RotatedBy:      v.RotatedBy,
			RotationSource: v.RotationSource,
			Provider:       v.Provider,
			ExecutionID:    v.ExecutionID,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"currentVersion": current,
		"versions":       rows,
	})
}

// ── Timeline ──────────────────────────────────────────────────────────────────

// SecretTimelineEvent is a unified event in the secret's history.
type SecretTimelineEvent struct {
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description"`
	ExecutionID string    `json:"executionId,omitempty"`
	DriftID     string    `json:"driftId,omitempty"`
	AuditID     string    `json:"auditId,omitempty"`
	Version     int       `json:"version,omitempty"`
	Actor       string    `json:"actor,omitempty"`
	Source      string    `json:"source,omitempty"`
}

func (h *SecretHistoryHandler) handleTimeline(w http.ResponseWriter, r *http.Request, name string) {
	ctx := r.Context()
	var events []SecretTimelineEvent

	// Rotation versions
	if h.versions != nil {
		versions, _ := h.versions.ListBySecret(ctx, name)
		for _, v := range versions {
			events = append(events, SecretTimelineEvent{
				Type:        "rotation",
				Timestamp:   v.CreatedAt,
				Description: "Secret rotated",
				Version:     v.Version,
				ExecutionID: v.ExecutionID,
				Actor:       v.RotatedBy,
				Source:      v.RotationSource,
			})
		}
	}

	// Audit events referencing this secret
	if h.audit != nil {
		auditEvents, _ := h.audit.Query(ctx, map[string]interface{}{"resource_id": name})
		for _, ae := range auditEvents {
			events = append(events, SecretTimelineEvent{
				Type:        "audit",
				Timestamp:   ae.Timestamp,
				Description: ae.Action,
				AuditID:     ae.ID,
				Actor:       ae.ActorName,
			})
		}
	}

	// Drift findings where Resource matches the secret name
	if h.driftStore != nil {
		findings, _ := h.driftStore.ListFindings(ctx)
		for _, f := range findings {
			if f.Resource != name {
				continue
			}
			desc := string(f.Type) + ": " + f.Description
			events = append(events, SecretTimelineEvent{
				Type:        "drift",
				Timestamp:   f.DetectedAt,
				Description: desc,
				DriftID:     f.ID,
			})
		}
	}

	// Sort newest-first
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})

	if events == nil {
		events = []SecretTimelineEvent{}
	}
	_ = json.NewEncoder(w).Encode(events)
}

// ── Diff ──────────────────────────────────────────────────────────────────────

func (h *SecretHistoryHandler) handleDiff(w http.ResponseWriter, r *http.Request, name string) {
	if h.versions == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "version store unavailable"})
		return
	}

	v1Str := r.URL.Query().Get("v1")
	v2Str := r.URL.Query().Get("v2")
	v1, err1 := strconv.Atoi(v1Str)
	v2, err2 := strconv.Atoi(v2Str)
	if err1 != nil || err2 != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "v1 and v2 must be integers"})
		return
	}

	ctx := r.Context()
	ver1, err := h.versions.GetByVersion(ctx, name, v1)
	if err != nil || ver1 == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "version v1 not found"})
		return
	}
	ver2, err := h.versions.GetByVersion(ctx, name, v2)
	if err != nil || ver2 == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "version v2 not found"})
		return
	}

	// Count containers affected: look at drift findings between the two timestamps
	containersAffected := 0
	if h.driftStore != nil {
		findings, _ := h.driftStore.ListFindings(ctx)
		t1, t2 := ver1.CreatedAt, ver2.CreatedAt
		if t1.After(t2) {
			t1, t2 = t2, t1
		}
		for _, f := range findings {
			if f.Resource != name {
				continue
			}
			if !f.DetectedAt.Before(t1) && !f.DetectedAt.After(t2) {
				containersAffected++
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"v1":                     v1,
		"v2":                     v2,
		"providerChanged":        ver1.Provider != ver2.Provider,
		"rotationSourceChanged":  ver1.RotationSource != ver2.RotationSource,
		"executionChanged":       ver1.ExecutionID != ver2.ExecutionID,
		"hashChanged":            ver1.Hash != ver2.Hash && ver1.Hash != "" && ver2.Hash != "",
		"containersAffected":     containersAffected,
		"v1RotatedBy":            ver1.RotatedBy,
		"v2RotatedBy":            ver2.RotatedBy,
		"v1CreatedAt":            ver1.CreatedAt,
		"v2CreatedAt":            ver2.CreatedAt,
	})
}
