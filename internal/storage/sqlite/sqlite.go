package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/docker-secret-operator/dso/internal/storage"
)

// SQLiteProvider implements storage.StorageProvider
type SQLiteProvider struct {
	db    *sql.DB
	path  string
	mu    sync.RWMutex

	// Store implementations
	drafts    *DraftStore
	reviews   *ReviewStore
	approvals *ApprovalStore
	activities *ReviewActivityStore
	snapshots *SnapshotStore
	audit     *AuditStore
}

// NewSQLiteProvider creates a new SQLite storage provider
func NewSQLiteProvider(path string) (*SQLiteProvider, error) {
	if path == "" {
		path = "data/dso.db"
	}

	// Open with WAL mode enabled
	connStr := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=5000", path)
	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5000)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite database: %w", err)
	}

	provider := &SQLiteProvider{
		db:   db,
		path: path,
	}

	// Initialize store implementations
	provider.drafts = &DraftStore{db: db}
	provider.reviews = &ReviewStore{db: db}
	provider.approvals = &ApprovalStore{db: db}
	provider.activities = &ReviewActivityStore{db: db}
	provider.snapshots = &SnapshotStore{db: db}
	provider.audit = &AuditStore{db: db}

	// Run migrations
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return provider, nil
}

// Drafts returns the draft store
func (sp *SQLiteProvider) Drafts() storage.DraftStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.drafts
}

// Reviews returns the review store
func (sp *SQLiteProvider) Reviews() storage.ReviewStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.reviews
}

// Approvals returns the approval store
func (sp *SQLiteProvider) Approvals() storage.ApprovalStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.approvals
}

// ReviewActivities returns the review activity store
func (sp *SQLiteProvider) ReviewActivities() storage.ReviewActivityStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.activities
}

// Snapshots returns the snapshot store
func (sp *SQLiteProvider) Snapshots() storage.SnapshotStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.snapshots
}

// Audit returns the audit store
func (sp *SQLiteProvider) Audit() storage.AuditStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.audit
}

// Health checks the database connection
func (sp *SQLiteProvider) Health(ctx context.Context) error {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if sp.db == nil {
		return fmt.Errorf("database not initialized")
	}

	if err := sp.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}

	// Check WAL mode
	var mode string
	if err := sp.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("failed to check journal mode: %w", err)
	}

	if mode != "wal" {
		return fmt.Errorf("WAL mode not enabled (got: %s)", mode)
	}

	return nil
}

// Close closes the database connection
func (sp *SQLiteProvider) Close(ctx context.Context) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.db != nil {
		if err := sp.db.Close(); err != nil {
			return fmt.Errorf("failed to close database: %w", err)
		}
		sp.db = nil
	}

	return nil
}

// BeginTx begins a new transaction
func (sp *SQLiteProvider) BeginTx(ctx context.Context) (storage.Transaction, error) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if sp.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	tx, err := sp.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &SQLiteTransaction{
		tx: tx,
		drafts: &DraftStore{db: tx},
		reviews: &ReviewStore{db: tx},
		approvals: &ApprovalStore{db: tx},
		activities: &ReviewActivityStore{db: tx},
		snapshots: &SnapshotStore{db: tx},
		audit: &AuditStore{db: tx},
	}, nil
}

// SQLiteTransaction implements storage.Transaction
type SQLiteTransaction struct {
	tx        *sql.Tx
	drafts    *DraftStore
	reviews   *ReviewStore
	approvals *ApprovalStore
	activities *ReviewActivityStore
	snapshots *SnapshotStore
	audit     *AuditStore
}

// Commit commits the transaction
func (st *SQLiteTransaction) Commit(ctx context.Context) error {
	if err := st.tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// Rollback rolls back the transaction
func (st *SQLiteTransaction) Rollback(ctx context.Context) error {
	if err := st.tx.Rollback(); err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}
	return nil
}

func (st *SQLiteTransaction) Drafts() storage.DraftStore {
	return st.drafts
}

func (st *SQLiteTransaction) Reviews() storage.ReviewStore {
	return st.reviews
}

func (st *SQLiteTransaction) Approvals() storage.ApprovalStore {
	return st.approvals
}

func (st *SQLiteTransaction) ReviewActivities() storage.ReviewActivityStore {
	return st.activities
}

func (st *SQLiteTransaction) Snapshots() storage.SnapshotStore {
	return st.snapshots
}

func (st *SQLiteTransaction) Audit() storage.AuditStore {
	return st.audit
}

// DBProvider is an interface for the underlying database (sql.DB or sql.Tx)
type DBProvider interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}
