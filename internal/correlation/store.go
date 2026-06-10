package correlation

import (
	"time"
)

// Store defines the persistence interface for incidents
type Store interface {
	// Incident operations
	SaveIncident(incident *Incident) error
	GetIncident(id string) (*Incident, error)
	ListIncidents(status IncidentStatus, limit int) ([]*Incident, error)
	UpdateIncident(incident *Incident) error
	DeleteIncident(id string) error

	// Event operations
	AddEventToIncident(incidentID, eventID string, event *IncidentEvent) error
	GetIncidentEvents(incidentID string) ([]*IncidentEvent, error)
	RemoveEventFromIncident(incidentID, eventID string) error

	// Query operations
	FindIncidentsByTimeRange(start, end time.Time) ([]*Incident, error)
	FindIncidentsBySeverity(severity Severity) ([]*Incident, error)
	FindIncidentsByAffectedNode(nodeID string) ([]*Incident, error)

	// Cleanup operations
	CleanupOldIncidents(olderThan time.Time) error
	CleanupOldEvents(olderThan time.Time) error
}

// InMemoryStore provides in-memory incident storage
type InMemoryStore struct {
	incidents map[string]*Incident
	events    map[string][]*IncidentEvent
}

// NewInMemoryStore creates a new in-memory store
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		incidents: make(map[string]*Incident),
		events:    make(map[string][]*IncidentEvent),
	}
}

// SaveIncident saves an incident
func (s *InMemoryStore) SaveIncident(incident *Incident) error {
	s.incidents[incident.ID] = incident
	return nil
}

// GetIncident retrieves an incident
func (s *InMemoryStore) GetIncident(id string) (*Incident, error) {
	incident, exists := s.incidents[id]
	if !exists {
		return nil, nil
	}
	return incident, nil
}

// ListIncidents lists incidents by status
func (s *InMemoryStore) ListIncidents(status IncidentStatus, limit int) ([]*Incident, error) {
	var incidents []*Incident
	for _, inc := range s.incidents {
		if inc.Status == status {
			incidents = append(incidents, inc)
		}
	}

	if limit > 0 && len(incidents) > limit {
		incidents = incidents[:limit]
	}

	return incidents, nil
}

// UpdateIncident updates an incident
func (s *InMemoryStore) UpdateIncident(incident *Incident) error {
	if _, exists := s.incidents[incident.ID]; !exists {
		return nil
	}
	s.incidents[incident.ID] = incident
	return nil
}

// DeleteIncident deletes an incident
func (s *InMemoryStore) DeleteIncident(id string) error {
	delete(s.incidents, id)
	delete(s.events, id)
	return nil
}

// AddEventToIncident adds an event to an incident
func (s *InMemoryStore) AddEventToIncident(incidentID, eventID string, event *IncidentEvent) error {
	s.events[incidentID] = append(s.events[incidentID], event)
	return nil
}

// GetIncidentEvents retrieves events for an incident
func (s *InMemoryStore) GetIncidentEvents(incidentID string) ([]*IncidentEvent, error) {
	return s.events[incidentID], nil
}

// RemoveEventFromIncident removes an event from an incident
func (s *InMemoryStore) RemoveEventFromIncident(incidentID, eventID string) error {
	events := s.events[incidentID]
	for i, e := range events {
		if e.EventID == eventID {
			s.events[incidentID] = append(events[:i], events[i+1:]...)
			break
		}
	}
	return nil
}

// FindIncidentsByTimeRange finds incidents in time range
func (s *InMemoryStore) FindIncidentsByTimeRange(start, end time.Time) ([]*Incident, error) {
	var incidents []*Incident
	for _, inc := range s.incidents {
		if inc.FirstSeen.After(start) && inc.FirstSeen.Before(end) {
			incidents = append(incidents, inc)
		}
	}
	return incidents, nil
}

// FindIncidentsBySeverity finds incidents by severity
func (s *InMemoryStore) FindIncidentsBySeverity(severity Severity) ([]*Incident, error) {
	var incidents []*Incident
	for _, inc := range s.incidents {
		if inc.Severity == severity {
			incidents = append(incidents, inc)
		}
	}
	return incidents, nil
}

// FindIncidentsByAffectedNode finds incidents affecting a node
func (s *InMemoryStore) FindIncidentsByAffectedNode(nodeID string) ([]*Incident, error) {
	var incidents []*Incident
	for _, inc := range s.incidents {
		for _, node := range inc.AffectedNodes {
			if node == nodeID {
				incidents = append(incidents, inc)
				break
			}
		}
	}
	return incidents, nil
}

// CleanupOldIncidents removes old incidents
func (s *InMemoryStore) CleanupOldIncidents(olderThan time.Time) error {
	for id, inc := range s.incidents {
		if inc.LastSeen.Before(olderThan) {
			delete(s.incidents, id)
			delete(s.events, id)
		}
	}
	return nil
}

// CleanupOldEvents removes old events
func (s *InMemoryStore) CleanupOldEvents(olderThan time.Time) error {
	for incID, events := range s.events {
		var filtered []*IncidentEvent
		for _, event := range events {
			if event.CreatedAt.After(olderThan) {
				filtered = append(filtered, event)
			}
		}
		s.events[incID] = filtered
	}
	return nil
}
