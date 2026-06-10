package forecast

import (
	"time"
)

// Store defines the persistence interface for forecasts
type Store interface {
	// Forecast operations
	SaveForecast(forecast *Forecast) error
	GetForecast(id string) (*Forecast, error)
	ListForecasts(limit int) ([]*Forecast, error)
	DeleteForecast(id string) error

	// Query operations
	FindForecastsByResource(resourceType ForecastResource) ([]*Forecast, error)
	FindForecastsBySeverity(severity ForecastSeverity) ([]*Forecast, error)
	FindForecastsByHorizon(horizon ForecastHorizon) ([]*Forecast, error)
	FindForecastsByTimeRange(start, end time.Time) ([]*Forecast, error)

	// Cleanup operations
	CleanupOldForecasts(olderThan time.Time) error
}

// InMemoryStore provides in-memory forecast storage
type InMemoryStore struct {
	forecasts map[string]*Forecast
}

// NewInMemoryStore creates a new in-memory store
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		forecasts: make(map[string]*Forecast),
	}
}

// SaveForecast saves a forecast
func (s *InMemoryStore) SaveForecast(forecast *Forecast) error {
	s.forecasts[forecast.ID] = forecast
	return nil
}

// GetForecast retrieves a forecast
func (s *InMemoryStore) GetForecast(id string) (*Forecast, error) {
	forecast, exists := s.forecasts[id]
	if !exists {
		return nil, nil
	}
	return forecast, nil
}

// ListForecasts lists all forecasts
func (s *InMemoryStore) ListForecasts(limit int) ([]*Forecast, error) {
	var forecasts []*Forecast
	for _, fc := range s.forecasts {
		forecasts = append(forecasts, fc)
	}

	if limit > 0 && len(forecasts) > limit {
		forecasts = forecasts[:limit]
	}

	return forecasts, nil
}

// DeleteForecast deletes a forecast
func (s *InMemoryStore) DeleteForecast(id string) error {
	delete(s.forecasts, id)
	return nil
}

// FindForecastsByResource finds forecasts by resource type
func (s *InMemoryStore) FindForecastsByResource(resourceType ForecastResource) ([]*Forecast, error) {
	var forecasts []*Forecast
	for _, fc := range s.forecasts {
		if fc.ResourceType == resourceType {
			forecasts = append(forecasts, fc)
		}
	}
	return forecasts, nil
}

// FindForecastsBySeverity finds forecasts by severity
func (s *InMemoryStore) FindForecastsBySeverity(severity ForecastSeverity) ([]*Forecast, error) {
	var forecasts []*Forecast
	for _, fc := range s.forecasts {
		if fc.Severity == severity {
			forecasts = append(forecasts, fc)
		}
	}
	return forecasts, nil
}

// FindForecastsByHorizon finds forecasts by horizon
func (s *InMemoryStore) FindForecastsByHorizon(horizon ForecastHorizon) ([]*Forecast, error) {
	var forecasts []*Forecast
	for _, fc := range s.forecasts {
		if fc.Horizon == horizon {
			forecasts = append(forecasts, fc)
		}
	}
	return forecasts, nil
}

// FindForecastsByTimeRange finds forecasts in time range
func (s *InMemoryStore) FindForecastsByTimeRange(start, end time.Time) ([]*Forecast, error) {
	var forecasts []*Forecast
	for _, fc := range s.forecasts {
		if fc.CreatedAt.After(start) && fc.CreatedAt.Before(end) {
			forecasts = append(forecasts, fc)
		}
	}
	return forecasts, nil
}

// CleanupOldForecasts removes old forecasts
func (s *InMemoryStore) CleanupOldForecasts(olderThan time.Time) error {
	for id, fc := range s.forecasts {
		if fc.CreatedAt.Before(olderThan) {
			delete(s.forecasts, id)
		}
	}
	return nil
}
