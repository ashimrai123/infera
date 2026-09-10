// Command infera starts the LLM inference gateway.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/ashimrai123/infera/internal/config"
	"github.com/ashimrai123/infera/internal/middleware"
	"github.com/ashimrai123/infera/internal/provider"
	"github.com/ashimrai123/infera/internal/router"
	"github.com/ashimrai123/infera/internal/usage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	registry := provider.NewRegistry()

	// Register providers only when their credentials are present.
	// The gateway starts with whatever subset is configured; a missing key
	// logs a warning so the operator knows that provider is unavailable.
	if cfg.OpenAIAPIKey != "" {
		registry.Register(provider.NewOpenAIProvider(cfg.OpenAIAPIKey))
		registry.AddRoute("gpt-", "openai")
		registry.AddRoute("o1", "openai")
		logger.Info("provider registered", "provider", "openai")
	} else {
		logger.Warn("OPENAI_API_KEY not set -- openai provider disabled")
	}

	if cfg.AnthropicAPIKey != "" {
		registry.Register(provider.NewAnthropicProvider(cfg.AnthropicAPIKey))
		registry.AddRoute("claude-", "anthropic")
		logger.Info("provider registered", "provider", "anthropic")
	} else {
		logger.Warn("ANTHROPIC_API_KEY not set -- anthropic provider disabled")
	}

	// Ollama runs locally and needs no API key.
	registry.Register(provider.NewOllamaProvider(cfg.OllamaURL))
	registry.AddRoute("llama", "ollama")
	registry.AddRoute("mistral", "ollama")
	registry.AddRoute("qwen", "ollama")
	logger.Info("provider registered", "provider", "ollama")

	tracker := usage.New()
	r := router.New(registry, tracker, logger)

	// Wrap the router with per-IP rate limiting (10 req/s, burst 20).
	handler := middleware.NewRateLimiter(r)

	logger.Info("infera starting", "port", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}


