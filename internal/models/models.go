// Package models defines the unified, OpenAI-compatible request/response
// schema that infera exposes to clients. Every provider adapter is
// responsible for translating between this schema and its own wire format.
package models

// ChatMessage is a single turn in a conversation.
type ChatMessage struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// ChatRequest is the unified inbound request shape, modeled on
// OpenAI's /v1/chat/completions so existing client tooling works
// against infera with zero changes.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

// Usage reports token accounting for a completed request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatChoice wraps a single generated message.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// ChatResponse is the unified, non-streaming response shape.
type ChatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
}

// ChatChunk is a single unified SSE chunk for streaming responses,
// modeled on OpenAI's streaming delta format.
type ChatChunk struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Delta string `json:"delta"`
	Done  bool   `json:"done"`
	Err   error  `json:"-"`
}
