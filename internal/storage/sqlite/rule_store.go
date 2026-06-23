package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/policy"
)

// RuleStore implements storage.RuleStore for policy rules
type RuleStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

// CreateRule creates a new rule
func (s *RuleStore) CreateRule(ctx context.Context, rule interface{}) error {
	r, ok := rule.(*policy.Rule)
	if !ok {
		return fmt.Errorf("invalid rule type")
	}

	conditionJSON, err := json.Marshal(r.Condition)
	if err != nil {
		return fmt.Errorf("failed to marshal condition: %w", err)
	}

	actionsJSON, err := json.Marshal(r.Actions)
	if err != nil {
		return fmt.Errorf("failed to marshal actions: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO rules (id, name, description, enabled, severity, trigger, schedule, event_type, condition_json, actions_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Description, r.Enabled, r.Severity, r.Trigger,
		r.Schedule, r.EventType, conditionJSON, actionsJSON, time.Now(), time.Now())

	return err
}

// UpdateRule updates an existing rule
func (s *RuleStore) UpdateRule(ctx context.Context, rule interface{}) error {
	r, ok := rule.(*policy.Rule)
	if !ok {
		return fmt.Errorf("invalid rule type")
	}

	conditionJSON, err := json.Marshal(r.Condition)
	if err != nil {
		return fmt.Errorf("failed to marshal condition: %w", err)
	}

	actionsJSON, err := json.Marshal(r.Actions)
	if err != nil {
		return fmt.Errorf("failed to marshal actions: %w", err)
	}

	var lastRunSQL interface{} = nil
	if r.LastRun != nil {
		lastRunSQL = *r.LastRun
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE rules SET name=?, description=?, enabled=?, severity=?, trigger=?, schedule=?, event_type=?,
		 condition_json=?, actions_json=?, last_run=?, last_result=?, updated_at=?
		 WHERE id=?`,
		r.Name, r.Description, r.Enabled, r.Severity, r.Trigger,
		r.Schedule, r.EventType, conditionJSON, actionsJSON, lastRunSQL, r.LastResult, time.Now(), r.ID)

	return err
}

// GetRule retrieves a rule by ID
func (s *RuleStore) GetRule(ctx context.Context, id string) (interface{}, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, enabled, severity, trigger, schedule, event_type,
		 condition_json, actions_json, last_run, last_result, created_at, updated_at
		 FROM rules WHERE id=?`, id)

	rule := &policy.Rule{}
	var conditionJSON, actionsJSON []byte
	var lastRun sql.NullTime
	var lastResult sql.NullString

	err := row.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Severity, &rule.Trigger,
		&rule.Schedule, &rule.EventType, &conditionJSON, &actionsJSON, &lastRun, &lastResult,
		&rule.CreatedAt, &rule.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("rule not found")
		}
		return nil, fmt.Errorf("failed to scan rule: %w", err)
	}

	if err := json.Unmarshal(conditionJSON, &rule.Condition); err != nil {
		return nil, fmt.Errorf("failed to unmarshal condition: %w", err)
	}

	if err := json.Unmarshal(actionsJSON, &rule.Actions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal actions: %w", err)
	}

	if lastRun.Valid {
		rule.LastRun = &lastRun.Time
	}

	if lastResult.Valid {
		rule.LastResult = policy.RuleResult(lastResult.String)
	}

	return rule, nil
}

// ListRules lists all rules
func (s *RuleStore) ListRules(ctx context.Context) ([]interface{}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, enabled, severity, trigger, schedule, event_type,
		 condition_json, actions_json, last_run, last_result, created_at, updated_at
		 FROM rules ORDER BY created_at DESC`)

	if err != nil {
		return nil, fmt.Errorf("failed to query rules: %w", err)
	}
	defer rows.Close()

	var rules []interface{}
	for rows.Next() {
		rule := &policy.Rule{}
		var conditionJSON, actionsJSON []byte
		var lastRun sql.NullTime
		var lastResult sql.NullString

		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Severity, &rule.Trigger,
			&rule.Schedule, &rule.EventType, &conditionJSON, &actionsJSON, &lastRun, &lastResult,
			&rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}

		if err := json.Unmarshal(conditionJSON, &rule.Condition); err != nil {
			return nil, fmt.Errorf("failed to unmarshal condition: %w", err)
		}

		if err := json.Unmarshal(actionsJSON, &rule.Actions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal actions: %w", err)
		}

		if lastRun.Valid {
			rule.LastRun = &lastRun.Time
		}

		if lastResult.Valid {
			rule.LastResult = policy.RuleResult(lastResult.String)
		}

		rules = append(rules, rule)
	}

	return rules, rows.Err()
}

// DeleteRule deletes a rule
func (s *RuleStore) DeleteRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM rules WHERE id=?", id)
	return err
}

// LogExecution logs a rule execution
func (s *RuleStore) LogExecution(ctx context.Context, execution *policy.RuleExecution) error {
	durationMs := int64(execution.Duration.Milliseconds())
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rule_executions (id, rule_id, success, duration_ms, error_message, result, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		execution.ID, execution.RuleID, execution.Success, durationMs, execution.Error, execution.Result, execution.CreatedAt)

	return err
}

// GetExecutions retrieves execution history for a rule
func (s *RuleStore) GetExecutions(ctx context.Context, ruleID string, limit int) ([]*policy.RuleExecution, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, rule_id, success, duration_ms, error_message, result, created_at
		 FROM rule_executions WHERE rule_id=?
		 ORDER BY created_at DESC LIMIT ?`, ruleID, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query executions: %w", err)
	}
	defer rows.Close()

	var executions []*policy.RuleExecution
	for rows.Next() {
		exec := &policy.RuleExecution{}
		var durationMs int64
		var errorMsg sql.NullString
		var result sql.NullString

		if err := rows.Scan(&exec.ID, &exec.RuleID, &exec.Success, &durationMs, &errorMsg, &result, &exec.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan execution: %w", err)
		}

		exec.Duration = time.Duration(durationMs) * time.Millisecond
		if errorMsg.Valid {
			exec.Error = errorMsg.String
		}
		if result.Valid {
			exec.Result = policy.RuleResult(result.String)
		}

		executions = append(executions, exec)
	}

	return executions, rows.Err()
}

// CleanupOldExecutions removes old execution records
func (s *RuleStore) CleanupOldExecutions(ctx context.Context, olderThan time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM rule_executions WHERE created_at < ?", olderThan)
	return err
}

// ruleStoreAdapter bridges RuleStore (generic storage.RuleStore) to policy.RuleStore (typed).
// sqlite.RuleStore uses interface{} parameters to avoid import cycles in storage/types.go;
// this adapter converts at the boundary so the policy engine gets the typed interface it expects.
type ruleStoreAdapter struct {
	inner *RuleStore
}

// NewPolicyStore returns a policy.RuleStore backed by SQLite.
func NewPolicyStore(db *sql.DB) policy.RuleStore {
	return &ruleStoreAdapter{inner: &RuleStore{db: db}}
}

func (a *ruleStoreAdapter) CreateRule(ctx context.Context, rule *policy.Rule) error {
	return a.inner.CreateRule(ctx, rule)
}

func (a *ruleStoreAdapter) UpdateRule(ctx context.Context, rule *policy.Rule) error {
	return a.inner.UpdateRule(ctx, rule)
}

func (a *ruleStoreAdapter) GetRule(ctx context.Context, id string) (*policy.Rule, error) {
	v, err := a.inner.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	if r, ok := v.(*policy.Rule); ok {
		return r, nil
	}
	return nil, fmt.Errorf("store returned unexpected type %T", v)
}

func (a *ruleStoreAdapter) ListRules(ctx context.Context) ([]*policy.Rule, error) {
	vs, err := a.inner.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*policy.Rule, 0, len(vs))
	for _, v := range vs {
		if r, ok := v.(*policy.Rule); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (a *ruleStoreAdapter) DeleteRule(ctx context.Context, id string) error {
	return a.inner.DeleteRule(ctx, id)
}

func (a *ruleStoreAdapter) LogExecution(ctx context.Context, execution *policy.RuleExecution) error {
	return a.inner.LogExecution(ctx, execution)
}

func (a *ruleStoreAdapter) GetExecutions(ctx context.Context, ruleID string, limit int) ([]*policy.RuleExecution, error) {
	return a.inner.GetExecutions(ctx, ruleID, limit)
}

func (a *ruleStoreAdapter) CleanupOldExecutions(ctx context.Context, olderThan time.Time) error {
	return a.inner.CleanupOldExecutions(ctx, olderThan)
}
