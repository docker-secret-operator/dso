package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteProvider implements storage.StorageProvider
type SQLiteProvider struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex

	// Store implementations
	drafts               *DraftStore
	reviews              *ReviewStore
	approvals            *ApprovalStore
	activities           *ReviewActivityStore
	snapshots            *SnapshotStore
	audit                *AuditStore
	executionRequests    *ExecutionRequestStore
	executionPlans       *ExecutionPlanStore
	executionSteps       *ExecutionStepStore
	executionResults     *ExecutionResultStore
	stepResults          *StepResultStore
	workerHeartbeats     *WorkerHeartbeatStore
	users                *UserStore
	sessions             *SessionStore
	securityEvents       *SecurityEventStore
	suspiciousActivities *SuspiciousActivityStore
	securityAlerts       *SecurityAlertStore
	alertRules           *AlertRuleStore
	alerts               *AlertStore
	backups              *BackupStore
	plugins              *PluginStore
	integrationConfigs   *IntegrationConfigStore
	integrationDeliveries *IntegrationDeliveryStore
	schedulerJobs        storage.SchedulerStore
	schedulerExecutions  storage.ExecutionStore
	rules                storage.RuleStore
	drift                storage.DriftStore
}

// NewSQLiteProvider creates a new SQLite storage provider
func NewSQLiteProvider(path string) (*SQLiteProvider, error) {
	if path == "" {
		path = "data/dso.db"
	}

	// Open with WAL mode enabled. Private cache (no cache=shared) avoids SQLITE_LOCKED
	// under concurrent writers; WAL + busy_timeout serialises contention gracefully.
	connStr := fmt.Sprintf("file:%s?mode=rwc&_journal_mode=WAL&_busy_timeout=5000", path)
	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// SQLite WAL mode allows concurrent readers but only one writer at a time.
	// Limiting to 1 open connection prevents write contention and busy-timeout errors.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	provider.executionRequests = &ExecutionRequestStore{db: &SQLiteDB{DB: db}}
	provider.executionPlans = &ExecutionPlanStore{db: &SQLiteDB{DB: db}}
	provider.executionSteps = &ExecutionStepStore{db: &SQLiteDB{DB: db}}
	provider.executionResults = &ExecutionResultStore{db: &SQLiteDB{DB: db}}
	provider.stepResults = &StepResultStore{db: &SQLiteDB{DB: db}}
	provider.workerHeartbeats = &WorkerHeartbeatStore{db: &SQLiteDB{DB: db}}
	provider.users = &UserStore{db: db}
	provider.sessions = &SessionStore{db: db}
	provider.securityEvents = &SecurityEventStore{db: db}
	provider.suspiciousActivities = &SuspiciousActivityStore{db: db}
	provider.securityAlerts = &SecurityAlertStore{db: db}
	provider.alertRules = &AlertRuleStore{db: db}
	provider.alerts = &AlertStore{db: db}
	provider.backups = &BackupStore{db: db}
	provider.plugins = &PluginStore{db: db}
	provider.integrationConfigs = &IntegrationConfigStore{db: db}
	provider.integrationDeliveries = &IntegrationDeliveryStore{db: db}
	provider.schedulerJobs = &SchedulerJobStore{db: db}
	provider.schedulerExecutions = &ExecutionHistoryStore{db: db}
	provider.rules = &RuleStore{db: db}
	provider.drift = &DriftStore{db: db}

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

// ExecutionRequests returns the execution request store
func (sp *SQLiteProvider) ExecutionRequests() storage.ExecutionRequestStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.executionRequests
}

// ExecutionPlans returns the execution plan store
func (sp *SQLiteProvider) ExecutionPlans() storage.ExecutionPlanStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.executionPlans
}

// ExecutionSteps returns the execution step store
func (sp *SQLiteProvider) ExecutionSteps() storage.ExecutionStepStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.executionSteps
}

// ExecutionResults returns the execution result store
func (sp *SQLiteProvider) ExecutionResults() storage.ExecutionResultStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.executionResults
}

// StepResults returns the step result store
func (sp *SQLiteProvider) StepResults() storage.StepResultStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.stepResults
}

// WorkerHeartbeats returns the worker heartbeat store
func (sp *SQLiteProvider) WorkerHeartbeats() storage.WorkerHeartbeatStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.workerHeartbeats
}

// Users returns the user store
func (sp *SQLiteProvider) Users() storage.UserStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.users
}

// Sessions returns the session store
func (sp *SQLiteProvider) Sessions() storage.SessionStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.sessions
}

// SecurityEvents returns the security event store
func (sp *SQLiteProvider) SecurityEvents() storage.SecurityEventStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.securityEvents
}

// SuspiciousActivities returns the suspicious activity store
func (sp *SQLiteProvider) SuspiciousActivities() storage.SuspiciousActivityStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.suspiciousActivities
}

// SecurityAlerts returns the security alert store
func (sp *SQLiteProvider) SecurityAlerts() storage.SecurityAlertStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.securityAlerts
}

// AlertRules returns the alert rule store
func (sp *SQLiteProvider) AlertRules() storage.AlertRuleStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.alertRules
}

// Alerts returns the alert store
func (sp *SQLiteProvider) Alerts() storage.AlertStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.alerts
}

// Backups returns the backup store
func (sp *SQLiteProvider) Backups() storage.BackupStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.backups
}

func (sp *SQLiteProvider) Plugins() storage.PluginStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.plugins
}

func (sp *SQLiteProvider) IntegrationConfigs() storage.IntegrationConfigStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.integrationConfigs
}

func (sp *SQLiteProvider) IntegrationDeliveries() storage.IntegrationDeliveryStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.integrationDeliveries
}

func (sp *SQLiteProvider) SchedulerJobs() storage.SchedulerStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.schedulerJobs
}

func (sp *SQLiteProvider) SchedulerExecutions() storage.ExecutionStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.schedulerExecutions
}

func (sp *SQLiteProvider) Rules() storage.RuleStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.rules
}

func (sp *SQLiteProvider) Drift() storage.DriftStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.drift
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
		tx:                tx,
		drafts:            &DraftStore{db: tx},
		reviews:           &ReviewStore{db: tx},
		approvals:         &ApprovalStore{db: tx},
		activities:        &ReviewActivityStore{db: tx},
		snapshots:         &SnapshotStore{db: tx},
		audit:             &AuditStore{db: tx},
		executionRequests: &ExecutionRequestStore{db: &SQLiteDB{DB: tx}},
		executionPlans:    &ExecutionPlanStore{db: &SQLiteDB{DB: tx}},
		executionSteps:    &ExecutionStepStore{db: &SQLiteDB{DB: tx}},
		executionResults:  &ExecutionResultStore{db: &SQLiteDB{DB: tx}},
		stepResults:       &StepResultStore{db: &SQLiteDB{DB: tx}},
		workerHeartbeats:  &WorkerHeartbeatStore{db: &SQLiteDB{DB: tx}},
		users:             &UserStore{db: tx},
		sessions:          &SessionStore{db: tx},
		securityEvents:    &SecurityEventStore{db: tx},
		suspiciousActivities: &SuspiciousActivityStore{db: tx},
		securityAlerts:    &SecurityAlertStore{db: tx},
		alertRules:        &AlertRuleStore{db: tx},
		alerts:            &AlertStore{db: tx},
		backups:               &BackupStore{db: tx},
		plugins:               &PluginStore{db: tx},
		integrationConfigs:    &IntegrationConfigStore{db: tx},
		integrationDeliveries: &IntegrationDeliveryStore{db: tx},
		schedulerJobs:         &SchedulerJobStore{db: tx},
		schedulerExecutions:   &ExecutionHistoryStore{db: tx},
		rules:                 &RuleStore{db: tx},
		drift:                 &DriftStore{db: tx},
	}, nil
}

// SQLiteTransaction implements storage.Transaction
type SQLiteTransaction struct {
	tx                *sql.Tx
	drafts            *DraftStore
	reviews           *ReviewStore
	approvals         *ApprovalStore
	activities        *ReviewActivityStore
	snapshots         *SnapshotStore
	audit             *AuditStore
	executionRequests *ExecutionRequestStore
	executionPlans    *ExecutionPlanStore
	executionSteps    *ExecutionStepStore
	executionResults  *ExecutionResultStore
	stepResults       *StepResultStore
	workerHeartbeats  *WorkerHeartbeatStore
	users             *UserStore
	sessions          *SessionStore
	securityEvents    *SecurityEventStore
	suspiciousActivities *SuspiciousActivityStore
	securityAlerts    *SecurityAlertStore
	alertRules            *AlertRuleStore
	alerts                *AlertStore
	backups               *BackupStore
	plugins               *PluginStore
	integrationConfigs    *IntegrationConfigStore
	integrationDeliveries *IntegrationDeliveryStore
	schedulerJobs         storage.SchedulerStore
	schedulerExecutions   storage.ExecutionStore
	rules                 storage.RuleStore
	drift                 storage.DriftStore
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

func (st *SQLiteTransaction) ExecutionRequests() storage.ExecutionRequestStore {
	return st.executionRequests
}

func (st *SQLiteTransaction) ExecutionPlans() storage.ExecutionPlanStore {
	return st.executionPlans
}

func (st *SQLiteTransaction) ExecutionSteps() storage.ExecutionStepStore {
	return st.executionSteps
}

func (st *SQLiteTransaction) ExecutionResults() storage.ExecutionResultStore {
	return st.executionResults
}

func (st *SQLiteTransaction) StepResults() storage.StepResultStore {
	return st.stepResults
}

func (st *SQLiteTransaction) WorkerHeartbeats() storage.WorkerHeartbeatStore {
	return st.workerHeartbeats
}

func (st *SQLiteTransaction) Users() storage.UserStore {
	return st.users
}

func (st *SQLiteTransaction) Sessions() storage.SessionStore {
	return st.sessions
}

func (st *SQLiteTransaction) SecurityEvents() storage.SecurityEventStore {
	return st.securityEvents
}

func (st *SQLiteTransaction) SuspiciousActivities() storage.SuspiciousActivityStore {
	return st.suspiciousActivities
}

func (st *SQLiteTransaction) SecurityAlerts() storage.SecurityAlertStore {
	return st.securityAlerts
}

func (st *SQLiteTransaction) AlertRules() storage.AlertRuleStore {
	return st.alertRules
}

func (st *SQLiteTransaction) Alerts() storage.AlertStore {
	return st.alerts
}

func (st *SQLiteTransaction) Backups() storage.BackupStore {
	return st.backups
}

func (st *SQLiteTransaction) Plugins() storage.PluginStore {
	return st.plugins
}

func (st *SQLiteTransaction) IntegrationConfigs() storage.IntegrationConfigStore {
	return st.integrationConfigs
}

func (st *SQLiteTransaction) IntegrationDeliveries() storage.IntegrationDeliveryStore {
	return st.integrationDeliveries
}

func (st *SQLiteTransaction) SchedulerJobs() storage.SchedulerStore {
	return st.schedulerJobs
}

func (st *SQLiteTransaction) SchedulerExecutions() storage.ExecutionStore {
	return st.schedulerExecutions
}

func (st *SQLiteTransaction) Rules() storage.RuleStore {
	return st.rules
}

func (st *SQLiteTransaction) Drift() storage.DriftStore {
	return st.drift
}

// DBProvider is an interface for the underlying database (sql.DB or sql.Tx)
type DBProvider interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// SQLiteDB wraps sql.DB or sql.Tx for store implementations
type SQLiteDB struct {
	DB DBProvider
}

func (s *SQLiteDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return s.DB.QueryContext(ctx, query, args...)
}

func (s *SQLiteDB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return s.DB.QueryRowContext(ctx, query, args...)
}

func (s *SQLiteDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return s.DB.ExecContext(ctx, query, args...)
}

// GetDB returns the underlying database connection
func (sp *SQLiteProvider) GetDB() *sql.DB {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.db
}

// PrepareContext is added to support prepared statements
// This uses reflection to handle both *sql.DB and *sql.Tx
func (s *SQLiteDB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	// Handle *sql.DB
	if db, ok := s.DB.(*sql.DB); ok {
		return db.PrepareContext(ctx, query)
	}
	// Handle *sql.Tx
	if tx, ok := s.DB.(*sql.Tx); ok {
		return tx.PrepareContext(ctx, query)
	}
	return nil, fmt.Errorf("unsupported DB provider type")
}
