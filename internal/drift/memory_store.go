package drift

import (
	"context"
	"sync"
	"time"
)

// InMemoryStore is a non-persistent implementation of Store.
//
// It exists so the drift Engine can be wired into the server without a SQLite
// backing table yet. Data does not survive a restart. Replace with a SQLite
// store (see internal/storage/sqlite) when drift persistence is implemented.
type InMemoryStore struct {
	mu       sync.RWMutex
	findings map[string]DriftFinding
	scans    []*DriftScan
}

// NewInMemoryStore creates an empty in-memory drift store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{findings: make(map[string]DriftFinding)}
}

func (s *InMemoryStore) CreateFinding(_ context.Context, finding DriftFinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findings[finding.ID] = finding
	return nil
}

func (s *InMemoryStore) UpdateFinding(_ context.Context, finding DriftFinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findings[finding.ID] = finding
	return nil
}

func (s *InMemoryStore) GetFinding(_ context.Context, id string) (*DriftFinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.findings[id]
	if !ok {
		return nil, nil
	}
	return &f, nil
}

func (s *InMemoryStore) ListFindings(_ context.Context) ([]DriftFinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DriftFinding, 0, len(s.findings))
	for _, f := range s.findings {
		out = append(out, f)
	}
	return out, nil
}

func (s *InMemoryStore) DeleteFinding(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.findings, id)
	return nil
}

func (s *InMemoryStore) LogScan(_ context.Context, scan *DriftScan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scans = append(s.scans, scan)
	return nil
}

func (s *InMemoryStore) GetScans(_ context.Context, limit int) ([]*DriftScan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.scans) {
		limit = len(s.scans)
	}
	// Most-recent first.
	out := make([]*DriftScan, 0, limit)
	for i := len(s.scans) - 1; i >= len(s.scans)-limit; i-- {
		out = append(out, s.scans[i])
	}
	return out, nil
}

func (s *InMemoryStore) CleanupOldFindings(_ context.Context, _ time.Time) error {
	return nil
}
