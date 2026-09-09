package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ashimrai123/infera/internal/models"
)

// ---- helpers -------------------------------------------------------------

// newTestAnthropic builds an AnthropicProvider pointed at the given test server URL.
func newTestAnthropic(baseURL, apiKey string) *AnthropicProvider {
	p := NewAnthropicProvider(apiKey)
	p.baseURL = baseURL
	return p
}

// ---- splitMessages tests -------------------------------------------------

func TestSplitMessages_NoSystem(t *testing.T) {
	msgs := []models.ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	system, out := splitMessages(msgs)
	if system != "" {
		t.Errorf("expected empty system, got %q", system)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
}

func TestSplitMessages_WithSystem(t *testing.T) {
	msgs := []models.ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "hello"},
	}
	system, out := splitMessages(msgs)
	if system != "You are helpful." {
		t.Errorf("unexpected system %q", system)
	}
	if len(out) != 1 || out[0].Role != "user" {
		t.Errorf("unexpected messages: %+v", out)
	}
}

func TestSplitMessages_MultipleSystemsConcatenated(t *testing.T) {
	msgs := []models.ChatMessage{
		{Role: "system", Content: "Part 1."},
		{Role: "system", Content: "Part 2."},
		{Role: "user", Content: "hi"},
	}
	system, out := splitMessages(msgs)
	if !strings.Contains(system, "Part 1.") || !strings.Contains(system, "Part 2.") {
		t.Errorf("system parts not concatenated: %q", system)
	}
	if len(out) != 1 {
		t.Errorf("expected 1 message, got %d", len(out))
	}
}

// ---- Complete tests -------------------------------------------------------

func TestAnthropicComplete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth headers are set.
		if r.Header.Get("x-api-key") == "" {
			t.Error("x-api-key header missing")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header missing")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "msg_test123",
			"model": "claude-3-5-sonnet-20241022",
			"content": []map[string]any{
				{"type": "text", "text": "Hello from Anthropic!"},
			},
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		})
	}))
	defer srv.Close()

	p := newTestAnthropic(srv.URL, "test-key")
	resp, err := p.Complete(context.Background(), &models.ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.ID != "msg_test123" {
		t.Errorf("unexpected ID: %q", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello from Anthropic!" {
		t.Errorf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 5 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
}

func TestAnthropicComplete_SystemPromptExtracted(t *testing.T) {
	var capturedBody anthropicRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "msg_sys",
			"model": "claude-3-5-sonnet-20241022",
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	p := newTestAnthropic(srv.URL, "test-key")
	p.Complete(context.Background(), &models.ChatRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []models.ChatMessage{
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Hi"},
		},
	})

	if capturedBody.System != "Be concise." {
		t.Errorf("system not extracted: %q", capturedBody.System)
	}
	// The messages array sent to Anthropic must not contain the system message.
	for _, m := range capturedBody.Messages {
		if m.Role == "system" {
			t.Error("system role must not appear in messages[]")
		}
	}
}

func TestAnthropicComplete_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newTestAnthropic(srv.URL, "test-key")
	_, err := p.Complete(context.Background(), &models.ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 503, got nil")
	}
}

// ---- Stream tests --------------------------------------------------------

func TestAnthropicStream_Success(t *testing.T) {
	// Anthropic streaming SSE response fixture.
	sseBody := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_stream1","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":8,"output_tokens":0}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	p := newTestAnthropic(srv.URL, "test-key")
	ch, err := p.Stream(context.Background(), &models.ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var deltas []string
	var done bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Done {
			done = true
		} else {
			deltas = append(deltas, chunk.Delta)
		}
	}

	if !done {
		t.Error("stream did not receive done chunk")
	}
	combined := strings.Join(deltas, "")
	if combined != "Hello world" {
		t.Errorf("unexpected combined delta: %q", combined)
	}
}

func TestAnthropicStream_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "auth error", http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestAnthropic(srv.URL, "bad-key")
	_, err := p.Stream(context.Background(), &models.ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}

// ---- HealthCheck tests ---------------------------------------------------

func TestAnthropicHealthCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestAnthropic(srv.URL, "test-key")
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() unexpected error: %v", err)
	}
}

func TestAnthropicHealthCheck_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestAnthropic(srv.URL, "test-key")
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Error("expected error on 500, got nil")
	}
}
