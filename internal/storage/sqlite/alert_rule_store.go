package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type AlertRuleStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *AlertRuleStore) Create(ctx context.Context, rule *storage.AlertRule) error {
	query := `
		INSERT INTO alert_rules (id, name, description, enabled, severity, metric, operator, threshold,
			duration_seconds, cooldown_seconds, is_builtin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, rule.ID, rule.Name, rule.Description, rule.Enabled,
		rule.Severity, rule.Metric, rule.Operator, rule.Threshold, rule.Duration, rule.Cooldown,
		rule.IsBuiltin, rule.CreatedAt, rule.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create alert rule: %w", err)
	}
	return nil
}

func (s *AlertRuleStore) Update(ctx context.Context, rule *storage.AlertRule) error {
	query := `
		UPDATE alert_rules
		SET name = ?, description = ?, enabled = ?, severity = ?, metric = ?, operator = ?,
			threshold = ?, duration_seconds = ?, cooldown_seconds = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, rule.Name, rule.Description, rule.Enabled,
		rule.Severity, rule.Metric, rule.Operator, rule.Threshold, rule.Duration, rule.Cooldown,
		rule.UpdatedAt, rule.ID)
	if err != nil {
		return fmt.Errorf("failed to update alert rule: %w", err)
	}
	return nil
}

func (s *AlertRuleStore) GetByID(ctx context.Context, id string) (*storage.AlertRule, error) {
	query := `
		SELECT id, name, description, enabled, severity, metric, operator, threshold,
			duration_seconds, cooldown_seconds, is_builtin, created_at, updated_at
		FROM alert_rules WHERE id = ?
	`

	var rule storage.AlertRule
	var description sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(&rule.ID, &rule.Name, &description,
		&rule.Enabled, &rule.Severity, &rule.Metric, &rule.Operator, &rule.Threshold,
		&rule.Duration, &rule.Cooldown, &rule.IsBuiltin, &rule.CreatedAt, &rule.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get alert rule: %w", err)
	}

	if description.Valid {
		rule.Description = &description.String
	}

	return &rule, nil
}

func (s *AlertRuleStore) List(ctx context.Context, limit int, offset int) ([]*storage.AlertRule, error) {
	query := `
		SELECT id, name, description, enabled, severity, metric, operator, threshold,
			duration_seconds, cooldown_seconds, is_builtin, created_at, updated_at
		FROM alert_rules ORDER BY created_at DESC LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert rules: %w", err)
	}
	defer rows.Close()

	var rules []*storage.AlertRule
	for rows.Next() {
		var rule storage.AlertRule
		var description sql.NullString

		if err := rows.Scan(&rule.ID, &rule.Name, &description, &rule.Enabled,
			&rule.Severity, &rule.Metric, &rule.Operator, &rule.Threshold,
			&rule.Duration, &rule.Cooldown, &rule.IsBuiltin, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan alert rule: %w", err)
		}

		if description.Valid {
			rule.Description = &description.String
		}

		rules = append(rules, &rule)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning alert rules: %w", err)
	}

	return rules, nil
}

func (s *AlertRuleStore) ListEnabled(ctx context.Context) ([]*storage.AlertRule, error) {
	query := `
		SELECT id, name, description, enabled, severity, metric, operator, threshold,
			duration_seconds, cooldown_seconds, is_builtin, created_at, updated_at
		FROM alert_rules WHERE enabled = 1 ORDER BY created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled alert rules: %w", err)
	}
	defer rows.Close()

	var rules []*storage.AlertRule
	for rows.Next() {
		var rule storage.AlertRule
		var description sql.NullString

		if err := rows.Scan(&rule.ID, &rule.Name, &description, &rule.Enabled,
			&rule.Severity, &rule.Metric, &rule.Operator, &rule.Threshold,
			&rule.Duration, &rule.Cooldown, &rule.IsBuiltin, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan alert rule: %w", err)
		}

		if description.Valid {
			rule.Description = &description.String
		}

		rules = append(rules, &rule)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning alert rules: %w", err)
	}

	return rules, nil
}

func (s *AlertRuleStore) Delete(ctx context.Context, id string) error {
	// Check if builtin
	rule, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if rule == nil {
		return fmt.Errorf("alert rule not found")
	}
	if rule.IsBuiltin {
		return fmt.Errorf("cannot delete builtin alert rule")
	}

	query := `DELETE FROM alert_rules WHERE id = ? AND is_builtin = 0`
	_, err = s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete alert rule: %w", err)
	}
	return nil
}
