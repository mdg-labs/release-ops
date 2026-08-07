package source

import (
	"errors"
	"fmt"
)

// ErrUnknownKind is returned when a source_kind is not supported or not registered.
var ErrUnknownKind = errors.New("unknown source kind")

// Registry maps source_kind values to SourceProvider implementations.
type Registry struct {
	providers map[string]SourceProvider
}

// NewRegistry returns an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]SourceProvider),
	}
}

// Register adds or replaces the provider for kind.
func (r *Registry) Register(kind string, provider SourceProvider) error {
	if !IsValidKind(kind) {
		return fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	if provider == nil {
		return errors.New("source provider must not be nil")
	}
	r.providers[kind] = provider
	return nil
}

// Get returns the provider registered for kind.
func (r *Registry) Get(kind string) (SourceProvider, error) {
	if !IsValidKind(kind) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	provider, ok := r.providers[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	return provider, nil
}
