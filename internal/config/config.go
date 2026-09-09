// Package config loads infera's runtime configuration from environment
// variables. v1 keeps this deliberately dependency-free (no viper) --
// a handful of env vars doesn't need a config framework yet.
package config

import (
	"os"
)

type Config struct {
	Port            string
	OpenAIAPIKey    string
	AnthropicAPIKey string
	OllamaURL       string
}

// Load reads configuration from environment variables.
// Individual provider API keys are optional -- the gateway starts as long
// as at least one provider can be registered. Validation of which providers
// are actually active happens in main when providers are registered.
func Load() (*Config, error) {
	cfg := &Config{
		Port:            getEnv("PORT", "8080"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		OllamaURL:       getEnv("OLLAMA_URL", "http://localhost:11434"),
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
