# infera

A reverse proxy for LLM APIs, written in Go. One endpoint, multiple providers.

## The problem

Teams building on LLMs usually end up integrating OpenAI, Anthropic, and self-hosted models separately -- each with its own API shape, its own streaming format, and no shared fallback if one provider goes down. Rate limiting, retries, and logging get rebuilt from scratch every time.

## What infera does

infera sits between your application and multiple LLM providers. Your app sends one request, infera routes it, retries on a different provider if the first fails, and returns one response. Same shape in, same shape out, regardless of which provider handled it.

---

## How it works

```
YOUR APP
  |
  | POST /v1/chat/completions
  | { "model": "claude-3-5-sonnet", "messages": [...] }
  |
  v
+------------------+
|      infera      |
|                  |
|  1. parse model  |
|  2. resolve      |  "claude-*" -> [anthropic, openai]
|     provider(s)  |
|  3. try primary  |-----> Anthropic API
|     if fail,     |         (down)
|  4. try fallback |-----> OpenAI API
|  5. return resp  |         (success)
+------------------+
  |
  | { "choices": [{ "message": { "content": "..." } }] }
  v
YOUR APP
```

**Your app never changes.** It always sends the same request shape and always gets the same response shape back. The model name is the only routing signal -- infera figures out the rest.

---

## Context and conversation history

LLM APIs are stateless. There is no server-side memory at OpenAI, Anthropic, or Ollama. Every request must include the full conversation history -- both what the user said and what the model said back.

```
Turn 1:
  app --> infera: { messages: [ {user: "hi"} ] }
  infera --> provider: same
  provider --> infera: {assistant: "hello"}
  infera --> app: {assistant: "hello"}

Turn 2 (app appends both sides):
  app --> infera: { messages: [
      {user: "hi"},
      {assistant: "hello"},   <-- model's previous reply, re-sent by app
      {user: "how are you?"}
  ]}
```

**infera does not store conversation history.** That is the calling app's responsibility. infera is stateless by design -- one request in, one response out.

This also means failover is clean: if Anthropic fails mid-conversation and we retry on OpenAI, we forward the exact same messages array (including prior replies) to OpenAI. The conversation continues without the caller noticing anything changed.

---

## Core features

- Unified request and response schema across providers (OpenAI-compatible)
- Automatic routing by model name prefix
- Ordered provider fallback -- retries the next provider on failure
- Streaming passthrough (SSE)
- Rate limiting and usage tracking (v2)
- Prometheus metrics, circuit breaking, Kubernetes support (v3)

## Roadmap

- **v1** - core gateway: unified API, OpenAI and Ollama adapters, streaming, routing
- **v2** - resilience: Anthropic adapter, automatic failover, rate limiting, usage tracking
- **v3** - production: metrics, dashboards, circuit breaking, Kubernetes and Helm deployment

---

Self-hosted. You bring the provider API keys. Work in progress -- v1 complete, v2 in progress.
