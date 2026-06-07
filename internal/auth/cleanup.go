package auth

import (
	"context"
	"log"
	"sync"
	"time"
)

// SessionCleanupManager manages periodic cleanup of expired sessions
type SessionCleanupManager struct {
	authService *AuthenticationService
	interval    time.Duration
	done        chan bool
	stopped     bool
	mu          sync.Mutex
}

// NewSessionCleanupManager creates a new session cleanup manager
func NewSessionCleanupManager(authService *AuthenticationService, interval time.Duration) *SessionCleanupManager {
	if interval == 0 {
		interval = 1 * time.Hour // Default cleanup every hour
	}
	return &SessionCleanupManager{
		authService: authService,
		interval:    interval,
		done:        make(chan bool),
	}
}

// Start begins the cleanup routine
func (scm *SessionCleanupManager) Start() {
	go scm.cleanupLoop()
}

// cleanupLoop runs the periodic cleanup
func (scm *SessionCleanupManager) cleanupLoop() {
	ticker := time.NewTicker(scm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := scm.authService.CleanupExpiredSessions(ctx); err != nil {
				log.Printf("[SESSION_CLEANUP] error: %v", err)
			}
			cancel()
		case <-scm.done:
			return
		}
	}
}

// Stop gracefully stops the cleanup routine (idempotent)
func (scm *SessionCleanupManager) Stop() {
	scm.mu.Lock()
	defer scm.mu.Unlock()
	if !scm.stopped {
		scm.stopped = true
		close(scm.done)
	}
}
