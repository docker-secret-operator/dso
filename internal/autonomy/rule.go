package autonomy

// Rule represents an autonomy rule that triggers actions
type Rule struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
	TriggerType string // recommendation, forecast, incident, drift, policy
	ActionType  ActionType
	SafetyLevel SafetyLevel
	Conditions  map[string]interface{}
	Priority    int
}

// DefaultRules returns the default autonomy rules
func DefaultRules() []*Rule {
	return []*Rule{
		// Plugin failure recovery
		{
			ID:          "rule_restart_plugin",
			Name:        "Auto-Restart Plugin",
			Description: "Automatically restart unhealthy plugins",
			Enabled:     true,
			TriggerType: "recommendation",
			ActionType:  ActionRestartPlugin,
			SafetyLevel: SafetyApprovalRequired,
			Priority:    90,
			Conditions: map[string]interface{}{
				"category": "plugin",
			},
		},
		// Drift auto-acknowledge (low severity only)
		{
			ID:          "rule_acknowledge_drift",
			Name:        "Auto-Acknowledge Drift",
			Description: "Automatically acknowledge low-severity drift",
			Enabled:     true,
			TriggerType: "drift",
			ActionType:  ActionAcknowledgeDrift,
			SafetyLevel: SafetyAutomatic,
			Priority:    50,
			Conditions: map[string]interface{}{
				"severity": "low,info",
			},
		},
		// Queue saturation - pause jobs
		{
			ID:          "rule_pause_queue_saturation",
			Name:        "Pause Jobs on Queue Saturation",
			Description: "Pause non-critical scheduler jobs when queue is saturated",
			Enabled:     true,
			TriggerType: "forecast",
			ActionType:  ActionPauseSchedulerJob,
			SafetyLevel: SafetyApprovalRequired,
			Priority:    85,
			Conditions: map[string]interface{}{
				"resource_type": "queue",
				"severity":      "high,critical",
			},
		},
		// Integration failure retry
		{
			ID:          "rule_retry_integration",
			Name:        "Retry Failed Integration",
			Description: "Automatically retry failed integration deliveries",
			Enabled:     true,
			TriggerType: "recommendation",
			ActionType:  ActionRetryIntegration,
			SafetyLevel: SafetyApprovalRequired,
			Priority:    75,
			Conditions: map[string]interface{}{
				"category": "integration",
			},
		},
		// Stale backup
		{
			ID:          "rule_run_stale_backup",
			Name:        "Run Stale Backup",
			Description: "Automatically run backup when backup is stale",
			Enabled:     true,
			TriggerType: "recommendation",
			ActionType:  ActionRunBackup,
			SafetyLevel: SafetyApprovalRequired,
			Priority:    95,
			Conditions: map[string]interface{}{
				"category": "backup",
			},
		},
		// Execution retry
		{
			ID:          "rule_retry_execution",
			Name:        "Retry Failed Execution",
			Description: "Automatically retry failed executions",
			Enabled:     true,
			TriggerType: "recommendation",
			ActionType:  ActionRetryExecution,
			SafetyLevel: SafetyApprovalRequired,
			Priority:    70,
			Conditions: map[string]interface{}{
				"resource_type": "execution",
			},
		},
		// Memory exhaustion - cleanup retention
		{
			ID:          "rule_cleanup_retention",
			Name:        "Cleanup Old Data",
			Description: "Cleanup old data when memory is running low",
			Enabled:     true,
			TriggerType: "forecast",
			ActionType:  ActionCleanupRetention,
			SafetyLevel: SafetyApprovalRequired,
			Priority:    80,
			Conditions: map[string]interface{}{
				"resource_type": "memory",
				"severity":      "high,critical",
			},
		},
		// High severity incident resolution
		{
			ID:          "rule_resolve_incident",
			Name:        "Resolve High Severity Incident",
			Description: "Automatically resolve incidents when root cause is fixed",
			Enabled:     true,
			TriggerType: "incident",
			ActionType:  ActionResolveIncident,
			SafetyLevel: SafetyManualOnly,
			Priority:    60,
			Conditions: map[string]interface{}{
				"severity": "high,critical",
			},
		},
	}
}

// RuleEngine manages autonomy rules
type RuleEngine struct {
	rules map[string]*Rule
}

// NewRuleEngine creates a new rule engine
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		rules: make(map[string]*Rule),
	}
}

// AddRule adds a rule
func (re *RuleEngine) AddRule(rule *Rule) {
	if rule == nil {
		return
	}
	re.rules[rule.ID] = rule
}

// GetRule retrieves a rule by ID
func (re *RuleEngine) GetRule(id string) *Rule {
	return re.rules[id]
}

// GetActiveRules returns all enabled rules
func (re *RuleEngine) GetActiveRules() []*Rule {
	var active []*Rule
	for _, rule := range re.rules {
		if rule.Enabled {
			active = append(active, rule)
		}
	}
	return active
}

// GetRulesByTriggerType returns rules for a specific trigger type
func (re *RuleEngine) GetRulesByTriggerType(triggerType string) []*Rule {
	var matches []*Rule
	for _, rule := range re.rules {
		if rule.Enabled && rule.TriggerType == triggerType {
			matches = append(matches, rule)
		}
	}
	return matches
}

// MatchRule checks if data matches a rule
func MatchRule(rule *Rule, data map[string]interface{}) bool {
	if !rule.Enabled {
		return false
	}

	for key, expectedVal := range rule.Conditions {
		actualVal, exists := data[key]
		if !exists {
			return false
		}

		if expectedVal != actualVal {
			return false
		}
	}

	return true
}
