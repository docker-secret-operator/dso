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
	Ready    chan struct{} // Signaled when the agent is listening
}

// NewAgent creates a new Agent daemon.
func NewAgent(docker *client.Client) *Agent {
	return &Agent{
		cache:    NewCache(),
		docker:   docker,
		injected: make(map[string]bool),
		Ready:    make(chan struct{}),
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

	close(a.Ready)
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
		filesToInject[filepath.Base(filePath)] = val
	}

	return injector.InjectFiles(ctx, a.docker, containerID, filesToInject, serviceSecrets.UID, serviceSecrets.GID)
}
