package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/recommendation"
)

// RecommendationStore implements recommendation.Store for SQLite
type RecommendationStore struct {
	db *sql.DB
}

// NewRecommendationStore creates a new recommendation store
func NewRecommendationStore(db *sql.DB) *RecommendationStore {
	return &RecommendationStore{db: db}
}

// SaveRecommendation saves a recommendation to the database
func (s *RecommendationStore) SaveRecommendation(rec *recommendation.Recommendation) error {
	metadata, _ := json.Marshal(rec.Metadata)

	_, err := s.db.Exec(`
		INSERT INTO recommendations (id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			priority = excluded.priority,
			category = excluded.category,
			status = excluded.status,
			resource_id = excluded.resource_id,
			incident_id = excluded.incident_id,
			suggested_action = excluded.suggested_action,
			confidence = excluded.confidence,
			acknowledged_at = excluded.acknowledged_at,
			implemented_at = excluded.implemented_at,
			dismissed_at = excluded.dismissed_at,
			metadata_json = excluded.metadata_json
	`, rec.ID, rec.Title, rec.Description, rec.Priority, rec.Category, rec.Status, rec.ResourceID, rec.IncidentID,
		rec.SuggestedAction, rec.Confidence, rec.CreatedAt,
		rec.AcknowledgedAt, rec.ImplementedAt, rec.DismissedAt, string(metadata))

	return err
}

// GetRecommendation retrieves a recommendation from the database
func (s *RecommendationStore) GetRecommendation(id string) (*recommendation.Recommendation, error) {
	var rec recommendation.Recommendation
	var metadataJSON sql.NullString

	err := s.db.QueryRow(`
		SELECT id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json
		FROM recommendations WHERE id = ?
	`, id).Scan(&rec.ID, &rec.Title, &rec.Description, &rec.Priority, &rec.Category, &rec.Status, &rec.ResourceID, &rec.IncidentID,
		&rec.SuggestedAction, &rec.Confidence, &rec.CreatedAt, &rec.AcknowledgedAt, &rec.ImplementedAt, &rec.DismissedAt, &metadataJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	rec.Metadata = make(map[string]string)
	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &rec.Metadata)
	}

	return &rec, nil
}

// ListRecommendations lists recommendations by status
func (s *RecommendationStore) ListRecommendations(status recommendation.Status, limit int) ([]*recommendation.Recommendation, error) {
	query := `SELECT id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json
		FROM recommendations WHERE status = ? ORDER BY created_at DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []*recommendation.Recommendation
	for rows.Next() {
		var rec recommendation.Recommendation
		var metadataJSON sql.NullString

		if err := rows.Scan(&rec.ID, &rec.Title, &rec.Description, &rec.Priority, &rec.Category, &rec.Status, &rec.ResourceID, &rec.IncidentID,
			&rec.SuggestedAction, &rec.Confidence, &rec.CreatedAt, &rec.AcknowledgedAt, &rec.ImplementedAt, &rec.DismissedAt, &metadataJSON); err != nil {
			return nil, err
		}

		rec.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &rec.Metadata)
		}

		recs = append(recs, &rec)
	}

	return recs, rows.Err()
}

// UpdateRecommendation updates a recommendation in the database
func (s *RecommendationStore) UpdateRecommendation(rec *recommendation.Recommendation) error {
	metadata, _ := json.Marshal(rec.Metadata)

	_, err := s.db.Exec(`
		UPDATE recommendations SET
			title = ?,
			description = ?,
			priority = ?,
			category = ?,
			status = ?,
			resource_id = ?,
			incident_id = ?,
			suggested_action = ?,
			confidence = ?,
			acknowledged_at = ?,
			implemented_at = ?,
			dismissed_at = ?,
			metadata_json = ?
		WHERE id = ?
	`, rec.Title, rec.Description, rec.Priority, rec.Category, rec.Status, rec.ResourceID, rec.IncidentID,
		rec.SuggestedAction, rec.Confidence, rec.AcknowledgedAt, rec.ImplementedAt, rec.DismissedAt, string(metadata), rec.ID)

	return err
}

// DeleteRecommendation deletes a recommendation from the database
func (s *RecommendationStore) DeleteRecommendation(id string) error {
	_, err := s.db.Exec(`DELETE FROM recommendations WHERE id = ?`, id)
	return err
}

// FindRecommendationsByCategory finds recommendations by category
func (s *RecommendationStore) FindRecommendationsByCategory(category recommendation.Category) ([]*recommendation.Recommendation, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json
		FROM recommendations WHERE category = ? ORDER BY created_at DESC
	`, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []*recommendation.Recommendation
	for rows.Next() {
		var rec recommendation.Recommendation
		var metadataJSON sql.NullString

		if err := rows.Scan(&rec.ID, &rec.Title, &rec.Description, &rec.Priority, &rec.Category, &rec.Status, &rec.ResourceID, &rec.IncidentID,
			&rec.SuggestedAction, &rec.Confidence, &rec.CreatedAt, &rec.AcknowledgedAt, &rec.ImplementedAt, &rec.DismissedAt, &metadataJSON); err != nil {
			return nil, err
		}

		rec.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &rec.Metadata)
		}

		recs = append(recs, &rec)
	}

	return recs, rows.Err()
}

// FindRecommendationsByPriority finds recommendations by priority
func (s *RecommendationStore) FindRecommendationsByPriority(priority recommendation.Priority) ([]*recommendation.Recommendation, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json
		FROM recommendations WHERE priority = ? ORDER BY created_at DESC
	`, priority)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []*recommendation.Recommendation
	for rows.Next() {
		var rec recommendation.Recommendation
		var metadataJSON sql.NullString

		if err := rows.Scan(&rec.ID, &rec.Title, &rec.Description, &rec.Priority, &rec.Category, &rec.Status, &rec.ResourceID, &rec.IncidentID,
			&rec.SuggestedAction, &rec.Confidence, &rec.CreatedAt, &rec.AcknowledgedAt, &rec.ImplementedAt, &rec.DismissedAt, &metadataJSON); err != nil {
			return nil, err
		}

		rec.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &rec.Metadata)
		}

		recs = append(recs, &rec)
	}

	return recs, rows.Err()
}

// FindRecommendationsByResource finds recommendations for a resource
func (s *RecommendationStore) FindRecommendationsByResource(resourceID string) ([]*recommendation.Recommendation, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json
		FROM recommendations WHERE resource_id = ? ORDER BY created_at DESC
	`, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []*recommendation.Recommendation
	for rows.Next() {
		var rec recommendation.Recommendation
		var metadataJSON sql.NullString

		if err := rows.Scan(&rec.ID, &rec.Title, &rec.Description, &rec.Priority, &rec.Category, &rec.Status, &rec.ResourceID, &rec.IncidentID,
			&rec.SuggestedAction, &rec.Confidence, &rec.CreatedAt, &rec.AcknowledgedAt, &rec.ImplementedAt, &rec.DismissedAt, &metadataJSON); err != nil {
			return nil, err
		}

		rec.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &rec.Metadata)
		}

		recs = append(recs, &rec)
	}

	return recs, rows.Err()
}

// FindRecommendationsByIncident finds recommendations for an incident
func (s *RecommendationStore) FindRecommendationsByIncident(incidentID string) ([]*recommendation.Recommendation, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json
		FROM recommendations WHERE incident_id = ? ORDER BY created_at DESC
	`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []*recommendation.Recommendation
	for rows.Next() {
		var rec recommendation.Recommendation
		var metadataJSON sql.NullString

		if err := rows.Scan(&rec.ID, &rec.Title, &rec.Description, &rec.Priority, &rec.Category, &rec.Status, &rec.ResourceID, &rec.IncidentID,
			&rec.SuggestedAction, &rec.Confidence, &rec.CreatedAt, &rec.AcknowledgedAt, &rec.ImplementedAt, &rec.DismissedAt, &metadataJSON); err != nil {
			return nil, err
		}

		rec.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &rec.Metadata)
		}

		recs = append(recs, &rec)
	}

	return recs, rows.Err()
}

// FindRecommendationsByTimeRange finds recommendations in a time range
func (s *RecommendationStore) FindRecommendationsByTimeRange(start, end time.Time) ([]*recommendation.Recommendation, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, priority, category, status, resource_id, incident_id, suggested_action, confidence, created_at, acknowledged_at, implemented_at, dismissed_at, metadata_json
		FROM recommendations WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC
	`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []*recommendation.Recommendation
	for rows.Next() {
		var rec recommendation.Recommendation
		var metadataJSON sql.NullString

		if err := rows.Scan(&rec.ID, &rec.Title, &rec.Description, &rec.Priority, &rec.Category, &rec.Status, &rec.ResourceID, &rec.IncidentID,
			&rec.SuggestedAction, &rec.Confidence, &rec.CreatedAt, &rec.AcknowledgedAt, &rec.ImplementedAt, &rec.DismissedAt, &metadataJSON); err != nil {
			return nil, err
		}

		rec.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &rec.Metadata)
		}

		recs = append(recs, &rec)
	}

	return recs, rows.Err()
}

// CleanupOldRecommendations removes recommendations older than specified time
func (s *RecommendationStore) CleanupOldRecommendations(olderThan time.Time) error {
	_, err := s.db.Exec(`DELETE FROM recommendations WHERE created_at < ?`, olderThan)
	return err
}
