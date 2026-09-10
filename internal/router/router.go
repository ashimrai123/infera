// Package router wires up the HTTP handlers that expose the unified
// /v1/chat/completions endpoint, resolving each request to an ordered
// list of Providers via the registry and attempting failover on error.
package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ashimrai123/infera/internal/models"
	"github.com/ashimrai123/infera/internal/provider"
	"github.com/ashimrai123/infera/internal/usage"
)

type Router struct {
	registry *provider.Registry
	tracker  *usage.Tracker
	logger   *slog.Logger
	mux      *http.ServeMux
}

func New(registry *provider.Registry, tracker *usage.Tracker, logger *slog.Logger) *Router {
	r := &Router{
		registry: registry,
		tracker:  tracker,
		logger:   logger,
		mux:      http.NewServeMux(),
	}
	r.mux.HandleFunc("POST /v1/chat/completions", r.handleChatCompletions)
	r.mux.HandleFunc("GET /healthz", r.handleHealthz)
	r.mux.HandleFunc("GET /v1/usage", r.handleUsage)
	return r
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
		r.logger.Info("attempting provider", "model", chatReq.Model, "provider", p.Name())
		resp, err := p.Complete(req.Context(), chatReq)
		if err != nil {
			r.logger.Warn("provider failed, trying next", "provider", p.Name(), "error", err)
			r.tracker.RecordError(p.Name())
			lastErr = err
			continue
		}
		r.tracker.RecordRequest(p.Name())
		r.tracker.RecordTokens(p.Name(), int64(resp.Usage.TotalTokens))
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
		r.logger.Info("attempting provider", "model", chatReq.Model, "provider", p.Name(), "stream", true)
		chunks, err := p.Stream(req.Context(), chatReq)
		if err != nil {
			r.logger.Warn("provider stream failed to start, trying next", "provider", p.Name(), "error", err)
			r.tracker.RecordError(p.Name())
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

		r.tracker.RecordRequest(p.Name())
		for chunk := range chunks {
			if chunk.Err != nil {
				r.logger.Error("stream error", "provider", p.Name(), "error", chunk.Err)
				r.tracker.RecordError(p.Name())
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

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
