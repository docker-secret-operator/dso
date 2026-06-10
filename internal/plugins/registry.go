package plugins

import (
	"fmt"
	"sync"
)

// Registry manages all registered plugins
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

// NewRegistry creates a new plugin registry
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
	}
}

// Register adds a plugin to the registry
func (r *Registry) Register(plugin Plugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin cannot be nil")
	}

	id := plugin.ID()
	if id == "" {
		return fmt.Errorf("plugin ID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[id]; exists {
		return fmt.Errorf("plugin with ID %s already registered", id)
	}

	r.plugins[id] = plugin
	return nil
}

// Get retrieves a plugin by ID
func (r *Registry) Get(id string) Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[id]
}

// List returns all registered plugins
func (r *Registry) List() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugins := make([]Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

// ListByType returns plugins of a specific type
func (r *Registry) ListByType(pluginType PluginType) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugins := make([]Plugin, 0)
	for _, p := range r.plugins {
		if p.Type() == pluginType {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

// Count returns the number of registered plugins
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}

// Unregister removes a plugin from the registry
func (r *Registry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[id]; !exists {
		return fmt.Errorf("plugin with ID %s not found", id)
	}

	delete(r.plugins, id)
	return nil
}
