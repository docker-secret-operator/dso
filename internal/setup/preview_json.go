package setup

import (
	"encoding/json"
	"fmt"
)

// JSONRenderer renders an InstallPlan as a deterministic JSON document.
// Suitable for CI pipelines, scripts, and audit logs.
// It never writes to stdout; the caller receives the JSON string.
type JSONRenderer struct{}

// Render serialises the plan to indented JSON.
func (r *JSONRenderer) Render(plan InstallPlan) (string, error) {
	doc := jsonPlanDocument{Plan: toJSONPlan(plan)}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON render failed: %w", err)
	}
	return string(out), nil
}

// ─── JSON-serialisable shadow types ──────────────────────────────────────────
// Kept separate from domain types so JSON concerns don't leak into the model.

type jsonPlanDocument struct {
	Plan jsonPlan `json:"plan"`
}

type jsonPlan struct {
	ID          string            `json:"id"`
	Version     int               `json:"version"`
	GeneratedAt string            `json:"generated_at"`
	Mode        string            `json:"mode"`
	Provider    string            `json:"provider"`
	DryRun      bool              `json:"dry_run"`
	Summary     jsonSummary       `json:"summary"`
	Directories []jsonDirectory   `json:"directories"`
	Files       []jsonFile        `json:"files"`
	Permissions []jsonPermission  `json:"permissions"`
	Services    []jsonService     `json:"services"`
	Groups      []jsonGroup       `json:"groups"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type jsonSummary struct {
	TotalOperations int   `json:"total_operations"`
	CreateCount     int   `json:"create_count"`
	ModifyCount     int   `json:"modify_count"`
	DeleteCount     int   `json:"delete_count"`
	RequiresRoot    bool  `json:"requires_root"`
	EstimatedTimeMs int64 `json:"estimated_time_ms"`
}

type jsonDirectory struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Owner     string `json:"owner,omitempty"`
	Operation string `json:"operation"`
}

type jsonFile struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Owner     string `json:"owner,omitempty"`
	Operation string `json:"operation"`
	SizeBytes int    `json:"size_bytes"`
}

type jsonPermission struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	CurrentMode string `json:"current_mode"`
	TargetMode  string `json:"target_mode"`
	Owner       string `json:"owner,omitempty"`
}

type jsonService struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Operation string `json:"operation"`
}

type jsonGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Operation string   `json:"operation"`
	Users     []string `json:"users,omitempty"`
}

// toJSONPlan converts a domain InstallPlan to its JSON representation.
// Arrays are always initialised to empty slices so JSON output has []
// instead of null when there are no operations.
func toJSONPlan(plan InstallPlan) jsonPlan {
	jp := jsonPlan{
		ID:       plan.ID,
		Version:  plan.Version,
		Mode:     string(plan.Mode),
		Provider: plan.Provider,
		DryRun:   plan.DryRun,
		Summary: jsonSummary{
			TotalOperations: plan.Summary.TotalOperations,
			CreateCount:     plan.Summary.CreateCount,
			ModifyCount:     plan.Summary.ModifyCount,
			DeleteCount:     plan.Summary.DeleteCount,
			RequiresRoot:    plan.Summary.RequiresRoot,
			EstimatedTimeMs: plan.Summary.EstimatedTime.Milliseconds(),
		},
		Metadata:    plan.Metadata,
		Directories: []jsonDirectory{},
		Files:       []jsonFile{},
		Permissions: []jsonPermission{},
		Services:    []jsonService{},
		Groups:      []jsonGroup{},
	}

	if !plan.Timestamp.IsZero() {
		jp.GeneratedAt = plan.Timestamp.UTC().Format("2006-01-02T15:04:05Z")
	}

	for _, d := range plan.Directories {
		jp.Directories = append(jp.Directories, jsonDirectory{
			ID:        d.ID,
			Path:      d.Path,
			Mode:      fmt.Sprintf("%04o", d.Mode),
			Owner:     d.Owner,
			Operation: d.Operation,
		})
	}
	for _, f := range plan.Files {
		jp.Files = append(jp.Files, jsonFile{
			ID:        f.ID,
			Path:      f.Path,
			Mode:      fmt.Sprintf("%04o", f.Mode),
			Owner:     f.Owner,
			Operation: f.Operation,
			SizeBytes: len(f.Content),
		})
	}
	for _, p := range plan.Permissions {
		jp.Permissions = append(jp.Permissions, jsonPermission{
			ID:          p.ID,
			Path:        p.Path,
			CurrentMode: fmt.Sprintf("%04o", p.Current),
			TargetMode:  fmt.Sprintf("%04o", p.Target),
			Owner:       p.Owner,
		})
	}
	for _, s := range plan.Services {
		jp.Services = append(jp.Services, jsonService{
			ID:        s.ID,
			Name:      s.Name,
			Operation: s.Operation,
		})
	}
	for _, g := range plan.Groups {
		jp.Groups = append(jp.Groups, jsonGroup{
			ID:        g.ID,
			Name:      g.Name,
			Operation: g.Operation,
			Users:     g.Users,
		})
	}

	return jp
}
