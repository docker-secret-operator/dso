package graph

import (
	"fmt"
)

// ImpactAnalysis represents the impact analysis of a node change
type ImpactAnalysis struct {
	DirectDependents      []*Node
	TransitiveDependents  []*Node
	BlastRadius           []*Node
	CriticalityScore      float64
	IsInCycle             bool
	AffectedCount         int
}

// AnalyzeImpact performs impact analysis on a node
func (g *Graph) AnalyzeImpact(nodeID string) (*ImpactAnalysis, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Check if node exists
	if _, exists := g.nodes[nodeID]; !exists {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	// Get direct dependents (nodes that directly depend on this node)
	directDeps := make([]*Node, 0)
	if edges, exists := g.reverseEdges[nodeID]; exists {
		for _, edge := range edges {
			if node, exists := g.nodes[edge.SourceID]; exists {
				directDeps = append(directDeps, node)
			}
		}
	}

	// Get transitive dependents (all nodes that depend on this node)
	visited := make(map[string]bool)
	transitiveDeps := make([]*Node, 0)
	g.collectDependentsUnlocked(nodeID, visited, &transitiveDeps)

	// Check if node is in a cycle
	isInCycle := false
	cycles := g.detectCyclesUnlocked()
	for _, cycle := range cycles {
		for _, nodeInCycle := range cycle {
			if nodeInCycle == nodeID {
				isInCycle = true
				break
			}
		}
		if isInCycle {
			break
		}
	}

	// Calculate criticality score
	fanIn := float64(len(g.reverseEdges[nodeID]))
	fanOut := float64(len(g.edges[nodeID]))
	criticalityScore := (fanIn * 0.7) + (fanOut * 0.3)

	return &ImpactAnalysis{
		DirectDependents:     directDeps,
		TransitiveDependents: transitiveDeps,
		BlastRadius:          transitiveDeps,
		CriticalityScore:     criticalityScore,
		IsInCycle:            isInCycle,
		AffectedCount:        len(transitiveDeps),
	}, nil
}

// collectDependentsUnlocked recursively collects dependents without locking (assumes already locked)
func (g *Graph) collectDependentsUnlocked(nodeID string, visited map[string]bool, dependents *[]*Node) {
	if visited[nodeID] {
		return
	}

	visited[nodeID] = true

	for _, edge := range g.reverseEdges[nodeID] {
		if !visited[edge.SourceID] {
			if node, exists := g.nodes[edge.SourceID]; exists {
				*dependents = append(*dependents, node)
				g.collectDependentsUnlocked(edge.SourceID, visited, dependents)
			}
		}
	}
}

// detectCyclesUnlocked detects cycles without locking (assumes already locked)
func (g *Graph) detectCyclesUnlocked() [][]string {
	cycles := make([][]string, 0)
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for nodeID := range g.nodes {
		if !visited[nodeID] {
			g.detectCyclesDFSUnlocked(nodeID, visited, recStack, &cycles, []string{})
		}
	}

	return cycles
}

// detectCyclesDFSUnlocked performs DFS for cycle detection without locking
func (g *Graph) detectCyclesDFSUnlocked(nodeID string, visited, recStack map[string]bool, cycles *[][]string, path []string) {
	visited[nodeID] = true
	recStack[nodeID] = true
	path = append(path, nodeID)

	for _, edge := range g.edges[nodeID] {
		if !visited[edge.TargetID] {
			g.detectCyclesDFSUnlocked(edge.TargetID, visited, recStack, cycles, path)
		} else if recStack[edge.TargetID] {
			// Found a cycle
			cycleStart := -1
			for i, node := range path {
				if node == edge.TargetID {
					cycleStart = i
					break
				}
			}

			if cycleStart >= 0 {
				cycle := make([]string, len(path)-cycleStart)
				copy(cycle, path[cycleStart:])
				cycle = append(cycle, edge.TargetID)
				*cycles = append(*cycles, cycle)
			}
		}
	}

	recStack[nodeID] = false
}

// GetDependencies returns all direct and transitive dependencies of a node
func (g *Graph) GetDependencies(nodeID string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool)
	dependencies := make([]*Node, 0)

	g.collectDependencies(nodeID, visited, &dependencies)

	return dependencies
}

// collectDependencies recursively collects dependencies
func (g *Graph) collectDependencies(nodeID string, visited map[string]bool, dependencies *[]*Node) {
	if visited[nodeID] {
		return
	}

	visited[nodeID] = true

	for _, edge := range g.edges[nodeID] {
		if !visited[edge.TargetID] {
			if node, exists := g.nodes[edge.TargetID]; exists {
				*dependencies = append(*dependencies, node)
				g.collectDependencies(edge.TargetID, visited, dependencies)
			}
		}
	}
}

// GetDependents returns all nodes that depend on the given node
func (g *Graph) GetDependentsTransitive(nodeID string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool)
	dependents := make([]*Node, 0)

	g.collectDependents(nodeID, visited, &dependents)

	return dependents
}

// collectDependents recursively collects dependents
func (g *Graph) collectDependents(nodeID string, visited map[string]bool, dependents *[]*Node) {
	if visited[nodeID] {
		return
	}

	visited[nodeID] = true

	for _, edge := range g.reverseEdges[nodeID] {
		if !visited[edge.SourceID] {
			if node, exists := g.nodes[edge.SourceID]; exists {
				*dependents = append(*dependents, node)
				g.collectDependents(edge.SourceID, visited, dependents)
			}
		}
	}
}

// GetBlastRadius returns the blast radius of a node change
// (all nodes that would be affected)
func (g *Graph) GetBlastRadius(nodeID string) []*Node {
	return g.GetDependentsTransitive(nodeID)
}

// FindPath finds a path between two nodes using BFS
func (g *Graph) FindPath(sourceID, targetID string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, exists := g.nodes[sourceID]; !exists {
		return nil
	}

	if _, exists := g.nodes[targetID]; !exists {
		return nil
	}

	queue := [][]string{{sourceID}}
	visited := make(map[string]bool)
	visited[sourceID] = true

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]

		current := path[len(path)-1]
		if current == targetID {
			return path
		}

		for _, edge := range g.edges[current] {
			if !visited[edge.TargetID] {
				visited[edge.TargetID] = true
				newPath := make([]string, len(path))
				copy(newPath, path)
				newPath = append(newPath, edge.TargetID)
				queue = append(queue, newPath)
			}
		}
	}

	return nil // No path found
}

// DetectCycles detects cycles in the graph using DFS
func (g *Graph) DetectCycles() [][]string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	cycles := make([][]string, 0)
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for nodeID := range g.nodes {
		if !visited[nodeID] {
			g.detectCyclesDFS(nodeID, visited, recStack, &cycles, []string{})
		}
	}

	return cycles
}

// detectCyclesDFS performs DFS for cycle detection
func (g *Graph) detectCyclesDFS(nodeID string, visited, recStack map[string]bool, cycles *[][]string, path []string) {
	visited[nodeID] = true
	recStack[nodeID] = true
	path = append(path, nodeID)

	for _, edge := range g.edges[nodeID] {
		if !visited[edge.TargetID] {
			g.detectCyclesDFS(edge.TargetID, visited, recStack, cycles, path)
		} else if recStack[edge.TargetID] {
			// Found a cycle
			cycleStart := -1
			for i, node := range path {
				if node == edge.TargetID {
					cycleStart = i
					break
				}
			}

			if cycleStart >= 0 {
				cycle := make([]string, len(path)-cycleStart)
				copy(cycle, path[cycleStart:])
				cycle = append(cycle, edge.TargetID)
				*cycles = append(*cycles, cycle)
			}
		}
	}

	recStack[nodeID] = false
}

// GetCriticalityScore calculates criticality of a node
// based on fan-in (incoming edges) and fan-out (outgoing edges)
func (g *Graph) GetCriticalityScore(nodeID string) float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	fanIn := float64(len(g.reverseEdges[nodeID]))
	fanOut := float64(len(g.edges[nodeID]))

	// Criticality = (fanIn * 0.7) + (fanOut * 0.3)
	// More weight on incoming dependencies (fan-in)
	criticality := (fanIn * 0.7) + (fanOut * 0.3)

	return criticality
}

// GetCriticalNodes returns nodes with high criticality scores
func (g *Graph) GetCriticalNodes(threshold float64) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	critical := make([]*Node, 0)

	for nodeID, node := range g.nodes {
		score := g.getCriticalityScoreUnlocked(nodeID)
		if score >= threshold {
			critical = append(critical, node)
		}
	}

	return critical
}

// getCriticalityScoreUnlocked calculates criticality without lock (assumes already locked)
func (g *Graph) getCriticalityScoreUnlocked(nodeID string) float64 {
	fanIn := float64(len(g.reverseEdges[nodeID]))
	fanOut := float64(len(g.edges[nodeID]))
	return (fanIn * 0.7) + (fanOut * 0.3)
}

// Traverse traverses the graph using DFS
func (g *Graph) Traverse(nodeID string, visitor func(*Node) error) error {
	g.mu.RLock()
	visited := make(map[string]bool)
	g.mu.RUnlock()

	return g.traverseDFS(nodeID, visited, visitor)
}

// traverseDFS performs DFS traversal
func (g *Graph) traverseDFS(nodeID string, visited map[string]bool, visitor func(*Node) error) error {
	if visited[nodeID] {
		return nil
	}

	g.mu.RLock()
	node := g.nodes[nodeID]
	edges := g.edges[nodeID]
	g.mu.RUnlock()

	if node == nil {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	visited[nodeID] = true

	if err := visitor(node); err != nil {
		return err
	}

	for _, edge := range edges {
		if err := g.traverseDFS(edge.TargetID, visited, visitor); err != nil {
			return err
		}
	}

	return nil
}

// GetConnectedComponents returns all connected components in the graph
func (g *Graph) GetConnectedComponents() [][]string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool)
	var components [][]string

	for nodeID := range g.nodes {
		if !visited[nodeID] {
			component := []string{}
			g.dfsBuildComponent(nodeID, visited, &component)
			components = append(components, component)
		}
	}

	return components
}

func (g *Graph) dfsBuildComponent(nodeID string, visited map[string]bool, component *[]string) {
	visited[nodeID] = true
	*component = append(*component, nodeID)

	// Visit outgoing edges
	for _, edge := range g.edges[nodeID] {
		if !visited[edge.TargetID] {
			g.dfsBuildComponent(edge.TargetID, visited, component)
		}
	}

	// Visit incoming edges
	for _, edge := range g.reverseEdges[nodeID] {
		if !visited[edge.SourceID] {
			g.dfsBuildComponent(edge.SourceID, visited, component)
		}
	}
}

// GetMaxDepth calculates maximum depth from a starting node
func (g *Graph) GetMaxDepth(nodeID string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool)
	return g.dfsMaxDepth(nodeID, visited)
}

func (g *Graph) dfsMaxDepth(nodeID string, visited map[string]bool) int {
	visited[nodeID] = true
	maxDepth := 0

	for _, edge := range g.edges[nodeID] {
		neighbor := edge.TargetID
		if !visited[neighbor] {
			depth := 1 + g.dfsMaxDepth(neighbor, visited)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
	}

	return maxDepth
}

// GetAveragePathLength calculates average path length across all node pairs
func (g *Graph) GetAveragePathLength() float64 {
	g.mu.RLock()
	nodes := g.nodes
	g.mu.RUnlock()

	if len(nodes) < 2 {
		return 0
	}

	totalDistance := 0
	count := 0

	nodeIDs := make([]string, 0, len(nodes))
	for id := range nodes {
		nodeIDs = append(nodeIDs, id)
	}

	for i := 0; i < len(nodeIDs); i++ {
		for j := i + 1; j < len(nodeIDs); j++ {
			path := g.FindPath(nodeIDs[i], nodeIDs[j])
			if path != nil {
				totalDistance += len(path) - 1
				count++
			}
		}
	}

	if count == 0 {
		return 0
	}

	return float64(totalDistance) / float64(count)
}
