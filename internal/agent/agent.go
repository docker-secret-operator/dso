package agent

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/internal/resolver"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Agent represents the background process that listens to Docker events
// and injects secrets into containers seamlessly.
type Agent struct {
	cache    *Cache
	docker   *client.Client
	injected map[string]bool
	mu       sync.Mutex
}

// NewAgent creates a new Agent daemon.
func NewAgent(docker *client.Client) *Agent {
	return &Agent{
		cache:    NewCache(),
		docker:   docker,
		injected: make(map[string]bool),
	}
}

// GetCache provides access to the Agent's secure memory store.
func (a *Agent) GetCache() *Cache {
	return a.cache
}

// Start begins listening to the Docker socket for lifecycle events.
func (a *Agent) Start(ctx context.Context) error {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")
	filterArgs.Add("event", "create")
	filterArgs.Add("event", "start")
	filterArgs.Add("event", "die")
	filterArgs.Add("event", "destroy")

	msgCh, errCh := a.docker.Events(ctx, events.ListOptions{
		Filters: filterArgs,
	})

	log.Println("✅ [DSO Agent] Started listening for Docker lifecycle events...")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return fmt.Errorf("docker event stream error: %w", err)
		case msg := <-msgCh:
			a.handleEvent(ctx, msg)
		}
	}
}

// handleEvent processes relevant Docker events concurrently.
func (a *Agent) handleEvent(ctx context.Context, msg events.Message) {
	containerID := msg.ID
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
	case "create", "start":
		a.mu.Lock()
		alreadyInjected := a.injected[containerID]
		if !alreadyInjected {
			a.injected[containerID] = true
		}
		a.mu.Unlock()

		if alreadyInjected {
			return // Avoid duplicate injection from rapid create->start events
		}

		// Ensure Docker API calls do not hang the agent
		injectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := a.inject(injectCtx, containerID, serviceSecrets); err != nil {
			log.Printf("❌ [DSO Agent] Failed injection [%s] for container %s (%s/%s): %v\n", msg.Action, containerID[:12], project, service, err)
			// Reset tracking on failure so a retry (e.g., on start) can naturally occur
			a.mu.Lock()
			delete(a.injected, containerID)
			a.mu.Unlock()
		} else {
			log.Printf("🔒 [DSO Agent] Injected secrets [%s] for container %s (%s/%s)\n", msg.Action, containerID[:12], project, service)
		}
	case "die", "destroy":
		// Clear idempotency tracker when container stops so restarts trigger re-injection
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
			return fmt.Errorf("secret missing from pool")
		}
		
		// Tar headers mandate filename only, as we copy directly to /run/secrets/dso
		fileName := filepath.Base(filePath)
		filesToInject[fileName] = val
	}

	return injector.InjectFiles(ctx, a.docker, containerID, filesToInject, serviceSecrets.UID, serviceSecrets.GID)
}
