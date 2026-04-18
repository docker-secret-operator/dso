// Package proxy implements the core DSO TCP proxy components.
// This file defines the Backend type and the in-memory Registry that maps
// service names to their set of upstream backends.
//
// The Registry is the single source of truth for all routing decisions. It is
// designed to be updated at runtime (backends added/removed without restart)
// and is safe for concurrent access from multiple goroutines.
package proxy

import (
	"fmt"
	"sync"
	"time"
)

// Backend represents a single upstream instance that can receive proxied traffic.
// The ID must be unique across all services in the registry.
type Backend struct {
	// ID uniquely identifies this backend instance.
	// Convention: "<service>-<host>-<port>" or a UUID for dynamic containers.
	ID string `json:"id"`

	// Service is the DSO service name this backend belongs to.
	// Must match the service name used when setting up a PortBinding.
	Service string `json:"service"`

	// Host is the DNS name or IP address of the upstream container.
	// In Docker Compose / Docker networks this is typically the service name
	// (e.g. "api"), which Docker's embedded DNS resolves to the container IP.
	Host string `json:"host"`

	// Port is the port the backend container listens on. When non-zero it
	// overrides the PortBinding.TargetPort used during dialling, allowing
	// heterogeneous backends to serve on different ports (Phase 2 use-case).
	// Set to 0 to use the binding's TargetPort (the common case).
	Port int `json:"port"`

	// Weight is reserved for weighted round-robin load balancing (Phase 2).
	// Not used by the current router implementation.
	Weight int `json:"weight,omitempty"`

	// AddedAt is the wall-clock time when this backend was registered.
	// Set automatically by Registry.Add; ignored on input.
	AddedAt time.Time `json:"added_at,omitempty"`
}

// Addr returns the "host:port" dial string for this backend.
// If Port is 0, the returned string will contain ":0" — callers that need a
// real port should fall back to PortBinding.TargetPort in that case.
func (b *Backend) Addr() string {
	return fmt.Sprintf("%s:%d", b.Host, b.Port)
}

// Registry stores the known set of backends, keyed by service name, and
// provides thread-safe add / remove / query operations.
//
// Invariants maintained by the registry:
//   - IDs are globally unique (across all services).
//   - A backend can only belong to one service.
//   - Snapshot reads (Backends, All) never block writers for long.
type Registry struct {
	mu       sync.RWMutex
	services map[string][]*Backend // service → ordered list of backends
	byID     map[string]*Backend   // id     → backend (O(1) delete)
}

// NewRegistry creates and returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{
		services: make(map[string][]*Backend),
		byID:     make(map[string]*Backend),
	}
}

// Add registers a new backend in the registry.
//
// Validation rules:
//   - ID, Service, and Host must all be non-empty.
//   - Port must be ≥ 0 (0 is a valid "use binding port" sentinel).
//   - ID must not already be registered (deduplication).
func (r *Registry) Add(b *Backend) error {
	if b.ID == "" {
		return fmt.Errorf("registry: backend ID must not be empty")
	}
	if b.Service == "" {
		return fmt.Errorf("registry: backend %q: service must not be empty", b.ID)
	}
	if b.Host == "" {
		return fmt.Errorf("registry: backend %q: host must not be empty", b.ID)
	}
	if b.Port < 0 {
		return fmt.Errorf("registry: backend %q: port must be ≥ 0, got %d", b.ID, b.Port)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byID[b.ID]; exists {
		return fmt.Errorf("registry: backend %q is already registered", b.ID)
	}

	b.AddedAt = time.Now()
	r.byID[b.ID] = b
	r.services[b.Service] = append(r.services[b.Service], b)
	return nil
}

// Remove deregisters a backend by its ID.
// Returns an error if the ID is not found.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	b, exists := r.byID[id]
	if !exists {
		return fmt.Errorf("registry: backend %q not found", id)
	}

	delete(r.byID, id)

	list := r.services[b.Service]
	for i, be := range list {
		if be.ID == id {
			r.services[b.Service] = append(list[:i], list[i+1:]...)
			break
		}
	}
	// Clean up the service key if it has no more backends.
	if len(r.services[b.Service]) == 0 {
		delete(r.services, b.Service)
	}
	return nil
}

// Backends returns a point-in-time snapshot of the backends registered for
// the given service. The returned slice is safe to read without holding any
// lock; modifications to it do not affect the registry.
//
// Returns nil if no backends are registered for the service.
func (r *Registry) Backends(service string) []*Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := r.services[service]
	if len(list) == 0 {
		return nil
	}
	snapshot := make([]*Backend, len(list))
	copy(snapshot, list)
	return snapshot
}

// All returns a point-in-time snapshot of every backend across all services.
func (r *Registry) All() []*Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Backend, 0, len(r.byID))
	for _, b := range r.byID {
		result = append(result, b)
	}
	return result
}

// ServiceNames returns the names of all services that currently have at least
// one registered backend.
func (r *Registry) ServiceNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	return names
}
