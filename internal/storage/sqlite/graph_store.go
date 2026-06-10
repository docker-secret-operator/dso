package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/graph"
)

// GraphNodeStore implements graph node persistence
type GraphNodeStore struct {
	db *sql.DB
}

// NewGraphNodeStore creates a new graph node store
func NewGraphNodeStore(db *sql.DB) *GraphNodeStore {
	return &GraphNodeStore{db: db}
}

// SaveNode saves a node to the database
func (s *GraphNodeStore) SaveNode(node *graph.Node) error {
	metadata, _ := json.Marshal(node.Metadata)

	_, err := s.db.Exec(`
		INSERT INTO graph_nodes (id, type, name, status, metadata)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			name = excluded.name,
			status = excluded.status,
			metadata = excluded.metadata,
			updated_at = CURRENT_TIMESTAMP
	`, node.ID, node.Type, node.Name, node.Status, string(metadata))

	return err
}

// GetNode retrieves a node from the database
func (s *GraphNodeStore) GetNode(nodeID string) (*graph.Node, error) {
	var node graph.Node
	var metadataJSON string

	err := s.db.QueryRow(`
		SELECT id, type, name, status, metadata FROM graph_nodes WHERE id = ?
	`, nodeID).Scan(&node.ID, &node.Type, &node.Name, &node.Status, &metadataJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("node not found: %s", nodeID)
		}
		return nil, err
	}

	node.Metadata = make(map[string]string)
	if metadataJSON != "" {
		json.Unmarshal([]byte(metadataJSON), &node.Metadata)
	}

	return &node, nil
}

// ListNodes retrieves all nodes from the database
func (s *GraphNodeStore) ListNodes() ([]*graph.Node, error) {
	rows, err := s.db.Query(`
		SELECT id, type, name, status, metadata FROM graph_nodes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*graph.Node
	for rows.Next() {
		var node graph.Node
		var metadataJSON string

		if err := rows.Scan(&node.ID, &node.Type, &node.Name, &node.Status, &metadataJSON); err != nil {
			return nil, err
		}

		node.Metadata = make(map[string]string)
		if metadataJSON != "" {
			json.Unmarshal([]byte(metadataJSON), &node.Metadata)
		}

		nodes = append(nodes, &node)
	}

	return nodes, rows.Err()
}

// DeleteNode deletes a node from the database
func (s *GraphNodeStore) DeleteNode(nodeID string) error {
	_, err := s.db.Exec(`DELETE FROM graph_nodes WHERE id = ?`, nodeID)
	return err
}

// GraphEdgeStore implements graph edge persistence
type GraphEdgeStore struct {
	db *sql.DB
}

// NewGraphEdgeStore creates a new graph edge store
func NewGraphEdgeStore(db *sql.DB) *GraphEdgeStore {
	return &GraphEdgeStore{db: db}
}

// SaveEdge saves an edge to the database
func (s *GraphEdgeStore) SaveEdge(edge *graph.Edge) error {
	edgeID := fmt.Sprintf("%s-%s-%s", edge.SourceID, edge.TargetID, edge.Type)

	_, err := s.db.Exec(`
		INSERT INTO graph_edges (id, source_id, target_id, type, weight)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			weight = excluded.weight
	`, edgeID, edge.SourceID, edge.TargetID, edge.Type, edge.Weight)

	return err
}

// GetEdges retrieves edges from source node
func (s *GraphEdgeStore) GetEdges(sourceID string) ([]*graph.Edge, error) {
	rows, err := s.db.Query(`
		SELECT source_id, target_id, type, weight FROM graph_edges WHERE source_id = ?
	`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*graph.Edge
	for rows.Next() {
		var edge graph.Edge
		if err := rows.Scan(&edge.SourceID, &edge.TargetID, &edge.Type, &edge.Weight); err != nil {
			return nil, err
		}
		edges = append(edges, &edge)
	}

	return edges, rows.Err()
}

// GetReverseEdges retrieves edges targeting a node
func (s *GraphEdgeStore) GetReverseEdges(targetID string) ([]*graph.Edge, error) {
	rows, err := s.db.Query(`
		SELECT source_id, target_id, type, weight FROM graph_edges WHERE target_id = ?
	`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*graph.Edge
	for rows.Next() {
		var edge graph.Edge
		if err := rows.Scan(&edge.SourceID, &edge.TargetID, &edge.Type, &edge.Weight); err != nil {
			return nil, err
		}
		edges = append(edges, &edge)
	}

	return edges, rows.Err()
}

// DeleteEdge deletes an edge from the database
func (s *GraphEdgeStore) DeleteEdge(sourceID, targetID string) error {
	_, err := s.db.Exec(`
		DELETE FROM graph_edges WHERE source_id = ? AND target_id = ?
	`, sourceID, targetID)
	return err
}

// ListAllEdges retrieves all edges from the database
func (s *GraphEdgeStore) ListAllEdges() ([]*graph.Edge, error) {
	rows, err := s.db.Query(`
		SELECT source_id, target_id, type, weight FROM graph_edges
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*graph.Edge
	for rows.Next() {
		var edge graph.Edge
		if err := rows.Scan(&edge.SourceID, &edge.TargetID, &edge.Type, &edge.Weight); err != nil {
			return nil, err
		}
		edges = append(edges, &edge)
	}

	return edges, rows.Err()
}
