package policy

import (
	"context"
	"sync"
	"time"
)

// InMemoryStore is a non-persistent implementation of RuleStore.
//
// It exists so the policy Engine can be wired into the server without a SQLite
// backing table yet. Data does not survive a restart. Replace with a SQLite
// store (see internal/storage/sqlite) when policy persistence is implemented.
type InMemoryStore struct {
	mu         sync.RWMutex
	rules      map[string]*Rule
	executions []*RuleExecution
}

// NewInMemoryStore creates an empty in-memory policy rule store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{rules: make(map[string]*Rule)}
}

func (s *InMemoryStore) CreateRule(_ context.Context, rule *Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[rule.ID] = rule
	return nil
}

func (s *InMemoryStore) UpdateRule(_ context.Context, rule *Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[rule.ID] = rule
	return nil
}

func (s *InMemoryStore) GetRule(_ context.Context, id string) (*Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rules[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (s *InMemoryStore) ListRules(_ context.Context) ([]*Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Rule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, r)
	}
	return out, nil
}

func (s *InMemoryStore) DeleteRule(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rules, id)
	return nil
}

func (s *InMemoryStore) LogExecution(_ context.Context, execution *RuleExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executions = append(s.executions, execution)
	return nil
}

func (s *InMemoryStore) GetExecutions(_ context.Context, ruleID string, limit int) ([]*RuleExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*RuleExecution, 0)
	for i := len(s.executions) - 1; i >= 0; i-- {
		if ruleID != "" && s.executions[i].RuleID != ruleID {
			continue
		}
		out = append(out, s.executions[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *InMemoryStore) CleanupOldExecutions(_ context.Context, _ time.Time) error {
	return nil
}
