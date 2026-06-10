package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/correlation"
	"github.com/google/uuid"
)

// CorrelationStore implements correlation.Store for SQLite
type CorrelationStore struct {
	db *sql.DB
}

// NewCorrelationStore creates a new correlation store
func NewCorrelationStore(db *sql.DB) *CorrelationStore {
	return &CorrelationStore{db: db}
}

// SaveIncident saves an incident to the database
func (s *CorrelationStore) SaveIncident(incident *correlation.Incident) error {
	metadata, _ := json.Marshal(incident.Metadata)

	_, err := s.db.Exec(`
		INSERT INTO incidents (id, title, severity, status, root_cause, correlation_score, first_seen, last_seen, acknowledged_at, resolved_at, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			severity = excluded.severity,
			status = excluded.status,
			root_cause = excluded.root_cause,
			correlation_score = excluded.correlation_score,
			last_seen = excluded.last_seen,
			acknowledged_at = excluded.acknowledged_at,
			resolved_at = excluded.resolved_at,
			metadata_json = excluded.metadata_json
	`, incident.ID, incident.Title, incident.Severity, incident.Status, incident.RootCause,
		incident.CorrelationScore, incident.FirstSeen, incident.LastSeen,
		incident.AcknowledgedAt, incident.ResolvedAt, string(metadata))

	return err
}

// GetIncident retrieves an incident from the database
func (s *CorrelationStore) GetIncident(id string) (*correlation.Incident, error) {
	var incident correlation.Incident
	var metadataJSON sql.NullString

	err := s.db.QueryRow(`
		SELECT id, title, severity, status, root_cause, correlation_score, first_seen, last_seen, acknowledged_at, resolved_at, metadata_json
		FROM incidents WHERE id = ?
	`, id).Scan(&incident.ID, &incident.Title, &incident.Severity, &incident.Status, &incident.RootCause,
		&incident.CorrelationScore, &incident.FirstSeen, &incident.LastSeen,
		&incident.AcknowledgedAt, &incident.ResolvedAt, &metadataJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	incident.Metadata = make(map[string]string)
	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &incident.Metadata)
	}

	// Load related events
	events, err := s.GetIncidentEvents(id)
	if err == nil {
		incident.RelatedEvents = make([]string, len(events))
		for i, e := range events {
			incident.RelatedEvents[i] = e.EventID
		}
	}

	return &incident, nil
}

// ListIncidents lists incidents by status
func (s *CorrelationStore) ListIncidents(status correlation.IncidentStatus, limit int) ([]*correlation.Incident, error) {
	query := `SELECT id, title, severity, status, root_cause, correlation_score, first_seen, last_seen, acknowledged_at, resolved_at, metadata_json
		FROM incidents WHERE status = ? ORDER BY last_seen DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []*correlation.Incident
	for rows.Next() {
		var incident correlation.Incident
		var metadataJSON sql.NullString

		if err := rows.Scan(&incident.ID, &incident.Title, &incident.Severity, &incident.Status, &incident.RootCause,
			&incident.CorrelationScore, &incident.FirstSeen, &incident.LastSeen,
			&incident.AcknowledgedAt, &incident.ResolvedAt, &metadataJSON); err != nil {
			return nil, err
		}

		incident.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &incident.Metadata)
		}

		incidents = append(incidents, &incident)
	}

	return incidents, rows.Err()
}

// UpdateIncident updates an incident in the database
func (s *CorrelationStore) UpdateIncident(incident *correlation.Incident) error {
	metadata, _ := json.Marshal(incident.Metadata)

	_, err := s.db.Exec(`
		UPDATE incidents SET
			title = ?,
			severity = ?,
			status = ?,
			root_cause = ?,
			correlation_score = ?,
			last_seen = ?,
			acknowledged_at = ?,
			resolved_at = ?,
			metadata_json = ?
		WHERE id = ?
	`, incident.Title, incident.Severity, incident.Status, incident.RootCause,
		incident.CorrelationScore, incident.LastSeen,
		incident.AcknowledgedAt, incident.ResolvedAt, string(metadata), incident.ID)

	return err
}

// DeleteIncident deletes an incident from the database
func (s *CorrelationStore) DeleteIncident(id string) error {
	_, err := s.db.Exec(`DELETE FROM incidents WHERE id = ?`, id)
	return err
}

// AddEventToIncident adds an event to an incident
func (s *CorrelationStore) AddEventToIncident(incidentID, eventID string, event *correlation.IncidentEvent) error {
	eventData, _ := json.Marshal(event.EventData)

	_, err := s.db.Exec(`
		INSERT INTO incident_events (id, incident_id, event_id, event_type, event_data, correlation_key)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(incident_id, event_id) DO NOTHING
	`, uuid.New().String(), incidentID, eventID, event.EventType, string(eventData), event.CorrelationKey)

	return err
}

// GetIncidentEvents retrieves all events for an incident
func (s *CorrelationStore) GetIncidentEvents(incidentID string) ([]*correlation.IncidentEvent, error) {
	rows, err := s.db.Query(`
		SELECT id, incident_id, event_id, event_type, event_data, correlation_key, created_at
		FROM incident_events WHERE incident_id = ?
	`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*correlation.IncidentEvent
	for rows.Next() {
		var event correlation.IncidentEvent
		var eventDataJSON sql.NullString

		if err := rows.Scan(&event.ID, &event.IncidentID, &event.EventID, &event.EventType, &eventDataJSON, &event.CorrelationKey, &event.CreatedAt); err != nil {
			return nil, err
		}

		event.EventData = make(map[string]interface{})
		if eventDataJSON.Valid {
			json.Unmarshal([]byte(eventDataJSON.String), &event.EventData)
		}

		events = append(events, &event)
	}

	return events, rows.Err()
}

// RemoveEventFromIncident removes an event from an incident
func (s *CorrelationStore) RemoveEventFromIncident(incidentID, eventID string) error {
	_, err := s.db.Exec(`DELETE FROM incident_events WHERE incident_id = ? AND event_id = ?`, incidentID, eventID)
	return err
}

// FindIncidentsByTimeRange finds incidents in a time range
func (s *CorrelationStore) FindIncidentsByTimeRange(start, end time.Time) ([]*correlation.Incident, error) {
	rows, err := s.db.Query(`
		SELECT id, title, severity, status, root_cause, correlation_score, first_seen, last_seen, acknowledged_at, resolved_at, metadata_json
		FROM incidents WHERE first_seen >= ? AND first_seen <= ?
		ORDER BY first_seen DESC
	`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []*correlation.Incident
	for rows.Next() {
		var incident correlation.Incident
		var metadataJSON sql.NullString

		if err := rows.Scan(&incident.ID, &incident.Title, &incident.Severity, &incident.Status, &incident.RootCause,
			&incident.CorrelationScore, &incident.FirstSeen, &incident.LastSeen,
			&incident.AcknowledgedAt, &incident.ResolvedAt, &metadataJSON); err != nil {
			return nil, err
		}

		incident.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &incident.Metadata)
		}

		incidents = append(incidents, &incident)
	}

	return incidents, rows.Err()
}

// FindIncidentsBySeverity finds incidents by severity
func (s *CorrelationStore) FindIncidentsBySeverity(severity correlation.Severity) ([]*correlation.Incident, error) {
	rows, err := s.db.Query(`
		SELECT id, title, severity, status, root_cause, correlation_score, first_seen, last_seen, acknowledged_at, resolved_at, metadata_json
		FROM incidents WHERE severity = ?
		ORDER BY correlation_score DESC
	`, severity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []*correlation.Incident
	for rows.Next() {
		var incident correlation.Incident
		var metadataJSON sql.NullString

		if err := rows.Scan(&incident.ID, &incident.Title, &incident.Severity, &incident.Status, &incident.RootCause,
			&incident.CorrelationScore, &incident.FirstSeen, &incident.LastSeen,
			&incident.AcknowledgedAt, &incident.ResolvedAt, &metadataJSON); err != nil {
			return nil, err
		}

		incident.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &incident.Metadata)
		}

		incidents = append(incidents, &incident)
	}

	return incidents, rows.Err()
}

// FindIncidentsByAffectedNode finds incidents affecting a specific node
func (s *CorrelationStore) FindIncidentsByAffectedNode(nodeID string) ([]*correlation.Incident, error) {
	// This would require parsing affected_nodes from metadata in a real implementation
	// For now, return all incidents and filter in application
	return s.ListIncidents(correlation.StatusOpen, 0)
}

// CleanupOldIncidents removes incidents older than specified time
func (s *CorrelationStore) CleanupOldIncidents(olderThan time.Time) error {
	_, err := s.db.Exec(`DELETE FROM incidents WHERE last_seen < ?`, olderThan)
	return err
}

// CleanupOldEvents removes incident events older than specified time
func (s *CorrelationStore) CleanupOldEvents(olderThan time.Time) error {
	_, err := s.db.Exec(`DELETE FROM incident_events WHERE created_at < ?`, olderThan)
	return err
}
