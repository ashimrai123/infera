// Package router wires up the HTTP handlers that expose the unified
// /v1/chat/completions endpoint, resolving each request to an ordered
// list of Providers via the registry and attempting failover on error.
package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ashimrai123/infera/internal/breaker"
	"github.com/ashimrai123/infera/internal/metrics"
	"github.com/ashimrai123/infera/internal/models"
	"github.com/ashimrai123/infera/internal/provider"
	"github.com/ashimrai123/infera/internal/ui"
	"github.com/ashimrai123/infera/internal/usage"
)

const (
	breakerThreshold = 5                // consecutive failures before opening
	breakerCooldown  = 30 * time.Second // how long to stay open before probing
)

type Router struct {
	registry *provider.Registry
	tracker  *usage.Tracker
	logger   *slog.Logger
	mux      *http.ServeMux

	breakerMu sync.Mutex
	breakers  map[string]*breaker.Breaker // one per provider, keyed by name
}

func New(registry *provider.Registry, tracker *usage.Tracker, logger *slog.Logger, demoMode bool) *Router {
	r := &Router{
		registry: registry,
		tracker:  tracker,
		logger:   logger,
		mux:      http.NewServeMux(),
		breakers: make(map[string]*breaker.Breaker),
	}
	r.mux.HandleFunc("POST /v1/chat/completions", r.handleChatCompletions)
	r.mux.HandleFunc("GET /healthz", r.handleHealthz)
	r.mux.HandleFunc("GET /v1/usage", r.handleUsage)
	// promhttp.Handler() reads all metrics registered with the default
	// Prometheus registry and writes them as plain text. That's all /metrics is.
	r.mux.Handle("GET /metrics", promhttp.Handler())
	// Catch-all: serve the embedded chat UI. All API routes above take priority
	// because ServeMux matches the most specific pattern first.
	r.mux.Handle("/", ui.Handler())
	if demoMode {
		r.mux.HandleFunc("GET /v1/debug/breakers", r.handleDebugBreakers)
		r.mux.HandleFunc("POST /v1/debug/break/{provider}", r.handleDebugBreak)
		r.mux.HandleFunc("POST /v1/debug/reset/{provider}", r.handleDebugReset)
		logger.Info("demo mode enabled: debug endpoints active",
			"endpoints", []string{
				"GET /v1/debug/breakers",
				"POST /v1/debug/break/{provider}",
				"POST /v1/debug/reset/{provider}",
			})
	}
	return r
}

// getBreakerFor returns the circuit breaker for a provider, creating it on
// first access. Lazy init avoids needing the provider list at construction time.
func (r *Router) getBreakerFor(name string) *breaker.Breaker {
	r.breakerMu.Lock()
	defer r.breakerMu.Unlock()
	if b, ok := r.breakers[name]; ok {
		return b
	}
	b := breaker.New(breakerThreshold, breakerCooldown)
	r.breakers[name] = b
	return b
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) handleHealthz(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (r *Router) handleChatCompletions(w http.ResponseWriter, req *http.Request) {
	var chatReq models.ChatRequest
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if chatReq.Model == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("model field is required"))
		return
	}

	providers, err := r.registry.Resolve(chatReq.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Try each provider in order. On failure, log and move to the next.
	// Only return 502 when every provider in the list has been exhausted.
	if chatReq.Stream {
		r.handleStream(w, req, providers, &chatReq)
	} else {
		r.handleComplete(w, req, providers, &chatReq)
	}
}

func (r *Router) handleComplete(w http.ResponseWriter, req *http.Request, providers []provider.Provider, chatReq *models.ChatRequest) {
	var lastErr error
	for _, p := range providers {
		b := r.getBreakerFor(p.Name())

		// If the breaker is open, skip this provider immediately.
		// No network call -- instant fallback.
		if !b.Allow() {
			r.logger.Warn("circuit breaker open, skipping provider",
				"provider", p.Name(),
				"state", b.CurrentState())
			lastErr = &breaker.ErrOpen{Provider: p.Name(), RetryIn: breakerCooldown}
			continue
		}

		r.logger.Info("attempting provider", "model", chatReq.Model, "provider", p.Name())
		start := time.Now()
		resp, err := p.Complete(req.Context(), chatReq)
		metrics.RequestDuration.WithLabelValues(p.Name()).Observe(time.Since(start).Seconds())
		if err != nil {
			b.RecordFailure()
			r.logger.Warn("provider failed, trying next", "provider", p.Name(), "error", err)
			r.tracker.RecordError(p.Name())
			metrics.ErrorsTotal.WithLabelValues(p.Name()).Inc()
			lastErr = err
			continue
		}
		b.RecordSuccess()
		r.tracker.RecordRequest(p.Name())
		r.tracker.RecordTokens(p.Name(), int64(resp.Usage.TotalTokens))
		metrics.RequestsTotal.WithLabelValues(p.Name(), chatReq.Model).Inc()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}
	r.logger.Error("all providers failed", "model", chatReq.Model, "last_error", lastErr)
	writeError(w, http.StatusBadGateway, fmt.Errorf("all providers failed: %w", lastErr))
}

func (r *Router) handleStream(w http.ResponseWriter, req *http.Request, providers []provider.Provider, chatReq *models.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported by response writer"))
		return
	}

	var lastErr error
	for _, p := range providers {
		b := r.getBreakerFor(p.Name())

		if !b.Allow() {
			r.logger.Warn("circuit breaker open, skipping provider",
				"provider", p.Name(),
				"state", b.CurrentState())
			lastErr = &breaker.ErrOpen{Provider: p.Name(), RetryIn: breakerCooldown}
			continue
		}

		r.logger.Info("attempting provider", "model", chatReq.Model, "provider", p.Name(), "stream", true)
		start := time.Now()
		chunks, err := p.Stream(req.Context(), chatReq)
		metrics.RequestDuration.WithLabelValues(p.Name()).Observe(time.Since(start).Seconds())
		if err != nil {
			b.RecordFailure()
			r.logger.Warn("provider stream failed to start, trying next", "provider", p.Name(), "error", err)
			r.tracker.RecordError(p.Name())
			metrics.ErrorsTotal.WithLabelValues(p.Name()).Inc()
			lastErr = err
			continue
		}

		// Stream started successfully -- commit the response headers now.
		// After this point we cannot fall back to another provider because
		// we have already started writing to the client.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		b.RecordSuccess()
		r.tracker.RecordRequest(p.Name())
		metrics.RequestsTotal.WithLabelValues(p.Name(), chatReq.Model).Inc()
		for chunk := range chunks {
			if chunk.Err != nil {
				r.logger.Error("stream error", "provider", p.Name(), "error", chunk.Err)
				r.tracker.RecordError(p.Name())
				metrics.ErrorsTotal.WithLabelValues(p.Name()).Inc()
				break
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if chunk.Done {
				break
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	r.logger.Error("all providers failed", "model", chatReq.Model, "last_error", lastErr)
	writeError(w, http.StatusBadGateway, fmt.Errorf("all providers failed: %w", lastErr))
}

// handleUsage returns a JSON snapshot of per-provider counters.
func (r *Router) handleUsage(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(r.tracker.Snapshot())
}

// --- Debug handlers (only registered when demoMode = true) ---

// handleDebugBreakers returns the current state of all known circuit breakers.
// The UI polls this to display live provider status in the demo panel.
func (r *Router) handleDebugBreakers(w http.ResponseWriter, req *http.Request) {
	r.breakerMu.Lock()
	states := make(map[string]string, len(r.breakers))
	for name, b := range r.breakers {
		states[name] = b.CurrentState().String()
	}
	r.breakerMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(states)
}

// handleDebugBreak trips the circuit breaker for the named provider by
// recording failures equal to the threshold. Used by the demo UI button.
func (r *Router) handleDebugBreak(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("provider")
	b := r.getBreakerFor(name)
	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure()
	}
	r.logger.Info("debug: circuit breaker tripped", "provider", name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"provider": name,
		"state":    b.CurrentState().String(),
		"message":  "breaker tripped -- will auto-recover after 30s",
	})
}

// handleDebugReset closes the circuit breaker for the named provider.
func (r *Router) handleDebugReset(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("provider")
	b := r.getBreakerFor(name)
	b.RecordSuccess()
	r.logger.Info("debug: circuit breaker reset", "provider", name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"provider": name,
		"state":    b.CurrentState().String(),
	})
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
