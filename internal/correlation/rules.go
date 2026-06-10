package correlation

import (
	"time"
)

// RuleEngine manages correlation rules
type RuleEngine struct {
	rules map[string]*CorrelationRule
}

// NewRuleEngine creates a new rule engine
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		rules: make(map[string]*CorrelationRule),
	}
}

// AddRule adds a correlation rule
func (re *RuleEngine) AddRule(rule *CorrelationRule) {
	if rule == nil {
		return
	}
	re.rules[rule.ID] = rule
}

// GetRule retrieves a rule by ID
func (re *RuleEngine) GetRule(id string) *CorrelationRule {
	return re.rules[id]
}

// GetActiveRules returns all enabled rules
func (re *RuleEngine) GetActiveRules() []*CorrelationRule {
	var active []*CorrelationRule
	for _, rule := range re.rules {
		if rule.Enabled {
			active = append(active, rule)
		}
	}
	return active
}

// DefaultRules returns the default correlation rules
func DefaultRules() []*CorrelationRule {
	return []*CorrelationRule{
		// Alert grouping rule
		{
			ID:          "rule_alert_grouping",
			Name:        "Alert Grouping",
			Description: "Group alerts by resource and type within time window",
			Enabled:     true,
			Priority:    100,
			GroupBy:     []string{"alert_id", "severity"},
			TimeWindow:  5 * time.Minute,
			MinEvents:   2,
			Filters: map[string]string{
				"event_type": "alert.triggered",
			},
		},
		// Drift detection correlation
		{
			ID:          "rule_drift_correlation",
			Name:        "Drift Detection Correlation",
			Description: "Correlate drift findings by resource type",
			Enabled:     true,
			Priority:    90,
			GroupBy:     []string{"drift_type", "resource"},
			TimeWindow:  10 * time.Minute,
			MinEvents:   2,
			Filters: map[string]string{
				"event_type": "drift.detected",
			},
		},
		// Policy failure correlation
		{
			ID:          "rule_policy_failure",
			Name:        "Policy Failure Correlation",
			Description: "Group policy failures by rule and resource",
			Enabled:     true,
			Priority:    85,
			GroupBy:     []string{"policy_id", "resource_id"},
			TimeWindow:  5 * time.Minute,
			MinEvents:   2,
			Filters: map[string]string{
				"event_type": "rule.failed",
			},
		},
		// Execution failure correlation
		{
			ID:          "rule_execution_failure",
			Name:        "Execution Failure Correlation",
			Description: "Correlate execution failures by job and resource",
			Enabled:     true,
			Priority:    80,
			GroupBy:     []string{"execution_id", "status"},
			TimeWindow:  5 * time.Minute,
			MinEvents:   1,
			Filters: map[string]string{
				"event_type": "execution.failed",
			},
		},
		// Integration failure correlation
		{
			ID:          "rule_integration_failure",
			Name:        "Integration Failure Correlation",
			Description: "Group integration failures by integration type",
			Enabled:     true,
			Priority:    75,
			GroupBy:     []string{"integration_id", "error_type"},
			TimeWindow:  5 * time.Minute,
			MinEvents:   2,
			Filters: map[string]string{
				"event_type": "integration.failed",
			},
		},
		// Scheduler failure correlation
		{
			ID:          "rule_scheduler_failure",
			Name:        "Scheduler Failure Correlation",
			Description: "Correlate scheduler job failures",
			Enabled:     true,
			Priority:    70,
			GroupBy:     []string{"job_id", "failure_reason"},
			TimeWindow:  5 * time.Minute,
			MinEvents:   2,
			Filters: map[string]string{
				"event_type": "job.failed",
			},
		},
		// Backup failure correlation
		{
			ID:          "rule_backup_failure",
			Name:        "Backup Failure Correlation",
			Description: "Group backup failures by resource",
			Enabled:     true,
			Priority:    65,
			GroupBy:     []string{"backup_id", "resource_type"},
			TimeWindow:  10 * time.Minute,
			MinEvents:   1,
			Filters: map[string]string{
				"event_type": "backup.failed",
			},
		},
		// Cascading failure detection
		{
			ID:          "rule_cascading_failure",
			Name:        "Cascading Failure Detection",
			Description: "Detect multiple failures on dependent resources",
			Enabled:     true,
			Priority:    95,
			GroupBy:     []string{"affected_node", "failure_type"},
			TimeWindow:  3 * time.Minute,
			MinEvents:   3,
			Filters: map[string]string{
				"severity": "high,critical",
			},
		},
		// Time-based correlation
		{
			ID:          "rule_time_correlation",
			Name:        "Time-Based Correlation",
			Description: "Correlate events occurring simultaneously across resources",
			Enabled:     true,
			Priority:    60,
			GroupBy:     []string{"time_bucket"},
			TimeWindow:  2 * time.Minute,
			MinEvents:   3,
		},
		// Resource dependency correlation
		{
			ID:          "rule_dependency_correlation",
			Name:        "Dependency Path Correlation",
			Description: "Correlate failures along dependency graph paths",
			Enabled:     true,
			Priority:    88,
			GroupBy:     []string{"path_id", "path_failure"},
			TimeWindow:  5 * time.Minute,
			MinEvents:   2,
		},
	}
}

// MatchRule checks if an event matches a rule
func MatchRule(rule *CorrelationRule, event map[string]interface{}) bool {
	if !rule.Enabled {
		return false
	}

	// Check filters
	for filterKey, filterValue := range rule.Filters {
		eventValue, exists := event[filterKey]
		if !exists {
			return false
		}

		// Support comma-separated values in filter
		if eventValue != filterValue {
			// Check if eventValue is in the comma-separated list
			values := parseCSV(filterValue)
			if !contains(values, eventValue.(string)) {
				return false
			}
		}
	}

	return true
}

// ExtractCorrelationKey extracts correlation key from event
func ExtractCorrelationKey(rule *CorrelationRule, event map[string]interface{}) string {
	key := ""
	for _, field := range rule.GroupBy {
		if val, exists := event[field]; exists {
			if key != "" {
				key += ":"
			}
			key += valueToString(val)
		}
	}
	return key
}

// Helper functions

func parseCSV(s string) []string {
	// Simple CSV parsing - split by comma
	var result []string
	var current string
	for _, ch := range s {
		if ch == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else if ch != ' ' {
			current += string(ch)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func valueToString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
