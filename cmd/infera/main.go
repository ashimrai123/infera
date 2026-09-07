// Command infera starts the LLM inference gateway.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/ashimrai123/infera/internal/config"
	"github.com/ashimrai123/infera/internal/provider"
	"github.com/ashimrai123/infera/internal/router"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	registry := provider.NewRegistry()
	registry.Register(provider.NewOpenAIProvider(cfg.OpenAIAPIKey))
	registry.Register(provider.NewOllamaProvider(cfg.OllamaURL))

	// v1 static routing: model name prefix -> provider.
	// v2 will add health-aware failover on top of this.
	registry.AddRoute("gpt-", "openai")
	registry.AddRoute("o1", "openai")
	registry.AddRoute("llama", "ollama")
	registry.AddRoute("mistral", "ollama")
	registry.AddRoute("qwen", "ollama")

	r := router.New(registry, logger)

	logger.Info("infera starting", "port", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
