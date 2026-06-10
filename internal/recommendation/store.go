package recommendation

import (
	"time"
)

// Store defines the persistence interface for recommendations
type Store interface {
	// Recommendation operations
	SaveRecommendation(rec *Recommendation) error
	GetRecommendation(id string) (*Recommendation, error)
	ListRecommendations(status Status, limit int) ([]*Recommendation, error)
	UpdateRecommendation(rec *Recommendation) error
	DeleteRecommendation(id string) error

	// Query operations
	FindRecommendationsByCategory(category Category) ([]*Recommendation, error)
	FindRecommendationsByPriority(priority Priority) ([]*Recommendation, error)
	FindRecommendationsByResource(resourceID string) ([]*Recommendation, error)
	FindRecommendationsByIncident(incidentID string) ([]*Recommendation, error)
	FindRecommendationsByTimeRange(start, end time.Time) ([]*Recommendation, error)

	// Cleanup operations
	CleanupOldRecommendations(olderThan time.Time) error
}

// InMemoryStore provides in-memory recommendation storage
type InMemoryStore struct {
	recommendations map[string]*Recommendation
}

// NewInMemoryStore creates a new in-memory store
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		recommendations: make(map[string]*Recommendation),
	}
}

// SaveRecommendation saves a recommendation
func (s *InMemoryStore) SaveRecommendation(rec *Recommendation) error {
	s.recommendations[rec.ID] = rec
	return nil
}

// GetRecommendation retrieves a recommendation
func (s *InMemoryStore) GetRecommendation(id string) (*Recommendation, error) {
	rec, exists := s.recommendations[id]
	if !exists {
		return nil, nil
	}
	return rec, nil
}

// ListRecommendations lists recommendations by status
func (s *InMemoryStore) ListRecommendations(status Status, limit int) ([]*Recommendation, error) {
	var recs []*Recommendation
	for _, rec := range s.recommendations {
		if rec.Status == status {
			recs = append(recs, rec)
		}
	}

	if limit > 0 && len(recs) > limit {
		recs = recs[:limit]
	}

	return recs, nil
}

// UpdateRecommendation updates a recommendation
func (s *InMemoryStore) UpdateRecommendation(rec *Recommendation) error {
	if _, exists := s.recommendations[rec.ID]; !exists {
		return nil
	}
	s.recommendations[rec.ID] = rec
	return nil
}

// DeleteRecommendation deletes a recommendation
func (s *InMemoryStore) DeleteRecommendation(id string) error {
	delete(s.recommendations, id)
	return nil
}

// FindRecommendationsByCategory finds recommendations by category
func (s *InMemoryStore) FindRecommendationsByCategory(category Category) ([]*Recommendation, error) {
	var recs []*Recommendation
	for _, rec := range s.recommendations {
		if rec.Category == category {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

// FindRecommendationsByPriority finds recommendations by priority
func (s *InMemoryStore) FindRecommendationsByPriority(priority Priority) ([]*Recommendation, error) {
	var recs []*Recommendation
	for _, rec := range s.recommendations {
		if rec.Priority == priority {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

// FindRecommendationsByResource finds recommendations for a resource
func (s *InMemoryStore) FindRecommendationsByResource(resourceID string) ([]*Recommendation, error) {
	var recs []*Recommendation
	for _, rec := range s.recommendations {
		if rec.ResourceID == resourceID {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

// FindRecommendationsByIncident finds recommendations for an incident
func (s *InMemoryStore) FindRecommendationsByIncident(incidentID string) ([]*Recommendation, error) {
	var recs []*Recommendation
	for _, rec := range s.recommendations {
		if rec.IncidentID == incidentID {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

// FindRecommendationsByTimeRange finds recommendations in time range
func (s *InMemoryStore) FindRecommendationsByTimeRange(start, end time.Time) ([]*Recommendation, error) {
	var recs []*Recommendation
	for _, rec := range s.recommendations {
		if rec.CreatedAt.After(start) && rec.CreatedAt.Before(end) {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

// CleanupOldRecommendations removes old recommendations
func (s *InMemoryStore) CleanupOldRecommendations(olderThan time.Time) error {
	for id, rec := range s.recommendations {
		if rec.CreatedAt.Before(olderThan) {
			delete(s.recommendations, id)
		}
	}
	return nil
}
