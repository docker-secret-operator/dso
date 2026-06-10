package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type PluginStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *PluginStore) Create(ctx context.Context, plugin *storage.Plugin) error {
	query := `INSERT INTO plugins (id, name, version, type, enabled, status, error_message, loaded_at,
	                              health, enabled_at, disabled_at, restart_count, event_count, last_error_time, last_heartbeat)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, plugin.ID, plugin.Name, plugin.Version, plugin.Type,
		plugin.Enabled, plugin.Status, plugin.ErrorMessage, plugin.LoadedAt,
		plugin.Health, plugin.EnabledAt, plugin.DisabledAt, plugin.RestartCount, plugin.EventCount,
		plugin.LastErrorTime, plugin.LastHeartbeat)
	if err != nil {
		return fmt.Errorf("failed to create plugin: %w", err)
	}
	return nil
}

func (s *PluginStore) Update(ctx context.Context, plugin *storage.Plugin) error {
	query := `UPDATE plugins SET name=?, version=?, type=?, enabled=?, status=?, error_message=?, loaded_at=?,
	                            health=?, enabled_at=?, disabled_at=?, restart_count=?, event_count=?,
	                            last_error_time=?, last_heartbeat=? WHERE id=?`
	_, err := s.db.ExecContext(ctx, query, plugin.Name, plugin.Version, plugin.Type,
		plugin.Enabled, plugin.Status, plugin.ErrorMessage, plugin.LoadedAt,
		plugin.Health, plugin.EnabledAt, plugin.DisabledAt, plugin.RestartCount, plugin.EventCount,
		plugin.LastErrorTime, plugin.LastHeartbeat, plugin.ID)
	if err != nil {
		return fmt.Errorf("failed to update plugin: %w", err)
	}
	return nil
}

func (s *PluginStore) GetByID(ctx context.Context, id string) (*storage.Plugin, error) {
	query := `SELECT id, name, version, type, enabled, status, error_message, loaded_at,
	          enabled_at, disabled_at, restart_count, event_count, last_error_time, last_heartbeat, health
	          FROM plugins WHERE id=?`
	var plugin storage.Plugin
	var enabled sql.NullBool
	var errorMsg sql.NullString
	var loadedAt sql.NullTime
	var enabledAt sql.NullTime
	var disabledAt sql.NullTime
	var lastErrorTime sql.NullTime
	var lastHeartbeat sql.NullTime
	var health sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(&plugin.ID, &plugin.Name, &plugin.Version,
		&plugin.Type, &enabled, &plugin.Status, &errorMsg, &loadedAt,
		&enabledAt, &disabledAt, &plugin.RestartCount, &plugin.EventCount,
		&lastErrorTime, &lastHeartbeat, &health)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin: %w", err)
	}

	plugin.Enabled = enabled.Bool
	if errorMsg.Valid {
		plugin.ErrorMessage = &errorMsg.String
	}
	if loadedAt.Valid {
		plugin.LoadedAt = &loadedAt.Time
	}
	if enabledAt.Valid {
		plugin.EnabledAt = &enabledAt.Time
	}
	if disabledAt.Valid {
		plugin.DisabledAt = &disabledAt.Time
	}
	if lastErrorTime.Valid {
		plugin.LastErrorTime = &lastErrorTime.Time
	}
	if lastHeartbeat.Valid {
		plugin.LastHeartbeat = &lastHeartbeat.Time
	}
	if health.Valid {
		plugin.Health = health.String
	}

	return &plugin, nil
}

func (s *PluginStore) List(ctx context.Context) ([]*storage.Plugin, error) {
	query := `SELECT id, name, version, type, enabled, status, error_message, loaded_at,
	          enabled_at, disabled_at, restart_count, event_count, last_error_time, last_heartbeat, health
	          FROM plugins ORDER BY id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list plugins: %w", err)
	}
	defer rows.Close()

	var plugins []*storage.Plugin
	for rows.Next() {
		var plugin storage.Plugin
		var enabled sql.NullBool
		var errorMsg sql.NullString
		var loadedAt sql.NullTime
		var enabledAt sql.NullTime
		var disabledAt sql.NullTime
		var lastErrorTime sql.NullTime
		var lastHeartbeat sql.NullTime
		var health sql.NullString

		if err := rows.Scan(&plugin.ID, &plugin.Name, &plugin.Version, &plugin.Type,
			&enabled, &plugin.Status, &errorMsg, &loadedAt,
			&enabledAt, &disabledAt, &plugin.RestartCount, &plugin.EventCount,
			&lastErrorTime, &lastHeartbeat, &health); err != nil {
			return nil, fmt.Errorf("failed to scan plugin: %w", err)
		}

		plugin.Enabled = enabled.Bool
		if errorMsg.Valid {
			plugin.ErrorMessage = &errorMsg.String
		}
		if loadedAt.Valid {
			plugin.LoadedAt = &loadedAt.Time
		}
		if enabledAt.Valid {
			plugin.EnabledAt = &enabledAt.Time
		}
		if disabledAt.Valid {
			plugin.DisabledAt = &disabledAt.Time
		}
		if lastErrorTime.Valid {
			plugin.LastErrorTime = &lastErrorTime.Time
		}
		if lastHeartbeat.Valid {
			plugin.LastHeartbeat = &lastHeartbeat.Time
		}
		if health.Valid {
			plugin.Health = health.String
		}

		plugins = append(plugins, &plugin)
	}

	return plugins, rows.Err()
}

// SaveConfig saves plugin configuration
func (s *PluginStore) SaveConfig(ctx context.Context, config *storage.PluginConfig) error {
	query := `INSERT INTO plugin_configs (plugin_id, config_json, updated_at)
	VALUES (?, ?, ?)
	ON CONFLICT(plugin_id) DO UPDATE SET config_json=?, updated_at=?`
	_, err := s.db.ExecContext(ctx, query, config.PluginID, config.ConfigJSON, config.UpdatedAt,
		config.ConfigJSON, config.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save plugin config: %w", err)
	}
	return nil
}

// GetConfig retrieves plugin configuration
func (s *PluginStore) GetConfig(ctx context.Context, pluginID string) (*storage.PluginConfig, error) {
	query := `SELECT plugin_id, config_json, updated_at FROM plugin_configs WHERE plugin_id=?`
	var config storage.PluginConfig
	err := s.db.QueryRowContext(ctx, query, pluginID).Scan(&config.PluginID, &config.ConfigJSON, &config.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin config: %w", err)
	}
	return &config, nil
}

// LogEvent logs a plugin event
func (s *PluginStore) LogEvent(ctx context.Context, event *storage.PluginEvent) error {
	query := `INSERT INTO plugin_events (plugin_id, level, message, created_at)
	VALUES (?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, event.PluginID, event.Level, event.Message, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to log plugin event: %w", err)
	}
	return nil
}

// GetEvents retrieves plugin events
func (s *PluginStore) GetEvents(ctx context.Context, pluginID string, limit int) ([]*storage.PluginEvent, error) {
	query := `SELECT id, plugin_id, level, message, created_at FROM plugin_events
	WHERE plugin_id=? ORDER BY created_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, pluginID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin events: %w", err)
	}
	defer rows.Close()

	var events []*storage.PluginEvent
	for rows.Next() {
		var event storage.PluginEvent
		if err := rows.Scan(&event.ID, &event.PluginID, &event.Level, &event.Message, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan plugin event: %w", err)
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}
