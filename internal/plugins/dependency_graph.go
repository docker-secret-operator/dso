package plugins

import (
	"fmt"
	"sort"
)

// DependencyError represents an error in the dependency graph
type DependencyError struct {
	PluginID string
	Message  string
}

func (e *DependencyError) Error() string {
	return fmt.Sprintf("plugin %s: %s", e.PluginID, e.Message)
}

// DependencyGraph represents plugin dependencies
type DependencyGraph struct {
	plugins map[string]Plugin
	graph   map[string][]string
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph(plugins []Plugin) *DependencyGraph {
	dg := &DependencyGraph{
		plugins: make(map[string]Plugin),
		graph:   make(map[string][]string),
	}

	for _, plugin := range plugins {
		dg.plugins[plugin.ID()] = plugin
		dg.graph[plugin.ID()] = plugin.Dependencies()
	}

	return dg
}

// Validate checks the dependency graph for errors
func (dg *DependencyGraph) Validate() error {
	// Check for cycles
	if err := dg.detectCycles(); err != nil {
		return err
	}

	// Check for missing dependencies
	if err := dg.checkMissingDependencies(); err != nil {
		return err
	}

	return nil
}

// detectCycles detects circular dependencies using DFS
func (dg *DependencyGraph) detectCycles() error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for pluginID := range dg.plugins {
		if !visited[pluginID] {
			if dg.hasCycle(pluginID, visited, recStack) {
				return &DependencyError{
					PluginID: pluginID,
					Message:  "circular dependency detected",
				}
			}
		}
	}

	return nil
}

// hasCycle checks if a node is part of a cycle
func (dg *DependencyGraph) hasCycle(pluginID string, visited, recStack map[string]bool) bool {
	visited[pluginID] = true
	recStack[pluginID] = true

	for _, dep := range dg.graph[pluginID] {
		if !visited[dep] {
			if dg.hasCycle(dep, visited, recStack) {
				return true
			}
		} else if recStack[dep] {
			return true
		}
	}

	recStack[pluginID] = false
	return false
}

// checkMissingDependencies verifies all dependencies exist
func (dg *DependencyGraph) checkMissingDependencies() error {
	for pluginID, deps := range dg.graph {
		for _, dep := range deps {
			if _, exists := dg.plugins[dep]; !exists {
				return &DependencyError{
					PluginID: pluginID,
					Message:  fmt.Sprintf("missing dependency: %s", dep),
				}
			}
		}
	}

	return nil
}

// TopologicalSort returns plugins in initialization order
func (dg *DependencyGraph) TopologicalSort() ([]string, error) {
	// Validate first
	if err := dg.Validate(); err != nil {
		return nil, err
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	// Initialize in-degree and graph
	for pluginID := range dg.plugins {
		inDegree[pluginID] = 0
		graph[pluginID] = make([]string, 0)
	}

	// Build reverse graph and calculate in-degrees
	for pluginID, deps := range dg.graph {
		for _, dep := range deps {
			graph[dep] = append(graph[dep], pluginID)
			inDegree[pluginID]++
		}
	}

	// Queue of nodes with no incoming edges
	queue := make([]string, 0)
	for pluginID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, pluginID)
		}
	}

	// Sort for consistent ordering
	sort.Strings(queue)

	result := make([]string, 0)

	for len(queue) > 0 {
		pluginID := queue[0]
		queue = queue[1:]
		result = append(result, pluginID)

		// Process neighbors
		neighbors := graph[pluginID]
		sort.Strings(neighbors)

		for _, neighbor := range neighbors {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
				sort.Strings(queue)
			}
		}
	}

	if len(result) != len(dg.plugins) {
		return nil, fmt.Errorf("dependency graph contains cycle or invalid dependencies")
	}

	return result, nil
}

// GetDependents returns plugins that depend on a given plugin
func (dg *DependencyGraph) GetDependents(pluginID string) []string {
	dependents := make([]string, 0)

	for id, deps := range dg.graph {
		for _, dep := range deps {
			if dep == pluginID {
				dependents = append(dependents, id)
				break
			}
		}
	}

	return dependents
}
