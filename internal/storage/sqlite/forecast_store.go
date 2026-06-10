package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/forecast"
)

// ForecastStore implements forecast.Store for SQLite
type ForecastStore struct {
	db *sql.DB
}

// NewForecastStore creates a new forecast store
func NewForecastStore(db *sql.DB) *ForecastStore {
	return &ForecastStore{db: db}
}

// SaveForecast saves a forecast to the database
func (s *ForecastStore) SaveForecast(f *forecast.Forecast) error {
	metadata, _ := json.Marshal(f.Metadata)

	_, err := s.db.Exec(`
		INSERT INTO forecasts (id, resource_type, resource_id, metric, current_value, predicted_value, growth_rate, confidence, horizon, severity, trend, created_at, updated_at, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			resource_type = excluded.resource_type,
			resource_id = excluded.resource_id,
			metric = excluded.metric,
			current_value = excluded.current_value,
			predicted_value = excluded.predicted_value,
			growth_rate = excluded.growth_rate,
			confidence = excluded.confidence,
			horizon = excluded.horizon,
			severity = excluded.severity,
			trend = excluded.trend,
			updated_at = excluded.updated_at,
			metadata_json = excluded.metadata_json
	`, f.ID, f.ResourceType, f.ResourceID, f.Metric, f.CurrentValue, f.PredictedValue,
		f.GrowthRate, f.Confidence, f.Horizon, f.Severity, f.Trend, f.CreatedAt, f.UpdatedAt, string(metadata))

	return err
}

// GetForecast retrieves a forecast from the database
func (s *ForecastStore) GetForecast(id string) (*forecast.Forecast, error) {
	var f forecast.Forecast
	var metadataJSON sql.NullString

	err := s.db.QueryRow(`
		SELECT id, resource_type, resource_id, metric, current_value, predicted_value, growth_rate, confidence, horizon, severity, trend, created_at, updated_at, metadata_json
		FROM forecasts WHERE id = ?
	`, id).Scan(&f.ID, &f.ResourceType, &f.ResourceID, &f.Metric, &f.CurrentValue, &f.PredictedValue,
		&f.GrowthRate, &f.Confidence, &f.Horizon, &f.Severity, &f.Trend, &f.CreatedAt, &f.UpdatedAt, &metadataJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	f.Metadata = make(map[string]string)
	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &f.Metadata)
	}

	return &f, nil
}

// ListForecasts lists forecasts
func (s *ForecastStore) ListForecasts(limit int) ([]*forecast.Forecast, error) {
	query := `SELECT id, resource_type, resource_id, metric, current_value, predicted_value, growth_rate, confidence, horizon, severity, trend, created_at, updated_at, metadata_json
		FROM forecasts ORDER BY created_at DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var forecasts []*forecast.Forecast
	for rows.Next() {
		var f forecast.Forecast
		var metadataJSON sql.NullString

		if err := rows.Scan(&f.ID, &f.ResourceType, &f.ResourceID, &f.Metric, &f.CurrentValue, &f.PredictedValue,
			&f.GrowthRate, &f.Confidence, &f.Horizon, &f.Severity, &f.Trend, &f.CreatedAt, &f.UpdatedAt, &metadataJSON); err != nil {
			return nil, err
		}

		f.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &f.Metadata)
		}

		forecasts = append(forecasts, &f)
	}

	return forecasts, rows.Err()
}

// DeleteForecast deletes a forecast from the database
func (s *ForecastStore) DeleteForecast(id string) error {
	_, err := s.db.Exec(`DELETE FROM forecasts WHERE id = ?`, id)
	return err
}

// FindForecastsByResource finds forecasts by resource type
func (s *ForecastStore) FindForecastsByResource(resourceType forecast.ForecastResource) ([]*forecast.Forecast, error) {
	rows, err := s.db.Query(`
		SELECT id, resource_type, resource_id, metric, current_value, predicted_value, growth_rate, confidence, horizon, severity, trend, created_at, updated_at, metadata_json
		FROM forecasts WHERE resource_type = ? ORDER BY created_at DESC
	`, resourceType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var forecasts []*forecast.Forecast
	for rows.Next() {
		var f forecast.Forecast
		var metadataJSON sql.NullString

		if err := rows.Scan(&f.ID, &f.ResourceType, &f.ResourceID, &f.Metric, &f.CurrentValue, &f.PredictedValue,
			&f.GrowthRate, &f.Confidence, &f.Horizon, &f.Severity, &f.Trend, &f.CreatedAt, &f.UpdatedAt, &metadataJSON); err != nil {
			return nil, err
		}

		f.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &f.Metadata)
		}

		forecasts = append(forecasts, &f)
	}

	return forecasts, rows.Err()
}

// FindForecastsBySeverity finds forecasts by severity
func (s *ForecastStore) FindForecastsBySeverity(severity forecast.ForecastSeverity) ([]*forecast.Forecast, error) {
	rows, err := s.db.Query(`
		SELECT id, resource_type, resource_id, metric, current_value, predicted_value, growth_rate, confidence, horizon, severity, trend, created_at, updated_at, metadata_json
		FROM forecasts WHERE severity = ? ORDER BY created_at DESC
	`, severity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var forecasts []*forecast.Forecast
	for rows.Next() {
		var f forecast.Forecast
		var metadataJSON sql.NullString

		if err := rows.Scan(&f.ID, &f.ResourceType, &f.ResourceID, &f.Metric, &f.CurrentValue, &f.PredictedValue,
			&f.GrowthRate, &f.Confidence, &f.Horizon, &f.Severity, &f.Trend, &f.CreatedAt, &f.UpdatedAt, &metadataJSON); err != nil {
			return nil, err
		}

		f.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &f.Metadata)
		}

		forecasts = append(forecasts, &f)
	}

	return forecasts, rows.Err()
}

// FindForecastsByHorizon finds forecasts by horizon
func (s *ForecastStore) FindForecastsByHorizon(horizon forecast.ForecastHorizon) ([]*forecast.Forecast, error) {
	rows, err := s.db.Query(`
		SELECT id, resource_type, resource_id, metric, current_value, predicted_value, growth_rate, confidence, horizon, severity, trend, created_at, updated_at, metadata_json
		FROM forecasts WHERE horizon = ? ORDER BY created_at DESC
	`, horizon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var forecasts []*forecast.Forecast
	for rows.Next() {
		var f forecast.Forecast
		var metadataJSON sql.NullString

		if err := rows.Scan(&f.ID, &f.ResourceType, &f.ResourceID, &f.Metric, &f.CurrentValue, &f.PredictedValue,
			&f.GrowthRate, &f.Confidence, &f.Horizon, &f.Severity, &f.Trend, &f.CreatedAt, &f.UpdatedAt, &metadataJSON); err != nil {
			return nil, err
		}

		f.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &f.Metadata)
		}

		forecasts = append(forecasts, &f)
	}

	return forecasts, rows.Err()
}

// FindForecastsByTimeRange finds forecasts in a time range
func (s *ForecastStore) FindForecastsByTimeRange(start, end time.Time) ([]*forecast.Forecast, error) {
	rows, err := s.db.Query(`
		SELECT id, resource_type, resource_id, metric, current_value, predicted_value, growth_rate, confidence, horizon, severity, trend, created_at, updated_at, metadata_json
		FROM forecasts WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC
	`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var forecasts []*forecast.Forecast
	for rows.Next() {
		var f forecast.Forecast
		var metadataJSON sql.NullString

		if err := rows.Scan(&f.ID, &f.ResourceType, &f.ResourceID, &f.Metric, &f.CurrentValue, &f.PredictedValue,
			&f.GrowthRate, &f.Confidence, &f.Horizon, &f.Severity, &f.Trend, &f.CreatedAt, &f.UpdatedAt, &metadataJSON); err != nil {
			return nil, err
		}

		f.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &f.Metadata)
		}

		forecasts = append(forecasts, &f)
	}

	return forecasts, rows.Err()
}

// CleanupOldForecasts removes forecasts older than specified time
func (s *ForecastStore) CleanupOldForecasts(olderThan time.Time) error {
	_, err := s.db.Exec(`DELETE FROM forecasts WHERE created_at < ?`, olderThan)
	return err
}
