// Package router wires up the HTTP handlers that expose the unified
// /v1/chat/completions endpoint, resolving each request to a Provider
// via the registry and handling both streaming and non-streaming paths.
package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ashimrai123/infera/internal/models"
	"github.com/ashimrai123/infera/internal/provider"
)

type Router struct {
	registry *provider.Registry
	logger   *slog.Logger
	mux      *http.ServeMux
}

func New(registry *provider.Registry, logger *slog.Logger) *Router {
	r := &Router{
		registry: registry,
		logger:   logger,
		mux:      http.NewServeMux(),
	}
	r.mux.HandleFunc("POST /v1/chat/completions", r.handleChatCompletions)
	r.mux.HandleFunc("GET /healthz", r.handleHealthz)
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

	p, err := r.registry.Resolve(chatReq.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	r.logger.Info("routing request", "model", chatReq.Model, "provider", p.Name(), "stream", chatReq.Stream)

	if chatReq.Stream {
		r.handleStream(w, req, p, &chatReq)
		return
	}
	r.handleComplete(w, req, p, &chatReq)
}

func (r *Router) handleComplete(w http.ResponseWriter, req *http.Request, p provider.Provider, chatReq *models.ChatRequest) {
	resp, err := p.Complete(req.Context(), chatReq)
	if err != nil {
		r.logger.Error("provider completion failed", "provider", p.Name(), "error", err)
		writeError(w, http.StatusBadGateway, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (r *Router) handleStream(w http.ResponseWriter, req *http.Request, p provider.Provider, chatReq *models.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported by response writer"))
		return
	}

	chunks, err := p.Stream(req.Context(), chatReq)
	if err != nil {
		r.logger.Error("provider stream failed to start", "provider", p.Name(), "error", err)
		writeError(w, http.StatusBadGateway, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	for chunk := range chunks {
		if chunk.Err != nil {
			r.logger.Error("stream error", "provider", p.Name(), "error", chunk.Err)
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
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
