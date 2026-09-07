// Package provider defines the abstraction every LLM backend (OpenAI,
// Anthropic, Ollama, ...) must implement, plus a registry used by the
// router to pick a provider for an incoming request.
package provider

import (
	"context"
	"fmt"

	"github.com/ashimrai123/infera/internal/models"
)

// Provider is the common interface every backend adapter implements.
// Adding a new LLM backend means writing one of these — nothing else
// in the gateway needs to change.
type Provider interface {
	// Name is a short identifier, e.g. "openai", "ollama".
	Name() string

	// Complete performs a non-streaming chat completion.
	Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error)

	// Stream performs a streaming chat completion, sending chunks on the
	// returned channel. The channel is closed when the stream ends
	// (successfully or with an error set on the final chunk).
	Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error)

	// HealthCheck reports whether the backend is currently reachable.
	// Used by the router for failover decisions.
	HealthCheck(ctx context.Context) error
}

// Registry holds all configured providers and resolves which one should
// handle a given model name. v1 uses simple prefix-based static routing;
// v2 adds health-aware failover on top of this.
type Registry struct {
	providers map[string]Provider
	// routes maps a model-name prefix (e.g. "gpt-") to a provider name.
	routes map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		routes:    make(map[string]string),
	}
}

// Register adds a provider under a name (e.g. "openai").
func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
}

// AddRoute maps a model-name prefix to a provider name.
// Example: AddRoute("gpt-", "openai"); AddRoute("llama", "ollama").
func (r *Registry) AddRoute(modelPrefix, providerName string) {
	r.routes[modelPrefix] = providerName
}

// Resolve picks a provider for the given model name using longest
// prefix match. Returns an error if no route matches.
func (r *Registry) Resolve(model string) (Provider, error) {
	var bestPrefix string
	var bestProvider string

	for prefix, providerName := range r.routes {
		if len(prefix) > len(bestPrefix) && hasPrefix(model, prefix) {
			bestPrefix = prefix
			bestProvider = providerName
		}
	}

	if bestProvider == "" {
		return nil, fmt.Errorf("no route configured for model %q", model)
	}

	p, ok := r.providers[bestProvider]
	if !ok {
		return nil, fmt.Errorf("model %q routes to unknown provider %q", model, bestProvider)
	}
	return p, nil
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
