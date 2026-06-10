package recommendation

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Engine implements the recommendation engine
type Engine struct {
	mu            sync.RWMutex
	logger        *zap.Logger
	store         Store
	rules         *RuleEngine
	metrics       *Metrics
	eventBus      interface{}
	active        bool
	stopChan      chan struct{}
	recommendations map[string]*Recommendation
	cleanupTicker *time.Ticker
}

// NewEngine creates a new recommendation engine
func NewEngine(logger *zap.Logger, store Store) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}

	if store == nil {
		store = NewInMemoryStore()
	}

	return &Engine{
		logger:          logger,
		store:           store,
		rules:           NewRuleEngine(),
		metrics:         NewMetrics(),
		stopChan:        make(chan struct{}),
		recommendations: make(map[string]*Recommendation),
	}
}

// Initialize initializes the recommendation engine
func (e *Engine) Initialize() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.active {
		return fmt.Errorf("engine already initialized")
	}

	// Add default rules
	for _, rule := range DefaultRules() {
		e.rules.AddRule(rule)
	}

	e.active = true
	e.logger.Info("Recommendation engine initialized")

	// Start background workers
	go e.cleanupLoop()

	return nil
}

// Shutdown gracefully shuts down the recommendation engine
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.active {
		return nil
	}

	e.active = false
	close(e.stopChan)

	// Stop cleanup ticker
	if e.cleanupTicker != nil {
		e.cleanupTicker.Stop()
	}

	e.logger.Info("Recommendation engine shutdown complete")
	return nil
}

// SetEventBus sets the event bus for publishing events
func (e *Engine) SetEventBus(eventBus interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventBus = eventBus
}

// Generate generates recommendations from event data
func (e *Engine) Generate(triggerType string, data map[string]interface{}) ([]*Recommendation, error) {
	e.mu.RLock()
	if !e.active {
		e.mu.RUnlock()
		return nil, fmt.Errorf("engine not active")
	}
	e.mu.RUnlock()

	var recs []*Recommendation

	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic generating recommendations", zap.Any("recover", r))
		}
	}()

	// Find matching rules
	rules := e.rules.GetRulesByTriggerType(triggerType)
	for _, rule := range rules {
		if MatchRule(rule, data) {
			rec := e.buildRecommendation(rule, data)
			recs = append(recs, rec)
		}
	}

	// Save recommendations
	for _, rec := range recs {
		e.mu.Lock()
		e.recommendations[rec.ID] = rec
		e.mu.Unlock()

		if err := e.store.SaveRecommendation(rec); err != nil {
			e.logger.Error("Failed to save recommendation", zap.Error(err))
		}

		e.metrics.RecordRecommendation(rec.Confidence)
		e.publishEvent("RecommendationCreated", map[string]interface{}{
			"recommendation_id": rec.ID,
			"title":             rec.Title,
			"priority":          rec.Priority,
		})
	}

	return recs, nil
}

// GenerateFromIncident generates recommendations from an incident
func (e *Engine) GenerateFromIncident(incidentID, title, severity string, affectedNodes []string) ([]*Recommendation, error) {
	data := map[string]interface{}{
		"incident_id":    incidentID,
		"incident_title": title,
		"severity":       severity,
		"affected_nodes": affectedNodes,
		"event_count":    len(affectedNodes),
	}

	recs, err := e.Generate("incident", data)
	if err == nil {
		for _, rec := range recs {
			rec.IncidentID = incidentID
		}
	}

	return recs, err
}

// GenerateFromDrift generates recommendations from drift findings
func (e *Engine) GenerateFromDrift(driftID, driftType string, severity string) ([]*Recommendation, error) {
	data := map[string]interface{}{
		"drift_id":   driftID,
		"drift_type": driftType,
		"severity":   severity,
		"event_type": "drift.detected",
	}

	return e.Generate("drift", data)
}

// GenerateFromAlert generates recommendations from alerts
func (e *Engine) GenerateFromAlert(alertID, alertType string) ([]*Recommendation, error) {
	data := map[string]interface{}{
		"alert_id":   alertID,
		"alert_type": alertType,
		"event_type": "alert.triggered",
	}

	return e.Generate("alert", data)
}

// ListRecommendations lists recommendations by status
func (e *Engine) ListRecommendations(status Status, limit int) ([]*Recommendation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.ListRecommendations(status, limit)
}

// GetRecommendation retrieves a specific recommendation
func (e *Engine) GetRecommendation(id string) (*Recommendation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.GetRecommendation(id)
}

// Acknowledge marks a recommendation as acknowledged
func (e *Engine) Acknowledge(recommendationID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	rec, err := e.store.GetRecommendation(recommendationID)
	if err != nil {
		return err
	}

	if rec == nil {
		return fmt.Errorf("recommendation not found: %s", recommendationID)
	}

	now := time.Now()
	rec.AcknowledgedAt = &now
	rec.Status = StatusAcknowledged

	if err := e.store.UpdateRecommendation(rec); err != nil {
		return err
	}

	e.metrics.RecordAcknowledged()

	e.publishEvent("RecommendationAcknowledged", map[string]interface{}{
		"recommendation_id": recommendationID,
	})

	return nil
}

// Implement marks a recommendation as implemented
func (e *Engine) Implement(recommendationID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	rec, err := e.store.GetRecommendation(recommendationID)
	if err != nil {
		return err
	}

	if rec == nil {
		return fmt.Errorf("recommendation not found: %s", recommendationID)
	}

	now := time.Now()
	rec.ImplementedAt = &now
	rec.Status = StatusImplemented

	if err := e.store.UpdateRecommendation(rec); err != nil {
		return err
	}

	e.metrics.RecordImplemented()

	e.publishEvent("RecommendationImplemented", map[string]interface{}{
		"recommendation_id": recommendationID,
	})

	return nil
}

// Dismiss marks a recommendation as dismissed
func (e *Engine) Dismiss(recommendationID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	rec, err := e.store.GetRecommendation(recommendationID)
	if err != nil {
		return err
	}

	if rec == nil {
		return fmt.Errorf("recommendation not found: %s", recommendationID)
	}

	now := time.Now()
	rec.DismissedAt = &now
	rec.Status = StatusDismissed

	if err := e.store.UpdateRecommendation(rec); err != nil {
		return err
	}

	e.metrics.RecordDismissed()

	e.publishEvent("RecommendationDismissed", map[string]interface{}{
		"recommendation_id": recommendationID,
	})

	return nil
}

// GetMetrics returns engine metrics
func (e *Engine) GetMetrics() *RecommendationMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.metrics.GetMetrics()
}

// buildRecommendation builds a recommendation from a rule and data
func (e *Engine) buildRecommendation(rule *RecommendationRule, data map[string]interface{}) *Recommendation {
	rec := &Recommendation{
		ID:              uuid.New().String(),
		Title:           rule.Name,
		Description:     rule.Description,
		Priority:        PriorityFromScore(rule.Priority),
		Category:        rule.Category,
		Status:          StatusOpen,
		Confidence:      CalculateConfidence(rule, data),
		CreatedAt:       time.Now(),
		Metadata:        make(map[string]string),
	}

	// Extract resource ID if available
	if resourceID, ok := data["resource_id"].(string); ok {
		rec.ResourceID = resourceID
	}

	// Build suggested action from actions list
	if len(rule.Actions) > 0 {
		rec.SuggestedAction = rule.Actions[0]
		if len(rule.Actions) > 1 {
			for i := 1; i < len(rule.Actions); i++ {
				rec.SuggestedAction += "; " + rule.Actions[i]
			}
		}
	}

	return rec
}

// cleanupLoop periodically cleans up old recommendations
func (e *Engine) cleanupLoop() {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic in cleanup loop", zap.Any("recover", r))
		}
	}()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.cleanup()
		}
	}
}

// cleanup removes old recommendations
func (e *Engine) cleanup() {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic during cleanup", zap.Any("recover", r))
		}
	}()

	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().Add(-90 * 24 * time.Hour) // 90 day retention

	if err := e.store.CleanupOldRecommendations(cutoff); err != nil {
		e.logger.Error("Failed to cleanup old recommendations", zap.Error(err))
	}
}

// publishEvent publishes a recommendation event
func (e *Engine) publishEvent(eventType string, data map[string]interface{}) {
	if e.eventBus == nil {
		return
	}

	if bus, ok := e.eventBus.(interface{ Publish(string, map[string]interface{}) }); ok {
		bus.Publish(eventType, data)
	}
}
