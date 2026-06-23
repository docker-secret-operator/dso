package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/drift"
)

type sqliteInjectionStore struct {
	db *sql.DB
}

// NewInjectionStore returns a drift.InjectionStore backed by SQLite.
func NewInjectionStore(db *sql.DB) drift.InjectionStore {
	return &sqliteInjectionStore{db: db}
}

func (s *sqliteInjectionStore) RecordInjection(ctx context.Context, secretName, providerHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO injection_records (secret_name, provider_hash, injected_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(secret_name) DO UPDATE SET
			 provider_hash = excluded.provider_hash,
			 injected_at   = excluded.injected_at`,
		secretName, providerHash, time.Now())
	return err
}

func (s *sqliteInjectionStore) GetRecord(ctx context.Context, secretName string) (*drift.InjectionRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT secret_name, provider_hash, injected_at FROM injection_records WHERE secret_name = ?`,
		secretName)
	r := &drift.InjectionRecord{}
	if err := row.Scan(&r.SecretName, &r.ProviderHash, &r.InjectedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("injection_records scan: %w", err)
	}
	return r, nil
}

func (s *sqliteInjectionStore) ListRecords(ctx context.Context) ([]drift.InjectionRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT secret_name, provider_hash, injected_at FROM injection_records ORDER BY injected_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("injection_records list: %w", err)
	}
	defer rows.Close()

	var out []drift.InjectionRecord
	for rows.Next() {
		var r drift.InjectionRecord
		if err := rows.Scan(&r.SecretName, &r.ProviderHash, &r.InjectedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
