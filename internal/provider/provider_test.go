package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/ashimrai123/infera/internal/models"
)

// stubProvider is a minimal Provider used to verify registry wiring and
// failover behaviour without making any real network calls.
// Set failWith to a non-nil error to make Complete and Stream return that error.
type stubProvider struct {
	name     string
	failWith error
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
	return &models.ChatResponse{Model: req.Model}, nil
}

func (s *stubProvider) Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
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

// ---- Resolve: single-provider routes ------------------------------------

func TestRegistry_Resolve_MatchesConfiguredPrefix(t *testing.T) {
	r := newTestRegistry()

	got, err := r.Resolve("gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "openai" {
		t.Errorf("expected [openai], got %v", got)
	}
}

func TestRegistry_Resolve_DifferentPrefixRoutesToDifferentProvider(t *testing.T) {
	r := newTestRegistry()

	got, err := r.Resolve("llama3.1-70b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "ollama" {
		t.Errorf("expected [ollama], got %v", got)
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
	if len(got) == 0 || got[0].Name() != "specific" {
		t.Errorf("expected longest-prefix match 'specific', got %v", got)
	}
}

// ---- Resolve: multi-provider fallback routes ----------------------------

func TestRegistry_Resolve_ReturnsOrderedFallbackList(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubProvider{name: "anthropic"})
	r.Register(&stubProvider{name: "openai"})
	r.AddRoute("claude-", "anthropic") // primary
	r.AddRoute("claude-", "openai")    // fallback

	got, err := r.Resolve("claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(got))
	}
	if got[0].Name() != "anthropic" || got[1].Name() != "openai" {
		t.Errorf("unexpected order: %v, %v", got[0].Name(), got[1].Name())
	}
}

func TestRegistry_Resolve_FallbackSkipsFailedProvider(t *testing.T) {
	// This test exercises the registry data only -- the retry loop itself
	// lives in the router. Here we just verify Resolve hands back both
	// providers so the router can iterate.
	r := NewRegistry()
	primary := &stubProvider{name: "primary", failWith: errors.New("down")}
	fallback := &stubProvider{name: "fallback"}
	r.Register(primary)
	r.Register(fallback)
	r.AddRoute("test-", "primary")
	r.AddRoute("test-", "fallback")

	providers, err := r.Resolve("test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate what the router does: try each provider in order.
	var used string
	for _, p := range providers {
		_, err := p.Complete(context.Background(), &models.ChatRequest{Model: "test-model"})
		if err == nil {
			used = p.Name()
			break
		}
	}
	if used != "fallback" {
		t.Errorf("expected fallback provider to be used, got %q", used)
	}
}
