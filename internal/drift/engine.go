package drift

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Engine orchestrates drift detection
type Engine struct {
	detectors  map[string]Detector
	findings   map[string]*DriftFinding
	store      Store
	metrics    *Metrics
	logger     *zap.Logger
	eventBus   interface{} // Can be EventBus for publishing events
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
}

// NewEngine creates a new drift detection engine
func NewEngine(store Store, logger *zap.Logger) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Engine{
		detectors: make(map[string]Detector),
		findings:  make(map[string]*DriftFinding),
		store:     store,
		metrics:   NewMetrics(),
		logger:    logger,
		done:      make(chan struct{}),
	}
}

// Initialize starts the drift detection engine
func (e *Engine) Initialize(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)

	// Load findings from storage
	findings, err := e.store.ListFindings(e.ctx)
	if err != nil {
		e.logger.Error("failed to load findings", zap.Error(err))
	} else {
		for _, finding := range findings {
			e.findings[finding.ID] = &finding
		}
	}

	// Start background loop for periodic scans
	go e.runLoop()

	e.logger.Info("Drift Detection Engine initialized", zap.Int("findings", len(e.findings)))
	return nil
}

// Shutdown gracefully stops the engine
func (e *Engine) Shutdown(ctx context.Context) error {
	if e.cancel != nil {
		e.cancel()
	}

	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		e.logger.Warn("drift engine shutdown timeout")
	}

	return nil
}

// SetEventBus sets the event bus for publishing events
func (e *Engine) SetEventBus(eventBus interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventBus = eventBus
}

// RegisterDetector registers a drift detector
func (e *Engine) RegisterDetector(detector Detector) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.detectors[detector.ID()]; exists {
		return fmt.Errorf("detector already registered: %s", detector.ID())
	}

	e.detectors[detector.ID()] = detector
	e.logger.Info("Detector registered", zap.String("detector_id", detector.ID()), zap.String("name", detector.Name()))
	return nil
}

// RunScan runs a complete drift detection scan
func (e *Engine) RunScan(ctx context.Context) error {
	e.mu.RLock()
	detectors := make([]Detector, 0, len(e.detectors))
	for _, d := range e.detectors {
		detectors = append(detectors, d)
	}
	e.mu.RUnlock()

	startTime := time.Now()
	totalFindings := 0

	for _, detector := range detectors {
		findings, err := e.runDetector(detector)
		if err != nil {
			e.logger.Error("detector execution failed",
				zap.String("detector_id", detector.ID()),
				zap.Error(err))
			continue
		}

		for _, finding := range findings {
			e.recordFinding(finding)
			totalFindings++
		}
	}

	duration := time.Since(startTime)
	e.metrics.RecordScan(duration, true)

	// Persist scan record
	e.store.LogScan(ctx, &DriftScan{
		ID:            fmt.Sprintf("scan_%d", time.Now().UnixNano()),
		DetectorID:    "all",
		FindingsCount: totalFindings,
		Duration:      duration,
		Success:       true,
		CreatedAt:     time.Now(),
	})

	e.publishEvent("DriftDetected", map[string]interface{}{
		"findings_count": totalFindings,
		"duration_ms":    duration.Milliseconds(),
	})

	return nil
}

// runDetector runs a single detector
func (e *Engine) runDetector(detector Detector) ([]DriftFinding, error) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("detector panic",
				zap.String("detector_id", detector.ID()),
				zap.Any("panic", r))
		}
	}()

	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()

	return detector.Detect(ctx)
}

// ListFindings returns all findings
func (e *Engine) ListFindings() []*DriftFinding {
	e.mu.RLock()
	defer e.mu.RUnlock()

	findings := make([]*DriftFinding, 0, len(e.findings))
	for _, finding := range e.findings {
		findings = append(findings, finding)
	}
	return findings
}

// GetFinding returns a specific finding
func (e *Engine) GetFinding(findingID string) *DriftFinding {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.findings[findingID]
}

// AcknowledgeFinding acknowledges a finding
func (e *Engine) AcknowledgeFinding(findingID string) error {
	e.mu.Lock()
	finding, exists := e.findings[findingID]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("finding not found: %s", findingID)
	}

	now := time.Now()
	finding.Status = StatusAcknowledged
	finding.AcknowledgedAt = &now

	e.metrics.RecordAcknowledgment()
	e.publishEvent("DriftAcknowledged", map[string]interface{}{
		"finding_id": findingID,
	})

	return e.store.UpdateFinding(e.ctx, *finding)
}

// ResolveFinding resolves a finding
func (e *Engine) ResolveFinding(findingID string) error {
	e.mu.Lock()
	finding, exists := e.findings[findingID]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("finding not found: %s", findingID)
	}

	now := time.Now()
	finding.Status = StatusResolved
	finding.ResolvedAt = &now

	e.metrics.RecordResolution()
	e.publishEvent("DriftResolved", map[string]interface{}{
		"finding_id": findingID,
	})

	return e.store.UpdateFinding(e.ctx, *finding)
}

// GetMetrics returns engine metrics
func (e *Engine) GetMetrics() *DriftMetrics {
	return e.metrics.GetMetrics()
}

// recordFinding upserts a finding. If a finding with the same ID already exists:
//   - acknowledged or resolved: skip (don't re-open what the operator closed)
//   - detected: refresh description/metadata while preserving original DetectedAt
//
// New findings are persisted; updates are written back to the store.
func (e *Engine) recordFinding(finding DriftFinding) {
	if finding.ID == "" {
		finding.ID = fmt.Sprintf("drift_%d", time.Now().UnixNano())
	}
	if finding.DetectedAt.IsZero() {
		finding.DetectedAt = time.Now()
	}

	e.mu.Lock()
	existing, exists := e.findings[finding.ID]
	if exists {
		if existing.Status == StatusAcknowledged || existing.Status == StatusResolved {
			// Operator closed this finding; don't re-open on rescan
			e.mu.Unlock()
			return
		}
		// Refresh description/metadata but keep the original DetectedAt
		finding.DetectedAt = existing.DetectedAt
		e.findings[finding.ID] = &finding
		e.mu.Unlock()
		_ = e.store.UpdateFinding(e.ctx, finding)
		return
	}
	e.findings[finding.ID] = &finding
	e.mu.Unlock()

	e.metrics.RecordFinding(finding)
	if err := e.store.CreateFinding(e.ctx, finding); err != nil {
		e.logger.Error("failed to persist finding", zap.Error(err))
	}
}

// GetOpenCount returns the number of findings with status StatusDetected.
func (e *Engine) GetOpenCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	n := 0
	for _, f := range e.findings {
		if f.Status == StatusDetected {
			n++
		}
	}
	return n
}

// runLoop is the background drift detection loop
func (e *Engine) runLoop() {
	defer close(e.done)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			if err := e.RunScan(e.ctx); err != nil {
				e.logger.Error("drift scan failed", zap.Error(err))
			}
		}
	}
}

// publishEvent publishes a drift event
func (e *Engine) publishEvent(eventType string, data map[string]interface{}) {
	e.mu.RLock()
	eventBus := e.eventBus
	e.mu.RUnlock()

	if eventBus == nil {
		return
	}

	if bus, ok := eventBus.(interface{ Publish(string, map[string]interface{}) }); ok {
		bus.Publish(eventType, data)
	}
}

// Store interface for drift persistence
type Store interface {
	CreateFinding(ctx context.Context, finding DriftFinding) error
	UpdateFinding(ctx context.Context, finding DriftFinding) error
	GetFinding(ctx context.Context, id string) (*DriftFinding, error)
	ListFindings(ctx context.Context) ([]DriftFinding, error)
	DeleteFinding(ctx context.Context, id string) error
	LogScan(ctx context.Context, scan *DriftScan) error
	GetScans(ctx context.Context, limit int) ([]*DriftScan, error)
	CleanupOldFindings(ctx context.Context, olderThan time.Time) error
}
