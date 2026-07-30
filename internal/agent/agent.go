package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	eventqueue "github.com/docker-secret-operator/dso/internal/events"
	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/internal/resolver"
	"github.com/docker-secret-operator/dso/internal/util"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// Agent represents the background process that listens to Docker events
// and injects secrets into containers seamlessly.
type Agent struct {
	cache      *Cache
	docker     *client.Client
	logger     *zap.Logger
	injected   map[string]bool
	mu         sync.Mutex
	Ready      chan struct{} // Signaled when the agent is listening
	eventQueue *eventqueue.BoundedEventQueue
	readyOnce  sync.Once // Ensures Ready channel closes exactly once
}

// NewAgent creates a new Agent daemon.
func NewAgent(docker *client.Client) *Agent {
	// Initialize logger via observability.NewLogger so output passes through
	// the shared redaction core (SEC-1), with fallback to development config
	// if production config fails. Both paths go through the same factory so
	// neither can bypass redaction.
	logger, err := observability.NewLogger("info", "json", true)
	if err != nil {
		logger, err = observability.NewLogger("info", "console", false)
		if err != nil {
			logger = zap.NewNop()
		}
	}
	return &Agent{
		cache:     NewCache(),
		docker:    docker,
		logger:    logger,
		injected:  make(map[string]bool),
		Ready:     make(chan struct{}),
		readyOnce: sync.Once{},
	}
}

// GetCache provides access to the Agent's secure memory store.
func (a *Agent) GetCache() *Cache {
	return a.cache
}

// Close gracefully shuts down the agent and its resources
func (a *Agent) Close() error {
	if a.docker != nil {
		return a.docker.Close()
	}
	return nil
}

// Start begins listening to the Docker socket for lifecycle events and
// starts the container label-watching loop.
func (a *Agent) Start(ctx context.Context) error {
	// Initialize bounded event queue (1000 max events, 16 workers)
	a.eventQueue = eventqueue.NewBoundedEventQueue(a.logger, 1000, 16, a.handleEventWithContext)
	a.eventQueue.Start(ctx)
	defer a.eventQueue.Stop()

	// Start the container label-watching loop
	go func() {
		if err := a.runMainLoop(ctx); err != nil && err != context.Canceled {
			a.logger.Error("main loop exited with error",
				zap.Error(err))
		}
	}()

	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")
	filterArgs.Add("event", "create")
	filterArgs.Add("event", "start")
	filterArgs.Add("event", "die")
	filterArgs.Add("event", "destroy")

	const (
		initialReconnectDelay  = 1 * time.Second
		maxReconnectDelay      = 5 * time.Minute // Increased from 30s to prevent spam
		maxConsecutiveAttempts = 20              // Exit after 20 failed attempts
	)

	var reconnectDelay time.Duration
	consecutiveFailures := 0
	totalReconnectAttempts := 0

	for {
		select {
		case <-ctx.Done():
			log.Println("✅ [DSO Agent] Event loop shutting down gracefully")
			return ctx.Err()
		default:
		}

		msgCh, errCh := a.docker.Events(ctx, events.ListOptions{Filters: filterArgs})
		reconnectDelay = initialReconnectDelay
		consecutiveFailures = 0
		log.Println("✅ [DSO Agent] Docker event stream connected")
		a.readyOnce.Do(func() { close(a.Ready) })

	EventStream:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				log.Printf("⚠️ [DSO Agent] Docker event stream error: %v", err)
				break EventStream
			case msg := <-msgCh:
				// Enqueue event with backpressure protection
				if !a.eventQueue.Enqueue(msg) {
					log.Printf("⚠️ [DSO Agent] Event queue full, dropping event: %s/%s", util.ShortID(msg.Actor.ID), string(msg.Action))
				}
			}
		}

		consecutiveFailures++
		totalReconnectAttempts++

		// If too many consecutive failures, give up to avoid slamming Docker daemon
		if consecutiveFailures > maxConsecutiveAttempts {
			log.Printf("❌ [DSO Agent] Too many consecutive failures (%d), exiting gracefully", consecutiveFailures)
			return fmt.Errorf("docker daemon unreachable after %d attempts", totalReconnectAttempts)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
			// Apply exponential backoff with ceiling
			reconnectDelay = time.Duration(float64(reconnectDelay) * 1.5)
			if reconnectDelay > maxReconnectDelay {
				reconnectDelay = maxReconnectDelay
			}
			log.Printf("✅ [DSO Agent] Reconnecting to Docker daemon (delay: %v, attempt: %d/%d)",
				reconnectDelay, consecutiveFailures, totalReconnectAttempts)
		}
	}
}

// handleEventWithContext wraps handleEvent for use with the bounded queue
func (a *Agent) handleEventWithContext(ctx context.Context, msg events.Message) error {
	a.handleEvent(ctx, msg)
	return nil
}

// handleEvent processes relevant Docker events concurrently.
func (a *Agent) handleEvent(ctx context.Context, msg events.Message) {
	containerID := msg.Actor.ID
	project := msg.Actor.Attributes["com.docker.compose.project"]
	service := msg.Actor.Attributes["com.docker.compose.service"]

	if project == "" || service == "" {
		return // Not a docker-compose container or missing labels
	}

	seed, ok := a.cache.GetSeed(project)
	if !ok {
		return // No secrets tracked for this deployment
	}

	serviceSecrets, ok := seed.Services[service]
	if !ok || len(serviceSecrets.FileSecrets) == 0 {
		return // This specific service does not require dsofile:// injection
	}

	switch msg.Action {
	case "create":
		// File secrets are injected via docker exec which requires a running
		// container. Skip the create event — injection happens on start.
		// The wait loop inside the container ensures startup is deferred until
		// files appear in the tmpfs.
		log.Printf("⏳ [DSO Agent] Container created, awaiting start to inject secrets (%s/%s)\n", project, service)

	case "start":
		injectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := a.inject(injectCtx, containerID, serviceSecrets); err != nil {
			log.Printf("❌ [DSO Agent] Failed injection [start] for container %s (%s/%s): %v\n", util.ShortID(containerID), project, service, err)
			// Clear tracker so a restart (die→start) retries injection
			a.mu.Lock()
			delete(a.injected, containerID)
			a.mu.Unlock()
		} else {
			a.mu.Lock()
			a.injected[containerID] = true
			a.mu.Unlock()
			log.Printf("🔒 [DSO Agent] Injected secrets [start] for container %s (%s/%s)\n", util.ShortID(containerID), project, service)
		}

	case "die", "destroy":
		// Clear tracker so the next start (on restart) re-injects into fresh tmpfs
		a.mu.Lock()
		delete(a.injected, containerID)
		a.mu.Unlock()
	}
}

func (a *Agent) inject(ctx context.Context, containerID string, serviceSecrets resolver.ServiceSecrets) error {
	filesToInject := make(map[string]string)

	for filePath, hash := range serviceSecrets.FileSecrets {
		val, ok := a.cache.Get(hash)
		if !ok {
			return fmt.Errorf("secret missing from cache pool")
		}
		filesToInject[filePath] = val
	}

	return injector.InjectFiles(ctx, a.docker, containerID, filesToInject, serviceSecrets.UID, serviceSecrets.GID)
}

// runMainLoop watches container label changes and feeds them to the event
// reactor.
//
// PERF-2/PERF-3: this used to also run an "adaptive polling" loop over a
// SmartPoller. That loop was removed because it could not do the job it
// claimed: pollSecret only ever hashed the *local cache* (it had no provider
// handle and made no provider API call), so a secret rotated at the provider
// was undetectable by it — it could only observe changes something else had
// already written locally. It also fired one spurious "change" per secret on
// the first tick after boot, and re-created a ticker plus goroutine on *every*
// poll rather than only when the interval actually changed.
//
// Real provider polling and rotation live in TriggerEngine.StartPolling
// (internal/agent/trigger.go), which does call the provider, and is what the
// `dso agent` daemon runs. This loop's remaining job is container label
// watching for the `docker dso up` path.
//
// Known gap, unchanged by this cleanup: rotationCallback is still a stub that
// only logs (see its comment). Wiring the reactor to real rotation is separate
// work from removing the poller that could never have worked.
func (a *Agent) runMainLoop(ctx context.Context) error {
	// Create listener only if Docker client is available
	var listener *eventqueue.ContainerListener
	if a.docker != nil {
		listener = eventqueue.NewContainerListener(a.docker)
	}

	reactor := eventqueue.NewEventReactorImpl(a.rotationCallback())

	// 2. Start components
	if listener != nil {
		if err := listener.Start(ctx); err != nil {
			log.Printf("⚠️ [DSO Agent] Container listener failed to start: %v (continuing without label watching)", err)
		}
	}

	if err := reactor.Start(ctx); err != nil {
		return fmt.Errorf("event reactor failed to start: %w", err)
	}

	// 3. Cleanup on exit
	defer func() {
		if listener != nil {
			_ = listener.Stop()
		}
		_ = reactor.Stop(ctx)

		log.Println("✅ [DSO Agent] Main loop components cleaned up")
	}()

	// 4. Get listener events channel (nil if listener not available)
	var listenerEventsChan <-chan *eventqueue.ContainerLabelEvent
	if listener != nil {
		listenerEventsChan = listener.Events()
	}

	// 7. Main select loop
	for {
		select {
		case <-ctx.Done():
			log.Println("✅ [DSO Agent] Main loop shutting down gracefully")
			return ctx.Err()

		case containerEvent := <-listenerEventsChan:
			if containerEvent != nil {
				if err := reactor.ProcessContainerEvent(ctx, *containerEvent); err != nil {
					a.logger.Warn("failed to process container event", zap.Error(err))
				}
			}

		}

		// Health check
		if !reactor.IsHealthy() {
			a.logger.Warn("event reactor unhealthy (no recent events)")
		}
	}
}

// rotationCallback returns the RotationTrigger the event reactor invokes when
// a container label change is detected.
//
// KNOWN GAP — this is observability only, not rotation. It logs and returns
// nil; it does not rotate anything. Real rotation is performed by
// TriggerEngine.ExecuteRotation (internal/agent/trigger.go), driven by
// TriggerEngine.StartPolling, which is what the `dso agent` daemon runs.
//
// Wiring this callback to the TriggerEngine would require the Agent to hold
// one (it currently does not) and to resolve a secret name to its provider and
// SecretMapping. That is deliberately out of scope here: this comment replaces
// an older one that described the same stub as if the rotation happened
// elsewhere in the queue processor, which was not accurate.
func (a *Agent) rotationCallback() eventqueue.RotationTrigger {
	return func(ctx context.Context, secretName string, priority eventqueue.EventPriority) error {
		a.logger.Info("container label change observed (no rotation performed here — see rotationCallback doc)",
			zap.String("secret", secretName),
			zap.Int("priority", int(priority)))
		return nil
	}
}
