package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ashimrai123/infera/internal/models"
)

// OpenAIProvider talks to the OpenAI Chat Completions API.
type OpenAIProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com/v1",
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

// wire-format structs, kept private since they're OpenAI-specific and
// never leak outside this file.
type openaiChatRequest struct {
	Model       string               `json:"model"`
	Messages    []models.ChatMessage `json:"messages"`
	Stream      bool                 `json:"stream,omitempty"`
	Temperature *float64             `json:"temperature,omitempty"`
	MaxTokens   *int                 `json:"max_tokens,omitempty"`
}

type openaiChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openaiStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func (p *OpenAIProvider) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	return req, nil
}

func (p *OpenAIProvider) Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error) {
	wireReq := openaiChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Stream:      false,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := p.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai returned status %d", resp.StatusCode)
	}

	var wireResp openaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&wireResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	out := &models.ChatResponse{
		ID:    wireResp.ID,
		Model: wireResp.Model,
		Usage: models.Usage{
			PromptTokens:     wireResp.Usage.PromptTokens,
			CompletionTokens: wireResp.Usage.CompletionTokens,
			TotalTokens:      wireResp.Usage.TotalTokens,
		},
	}
	for _, c := range wireResp.Choices {
		out.Choices = append(out.Choices, models.ChatChoice{
			Index:        c.Index,
			Message:      models.ChatMessage{Role: c.Message.Role, Content: c.Message.Content},
			FinishReason: c.FinishReason,
		})
	}
	return out, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error) {
	wireReq := openaiChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := p.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("openai returned status %d", resp.StatusCode)
	}

	out := make(chan models.ChatChunk)

	go func() {
		defer close(out)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// SSE lines can be long; bump the buffer.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				out <- models.ChatChunk{Done: true}
				return
			}

			var chunk openaiStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				// Skip malformed lines rather than aborting the whole stream.
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			select {
			case out <- models.ChatChunk{
				ID:    chunk.ID,
				Model: chunk.Model,
				Delta: chunk.Choices[0].Delta.Content,
			}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			out <- models.ChatChunk{Err: err, Done: true}
		}
	}()

	return out, nil
}

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("openai health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai health check returned status %d", resp.StatusCode)
	}
	return nil
}
