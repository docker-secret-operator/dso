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
	rotationLocks sync.Map // map[string]*lockInfo (key: service name)
	degraded      sync.Map // map[string]string (key: service name, val: error)
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

	go func() {
		r.populateInitialTargets(ctx)
		r.daemonEventLoop(ctx)
	}()
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
	r.Logger.Info("Runtime reconciliation complete",
		zap.Int("total_targets", targetCount),
		zap.Int("orphaned", len(orphaned)),
		zap.Int("active", remainingCount))
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
		if _, hasLabel := msg.Actor.Attributes["dso.reloader"]; hasLabel {
			strategy := msg.Actor.Attributes["dso.update.strategy"]
			if strategy == "" {
				strategy = "restart"
			}

			// Validate strategy
			validStrategies := map[string]bool{"restart": true, "signal": true, "auto": true}
			if !validStrategies[strategy] {
				r.Logger.Warn("Invalid rotation strategy", zap.String("id", msg.Actor.ID), zap.String("strategy", strategy))
				strategy = "restart" // Default to safe strategy
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
			r.Logger.Info("Registered target container dynamically", zap.String("id", msg.Actor.ID), zap.Strings("secrets", secretList))
		}
	case "die", "stop":
		if _, loaded := r.Targets.LoadAndDelete(msg.Actor.ID); loaded {
			r.Logger.Info("De-registered target container", zap.String("id", msg.Actor.ID))
		}
	}
	return nil
}

func (r *ReloaderController) populateInitialTargets(ctx context.Context) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "dso.reloader=true")

	containers, err := r.cli.ContainerList(ctx, container.ListOptions{Filters: filterArgs})
	if err != nil {
		r.Logger.Error("Failed to list initial containers", zap.Error(err))
		return
	}

	for _, c := range containers {
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
		r.Logger.Debug("Initial population: Managed container found", zap.String("id", c.ID), zap.Strings("secrets", secretList))
	}
	r.Logger.Info("Initial container population complete", zap.Int("count", len(containers)))
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

			rs := rotation.NewRollingStrategy(r.cli)

			go func() {
				if r.Server != nil {
					if as, ok := r.Server.(interface{ Emit(string) }); ok {
						as.Emit(fmt.Sprintf("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: rolling\n🔄 Rolling Swap Start: %s", target.ID[:12]))
					}
				}

				err := rs.Execute(ctx, target.ID, map[string]string{}, 30*time.Second)
				releaseLock()
				if err != nil {
					r.Logger.Error("Rolling rotation failed, triggering fallback", zap.Error(err))
					if r.Server != nil {
						if as, ok := r.Server.(interface{ Emit(string) }); ok {
							as.Emit(fmt.Sprintf("\033[1;31m[DSO FALLBACK]\033[0m\nRolling failed due to: %v → switching to restart", err))
							as.Emit("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: restart\nStopping container → injecting new secrets → starting container")
						}
					}
					// FALLBACK
					timeout := 10
					_ = r.cli.ContainerStop(ctx, target.ID, container.StopOptions{Timeout: &timeout})
					_ = r.cli.ContainerStart(ctx, target.ID, container.StartOptions{})
				}
			}()

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
				stopTimeout := 10
				_ = r.cli.ContainerStop(ctx, target.ID, container.StopOptions{Timeout: &stopTimeout})

				if err := r.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
					r.Logger.Error("Failed to start new container, rolling back with retries", zap.Error(err))

					// ROLLBACK WITH 3 RETRIES
					for i := 0; i < 3; i++ {
						_ = r.cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
						_ = r.cli.ContainerRename(ctx, target.ID, originalName)
						if err := r.cli.ContainerStart(ctx, target.ID, container.StartOptions{}); err == nil {
							r.Logger.Info("Rollback successful", zap.Int("attempt", i+1))
							releaseLock()
							return
						}
						time.Sleep(time.Duration(i+1) * time.Second)
					}

					r.degraded.Store(serviceName, "Rotation failed, rollback failed after 3 attempts")
					r.Logger.Error("CRITICAL: Rollback failed for service", zap.String("service", serviceName))
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
					// Rollback logic (already there)
					_ = r.cli.ContainerStop(ctx, created.ID, container.StopOptions{Timeout: &stopTimeout})
					goto ROLLBACK
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
							goto ROLLBACK
						}
					}
				}

				r.Logger.Info("Rotation successful, removing old container", zap.String("id", target.ID))
				_ = r.cli.ContainerRemove(ctx, target.ID, container.RemoveOptions{Force: true})
				r.degraded.Delete(serviceName)
				goto FINISH

			ROLLBACK:
				for i := 0; i < 3; i++ {
					_ = r.cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
					_ = r.cli.ContainerRename(ctx, target.ID, originalName)
					if err := r.cli.ContainerStart(ctx, target.ID, container.StartOptions{}); err == nil {
						r.Logger.Info("Rollback successful", zap.Int("attempt", i+1))
						goto FINISH
					}
					time.Sleep(time.Duration(i+1) * time.Second)
				}

				r.degraded.Store(serviceName, "Rotation failed, rollback failed after 3 attempts")
				r.Logger.Error("CRITICAL: New container failed and rollback failed", zap.String("service", serviceName))

			FINISH:
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
				// Count existing targets for diagnosis
				targetCount := 0
				r.Targets.Range(func(_, _ interface{}) bool { targetCount++; return true })

				as.Emit(fmt.Sprintf("\033[1;33m[DSO SCAN]\033[0m Target map scan complete. Total managed containers in memory: %d", targetCount))
				as.Emit("\033[1;33m[DSO ROTATION]\033[0m " + msg)
			}
		}
	} else {
		msg := fmt.Sprintf("Triggered rotation for %d managed containers using secret: %s", matchedCount, secretName)
		r.Logger.Info(msg)
	}
	return nil
}
