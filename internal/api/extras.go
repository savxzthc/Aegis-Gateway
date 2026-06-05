package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/backends"
)

// HardwareStream handles GET /v1/hardware/stream — SSE stream of GPU state every 2s.
func (s *Server) HardwareStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "STREAMING_UNSUPPORTED")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	send := func() {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		info, err := s.HardwareProvider.Query(ctx)
		if err != nil {
			return
		}
		payload, err := json.Marshal(map[string]interface{}{
			"gpus": []interface{}{map[string]interface{}{
				"index":         0,
				"name":          info.GPUName,
				"vram_free_gb":  info.VRAMFreeGB,
				"vram_total_gb": info.VRAMTotalGB,
				"detected":      info.Detected,
			}},
			"ts": time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	send()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

// UnloadModel handles POST /v1/models/unload.
func (s *Server) UnloadModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required", "INVALID_REQUEST")
		return
	}
	if err := s.Lifecycle.ForceUnload(req.Model); err != nil {
		log.Printf("aegis: unload model=%s err=%v", req.Model, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// Embeddings handles POST /v1/embeddings.
func (s *Server) Embeddings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string      `json:"model"`
		Input interface{} `json:"input"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required", "INVALID_REQUEST")
		return
	}

	backendType, backend, ok := s.backendFor(req.Model)
	if !ok || backend == nil {
		writeError(w, http.StatusBadGateway, "configured backend is unavailable", "BACKEND_UNAVAILABLE")
		return
	}

	if emb, ok := backend.(backends.EmbeddingBackend); ok {
		var inputs []string
		switch v := req.Input.(type) {
		case string:
			inputs = []string{v}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					inputs = append(inputs, s)
				}
			}
		}
		result, err := emb.Embed(r.Context(), req.Model, inputs)
		if err != nil {
			writeError(w, http.StatusBadGateway, "embedding failed", "BACKEND_ERROR")
			return
		}
		data := make([]map[string]interface{}, len(result))
		for i, vec := range result {
			data[i] = map[string]interface{}{
				"object":    "embedding",
				"embedding": vec,
				"index":     i,
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"object": "list",
			"data":   data,
			"model":  req.Model,
		})
		return
	}
	_ = backendType
	writeError(w, http.StatusNotImplemented, "this backend does not support embeddings", "EMBEDDINGS_NOT_SUPPORTED")
}

// ReloadConfig handles POST /v1/config/reload — admin only.
func (s *Server) ReloadConfig(w http.ResponseWriter, r *http.Request) {
	if !requireRole(w, r, "admin") {
		return
	}
	warnings, err := s.Config.Reload()
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "config reload failed", "CONFIG_RELOAD_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reloaded": true,
		"warnings": warnings,
	})
}

// BatchCompletions handles POST /v1/batch.
func (s *Server) BatchCompletions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Requests []backends.ChatRequest `json:"requests"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	if len(req.Requests) == 0 {
		writeError(w, http.StatusBadRequest, "requests array is required", "INVALID_REQUEST")
		return
	}
	if len(req.Requests) > 10 {
		writeError(w, http.StatusBadRequest, "batch supports at most 10 requests", "BATCH_TOO_LARGE")
		return
	}

	type result struct {
		idx  int
		resp interface{}
	}

	results := make([]interface{}, len(req.Requests))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for i, chatReq := range req.Requests {
		wg.Add(1)
		go func(idx int, cr backends.ChatRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cr.Stream = false
			_, backend, ok := s.backendFor(cr.Model)
			if !ok || backend == nil {
				results[idx] = map[string]interface{}{"error": "backend unavailable", "code": "BACKEND_UNAVAILABLE"}
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), s.Config.RequestTimeout())
			defer cancel()
			resp, err := backend.Chat(ctx, &cr, false)
			if err != nil {
				results[idx] = map[string]interface{}{"error": err.Error(), "code": "BACKEND_ERROR"}
				return
			}
			results[idx] = newChatCompletionResponse(cr.Model, resp.Content, 0, resp.Usage.CompletionTokens, "stop")
		}(i, chatReq)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": results})
}

