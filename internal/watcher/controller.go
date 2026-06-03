package watcher

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/analyzer"
	"github.com/docker-secret-operator/dso/internal/core"
	eventqueue "github.com/docker-secret-operator/dso/internal/events"
	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/strategy"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type lockInfo struct {
	startTime   time.Time
	serviceName string
}

type TargetContainer struct {
	ID          string
	Strategy    string   // "signal" or "restart"
	ComposePath string   // Optional path to docker-compose.yml
	Secrets     []string // List of secrets this container depends on
}

type SecretCache interface {
	Get(key string) (map[string]string, bool)
}

type ReloaderController struct {
	Logger        *zap.Logger
	Targets       sync.Map // map[string]*TargetContainer (key: containerID)
	cli           *client.Client
	Server        interface{}
	Cache         SecretCache
	Config        *dsoConfig.Config
	ProxyManager  *dsoProxy.Manager // nil when no port-bound containers exist
	rotationLocks sync.Map          // map[string]*lockInfo (key: service name)
	degraded      sync.Map          // map[string]string (key: service name, val: error)
	eventQueue    *eventqueue.BoundedEventQueue
}

func NewReloaderController(logger *zap.Logger) (*ReloaderController, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &ReloaderController{
		Logger: logger,
		cli:    cli,
	}, nil
}

// Close gracefully closes the Docker client connection and event queue
func (r *ReloaderController) Close() error {
	if r.eventQueue != nil {
		r.eventQueue.Stop()
	}
	if r.cli != nil {
		return r.cli.Close()
	}
	return nil
}

func (r *ReloaderController) StartEventLoop(ctx context.Context) {
	r.Logger.Info("Starting Docker Events loop for ReloaderController")

	// Initialize bounded event queue (2000 max events for rotation-heavy workload, 32 workers)
	r.eventQueue = eventqueue.NewBoundedEventQueue(r.Logger, 2000, 32, r.handleContainerEventWithContext)
	r.eventQueue.Start(ctx)

	// Populate synchronously so all containers are registered before polling starts.
	// This must complete before StartAll() fires any rotations.
	r.populateInitialTargets(ctx)
	go r.daemonEventLoop(ctx)
}

func (r *ReloaderController) daemonEventLoop(ctx context.Context) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")
	filterArgs.Add("event", "start")
	filterArgs.Add("event", "die")
	filterArgs.Add("event", "stop")

	reconnectDelay := time.Second
	maxReconnectDelay := 30 * time.Second

	// Start periodic reconciliation (every 10 minutes)
	go r.periodicReconciliation(ctx)

	for {
		select {
		case <-ctx.Done():
			r.Logger.Info("Shutting down ReloaderController event loop")
			_ = r.cli.Close()
			return
		default:
		}

		msgCh, errCh := r.cli.Events(ctx, events.ListOptions{Filters: filterArgs})
		reconnectDelay = time.Second // Reset on successful connection

		r.Logger.Info("Docker event stream connected", zap.Duration("reconnectDelay", reconnectDelay))
		observability.DaemonReconnectsTotal.WithLabelValues("success").Inc()

		// CRITICAL FIX: Immediately reconcile state after daemon reconnect
		// This detects: dual-running containers, orphaned containers, stale state
		r.Logger.Info("Performing immediate reconciliation after daemon reconnect")
		r.reconcileRuntimeState(ctx)

		streamActive := false
		for {
			select {
			case <-ctx.Done():
				r.Logger.Info("Shutting down ReloaderController event loop")
				_ = r.cli.Close()
				return
			case err := <-errCh:
				r.Logger.Error("Docker Events stream error", zap.Error(err), zap.Duration("nextRetry", reconnectDelay))
				observability.BackendFailuresTotal.WithLabelValues("docker_events", "stream_error").Inc()
				streamActive = false
				break
			case msg := <-msgCh:
				streamActive = true
				// Enqueue event with backpressure protection
				if !r.eventQueue.Enqueue(msg) {
					r.Logger.Warn("Event queue full, dropping container event", zap.String("containerID", msg.Actor.ID), zap.String("action", string(msg.Action)))
					observability.BackendFailuresTotal.WithLabelValues("event_queue", "dropped").Inc()
				} else {
					observability.EventsProcessedTotal.WithLabelValues(string(msg.Action)).Inc()
				}
			}
			if !streamActive {
				break
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
			reconnectDelay = time.Duration(float64(reconnectDelay) * 1.5)
			if reconnectDelay > maxReconnectDelay {
				reconnectDelay = maxReconnectDelay
			}
			r.Logger.Info("Reconnecting to Docker daemon", zap.Duration("delay", reconnectDelay))
			observability.DaemonReconnectsTotal.WithLabelValues("attempt").Inc()
		}
	}
}

// periodicReconciliation runs every 10 minutes to verify runtime state consistency
func (r *ReloaderController) periodicReconciliation(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := time.Now()
			r.reconcileRuntimeState(ctx)
			duration := time.Since(start).Seconds()
			observability.ReconciliationDurationSeconds.Observe(duration)
		}
	}
}

// reconcileRuntimeState verifies that registered containers still exist and are labeled correctly
func (r *ReloaderController) reconcileRuntimeState(ctx context.Context) {
	r.Logger.Info("Starting periodic runtime reconciliation")

	// Count total targets before cleanup
	targetCount := 0
	r.Targets.Range(func(key, value interface{}) bool {
		targetCount++
		return true
	})

	orphaned := make([]string, 0)
	r.Targets.Range(func(key, value interface{}) bool {
		containerID := key.(string)
		// Try to inspect container
		_, err := r.cli.ContainerInspect(ctx, containerID)
		if err != nil {
			// Container no longer exists
			orphaned = append(orphaned, containerID)
			r.Logger.Debug("Found orphaned container", zap.String("id", containerID), zap.Error(err))
		}
		return true
	})

	// Clean up orphaned containers
	for _, id := range orphaned {
		r.Targets.Delete(id)
		r.Logger.Info("Cleaned up orphaned container from tracking", zap.String("id", id))
	}

	remainingCount := targetCount - len(orphaned)

	// Pass 2: re-register labeled containers that started while the event stream
	// was disconnected (their start events were missed).
	added := 0
	labelFilter := filters.NewArgs()
	labelFilter.Add("label", "dso.reloader=true")
	labeledContainers, err := r.cli.ContainerList(ctx, container.ListOptions{Filters: labelFilter})
	if err != nil {
		r.Logger.Warn("Reconciliation: failed to list labeled containers", zap.Error(err))
	} else {
		for _, c := range labeledContainers {
			if _, exists := r.Targets.Load(c.ID); exists {
				continue
			}
			secretsStr := c.Labels["dso.secrets"]
			if secretsStr == "" {
				continue
			}
			strat := c.Labels["dso.update.strategy"]
			if strat == "" {
				strat = "restart"
			}
			secretList := strings.Split(secretsStr, ",")
			r.Targets.Store(c.ID, &TargetContainer{
				ID:          c.ID,
				Strategy:    strat,
				ComposePath: c.Labels["dso.compose.path"],
				Secrets:     secretList,
			})
			r.Logger.Info("Reconciliation: re-registered container missed during reconnect",
				zap.String("id", c.ID[:12]),
				zap.Strings("secrets", secretList))
			added++

			// Also re-register with the proxy if this container has host-port mappings.
			// If the start event was missed the proxy has no backend for this container.
			if r.ProxyManager != nil {
				if portsLabel := c.Labels["dso.host_ports"]; portsLabel != "" {
					inspect, ierr := r.cli.ContainerInspect(ctx, c.ID)
					if ierr == nil {
						containerIP := ""
						for _, ep := range inspect.NetworkSettings.Networks {
							if ep.IPAddress != "" {
								containerIP = ep.IPAddress
								break
							}
						}
						if containerIP != "" {
							for _, pm := range dsoProxy.ParseHostPorts(portsLabel) {
								if err := r.ProxyManager.EnsurePort(pm.HostPort, pm.ContainerPort); err != nil {
									r.Logger.Warn("Reconciliation: failed to bind proxy port",
										zap.Int("port", pm.HostPort), zap.Error(err))
									continue
								}
								if err := r.ProxyManager.RegisterContainer(c.ID, containerIP, pm.HostPort, pm.ContainerPort); err != nil {
									r.Logger.Debug("Reconciliation: container already in proxy registry",
										zap.String("id", c.ID[:12]))
								}
							}
						}
					}
				}
			}
		}
	}

	r.Logger.Info("Runtime reconciliation complete",
		zap.Int("total_targets", targetCount),
		zap.Int("orphaned", len(orphaned)),
		zap.Int("re_added", added),
		zap.Int("active", remainingCount+added))
}

// handleContainerEventWithContext wraps handleContainerEvent for use with the bounded queue
func (r *ReloaderController) handleContainerEventWithContext(ctx context.Context, msg events.Message) error {
	return r.handleContainerEvent(msg)
}

func (r *ReloaderController) handleContainerEvent(msg events.Message) error {
	// Defensive: validate container ID
	if msg.Actor.ID == "" {
		r.Logger.Warn("Received event with empty container ID", zap.String("action", string(msg.Action)))
		return nil
	}

	// Defensive: validate attributes map exists
	if msg.Actor.Attributes == nil {
		r.Logger.Debug("Event has nil attributes", zap.String("id", msg.Actor.ID), zap.String("action", string(msg.Action)))
		return nil
	}

	switch msg.Action {
	case "start":
		_, hasLabel := msg.Actor.Attributes["dso.reloader"]
		if hasLabel {
			// --- Label-driven registration ---
			strategy := msg.Actor.Attributes["dso.update.strategy"]
			if strategy == "" {
				strategy = "restart"
			}

			// Validate strategy
			validStrategies := map[string]bool{"restart": true, "signal": true, "rolling": true, "none": true, "auto": true}
			if !validStrategies[strategy] {
				r.Logger.Warn("Invalid rotation strategy, defaulting to restart", zap.String("id", msg.Actor.ID), zap.String("strategy", strategy))
				strategy = "restart"
			}

			composePath := msg.Actor.Attributes["dso.compose.path"]
			secretsStr := msg.Actor.Attributes["dso.secrets"]

			// Defensive: handle empty secrets
			if secretsStr == "" {
				r.Logger.Warn("Container has no secrets configured", zap.String("id", msg.Actor.ID))
				return nil
			}

			// Parse and trim secret names
			secretList := make([]string, 0)
			for _, s := range strings.Split(secretsStr, ",") {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					secretList = append(secretList, trimmed)
				}
			}

			if len(secretList) == 0 {
				r.Logger.Warn("Container has no valid secrets after parsing", zap.String("id", msg.Actor.ID))
				return nil
			}

			r.Targets.Store(msg.Actor.ID, &TargetContainer{
				ID:          msg.Actor.ID,
				Strategy:    strategy,
				ComposePath: composePath,
				Secrets:     secretList,
			})
			r.Logger.Info("Registered target container dynamically (label-driven)", zap.String("id", msg.Actor.ID), zap.Strings("secrets", secretList))
		} else if r.Config != nil {
			// --- Config-driven registration fallback ---
			// Register containers that start without the DSO label if they are
			// listed in secrets[].targets.containers, or if no target restriction is set.
			cname := strings.TrimPrefix(msg.Actor.Attributes["name"], "/")
			if cname == "" {
				cname = msg.Actor.ID
			}

			var matchedSecrets []string
			for _, sec := range r.Config.Secrets {
				if len(sec.Targets.Containers) == 0 {
					// No explicit targets — label-driven registration covers these containers
					continue
				}
				for _, target := range sec.Targets.Containers {
					if target == cname || target == msg.Actor.ID {
						matchedSecrets = append(matchedSecrets, sec.Name)
						break
					}
				}
			}

			if len(matchedSecrets) > 0 {
				strategy := r.Config.Agent.Rotation.Strategy
				if strategy == "" {
					strategy = r.Config.Defaults.Rotation.Strategy
				}
				if strategy == "" {
					strategy = "restart"
				}
				r.Targets.Store(msg.Actor.ID, &TargetContainer{
					ID:       msg.Actor.ID,
					Strategy: strategy,
					Secrets:  matchedSecrets,
				})
				r.Logger.Info("Registered target container dynamically (config-driven)",
					zap.String("name", cname),
					zap.String("id", msg.Actor.ID),
					zap.Strings("secrets", matchedSecrets),
					zap.String("strategy", strategy))
			}
		}
		// Register with proxy for any container that has DSO-managed host ports
		if r.ProxyManager != nil {
			if portsLabel := msg.Actor.Attributes["dso.host_ports"]; portsLabel != "" {
				inspect, ierr := r.cli.ContainerInspect(context.Background(), msg.Actor.ID)
				if ierr == nil {
					containerIP := ""
					for _, ep := range inspect.NetworkSettings.Networks {
						if ep.IPAddress != "" {
							containerIP = ep.IPAddress
							break
						}
					}
					if containerIP != "" {
						for _, pm := range dsoProxy.ParseHostPorts(portsLabel) {
							if err := r.ProxyManager.EnsurePort(pm.HostPort, pm.ContainerPort); err != nil {
								r.Logger.Warn("proxy: failed to bind port on container start",
									zap.Int("port", pm.HostPort), zap.Error(err))
								continue
							}
							if err := r.ProxyManager.RegisterContainer(msg.Actor.ID, containerIP, pm.HostPort, pm.ContainerPort); err != nil {
								r.Logger.Warn("proxy: failed to register container",
									zap.String("id", msg.Actor.ID[:12]), zap.Error(err))
							}
						}
					}
				}
			}
		}
	case "die", "stop":
		if _, loaded := r.Targets.LoadAndDelete(msg.Actor.ID); loaded {
			r.Logger.Info("De-registered target container", zap.String("id", msg.Actor.ID))
		}
		if r.ProxyManager != nil {
			r.ProxyManager.DeregisterContainer(msg.Actor.ID)
		}
	}
	return nil
}

func (r *ReloaderController) populateInitialTargets(ctx context.Context) {
	// Pass 1: discover containers with the explicit dso.reloader=true label
	labelFilter := filters.NewArgs()
	labelFilter.Add("label", "dso.reloader=true")

	labeledContainers, err := r.cli.ContainerList(ctx, container.ListOptions{Filters: labelFilter})
	if err != nil {
		r.Logger.Error("Failed to list labeled containers", zap.Error(err))
		return
	}

	registered := 0
	for _, c := range labeledContainers {
		strategy := c.Labels["dso.update.strategy"]
		if strategy == "" {
			strategy = "restart"
		}
		composePath := c.Labels["dso.compose.path"]
		secretList := strings.Split(c.Labels["dso.secrets"], ",")

		r.Targets.Store(c.ID, &TargetContainer{
			ID:          c.ID,
			Strategy:    strategy,
			ComposePath: composePath,
			Secrets:     secretList,
		})
		r.Logger.Debug("Initial population: Labeled container found", zap.String("id", c.ID), zap.Strings("secrets", secretList))
		registered++
	}

	// Pass 2: config-driven fallback — discover containers without the dso.reloader label
	// by matching running container names against secrets[].targets.containers in the config.
	// Only containers listed in targets.containers are registered; secrets with no explicit
	// targets are covered by label-driven registration (Pass 1) via docker dso compose up.
	if r.Config != nil {
		allContainers, err := r.cli.ContainerList(ctx, container.ListOptions{})
		if err != nil {
			r.Logger.Warn("Config-driven discovery: failed to list all containers", zap.Error(err))
		} else {
			for _, c := range allContainers {
				// Skip already-registered containers
				if _, exists := r.Targets.Load(c.ID); exists {
					continue
				}

				// Derive the canonical container name (first name without leading /)
				cname := ""
				if len(c.Names) > 0 {
					cname = strings.TrimPrefix(c.Names[0], "/")
				}

				// Collect every secret this container is explicitly targeted for
				var matchedSecrets []string
				for _, sec := range r.Config.Secrets {
					if len(sec.Targets.Containers) == 0 {
						// No explicit targets — label-driven registration covers these containers
						continue
					}
					for _, target := range sec.Targets.Containers {
						if target == cname || target == c.ID {
							matchedSecrets = append(matchedSecrets, sec.Name)
							break
						}
					}
				}

				if len(matchedSecrets) == 0 {
					continue
				}

				// Determine strategy from config defaults
				strategy := r.Config.Agent.Rotation.Strategy
				if strategy == "" {
					strategy = r.Config.Defaults.Rotation.Strategy
				}
				if strategy == "" {
					strategy = "restart"
				}

				r.Targets.Store(c.ID, &TargetContainer{
					ID:       c.ID,
					Strategy: strategy,
					Secrets:  matchedSecrets,
				})
				r.Logger.Info("Config-driven discovery: registered container without dso.reloader label",
					zap.String("name", cname),
					zap.String("id", c.ID),
					zap.Strings("secrets", matchedSecrets),
					zap.String("strategy", strategy))
				registered++
			}
		}
	}

	r.Logger.Info("Initial container population complete", zap.Int("count", registered))
}

func (r *ReloaderController) TriggerReload(ctx context.Context, secretName string) error {
	matchedCount := 0
	// Step 1: Identify all affected containers and their strategies
	type restartJob struct {
		target      *TargetContainer
		preInjected map[string]string
		releaseLock func()
	}
	projectsToRestart := make(map[string]restartJob)

	r.Targets.Range(func(key, value interface{}) bool {
		target := value.(*TargetContainer)

		usesSecret := false
		if secretName == "" {
			usesSecret = true
		} else {
			for _, s := range target.Secrets {
				if strings.TrimSpace(s) == secretName {
					usesSecret = true
					break
				}
			}
		}

		if !usesSecret {
			return true
		}

		matchedCount++

		// EXTRACT SERVICE NAME FOR LOCKING
		serviceName := strings.TrimPrefix(target.ID, "/")
		inspect, err := r.cli.ContainerInspect(ctx, target.ID)
		if err == nil {
			if sn, ok := inspect.Config.Labels["com.docker.compose.service"]; ok {
				serviceName = sn
			}
		}

		// STALE LOCK RECOVERY (5 MINUTES)
		if val, busy := r.rotationLocks.Load(serviceName); busy {
			info := val.(*lockInfo)
			if time.Since(info.startTime) > 5*time.Minute {
				r.Logger.Warn("Reclaiming stale rotation lock", zap.String("service", serviceName))
				r.rotationLocks.Delete(serviceName)
			} else {
				r.Logger.Debug("Rotation already in progress, skipping", zap.String("service", serviceName))
				return true
			}
		}

		r.rotationLocks.Store(serviceName, &lockInfo{startTime: time.Now(), serviceName: serviceName})

		releaseLock := func() {
			r.rotationLocks.Delete(serviceName)
		}

		activeStrategy := target.Strategy
		if activeStrategy == "auto" || activeStrategy == "" {
			if inspect.Config != nil {
				analysisResult := analyzer.AnalyzeContainer(inspect)
				decision := strategy.DecideStrategy(analysisResult)
				activeStrategy = decision.Strategy
			} else {
				activeStrategy = "restart"
			}
		}

		// DE-DUPLICATE RESTART STRATEGY BY COMPOSE PATH
		if target.ComposePath != "" && activeStrategy == "restart" {
			if _, exists := projectsToRestart[target.ComposePath]; !exists {
				// Build the pre-injected secrets map from cache
				preInjected := make(map[string]string)
				if r.Cache != nil && r.Config != nil {
					for _, sec := range r.Config.Secrets {
						pName := sec.Provider
						if pName == "" {
							for name := range r.Config.Providers {
								pName = name
								break
							}
						}
						cacheKey := fmt.Sprintf("%s:%s", pName, sec.Name)
						if data, found := r.Cache.Get(cacheKey); found {
							for provKey, envName := range sec.Mappings {
								if val, ok := data[envName]; ok {
									preInjected[envName] = val
								} else if val, ok := data[provKey]; ok {
									preInjected[envName] = val
								}
							}
						}
					}
				}
				projectsToRestart[target.ComposePath] = restartJob{
					target:      target,
					preInjected: preInjected,
					releaseLock: releaseLock,
				}
			} else {
				// Already handled by project leader
				releaseLock()
			}
			return true
		}

		// EXECUTE PER-CONTAINER STRATEGIES (NON-COMPOSE)
		if activeStrategy == "signal" {
			r.Logger.Info("Sending SIGHUP to container", zap.String("id", target.ID))
			_ = r.cli.ContainerKill(ctx, target.ID, "SIGHUP")
			releaseLock()
		} else if activeStrategy == "rolling" {
			r.Logger.Info("🚀 Executing Zero-Downtime Rolling Rotation", zap.String("id", target.ID))
			RecordDSOAction(target.ID)

			// Build the new secret values from cache so the new container gets
			// the rotated secrets. Without this, rolling creates an identical
			// container with the old secrets and the rotation has no effect.
			rollingEnvs := make(map[string]string)
			if r.Cache != nil && r.Config != nil {
				for _, sec := range r.Config.Secrets {
					pName := sec.Provider
					if pName == "" {
						for name := range r.Config.Providers {
							pName = name
							break
						}
					}
					containerUsesSecret := false
					for _, ts := range target.Secrets {
						if ts == sec.Name {
							containerUsesSecret = true
							break
						}
					}
					if !containerUsesSecret {
						continue
					}
					cacheKey := fmt.Sprintf("%s:%s", pName, sec.Name)
					if data, found := r.Cache.Get(cacheKey); found {
						for provKey, envName := range sec.Mappings {
							if val, ok := data[envName]; ok {
								rollingEnvs[envName] = val
							} else if val, ok := data[provKey]; ok {
								rollingEnvs[envName] = val
							}
						}
					}
				}
			}

			healthTimeout := 60 * time.Second
			if r.Config != nil && r.Config.Agent.Rotation.HealthCheckTimeout != "" {
				if d, err := time.ParseDuration(r.Config.Agent.Rotation.HealthCheckTimeout); err == nil {
					healthTimeout = d
				}
			}

			rs := rotation.NewRollingStrategyWithLogger(r.cli, r.Logger)

			go func(newEnvs map[string]string, hTimeout time.Duration) {
				if r.Server != nil {
					if as, ok := r.Server.(interface{ Emit(string) }); ok {
						as.Emit(fmt.Sprintf("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: rolling\n🔄 Rolling Swap Start: %s", target.ID[:12]))
					}
				}

				err := rs.Execute(ctx, target.ID, newEnvs, hTimeout)
				releaseLock()
				if err != nil {
					r.Logger.Error("Rolling rotation failed, triggering fallback restart", zap.Error(err))
					if r.Server != nil {
						if as, ok := r.Server.(interface{ Emit(string) }); ok {
							as.Emit(fmt.Sprintf("\033[1;31m[DSO FALLBACK]\033[0m\nRolling failed due to: %v → switching to restart", err))
						}
					}
					// Fallback: full container recreation via shared helper (L5).
					if ferr := r.executeSimpleRestart(ctx, target.ID, newEnvs); ferr != nil {
						r.Logger.Error("Fallback restart also failed", zap.Error(ferr))
					}
				}
			}(rollingEnvs, healthTimeout)

		} else if activeStrategy == "restart" {
			r.Logger.Info("Restarting container (Full Recreation)", zap.String("id", target.ID))
			RecordDSOAction(target.ID)

			if r.Server != nil {
				if as, ok := r.Server.(interface{ Emit(string) }); ok {
					as.Emit("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: restart\nStopping container → removing → recreating with new secrets → starting")
				}
			}

			go func() {
				// 1. Inspect original container
				inspect, err := r.cli.ContainerInspect(ctx, target.ID)
				if err != nil {
					r.Logger.Error("Failed to inspect container for restart", zap.Error(err))
					releaseLock()
					return
				}

				originalName := strings.TrimPrefix(inspect.Name, "/")
				tempOldName := originalName + "_old_" + fmt.Sprintf("%d", time.Now().Unix())

				// 2. Fetch latest environment variables from Cache
				newEnvs := make(map[string]string)
				if r.Cache != nil && r.Config != nil {
					for _, sec := range r.Config.Secrets {
						// 2.1 Identify Provider
						pName := sec.Provider
						if pName == "" {
							if len(r.Config.Providers) == 1 {
								for name := range r.Config.Providers {
									pName = name
									break
								}
							}
						}

						containerUsesSecret := false
						for _, ts := range target.Secrets {
							if ts == sec.Name {
								containerUsesSecret = true
								break
							}
						}

						if containerUsesSecret {
							cacheKey := fmt.Sprintf("%s:%s", pName, sec.Name)
							if data, found := r.Cache.Get(cacheKey); found {
								for keyInProvider, envName := range sec.Mappings {
									if val, ok := data[envName]; ok {
										newEnvs[envName] = val
									} else if val, ok := data[keyInProvider]; ok {
										newEnvs[envName] = val
									}
								}
							}
						}
					}
				}

				// 3. Rename old to clear path
				if err := r.cli.ContainerRename(ctx, target.ID, tempOldName); err != nil {
					r.Logger.Error("Failed to rename original container", zap.Error(err))
					releaseLock()
					return
				}

				// 4. Prepare new config
				config := inspect.Config
				// Clean up state that shouldn't be copied
				config.Hostname = ""

				// Merge new envs
				for k, v := range newEnvs {
					found := false
					for i, e := range config.Env {
						if strings.HasPrefix(e, k+"=") {
							config.Env[i] = fmt.Sprintf("%s=%s", k, v)
							found = true
							break
						}
					}
					if !found {
						config.Env = append(config.Env, fmt.Sprintf("%s=%s", k, v))
					}
				}

				// 5. Create new instance with ORIGINAL name (STOPPED)
				networkingConfig := &network.NetworkingConfig{
					EndpointsConfig: inspect.NetworkSettings.Networks,
				}
				created, err := r.cli.ContainerCreate(ctx, config, inspect.HostConfig, networkingConfig, nil, originalName)
				if err != nil {
					r.Logger.Error("Failed to create new container, rolling back name", zap.Error(err))
					_ = r.cli.ContainerRename(ctx, target.ID, originalName)
					releaseLock()
					return
				}

				// 5.1 PRE-START SECRET INJECTION (TAR STREAMING)
				for _, secretName := range target.Secrets {
					var targetMapping *dsoConfig.SecretMapping
					for _, sm := range r.Config.Secrets {
						if sm.Name == secretName {
							targetMapping = &sm
							break
						}
					}

					if targetMapping != nil {
						injectConfig := targetMapping.Inject
						if injectConfig.Type == "" {
							injectConfig = r.Config.Defaults.Inject
						}

						if injectConfig.Type == "file" && injectConfig.Path != "" {
							pName := targetMapping.Provider
							if pName == "" {
								if len(r.Config.Providers) == 1 {
									for name := range r.Config.Providers {
										pName = name
										break
									}
								}
							}

							cacheKey := fmt.Sprintf("%s:%s", pName, secretName)
							if data, found := r.Cache.Get(cacheKey); found {
								if err := rotation.StreamSecretToContainer(ctx, r.cli, created.ID, injectConfig.Path, data, injectConfig.UID, injectConfig.GID); err != nil {
									r.Logger.Error("Secret injection failed", zap.Error(err))
								}
							}
						}
					}
				}

				// 6. Stop old and Start new
				// When the DSO proxy owns the host port (dso.host_ports label present),
				// start new BEFORE stopping old so traffic never drops — proxy swap is
				// atomic. Without proxy ownership we must stop old first to free the port.
				portsLabel := inspect.Config.Labels["dso.host_ports"]
				useZeroDowntime := r.ProxyManager != nil && portsLabel != ""
				stopTimeout := 10

				if !useZeroDowntime {
					_ = r.cli.ContainerStop(ctx, target.ID, container.StopOptions{Timeout: &stopTimeout})
				}

				if err := r.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
					r.Logger.Error("Failed to start new container, rolling back with retries", zap.Error(err))
					r.executeRollback(ctx, created.ID, target.ID, originalName, serviceName)
					releaseLock()
					return
				}

				// 7. Health Check & Finalize
				healthTimeout := 60 * time.Second
				if r.Config != nil && r.Config.Agent.Rotation.HealthCheckTimeout != "" {
					if d, err := time.ParseDuration(r.Config.Agent.Rotation.HealthCheckTimeout); err == nil {
						healthTimeout = d
					}
				}

				if err := rotation.WaitHealthy(ctx, r.cli, created.ID, healthTimeout); err != nil {
					r.Logger.Error("New container unhealthy, rolling back with retries", zap.Error(err))
					_ = r.cli.ContainerStop(ctx, created.ID, container.StopOptions{Timeout: &stopTimeout})
					r.executeRollback(ctx, created.ID, target.ID, originalName, serviceName)
					releaseLock()
					return
				}

				// 7.1 EXEC PROBE FOR SECRET EXISTENCE
				for _, secretName := range target.Secrets {
					var targetMapping *dsoConfig.SecretMapping
					for _, sm := range r.Config.Secrets {
						if sm.Name == secretName {
							targetMapping = &sm
							break
						}
					}
					if targetMapping != nil && targetMapping.Inject.Type == "file" {
						if err := rotation.ExecProbe(ctx, r.cli, created.ID, targetMapping.Inject.Path, 15*time.Second, 3); err != nil {
							r.Logger.Error("Exec probe failed, rolling back with retries", zap.Error(err), zap.String("path", targetMapping.Inject.Path))
							_ = r.cli.ContainerStop(ctx, created.ID, container.StopOptions{Timeout: &stopTimeout})
							r.executeRollback(ctx, created.ID, target.ID, originalName, serviceName)
							releaseLock()
							return
						}
					}
				}

				if useZeroDowntime {
					// Proxy swap: new container gets all traffic immediately;
					// old connections drain on old backend within 5 s.
					newInspect, ierr := r.cli.ContainerInspect(ctx, created.ID)
					if ierr == nil {
						newIP := ""
						for _, ep := range newInspect.NetworkSettings.Networks {
							if ep.IPAddress != "" {
								newIP = ep.IPAddress
								break
							}
						}
						if newIP != "" {
							for _, pm := range dsoProxy.ParseHostPorts(portsLabel) {
								if serr := r.ProxyManager.SwapBackend(
									target.ID, created.ID, newIP,
									pm.ContainerPort, pm.HostPort,
								); serr != nil {
									r.Logger.Warn("proxy swap failed, continuing", zap.Error(serr))
								}
							}
						}
					}
					// Traffic is on new container; safe to stop old now
					_ = r.cli.ContainerStop(ctx, target.ID, container.StopOptions{Timeout: &stopTimeout})
				}

				r.Logger.Info("Rotation successful, removing old container", zap.String("id", target.ID))
				_ = r.cli.ContainerRemove(ctx, target.ID, container.RemoveOptions{Force: true})
				r.degraded.Delete(serviceName)
				releaseLock()
			}()
		} else {
			// If we skipped all strategies (e.g. signal), release lock immediately
			releaseLock()
		}
		return true
	})

	// STAGE 2: EXECUTE DE-DUPLICATED COMPOSE RESTARTS
	for path, job := range projectsToRestart {
		target := job.target
		preInjected := job.preInjected
		releaseLock := job.releaseLock

		r.Logger.Info("Triggering de-duplicated Docker Compose rotation", zap.String("path", path))

		go func(p string, t *TargetContainer, pi map[string]string, rl func()) {
			RecordDSOAction(filepath.Base(filepath.Dir(p)))

			if r.Server != nil {
				if as, ok := r.Server.(interface{ Emit(string) }); ok {
					as.Emit(fmt.Sprintf("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: restart (compose)\n🔄 Native rotation: Scaling %s from compose context.", p))
				}
			}

			// Pass pre-injected secrets directly — avoids calling back into the agent socket (deadlock fix)
			err := core.RunComposeUpWithEnv(p, []string{"-d", "--remove-orphans"}, "", false, pi)
			rl()

			if err != nil {
				r.Logger.Error("Background rotation failed", zap.Error(err))
				if r.Server != nil {
					if as, ok := r.Server.(interface{ Emit(string) }); ok {
						as.Emit(fmt.Sprintf("\033[1;31m[DSO ROTATION]\033[0m ❌ Background rotation failed: %s (Check agent logs for details)", p))
					}
				}
			} else {
				r.Logger.Info("Background rotation successful", zap.String("path", p))
				if r.Server != nil {
					if as, ok := r.Server.(interface{ Emit(string) }); ok {
						as.Emit(fmt.Sprintf("\033[1;32m[DSO ROTATION]\033[0m ✅ Background rotation successful: %s", p))
					}
				}
			}
		}(path, target, preInjected, releaseLock)
	}

	if matchedCount == 0 {
		msg := fmt.Sprintf("No managed containers found using secret: %s", secretName)
		r.Logger.Warn(msg)

		// DIAGNOSTIC LOG: Print everything we KNOW
		r.Targets.Range(func(k, v interface{}) bool {
			target := v.(*TargetContainer)
			r.Logger.Debug("Diagnostic Scan: Checking Target Map Entry",
				zap.String("id", target.ID),
				zap.Strings("linked_secrets", target.Secrets))
			return true
		})

		if r.Server != nil {
			if as, ok := r.Server.(interface{ Emit(string) }); ok {
				targetCount := 0
				r.Targets.Range(func(_, _ interface{}) bool { targetCount++; return true })

				as.Emit(fmt.Sprintf("\033[1;33m[DSO SCAN]\033[0m Target map scan complete. Total managed containers in memory: %d", targetCount))
				as.Emit("\033[1;33m[DSO ROTATION]\033[0m " + msg)
			}
		}
		return fmt.Errorf("no managed containers for secret: %s", secretName)
	}

	r.Logger.Info(fmt.Sprintf("Triggered rotation for %d managed containers using secret: %s", matchedCount, secretName))
	return nil
}

// executeRollback attempts to restore the original container after a failed rotation.
// It removes the newly-created replacement container and renames + restarts the original.
// On persistent failure it marks the service degraded.
// Callers must release the rotation lock after this returns.
func (r *ReloaderController) executeRollback(ctx context.Context, createdID, originalID, originalName, serviceName string) {
	for i := 0; i < 3; i++ {
		_ = r.cli.ContainerRemove(ctx, createdID, container.RemoveOptions{Force: true})
		_ = r.cli.ContainerRename(ctx, originalID, originalName)
		if err := r.cli.ContainerStart(ctx, originalID, container.StartOptions{}); err == nil {
			r.Logger.Info("Rollback successful", zap.Int("attempt", i+1), zap.String("service", serviceName))
			return
		}
		r.Logger.Warn("Rollback attempt failed, retrying",
			zap.Int("attempt", i+1),
			zap.String("service", serviceName))
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	r.degraded.Store(serviceName, "Rotation failed, rollback failed after 3 attempts")
	r.Logger.Error("CRITICAL: New container failed and rollback failed after all retries",
		zap.String("service", serviceName))
}

// executeSimpleRestart recreates containerID with envOverrides merged into its environment.
// Used by the rolling-strategy fallback path (L5). It does not perform health checks,
// exec probes, or proxy swaps — those belong to the primary restart strategy path.
func (r *ReloaderController) executeSimpleRestart(ctx context.Context, containerID string, envOverrides map[string]string) error {
	inspect, err := r.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("executeSimpleRestart: inspect failed: %w", err)
	}

	originalName := strings.TrimPrefix(inspect.Name, "/")
	tempOldName := originalName + "_old_" + fmt.Sprintf("%d", time.Now().Unix())

	if err := r.cli.ContainerRename(ctx, containerID, tempOldName); err != nil {
		return fmt.Errorf("executeSimpleRestart: rename failed: %w", err)
	}

	cfg := inspect.Config
	cfg.Hostname = ""
	for k, v := range envOverrides {
		found := false
		for i, e := range cfg.Env {
			if strings.HasPrefix(e, k+"=") {
				cfg.Env[i] = fmt.Sprintf("%s=%s", k, v)
				found = true
				break
			}
		}
		if !found {
			cfg.Env = append(cfg.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	networkingConfig := &network.NetworkingConfig{EndpointsConfig: inspect.NetworkSettings.Networks}
	created, err := r.cli.ContainerCreate(ctx, cfg, inspect.HostConfig, networkingConfig, nil, originalName)
	if err != nil {
		// Rename back so the original container keeps its name.
		_ = r.cli.ContainerRename(ctx, containerID, originalName)
		return fmt.Errorf("executeSimpleRestart: container create failed: %w", err)
	}

	stopTO := 10
	_ = r.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTO})

	if err := r.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = r.cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
		_ = r.cli.ContainerRename(ctx, containerID, originalName)
		_ = r.cli.ContainerStart(ctx, containerID, container.StartOptions{})
		return fmt.Errorf("executeSimpleRestart: start failed: %w", err)
	}

	_ = r.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
	return nil
}
