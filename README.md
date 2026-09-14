# infera

A stateless LLM reverse proxy written in Go. One endpoint, multiple providers — with streaming, automatic failover, circuit breaking, and a live chat UI.

**[Try the live demo →](https://ashimrai1.com.np)**

---

## What it does

infera sits between your app and multiple LLM providers. Send one request, get one response. The provider that handled it is invisible to the caller.

```
your app
  │
  │  POST /v1/chat/completions
  │  { "model": "gemini-3.6-flash", "messages": [...] }
  │
  ▼
┌──────────────────────────────────────┐
│               infera                 │
│                                      │
│  1. resolve provider(s) by prefix    │
│  2. check circuit breaker            │
│  3. try primary  ──────────────────► │ Gemini (success)
│     if fail, try fallback  ────────► │ OpenAI (fallback)
│  4. stream response back             │
│  5. record metrics                   │
└──────────────────────────────────────┘
  │
  │  token by token via SSE
  ▼
your app
```

**Your app never changes.** It always sends the same request shape and always gets the same response back. The model name prefix is the only routing signal — infera figures out the rest.

---

## Providers

| Provider | Prefix | Free tier |
|---|---|---|
| Google Gemini | `gemini-` | ✅ 15 req/min, no card |
| OpenAI | `gpt-`, `o1` | ❌ paid |
| Anthropic | `claude-` | ❌ paid |
| Ollama (local) | `llama`, `mistral`, `qwen` | ✅ self-hosted |

---

## Features

- **Unified API** — OpenAI-compatible request/response schema across all providers
- **Streaming** — SSE passthrough, token by token, for all providers
- **Automatic failover** — ordered provider list per route, tries next on failure
- **Circuit breaker** — trips after 5 consecutive failures, auto-recovers after 30s
- **Per-IP rate limiting** — 10 req/s, burst 20, token bucket via `golang.org/x/time`
- **Prometheus metrics** — request count, error count, latency histogram at `GET /metrics`
- **Usage tracking** — per-provider request/token/error counters at `GET /v1/usage`
- **Embedded chat UI** — served at `GET /`, no separate frontend process needed
- **Demo mode** — live failover demo and circuit breaker controls via `DEMO_MODE=true`

---

## Architecture

```
cmd/infera/main.go
  │
  ├── metrics.Register()        → 3 Prometheus metrics on default registry
  ├── provider.Registry         → prefix → ordered provider list
  ├── usage.Tracker             → atomic per-provider counters
  ├── router.New()              → HTTP mux + circuit breakers
  │     ├── POST /v1/chat/completions
  │     ├── GET  /healthz
  │     ├── GET  /v1/usage
  │     ├── GET  /metrics
  │     └── GET  /              → embedded chat UI
  ├── middleware.NewRateLimiter  → per-IP token bucket
  └── middleware.CORS            → optional, via CORS_ORIGIN env var
```

Request flow per provider attempt:

1. `getBreakerFor(provider)` — lazy-init circuit breaker
2. `b.Allow()` — if open, skip (fast-fail, no network call)
3. Call provider `Stream()` or `Complete()`
4. On success → `b.RecordSuccess()`, record metrics and usage
5. On failure → `b.RecordFailure()`, try next provider in list

---

## Quick start

```bash
git clone https://github.com/ashimrai123/infera
cd infera

# Gemini is free -- get a key at aistudio.google.com
export GEMINI_API_KEY=AIza...
export DEMO_MODE=true

go run ./cmd/infera
# open http://localhost:8080
```

---

## Configuration

All configuration is via environment variables. No config files.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `GEMINI_API_KEY` | — | Google Gemini (free tier available) |
| `OPENAI_API_KEY` | — | OpenAI |
| `ANTHROPIC_API_KEY` | — | Anthropic |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama base URL |
| `DEMO_MODE` | `false` | Enables debug endpoints and demo model routes |
| `CORS_ORIGIN` | — | Set to `*` or a domain when frontend is separate |

At least one provider key is required. Missing providers log a warning and are skipped.

---

## API

### `POST /v1/chat/completions`

OpenAI-compatible. Accepts `stream: true` for SSE streaming.

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.6-flash",
    "messages": [{"role": "user", "content": "hello"}],
    "stream": true
  }'
```

### `GET /healthz`
Returns `ok`. Used by Kubernetes liveness and readiness probes.

### `GET /metrics`
Prometheus text format. Metrics exposed:
- `infera_requests_total{provider, model}` — counter
- `infera_request_errors_total{provider}` — counter
- `infera_request_duration_seconds{provider}` — histogram (50ms–30s buckets)

### `GET /v1/usage`
JSON snapshot of per-provider request, token, and error counts.

### `GET /` 
Embedded chat UI. Dark minimal aesthetic, streaming responses, model selector, provider badge per response.

---

## Demo mode

Set `DEMO_MODE=true` to enable:

- **`demo-*` model routes** — primary provider always fails intentionally, request falls through to the real provider. Demonstrates live failover on every request.
- **`GET /v1/debug/breakers`** — current state of all circuit breakers (closed/open/half-open)
- **`POST /v1/debug/break/{provider}`** — trip a circuit breaker manually
- **`POST /v1/debug/reset/{provider}`** — reset a tripped breaker

The chat UI shows a demo panel with live provider state badges and break/reset buttons when demo mode is active.

---

## Deployment

### Docker

```bash
docker build -f deploy/docker/Dockerfile -t infera:dev .
docker run -p 8080:8080 \
  -e GEMINI_API_KEY=AIza... \
  -e DEMO_MODE=true \
  infera:dev
```

### Docker Compose (with Prometheus)

```bash
cp .env.example .env   # fill in your keys
docker compose -f deploy/docker-compose.yml up
# infera:    http://localhost:8080
# Prometheus: http://localhost:9090
```

### Kubernetes

```bash
kubectl apply -f deploy/k8s/namespace.yaml
kubectl create secret generic infera-keys \
  --namespace infera \
  --from-literal=gemini-api-key="AIza..."
kubectl apply -f deploy/k8s/
```

The Deployment has liveness and readiness probes on `/healthz`, resource limits (100m–500m CPU, 64–128MB RAM), and Prometheus scrape annotations for auto-discovery.

---

## Development

```bash
# Run tests
go test ./...          # 43 tests, all packages

# Run locally with demo mode
export GEMINI_API_KEY=AIza...
export DEMO_MODE=true
go run ./cmd/infera

# Add a new provider
# 1. Implement the Provider interface in internal/provider/
# 2. Register it in cmd/infera/main.go
# 3. AddRoute() for the model prefix
```

### Provider interface

```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error)
    Stream(ctx context.Context, req *models.ChatRequest) (<-chan models.ChatChunk, error)
    HealthCheck(ctx context.Context) error
}
```

Four methods. That's all it takes to add a new LLM backend.

---

## Project structure

```
cmd/infera/          entry point, wires all components
internal/
  breaker/           circuit breaker state machine (closed → open → half-open)
  config/            env var loading
  metrics/           Prometheus metric declarations
  middleware/        per-IP rate limiter, CORS
  models/            shared request/response types
  provider/          OpenAI, Gemini, Anthropic, Ollama adapters + registry
  router/            HTTP mux, failover logic, debug endpoints
  ui/                embedded chat UI (web/index.html baked into binary)
  usage/             atomic per-provider counters
deploy/
  docker/            Dockerfile (multi-stage, 21MB final image)
  docker-compose.yml infera + Prometheus
  prometheus.yml     scrape config
  k8s/               Deployment, Service, Secret, Namespace manifests
```
