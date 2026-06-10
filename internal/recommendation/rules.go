package recommendation

// DefaultRules returns the default recommendation rules
func DefaultRules() []*RecommendationRule {
	return []*RecommendationRule{
		// Backup Stale
		{
			ID:          "rule_backup_stale",
			Name:        "Backup Stale",
			Description: "Recommend backup when last backup is old",
			Enabled:     true,
			Priority:    3,
			Category:    CategoryBackup,
			TriggerType: "incident",
			Actions: []string{
				"Run backup immediately",
				"Verify retention policy",
			},
			Conditions: map[string]interface{}{
				"incident_title": "backup",
			},
		},
		// Plugin Failure
		{
			ID:          "rule_plugin_failure",
			Name:        "Plugin Failure Recovery",
			Description: "Recommend plugin recovery steps",
			Enabled:     true,
			Priority:    3,
			Category:    CategoryPlugin,
			TriggerType: "plugin_failure",
			Actions: []string{
				"Restart plugin",
				"Inspect logs",
				"Disable plugin if instability persists",
			},
			Conditions: map[string]interface{}{
				"event_type": "plugin.failed",
			},
		},
		// Drift Detection
		{
			ID:          "rule_drift_detection",
			Name:        "Drift Configuration Recovery",
			Description: "Recommend drift remediation",
			Enabled:     true,
			Priority:    2,
			Category:    CategoryDrift,
			TriggerType: "drift",
			Actions: []string{
				"Acknowledge drift",
				"Restore configuration",
				"Create alert",
			},
			Conditions: map[string]interface{}{
				"severity": "medium,high,critical",
			},
		},
		// Integration Failure
		{
			ID:          "rule_integration_failure",
			Name:        "Integration Failure Recovery",
			Description: "Recommend integration troubleshooting",
			Enabled:     true,
			Priority:    3,
			Category:    CategoryIntegration,
			TriggerType: "integration_failure",
			Actions: []string{
				"Test connectivity",
				"Retry webhook",
				"Verify credentials",
			},
			Conditions: map[string]interface{}{
				"event_type": "integration.failed",
			},
		},
		// Scheduler Failure
		{
			ID:          "rule_scheduler_failure",
			Name:        "Scheduler Job Recovery",
			Description: "Recommend scheduler job recovery",
			Enabled:     true,
			Priority:    2,
			Category:    CategoryScheduler,
			TriggerType: "scheduler_failure",
			Actions: []string{
				"Run job manually",
				"Inspect execution logs",
				"Increase timeout",
			},
			Conditions: map[string]interface{}{
				"event_type": "job.failed",
			},
		},
		// Policy Failure
		{
			ID:          "rule_policy_failure",
			Name:        "Policy Review and Recovery",
			Description: "Recommend policy verification",
			Enabled:     true,
			Priority:    2,
			Category:    CategoryPolicy,
			TriggerType: "policy_failure",
			Actions: []string{
				"Review conditions",
				"Verify metrics",
				"Re-enable policy",
			},
			Conditions: map[string]interface{}{
				"event_type": "rule.failed",
			},
		},
		// High Severity Incident
		{
			ID:          "rule_high_severity_incident",
			Name:        "High Severity Incident Response",
			Description: "Recommend immediate escalation for high/critical incidents",
			Enabled:     true,
			Priority:    4,
			Category:    CategorySecurity,
			TriggerType: "incident",
			Actions: []string{
				"Escalate immediately",
				"Review affected nodes",
				"Notify operators",
				"Enable detailed logging",
			},
			Conditions: map[string]interface{}{
				"severity": "high,critical",
			},
		},
		// Performance Degradation
		{
			ID:          "rule_performance_degradation",
			Name:        "Performance Optimization",
			Description: "Recommend optimization for performance issues",
			Enabled:     true,
			Priority:    2,
			Category:    CategoryPerformance,
			TriggerType: "alert",
			Actions: []string{
				"Review resource usage",
				"Check queue depths",
				"Scale resources if needed",
			},
			Conditions: map[string]interface{}{
				"alert_type": "performance",
			},
		},
		// Security Recommendation
		{
			ID:          "rule_security_hardening",
			Name:        "Security Hardening",
			Description: "Recommend security improvements",
			Enabled:     true,
			Priority:    3,
			Category:    CategorySecurity,
			TriggerType: "incident",
			Actions: []string{
				"Review access controls",
				"Rotate credentials",
				"Enable audit logging",
			},
			Conditions: map[string]interface{}{
				"category": "security",
			},
		},
		// Cascading Failure Prevention
		{
			ID:          "rule_cascading_failure_prevention",
			Name:        "Cascading Failure Prevention",
			Description: "Recommend prevention of cascading failures",
			Enabled:     true,
			Priority:    4,
			Category:    CategoryPerformance,
			TriggerType: "incident",
			Actions: []string{
				"Isolate affected components",
				"Enable circuit breakers",
				"Review dependency graph",
				"Implement rate limiting",
			},
			Conditions: map[string]interface{}{
				"event_count": ">=3",
				"severity":    "high,critical",
			},
		},
	}
}

// RuleEngine manages recommendation rules
type RuleEngine struct {
	rules map[string]*RecommendationRule
}

// NewRuleEngine creates a new rule engine
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		rules: make(map[string]*RecommendationRule),
	}
}

// AddRule adds a recommendation rule
func (re *RuleEngine) AddRule(rule *RecommendationRule) {
	if rule == nil {
		return
	}
	re.rules[rule.ID] = rule
}

// GetRule retrieves a rule by ID
func (re *RuleEngine) GetRule(id string) *RecommendationRule {
	return re.rules[id]
}

// GetActiveRules returns all enabled rules
func (re *RuleEngine) GetActiveRules() []*RecommendationRule {
	var active []*RecommendationRule
	for _, rule := range re.rules {
		if rule.Enabled {
			active = append(active, rule)
		}
	}
	return active
}

// GetRulesByTriggerType returns rules for a specific trigger type
func (re *RuleEngine) GetRulesByTriggerType(triggerType string) []*RecommendationRule {
	var matches []*RecommendationRule
	for _, rule := range re.rules {
		if rule.Enabled && rule.TriggerType == triggerType {
			matches = append(matches, rule)
		}
	}
	return matches
}

// MatchRule checks if an event matches a rule
func MatchRule(rule *RecommendationRule, data map[string]interface{}) bool {
	if !rule.Enabled {
		return false
	}

	for key, expectedVal := range rule.Conditions {
		actualVal, exists := data[key]
		if !exists {
			return false
		}

		// Simple matching logic
		if expectedVal != actualVal {
			return false
		}
	}

	return true
}

// CalculateConfidence calculates confidence score for a recommendation
func CalculateConfidence(rule *RecommendationRule, data map[string]interface{}) float64 {
	confidence := 0.5 // Base confidence

	// Increase confidence based on matching conditions
	matchCount := 0
	for range rule.Conditions {
		matchCount++
	}

	if matchCount > 0 {
		confidence += float64(matchCount) * 0.1
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}
