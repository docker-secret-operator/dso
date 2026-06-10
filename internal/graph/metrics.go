package graph

import (
	"sync"
	"time"
)

// GraphMetrics tracks graph metrics
type GraphMetrics struct {
	TotalNodes         int
	TotalEdges         int
	AverageDegree      float64
	MaxFanIn           int
	MaxFanOut          int
	MaxDepth           int
	AveragePathLength  float64
	Cycles             int
	CriticalNodes      int
	ConnectedComponents int
	CreatedAt          time.Time
	LastUpdated        *time.Time
}

// Metrics tracks graph metrics internally
type Metrics struct {
	mu                  sync.RWMutex
	totalNodes          int
	totalEdges          int
	maxFanIn            int
	maxFanOut           int
	maxDepth            int
	averagePathLength   float64
	cycles              int
	criticalNodes       int
	connectedComponents int
	lastUpdated         *time.Time
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{}
}

// RecordNodeAdded records a node being added
func (m *Metrics) RecordNodeAdded() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalNodes++
	now := time.Now()
	m.lastUpdated = &now
}

// RecordNodeRemoved records a node being removed
func (m *Metrics) RecordNodeRemoved() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.totalNodes > 0 {
		m.totalNodes--
	}
	now := time.Now()
	m.lastUpdated = &now
}

// RecordEdgeAdded records an edge being added
func (m *Metrics) RecordEdgeAdded() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalEdges++
	now := time.Now()
	m.lastUpdated = &now
}

// RecordEdgeRemoved records an edge being removed
func (m *Metrics) RecordEdgeRemoved() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.totalEdges > 0 {
		m.totalEdges--
	}
	now := time.Now()
	m.lastUpdated = &now
}

// RecordCycleDetected records cycle detection
func (m *Metrics) RecordCycleDetected(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cycles = count
	now := time.Now()
	m.lastUpdated = &now
}

// RecordCriticalNodes records critical node count
func (m *Metrics) RecordCriticalNodes(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.criticalNodes = count
	now := time.Now()
	m.lastUpdated = &now
}

// RecordMaxFanIn records maximum fan-in
func (m *Metrics) RecordMaxFanIn(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxFanIn = count
	now := time.Now()
	m.lastUpdated = &now
}

// RecordMaxFanOut records maximum fan-out
func (m *Metrics) RecordMaxFanOut(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxFanOut = count
	now := time.Now()
	m.lastUpdated = &now
}

// RecordMaxDepth records maximum depth
func (m *Metrics) RecordMaxDepth(depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxDepth = depth
	now := time.Now()
	m.lastUpdated = &now
}

// RecordAveragePathLength records average path length
func (m *Metrics) RecordAveragePathLength(length float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.averagePathLength = length
	now := time.Now()
	m.lastUpdated = &now
}

// RecordConnectedComponents records connected components count
func (m *Metrics) RecordConnectedComponents(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectedComponents = count
	now := time.Now()
	m.lastUpdated = &now
}

// GetMetrics returns current metrics
func (m *Metrics) GetMetrics(nodeCount, edgeCount int) *GraphMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgDegree := 0.0
	if nodeCount > 0 {
		avgDegree = float64(edgeCount) / float64(nodeCount)
	}

	return &GraphMetrics{
		TotalNodes:          nodeCount,
		TotalEdges:          edgeCount,
		AverageDegree:       avgDegree,
		MaxFanIn:            m.maxFanIn,
		MaxFanOut:           m.maxFanOut,
		MaxDepth:            m.maxDepth,
		AveragePathLength:   m.averagePathLength,
		Cycles:              m.cycles,
		CriticalNodes:       m.criticalNodes,
		ConnectedComponents: m.connectedComponents,
		LastUpdated:         m.lastUpdated,
	}
}

// Reset resets metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalNodes = 0
	m.totalEdges = 0
	m.maxFanIn = 0
	m.maxFanOut = 0
	m.maxDepth = 0
	m.averagePathLength = 0
	m.cycles = 0
	m.criticalNodes = 0
	m.connectedComponents = 0
	m.lastUpdated = nil
}
