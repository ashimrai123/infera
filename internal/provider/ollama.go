package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ashimrai123/infera/internal/models"
)

// OllamaProvider talks to a local (or remote) Ollama instance.
// Ollama's native chat API differs slightly from OpenAI's, which is
// exactly why the Provider interface exists — this file is the
// translation layer.
type OllamaProvider struct {
	baseURL string
	client  *http.Client
}

func NewOllamaProvider(baseURL string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OllamaProvider) Name() string { return "ollama" }

type ollamaChatRequest struct {
	Model    string               `json:"model"`
	Messages []models.ChatMessage `json:"messages"`
	Stream   bool                 `json:"stream"`
}

type ollamaChatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done            bool `json:"done"`
	PromptEvalCount int  `json:"prompt_eval_count"`
	EvalCount       int  `json:"eval_count"`
}

func (p *OllamaProvider) Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error) {
	wireReq := ollamaChatRequest{Model: req.Model, Messages: req.Messages, Stream: false}
	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var wireResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&wireResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &models.ChatResponse{
		Model: wireResp.Model,
		Choices: []models.ChatChoice{{
			Index:        0,
			Message:      models.ChatMessage{Role: wireResp.Message.Role, Content: wireResp.Message.Content},
			FinishReason: "stop",
		}},
		Usage: models.Usage{
			PromptTokens:     wireResp.PromptEvalCount,
			CompletionTokens: wireResp.EvalCount,
			TotalTokens:      wireResp.PromptEvalCount + wireResp.EvalCount,
		},
	}, nil
}

func (p *OllamaProvider) Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error) {
	wireReq := ollamaChatRequest{Model: req.Model, Messages: req.Messages, Stream: true}
	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	out := make(chan models.ChatChunk)

	go func() {
		defer close(out)
		defer resp.Body.Close()

		// Ollama streams newline-delimited JSON objects (not SSE).
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var chunk ollamaChatResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				continue
			}
			select {
			case out <- models.ChatChunk{
				Model: chunk.Model,
				Delta: chunk.Message.Content,
				Done:  chunk.Done,
			}:
			case <-ctx.Done():
				return
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			out <- models.ChatChunk{Err: err, Done: true}
		}
	}()

	return out, nil
}

func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check returned status %d", resp.StatusCode)
	}
	return nil
}
