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

	// Polling and event reaction components
	poller           *polling.SmartPoller
	pollerMu         sync.RWMutex
	secretVersions   map[string]string // Track last seen versions for change detection
	secretVersionsMu sync.RWMutex
	tickerStopChans  map[string]chan struct{} // Signal channels for ticker goroutines
	tickerStopMu     sync.Mutex               // Protect tickerStopChans map
	tickers          map[string]*time.Ticker  // Polling tickers for each secret
	tickersMu        sync.Mutex               // Protect tickers map
	lastCleanup      time.Time                // Track last cleanup time for stale secret entries
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
		cache:           NewCache(),
		docker:          docker,
		logger:          logger,
		injected:        make(map[string]bool),
		Ready:           make(chan struct{}),
		readyOnce:       sync.Once{},
		poller:          polling.NewSmartPoller(),
		secretVersions:  make(map[string]string),
		tickerStopChans: make(map[string]chan struct{}),
		tickers:         make(map[string]*time.Ticker),
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
// It also starts the adaptive polling main loop for secret change detection.
func (a *Agent) Start(ctx context.Context) error {
	// Initialize bounded event queue (1000 max events, 16 workers)
	a.eventQueue = eventqueue.NewBoundedEventQueue(a.logger, 1000, 16, a.handleEventWithContext)
	a.eventQueue.Start(ctx)
	defer a.eventQueue.Stop()

	// Start the adaptive polling and event-driven rotation main loop
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
			_ = listener.Stop()
		}
		_ = reactor.Stop(ctx)

		// Close all ticker stop channels to signal goroutines to exit
		a.tickerStopMu.Lock()
		for _, stopChan := range a.tickerStopChans {
			close(stopChan)
		}
		a.tickerStopChans = make(map[string]chan struct{})
		a.tickerStopMu.Unlock()

		// Stop all tickers and clean up map
		a.tickersMu.Lock()
		for _, ticker := range a.tickers {
			ticker.Stop()
		}
		a.tickers = make(map[string]*time.Ticker)
		a.tickersMu.Unlock()

		log.Println("✅ [DSO Agent] Main loop components cleaned up")
	}()

	// 4. Channel for poll events
	tickersChan := make(chan string, 10)

	defer func() {
		close(tickersChan)
	}()

	// 5. Start ticker goroutines for each secret
	go a.startPollingGoroutines(ctx, poller, tickersChan)

	// 5.5. Start cleanup goroutine to remove stale secret entries
	go a.cleanupStaleSecrets(ctx)

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
			a.updateTicker(ctx, tickersChan, secretName, poller)
		}

		// Health check
		if !reactor.IsHealthy() {
			a.logger.Warn("event reactor unhealthy (no recent events)")
		}
	}
}

// startPollingGoroutines creates and manages polling goroutines for each monitored secret.
// Each secret gets its own ticker that fires at adaptive intervals based on recent change frequency.
// Poll events are sent to tickersChan for processing in the main event loop.
func (a *Agent) startPollingGoroutines(ctx context.Context, poller *polling.SmartPoller,
	tickersChan chan<- string) {

	// Initial secret list
	secrets := a.getSecretsToMonitor()

	for _, secretName := range secrets {
		interval := poller.GetNextInterval(secretName)
		ticker := time.NewTicker(interval)

		// Create stop channel for this secret's goroutine
		stopChan := make(chan struct{})

		// Lock tickers map to add new ticker
		a.tickersMu.Lock()
		a.tickers[secretName] = ticker
		a.tickersMu.Unlock()

		// Lock stop channels map to add new stop channel
		a.tickerStopMu.Lock()
		a.tickerStopChans[secretName] = stopChan
		a.tickerStopMu.Unlock()

		go func(name string, t *time.Ticker, stop <-chan struct{}) {
			for {
				select {
				case <-stop:
					return
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
		}(secretName, ticker, stopChan)
	}
}

// updateTicker adapts the polling interval for a secret based on recent change frequency.
// It stops the old ticker and creates a new one with the interval from SmartPoller.
// The adaptive interval uses: 5s for active changes, 30s for baseline, 5m for backoff.
// This method must be called with both tickersMu and tickerStopMu locked or held individually.
func (a *Agent) updateTicker(ctx context.Context,
	tickersChan chan<- string, secretName string, poller *polling.SmartPoller) {

	// Lock stop channels to signal old goroutine and create new one
	a.tickerStopMu.Lock()
	// Signal old goroutine to exit immediately
	if stopChan, exists := a.tickerStopChans[secretName]; exists {
		close(stopChan)
	}

	// Create new stop channel for new goroutine
	newStopChan := make(chan struct{})
	a.tickerStopChans[secretName] = newStopChan
	a.tickerStopMu.Unlock()

	// Get new interval for the secret
	interval := poller.GetNextInterval(secretName)

	// Lock tickers map to replace old ticker with new one
	a.tickersMu.Lock()
	if oldTicker, exists := a.tickers[secretName]; exists {
		oldTicker.Stop()
	}

	newTicker := time.NewTicker(interval)
	a.tickers[secretName] = newTicker
	a.tickersMu.Unlock()

	// Start new ticker goroutine with new stop channel
	go func(name string, t *time.Ticker, stop <-chan struct{}) {
		for {
			select {
			case <-stop:
				return
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
	}(secretName, newTicker, newStopChan)
}

// pollSecret queries the current version of a secret from its configured backend.
// It compares against the last known version and returns whether the secret changed.
func (a *Agent) pollSecret(secretName string) (version string, changed bool) {
	var currentVersion string

	// Try to get current secret value from cache
	currentVal, ok := a.cache.Get(secretName)
	if ok {
		// Compute hash of current secret value to use as version identifier
		currentVersion = ComputeHash(map[string]string{"value": currentVal})
	} else {
		// Secret not in cache - use a stable identifier based on secret name
		// This handles the case where a secret is tracked but not yet in cache
		currentVersion = ComputeHash(map[string]string{"name": secretName})
	}

	a.secretVersionsMu.Lock()
	defer a.secretVersionsMu.Unlock()

	lastVersion, exists := a.secretVersions[secretName]
	if !exists || lastVersion != currentVersion {
		a.secretVersions[secretName] = currentVersion
		return currentVersion, true
	}

	return currentVersion, false
}

// getSecretsToMonitor returns the list of secrets that should be actively monitored.
func (a *Agent) getSecretsToMonitor() []string {
	a.cache.mu.RLock()
	defer a.cache.mu.RUnlock()

	var secrets []string
	seenSecrets := make(map[string]bool)

	// Iterate through all cached projects and extract their secret hashes
	for _, seed := range a.cache.projects {
		if seed == nil {
			continue
		}
		// Each hash in SecretPool is a unique secret to monitor
		for hash := range seed.SecretPool {
			if !seenSecrets[hash] {
				secrets = append(secrets, hash)
				seenSecrets[hash] = true
			}
		}
	}

	return secrets
}

// rotationCallback returns a RotationTrigger that handles secret rotations triggered by polling or container events.
func (a *Agent) rotationCallback() eventqueue.RotationTrigger {
	return func(ctx context.Context, secretName string, priority eventqueue.EventPriority) error {
		a.logger.Info("rotation triggered",
			zap.String("secret", secretName),
			zap.String("priority", fmt.Sprintf("%d", priority)))

		// In a production implementation with TriggerEngine, this would call:
		// a.triggerEngine.ExecuteRotation(providerName, secretName, secretData, secretMapping)
		//
		// For now, record that rotation was triggered and log the action.
		// The actual rotation coordination happens in the bounded event queue processor.
		//
		// Mark this secret as recently rotated to prevent duplicate rotation triggers
		a.secretVersionsMu.Lock()
		a.secretVersions[secretName] = fmt.Sprintf("rotated_%d", time.Now().Unix())
		a.secretVersionsMu.Unlock()

		a.logger.Info("secret rotation recorded",
			zap.String("secret", secretName),
			zap.String("priority", fmt.Sprintf("%d", priority)))

		return nil
	}
}

// cleanupStaleSecrets periodically removes entries for secrets no longer being monitored.
// This prevents unbounded growth of the secretVersions map. Cleanup runs every 5 minutes.
func (a *Agent) cleanupStaleSecrets(ctx context.Context) {
	// Helper function to perform cleanup
	performCleanup := func() {
		// Get list of currently monitored secrets
		currentSecrets := a.getSecretsToMonitor()
		currentSet := make(map[string]bool)
		for _, s := range currentSecrets {
			currentSet[s] = true
		}

		// Remove entries for secrets no longer monitored
		a.secretVersionsMu.Lock()
		staleCount := 0
		for secret := range a.secretVersions {
			if !currentSet[secret] {
				delete(a.secretVersions, secret)
				staleCount++
			}
		}
		a.lastCleanup = time.Now()
		mapSize := len(a.secretVersions)
		a.secretVersionsMu.Unlock()

		// Log cleanup result
		if staleCount > 0 {
			a.logger.Debug("cleaned stale secret entries",
				zap.Int("stale_count", staleCount),
				zap.Int("map_size", mapSize))
		}
	}

	// Run cleanup immediately on startup
	performCleanup()

	// Then set up periodic cleanup every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			performCleanup()
		}
	}
}

// GetSecretVersionsMapSize returns the current size of the secretVersions map for monitoring.
func (a *Agent) GetSecretVersionsMapSize() int {
	a.secretVersionsMu.RLock()
	defer a.secretVersionsMu.RUnlock()
	return len(a.secretVersions)
}

// GetLastCleanupTime returns when cleanup last ran.
func (a *Agent) GetLastCleanupTime() time.Time {
	a.secretVersionsMu.RLock()
	defer a.secretVersionsMu.RUnlock()
	return a.lastCleanup
}
