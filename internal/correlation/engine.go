package correlation

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Engine implements the correlation engine
type Engine struct {
	mu            sync.RWMutex
	logger        *zap.Logger
	store         Store
	rules         *RuleEngine
	metrics       *Metrics
	eventBus      interface{}
	active        bool
	stopChan      chan struct{}
	eventQueue    chan map[string]interface{}
	incidents     map[string]*Incident
	correlations  map[string]*CorrelationBucket
	cleanupTicker *time.Ticker
}

// CorrelationBucket groups related events
type CorrelationBucket struct {
	Rule           *CorrelationRule
	CorrelationKey string
	Events         []map[string]interface{}
	LastSeen       time.Time
	IncidentID     string
}

// NewEngine creates a new correlation engine
func NewEngine(logger *zap.Logger, store Store) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}

	if store == nil {
		store = NewInMemoryStore()
	}

	return &Engine{
		logger:       logger,
		store:        store,
		rules:        NewRuleEngine(),
		metrics:      NewMetrics(),
		stopChan:     make(chan struct{}),
		eventQueue:   make(chan map[string]interface{}, 1000),
		incidents:    make(map[string]*Incident),
		correlations: make(map[string]*CorrelationBucket),
	}
}

// Initialize initializes the correlation engine
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
	e.logger.Info("Correlation engine initialized")

	// Start background workers
	go e.processEventLoop()
	go e.cleanupLoop()

	return nil
}

// Shutdown gracefully shuts down the correlation engine
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

	e.logger.Info("Correlation engine shutdown complete")
	return nil
}

// SetEventBus sets the event bus for publishing events
func (e *Engine) SetEventBus(eventBus interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventBus = eventBus
}

// Correlate processes an event for correlation
func (e *Engine) Correlate(event map[string]interface{}) error {
	e.mu.RLock()
	if !e.active {
		e.mu.RUnlock()
		return fmt.Errorf("engine not active")
	}
	e.mu.RUnlock()

	e.metrics.RecordEventProcessed()

	// Non-blocking send to event queue
	select {
	case e.eventQueue <- event:
	default:
		e.logger.Warn("Event queue full, dropping event")
	}

	return nil
}

// processEventLoop processes events from the queue
func (e *Engine) processEventLoop() {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic in event loop", zap.Any("recover", r))
		}
	}()

	for {
		select {
		case <-e.stopChan:
			return
		case event := <-e.eventQueue:
			e.processEvent(event)
		}
	}
}

// processEvent processes a single event
func (e *Engine) processEvent(event map[string]interface{}) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic processing event", zap.Any("recover", r))
		}
	}()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Find matching rules
	for _, rule := range e.rules.GetActiveRules() {
		if !MatchRule(rule, event) {
			continue
		}

		// Extract correlation key
		key := ExtractCorrelationKey(rule, event)
		bucketKey := rule.ID + ":" + key

		// Get or create correlation bucket
		bucket, exists := e.correlations[bucketKey]
		if !exists {
			bucket = &CorrelationBucket{
				Rule:           rule,
				CorrelationKey: key,
				Events:         make([]map[string]interface{}, 0),
			}
			e.correlations[bucketKey] = bucket
		}

		bucket.Events = append(bucket.Events, event)
		bucket.LastSeen = time.Now()

		// Check if we should create/merge incident
		if len(bucket.Events) >= rule.MinEvents {
			e.createOrMergeIncident(bucket)
		}
	}
}

// createOrMergeIncident creates or merges incidents from a correlation bucket
func (e *Engine) createOrMergeIncident(bucket *CorrelationBucket) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic creating incident", zap.Any("recover", r))
		}
	}()

	if bucket.IncidentID != "" {
		// Incident already exists, merge new events
		incident := e.incidents[bucket.IncidentID]
		if incident != nil {
			e.mergeEventIntoIncident(incident, bucket)
		}
		return
	}

	// Create new incident
	incident := e.buildIncident(bucket)
	incident.ID = uuid.New().String()
	incident.FirstSeen = time.Now()
	incident.LastSeen = time.Now()

	e.incidents[incident.ID] = incident
	bucket.IncidentID = incident.ID

	// Save to store
	if err := e.store.SaveIncident(incident); err != nil {
		e.logger.Error("Failed to save incident", zap.Error(err))
	}

	e.metrics.RecordIncident(incident.Severity)
	e.metrics.RecordCorrelationScore(incident.CorrelationScore)

	e.publishEvent("IncidentCreated", map[string]interface{}{
		"incident_id": incident.ID,
		"title":       incident.Title,
		"severity":    incident.Severity,
	})

	e.logger.Info("Incident created",
		zap.String("id", incident.ID),
		zap.String("title", incident.Title),
		zap.String("severity", string(incident.Severity)),
	)
}

// mergeEventIntoIncident merges new events into an existing incident
func (e *Engine) mergeEventIntoIncident(incident *Incident, bucket *CorrelationBucket) {
	incident.LastSeen = time.Now()
	incident.EventCount += len(bucket.Events)

	// Update correlation score
	incident.CorrelationScore = calculateCorrelationScore(incident, bucket)

	// Add new events
	for _, event := range bucket.Events {
		if eventID, ok := event["event_id"].(string); ok {
			incident.RelatedEvents = append(incident.RelatedEvents, eventID)
		}
	}

	// Update store
	if err := e.store.UpdateIncident(incident); err != nil {
		e.logger.Error("Failed to update incident", zap.Error(err))
	}

	e.metrics.RecordMerge()

	e.publishEvent("IncidentUpdated", map[string]interface{}{
		"incident_id": incident.ID,
		"event_count": incident.EventCount,
	})
}

// buildIncident builds an incident from a correlation bucket
func (e *Engine) buildIncident(bucket *CorrelationBucket) *Incident {
	incident := &Incident{
		Title:         fmt.Sprintf("%s: %d events", bucket.Rule.Name, len(bucket.Events)),
		Severity:      extractSeverity(bucket.Events),
		Status:        StatusOpen,
		RootCause:     extractRootCause(bucket.Events),
		AffectedNodes: extractAffectedNodes(bucket.Events),
		RelatedEvents: extractEventIDs(bucket.Events),
		EventCount:    len(bucket.Events),
		Metadata:      extractMetadata(bucket.Events),
	}

	incident.CorrelationScore = calculateCorrelationScore(incident, bucket)

	return incident
}

// AcknowledgeIncident marks an incident as acknowledged
func (e *Engine) AcknowledgeIncident(incidentID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	incident, exists := e.incidents[incidentID]
	if !exists {
		return fmt.Errorf("incident not found: %s", incidentID)
	}

	now := time.Now()
	incident.AcknowledgedAt = &now
	incident.Status = StatusAcknowledged

	if err := e.store.UpdateIncident(incident); err != nil {
		return err
	}

	e.metrics.RecordAcknowledged()

	e.publishEvent("IncidentUpdated", map[string]interface{}{
		"incident_id": incidentID,
		"status":      incident.Status,
	})

	return nil
}

// ResolveIncident marks an incident as resolved
func (e *Engine) ResolveIncident(incidentID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	incident, exists := e.incidents[incidentID]
	if !exists {
		return fmt.Errorf("incident not found: %s", incidentID)
	}

	now := time.Now()
	incident.ResolvedAt = &now
	incident.Status = StatusResolved

	if err := e.store.UpdateIncident(incident); err != nil {
		return err
	}

	e.metrics.RecordResolved()

	e.publishEvent("IncidentResolved", map[string]interface{}{
		"incident_id": incidentID,
		"duration":    incident.Duration().Seconds(),
	})

	return nil
}

// ListIncidents lists incidents by status
func (e *Engine) ListIncidents(status IncidentStatus, limit int) ([]*Incident, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.ListIncidents(status, limit)
}

// GetIncident retrieves a specific incident
func (e *Engine) GetIncident(id string) (*Incident, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.GetIncident(id)
}

// GetMetrics returns engine metrics
func (e *Engine) GetMetrics() *IncidentMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.metrics.GetMetrics()
}

// cleanupLoop periodically cleans up old incidents
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

// cleanup removes old incidents and events
func (e *Engine) cleanup() {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic during cleanup", zap.Any("recover", r))
		}
	}()

	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().Add(-90 * 24 * time.Hour) // 90 day retention

	if err := e.store.CleanupOldIncidents(cutoff); err != nil {
		e.logger.Error("Failed to cleanup old incidents", zap.Error(err))
	}

	if err := e.store.CleanupOldEvents(cutoff); err != nil {
		e.logger.Error("Failed to cleanup old events", zap.Error(err))
	}
}

// publishEvent publishes a correlation event
func (e *Engine) publishEvent(eventType string, data map[string]interface{}) {
	if e.eventBus == nil {
		return
	}

	if bus, ok := e.eventBus.(interface{ Publish(string, map[string]interface{}) }); ok {
		bus.Publish(eventType, data)
	}
}

// Helper functions

func extractSeverity(events []map[string]interface{}) Severity {
	maxSeverity := SeverityInfo
	for _, event := range events {
		if sev, ok := event["severity"].(string); ok {
			severity := SeverityLevel(sev)
			if severity.Score() > maxSeverity.Score() {
				maxSeverity = severity
			}
		}
	}
	return maxSeverity
}

func extractRootCause(events []map[string]interface{}) string {
	if len(events) == 0 {
		return "Unknown"
	}
	// Use first event's description as root cause
	if desc, ok := events[0]["description"].(string); ok && desc != "" {
		return desc
	}
	if msg, ok := events[0]["message"].(string); ok && msg != "" {
		return msg
	}
	return "Multiple correlated events"
}

func extractAffectedNodes(events []map[string]interface{}) []string {
	nodeMap := make(map[string]bool)
	for _, event := range events {
		if nodeID, ok := event["node_id"].(string); ok {
			nodeMap[nodeID] = true
		}
		if resourceID, ok := event["resource_id"].(string); ok {
			nodeMap[resourceID] = true
		}
	}
	var nodes []string
	for node := range nodeMap {
		nodes = append(nodes, node)
	}
	return nodes
}

func extractEventIDs(events []map[string]interface{}) []string {
	var eventIDs []string
	for _, event := range events {
		if id, ok := event["event_id"].(string); ok {
			eventIDs = append(eventIDs, id)
		}
	}
	return eventIDs
}

func extractMetadata(events []map[string]interface{}) map[string]string {
	metadata := make(map[string]string)
	for _, event := range events {
		if eventType, ok := event["event_type"].(string); ok {
			metadata["event_type"] = eventType
		}
		if source, ok := event["source"].(string); ok {
			metadata["source"] = source
		}
	}
	return metadata
}

func calculateCorrelationScore(incident *Incident, bucket *CorrelationBucket) float64 {
	score := 0.0

	// Base score on event count
	score += float64(incident.EventCount) * 0.2

	// Severity weight
	score += float64(incident.Severity.Score()) * 0.3

	// Affected nodes weight
	score += float64(len(incident.AffectedNodes)) * 0.2

	// Time window bonus
	if bucket.Rule.TimeWindow > 0 {
		score += 0.3
	}

	// Normalize to 0-100
	if score > 100 {
		score = 100
	}

	return score
}
