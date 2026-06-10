package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/autonomy"
)

// AutonomyStore implements autonomy.Store for SQLite
type AutonomyStore struct {
	db *sql.DB
}

// NewAutonomyStore creates a new autonomy store
func NewAutonomyStore(db *sql.DB) *AutonomyStore {
	return &AutonomyStore{db: db}
}

// SaveAction saves an action
func (s *AutonomyStore) SaveAction(action *autonomy.AutonomousAction) error {
	metadata, _ := json.Marshal(action.Metadata)

	_, err := s.db.Exec(`
		INSERT INTO autonomous_actions (id, action_type, status, safety_level, resource_id, trigger, reason, rollback_supported, dry_run, started_at, completed_at, created_at, updated_at, result, error, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at,
			result = excluded.result,
			error = excluded.error,
			updated_at = excluded.updated_at
	`, action.ID, action.Type, action.Status, action.SafetyLevel, action.ResourceID, action.Trigger, action.Reason,
		action.RollbackSupported, action.DryRun, action.StartedAt, action.CompletedAt, action.CreatedAt, action.UpdatedAt, action.Result, action.Error, string(metadata))

	return err
}

// GetAction retrieves an action
func (s *AutonomyStore) GetAction(id string) (*autonomy.AutonomousAction, error) {
	var action autonomy.AutonomousAction
	var metadataJSON sql.NullString

	err := s.db.QueryRow(`
		SELECT id, action_type, status, safety_level, resource_id, trigger, reason, rollback_supported, dry_run, started_at, completed_at, created_at, updated_at, result, error, metadata_json
		FROM autonomous_actions WHERE id = ?
	`, id).Scan(&action.ID, &action.Type, &action.Status, &action.SafetyLevel, &action.ResourceID, &action.Trigger, &action.Reason,
		&action.RollbackSupported, &action.DryRun, &action.StartedAt, &action.CompletedAt, &action.CreatedAt, &action.UpdatedAt, &action.Result, &action.Error, &metadataJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	action.Metadata = make(map[string]string)
	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &action.Metadata)
	}

	return &action, nil
}

// ListActions lists actions
func (s *AutonomyStore) ListActions(limit int) ([]*autonomy.AutonomousAction, error) {
	query := `SELECT id, action_type, status, safety_level, resource_id, trigger, reason, rollback_supported, dry_run, started_at, completed_at, created_at, updated_at, result, error, metadata_json
		FROM autonomous_actions ORDER BY created_at DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []*autonomy.AutonomousAction
	for rows.Next() {
		var action autonomy.AutonomousAction
		var metadataJSON sql.NullString

		if err := rows.Scan(&action.ID, &action.Type, &action.Status, &action.SafetyLevel, &action.ResourceID, &action.Trigger, &action.Reason,
			&action.RollbackSupported, &action.DryRun, &action.StartedAt, &action.CompletedAt, &action.CreatedAt, &action.UpdatedAt, &action.Result, &action.Error, &metadataJSON); err != nil {
			return nil, err
		}

		action.Metadata = make(map[string]string)
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &action.Metadata)
		}

		actions = append(actions, &action)
	}

	return actions, rows.Err()
}

// UpdateAction updates an action
func (s *AutonomyStore) UpdateAction(action *autonomy.AutonomousAction) error {
	metadata, _ := json.Marshal(action.Metadata)

	_, err := s.db.Exec(`
		UPDATE autonomous_actions SET
			status = ?,
			started_at = ?,
			completed_at = ?,
			result = ?,
			error = ?,
			updated_at = ?,
			metadata_json = ?
		WHERE id = ?
	`, action.Status, action.StartedAt, action.CompletedAt, action.Result, action.Error, action.UpdatedAt, string(metadata), action.ID)

	return err
}

// SaveRollback saves a rollback entry
func (s *AutonomyStore) SaveRollback(entry *autonomy.RollbackEntry) error {
	_, err := s.db.Exec(`
		INSERT INTO autonomous_rollbacks (id, action_id, success, timestamp, reason, result)
		VALUES (?, ?, ?, ?, ?, ?)
	`, entry.ActionID+"-rollback-"+time.Now().Format("20060102150405"), entry.ActionID, entry.Success, entry.Timestamp, entry.Reason, entry.Result)

	return err
}

// ListRollbacks lists rollbacks
func (s *AutonomyStore) ListRollbacks(actionID string) ([]*autonomy.RollbackEntry, error) {
	rows, err := s.db.Query(`
		SELECT action_id, success, timestamp, reason, result
		FROM autonomous_rollbacks WHERE action_id = ? ORDER BY timestamp DESC
	`, actionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*autonomy.RollbackEntry
	for rows.Next() {
		var entry autonomy.RollbackEntry
		if err := rows.Scan(&entry.ActionID, &entry.Success, &entry.Timestamp, &entry.Reason, &entry.Result); err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// CleanupOld removes old entries
func (s *AutonomyStore) CleanupOld(olderThan time.Time) error {
	_, err := s.db.Exec(`DELETE FROM autonomous_actions WHERE created_at < ?`, olderThan)
	return err
}
