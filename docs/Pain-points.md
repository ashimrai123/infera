# Infera - Documentation

## 1. The Problem

Any team building a product on top of LLMs runs into the same set of
problems, usually in this order:

**a) Every provider has a different API.**
OpenAI, Anthropic, and a self-hosted model server like Ollama each
expose different request/response shapes, different streaming formats,
and different client libraries. Code that talks to one provider doesn't
work against another without a rewrite. If a team wants to compare
providers, or use a different model for different tasks (e.g. a cheap
local model for simple tasks, GPT-4o for complex ones), they end up
hand-rolling their own translation layer , usually inside application
code that has nothing to do with LLMs.

**b) A single point of failure sits in the critical path.**
If a product depends entirely on one provider's API and that provider
has an outage, a rate-limit spike, or elevated latency, the product's
core functionality goes down with it. There is no fallback unless the
team builds one , and most don't, until the first outage forces them to.

**c) There's no consistent view of usage or cost.**
Token usage, request volume, latency, and error rates all live inside
each provider's own dashboard, in each provider's own format. Teams
using multiple providers have no single place to see "how much are we
spending, on what, and is it working."

**d) Cross-cutting concerns get duplicated per provider.**
Rate limiting, retries, logging, and request/response transformation
all need to be implemented again for every provider a team integrates,
because none of that logic is shared , it's copy-pasted per SDK.

None of these problems are unique to LLMs. They're the exact class of
problem that a **reverse proxy / API gateway** exists to solve for any
set of backend services , the LLM-specific part is that this pattern
hasn't been standardized yet the way it has for, say, HTTP microservices
behind nginx or Envoy.

## 2. What Infera Is

**Infera is a reverse proxy that sits between an application and one or
more LLM providers, exposing a single, unified API regardless of which
provider actually serves the request.**

An application talks to Infera exactly the way it would talk to OpenAI
directly , same request shape, same streaming behavior , and Infera
handles routing the request to the right backend (OpenAI, Anthropic, or
a locally-hosted Ollama model), retrying on a different provider if the
first one fails, enforcing rate limits, and recording usage , all
without the application needing to know or care which provider actually
generated the response.

```
Application code
      │
      │  POST /v1/chat/completions   (one API, always)
      ▼
   Infera  ──┬─→ OpenAI
             ├─→ Anthropic
             └─→ Ollama (local)
```

### Who deploys it, and who holds the API keys

Infera is **self-hosted infrastructure**, not a multi-tenant public
service. A single team or company deploys Infera into their own
environment (a container, a Kubernetes cluster) and configures it with
their own provider API keys, once, at deploy time. Every request that
flows through Infera afterward uses those same keys , end users of the
team's product never see, provide, or need to know about any provider
credentials. This is the same trust model as any internal API gateway:
one set of credentials, held by the operator, never exposed to callers.

_(A metered, pay-per-use public version of Infera , where Infera's
operator holds the keys and end users pay for access , is a plausible
future direction, but is a distinct product with its own auth and
billing requirements. It is out of scope for the core gateway described
in this document.)_

## 3. What Infera Is Not

To keep this precise:

- It is **not** a chat UI or end-user application , there is no
  frontend. The "user" of Infera is application code, via HTTP.
- It is **not** a way to extend a model's context window , token/context
  limits are a property of the underlying model and are unaffected by
  routing through Infera.
- It does **not** hand off an in-progress streamed response from one
  provider to another mid-generation. Failover happens at the request
  level , before generation starts , not mid-stream (see §4).

## 4. Core Capabilities

| Capability        | What it does                                                                                 |
| ----------------- | -------------------------------------------------------------------------------------------- |
| **Unification**   | One request/response schema across all providers, modeled on OpenAI's `/v1/chat/completions` |
| **Routing**       | Decides which provider/model serves a given request (v1: by model-name prefix)               |
| **Resilience**    | Retries a failed or unhealthy request on a different provider before it reaches the caller   |
| **Observability** | Centralized metrics, logs, and usage tracking across every provider, in one place            |

---

_Draft , next sections to add: request/response schema reference,
failover behavior in detail, deployment guide._
