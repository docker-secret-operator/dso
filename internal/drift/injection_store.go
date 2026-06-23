package drift

import (
	"context"
	"sync"
	"time"
)

// InjectionRecord stores the hash that was last successfully injected for a secret.
type InjectionRecord struct {
	SecretName   string
	ProviderHash string // SHA-256 of the value at injection time
	InjectedAt   time.Time
}

// InjectionStore persists injection records across restarts.
type InjectionStore interface {
	RecordInjection(ctx context.Context, secretName, providerHash string) error
	GetRecord(ctx context.Context, secretName string) (*InjectionRecord, error)
	ListRecords(ctx context.Context) ([]InjectionRecord, error)
}

// InMemoryInjectionStore is a non-persistent InjectionStore used when SQLite is unavailable.
type InMemoryInjectionStore struct {
	mu      sync.RWMutex
	records map[string]InjectionRecord
}

func NewInMemoryInjectionStore() *InMemoryInjectionStore {
	return &InMemoryInjectionStore{records: make(map[string]InjectionRecord)}
}

func (s *InMemoryInjectionStore) RecordInjection(_ context.Context, secretName, providerHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[secretName] = InjectionRecord{
		SecretName:   secretName,
		ProviderHash: providerHash,
		InjectedAt:   time.Now(),
	}
	return nil
}

func (s *InMemoryInjectionStore) GetRecord(_ context.Context, secretName string) (*InjectionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[secretName]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (s *InMemoryInjectionStore) ListRecords(_ context.Context) ([]InjectionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]InjectionRecord, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out, nil
}
