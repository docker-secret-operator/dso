package graph

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Graph represents a dependency graph of DSO resources
type Graph struct {
	nodes     map[string]*Node
	edges     map[string][]*Edge
	reverseEdges map[string][]*Edge
	metrics   *Metrics
	logger    *zap.Logger
	mu        sync.RWMutex
	eventBus  interface{}
	createdAt time.Time
}

// NewGraph creates a new dependency graph
func NewGraph(logger *zap.Logger) *Graph {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Graph{
		nodes:        make(map[string]*Node),
		edges:        make(map[string][]*Edge),
		reverseEdges: make(map[string][]*Edge),
		metrics:      NewMetrics(),
		logger:       logger,
		createdAt:    time.Now(),
	}
}

// Initialize initializes the graph
func (g *Graph) Initialize() error {
	g.logger.Info("Graph initialized")
	return nil
}

// Shutdown gracefully shuts down the graph
func (g *Graph) Shutdown() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.nodes = make(map[string]*Node)
	g.edges = make(map[string][]*Edge)
	g.reverseEdges = make(map[string][]*Edge)
	g.metrics.Reset()

	g.logger.Info("Graph shutdown complete")
	return nil
}

// SetEventBus sets the event bus for publishing events
func (g *Graph) SetEventBus(eventBus interface{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.eventBus = eventBus
}

// AddNode adds a node to the graph
func (g *Graph) AddNode(node *Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.nodes[node.ID]; exists {
		return fmt.Errorf("node already exists: %s", node.ID)
	}

	g.nodes[node.ID] = node
	g.metrics.RecordNodeAdded()
	g.logger.Debug("Node added", zap.String("id", node.ID), zap.String("type", string(node.Type)))

	return nil
}

// RemoveNode removes a node from the graph
func (g *Graph) RemoveNode(nodeID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.nodes[nodeID]; !exists {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	// Remove all edges connected to this node
	delete(g.edges, nodeID)
	delete(g.reverseEdges, nodeID)

	// Remove this node from other nodes' edge lists
	for _, edgeList := range g.edges {
		for i, edge := range edgeList {
			if edge.TargetID == nodeID {
				edgeList = append(edgeList[:i], edgeList[i+1:]...)
			}
		}
	}

	for _, edgeList := range g.reverseEdges {
		for i, edge := range edgeList {
			if edge.SourceID == nodeID {
				edgeList = append(edgeList[:i], edgeList[i+1:]...)
			}
		}
	}

	delete(g.nodes, nodeID)
	g.metrics.RecordNodeRemoved()

	return nil
}

// AddEdge adds an edge between two nodes
func (g *Graph) AddEdge(edge *Edge) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.nodes[edge.SourceID]; !exists {
		return fmt.Errorf("source node not found: %s", edge.SourceID)
	}

	if _, exists := g.nodes[edge.TargetID]; !exists {
		return fmt.Errorf("target node not found: %s", edge.TargetID)
	}

	// Check if edge already exists
	for _, e := range g.edges[edge.SourceID] {
		if e.TargetID == edge.TargetID && e.Type == edge.Type {
			return fmt.Errorf("edge already exists: %s -> %s", edge.SourceID, edge.TargetID)
		}
	}

	g.edges[edge.SourceID] = append(g.edges[edge.SourceID], edge)
	g.reverseEdges[edge.TargetID] = append(g.reverseEdges[edge.TargetID], edge)
	g.metrics.RecordEdgeAdded()

	return nil
}

// RemoveEdge removes an edge from the graph
func (g *Graph) RemoveEdge(sourceID, targetID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	edgeList := g.edges[sourceID]
	for i, edge := range edgeList {
		if edge.TargetID == targetID {
			g.edges[sourceID] = append(edgeList[:i], edgeList[i+1:]...)
			g.metrics.RecordEdgeRemoved()
			return nil
		}
	}

	return fmt.Errorf("edge not found: %s -> %s", sourceID, targetID)
}

// GetNode returns a node by ID
func (g *Graph) GetNode(nodeID string) *Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[nodeID]
}

// GetNeighbors returns direct outgoing neighbors
func (g *Graph) GetNeighbors(nodeID string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	neighbors := make([]*Node, 0)
	for _, edge := range g.edges[nodeID] {
		if node, exists := g.nodes[edge.TargetID]; exists {
			neighbors = append(neighbors, node)
		}
	}

	return neighbors
}

// GetDependents returns direct incoming neighbors (dependents)
func (g *Graph) GetDependents(nodeID string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	dependents := make([]*Node, 0)
	for _, edge := range g.reverseEdges[nodeID] {
		if node, exists := g.nodes[edge.SourceID]; exists {
			dependents = append(dependents, node)
		}
	}

	return dependents
}

// GetMetrics returns graph metrics
func (g *Graph) GetMetrics() *GraphMetrics {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.metrics.GetMetrics(len(g.nodes), len(g.edges))
}

// ListNodes returns all nodes
func (g *Graph) ListNodes() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]*Node, 0, len(g.nodes))
	for _, node := range g.nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// publishEvent publishes a graph event
func (g *Graph) publishEvent(eventType string, data map[string]interface{}) {
	g.mu.RLock()
	eventBus := g.eventBus
	g.mu.RUnlock()

	if eventBus == nil {
		return
	}

	if bus, ok := eventBus.(interface{ Publish(string, map[string]interface{}) }); ok {
		bus.Publish(eventType, data)
	}
}
