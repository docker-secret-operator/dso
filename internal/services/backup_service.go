package services

import (
	"compress/gzip"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	backupDir       = "data/backups"
	backupRetention = 30 * 24 * time.Hour
)

// BackupService manages database backups and restores
type BackupService struct {
	store          storage.StorageProvider
	logger         *zap.Logger
	dbPath         string
	backupMutex    sync.Mutex
	stopCh         chan struct{}
	wg             sync.WaitGroup
	running        bool
	mu             sync.Mutex
}

// NewBackupService creates a new backup service
func NewBackupService(store storage.StorageProvider, logger *zap.Logger, dbPath *string) *BackupService {
	if logger == nil {
		logger = zap.NewNop()
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		logger.Warn("failed to create backup directory", zap.Error(err))
	}
	path := "data/dso.db"
	if dbPath != nil {
		path = *dbPath
	}
	return &BackupService{
		store:   store,
		logger:  logger,
		dbPath:  path,
		stopCh:  make(chan struct{}),
	}
}

// Start begins the background backup worker
func (bs *BackupService) Start(ctx context.Context) error {
	bs.mu.Lock()
	if bs.running {
		bs.mu.Unlock()
		return fmt.Errorf("backup service already running")
	}
	bs.running = true
	bs.mu.Unlock()

	bs.wg.Add(1)
	go bs.scheduledBackupWorker(ctx)
	bs.logger.Info("Backup service started")
	return nil
}

// Stop gracefully shuts down the backup worker
func (bs *BackupService) Stop() {
	bs.mu.Lock()
	if !bs.running {
		bs.mu.Unlock()
		return
	}
	bs.running = false
	bs.mu.Unlock()

	close(bs.stopCh)
	bs.wg.Wait()
	bs.logger.Info("Backup service stopped")
}

// scheduledBackupWorker periodically creates backups
func (bs *BackupService) scheduledBackupWorker(ctx context.Context) {
	defer bs.wg.Done()

	// Run backup at 2 AM daily
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Calculate time until next 2 AM
	now := time.Now()
	next2AM := now.AddDate(0, 0, 1)
	next2AM = time.Date(next2AM.Year(), next2AM.Month(), next2AM.Day(), 2, 0, 0, 0, next2AM.Location())
	initialWait := next2AM.Sub(now)

	initialTimer := time.NewTimer(initialWait)
	defer initialTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopCh:
			return
		case <-initialTimer.C:
			if err := bs.CreateBackup(ctx, "scheduled"); err != nil {
				bs.logger.Error("scheduled backup failed", zap.Error(err))
			}
			ticker.Reset(24 * time.Hour)
		case <-ticker.C:
			if err := bs.CreateBackup(ctx, "scheduled"); err != nil {
				bs.logger.Error("scheduled backup failed", zap.Error(err))
			}
		}
	}
}

// CreateBackup creates a new backup
func (bs *BackupService) CreateBackup(ctx context.Context, backupType string) error {
	bs.backupMutex.Lock()
	defer bs.backupMutex.Unlock()

	backupID := uuid.New().String()
	now := time.Now()
	filename := fmt.Sprintf("dso-backup-%s.db.gz", now.Format("20060102-150405"))
	filepath := filepath.Join(backupDir, filename)

	// Create backup metadata entry
	backup := &storage.Backup{
		ID:         backupID,
		Filename:   filename,
		BackupType: backupType,
		Status:     "running",
		CreatedAt:  now,
	}

	if err := bs.store.Backups().Create(ctx, backup); err != nil {
		return fmt.Errorf("failed to create backup metadata: %w", err)
	}

	startTime := time.Now()

	// Create backup file
	checksum, size, err := bs.createBackupFile(filepath)
	if err != nil {
		duration := int(time.Since(startTime).Milliseconds())
		errMsg := err.Error()
		backup.Status = "failed"
		backup.ErrorMsg = &errMsg
		backup.DurationMs = duration
		bs.store.Backups().Update(ctx, backup)
		bs.logger.Error("failed to create backup file", zap.Error(err))
		return err
	}

	duration := int(time.Since(startTime).Milliseconds())
	completedAt := time.Now()

	backup.Status = "completed"
	backup.Checksum = checksum
	backup.SizeBytes = size
	backup.DurationMs = duration
	backup.CompletedAt = &completedAt

	if err := bs.store.Backups().Update(ctx, backup); err != nil {
		return fmt.Errorf("failed to update backup metadata: %w", err)
	}

	// Clean up old backups
	if err := bs.cleanupOldBackups(ctx); err != nil {
		bs.logger.Warn("failed to cleanup old backups", zap.Error(err))
	}

	bs.logger.Info("Backup created successfully",
		zap.String("backup_id", backupID),
		zap.String("filename", filename),
		zap.Int64("size_bytes", size),
		zap.Int("duration_ms", duration))

	return nil
}

// createBackupFile creates a compressed backup file
func (bs *BackupService) createBackupFile(filepath string) (string, int64, error) {
	// Create temp file for atomic write
	tempFile := filepath + ".tmp"

	outFile, err := os.Create(tempFile)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer outFile.Close()

	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	// Copy database to backup
	srcDB, err := os.Open(bs.dbPath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to open source database: %w", err)
	}
	defer srcDB.Close()

	// Create MD5 hash while copying
	hash := md5.New()
	multiWriter := io.MultiWriter(gzWriter, hash)

	_, err = io.Copy(multiWriter, srcDB)
	if err != nil {
		os.Remove(tempFile)
		return "", 0, fmt.Errorf("failed to copy database: %w", err)
	}

	if err := gzWriter.Close(); err != nil {
		os.Remove(tempFile)
		return "", 0, fmt.Errorf("failed to finalize backup: %w", err)
	}

	if err := outFile.Close(); err != nil {
		os.Remove(tempFile)
		return "", 0, fmt.Errorf("failed to close backup file: %w", err)
	}

	// Atomically move temp file to final location
	if err := os.Rename(tempFile, filepath); err != nil {
		os.Remove(tempFile)
		return "", 0, fmt.Errorf("failed to finalize backup file: %w", err)
	}

	// Get actual file size
	info, err := os.Stat(filepath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to stat backup file: %w", err)
	}

	checksum := fmt.Sprintf("%x", hash.Sum(nil))
	return checksum, info.Size(), nil
}

// cleanupOldBackups deletes backups older than retention period
func (bs *BackupService) cleanupOldBackups(ctx context.Context) error {
	backups, err := bs.store.Backups().ListCompleted(ctx)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-backupRetention)

	for _, backup := range backups {
		if backup.CreatedAt.Before(cutoff) {
			backupPath := filepath.Join(backupDir, backup.Filename)
			if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
				bs.logger.Warn("failed to delete old backup", zap.String("filename", backup.Filename), zap.Error(err))
			}
			if err := bs.store.Backups().Delete(ctx, backup.ID); err != nil {
				bs.logger.Warn("failed to delete backup metadata", zap.String("backup_id", backup.ID), zap.Error(err))
			}
		}
	}

	return nil
}

// GetBackups retrieves all backups
func (bs *BackupService) GetBackups(ctx context.Context, limit, offset int) ([]*storage.Backup, error) {
	return bs.store.Backups().List(ctx, limit, offset)
}

// GetBackup retrieves a specific backup
func (bs *BackupService) GetBackup(ctx context.Context, backupID string) (*storage.Backup, error) {
	return bs.store.Backups().GetByID(ctx, backupID)
}

// DeleteBackup deletes a backup
func (bs *BackupService) DeleteBackup(ctx context.Context, backupID string) error {
	backup, err := bs.store.Backups().GetByID(ctx, backupID)
	if err != nil {
		return err
	}
	if backup == nil {
		return fmt.Errorf("backup not found")
	}

	backupPath := filepath.Join(backupDir, backup.Filename)
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete backup file: %w", err)
	}

	return bs.store.Backups().Delete(ctx, backupID)
}

// RestoreBackup restores a database from a backup
func (bs *BackupService) RestoreBackup(ctx context.Context, backupID string) error {
	bs.backupMutex.Lock()
	defer bs.backupMutex.Unlock()

	backup, err := bs.store.Backups().GetByID(ctx, backupID)
	if err != nil {
		return err
	}
	if backup == nil {
		return fmt.Errorf("backup not found")
	}

	if backup.Status != "completed" {
		return fmt.Errorf("backup is not in completed state")
	}

	backupPath := filepath.Join(backupDir, backup.Filename)
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}

	// Validate backup before restore
	if err := bs.validateBackup(backupPath, backup.Checksum); err != nil {
		return fmt.Errorf("backup validation failed: %w", err)
	}

	// Extract backup to temporary location
	tempDB := "data/dso.db.restore"
	if err := bs.extractBackup(backupPath, tempDB); err != nil {
		os.Remove(tempDB)
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	// Verify extracted database
	if err := bs.verifyDatabase(tempDB); err != nil {
		os.Remove(tempDB)
		return fmt.Errorf("restored database verification failed: %w", err)
	}

	// Backup current database
	currentDB := bs.dbPath
	backupCurrent := "data/dso.db.old"
	if err := os.Rename(currentDB, backupCurrent); err != nil {
		return fmt.Errorf("failed to backup current database: %w", err)
	}

	// Move restored database to current location
	if err := os.Rename(tempDB, currentDB); err != nil {
		// Rollback: restore the old database
		os.Rename(backupCurrent, currentDB)
		return fmt.Errorf("failed to restore database: %w", err)
	}

	// Clean up old backup
	os.Remove(backupCurrent)

	bs.logger.Info("Database restored successfully",
		zap.String("backup_id", backupID),
		zap.String("filename", backup.Filename))

	return nil
}

// validateBackup validates a backup file
func (bs *BackupService) validateBackup(filepath string, expectedChecksum string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("backup is not valid gzip: %w", err)
	}
	defer gzReader.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, gzReader); err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	checksum := fmt.Sprintf("%x", hash.Sum(nil))
	if checksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, checksum)
	}

	return nil
}

// extractBackup extracts a backup file
func (bs *BackupService) extractBackup(backupPath, targetPath string) error {
	backupFile, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer backupFile.Close()

	gzReader, err := gzip.NewReader(backupFile)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}
	defer gzReader.Close()

	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer targetFile.Close()

	if _, err := io.Copy(targetFile, gzReader); err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	return nil
}

// verifyDatabase verifies a database file is valid
func (bs *BackupService) verifyDatabase(dbPath string) error {
	// Check file exists and is readable
	file, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("database file not readable: %w", err)
	}
	defer file.Close()

	// Read SQLite header
	header := make([]byte, 16)
	if _, err := file.Read(header); err != nil {
		return fmt.Errorf("failed to read database header: %w", err)
	}

	// Check SQLite magic number
	if string(header[:13]) != "SQLite format" {
		return fmt.Errorf("not a valid SQLite database")
	}

	return nil
}
