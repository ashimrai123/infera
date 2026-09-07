# infera

A reverse proxy for LLM APIs, written in Go. One endpoint, multiple providers.

## The problem

Teams building on LLMs usually end up integrating OpenAI, Anthropic, and self-hosted models like Ollama separately, each with its own API shape, its own streaming format, and no shared fallback if one provider goes down. Usage and cost tracking end up scattered across each provider's own dashboard, and rate limiting, retries, and logging get rebuilt from scratch for every integration.

## What infera does

infera sits between your application and multiple LLM providers behind a single, unified API. Your app sends one request format, infera routes it to the right provider, retries on a different one if the first fails, enforces rate limits, and tracks usage, all in one place.

```
Your app → infera → OpenAI / Anthropic / Ollama
```

It's self-hosted infrastructure: you deploy it with your own provider API keys, and it acts as the single gateway your application talks to.

## Core features

- Unified request and response API across providers
- Automatic routing by model
- Failover between providers on error
- Token-level streaming
- Rate limiting and usage tracking
- Prometheus metrics and Kubernetes deployment support

## Roadmap

- **v1** - core gateway: unified API, OpenAI and Ollama support, streaming, routing
- **v2** - resilience: Anthropic support, automatic failover, rate limiting, usage tracking
- **v3** - production readiness: metrics, dashboards, circuit breaking, Kubernetes and Helm deployment

---

Work in progress. v1 is functional, v2 and v3 are in active development.
