package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	eventqueue "github.com/docker-secret-operator/dso/internal/events"
	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/internal/polling"
	"github.com/docker-secret-operator/dso/internal/resolver"
	"github.com/docker-secret-operator/dso/internal/util"
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

	// Polling and event reaction components
	poller              *polling.SmartPoller
	pollerMu            sync.RWMutex
	secretVersions      map[string]string // Track last seen versions for change detection
	secretVersionsMu    sync.RWMutex
}

// NewAgent creates a new Agent daemon.
func NewAgent(docker *client.Client) *Agent {
	// Initialize logger with fallback to development logger if production fails
	logger, err := zap.NewProduction()
	if err != nil {
		logger = zap.Must(zap.NewDevelopment())
	}
	return &Agent{
		cache:           NewCache(),
		docker:          docker,
		logger:          logger,
		injected:        make(map[string]bool),
		Ready:           make(chan struct{}),
		readyOnce:       sync.Once{},
		poller:          polling.NewSmartPoller(),
		secretVersions:  make(map[string]string),
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
		maxReconnectDelay      = 5 * time.Minute // Increased from 30s to prevent spam
		maxConsecutiveAttempts = 20              // Exit after 20 failed attempts
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

// runMainLoop runs the adaptive polling and event-driven rotation loop.
// It integrates SmartPoller, ContainerListener, and EventReactor to replace
// fixed polling with adaptive event-driven rotation.
func (a *Agent) runMainLoop(ctx context.Context) error {
	// 1. Initialize components
	poller := polling.NewSmartPoller()

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
			listener.Stop()
		}
		reactor.Stop(ctx)
		log.Println("✅ [DSO Agent] Main loop components cleaned up")
	}()

	// 4. Build secret tickers map
	tickers := make(map[string]*time.Ticker)
	tickersChan := make(chan string, 10) // Channel for poll events

	defer func() {
		for _, ticker := range tickers {
			ticker.Stop()
		}
		close(tickersChan)
	}()

	// 5. Start ticker goroutines for each secret
	go a.startPollingGoroutines(ctx, poller, tickers, tickersChan)

	// 6. Get listener events channel (nil if listener not available)
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

		case secretName := <-tickersChan:
			if secretName == "" {
				continue
			}

			// Poll secret to check for version change
			version, changed := a.pollSecret(secretName)
			if changed && version != "" {
				a.logger.Info("secret version changed detected",
					zap.String("secret", secretName),
					zap.String("version", version))

				// Record change in poller for interval adaptation
				poller.RecordChange(secretName)

				// Process as secret event
				secretEvent := eventqueue.SecretChangeEvent{
					SecretName: secretName,
					Version:    version,
					Source:     eventqueue.SourceLocalVault,
					Severity:   eventqueue.SeverityNormal,
					Timestamp:  time.Now(),
				}
				if err := reactor.ProcessSecretEvent(ctx, secretEvent); err != nil {
					a.logger.Warn("failed to process secret event", zap.Error(err))
				}
			}

			// Record poll regardless of change
			poller.RecordPoll(secretName)

			// Update interval for next poll
			a.updateTicker(ctx, tickers, tickersChan, secretName, poller)
		}

		// Health check
		if !reactor.IsHealthy() {
			a.logger.Warn("event reactor unhealthy (no recent events)")
		}
	}
}

// startPollingGoroutines creates and manages polling tickers for all tracked secrets.
// For each secret, a goroutine sends poll events to tickersChan at adaptive intervals.
func (a *Agent) startPollingGoroutines(ctx context.Context, poller *polling.SmartPoller,
	tickers map[string]*time.Ticker, tickersChan chan<- string) {

	// Initial secret list
	secrets := a.getSecretsToMonitor()
	tickersMu := sync.Mutex{}

	for _, secretName := range secrets {
		interval := poller.GetNextInterval(secretName)
		ticker := time.NewTicker(interval)

		tickersMu.Lock()
		tickers[secretName] = ticker
		tickersMu.Unlock()

		go func(name string, t *time.Ticker) {
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					select {
					case tickersChan <- name:
					case <-ctx.Done():
						return
					}
				}
			}
		}(secretName, ticker)
	}
}

// updateTicker updates the polling interval for a secret after a poll.
// It stops the old ticker and creates a new one with the adaptive interval.
func (a *Agent) updateTicker(ctx context.Context, tickers map[string]*time.Ticker,
	tickersChan chan<- string, secretName string, poller *polling.SmartPoller) {

	interval := poller.GetNextInterval(secretName)

	if oldTicker, exists := tickers[secretName]; exists {
		oldTicker.Stop()
	}

	newTicker := time.NewTicker(interval)
	tickers[secretName] = newTicker

	// Start new ticker goroutine
	go func(name string, t *time.Ticker) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				select {
				case tickersChan <- name:
				case <-ctx.Done():
					return
				}
			}
		}
	}(secretName, newTicker)
}

// pollSecret checks the current version of a secret and returns whether it changed.
// It compares against the last known version and returns the new version if different.
func (a *Agent) pollSecret(secretName string) (version string, changed bool) {
	// Get current version from cache (using a hash/version identifier)
	// For this implementation, we track secretVersions map
	// In a real implementation, this would query the actual secret backend
	currentVersion := fmt.Sprintf("%d", time.Now().Unix())

	a.secretVersionsMu.Lock()
	defer a.secretVersionsMu.Unlock()

	lastVersion, exists := a.secretVersions[secretName]
	if !exists || lastVersion != currentVersion {
		a.secretVersions[secretName] = currentVersion
		return currentVersion, true
	}

	return currentVersion, false
}

// getSecretsToMonitor returns the list of secrets currently being tracked in the cache.
func (a *Agent) getSecretsToMonitor() []string {
	// Extract secrets from all cached projects
	// This is a simplified implementation
	var secrets []string

	// In a production implementation, this would iterate through cache projects
	// For now, return an empty list (will be populated when projects are cached)

	return secrets
}

// rotationCallback returns a RotationTrigger function that handles rotation for a secret.
func (a *Agent) rotationCallback() eventqueue.RotationTrigger {
	return func(ctx context.Context, secretName string, priority eventqueue.EventPriority) error {
		a.logger.Info("rotation triggered",
			zap.String("secret", secretName),
			zap.Int("priority", int(priority)))

		// In a production implementation, this would:
		// 1. Fetch updated secret from backend
		// 2. Update cache
		// 3. Trigger rotation in dependent services
		// 4. Track rotation metrics

		// For now, just log the action
		return nil
	}
}
