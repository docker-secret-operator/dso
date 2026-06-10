package execution

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ExecutionQueueItem represents an item in the execution queue
type ExecutionQueueItem struct {
	ExecutionID   string
	CorrelationID string
	Priority      int
	RetryCount    int
	MaxRetries    int
	ExpiresAt     time.Time
	CreatedAt     time.Time
	EnqueuedAt    time.Time
	DequeuedAt    *time.Time
}

// ExecutionQueue manages pending executions
type ExecutionQueue struct {
	items     []*ExecutionQueueItem
	itemsByID map[string]*ExecutionQueueItem
	mutex     sync.RWMutex
	notifyCh  chan struct{}
	closed    bool
}

// NewExecutionQueue creates a new execution queue
func NewExecutionQueue() *ExecutionQueue {
	return &ExecutionQueue{
		items:     make([]*ExecutionQueueItem, 0),
		itemsByID: make(map[string]*ExecutionQueueItem),
		notifyCh:  make(chan struct{}, 100),
	}
}

// Enqueue adds an execution request to the queue
func (eq *ExecutionQueue) Enqueue(ctx context.Context, executionID string, correlationID string, priority int, ttl time.Duration) error {
	eq.mutex.Lock()
	defer eq.mutex.Unlock()

	if eq.closed {
		return fmt.Errorf("queue is closed")
	}

	if _, exists := eq.itemsByID[executionID]; exists {
		return fmt.Errorf("execution already queued: %s", executionID)
	}

	item := &ExecutionQueueItem{
		ExecutionID:   executionID,
		CorrelationID: correlationID,
		Priority:      priority,
		RetryCount:    0,
		MaxRetries:    3,
		ExpiresAt:     time.Now().Add(ttl),
		CreatedAt:     time.Now(),
		EnqueuedAt:    time.Now(),
	}

	eq.items = append(eq.items, item)
	eq.itemsByID[executionID] = item

	// Notify dequeuer
	select {
	case eq.notifyCh <- struct{}{}:
	default:
	}

	return nil
}

// Dequeue removes and returns the next execution request (highest priority first)
func (eq *ExecutionQueue) Dequeue(ctx context.Context) (*ExecutionQueueItem, error) {
	eq.mutex.Lock()
	defer eq.mutex.Unlock()

	// Remove expired items
	eq.removeExpiredLocked()

	if len(eq.items) == 0 {
		return nil, fmt.Errorf("queue is empty")
	}

	// Find highest priority item
	maxPriority := -1
	maxIndex := -1

	for i, item := range eq.items {
		if item.Priority > maxPriority {
			maxPriority = item.Priority
			maxIndex = i
		}
	}

	if maxIndex == -1 {
		return nil, fmt.Errorf("queue is empty")
	}

	// Remove from queue
	item := eq.items[maxIndex]
	eq.items = append(eq.items[:maxIndex], eq.items[maxIndex+1:]...)
	delete(eq.itemsByID, item.ExecutionID)

	now := time.Now()
	item.DequeuedAt = &now

	return item, nil
}

// Peek returns the next item without removing it
func (eq *ExecutionQueue) Peek(ctx context.Context) (*ExecutionQueueItem, error) {
	eq.mutex.RLock()
	defer eq.mutex.RUnlock()

	if len(eq.items) == 0 {
		return nil, fmt.Errorf("queue is empty")
	}

	// Find highest priority item
	maxPriority := -1
	maxIndex := -1

	for i, item := range eq.items {
		if item.Priority > maxPriority {
			maxPriority = item.Priority
			maxIndex = i
		}
	}

	if maxIndex == -1 {
		return nil, fmt.Errorf("queue is empty")
	}

	return eq.items[maxIndex], nil
}

// Requeue puts an item back in the queue for retry
func (eq *ExecutionQueue) Requeue(ctx context.Context, item *ExecutionQueueItem) error {
	eq.mutex.Lock()
	defer eq.mutex.Unlock()

	if eq.closed {
		return fmt.Errorf("queue is closed")
	}

	if item.RetryCount >= item.MaxRetries {
		return fmt.Errorf("max retries exceeded")
	}

	item.RetryCount++
	item.EnqueuedAt = time.Now()
	item.DequeuedAt = nil

	eq.items = append(eq.items, item)
	eq.itemsByID[item.ExecutionID] = item

	// Notify dequeuer
	select {
	case eq.notifyCh <- struct{}{}:
	default:
	}

	return nil
}

// Remove removes an item from the queue
func (eq *ExecutionQueue) Remove(ctx context.Context, executionID string) error {
	eq.mutex.Lock()
	defer eq.mutex.Unlock()

	_, exists := eq.itemsByID[executionID]
	if !exists {
		return fmt.Errorf("execution not in queue: %s", executionID)
	}

	// Find and remove from items slice
	for i, it := range eq.items {
		if it.ExecutionID == executionID {
			eq.items = append(eq.items[:i], eq.items[i+1:]...)
			break
		}
	}

	delete(eq.itemsByID, executionID)
	return nil
}

// GetItem retrieves an item by ID
func (eq *ExecutionQueue) GetItem(ctx context.Context, executionID string) (*ExecutionQueueItem, error) {
	eq.mutex.RLock()
	defer eq.mutex.RUnlock()

	item, exists := eq.itemsByID[executionID]
	if !exists {
		return nil, fmt.Errorf("execution not in queue: %s", executionID)
	}

	return item, nil
}

// Length returns the current queue length
func (eq *ExecutionQueue) Length(ctx context.Context) int {
	eq.mutex.RLock()
	defer eq.mutex.RUnlock()

	return len(eq.items)
}

// List returns all queued items
func (eq *ExecutionQueue) List(ctx context.Context) ([]*ExecutionQueueItem, error) {
	eq.mutex.RLock()
	defer eq.mutex.RUnlock()

	items := make([]*ExecutionQueueItem, len(eq.items))
	copy(items, eq.items)

	return items, nil
}

// Close closes the queue
func (eq *ExecutionQueue) Close() error {
	eq.mutex.Lock()
	defer eq.mutex.Unlock()

	eq.closed = true
	close(eq.notifyCh)

	return nil
}

// NotifyChannel returns channel that notifies when items are queued
func (eq *ExecutionQueue) NotifyChannel() <-chan struct{} {
	return eq.notifyCh
}

// removeExpiredLocked removes expired items (must hold lock)
func (eq *ExecutionQueue) removeExpiredLocked() {
	now := time.Now()
	validItems := make([]*ExecutionQueueItem, 0, len(eq.items))

	for _, item := range eq.items {
		if item.ExpiresAt.After(now) {
			validItems = append(validItems, item)
		} else {
			// Item expired - remove from map
			delete(eq.itemsByID, item.ExecutionID)
		}
	}

	eq.items = validItems
}

// PurgeExpired removes all expired items
func (eq *ExecutionQueue) PurgeExpired(ctx context.Context) int {
	eq.mutex.Lock()
	defer eq.mutex.Unlock()

	oldLen := len(eq.items)
	eq.removeExpiredLocked()
	return oldLen - len(eq.items)
}

// Stats returns queue statistics
func (eq *ExecutionQueue) Stats(ctx context.Context) map[string]interface{} {
	eq.mutex.RLock()
	defer eq.mutex.RUnlock()

	totalRetries := 0
	for _, item := range eq.items {
		totalRetries += item.RetryCount
	}

	return map[string]interface{}{
		"queued_count":    len(eq.items),
		"total_retries":   totalRetries,
		"average_retries": float64(totalRetries) / float64(len(eq.items)+1),
	}
}
