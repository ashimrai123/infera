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
// Adding a new LLM backend means writing one of these -- nothing else
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

// Registry holds all configured providers and resolves which ones should
// handle a given model name.
//
// v1: one provider per route (simple prefix-based static routing).
// v2: ordered list of providers per route -- the router tries them in
//
//	order and falls back to the next on failure.
type Registry struct {
	providers map[string]Provider
	// routes maps a model-name prefix to an ordered list of provider names.
	// The first entry is the preferred provider; subsequent entries are
	// fallbacks tried in order when the preferred provider fails.
	routes map[string][]string
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		routes:    make(map[string][]string),
	}
}

// Register adds a provider under its name (e.g. "openai").
func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
}

// AddRoute appends a provider to the ordered fallback list for a model-name
// prefix. Call it once for the primary provider, again for each fallback:
//
//	registry.AddRoute("claude-", "anthropic")  // primary
//	registry.AddRoute("claude-", "openai")     // fallback
func (r *Registry) AddRoute(modelPrefix, providerName string) {
	r.routes[modelPrefix] = append(r.routes[modelPrefix], providerName)
}

// Resolve returns the ordered list of providers for the given model name
// using longest-prefix match. The router tries them in order, falling back
// on error. Returns an error if no route matches.
func (r *Registry) Resolve(model string) ([]Provider, error) {
	var bestPrefix string
	var bestNames []string

	for prefix, names := range r.routes {
		if len(prefix) > len(bestPrefix) && hasPrefix(model, prefix) {
			bestPrefix = prefix
			bestNames = names
		}
	}

	if len(bestNames) == 0 {
		return nil, fmt.Errorf("no route configured for model %q", model)
	}

	out := make([]Provider, 0, len(bestNames))
	for _, name := range bestNames {
		p, ok := r.providers[name]
		if !ok {
			return nil, fmt.Errorf("model %q routes to unknown provider %q", model, name)
		}
		out = append(out, p)
	}
	return out, nil
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
