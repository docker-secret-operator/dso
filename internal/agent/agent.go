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
	readyOnce  sync.Once     // Ensures Ready channel closes exactly once
}

// NewAgent creates a new Agent daemon.
func NewAgent(docker *client.Client) *Agent {
	// Initialize logger with fallback to development logger if production fails
	logger, err := zap.NewProduction()
	if err != nil {
		logger = zap.Must(zap.NewDevelopment())
	}
	return &Agent{
		cache:    NewCache(),
		docker:   docker,
		logger:   logger,
		injected: make(map[string]bool),
		Ready:    make(chan struct{}),
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

// Start begins listening to the Docker socket for lifecycle events.
func (a *Agent) Start(ctx context.Context) error {
	// Initialize bounded event queue (1000 max events, 16 workers)
	a.eventQueue = eventqueue.NewBoundedEventQueue(a.logger, 1000, 16, a.handleEventWithContext)
	a.eventQueue.Start(ctx)
	defer a.eventQueue.Stop()

	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")
	filterArgs.Add("event", "create")
	filterArgs.Add("event", "start")
	filterArgs.Add("event", "die")
	filterArgs.Add("event", "destroy")

	const (
		initialReconnectDelay  = 1 * time.Second
		maxReconnectDelay      = 5 * time.Minute  // Increased from 30s to prevent spam
		maxConsecutiveAttempts = 20                // Exit after 20 failed attempts
	)

	reconnectDelay := initialReconnectDelay
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
					log.Printf("⚠️ [DSO Agent] Event queue full, dropping event: %s/%s", msg.Actor.ID[:12], string(msg.Action))
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
			log.Printf("❌ [DSO Agent] Failed injection [start] for container %s (%s/%s): %v\n", containerID[:12], project, service, err)
			// Clear tracker so a restart (die→start) retries injection
			a.mu.Lock()
			delete(a.injected, containerID)
			a.mu.Unlock()
		} else {
			a.mu.Lock()
			a.injected[containerID] = true
			a.mu.Unlock()
			log.Printf("🔒 [DSO Agent] Injected secrets [start] for container %s (%s/%s)\n", containerID[:12], project, service)
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
