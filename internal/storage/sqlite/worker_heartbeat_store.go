package sqlite

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// WorkerHeartbeatStore implements storage.WorkerHeartbeatStore for SQLite
type WorkerHeartbeatStore struct {
	db *SQLiteDB
}

// Create creates a new worker heartbeat record
func (whs *WorkerHeartbeatStore) Create(ctx context.Context, heartbeat *storage.WorkerHeartbeat) error {
	query := `
		INSERT INTO worker_heartbeats (id, worker_id, timestamp, state, running_steps, completed_count, failed_count, last_error, system_load, memory_usage, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := whs.db.ExecContext(ctx, query,
		heartbeat.ID, heartbeat.WorkerID, heartbeat.Timestamp, heartbeat.State,
		heartbeat.RunningSteps, heartbeat.CompletedCount, heartbeat.FailedCount,
		heartbeat.LastError, heartbeat.SystemLoad, heartbeat.MemoryUsage, heartbeat.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to create worker heartbeat: %w", err)
	}

	return nil
}

// GetByID retrieves a heartbeat by ID
func (whs *WorkerHeartbeatStore) GetByID(ctx context.Context, id string) (*storage.WorkerHeartbeat, error) {
	query := `
		SELECT id, worker_id, timestamp, state, running_steps, completed_count, failed_count, last_error, system_load, memory_usage, version
		FROM worker_heartbeats WHERE id = ?
	`

	row := whs.db.QueryRowContext(ctx, query, id)
	heartbeat := &storage.WorkerHeartbeat{}

	err := row.Scan(
		&heartbeat.ID, &heartbeat.WorkerID, &heartbeat.Timestamp, &heartbeat.State,
		&heartbeat.RunningSteps, &heartbeat.CompletedCount, &heartbeat.FailedCount,
		&heartbeat.LastError, &heartbeat.SystemLoad, &heartbeat.MemoryUsage, &heartbeat.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get worker heartbeat: %w", err)
	}

	return heartbeat, nil
}

// ListByWorker lists heartbeats for a worker (most recent first)
func (whs *WorkerHeartbeatStore) ListByWorker(ctx context.Context, workerID string, limit int) ([]*storage.WorkerHeartbeat, error) {
	query := `
		SELECT id, worker_id, timestamp, state, running_steps, completed_count, failed_count, last_error, system_load, memory_usage, version
		FROM worker_heartbeats
		WHERE worker_id = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := whs.db.QueryContext(ctx, query, workerID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list worker heartbeats: %w", err)
	}
	defer rows.Close()

	heartbeats := make([]*storage.WorkerHeartbeat, 0)
	for rows.Next() {
		heartbeat := &storage.WorkerHeartbeat{}
		err := rows.Scan(
			&heartbeat.ID, &heartbeat.WorkerID, &heartbeat.Timestamp, &heartbeat.State,
			&heartbeat.RunningSteps, &heartbeat.CompletedCount, &heartbeat.FailedCount,
			&heartbeat.LastError, &heartbeat.SystemLoad, &heartbeat.MemoryUsage, &heartbeat.Version,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan worker heartbeat: %w", err)
		}

		heartbeats = append(heartbeats, heartbeat)
	}

	return heartbeats, nil
}

// GetLatestByWorker returns the most recent heartbeat for a worker
func (whs *WorkerHeartbeatStore) GetLatestByWorker(ctx context.Context, workerID string) (*storage.WorkerHeartbeat, error) {
	query := `
		SELECT id, worker_id, timestamp, state, running_steps, completed_count, failed_count, last_error, system_load, memory_usage, version
		FROM worker_heartbeats
		WHERE worker_id = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	row := whs.db.QueryRowContext(ctx, query, workerID)
	heartbeat := &storage.WorkerHeartbeat{}

	err := row.Scan(
		&heartbeat.ID, &heartbeat.WorkerID, &heartbeat.Timestamp, &heartbeat.State,
		&heartbeat.RunningSteps, &heartbeat.CompletedCount, &heartbeat.FailedCount,
		&heartbeat.LastError, &heartbeat.SystemLoad, &heartbeat.MemoryUsage, &heartbeat.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get latest worker heartbeat: %w", err)
	}

	return heartbeat, nil
}
