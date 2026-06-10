package plugins

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// EventBus handles publishing and subscribing to events
type EventBus struct {
	subscribers map[string][]Subscriber
	mu          sync.RWMutex
	logger      *zap.Logger
}

// NewEventBus creates a new event bus
func NewEventBus(logger *zap.Logger) *EventBus {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &EventBus{
		subscribers: make(map[string][]Subscriber),
		logger:      logger,
	}
}

// Subscribe registers a subscriber for an event type
func (eb *EventBus) Subscribe(eventType string, subscriber Subscriber) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.subscribers[eventType] = append(eb.subscribers[eventType], subscriber)
	eb.logger.Debug("subscriber registered", zap.String("event_type", eventType))
}

// Unsubscribe removes a subscriber from an event type
func (eb *EventBus) Unsubscribe(eventType string, subscriber Subscriber) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subs, ok := eb.subscribers[eventType]
	if !ok {
		return
	}

	// Remove subscriber by filtering
	for i, s := range subs {
		if s == subscriber {
			eb.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	eb.logger.Debug("subscriber unregistered", zap.String("event_type", eventType))
}

// Publish sends an event to all subscribers of that type
func (eb *EventBus) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	eb.mu.RLock()
	subs := eb.subscribers[event.Type]
	eb.mu.RUnlock()

	if len(subs) == 0 {
		return
	}

	// Deliver to subscribers, recovering from panics
	for _, subscriber := range subs {
		go func(s Subscriber, evt Event) {
			defer func() {
				if r := recover(); r != nil {
					eb.logger.Error("subscriber panic",
						zap.String("event_type", evt.Type),
						zap.Any("panic", r))
				}
			}()
			s.Handle(evt)
		}(subscriber, event)
	}
}

// PublishSync sends an event synchronously to all subscribers
func (eb *EventBus) PublishSync(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	eb.mu.RLock()
	subs := eb.subscribers[event.Type]
	eb.mu.RUnlock()

	if len(subs) == 0 {
		return
	}

	// Deliver to subscribers synchronously, recovering from panics
	for _, subscriber := range subs {
		func(s Subscriber, evt Event) {
			defer func() {
				if r := recover(); r != nil {
					eb.logger.Error("subscriber panic",
						zap.String("event_type", evt.Type),
						zap.Any("panic", r))
				}
			}()
			s.Handle(evt)
		}(subscriber, event)
	}
}

// GetSubscriberCount returns the number of subscribers for an event type
func (eb *EventBus) GetSubscriberCount(eventType string) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers[eventType])
}

// GetAllSubscribers returns all subscribers grouped by event type
func (eb *EventBus) GetAllSubscribers() map[string]int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	result := make(map[string]int)
	for eventType, subs := range eb.subscribers {
		result[eventType] = len(subs)
	}
	return result
}
