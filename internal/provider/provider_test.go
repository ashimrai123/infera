package provider

import (
	"context"
	"testing"

	"github.com/ashimrai123/infera/internal/models"
)

// stubProvider is a minimal Provider used only to verify registry
// wiring, it never makes a real network call.
type stubProvider struct {
	name string
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error) {
	return &models.ChatResponse{Model: req.Model}, nil
}

func (s *stubProvider) Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error) {
	ch := make(chan models.ChatChunk, 1)
	ch <- models.ChatChunk{Done: true}
	close(ch)
	return ch, nil
}

func (s *stubProvider) HealthCheck(ctx context.Context) error { return nil }

func newTestRegistry() *Registry {
	r := NewRegistry()
	r.Register(&stubProvider{name: "openai"})
	r.Register(&stubProvider{name: "ollama"})
	r.AddRoute("gpt-", "openai")
	r.AddRoute("o1", "openai")
	r.AddRoute("llama", "ollama")
	return r
}

func TestRegistry_Resolve_MatchesConfiguredPrefix(t *testing.T) {
	r := newTestRegistry()

	got, err := r.Resolve("gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "openai" {
		t.Errorf("expected openai, got %s", got.Name())
	}
}

func TestRegistry_Resolve_DifferentPrefixRoutesToDifferentProvider(t *testing.T) {
	r := newTestRegistry()

	got, err := r.Resolve("llama3.1-70b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "ollama" {
		t.Errorf("expected ollama, got %s", got.Name())
	}
}

func TestRegistry_Resolve_NoMatchingRouteReturnsError(t *testing.T) {
	r := newTestRegistry()

	_, err := r.Resolve("mistral-large")
	if err == nil {
		t.Fatal("expected an error for an unrouted model, got nil")
	}
}

func TestRegistry_Resolve_PicksLongestMatchingPrefix(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubProvider{name: "generic"})
	r.Register(&stubProvider{name: "specific"})
	r.AddRoute("gpt", "generic")
	r.AddRoute("gpt-4o", "specific")

	got, err := r.Resolve("gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "specific" {
		t.Errorf("expected longest-prefix match 'specific', got %s", got.Name())
	}
}
