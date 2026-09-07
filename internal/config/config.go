// Package config loads infera's runtime configuration from environment
// variables. v1 keeps this deliberately dependency-free (no viper) —
// a handful of env vars doesn't need a config framework yet.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port         string
	OpenAIAPIKey string
	OllamaURL    string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:         getEnv("PORT", "8080"),
		OpenAIAPIKey: os.Getenv("OPENAI_API_KEY"),
		OllamaURL:    getEnv("OLLAMA_URL", "http://localhost:11434"),
	}

	if cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
