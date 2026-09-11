// Command infera starts the LLM inference gateway.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/ashimrai123/infera/internal/config"
	"github.com/ashimrai123/infera/internal/metrics"
	"github.com/ashimrai123/infera/internal/middleware"
	"github.com/ashimrai123/infera/internal/provider"
	"github.com/ashimrai123/infera/internal/router"
	"github.com/ashimrai123/infera/internal/usage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Register Prometheus metrics before the server starts.
	// MustRegister panics on duplicate registration, catching mistakes early.
	metrics.Register()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// demoMode enables /v1/debug/* endpoints for the live demo UI.
	// Set DEMO_MODE=true to activate. Never expose in production without auth.
	demoMode := os.Getenv("DEMO_MODE") == "true"

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

	if demoMode {
		// demo-* routes: FailingProvider is primary, OpenAI is fallback.
		// This makes the failover path live and visible in the demo UI
		// without needing to manually break anything.
		registry.Register(provider.NewFailingProvider())
		registry.AddRoute("demo-", "demo-failing") // primary: always fails
		registry.AddRoute("demo-", "openai")       // fallback: handles the request
		logger.Info("demo provider registered", "route", "demo-*", "fallback", "openai")
	}

	tracker := usage.New()
	r := router.New(registry, tracker, logger, demoMode)

	// Layer middleware: CORS first (outermost), then rate limiter.
	// CORS_ORIGIN is empty by default (same-origin). Set it when the
	// frontend moves to a separate domain (e.g. Vercel + Koyeb split).
	corsOrigin := os.Getenv("CORS_ORIGIN")
	var handler http.Handler = r
	handler = middleware.NewRateLimiter(handler)
	handler = middleware.CORS(corsOrigin)(handler)

	logger.Info("infera starting", "port", cfg.Port, "demo_mode", demoMode)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
