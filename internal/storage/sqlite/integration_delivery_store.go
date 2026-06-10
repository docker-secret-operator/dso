package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type IntegrationDeliveryStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *IntegrationDeliveryStore) LogDelivery(ctx context.Context, delivery *storage.IntegrationDelivery) error {
	query := `INSERT INTO integration_deliveries (id, plugin_id, event_type, event_id, success, response_code, error_message, attempt, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		delivery.ID, delivery.PluginID, delivery.EventType, delivery.EventID, delivery.Success, delivery.ResponseCode, delivery.ErrorMessage, delivery.Attempt, delivery.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to log integration delivery: %w", err)
	}

	return nil
}

func (s *IntegrationDeliveryStore) GetDeliveries(ctx context.Context, pluginID string, limit int) ([]*storage.IntegrationDelivery, error) {
	query := `SELECT id, plugin_id, event_type, event_id, success, response_code, error_message, attempt, created_at
	FROM integration_deliveries WHERE plugin_id=? ORDER BY created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, pluginID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []*storage.IntegrationDelivery
	for rows.Next() {
		var delivery storage.IntegrationDelivery
		var success sql.NullBool
		var responseCode sql.NullInt64
		var errorMsg sql.NullString

		if err := rows.Scan(&delivery.ID, &delivery.PluginID, &delivery.EventType, &delivery.EventID,
			&success, &responseCode, &errorMsg, &delivery.Attempt, &delivery.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan delivery: %w", err)
		}

		delivery.Success = success.Bool
		if responseCode.Valid {
			delivery.ResponseCode = int(responseCode.Int64)
		}
		if errorMsg.Valid {
			delivery.ErrorMessage = &errorMsg.String
		}

		deliveries = append(deliveries, &delivery)
	}

	return deliveries, rows.Err()
}

func (s *IntegrationDeliveryStore) GetDeliveriesByEvent(ctx context.Context, eventID string) ([]*storage.IntegrationDelivery, error) {
	query := `SELECT id, plugin_id, event_type, event_id, success, response_code, error_message, attempt, created_at
	FROM integration_deliveries WHERE event_id=? ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deliveries by event: %w", err)
	}
	defer rows.Close()

	var deliveries []*storage.IntegrationDelivery
	for rows.Next() {
		var delivery storage.IntegrationDelivery
		var success sql.NullBool
		var responseCode sql.NullInt64
		var errorMsg sql.NullString

		if err := rows.Scan(&delivery.ID, &delivery.PluginID, &delivery.EventType, &delivery.EventID,
			&success, &responseCode, &errorMsg, &delivery.Attempt, &delivery.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan delivery: %w", err)
		}

		delivery.Success = success.Bool
		if responseCode.Valid {
			delivery.ResponseCode = int(responseCode.Int64)
		}
		if errorMsg.Valid {
			delivery.ErrorMessage = &errorMsg.String
		}

		deliveries = append(deliveries, &delivery)
	}

	return deliveries, rows.Err()
}

func (s *IntegrationDeliveryStore) CleanupOldDeliveries(ctx context.Context, olderThan time.Time) error {
	query := `DELETE FROM integration_deliveries WHERE created_at < ?`
	_, err := s.db.ExecContext(ctx, query, olderThan)
	if err != nil {
		return fmt.Errorf("failed to cleanup old deliveries: %w", err)
	}
	return nil
}
