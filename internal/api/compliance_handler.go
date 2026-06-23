package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// ComplianceHandler serves the /api/compliance/* family of endpoints.
type ComplianceHandler struct {
	engine   *compliance.Engine
	reporter *compliance.Reporter
	config   *config.Config
}

// NewComplianceHandler creates the handler.
func NewComplianceHandler(
	engine *compliance.Engine,
	reporter *compliance.Reporter,
	_ interface{}, // reserved for future SecretLister injection
	cfg *config.Config,
) *ComplianceHandler {
	return &ComplianceHandler{
		engine:   engine,
		reporter: reporter,
		config:   cfg,
	}
}

// ServeHTTP routes /api/compliance/* requests.
func (h *ComplianceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/compliance")
	path = strings.TrimPrefix(path, "/")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	switch {
	case path == "summary":
		h.handleSummary(w, r)
	case path == "secrets":
		h.handleSecretsList(w, r)
	case path == "export":
		h.handleExport(w, r)
	case strings.HasPrefix(path, "secrets/"):
		name := strings.TrimPrefix(path, "secrets/")
		h.handleSecretDetail(w, r, name)
	case strings.HasPrefix(path, "reports/"):
		kind := strings.TrimPrefix(path, "reports/")
		h.handleReport(w, r, kind)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// ── Summary ────────────────────────────────────────────────────────────────────

func (h *ComplianceHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	inputs := h.secretInputs()
	records := h.engine.EvaluateAll(r.Context(), inputs)
	summary := compliance.Summarize(records)
	_ = json.NewEncoder(w).Encode(summary)
}

// ── Secrets list (paginated, filterable) ─────────────────────────────────────

func (h *ComplianceHandler) handleSecretsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()
	statusFilter := q.Get("status")
	providerFilter := q.Get("provider")
	search := strings.ToLower(q.Get("search"))
	page := parseIntParam(q.Get("page"), 1)
	pageSize := parseIntParam(q.Get("pageSize"), 50)
	if pageSize > 200 {
		pageSize = 200
	}

	inputs := h.secretInputs()
	records := h.engine.EvaluateAll(r.Context(), inputs)

	// Filter
	var filtered []compliance.SecretCompliance
	for _, c := range records {
		if statusFilter != "" && string(c.OverallStatus) != statusFilter {
			continue
		}
		if providerFilter != "" && c.Provider != providerFilter {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(c.SecretName), search) {
			continue
		}
		filtered = append(filtered, c)
	}

	// Paginate
	total := len(filtered)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	page_records := filtered[start:end]

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
		"items":    page_records,
	})
}

// ── Per-secret detail ─────────────────────────────────────────────────────────

func (h *ComplianceHandler) handleSecretDetail(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Content-Type", "application/json")
	input := h.secretInputByName(name)
	if input == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "secret not found"})
		return
	}
	c := h.engine.Evaluate(r.Context(), *input)
	_ = json.NewEncoder(w).Encode(buildSecretDetailResponse(c))
}

func buildSecretDetailResponse(c compliance.SecretCompliance) map[string]interface{} {
	return map[string]interface{}{
		"rotationStatus": string(c.RotationStatus),
		"openDrift":      c.OpenDriftFindings,
		"versionCount":   c.VersionCount,
		"auditCount":     c.AuditEventCount,
		"lastRotation":   c.LastRotatedAt,
		"overallStatus":  string(c.OverallStatus),
	}
}

// ── Compliance export ─────────────────────────────────────────────────────────

func (h *ComplianceHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	inputs := h.secretInputs()
	records := h.engine.EvaluateAll(r.Context(), inputs)
	rows := compliance.ToExportRows(records)

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="compliance.csv"`)
		_ = compliance.WriteComplianceCSV(w, rows)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = compliance.WriteJSON(w, rows)
}

// ── Reports ───────────────────────────────────────────────────────────────────

func (h *ComplianceHandler) handleReport(w http.ResponseWriter, r *http.Request, kind string) {
	format := r.URL.Query().Get("format")
	ctx := r.Context()
	secretNames := h.secretNames()

	switch kind {
	case "rotation":
		rows, err := h.reporter.RotationReport(ctx, secretNames)
		if err != nil {
			h.jsonErr(w, err)
			return
		}
		if format == "csv" {
			h.csvResponse(w, "rotation_report.csv", func() error { return compliance.WriteRotationCSV(w, rows) })
			return
		}
		h.jsonResponse(w, rows)

	case "drift":
		rows, err := h.reporter.DriftReport(ctx)
		if err != nil {
			h.jsonErr(w, err)
			return
		}
		if format == "csv" {
			h.csvResponse(w, "drift_report.csv", func() error { return compliance.WriteDriftCSV(w, rows) })
			return
		}
		h.jsonResponse(w, rows)

	case "policy":
		rows, err := h.reporter.PolicyReport(ctx)
		if err != nil {
			h.jsonErr(w, err)
			return
		}
		if format == "csv" {
			h.csvResponse(w, "policy_report.csv", func() error { return compliance.WritePolicyCSV(w, rows) })
			return
		}
		h.jsonResponse(w, rows)

	case "activity":
		rows, err := h.reporter.ActivityReport(ctx, nil)
		if err != nil {
			h.jsonErr(w, err)
			return
		}
		if format == "csv" {
			h.csvResponse(w, "activity_report.csv", func() error { return compliance.WriteActivityCSV(w, rows) })
			return
		}
		h.jsonResponse(w, rows)

	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown report type"})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (h *ComplianceHandler) secretInputs() []compliance.SecretInput {
	if h.config == nil {
		return nil
	}
	out := make([]compliance.SecretInput, 0, len(h.config.Secrets))
	for _, s := range h.config.Secrets {
		out = append(out, compliance.SecretInput{
			Name:     s.Name,
			Provider: s.Provider,
		})
	}
	return out
}

func (h *ComplianceHandler) secretNames() []string {
	if h.config == nil {
		return nil
	}
	names := make([]string, 0, len(h.config.Secrets))
	for _, s := range h.config.Secrets {
		names = append(names, s.Name)
	}
	return names
}

func (h *ComplianceHandler) secretInputByName(name string) *compliance.SecretInput {
	if h.config == nil {
		return nil
	}
	for _, s := range h.config.Secrets {
		if s.Name == name {
			cp := compliance.SecretInput{Name: s.Name, Provider: s.Provider}
			return &cp
		}
	}
	return nil
}

func (h *ComplianceHandler) jsonResponse(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (h *ComplianceHandler) csvResponse(w http.ResponseWriter, filename string, write func() error) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_ = write()
}

func (h *ComplianceHandler) jsonErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func parseIntParam(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil && v > 0 {
		return v
	}
	return def
}
