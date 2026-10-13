package provider

import (
	"fmt"
	"sync"
)

var (
	registry = &Registry{
		providers: make(map[string]Factory),
	}
)

// Factory is a function that creates a new provider instance
type Factory func() Provider

// Registry manages provider registration and creation
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Factory
}

// Register adds a provider factory to the registry
func Register(name string, factory Factory) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.providers[name] = factory
}

// Get retrieves a provider factory by name
func (r *Registry) Get(name string) (Factory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not registered", name)
	}
	return factory, nil
}

// List returns all registered provider names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Exists checks if a provider is registered
func (r *Registry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}

// Create instantiates a provider by name
func Create(name string) (Provider, error) {
	factory, err := registry.Get(name)
	if err != nil {
		return nil, err
	}
	return factory(), nil
}

// GetRegistry returns the global registry instance
func GetRegistry() *Registry {
	return registry
}

// ListRegistered returns all registered provider names
func ListRegistered() []string {
	return registry.List()
}

// IsRegistered checks if a provider is registered
func IsRegistered(name string) bool {
	return registry.Exists(name)
}
