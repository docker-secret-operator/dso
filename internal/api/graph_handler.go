package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/graph"
)

// GraphHandler handles dependency graph API endpoints
type GraphHandler struct {
	graph *graph.Graph
}

// NewGraphHandler creates a new graph handler
func NewGraphHandler(g *graph.Graph) *GraphHandler {
	return &GraphHandler{
		graph: g,
	}
}

// ServeHTTP routes graph API requests
func (h *GraphHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || (r.Method != http.MethodGet && user.Role != "admin") {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/graph" && r.Method == "GET":
		h.GetOverview(w, r)
	case path == "/api/graph/metrics" && r.Method == "GET":
		h.GetMetrics(w, r)
	case path == "/api/graph/components" && r.Method == "GET":
		h.GetConnectedComponents(w, r)
	case strings.HasPrefix(path, "/api/graph/path") && r.Method == "GET":
		h.FindPath(w, r)
	case strings.HasPrefix(path, "/api/graph/nodes/") && strings.HasSuffix(path, "/recommendations"):
		h.GetRecommendations(w, r)
	case strings.HasPrefix(path, "/api/graph/nodes/") && strings.HasSuffix(path, "/impact"):
		h.AnalyzeImpact(w, r)
	case strings.HasPrefix(path, "/api/graph/nodes/") && strings.HasSuffix(path, "/dependencies"):
		h.GetDependencies(w, r)
	case strings.HasPrefix(path, "/api/graph/nodes/") && strings.HasSuffix(path, "/dependents"):
		h.GetDependents(w, r)
	case strings.HasPrefix(path, "/api/graph/nodes/") && strings.HasSuffix(path, "/blast-radius"):
		h.GetBlastRadius(w, r)
	case strings.HasPrefix(path, "/api/graph/nodes/") && r.Method == "GET":
		h.GetNode(w, r)
	default:
		http.NotFound(w, r)
	}
}

// extractNodeIDFromPath extracts node ID from URL path
func extractNodeIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	nodeID := parts[4]
	if idx := strings.Index(nodeID, "/"); idx != -1 {
		nodeID = nodeID[:idx]
	}
	return nodeID
}

// NodeResponse represents a graph node in API response
type NodeResponse struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// GetOverview handles GET /api/graph
func (h *GraphHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	nodes := h.graph.ListNodes()
	metrics := h.graph.GetMetrics()

	responses := make([]NodeResponse, len(nodes))
	for i, node := range nodes {
		responses[i] = NodeResponse{
			ID:       node.ID,
			Type:     string(node.Type),
			Name:     node.Name,
			Metadata: node.Metadata,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes":   responses,
		"metrics": metrics,
	})
}

// GetNode handles GET /api/graph/nodes/:id
func (h *GraphHandler) GetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeIDFromPath(r.URL.Path)
	node := h.graph.GetNode(nodeID)
	if node == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	response := NodeResponse{
		ID:       node.ID,
		Type:     string(node.Type),
		Name:     node.Name,
		Metadata: node.Metadata,
	}

	json.NewEncoder(w).Encode(response)
}

// GetDependencies handles GET /api/graph/nodes/:id/dependencies
func (h *GraphHandler) GetDependencies(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeIDFromPath(r.URL.Path)
	deps := h.graph.GetDependencies(nodeID)

	responses := make([]NodeResponse, len(deps))
	for i, node := range deps {
		responses[i] = NodeResponse{
			ID:       node.ID,
			Type:     string(node.Type),
			Name:     node.Name,
			Metadata: node.Metadata,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"dependencies": responses,
		"count":        len(responses),
	})
}

// GetDependents handles GET /api/graph/nodes/:id/dependents
func (h *GraphHandler) GetDependents(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeIDFromPath(r.URL.Path)
	deps := h.graph.GetDependentsTransitive(nodeID)

	responses := make([]NodeResponse, len(deps))
	for i, node := range deps {
		responses[i] = NodeResponse{
			ID:       node.ID,
			Type:     string(node.Type),
			Name:     node.Name,
			Metadata: node.Metadata,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"dependents": responses,
		"count":      len(responses),
	})
}

// GetBlastRadius handles GET /api/graph/nodes/:id/blast-radius
func (h *GraphHandler) GetBlastRadius(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeIDFromPath(r.URL.Path)
	affected := h.graph.GetBlastRadius(nodeID)

	responses := make([]NodeResponse, len(affected))
	for i, node := range affected {
		responses[i] = NodeResponse{
			ID:       node.ID,
			Type:     string(node.Type),
			Name:     node.Name,
			Metadata: node.Metadata,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"affected": responses,
		"count":    len(responses),
	})
}

// FindPath handles GET /api/graph/path?source=A&target=B
func (h *GraphHandler) FindPath(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	target := r.URL.Query().Get("target")

	if source == "" || target == "" {
		http.Error(w, "Missing source or target", http.StatusBadRequest)
		return
	}

	path := h.graph.FindPath(source, target)
	if path == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path": []string{},
			"found": false,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"path":  path,
		"found": true,
		"length": len(path) - 1,
	})
}

// GetMetrics handles GET /api/graph/metrics
func (h *GraphHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.graph.GetMetrics()
	cycles := h.graph.DetectCycles()
	components := h.graph.GetConnectedComponents()
	critical := h.graph.GetCriticalNodes(5.0) // threshold of 5.0

	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_nodes":           metrics.TotalNodes,
		"total_edges":           metrics.TotalEdges,
		"average_degree":        metrics.AverageDegree,
		"max_fan_in":            metrics.MaxFanIn,
		"max_fan_out":           metrics.MaxFanOut,
		"max_depth":             metrics.MaxDepth,
		"average_path_length":   metrics.AveragePathLength,
		"cycles":                len(cycles),
		"critical_nodes":        len(critical),
		"connected_components":  len(components),
		"last_updated":          metrics.LastUpdated,
	})
}

// GetConnectedComponents handles GET /api/graph/components
func (h *GraphHandler) GetConnectedComponents(w http.ResponseWriter, r *http.Request) {
	components := h.graph.GetConnectedComponents()

	result := make([]map[string]interface{}, len(components))
	for i, comp := range components {
		nodes := make([]NodeResponse, 0)
		for _, nodeID := range comp {
			if node := h.graph.GetNode(nodeID); node != nil {
				nodes = append(nodes, NodeResponse{
					ID:   node.ID,
					Type: string(node.Type),
					Name: node.Name,
				})
			}
		}
		result[i] = map[string]interface{}{
			"id":    i,
			"nodes": nodes,
			"size":  len(comp),
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"components": result,
		"count":      len(components),
	})
}

// GetRecommendations handles GET /api/graph/nodes/:id/recommendations
func (h *GraphHandler) GetRecommendations(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeIDFromPath(r.URL.Path)
	recommendations := h.graph.GetRecommendations(nodeID)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"recommendations": recommendations,
		"count":           len(recommendations),
	})
}

// AnalyzeImpact handles GET /api/graph/nodes/:id/impact
func (h *GraphHandler) AnalyzeImpact(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeIDFromPath(r.URL.Path)
	analysis, err := h.graph.AnalyzeImpact(nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	directDeps := make([]NodeResponse, len(analysis.DirectDependents))
	for i, node := range analysis.DirectDependents {
		directDeps[i] = NodeResponse{
			ID:   node.ID,
			Type: string(node.Type),
			Name: node.Name,
		}
	}

	transitDeps := make([]NodeResponse, len(analysis.TransitiveDependents))
	for i, node := range analysis.TransitiveDependents {
		transitDeps[i] = NodeResponse{
			ID:   node.ID,
			Type: string(node.Type),
			Name: node.Name,
		}
	}

	blastRadius := make([]NodeResponse, len(analysis.BlastRadius))
	for i, node := range analysis.BlastRadius {
		blastRadius[i] = NodeResponse{
			ID:   node.ID,
			Type: string(node.Type),
			Name: node.Name,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_id":                nodeID,
		"direct_dependents":      directDeps,
		"transitive_dependents":  transitDeps,
		"blast_radius":           blastRadius,
		"criticality_score":      analysis.CriticalityScore,
		"is_in_cycle":            analysis.IsInCycle,
		"affected_count":         analysis.AffectedCount,
	})
}
