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
	Logger  *zap.Logger
	Targets sync.Map // map[string]*TargetContainer (key: containerID)
	cli     *client.Client
	Server  interface{}
	Cache         SecretCache
	Config        *dsoConfig.Config
	rotationLocks sync.Map // map[string]*lockInfo (key: service name)
	degraded      sync.Map // map[string]string (key: service name, val: error)
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

func (r *ReloaderController) StartEventLoop(ctx context.Context) {
	r.Logger.Info("Starting Docker Events loop for ReloaderController")

	go func() {
		r.populateInitialTargets(ctx)

		filterArgs := filters.NewArgs()
		filterArgs.Add("type", "container")
		filterArgs.Add("event", "start")
		filterArgs.Add("event", "die")
		filterArgs.Add("event", "stop")

		msgCh, errCh := r.cli.Events(ctx, events.ListOptions{Filters: filterArgs})

		for {
			select {
			case <-ctx.Done():
				r.Logger.Info("Shutting down ReloaderController event loop")
				r.cli.Close()
				return
			case err := <-errCh:
				r.Logger.Error("Docker Events API error", zap.Error(err))
				observability.BackendFailuresTotal.WithLabelValues("docker_events", "stream_error").Inc()
				time.Sleep(5 * time.Second)
				msgCh, errCh = r.cli.Events(ctx, events.ListOptions{Filters: filterArgs})
			case msg := <-msgCh:
				if msg.Action == "start" {
					if _, hasLabel := msg.Actor.Attributes["dso.reloader"]; hasLabel {
						strategy := msg.Actor.Attributes["dso.update.strategy"]
						if strategy == "" {
							strategy = "restart"
						}
						composePath := msg.Actor.Attributes["dso.compose.path"]
						secretList := strings.Split(msg.Actor.Attributes["dso.secrets"], ",")

						r.Targets.Store(msg.Actor.ID, &TargetContainer{
							ID:          msg.Actor.ID,
							Strategy:    strategy,
							ComposePath: composePath,
							Secrets:     secretList,
						})
						r.Logger.Info("Registered target container dynamically", zap.String("id", msg.Actor.ID))
					}
				} else if msg.Action == "die" || msg.Action == "stop" {
					if _, loaded := r.Targets.LoadAndDelete(msg.Actor.ID); loaded {
						r.Logger.Info("De-registered target container", zap.String("id", msg.Actor.ID))
					}
				}
			}
		}
	}()
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
	}
	r.Logger.Info("Initial container population complete")
}

func (r *ReloaderController) TriggerReload(ctx context.Context, secretName string) error {
	r.Targets.Range(func(key, value interface{}) bool {
		target := value.(*TargetContainer)

		usesSecret := false
		if secretName == "" {
			usesSecret = true
		} else {
			for _, s := range target.Secrets {
				if s == secretName || strings.Contains(secretName, s) || strings.Contains(s, secretName) {
					usesSecret = true
					break
				}
			}
		}

		if !usesSecret {
			return true
		}

		// EXTRACT SERVICE NAME FOR LOCKING
		serviceName := strings.TrimPrefix(target.ID, "/") 
		inspect, err := r.cli.ContainerInspect(ctx, target.ID)
		if err == nil {
			if sn, ok := inspect.Config.Labels["com.docker.compose.service"]; ok {
				serviceName = sn
			}
		}

		// STALE LOCK RECOVERY (5 MINUTED)
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
		
		defer func() {
			// Lock cleanup is handled in goroutines for background tasks
		}()

		// Helper to release lock in background goroutines
		releaseLock := func() {
			r.rotationLocks.Delete(serviceName)
		}

		activeStrategy := target.Strategy
		if activeStrategy == "auto" || activeStrategy == "" {
			inspect, err := r.cli.ContainerInspect(ctx, target.ID)
			if err == nil {
				analysisResult := analyzer.AnalyzeContainer(inspect)
				decision := strategy.DecideStrategy(analysisResult)

				if r.Server != nil {
					if as, ok := r.Server.(interface{ Emit(string) }); ok {
						as.Emit("\n" + decision.Report)
					}
				}
				activeStrategy = decision.Strategy
			} else {
				activeStrategy = "restart"
			}
		}

		if target.ComposePath != "" && activeStrategy == "restart" {
			r.Logger.Info("Triggering native Docker Compose rotation via Core Engine", zap.String("path", target.ComposePath))
			
			go func() {
				RecordDSOAction(filepath.Base(filepath.Dir(target.ComposePath)))
				
				if r.Server != nil {
					if as, ok := r.Server.(interface{ Emit(string) }); ok {
						as.Emit(fmt.Sprintf("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: restart (compose)\n🔄 Native rotation: Scaling %s from compose context.", target.ComposePath))
					}
				}
				err := core.RunComposeUpWithEnv(target.ComposePath, []string{"-d"}, "", false)
				releaseLock()
				if err != nil {
					r.Logger.Error("Background rotation failed", zap.Error(err))
				} else {
					r.Logger.Info("Background rotation successful", zap.String("path", target.ComposePath))
				}
			}()
			return true
		}

		if activeStrategy == "signal" {
			r.Logger.Info("Sending SIGHUP to container", zap.String("id", target.ID))
			_ = r.cli.ContainerKill(ctx, target.ID, "SIGHUP")
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
									} else if val, ok := data[keyInProvider] ; ok {
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
	return nil
}
