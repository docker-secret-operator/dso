package watcher

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/analyzer"
	"github.com/docker-secret-operator/dso/internal/audit"
	"github.com/docker-secret-operator/dso/internal/core"
	eventqueue "github.com/docker-secret-operator/dso/internal/events"
	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/strategy"
	"github.com/docker-secret-operator/dso/internal/util"
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

	// pendingProxy holds container IDs of rotation replacements that have been
	// created (and may already be running) but have NOT yet passed their
	// readiness gate. The "start" event handler refuses to register these as
	// proxy backends, because the router would immediately send real client
	// traffic to an application that is still booting. The rotation path
	// clears the entry at the moment it performs the proxy handoff.
	//
	// This is deliberately in-memory and transient rather than a container
	// label: a label would persist on the container after promotion, so a
	// later operator-initiated `docker restart` would be skipped forever.
	// Losing this map on daemon restart is safe -- reconciliation re-registers
	// whatever is actually running, and recovery removes orphaned shadows.
	pendingProxy sync.Map // map[string]struct{} (key: container ID)
}

// ownerID returns this agent's dso.owner identity (see EnsureOwnerID in
// pkg/config), or "" if no Config is attached (e.g. some unit tests
// construct a bare ReloaderController). An empty ownerID is treated as
// "ownership checking unavailable" by checkOwnership below -- it does NOT
// fall back to matching everything, which would silently disable the fix
// this field exists for.
func (r *ReloaderController) ownerID() string {
	if r.Config == nil {
		return ""
	}
	return r.Config.Agent.OwnerID
}

// checkOwnership is the SEC-P0 discovery-isolation gate. It must be called
// by every path that discovers a dso.reloader=true labeled container before
// that container is registered into r.Targets (and therefore before it can
// ever be rotated, restarted, or otherwise mutated by this agent).
//
// Default-deny: a dso.reloader=true container with NO dso.owner label (a
// pre-fix "legacy" container) is rejected, not silently adopted. A
// container whose dso.owner does not match this agent's own ID is rejected.
// Only an exact match registers. If this agent has no ownerID configured at
// all (r.Config == nil, e.g. a unit test harness with no Config wired up),
// ownership enforcement is skipped entirely -- this mirrors pre-fix
// behavior for callers that never had a Config to enforce with, and is not
// reachable from a real `dso agent` process, which always loads a Config
// with EnsureOwnerID already applied.
func (r *ReloaderController) checkOwnership(containerID, containerName string, labels map[string]string) bool {
	self := r.ownerID()
	if self == "" {
		return true
	}
	owner, present := labels["dso.owner"]
	if !present || owner == "" {
		r.Logger.Warn("Found DSO-managed-looking container without dso.owner label; skipping until explicitly adopted",
			zap.String("id", util.ShortID(containerID)),
			zap.String("name", containerName))
		return false
	}
	if owner != self {
		r.Logger.Warn("Found dso.reloader container owned by a different DSO agent instance; skipping",
			zap.String("id", util.ShortID(containerID)),
			zap.String("name", containerName))
		return false
	}
	return true
}

// ownedByOtherAgent (also guards the start-event proxy registration) reports whether labels carry a dso.owner that names a
// DIFFERENT agent. The config-driven discovery fallbacks (a container matched
// by name in secrets[].targets.containers, without the dso.reloader label) are
// an explicit operator opt-in, so an unowned container named in the config is
// still honoured, but a container another DSO agent has claimed is never
// adopted. With no ownerID configured (bare test harness) nothing is denied,
// matching checkOwnership.
func (r *ReloaderController) ownedByOtherAgent(containerID, containerName string, labels map[string]string) bool {
	self := r.ownerID()
	if self == "" {
		return false
	}
	if owner := labels["dso.owner"]; owner != "" && owner != self {
		r.Logger.Warn("Container is owned by a different DSO agent instance; skipping",
			zap.String("id", util.ShortID(containerID)),
			zap.String("name", containerName))
		return true
	}
	return false
}

// markProxyPending marks containerID as not-yet-eligible for proxy traffic.
// Must be called before the container is started.
func (r *ReloaderController) markProxyPending(containerID string) {
	r.pendingProxy.Store(containerID, struct{}{})
}

// clearProxyPending removes the not-yet-ready marker, either because the
// container was promoted or because the rotation was abandoned.
func (r *ReloaderController) clearProxyPending(containerID string) {
	r.pendingProxy.Delete(containerID)
}

// isProxyPending reports whether containerID is a rotation replacement that
// has not yet passed its readiness gate.
func (r *ReloaderController) isProxyPending(containerID string) bool {
	_, pending := r.pendingProxy.Load(containerID)
	return pending
}

// recoverRotationPanic returns a function meant to be `defer`red as the
// first statement of a rotation goroutine (the rolling, restart, and
// compose-restart branches of TriggerReload). If that goroutine panics,
// this recovers it -- converting an otherwise fatal, whole-process crash
// into an ordinary rotation failure -- logs a safe diagnostic (container/
// path identifier and the panic value only; Go's own panic messages never
// carry secret material, and no secret is threaded through this call), and
// invokes onPanic exactly once so the caller can run its normal
// failure-path cleanup (idempotent lock release, marking the service
// degraded, recording the async error) through the SAME code its ordinary
// error-return path already uses, rather than a second, divergent cleanup
// path that could double-release a lock or double-clear pending-proxy
// state.
//
// P2-2 (Phase 6 audit): before this existed, a panic inside any of these
// goroutines was unrecovered and took down the entire DSO agent process.
func (r *ReloaderController) recoverRotationPanic(logCtx, idForLog string, onPanic func(err error, rec interface{})) func() {
	return func() {
		rec := recover()
		if rec == nil {
			return
		}
		err := fmt.Errorf("panic during %s: %v", logCtx, rec)
		r.Logger.Error("Recovered from panic in rotation goroutine; agent remains running",
			zap.String("context", logCtx),
			zap.String("id", idForLog),
			zap.Any("panic", rec))
		onPanic(err, rec)
	}
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

	var reconnectDelay time.Duration
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

	// PERF-5: determine which tracked containers still exist with ONE batched
	// list call instead of one ContainerInspect per tracked target. This runs
	// on a 10-minute ticker *and* on every Docker daemon reconnect, so the old
	// N+1 shape meant a host tracking 200 containers issued 200 round-trips
	// per cycle, and a flapping daemon produced a burst on each reconnect.
	//
	// All: true is required, not incidental: ContainerInspect succeeds for a
	// stopped container, so the previous code treated "stopped but present" as
	// existing. A default (running-only) list would reclassify every stopped
	// container as orphaned and silently drop it from tracking.
	existing, listErr := r.cli.ContainerList(ctx, container.ListOptions{All: true})

	remainingCount := targetCount
	orphanedCount := 0
	if listErr != nil {
		// Deliberately do not treat this as "everything is orphaned". A
		// transient API error must not wipe the entire tracking set; skip the
		// orphan sweep this cycle and let the next one reconcile.
		r.Logger.Warn("Reconciliation: failed to list containers, skipping orphan cleanup this cycle",
			zap.Error(listErr))
	} else {
		alive := make(map[string]struct{}, len(existing))
		for _, c := range existing {
			alive[c.ID] = struct{}{}
		}

		orphaned := make([]string, 0)
		r.Targets.Range(func(key, value interface{}) bool {
			containerID := key.(string)
			if _, ok := alive[containerID]; !ok {
				orphaned = append(orphaned, containerID)
				r.Logger.Debug("Found orphaned container", zap.String("id", containerID))
			}
			return true
		})

		// Clean up orphaned containers
		for _, id := range orphaned {
			r.Targets.Delete(id)
			r.Logger.Info("Cleaned up orphaned container from tracking", zap.String("id", id))
		}

		orphanedCount = len(orphaned)
		remainingCount = targetCount - orphanedCount
	}

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
			cname := ""
			if len(c.Names) > 0 {
				cname = strings.TrimPrefix(c.Names[0], "/")
			}
			if !r.checkOwnership(c.ID, cname, c.Labels) {
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
				zap.String("id", util.ShortID(c.ID)),
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
										zap.String("id", util.ShortID(c.ID)))
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
		zap.Int("orphaned", orphanedCount),
		zap.Int("re_added", added),
		zap.Int("active", remainingCount+added))
}

// handleContainerEventWithContext wraps handleContainerEvent for use with the bounded queue
func (r *ReloaderController) handleContainerEventWithContext(ctx context.Context, msg events.Message) error {
	return r.handleContainerEvent(ctx, msg)
}

// handleContainerEvent processes a single Docker event. CQ-C5: it threads the
// caller's context (carrying the agent shutdown signal and the bounded queue's
// per-event timeout) into every Docker API call instead of context.Background(),
// so a slow daemon can no longer pin a worker past shutdown or the event deadline.
func (r *ReloaderController) handleContainerEvent(ctx context.Context, msg events.Message) error {
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
		if hasLabel && !r.checkOwnership(msg.Actor.ID, strings.TrimPrefix(msg.Actor.Attributes["name"], "/"), msg.Actor.Attributes) {
			// SEC-P0 default-deny: a dso.reloader=true container that is
			// unowned or owned by a different agent must never be
			// registered, rotated, or proxy-registered by this agent --
			// stop processing this start event entirely.
			return nil
		}
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

			if len(matchedSecrets) > 0 && !r.ownedByOtherAgent(msg.Actor.ID, cname, msg.Actor.Attributes) {
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
		// Register with proxy for any container that has DSO-managed host ports.
		//
		// A rotation replacement is deliberately excluded here: it is started
		// long before its health check completes, so registering it now would
		// make the router select an application that is not listening yet and
		// drop real client requests. The rotation path registers it itself,
		// after readiness, via the proxy handoff (see markProxyPending).
		if r.ProxyManager != nil && !r.isProxyPending(msg.Actor.ID) &&
			!r.ownedByOtherAgent(msg.Actor.ID, strings.TrimPrefix(msg.Actor.Attributes["name"], "/"), msg.Actor.Attributes) {
			if portsLabel := msg.Actor.Attributes["dso.host_ports"]; portsLabel != "" {
				inspect, ierr := r.cli.ContainerInspect(ctx, msg.Actor.ID)
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
									zap.String("id", util.ShortID(msg.Actor.ID)), zap.Error(err))
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
		cname := ""
		if len(c.Names) > 0 {
			cname = strings.TrimPrefix(c.Names[0], "/")
		}
		if !r.checkOwnership(c.ID, cname, c.Labels) {
			continue
		}
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
				if r.ownedByOtherAgent(c.ID, cname, c.Labels) {
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

// resolveSecretProvider returns the configured provider name for secretName,
// for audit attribution. Mirrors the same provider-resolution fallback
// (explicit sec.Provider, else the sole configured provider) already used
// inline elsewhere in this file when building rotation env vars.
func (r *ReloaderController) resolveSecretProvider(secretName string) string {
	if r.Config == nil {
		return ""
	}
	for _, sec := range r.Config.Secrets {
		if sec.Name != secretName {
			continue
		}
		if sec.Provider != "" {
			return sec.Provider
		}
		for name := range r.Config.Providers {
			return name
		}
	}
	return ""
}

// TriggerReload identifies containers using secretName and rotates them. Some
// strategies (rolling, restart, compose) run their actual work in detached
// goroutines so TriggerReload can return quickly; onComplete (if non-nil) is
// invoked exactly once, after every one of those detached goroutines has
// finished, with the first error encountered (nil if all succeeded). This
// lets callers (e.g. StateTracker.CompleteRotation in agent/trigger.go) mark
// rotation state based on the real outcome instead of TriggerReload's early
// return (REL-1 fix).
func (r *ReloaderController) TriggerReload(ctx context.Context, secretName string, onComplete func(err error)) error {
	matchedCount := 0
	var asyncWG sync.WaitGroup
	var asyncMu sync.Mutex
	var asyncFirstErr error
	recordAsyncErr := func(err error) {
		if err == nil {
			return
		}
		asyncMu.Lock()
		if asyncFirstErr == nil {
			asyncFirstErr = err
		}
		asyncMu.Unlock()
	}
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
				// Build the pre-injected secrets map from cache.
				//
				// SEC: file-mode secrets (Inject.Type == "file") are excluded
				// -- this map is merged into the `docker compose up` subprocess
				// environment (core.RunComposeUpWithEnv), which can end up as
				// plaintext container env vars via compose interpolation. A
				// secret the operator configured for file-mode delivery has no
				// business flowing through env at all.
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

						injectConfig := sec.Inject
						if injectConfig.Type == "" {
							injectConfig = r.Config.Defaults.Inject
						}
						if injectConfig.Type == "file" {
							continue
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
			if err := r.cli.ContainerKill(ctx, target.ID, "SIGHUP"); err != nil {
				// Best-effort: SIGHUP-based reload already relies on the
				// target noticing a changed file and reloading itself, so a
				// signal that never arrived (e.g. the container disappeared
				// mid-rotation) has the same practical effect as one that
				// arrived but was ignored. Logged for operator visibility
				// rather than surfaced as a rotation failure.
				r.Logger.Debug("SIGHUP delivery failed, target likely already gone",
					zap.String("id", target.ID), zap.Error(err))
			}
			releaseLock()
		} else if activeStrategy == "rolling" {
			r.Logger.Info("🚀 Executing Zero-Downtime Rolling Rotation", zap.String("id", target.ID))
			RecordDSOAction(target.ID)

			// Build the new secret values from cache so the new container gets
			// the rotated secrets. Without this, rolling creates an identical
			// container with the old secrets and the rotation has no effect.
			//
			// SEC: only secrets configured for env-mode injection go into
			// rollingEnvs -- that map is merged directly into the new
			// container's Config.Env (PrepareShadowConfig/executeSimpleRestart),
			// which is visible via `docker inspect`. File-mode secrets
			// (Inject.Type == "file") are collected separately into
			// rollingFileSecrets and delivered via the existing
			// StreamSecretToContainer tar-copy mechanism instead, matching how
			// the compose-recreation path below already handles them.
			rollingEnvs := make(map[string]string)
			var rollingFileSecrets []rotation.FileSecretSpec
			// SEC-P0: every env name this container's config ever maps a secret
			// to, regardless of that secret's *current* inject type or whether
			// its cache entry is fresh. This is handed to executeSimpleRestart
			// so it can strip stale values it would otherwise inherit unchanged
			// from the original container's Config.Env (e.g. a secret that used
			// to be env-mode and has since been switched to file-mode, or whose
			// cache entry is temporarily missing) -- without this, a value once
			// written to Config.Env persists across every future recreation
			// even after the config no longer calls for it to be there.
			managedEnvNames := make(map[string]bool)
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
					for _, envName := range sec.Mappings {
						managedEnvNames[envName] = true
					}
					cacheKey := fmt.Sprintf("%s:%s", pName, sec.Name)
					data, found := r.Cache.Get(cacheKey)
					if !found {
						continue
					}

					injectConfig := sec.Inject
					if injectConfig.Type == "" {
						injectConfig = r.Config.Defaults.Inject
					}

					if injectConfig.Type == "file" {
						rollingFileSecrets = append(rollingFileSecrets, rotation.FileSecretSpec{
							Path: injectConfig.Path,
							Data: data,
							UID:  injectConfig.UID,
							GID:  injectConfig.GID,
						})
						continue
					}

					for provKey, envName := range sec.Mappings {
						if val, ok := data[envName]; ok {
							rollingEnvs[envName] = val
						} else if val, ok := data[provKey]; ok {
							rollingEnvs[envName] = val
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
			auditProvider := r.resolveSecretProvider(secretName)

			// Zero-downtime proxy handoff. The shadow is kept out of the proxy
			// until it is actually ready (OnShadowCreated), then traffic is
			// moved to it and the old backend is drained (OnReady) before the
			// old container is stopped by Execute's cleanup step.
			rollingPortsLabel := ""
			if inspect.Config != nil {
				rollingPortsLabel = inspect.Config.Labels["dso.host_ports"]
			}
			rollingOldID := target.ID
			rollingServiceName := serviceName
			// Only proxy-owned containers carry traffic that needs draining;
			// everything else keeps the previous immediate-cleanup behaviour.
			if r.ProxyManager != nil && rollingPortsLabel != "" {
				rs.PreCleanupDelay = dsoProxy.DrainWindow
			}
			// Written and read only by the rotation goroutine below (the hook
			// is invoked synchronously from inside rs.Execute), so no
			// synchronisation is needed here.
			var rollingShadowID string
			rs.OnShadowCreated = func(newID string) {
				rollingShadowID = newID
				r.markProxyPending(newID)
			}
			rs.OnReady = func(hctx context.Context, newID string) error {
				// Nothing to hand off when this container's ports aren't
				// proxy-owned: rotation is a pure Docker rename swap.
				if r.ProxyManager == nil || rollingPortsLabel == "" {
					r.clearProxyPending(newID)
					return nil
				}
				newInspect, ierr := r.cli.ContainerInspect(hctx, newID)
				if ierr != nil {
					return fmt.Errorf("proxy handoff: inspect new container: %w", ierr)
				}
				newIP := ""
				for _, ep := range newInspect.NetworkSettings.Networks {
					if ep.IPAddress != "" {
						newIP = ep.IPAddress
						break
					}
				}
				if newIP == "" {
					return fmt.Errorf("proxy handoff: new container %s has no IP address", util.ShortID(newID))
				}
				portMaps := dsoProxy.ParseHostPorts(rollingPortsLabel)
				if len(portMaps) == 0 {
					r.clearProxyPending(newID)
					return nil
				}
				// Every declared host port needs a bound listener.
				for _, pm := range portMaps {
					if err := r.ProxyManager.EnsurePort(pm.HostPort, pm.ContainerPort); err != nil {
						return fmt.Errorf("proxy handoff: ensure port %d: %w", pm.HostPort, err)
					}
				}
				// The registry holds exactly ONE backend entry per container
				// (keyed by container ID), so the swap is performed once --
				// looping it over ports would make every call after the first
				// fail with "already registered". That mattered: aborting on
				// that error after the first swap had already committed left
				// the new backend ACTIVE while the caller tore its container
				// down, black-holing all traffic. One swap, one outcome:
				// either nothing is committed and we abort safely, or the
				// handoff is complete.
				if err := r.ProxyManager.SwapBackend(
					rollingOldID, newID, newIP, portMaps[0].ContainerPort, portMaps[0].HostPort,
				); err != nil {
					return fmt.Errorf("proxy handoff: swap backend on port %d: %w", portMaps[0].HostPort, err)
				}
				// Promoted: future events for this container are normal.
				r.clearProxyPending(newID)
				r.Logger.Info("proxy: rotation handoff complete, new backend serving",
					zap.String("old", util.ShortID(rollingOldID)),
					zap.String("new", util.ShortID(newID)))
				return nil
			}

			// CQ-C4: pass target and releaseLock as explicit arguments so the
			// goroutine holds a stable, independent reference rather than capturing
			// the sync.Map range variable by reference. secretName/auditProvider are
			// read-only for the remainder of TriggerReload, so capturing them
			// directly would be safe too, but passing them explicitly keeps this
			// goroutine's inputs self-contained, matching the existing convention.
			asyncWG.Add(1)
			go func(tc *TargetContainer, release func(), newEnvs map[string]string, fileSecrets []rotation.FileSecretSpec, managedEnvs map[string]bool, hTimeout time.Duration, secret, provider string) {
				defer asyncWG.Done()
				// Safety net: whatever happens (health-check failure, secret
				// injection failure, failed handoff, panic-free early return),
				// never leave a stale not-ready marker behind. A leaked entry
				// would permanently suppress proxy registration for that ID.
				defer func() {
					if rollingShadowID != "" {
						r.clearProxyPending(rollingShadowID)
					}
				}()

				// P2-2: releaseOnce makes the lock release idempotent so the
				// panic-recovery defer below and the normal-path release()
				// call further down can never double-release the same lock,
				// regardless of which one fires.
				var released bool
				releaseOnce := func() {
					if !released {
						released = true
						release()
					}
				}
				// P2-2: converts an unexpected panic (rs.Execute, or anything
				// it calls) into an ordinary rotation failure instead of
				// crashing the agent -- lock released, outcome audited
				// failed, and (when proxy-owned) the service marked
				// degraded with the old container left running, matching
				// the existing non-panic failure branch below.
				defer r.recoverRotationPanic("rolling rotation", util.ShortID(tc.ID), func(err error, rec interface{}) {
					recordAsyncErr(err)
					audit.Log(ctx, "rotate", "system", provider, secret, tc.ID, "failed")
					if r.ProxyManager != nil && rollingPortsLabel != "" {
						r.markDegraded(rollingServiceName, fmt.Sprintf("rotation panicked, still serving previous secret: %v", rec))
					}
					releaseOnce()
				})()

				if r.Server != nil {
					if as, ok := r.Server.(interface{ Emit(string) }); ok {
						as.Emit(fmt.Sprintf("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: rolling\n🔄 Rolling Swap Start: %s", util.ShortID(tc.ID)))
					}
				}

				err := rs.Execute(ctx, tc.ID, newEnvs, fileSecrets, managedEnvs, hTimeout)
				releaseOnce()
				recordAsyncErr(err)
				if err != nil {
					// AUDIT-3: compliance trail for "secret X was rotated into
					// container Y" -- this is the point where the rolling
					// (blue-green) rotation's real, final outcome is known.
					audit.Log(ctx, "rotate", "system", provider, secret, tc.ID, "failed")

					// When this container's traffic is proxy-owned, the
					// fallback restart is NOT safe: it stops the old container
					// to recreate it, and it performs no readiness-gated proxy
					// handoff, so the proxy is left with no backend and clients
					// see a hard outage. A rotation that could not prove the
					// new secret healthy must not cost availability -- the old
					// container keeps serving the previous secret and the
					// service is flagged degraded for the operator instead.
					if r.ProxyManager != nil && rollingPortsLabel != "" {
						r.Logger.Error("Rolling rotation failed; keeping old container in service (proxy-managed, fallback restart would drop traffic)",
							zap.String("id", util.ShortID(tc.ID)),
							zap.Error(err))
						r.markDegraded(rollingServiceName, fmt.Sprintf("rotation failed, still serving previous secret: %v", err))
						if r.Server != nil {
							if as, ok := r.Server.(interface{ Emit(string) }); ok {
								as.Emit(fmt.Sprintf("\033[1;31m[DSO ROTATION]\033[0m\nRolling failed: %v → old container kept in service (no downtime); service marked degraded", err))
							}
						}
						return
					}

					r.Logger.Error("Rolling rotation failed, triggering fallback restart", zap.Error(err))
					if r.Server != nil {
						if as, ok := r.Server.(interface{ Emit(string) }); ok {
							as.Emit(fmt.Sprintf("\033[1;31m[DSO FALLBACK]\033[0m\nRolling failed due to: %v → switching to restart", err))
						}
					}
					// Fallback: full container recreation via shared helper (L5).
					if ferr := r.executeSimpleRestart(ctx, tc.ID, newEnvs, fileSecrets, managedEnvs); ferr != nil {
						r.Logger.Error("Fallback restart also failed", zap.Error(ferr))
					}
					return
				}
				audit.Log(ctx, "rotate", "system", provider, secret, tc.ID, "success")
				// A previous failed rotation may have marked this service
				// degraded while keeping the old secret in service. It has now
				// rotated successfully, so clear that state -- otherwise a
				// recovered service stays flagged forever, matching what the
				// restart path already does on success.
				r.clearDegraded(rollingServiceName)
			}(target, releaseLock, rollingEnvs, rollingFileSecrets, managedEnvNames, healthTimeout, secretName, auditProvider)

		} else if activeStrategy == "restart" {
			r.Logger.Info("Restarting container (Full Recreation)", zap.String("id", target.ID))
			RecordDSOAction(target.ID)

			if r.Server != nil {
				if as, ok := r.Server.(interface{ Emit(string) }); ok {
					as.Emit("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: restart\nStopping container → removing → recreating with new secrets → starting")
				}
			}

			// CQ-C4: target, releaseLock and serviceName are passed as explicit
			// arguments (shadowing the sync.Map range variables) so this goroutine
			// operates on stable, independent values even if the Targets map entry
			// is concurrently deleted or replaced.
			asyncWG.Add(1)
			go func(target *TargetContainer, releaseLock func(), serviceName string) {
				defer asyncWG.Done()
				// P2-2: finished guards finish() so it can never fire twice --
				// once from an explicit call on a normal error/success path,
				// and again from the panic-recovery defer below.
				var finished bool
				finish := func(err error) {
					if finished {
						return
					}
					finished = true
					recordAsyncErr(err)
					releaseLock()
				}
				// P2-2: converts an unexpected panic into an ordinary
				// rotation failure instead of crashing the agent -- lock
				// released via finish() and the service marked degraded,
				// matching how every other failure branch below already
				// leaves the operator a visible signal.
				defer r.recoverRotationPanic("restart rotation", util.ShortID(target.ID), func(err error, rec interface{}) {
					r.markDegraded(serviceName, fmt.Sprintf("rotation panicked: %v", rec))
					finish(err)
				})()

				// 1. Inspect original container
				inspect, err := r.cli.ContainerInspect(ctx, target.ID)
				if err != nil {
					r.Logger.Error("Failed to inspect container for restart", zap.Error(err))
					finish(err)
					return
				}

				originalName := strings.TrimPrefix(inspect.Name, "/")
				tempOldName := originalName + "_old_" + fmt.Sprintf("%d", time.Now().Unix())

				// 2. Fetch latest environment variables from Cache.
				//
				// SEC: file-mode secrets (Inject.Type == "file") are
				// deliberately excluded here -- they're delivered via
				// StreamSecretToContainer in step 5.1 below (PRE-START SECRET
				// INJECTION). Including them here too would additionally
				// leak them as plaintext into Config.Env / `docker inspect`.
				//
				// SEC-P0-2: managedEnvNames tracks every env name this
				// container's config maps ANY of its secrets to, regardless
				// of that secret's *current* inject type -- mirrors the fix
				// applied to executeSimpleRestart (the rolling-strategy
				// fallback path) for the same root cause: this branch clones
				// the original container's Config.Env verbatim below and
				// only ever adds/updates keys present in newEnvs, so without
				// this a secret's value once written to Config.Env (e.g.
				// while it was env-mode) would persist unchanged across
				// every future recreation even after it is switched to
				// file-mode or dropped from config entirely.
				newEnvs := make(map[string]string)
				managedEnvNames := make(map[string]bool)
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

						if !containerUsesSecret {
							continue
						}

						for _, envName := range sec.Mappings {
							managedEnvNames[envName] = true
						}

						injectConfig := sec.Inject
						if injectConfig.Type == "" {
							injectConfig = r.Config.Defaults.Inject
						}
						if injectConfig.Type == "file" {
							continue
						}

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

				// 3. Rename old to clear path
				if err := r.cli.ContainerRename(ctx, target.ID, tempOldName); err != nil {
					r.Logger.Error("Failed to rename original container", zap.Error(err))
					finish(err)
					return
				}

				// 4. Prepare new config. CQ-H5: deep-copy before mutating so we never
				// modify the struct returned by the Docker client.
				config := cloneContainerConfig(inspect.Config)
				// Clean up state that shouldn't be copied
				config.Hostname = ""

				// SEC-P0-2: drop any inherited env entry this rotation manages
				// but did not supply a fresh value for (e.g. switched to
				// file-mode, or dropped from config) -- see the doc comment
				// on managedEnvNames above for why this can't be skipped.
				if len(managedEnvNames) > 0 {
					filtered := make([]string, 0, len(config.Env))
					for _, e := range config.Env {
						k, _, _ := strings.Cut(e, "=")
						if _, stillOverridden := newEnvs[k]; managedEnvNames[k] && !stillOverridden {
							continue
						}
						filtered = append(filtered, e)
					}
					config.Env = filtered
				}

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
					// Strip the original's runtime network identity (MAC in
					// particular): this replacement runs alongside it until the
					// proxy handoff completes, and a duplicate MAC corrupts the
					// bridge for the container still serving traffic.
					EndpointsConfig: rotation.SanitizedEndpointsConfig(inspect.NetworkSettings.Networks),
				}
				created, err := r.cli.ContainerCreate(ctx, config, inspect.HostConfig, networkingConfig, nil, originalName)
				if err != nil {
					r.Logger.Error("Failed to create new container, rolling back name", zap.Error(err))
					r.restoreOriginalName(ctx, target.ID, originalName)
					finish(err)
					return
				}

				// Keep the replacement out of the proxy until it passes the
				// health check and exec probes below. Without this the "start"
				// event registers it as an active backend immediately, which
				// both routes traffic at a booting application and makes the
				// SwapBackend call further down fail with "already
				// registered" -- which would skip draining the old backend.
				r.markProxyPending(created.ID)
				defer r.clearProxyPending(created.ID)

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
					finish(err)
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
					r.stopReplacement(ctx, created.ID, stopTimeout)
					r.executeRollback(ctx, created.ID, target.ID, originalName, serviceName)
					finish(err)
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
							r.stopReplacement(ctx, created.ID, stopTimeout)
							r.executeRollback(ctx, created.ID, target.ID, originalName, serviceName)
							finish(err)
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
					// New connections now go to the replacement, but connections
					// that selected the old backend moments before the swap are
					// still dialing or mid-response. Marking a backend draining
					// only stops NEW routing -- killing the container now would
					// drop those in-flight requests, making the drain nominal.
					// Hold the old container for the proxy's drain window first.
					select {
					case <-time.After(dsoProxy.DrainWindow):
					case <-ctx.Done():
					}
					_ = r.cli.ContainerStop(ctx, target.ID, container.StopOptions{Timeout: &stopTimeout})
				}

				r.Logger.Info("Rotation successful, removing old container", zap.String("id", target.ID))
				_ = r.cli.ContainerRemove(ctx, target.ID, container.RemoveOptions{Force: true})
				r.clearDegraded(serviceName)
				finish(nil)
			}(target, releaseLock, serviceName)
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

		asyncWG.Add(1)
		go func(p string, t *TargetContainer, pi map[string]string, rl func()) {
			defer asyncWG.Done()

			// P2-2: releaseOnce mirrors the same idempotent-release pattern
			// used by the rolling/restart goroutines above, so a panic
			// recovered below and the normal-path release() call further
			// down can never double-release this lock.
			var released bool
			releaseOnce := func() {
				if !released {
					released = true
					rl()
				}
			}
			// P2-2: converts an unexpected panic (e.g. inside
			// core.RunComposeUpWithEnv) into an ordinary rotation failure
			// instead of crashing the agent -- lock released exactly once,
			// failure recorded the same way an ordinary error return does.
			defer r.recoverRotationPanic("compose rotation", p, func(err error, _ interface{}) {
				recordAsyncErr(err)
				releaseOnce()
			})()

			RecordDSOAction(filepath.Base(filepath.Dir(p)))

			if r.Server != nil {
				if as, ok := r.Server.(interface{ Emit(string) }); ok {
					as.Emit(fmt.Sprintf("\033[1;36m[DSO EXECUTION]\033[0m\nStrategy: restart (compose)\n🔄 Native rotation: Scaling %s from compose context.", p))
				}
			}

			// Pass pre-injected secrets directly — avoids calling back into the agent socket (deadlock fix)
			err := core.RunComposeUpWithEnv(p, []string{"-d", "--remove-orphans"}, "", false, pi)
			releaseOnce()
			recordAsyncErr(err)

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

	if onComplete != nil {
		go func() {
			// P2-2: onComplete is a caller-supplied callback; a panic inside
			// it must not take down the agent process either. No lock/proxy
			// state is held by this goroutine, so recovery here only needs
			// to log, not run any cleanup path.
			defer func() {
				if rec := recover(); rec != nil {
					r.Logger.Error("Recovered from panic in rotation onComplete callback; agent remains running",
						zap.Any("panic", rec))
				}
			}()
			asyncWG.Wait()
			asyncMu.Lock()
			err := asyncFirstErr
			asyncMu.Unlock()
			onComplete(err)
		}()
	}

	return nil
}

// executeRollback attempts to restore the original container after a failed rotation.
// It removes the newly-created replacement container and renames + restarts the original.
// On persistent failure it marks the service degraded.
// Callers must release the rotation lock after this returns.
func (r *ReloaderController) executeRollback(ctx context.Context, createdID, originalID, originalName, serviceName string) {
	if ctx.Err() != nil {
		r.Logger.Warn("Rotation context ended before rollback; rolling back on a bounded cleanup context",
			zap.String("service", serviceName), zap.Error(ctx.Err()))
	}
	for i := 0; i < 3; i++ {
		// Each attempt gets its own bounded context that survives cancellation
		// of the rotation context (timeout / daemon shutdown): the rollback
		// must be able to undo the half-finished rotation.
		cctx, cancel := rotation.CleanupContext(ctx)
		if err := r.cli.ContainerRemove(cctx, createdID, container.RemoveOptions{Force: true}); err != nil {
			r.Logger.Warn("Rollback: could not remove replacement container",
				zap.String("replacement_id", createdID), zap.Error(err))
		}
		if err := r.cli.ContainerRename(cctx, originalID, originalName); err != nil {
			r.Logger.Warn("Rollback: could not restore original container name",
				zap.String("original_id", originalID), zap.Error(err))
		}
		err := r.cli.ContainerStart(cctx, originalID, container.StartOptions{})
		cancel()
		if err == nil {
			r.Logger.Info("Rollback successful", zap.Int("attempt", i+1), zap.String("service", serviceName))
			return
		}
		r.Logger.Warn("Rollback attempt failed, retrying",
			zap.Int("attempt", i+1),
			zap.String("service", serviceName))
		time.Sleep(time.Duration(i+1) * rollbackRetryUnit)
	}
	r.markDegraded(serviceName, "Rotation failed, rollback failed after 3 attempts")
	r.Logger.Error("CRITICAL: New container failed and rollback failed after all retries. Manual recovery: "+
		"docker rm -f "+createdID+" ; docker rename "+originalID+" "+originalName+" ; docker start "+originalID,
		zap.String("service", serviceName),
		zap.String("original_id", originalID),
		zap.String("replacement_id", createdID))
}

// rollbackRetryUnit is the backoff unit between rollback attempts (1x, 2x, 3x).
// A variable so tests do not wait six real seconds.
var rollbackRetryUnit = time.Second

// restoreOriginalName renames a container back to its original name on a
// bounded cleanup context (it must work after the rotation context ended) and
// logs an error with the manual recovery command if that fails.
func (r *ReloaderController) restoreOriginalName(ctx context.Context, containerID, originalName string) {
	cctx, cancel := rotation.CleanupContext(ctx)
	defer cancel()
	if err := r.cli.ContainerRename(cctx, containerID, originalName); err != nil {
		r.Logger.Error("ROLLBACK INCOMPLETE: could not restore the original container name; it keeps its temporary name. "+
			"Recover with: docker rename "+containerID+" "+originalName,
			zap.String("container_id", containerID), zap.String("original_name", originalName), zap.Error(err))
	}
}

// stopReplacement stops a replacement container that failed its checks, on a
// bounded cleanup context. Best effort: executeRollback force-removes it next.
func (r *ReloaderController) stopReplacement(ctx context.Context, containerID string, timeoutSec int) {
	cctx, cancel := rotation.CleanupContext(ctx)
	defer cancel()
	if err := r.cli.ContainerStop(cctx, containerID, container.StopOptions{Timeout: &timeoutSec}); err != nil {
		r.Logger.Warn("Could not stop replacement container before rollback (removal is forced)",
			zap.String("replacement_id", containerID), zap.Error(err))
	}
}

// discardReplacement force-removes a replacement container that must not
// survive a failed restart, on a bounded cleanup context.
func (r *ReloaderController) discardReplacement(ctx context.Context, containerID string) {
	cctx, cancel := rotation.CleanupContext(ctx)
	defer cancel()
	if err := r.cli.ContainerRemove(cctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		r.Logger.Error("ROLLBACK INCOMPLETE: could not remove the replacement container; it may still exist. "+
			"The original container was not deleted. Recover with: docker rm -f "+containerID,
			zap.String("replacement_id", containerID), zap.Error(err))
	}
}

// restartOriginal starts the original container again after a failed restart
// attempt, on a bounded cleanup context.
func (r *ReloaderController) restartOriginal(ctx context.Context, containerID, originalName string) {
	cctx, cancel := rotation.CleanupContext(ctx)
	defer cancel()
	if err := r.cli.ContainerStart(cctx, containerID, container.StartOptions{}); err != nil {
		r.Logger.Error("ROLLBACK INCOMPLETE: could not restart the original container; it may be stopped. "+
			"Recover with: docker start "+containerID+" (expected name: "+originalName+")",
			zap.String("container_id", containerID), zap.Error(err))
	}
}

// DegradedService is a read-only snapshot of one currently degraded
// service/container. Fields are strictly safe metadata -- never secret
// material.
type DegradedService struct {
	Service string `json:"service"`
	Reason  string `json:"reason"`
}

// DegradedServices returns a snapshot of every currently degraded
// service. This reflects only the in-memory degraded map's CURRENT state
// -- it is rebuilt from scratch on every agent restart (populateInitialTargets
// re-derives Targets from live Docker state, and this map starts empty),
// so it deliberately does not claim to survive a restart.
func (r *ReloaderController) DegradedServices() []DegradedService {
	out := make([]DegradedService, 0)
	r.degraded.Range(func(k, v interface{}) bool {
		service, _ := k.(string)
		reason, _ := v.(string)
		out = append(out, DegradedService{Service: service, Reason: reason})
		return true
	})
	return out
}

// IsDegraded reports whether serviceName is currently marked degraded, and
// its reason if so. serviceName here follows the same key convention
// executeRollback/the restart-rotation goroutine already use: the
// container ID by default, or the container's
// "com.docker.compose.service" label when present (see the "EXTRACT
// SERVICE NAME FOR LOCKING" comment above) -- callers matching against
// ContainerSummary.ID will only match the container-ID-keyed case; a
// compose-service-named entry needs the resolved service name instead.
func (r *ReloaderController) IsDegraded(serviceName string) (string, bool) {
	v, ok := r.degraded.Load(serviceName)
	if !ok {
		return "", false
	}
	reason, _ := v.(string)
	return reason, true
}

// markDegraded marks serviceName as degraded and pushes a metadata-only
// "service_degraded" event onto observability.EventStream, but ONLY on a
// real healthy->degraded transition (Swap's `loaded` return tells us
// whether the key already existed). This prevents a service that fails
// repeatedly, or a second unrelated secret failing on the same service,
// from re-emitting the same degraded-entry event over and over.
func (r *ReloaderController) markDegraded(serviceName, reason string) {
	_, alreadyDegraded := r.degraded.Swap(serviceName, reason)
	if alreadyDegraded {
		return
	}
	pushOperationalEvent(map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"container":  serviceName,
		"event_type": "service_degraded",
		"status":     "failure",
		"error":      reason,
	})
}

// clearDegraded clears serviceName's degraded state and pushes a
// metadata-only "service_recovered" event onto observability.EventStream,
// but ONLY when the service was actually degraded (LoadAndDelete's
// `loaded` return confirms a real transition happened) -- a healthy
// service completing a normal rotation must never emit a spurious
// recovery event.
func (r *ReloaderController) clearDegraded(serviceName string) {
	_, wasDegraded := r.degraded.LoadAndDelete(serviceName)
	if !wasDegraded {
		return
	}
	pushOperationalEvent(map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"container":  serviceName,
		"event_type": "service_recovered",
		"status":     "success",
	})
}

// pushOperationalEvent is a thin, non-blocking wrapper around
// observability.EventStream -- the same fire-and-forget, never-blocks
// pattern already used by internal/injector/docker.go's LogInjectionEvent
// and internal/agent/trigger.go's emitEvent. A full EventStream (1000
// buffered) drops the event rather than ever blocking the caller, which
// here is inline in the rotation/rollback goroutine and must never stall
// on observability.
func pushOperationalEvent(ev map[string]interface{}) {
	select {
	case observability.EventStream <- ev:
	default:
	}
}

// cloneContainerConfig returns a deep copy of a *container.Config that is safe to
// mutate without affecting the struct returned by the Docker client (CQ-H5).
// The Docker SDK returns pointers into the client's response; mutating them in
// place is a data race if the client ever caches or reuses inspect responses.
// This mirrors the defensive copying in rotation.PrepareShadowConfig.
func cloneContainerConfig(src *container.Config) *container.Config {
	if src == nil {
		return nil
	}
	dst := *src // copy value fields (Hostname, Image, etc.)
	// Never inherit the original's MAC: a replacement coexists with it during
	// rotation, and a duplicate MAC poisons the bridge forwarding table.
	dst.MacAddress = "" //nolint:staticcheck // deprecated field, cleared deliberately
	if src.Env != nil {
		dst.Env = make([]string, len(src.Env))
		copy(dst.Env, src.Env)
	}
	if src.Labels != nil {
		dst.Labels = make(map[string]string, len(src.Labels))
		for k, v := range src.Labels {
			dst.Labels[k] = v
		}
	}
	if src.Volumes != nil {
		dst.Volumes = make(map[string]struct{}, len(src.Volumes))
		for k, v := range src.Volumes {
			dst.Volumes[k] = v
		}
	}
	return &dst
}

// executeSimpleRestart recreates containerID with envOverrides merged into its environment.
// Used by the rolling-strategy fallback path (L5). It does not perform health checks,
// exec probes, or proxy swaps — those belong to the primary restart strategy path.
//
// envOverrides must contain only env-mode secrets; fileSecrets carries any
// file-mode secrets (Inject.Type == "file") that the container depends on,
// streamed into its filesystem via StreamSecretToContainer instead of
// Config.Env -- see the SEC comment on the rolling-branch env-building loop
// this fallback is fed from.
//
// SEC-P0: managedEnvNames is every env var name this container's config ever
// maps a secret to (see the rolling-branch env-building loop), independent of
// each secret's current inject type. The replacement container's Config.Env
// starts as a verbatim copy of the original's (cloneContainerConfig), so
// without this, a secret value once written to env-mode Config.Env would
// persist unchanged across every future recreation even after the config
// switches that secret to file-mode or drops it entirely -- envOverrides
// alone only ever adds/updates keys, it never removes ones that shouldn't be
// there anymore. Any managedEnvNames entry not present in envOverrides for
// this rotation is stripped from the inherited env before recreation.
func (r *ReloaderController) executeSimpleRestart(ctx context.Context, containerID string, envOverrides map[string]string, fileSecrets []rotation.FileSecretSpec, managedEnvNames map[string]bool) error {
	inspect, err := r.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("executeSimpleRestart: inspect failed: %w", err)
	}

	originalName := strings.TrimPrefix(inspect.Name, "/")
	tempOldName := originalName + "_old_" + fmt.Sprintf("%d", time.Now().Unix())

	if err := r.cli.ContainerRename(ctx, containerID, tempOldName); err != nil {
		return fmt.Errorf("executeSimpleRestart: rename failed: %w", err)
	}

	// CQ-H5: deep-copy before mutating so we never modify the Docker-returned struct.
	cfg := cloneContainerConfig(inspect.Config)
	cfg.Hostname = ""

	// SEC-P0: drop any inherited env entry this rotation manages but did not
	// supply a fresh value for (e.g. switched to file-mode, or dropped from
	// config) -- see the doc comment above for why this can't be skipped.
	if len(managedEnvNames) > 0 {
		filtered := make([]string, 0, len(cfg.Env))
		for _, e := range cfg.Env {
			k, _, _ := strings.Cut(e, "=")
			if _, stillOverridden := envOverrides[k]; managedEnvNames[k] && !stillOverridden {
				continue
			}
			filtered = append(filtered, e)
		}
		cfg.Env = filtered
	}

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

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: rotation.SanitizedEndpointsConfig(inspect.NetworkSettings.Networks),
	}
	created, err := r.cli.ContainerCreate(ctx, cfg, inspect.HostConfig, networkingConfig, nil, originalName)
	if err != nil {
		// Rename back so the original container keeps its name.
		r.restoreOriginalName(ctx, containerID, originalName)
		return fmt.Errorf("executeSimpleRestart: container create failed: %w", err)
	}

	// Stream file-mode secrets before starting, same ordering as the primary
	// restart/rolling paths -- the app must never start without them present.
	for _, fs := range fileSecrets {
		if err := rotation.StreamSecretToContainer(ctx, r.cli, created.ID, fs.Path, fs.Data, fs.UID, fs.GID); err != nil {
			r.discardReplacement(ctx, created.ID)
			r.restoreOriginalName(ctx, containerID, originalName)
			return fmt.Errorf("executeSimpleRestart: file secret injection failed: %w", err)
		}
	}

	stopTO := 10
	_ = r.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTO})

	if err := r.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		r.discardReplacement(ctx, created.ID)
		r.restoreOriginalName(ctx, containerID, originalName)
		r.restartOriginal(ctx, containerID, originalName)
		return fmt.Errorf("executeSimpleRestart: start failed: %w", err)
	}

	_ = r.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
	return nil
}
