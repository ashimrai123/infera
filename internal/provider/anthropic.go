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

// AnthropicProvider talks to the Anthropic Messages API.
// Anthropic's wire format differs from OpenAI's in three meaningful ways
// that make it a good test of whether the Provider interface holds up:
//
//  1. System messages are NOT part of the messages array -- they live at
//     the top level of the request as a plain string field.
//  2. The response shape is entirely different (content blocks, not choices).
//  3. Streaming uses named SSE event types rather than a single data stream,
//     so the parser must track event name + data lines in pairs.
type AnthropicProvider struct {
	apiKey  string
	baseURL string
	version string // Anthropic-Version header, e.g. "2023-06-01"
	client  *http.Client
}

func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1",
		version: "2023-06-01",
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

// ---- wire-format structs (private, never leave this file) ---------------

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`
}

// Non-streaming response types.

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicResponse struct {
	ID      string                  `json:"id"`
	Model   string                  `json:"model"`
	Content []anthropicContentBlock `json:"content"`
	Usage   anthropicUsage          `json:"usage"`
}

// Streaming event payloads.
// Anthropic SSE looks like:
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
//
//	event: message_stop
//	data: {"type":"message_stop"}

type anthropicStreamDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type anthropicStreamMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		ID    string         `json:"id"`
		Model string         `json:"model"`
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

// ---- helpers -------------------------------------------------------------

// splitMessages separates the system prompt (if any) from the conversation
// turns and converts them into Anthropic's wire message format.
// Anthropic requires that messages[] contains only "user"/"assistant" roles;
// system content goes in the top-level system field.
func splitMessages(msgs []models.ChatMessage) (system string, out []anthropicMessage) {
	for _, m := range msgs {
		if m.Role == "system" {
			if system != "" {
				system += "\n" + m.Content
			} else {
				system = m.Content
			}
			continue
		}
		out = append(out, anthropicMessage{Role: m.Role, Content: m.Content})
	}
	return
}

func (p *AnthropicProvider) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", p.version)
	return req, nil
}

// defaultMaxTokens is used when the caller does not specify max_tokens.
// Anthropic requires this field explicitly (unlike OpenAI where it is optional).
const defaultMaxTokens = 1024

// ---- Provider interface implementation ----------------------------------

func (p *AnthropicProvider) Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error) {
	system, msgs := splitMessages(req.Messages)

	maxTokens := defaultMaxTokens
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	wireReq := anthropicRequest{
		Model:     req.Model,
		Messages:  msgs,
		System:    system,
		MaxTokens: maxTokens,
		Stream:    false,
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
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic returned status %d", resp.StatusCode)
	}

	var wireResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&wireResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Anthropic returns an array of content blocks; collect all text blocks
	// into a single assistant message to match the unified schema.
	var sb strings.Builder
	for _, block := range wireResp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}

	return &models.ChatResponse{
		ID:    wireResp.ID,
		Model: wireResp.Model,
		Choices: []models.ChatChoice{{
			Index:        0,
			Message:      models.ChatMessage{Role: "assistant", Content: sb.String()},
			FinishReason: "stop",
		}},
		Usage: models.Usage{
			PromptTokens:     wireResp.Usage.InputTokens,
			CompletionTokens: wireResp.Usage.OutputTokens,
			TotalTokens:      wireResp.Usage.InputTokens + wireResp.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error) {
	system, msgs := splitMessages(req.Messages)

	maxTokens := defaultMaxTokens
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	wireReq := anthropicRequest{
		Model:     req.Model,
		Messages:  msgs,
		System:    system,
		MaxTokens: maxTokens,
		Stream:    true,
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
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic returned status %d", resp.StatusCode)
	}

	out := make(chan models.ChatChunk)

	go func() {
		defer close(out)
		defer resp.Body.Close()

		// Anthropic SSE uses named event types. We read pairs:
		//   event: <type>
		//   data:  <json>
		// We capture ID and model from the opening message_start event.
		var streamID, streamModel string
		var currentEvent string

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			switch {
			case strings.HasPrefix(line, "event: "):
				currentEvent = strings.TrimPrefix(line, "event: ")

			case strings.HasPrefix(line, "data: "):
				payload := strings.TrimPrefix(line, "data: ")

				switch currentEvent {
				case "message_start":
					var ms anthropicStreamMessageStart
					if err := json.Unmarshal([]byte(payload), &ms); err == nil {
						streamID = ms.Message.ID
						streamModel = ms.Message.Model
					}

				case "content_block_delta":
					var delta anthropicStreamDelta
					if err := json.Unmarshal([]byte(payload), &delta); err != nil {
						continue
					}
					if delta.Delta.Type != "text_delta" || delta.Delta.Text == "" {
						continue
					}
					select {
					case out <- models.ChatChunk{
						ID:    streamID,
						Model: streamModel,
						Delta: delta.Delta.Text,
					}:
					case <-ctx.Done():
						return
					}

				case "message_stop":
					select {
					case out <- models.ChatChunk{
						ID:    streamID,
						Model: streamModel,
						Done:  true,
					}:
					case <-ctx.Done():
					}
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			out <- models.ChatChunk{Err: err, Done: true}
		}
	}()

	return out, nil
}

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	// GET /v1/models is the lightest authenticated endpoint Anthropic exposes.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", p.version)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anthropic health check returned status %d", resp.StatusCode)
	}
	return nil
}
