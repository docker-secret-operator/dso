package autonomy

import "time"

// Store defines the persistence interface
type Store interface {
	SaveAction(action *AutonomousAction) error
	GetAction(id string) (*AutonomousAction, error)
	ListActions(limit int) ([]*AutonomousAction, error)
	UpdateAction(action *AutonomousAction) error
	SaveRollback(entry *RollbackEntry) error
	ListRollbacks(actionID string) ([]*RollbackEntry, error)
	CleanupOld(olderThan time.Time) error
}

// InMemoryStore provides in-memory storage
type InMemoryStore struct {
	actions   map[string]*AutonomousAction
	rollbacks map[string][]*RollbackEntry
}

// NewInMemoryStore creates a new in-memory store
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		actions:   make(map[string]*AutonomousAction),
		rollbacks: make(map[string][]*RollbackEntry),
	}
}

// SaveAction saves an action
func (s *InMemoryStore) SaveAction(action *AutonomousAction) error {
	s.actions[action.ID] = action
	return nil
}

// GetAction retrieves an action
func (s *InMemoryStore) GetAction(id string) (*AutonomousAction, error) {
	action, exists := s.actions[id]
	if !exists {
		return nil, nil
	}
	return action, nil
}

// ListActions lists actions
func (s *InMemoryStore) ListActions(limit int) ([]*AutonomousAction, error) {
	var actions []*AutonomousAction
	for _, action := range s.actions {
		actions = append(actions, action)
	}
	if limit > 0 && len(actions) > limit {
		actions = actions[:limit]
	}
	return actions, nil
}

// UpdateAction updates an action
func (s *InMemoryStore) UpdateAction(action *AutonomousAction) error {
	s.actions[action.ID] = action
	return nil
}

// SaveRollback saves a rollback entry
func (s *InMemoryStore) SaveRollback(entry *RollbackEntry) error {
	s.rollbacks[entry.ActionID] = append(s.rollbacks[entry.ActionID], entry)
	return nil
}

// ListRollbacks lists rollbacks for an action
func (s *InMemoryStore) ListRollbacks(actionID string) ([]*RollbackEntry, error) {
	return s.rollbacks[actionID], nil
}

// CleanupOld removes old entries
func (s *InMemoryStore) CleanupOld(olderThan time.Time) error {
	for id, action := range s.actions {
		if action.CreatedAt.Before(olderThan) {
			delete(s.actions, id)
			delete(s.rollbacks, id)
		}
	}
	return nil
}
