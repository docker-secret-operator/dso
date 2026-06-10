package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type IntegrationConfigStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *IntegrationConfigStore) SaveConfig(ctx context.Context, config *storage.IntegrationConfig) error {
	query := `INSERT INTO integration_configs (plugin_id, enabled, endpoint, auth_type, auth_config_json, filters_json, retry_policy_json, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(plugin_id) DO UPDATE SET enabled=?, endpoint=?, auth_type=?, auth_config_json=?, filters_json=?, retry_policy_json=?, updated_at=?`

	_, err := s.db.ExecContext(ctx, query,
		config.PluginID, config.Enabled, config.Endpoint, config.AuthType, config.AuthConfigJSON, config.FiltersJSON, config.RetryPolicyJSON, config.UpdatedAt,
		config.Enabled, config.Endpoint, config.AuthType, config.AuthConfigJSON, config.FiltersJSON, config.RetryPolicyJSON, config.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to save integration config: %w", err)
	}

	return nil
}

func (s *IntegrationConfigStore) GetConfig(ctx context.Context, pluginID string) (*storage.IntegrationConfig, error) {
	query := `SELECT plugin_id, enabled, endpoint, auth_type, auth_config_json, filters_json, retry_policy_json, updated_at
	FROM integration_configs WHERE plugin_id=?`

	var config storage.IntegrationConfig
	var enabled sql.NullBool

	err := s.db.QueryRowContext(ctx, query, pluginID).Scan(
		&config.PluginID, &enabled, &config.Endpoint, &config.AuthType, &config.AuthConfigJSON, &config.FiltersJSON, &config.RetryPolicyJSON, &config.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get integration config: %w", err)
	}

	config.Enabled = enabled.Bool

	return &config, nil
}

func (s *IntegrationConfigStore) ListConfigs(ctx context.Context) ([]*storage.IntegrationConfig, error) {
	query := `SELECT plugin_id, enabled, endpoint, auth_type, auth_config_json, filters_json, retry_policy_json, updated_at
	FROM integration_configs ORDER BY plugin_id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list integration configs: %w", err)
	}
	defer rows.Close()

	var configs []*storage.IntegrationConfig
	for rows.Next() {
		var config storage.IntegrationConfig
		var enabled sql.NullBool

		if err := rows.Scan(&config.PluginID, &enabled, &config.Endpoint, &config.AuthType, &config.AuthConfigJSON, &config.FiltersJSON, &config.RetryPolicyJSON, &config.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan integration config: %w", err)
		}

		config.Enabled = enabled.Bool
		configs = append(configs, &config)
	}

	return configs, rows.Err()
}

func (s *IntegrationConfigStore) DeleteConfig(ctx context.Context, pluginID string) error {
	query := `DELETE FROM integration_configs WHERE plugin_id=?`
	_, err := s.db.ExecContext(ctx, query, pluginID)
	if err != nil {
		return fmt.Errorf("failed to delete integration config: %w", err)
	}
	return nil
}
