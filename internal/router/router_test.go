package router

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ashimrai123/infera/internal/models"
	"github.com/ashimrai123/infera/internal/provider"
)

// stubProvider lets us test the router's HTTP handling in isolation,
// without making any real calls to OpenAI/Anthropic/Ollama.
type stubProvider struct {
	name         string
	failComplete bool
	streamChunks []string
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error) {
	if s.failComplete {
		return nil, io.ErrUnexpectedEOF
	}
	return &models.ChatResponse{
		ID:    "test-id",
		Model: req.Model,
		Choices: []models.ChatChoice{{
			Index:        0,
			Message:      models.ChatMessage{Role: "assistant", Content: "hello from " + s.name},
			FinishReason: "stop",
		}},
		Usage: models.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}, nil
}

func (s *stubProvider) Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error) {
	ch := make(chan models.ChatChunk)
	go func() {
		defer close(ch)
		for _, c := range s.streamChunks {
			ch <- models.ChatChunk{Model: req.Model, Delta: c}
		}
		ch <- models.ChatChunk{Done: true}
	}()
	return ch, nil
}

func (s *stubProvider) HealthCheck(ctx context.Context) error { return nil }

func newTestRouter() *Router {
	reg := provider.NewRegistry()
	reg.Register(&stubProvider{name: "openai", streamChunks: []string{"hel", "lo"}})
	reg.AddRoute("gpt-", "openai")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(reg, logger)
}

func TestHealthz_ReturnsOK(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestChatCompletions_NonStreaming_RoutesAndReturnsResponse(t *testing.T) {
	r := newTestRouter()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp models.ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "hello from openai" {
		t.Errorf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
}

func TestChatCompletions_UnroutableModel_Returns400(t *testing.T) {
	r := newTestRouter()

	body := `{"model":"unknown-model-xyz","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for unroutable model, got %d", rec.Code)
	}
}

func TestChatCompletions_MissingModel_Returns400(t *testing.T) {
	r := newTestRouter()

	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for missing model field, got %d", rec.Code)
	}
}

func TestChatCompletions_Streaming_EmitsSSEChunksAndDone(t *testing.T) {
	r := newTestRouter()

	body := `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream content type, got %q", ct)
	}

	out := rec.Body.String()
	if !strings.Contains(out, "hel") || !strings.Contains(out, "lo") {
		t.Errorf("expected stream chunks 'hel' and 'lo' in output, got: %s", out)
	}
	if !strings.Contains(out, "[DONE]") {
		t.Errorf("expected stream to terminate with [DONE], got: %s", out)
	}
}

func TestChatCompletions_ProviderFailure_Returns502(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&stubProvider{name: "openai", failComplete: true})
	reg.AddRoute("gpt-", "openai")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(reg, logger)

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502 when provider fails, got %d", rec.Code)
	}
}

func TestChatCompletions_FailoverToSecondProvider_Returns200(t *testing.T) {
	reg := provider.NewRegistry()
	// Primary always fails.
	reg.Register(&stubProvider{name: "primary", failComplete: true})
	// Fallback succeeds.
	reg.Register(&stubProvider{name: "fallback", streamChunks: []string{}})
	// Both registered under the same prefix -- primary first.
	reg.AddRoute("test-", "primary")
	reg.AddRoute("test-", "fallback")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(reg, logger)

	body := `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after failover to fallback provider, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp models.ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Choices[0].Message.Content != "hello from fallback" {
		t.Errorf("expected response from fallback provider, got: %q", resp.Choices[0].Message.Content)
	}
}
