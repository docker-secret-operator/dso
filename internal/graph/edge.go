package graph

// EdgeType represents the type of relationship between nodes
type EdgeType string

const (
	EdgeDependsOn   EdgeType = "depends_on"
	EdgeUses        EdgeType = "uses"
	EdgeTriggers    EdgeType = "triggers"
	EdgeGenerates   EdgeType = "generates"
	EdgeOwns        EdgeType = "owns"
	EdgeConsumes    EdgeType = "consumes"
	EdgeProduces    EdgeType = "produces"
	EdgeProtects    EdgeType = "protects"
	EdgeNotifies    EdgeType = "notifies"
	EdgeSchedules   EdgeType = "schedules"
	EdgeMonitors    EdgeType = "monitors"
	EdgeRemediates  EdgeType = "remediates"
)

// Edge represents a directed edge between two nodes
type Edge struct {
	SourceID string
	TargetID string
	Type     EdgeType
	Weight   int // Importance and relationship strength (1-100)
}

// NewEdge creates a new graph edge with default weight
func NewEdge(sourceID, targetID string, edgeType EdgeType) *Edge {
	return &Edge{
		SourceID: sourceID,
		TargetID: targetID,
		Type:     edgeType,
		Weight:   50, // Default medium weight
	}
}

// NewWeightedEdge creates a new graph edge with specified weight
func NewWeightedEdge(sourceID, targetID string, edgeType EdgeType, weight int) *Edge {
	if weight < 1 {
		weight = 1
	}
	if weight > 100 {
		weight = 100
	}
	return &Edge{
		SourceID: sourceID,
		TargetID: targetID,
		Type:     edgeType,
		Weight:   weight,
	}
}
