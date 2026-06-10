# EventBus Architecture

The EventBus is the central nervous system of DSO, enabling asynchronous communication between subsystems without tight coupling.

## Design Principles

### 1. Decoupling
Subsystems publish events without knowing who consumes them. Consumers subscribe without knowing who produces events.

### 2. Non-Blocking
Publishing is fire-and-forget. Subscribers process events asynchronously to avoid blocking the publisher.

### 3. Resilience
Subscriber failures must never affect the publisher or other subscribers. Each subscriber is isolated.

### 4. Type-Safe
Events are strongly typed via constants to prevent typos and enable refactoring.

## Architecture

```
Publisher (Subsystem A)
    ↓
    publish(eventType, data)
    ↓
EventBus
    ├─→ Subscriber 1 (async)
    ├─→ Subscriber 2 (async)
    └─→ Subscriber 3 (async)
```

## Event Types

All event types are defined in `internal/plugins/events.go` as constants:

### Core Events
```go
ExecutionStarted = "execution.started"
ExecutionCompleted = "execution.completed"
ExecutionFailed = "execution.failed"

ReviewCreated = "review.created"
ReviewApproved = "review.approved"
ReviewRejected = "review.rejected"
```

### Advanced Layer Events (Policy, Drift, Graph)
```go
RuleStarted = "rule.started"
RuleSucceeded = "rule.succeeded"
RuleFailed = "rule.failed"

DriftDetected = "drift.detected"
DriftAcknowledged = "drift.acknowledged"
DriftResolved = "drift.resolved"

GraphUpdated = "graph.updated"
CycleDetected = "graph.cycle_detected"
CriticalNodeDetected = "graph.critical_node_detected"
```

### Intelligence Layer Events
```go
IncidentCreated = "incident.created"
IncidentUpdated = "incident.updated"
IncidentResolved = "incident.resolved"

RecommendationCreated = "recommendation.created"
RecommendationImplemented = "recommendation.implemented"

ForecastCreated = "forecast.created"
CriticalForecastDetected = "forecast.critical_detected"

AutonomousActionStarted = "autonomy.action_started"
AutonomousActionSucceeded = "autonomy.action_succeeded"
AutonomousActionFailed = "autonomy.action_failed"
AutonomousActionRolledBack = "autonomy.action_rolled_back"
```

## Event Structure

```go
type Event struct {
    Type          string      // e.g., "execution.started"
    Timestamp     time.Time   // when the event occurred
    CorrelationID string      // for tracing across subsystems
    Payload       interface{} // event-specific data
}
```

## Publisher Side

### Publishing an Event

```go
// In a subsystem (e.g., execution engine)
eventBus.Publish("execution.started", map[string]interface{}{
    "execution_id": execID,
    "draft_id":     draftID,
    "timestamp":    time.Now(),
})
```

### Setting EventBus

Most subsystems accept an EventBus via `SetEventBus()`:

```go
// In initialization
engine := NewExecutionEngine(logger, store)
engine.SetEventBus(eventBus)  // or nil if not available
```

## Subscriber Side

### Subscribing to Events

```go
type CorrelationSubscriber struct {
    engine *Engine
}

func (cs *CorrelationSubscriber) Handle(event plugins.Event) {
    defer func() {
        if r := recover(); r != nil {
            log.Error("Panic in correlation subscriber", r)
        }
    }()
    
    switch event.Type {
    case plugins.ExecutionFailed:
        cs.engine.OnExecutionFailed(event.Payload)
    case plugins.AlertTriggered:
        cs.engine.OnAlertTriggered(event.Payload)
    }
}

// Register subscriber
eventBus.Subscribe(correlationSubscriber)
```

### Panic Recovery

All subscribers must have panic recovery:

```go
func (s *Subscriber) Handle(event plugins.Event) {
    defer func() {
        if r := recover(); r != nil {
            logger.Error("Panic in subscriber", zap.Any("panic", r))
            // Continue processing other subscribers
        }
    }()
    
    // Handle event
}
```

## Integration Points

### Policy Engine
Publishes:
- `rule.started` - when evaluating a rule
- `rule.succeeded` - when rule evaluation succeeds
- `rule.failed` - when rule evaluation fails

Consumes:
- Policies can be triggered by events

### Drift Detection
Publishes:
- `drift.detected` - when drift is discovered
- `drift.acknowledged` - when operator acknowledges drift
- `drift.resolved` - when drift is remediated

### Graph
Publishes:
- `graph.updated` - when graph structure changes
- `graph.cycle_detected` - when a cycle is found
- `graph.critical_node_detected` - when critical nodes are identified

### Correlation Engine
Consumes:
- `execution.failed`
- `alert.triggered`
- `drift.detected`
- `rule.failed`

Publishes:
- `incident.created` - groups related failures
- `incident.updated` - incident status changed
- `incident.resolved` - incident is closed

### Recommendation Engine
Consumes:
- `incident.created`
- `drift.detected`
- `forecast.critical_detected`

Publishes:
- `recommendation.created` - new recommendation
- `recommendation.implemented` - recommendation applied

### Forecasting Engine
Consumes:
- `execution.completed` - for time-series data
- `drift.detected` - for pattern analysis
- Background: generates periodic forecasts

Publishes:
- `forecast.created` - forecast generated
- `forecast.critical_detected` - critical prediction

### Autonomous Operations
Consumes:
- `incident.created` - trigger automatic actions
- `recommendation.created` - may auto-implement
- `drift.detected` - auto-remediate drift

Publishes:
- `autonomy.action_started` - action triggered
- `autonomy.action_succeeded` - action completed
- `autonomy.action_failed` - action error
- `autonomy.action_rolled_back` - action reversed

## Threading Model

### Publisher Thread-Safety

Publishing is thread-safe:

```go
// Safe to call from multiple goroutines
go eventBus.Publish("event1", data)
go eventBus.Publish("event2", data)
```

### Subscriber Concurrency

Subscribers are called asynchronously in separate goroutines:

```go
// Non-blocking
eventBus.Publish("event", data)  // Returns immediately
// Subscribers handle event in background
```

### Ordering Guarantees

- Events from a single publisher are ordered
- Events from different publishers have no guaranteed order
- Each subscriber processes its events in order

## Error Handling

### Subscriber Failures

If a subscriber panics, the system continues:

```go
// If Sub1 panics, Sub2 and Sub3 still run
eventBus.Publish("event", data)
```

### Publisher Failures

If publishing fails, an error is returned:

```go
if err := eventBus.Publish("event", data); err != nil {
    logger.Error("Failed to publish event", zap.Error(err))
}
```

### Graceful Degradation

If EventBus is nil, publishing is silently skipped:

```go
if e.eventBus != nil {
    e.eventBus.Publish("event", data)
}
```

## Performance Characteristics

### Latency
- Publishing: < 1ms (queuing only)
- Subscriber execution: depends on handler logic
- No head-of-line blocking

### Throughput
- Can handle thousands of events per second
- Scalable to multiple subscribers per event type

### Memory
- Events are queued temporarily
- Memory is reclaimed after subscriber processing
- No unbounded growth with proper cleanup

## Monitoring

### Metrics Tracked
- Events published per type
- Subscriber execution time
- Subscriber errors/panics
- Queue depth

### Debugging

Enable verbose logging:

```go
logger.Debug("Event published",
    zap.String("type", eventType),
    zap.String("correlation_id", event.CorrelationID))
```

## Testing

### Unit Tests

```go
func TestEventPublishing(t *testing.T) {
    eventBus := NewEventBus()
    received := false
    
    eventBus.Subscribe(plugins.SubscriberFunc(func(e plugins.Event) {
        if e.Type == plugins.ExecutionStarted {
            received = true
        }
    }))
    
    eventBus.Publish(plugins.ExecutionStarted, map[string]interface{}{})
    
    // Wait for async processing
    time.Sleep(100 * time.Millisecond)
    assert.True(t, received)
}
```

### Stress Tests

Test with high event throughput:

```bash
go test -bench=. ./internal/plugins/...
```

## Best Practices

### For Publishers
1. **Use constants**: `plugins.ExecutionStarted`, not `"execution.started"`
2. **Include correlation IDs**: For distributed tracing
3. **Handle publish errors**: Though they're rare
4. **Don't block**: Publish and continue

### For Subscribers
1. **Panic recovery**: Always wrap with defer-recover
2. **Fast processing**: Keep handlers quick
3. **Async I/O**: Use goroutines for blocking operations
4. **Logging**: Log important events for debugging

### For Integration
1. **Late binding**: Set EventBus after subsystem creation
2. **Graceful nil**: Handle nil EventBus
3. **Type-safe**: Use event constants
4. **Validation**: Validate event payload structure

## Future Enhancements

### Priority (Post-Stabilization)
1. **Event replay**: Ability to replay events for recovery
2. **Event filtering**: Subscribe to event patterns
3. **Dead letter queue**: Capture failed events
4. **Metrics export**: Prometheus integration
5. **Event versioning**: Handle schema evolution
6. **Request/reply pattern**: Request-response events
7. **Event persistence**: Log all events to audit trail
8. **Circuit breakers**: Failfast for slow subscribers

## Troubleshooting

### Events Not Being Received
1. Check subscriber is registered
2. Check event type spelling
3. Verify EventBus is not nil
4. Check for panics in subscriber (check logs)

### Memory Growing
1. Check for unbounded queues
2. Verify subscribers complete processing
3. Monitor goroutine count

### High Latency
1. Check subscriber processing time
2. Look for blocking operations in handlers
3. Monitor queue depth
4. Consider async I/O patterns

## References

- Event types: `internal/plugins/events.go`
- EventBus implementation: `internal/plugins/event_bus.go`
- Example: `internal/correlation/engine.go` (consumer)
- Example: `internal/autonomy/engine.go` (producer)
