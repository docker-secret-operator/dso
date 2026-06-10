package plugins

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// IntegrationQueue manages event delivery with retries
type IntegrationQueue struct {
	queue      map[string]*DeliveryQueueItem
	deadLetter []*DeliveryQueueItem
	mu         sync.RWMutex
	logger     *zap.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
}

// NewIntegrationQueue creates a new delivery queue
func NewIntegrationQueue(logger *zap.Logger) *IntegrationQueue {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &IntegrationQueue{
		queue:      make(map[string]*DeliveryQueueItem),
		deadLetter: make([]*DeliveryQueueItem, 0),
		logger:     logger,
		done:       make(chan struct{}),
	}
}

// Start begins the queue processing loop
func (iq *IntegrationQueue) Start(ctx context.Context) error {
	iq.ctx, iq.cancel = context.WithCancel(ctx)

	go iq.processLoop()

	return nil
}

// Stop gracefully stops the queue
func (iq *IntegrationQueue) Stop() {
	if iq.cancel != nil {
		iq.cancel()
	}

	select {
	case <-iq.done:
	case <-time.After(5 * time.Second):
		iq.logger.Warn("integration queue shutdown timeout")
	}
}

// Enqueue adds an event to the delivery queue
func (iq *IntegrationQueue) Enqueue(item *DeliveryQueueItem) {
	iq.mu.Lock()
	defer iq.mu.Unlock()

	iq.queue[item.ID] = item
}

// processLoop is the main processing loop
func (iq *IntegrationQueue) processLoop() {
	defer close(iq.done)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-iq.ctx.Done():
			return
		case <-ticker.C:
			iq.processQueue()
		}
	}
}

// processQueue processes pending deliveries
func (iq *IntegrationQueue) processQueue() {
	iq.mu.Lock()
	defer iq.mu.Unlock()

	now := time.Now()

	for id, item := range iq.queue {
		// Skip if not yet ready for retry
		if item.NextRetryTime.After(now) {
			continue
		}

		// Process in goroutine to not block the lock
		go func(itemCopy *DeliveryQueueItem, itemID string) {
			defer func() {
				if r := recover(); r != nil {
					iq.logger.Error("integration delivery panic",
						zap.String("item_id", itemID),
						zap.Any("panic", r))
				}
			}()

			iq.processItem(itemCopy, itemID)
		}(item, id)
	}
}

// processItem processes a single queue item
func (iq *IntegrationQueue) processItem(item *DeliveryQueueItem, itemID string) {
	// This will be implemented by the integration manager
	// For now, just log that we would process it
	iq.logger.Debug("processing queue item", zap.String("id", itemID))

	// Remove from queue after processing (success or failure)
	iq.mu.Lock()
	defer iq.mu.Unlock()

	delete(iq.queue, itemID)
}

// CalculateBackoff calculates exponential backoff time
func CalculateBackoff(attempt int, policy *RetryPolicy) time.Duration {
	if policy == nil {
		policy = &RetryPolicy{
			MaxRetries:        3,
			InitialBackoff:    5,
			MaxBackoff:        300,
			BackoffMultiplier: 2.0,
		}
	}

	if attempt <= 0 {
		return 0
	}

	backoff := float64(policy.InitialBackoff) * math.Pow(policy.BackoffMultiplier, float64(attempt-1))
	if backoff > float64(policy.MaxBackoff) {
		backoff = float64(policy.MaxBackoff)
	}

	return time.Duration(backoff) * time.Second
}

// ShouldRetry determines if an item should be retried
func ShouldRetry(item *DeliveryQueueItem, policy *RetryPolicy) bool {
	if policy == nil {
		policy = &RetryPolicy{MaxRetries: 3}
	}

	return item.Attempt <= policy.MaxRetries
}

// GetQueueStats returns statistics about the queue
func (iq *IntegrationQueue) GetQueueStats() map[string]interface{} {
	iq.mu.RLock()
	defer iq.mu.RUnlock()

	return map[string]interface{}{
		"pending":       len(iq.queue),
		"dead_letters":  len(iq.deadLetter),
		"total":         len(iq.queue) + len(iq.deadLetter),
	}
}

// GetDeadLetters returns items in the dead-letter queue
func (iq *IntegrationQueue) GetDeadLetters(limit int) []*DeliveryQueueItem {
	iq.mu.RLock()
	defer iq.mu.RUnlock()

	if limit <= 0 || limit > len(iq.deadLetter) {
		limit = len(iq.deadLetter)
	}

	result := make([]*DeliveryQueueItem, limit)
	copy(result, iq.deadLetter[:limit])
	return result
}

// RetryDeadLetter retries a dead-letter item
func (iq *IntegrationQueue) RetryDeadLetter(itemID string) error {
	iq.mu.Lock()
	defer iq.mu.Unlock()

	for i, item := range iq.deadLetter {
		if item.ID == itemID {
			item.Attempt = 1
			item.NextRetryTime = time.Now()
			iq.queue[itemID] = item

			// Remove from dead letter
			iq.deadLetter = append(iq.deadLetter[:i], iq.deadLetter[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("dead letter item not found: %s", itemID)
}
