package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/backends"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
	modelrouter "github.com/savxzthc/aegis-gateway/internal/router"
	searchpkg "github.com/savxzthc/aegis-gateway/internal/search"
)

const maxRequestBodyBytes int64 = 4 << 20
const maxChatMessages = 1000
const maxMessageContentBytes = 256 << 10

// ChatCompletions handles POST /v1/chat/completions.
func (s *Server) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	requestCtx, cancelRequest := context.WithTimeout(r.Context(), s.Config.RequestTimeout())
	defer cancelRequest()
	r = r.WithContext(requestCtx)

	started := time.Now().UTC()
	keyID := auth.KeyIDFromContext(r.Context())
	status := http.StatusOK
	requested := ""
	used := ""
	backendType := ""
	fallback := false
	promptTokens := 0
	completionTokens := 0
	tokensPerSecond := 0.0
	defer func() {
		s.logRequest(r.Context(), started, keyID, requested, used, fallback, backendType, status, promptTokens, completionTokens, tokensPerSecond)
	}()

	var req backends.ChatRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, "invalid JSON request body", "INVALID_JSON")
		return
	}
	requested = req.Model
	if keyID == "" {
		keyID = requestOwnerID(r)
	}
	promptTokens = estimateMessages(req.Messages)
	if err := validateChatRequest(&req); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, err.Error(), "INVALID_REQUEST")
		return
	}
	if keyID != "" && auth.KeyIDFromContext(r.Context()) != "" {
		if ok, err := s.DB.KeyAllowsModel(r.Context(), keyID, req.Model); err != nil {
			status = http.StatusInternalServerError
			writePrivateError(w, r, status, "model ACL check failed", "MODEL_ACL_CHECK_FAILED", err)
			return
		} else if !ok {
			status = http.StatusForbidden
			writeError(w, status, "API key is not allowed to use this model", "MODEL_NOT_ALLOWED")
			return
		}
	}
	conversationID, err := s.prepareConversation(r, req.ConversationID, req.Persist, req.Model, lastUserText(req.Messages))
	if err != nil {
		status = http.StatusNotFound
		writeError(w, status, "conversation not found", "CONVERSATION_NOT_FOUND")
		return
	}
	if conversationID != "" {
		req.ConversationID = conversationID
		w.Header().Set("X-Aegis-Conversation-ID", conversationID)
	}

	selected, didFallback, err := s.VRAMRouter.SelectModel(r.Context(), req.Model)
	if err != nil {
		status = routeErrorStatus(err)
		writeError(w, status, err.Error(), routeErrorCode(err))
		return
	}
	if selected != req.Model && auth.KeyIDFromContext(r.Context()) != "" {
		if ok, err := s.DB.KeyAllowsModel(r.Context(), keyID, selected); err != nil {
			status = http.StatusInternalServerError
			writePrivateError(w, r, status, "model ACL check failed", "MODEL_ACL_CHECK_FAILED", err)
			return
		} else if !ok {
			status = http.StatusForbidden
			writeError(w, status, "API key is not allowed to use the routed fallback model", "MODEL_NOT_ALLOWED")
			return
		}
	}
	used = selected
	fallback = didFallback
	req.Model = selected
	req.Messages = s.withConfiguredSystemPrompt(req.Messages, selected)
	var searchUsed bool
	req.Messages, searchUsed = s.withSearchResults(r, req.Messages, req.Search)
	if searchUsed {
		w.Header().Set("X-Aegis-Search-Used", "true")
	}
	resolvedMessages, hasImages, err := s.resolveUploadImages(r, req.Messages)
	if err != nil {
		status = http.StatusBadRequest
		writeError(w, status, "referenced upload could not be loaded", "UPLOAD_NOT_FOUND")
		return
	}
	if hasImages && !config.SupportsVision(s.Config.Get(), selected) {
		status = http.StatusBadRequest
		writeError(w, status, "selected model is not configured for vision", "MODEL_NO_VISION")
		return
	}
	req.Messages = resolvedMessages
	promptTokens = estimateMessages(req.Messages)
	w.Header().Set("X-Aegis-Routed-Model", selected)
	w.Header().Set("X-Aegis-Fallback", fmt.Sprintf("%t", didFallback))

	var backend backends.Backend
	backendType, backend, _ = s.backendFor(selected)
	if backend == nil {
		status = http.StatusBadGateway
		writeError(w, status, "configured backend is unavailable", "BACKEND_UNAVAILABLE")
		return
	}
	if backendType == "ollama" {
		if err := s.Lifecycle.EnsureLoaded(r.Context(), selected); err != nil {
			status = http.StatusBadGateway
			s.logPrivateFailure(r, "MODEL_LOAD_FAILED", selected, backendType)
			writeError(w, status, "model load failed", "MODEL_LOAD_FAILED")
			return
		}
		defer s.Lifecycle.MarkIdle(selected)
	}

	if req.Stream {
		result := s.streamChat(w, r, backend, &req, selected)
		completionTokens = result.Tokens
		tokensPerSecond = result.TokensPerSecond
		status = result.Status
		if status == http.StatusOK && conversationID != "" {
			s.persistConversationPair(r, conversationID, lastUserText(req.Messages), result.Content, selected, tokensPerSecond)
		}
		return
	}

	generationStarted := time.Now()
	resp, err := backend.Chat(r.Context(), &req, false)
	if err != nil {
		status = http.StatusBadGateway
		s.logPrivateFailure(r, "BACKEND_ERROR", selected, backendType)
		writeError(w, status, "backend request failed", "BACKEND_ERROR")
		return
	}
	completionTokens = resp.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = estimateText(resp.Content)
	}
	tokensPerSecond = calculateTPS(completionTokens, time.Since(generationStarted))
	w.Header().Set("X-Aegis-TPS", fmt.Sprintf("%.1f", tokensPerSecond))
	if conversationID != "" {
		s.persistConversationPair(r, conversationID, lastUserText(req.Messages), resp.Content, selected, tokensPerSecond)
	}
	writeJSON(w, status, newChatCompletionResponse(selected, resp.Content, promptTokens, completionTokens, completionFinishReason(resp.FinishReason, req.MaxTokens, completionTokens)))
}

// Completions handles POST /v1/completions.
func (s *Server) Completions(w http.ResponseWriter, r *http.Request) {
	requestCtx, cancelRequest := context.WithTimeout(r.Context(), s.Config.RequestTimeout())
	defer cancelRequest()
	r = r.WithContext(requestCtx)

	started := time.Now().UTC()
	keyID := auth.KeyIDFromContext(r.Context())
	status := http.StatusOK
	requested := ""
	used := ""
	backendType := ""
	fallback := false
	promptTokens := 0
	completionTokens := 0
	tokensPerSecond := 0.0
	defer func() {
		s.logRequest(r.Context(), started, keyID, requested, used, fallback, backendType, status, promptTokens, completionTokens, tokensPerSecond)
	}()

	var req backends.CompletionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, "invalid JSON request body", "INVALID_JSON")
		return
	}
	requested = req.Model
	if keyID == "" {
		keyID = requestOwnerID(r)
	}
	prompt := req.Prompt.String()
	promptTokens = estimateText(prompt)
	if req.Model == "" || req.Prompt.Empty() {
		status = http.StatusBadRequest
		writeError(w, status, "model and prompt are required", "INVALID_REQUEST")
		return
	}
	if auth.KeyIDFromContext(r.Context()) != "" {
		if ok, err := s.DB.KeyAllowsModel(r.Context(), keyID, req.Model); err != nil {
			status = http.StatusInternalServerError
			writePrivateError(w, r, status, "model ACL check failed", "MODEL_ACL_CHECK_FAILED", err)
			return
		} else if !ok {
			status = http.StatusForbidden
			writeError(w, status, "API key is not allowed to use this model", "MODEL_NOT_ALLOWED")
			return
		}
	}
	if err := validateSampling(req.N, req.MaxTokens, req.Temperature, req.TopP, req.PresencePenalty, req.FrequencyPenalty); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, err.Error(), "INVALID_REQUEST")
		return
	}
	conversationID, err := s.prepareConversation(r, req.ConversationID, req.Persist, req.Model, prompt)
	if err != nil {
		status = http.StatusNotFound
		writeError(w, status, "conversation not found", "CONVERSATION_NOT_FOUND")
		return
	}
	if conversationID != "" {
		w.Header().Set("X-Aegis-Conversation-ID", conversationID)
	}

	selected, didFallback, err := s.VRAMRouter.SelectModel(r.Context(), req.Model)
	if err != nil {
		status = routeErrorStatus(err)
		writeError(w, status, err.Error(), routeErrorCode(err))
		return
	}
	if selected != req.Model && auth.KeyIDFromContext(r.Context()) != "" {
		if ok, err := s.DB.KeyAllowsModel(r.Context(), keyID, selected); err != nil {
			status = http.StatusInternalServerError
			writePrivateError(w, r, status, "model ACL check failed", "MODEL_ACL_CHECK_FAILED", err)
			return
		} else if !ok {
			status = http.StatusForbidden
			writeError(w, status, "API key is not allowed to use the routed fallback model", "MODEL_NOT_ALLOWED")
			return
		}
	}
	used = selected
	fallback = didFallback
	req.Model = selected
	w.Header().Set("X-Aegis-Routed-Model", selected)
	w.Header().Set("X-Aegis-Fallback", fmt.Sprintf("%t", didFallback))

	var backend backends.Backend
	backendType, backend, _ = s.backendFor(selected)
	if backend == nil {
		status = http.StatusBadGateway
		writeError(w, status, "configured backend is unavailable", "BACKEND_UNAVAILABLE")
		return
	}
	if backendType == "ollama" {
		if err := s.Lifecycle.EnsureLoaded(r.Context(), selected); err != nil {
			status = http.StatusBadGateway
			s.logPrivateFailure(r, "MODEL_LOAD_FAILED", selected, backendType)
			writeError(w, status, "model load failed", "MODEL_LOAD_FAILED")
			return
		}
		defer s.Lifecycle.MarkIdle(selected)
	}

	chatReq := &backends.ChatRequest{
		Model:       selected,
		Messages:    []backends.ChatMessage{{Role: "user", Content: backends.NewMessageContent(prompt)}},
		Stream:      req.Stream,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop,
		Seed:        req.Seed,
	}
	chatReq.Messages = s.withConfiguredSystemPrompt(chatReq.Messages, selected)
	var searchUsed bool
	chatReq.Messages, searchUsed = s.withSearchResults(r, chatReq.Messages, req.Search)
	if searchUsed {
		w.Header().Set("X-Aegis-Search-Used", "true")
	}
	if req.Stream {
		result := s.streamCompletion(w, r, backend, chatReq, selected)
		completionTokens = result.Tokens
		tokensPerSecond = result.TokensPerSecond
		status = result.Status
		if status == http.StatusOK && conversationID != "" {
			s.persistConversationPair(r, conversationID, prompt, result.Content, selected, tokensPerSecond)
		}
		return
	}

	generationStarted := time.Now()
	resp, err := backend.Chat(r.Context(), chatReq, false)
	if err != nil {
		status = http.StatusBadGateway
		s.logPrivateFailure(r, "BACKEND_ERROR", selected, backendType)
		writeError(w, status, "backend request failed", "BACKEND_ERROR")
		return
	}
	completionTokens = resp.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = estimateText(resp.Content)
	}
	tokensPerSecond = calculateTPS(completionTokens, time.Since(generationStarted))
	w.Header().Set("X-Aegis-TPS", fmt.Sprintf("%.1f", tokensPerSecond))
	if conversationID != "" {
		s.persistConversationPair(r, conversationID, prompt, resp.Content, selected, tokensPerSecond)
	}
	writeJSON(w, status, newCompletionResponse(selected, resp.Content, promptTokens, completionTokens, completionFinishReason(resp.FinishReason, req.MaxTokens, completionTokens)))
}

type streamResult struct {
	Tokens          int
	Status          int
	Content         string
	TokensPerSecond float64
}

func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, backend backends.Backend, req *backends.ChatRequest, model string) streamResult {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "STREAMING_UNSUPPORTED")
		return streamResult{Status: http.StatusInternalServerError}
	}
	prepareSSE(w)
	w.Header().Add("Trailer", "X-Aegis-TPS")
	ch := make(chan backends.StreamChunk)
	errCh := make(chan error, 1)
	streamCtx, cancelStream := s.streamContext(r.Context())
	defer cancelStream()
	go func() {
		errCh <- backend.StreamChat(streamCtx, req, ch)
		close(ch)
	}()

	completionTokens := 0
	id := responseID("chatcmpl")
	created := time.Now().Unix()
	writeSSE(w, flusher, chatCompletionChunk{
		ID:                id,
		Object:            "chat.completion.chunk",
		Created:           created,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []chatStreamChoice{{
			Index: 0,
			Delta: chatDelta{Role: "assistant"},
		}},
	})
	finishReason := ""
	var content strings.Builder
	var firstTokenAt time.Time
	for chunk := range ch {
		if chunk.Usage.CompletionTokens > 0 {
			completionTokens = chunk.Usage.CompletionTokens
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
		if chunk.Content == "" {
			continue
		}
		if firstTokenAt.IsZero() {
			firstTokenAt = time.Now()
		}
		content.WriteString(chunk.Content)
		if completionTokens == 0 || chunk.Usage.CompletionTokens == 0 {
			completionTokens += estimateText(chunk.Content)
		}
		writeSSE(w, flusher, chatCompletionChunk{
			ID:                id,
			Object:            "chat.completion.chunk",
			Created:           created,
			Model:             model,
			SystemFingerprint: systemFingerprint,
			Choices: []chatStreamChoice{{
				Index: 0,
				Delta: chatDelta{Content: chunk.Content},
			}},
		})
	}
	if err := <-errCh; err != nil && !isContextDone(streamCtx) {
		s.logPrivateFailure(r, "BACKEND_STREAM_ERROR", model, "")
		writeSSE(w, flusher, errorResponse{Error: "backend stream failed", Code: "BACKEND_STREAM_ERROR"})
		return streamResult{Tokens: completionTokens, Status: http.StatusBadGateway, Content: content.String()}
	}
	if isContextDone(streamCtx) {
		return streamResult{Tokens: completionTokens, Status: 499, Content: content.String()}
	}
	finish := completionFinishReason(finishReason, req.MaxTokens, completionTokens)
	var usage *backends.Usage
	if req.StreamOptions.IncludeUsage {
		promptTokens := estimateMessages(req.Messages)
		usage = &backends.Usage{PromptTokens: promptTokens, CompletionTokens: completionTokens, TotalTokens: promptTokens + completionTokens}
	}
	writeSSE(w, flusher, chatCompletionChunk{
		ID:                id,
		Object:            "chat.completion.chunk",
		Created:           created,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices:           []chatStreamChoice{{Index: 0, Delta: chatDelta{}, FinishReason: &finish}},
		Usage:             usage,
	})
	tps := calculateTPS(completionTokens, time.Since(firstTokenAt))
	writeSSE(w, flusher, map[string]interface{}{"type": "meta", "tokens_per_second": tps})
	w.Header().Set("X-Aegis-TPS", fmt.Sprintf("%.1f", tps))
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	return streamResult{Tokens: completionTokens, Status: http.StatusOK, Content: content.String(), TokensPerSecond: tps}
}

func (s *Server) streamCompletion(w http.ResponseWriter, r *http.Request, backend backends.Backend, req *backends.ChatRequest, model string) streamResult {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "STREAMING_UNSUPPORTED")
		return streamResult{Status: http.StatusInternalServerError}
	}
	prepareSSE(w)
	w.Header().Add("Trailer", "X-Aegis-TPS")
	ch := make(chan backends.StreamChunk)
	errCh := make(chan error, 1)
	streamCtx, cancelStream := s.streamContext(r.Context())
	defer cancelStream()
	go func() {
		errCh <- backend.StreamChat(streamCtx, req, ch)
		close(ch)
	}()

	completionTokens := 0
	id := responseID("cmpl")
	created := time.Now().Unix()
	finishReason := ""
	var firstTokenAt time.Time
	var content strings.Builder
	for chunk := range ch {
		if chunk.Usage.CompletionTokens > 0 {
			completionTokens = chunk.Usage.CompletionTokens
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
		if chunk.Content == "" {
			continue
		}
		if firstTokenAt.IsZero() {
			firstTokenAt = time.Now()
		}
		content.WriteString(chunk.Content)
		if completionTokens == 0 || chunk.Usage.CompletionTokens == 0 {
			completionTokens += estimateText(chunk.Content)
		}
		writeSSE(w, flusher, completionChunk{
			ID:      id,
			Object:  "text_completion",
			Created: created,
			Model:   model,
			Choices: []completionChoice{{Text: chunk.Content, Index: 0}},
		})
	}
	if err := <-errCh; err != nil && !isContextDone(streamCtx) {
		s.logPrivateFailure(r, "BACKEND_STREAM_ERROR", model, "")
		writeSSE(w, flusher, errorResponse{Error: "backend stream failed", Code: "BACKEND_STREAM_ERROR"})
		return streamResult{Tokens: completionTokens, Status: http.StatusBadGateway}
	}
	if isContextDone(streamCtx) {
		return streamResult{Tokens: completionTokens, Status: 499}
	}
	finish := completionFinishReason(finishReason, req.MaxTokens, completionTokens)
	writeSSE(w, flusher, completionChunk{
		ID:                id,
		Object:            "text_completion",
		Created:           created,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices:           []completionChoice{{Text: "", Index: 0, FinishReason: &finish}},
	})
	tps := calculateTPS(completionTokens, time.Since(firstTokenAt))
	writeSSE(w, flusher, map[string]interface{}{"type": "meta", "tokens_per_second": tps})
	w.Header().Set("X-Aegis-TPS", fmt.Sprintf("%.1f", tps))
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	return streamResult{Tokens: completionTokens, Status: http.StatusOK, Content: content.String(), TokensPerSecond: tps}
}

func prepareSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, value interface{}) {
	payload, _ := json.Marshal(value)
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

func (s *Server) streamContext(parent context.Context) (context.Context, context.CancelFunc) {
	if s.Shutdown == nil {
		return context.WithCancel(parent)
	}
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-ctx.Done():
		case <-s.Shutdown:
			cancel()
		}
	}()
	return ctx, cancel
}

func (s *Server) logRequest(ctx context.Context, started time.Time, keyID, requested, used string, fallback bool, backendType string, status int, promptTokens, completionTokens int, tokensPerSecond float64) {
	if keyID == "" {
		log.Printf("aegis: request log skipped because authenticated key id is missing")
		return
	}
	if used == "" {
		used = requested
	}
	logCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.DB.InsertRequestLog(logCtx, db.RequestLog{
		Timestamp:                 started,
		KeyID:                     keyID,
		ModelRequested:            requested,
		ModelUsed:                 used,
		FallbackTriggered:         fallback,
		BackendType:               backendType,
		LatencyMS:                 time.Since(started).Milliseconds(),
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: completionTokens,
		StatusCode:                status,
		TokensPerSecond:           tokensPerSecond,
	}); err != nil {
		log.Printf("aegis: request log failed: %v", err)
	}
}

func (s *Server) logPrivateFailure(r *http.Request, code, model, backendType string) {
	requestID := middleware.GetReqID(r.Context())
	if backendType == "" && model != "" {
		backendType, _, _ = s.backendFor(model)
	}
	log.Printf("aegis: request failure request_id=%s code=%s model=%s backend=%s", requestID, code, model, backendType)
}

func routeErrorStatus(err error) int {
	if errors.Is(err, modelrouter.ErrModelNotRegistered) {
		return http.StatusBadRequest
	}
	return http.StatusServiceUnavailable
}

func routeErrorCode(err error) string {
	if errors.Is(err, modelrouter.ErrModelNotRegistered) {
		return "MODEL_NOT_REGISTERED"
	}
	return "NO_MODEL_FITS"
}

func isContextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func estimateMessages(messages []backends.ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += estimateText(msg.Role) + estimateText(msg.Content.String())
	}
	return total
}

func (s *Server) withConfiguredSystemPrompt(messages []backends.ChatMessage, model string) []backends.ChatMessage {
	if hasSystemOrDeveloperMessage(messages) {
		return messages
	}
	cfg := s.Config.Get()
	systemPrompt := strings.TrimSpace(cfg.Server.SystemPrompt)
	if modelCfg, ok := cfg.Models.Registry[model]; ok && strings.TrimSpace(modelCfg.SystemPrompt) != "" {
		if systemPrompt == "" {
			systemPrompt = strings.TrimSpace(modelCfg.SystemPrompt)
		} else {
			systemPrompt = systemPrompt + "\n\nModel-specific addendum:\n" + strings.TrimSpace(modelCfg.SystemPrompt)
		}
	}
	if systemPrompt == "" {
		return messages
	}
	next := make([]backends.ChatMessage, 0, len(messages)+1)
	next = append(next, backends.ChatMessage{Role: "system", Content: backends.NewMessageContent(systemPrompt)})
	next = append(next, messages...)
	return next
}

func hasSystemOrDeveloperMessage(messages []backends.ChatMessage) bool {
	for _, message := range messages {
		if message.Role == "system" || message.Role == "developer" {
			return true
		}
	}
	return false
}

func validateChatRequest(req *backends.ChatRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages are required")
	}
	if len(req.Messages) > maxChatMessages {
		return fmt.Errorf("messages must contain at most %d items", maxChatMessages)
	}
	for i, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer", "user", "assistant", "tool":
		default:
			return fmt.Errorf("messages[%d].role is invalid", i)
		}
		if msg.Content.Empty() {
			return fmt.Errorf("messages[%d].content is required", i)
		}
		if len(msg.Content.String()) > maxMessageContentBytes {
			return fmt.Errorf("messages[%d].content exceeds %d bytes", i, maxMessageContentBytes)
		}
	}
	return validateSampling(req.N, req.MaxTokens, req.Temperature, req.TopP, req.PresencePenalty, req.FrequencyPenalty)
}

func validateSampling(n *int, maxTokens *int, temperature, topP, presencePenalty, frequencyPenalty *float64) error {
	if n != nil && *n != 1 {
		return fmt.Errorf("n values other than 1 are not supported")
	}
	if maxTokens != nil && *maxTokens <= 0 {
		return fmt.Errorf("max_tokens must be greater than zero")
	}
	if temperature != nil && (*temperature < 0 || *temperature > 2) {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	if topP != nil && (*topP < 0 || *topP > 1) {
		return fmt.Errorf("top_p must be between 0 and 1")
	}
	if presencePenalty != nil && (*presencePenalty < -2 || *presencePenalty > 2) {
		return fmt.Errorf("presence_penalty must be between -2 and 2")
	}
	if frequencyPenalty != nil && (*frequencyPenalty < -2 || *frequencyPenalty > 2) {
		return fmt.Errorf("frequency_penalty must be between -2 and 2")
	}
	return nil
}

func estimateText(value string) int {
	// This is a fast cross-backend approximation, not a model tokenizer.
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	asciiBytes := 0
	nonLatin := 0
	symbols := 0
	for _, r := range value {
		switch {
		case r <= unicode.MaxASCII:
			asciiBytes++
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				symbols++
			}
		case unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana):
			nonLatin++
		case !unicode.IsSpace(r):
			nonLatin++
		}
	}
	byBytes := (asciiBytes + 3) / 4
	byWords := len(tokenEstimateRE.FindAllString(value, -1))
	bySymbols := symbols / 2
	estimate := maxInt(byBytes, byWords+bySymbols, nonLatin)
	if estimate < 1 {
		return 1
	}
	return estimate
}

func calculateTPS(tokens int, elapsed time.Duration) float64 {
	if tokens <= 0 || elapsed <= 0 || elapsed > 24*time.Hour {
		return 0
	}
	return float64(tokens) / elapsed.Seconds()
}

func lastUserText(messages []backends.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content.String()
		}
	}
	return ""
}

func (s *Server) prepareConversation(r *http.Request, requested string, persist bool, model, firstMessage string) (string, error) {
	if requested != "" {
		_, ok, err := s.DB.ConversationByID(r.Context(), requested, requestOwnerID(r))
		if err != nil || !ok {
			return "", fmt.Errorf("conversation unavailable")
		}
		return requested, nil
	}
	if !persist {
		return "", nil
	}
	title := strings.TrimSpace(firstMessage)
	if len([]rune(title)) > 80 {
		title = string([]rune(title)[:80])
	}
	item, err := s.newConversation(r, title, model)
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

func (s *Server) persistConversationPair(r *http.Request, conversationID, userText, assistantText, model string, tps float64) {
	now := time.Now().UTC()
	userID, err := auth.GenerateID("msg")
	if err != nil {
		log.Printf("aegis: conversation message id failed: %v", err)
		return
	}
	assistantID, err := auth.GenerateID("msg")
	if err != nil {
		log.Printf("aegis: conversation message id failed: %v", err)
		return
	}
	ok, err := s.DB.AppendConversationMessages(r.Context(), conversationID, requestOwnerID(r), []db.ChatMessage{
		{ID: userID, Role: "user", Content: userText, CreatedAt: now},
		{ID: assistantID, Role: "assistant", Content: assistantText, ModelUsed: model, TokensPerSecond: tps, CreatedAt: now.Add(time.Nanosecond)},
	}, now)
	if err != nil || !ok {
		log.Printf("aegis: conversation append failed conversation=%s error=%v", conversationID, err)
	}
}

func (s *Server) withSearchResults(r *http.Request, messages []backends.ChatMessage, requested *bool) ([]backends.ChatMessage, bool) {
	cfg := s.Config.Get()
	enabled := cfg.Search.Enabled
	if requested != nil {
		enabled = *requested
	}
	if !enabled || containsSearchResults(messages) {
		return messages, false
	}
	query := lastUserText(messages)
	if strings.TrimSpace(query) == "" {
		return messages, false
	}
	client, err := searchpkg.NewClient(cfg.Search)
	if err != nil {
		return messages, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Search.TimeoutSeconds)*time.Second)
	defer cancel()
	results, err := client.Search(ctx, query)
	if err != nil {
		log.Printf("aegis: search grounding unavailable provider=%s error=%v", cfg.Search.Provider, err)
		return messages, false
	}
	formatted := searchpkg.FormatResults(results)
	if formatted == "" {
		return messages, false
	}
	index := 0
	for index < len(messages) && (messages[index].Role == "system" || messages[index].Role == "developer") {
		index++
	}
	next := make([]backends.ChatMessage, 0, len(messages)+1)
	next = append(next, messages[:index]...)
	next = append(next, backends.ChatMessage{Role: "system", Content: backends.NewMessageContent(formatted)})
	next = append(next, messages[index:]...)
	return next, true
}

func containsSearchResults(messages []backends.ChatMessage) bool {
	for _, message := range messages {
		if strings.Contains(message.Content.String(), "[Search Results]") {
			return true
		}
	}
	return false
}

var tokenEstimateRE = regexp.MustCompile(`[A-Za-z0-9_]+|[^\sA-Za-z0-9_]`)

func maxInt(values ...int) int {
	maximum := 0
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func responseID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

type chatCompletionResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	SystemFingerprint string         `json:"system_fingerprint"`
	Choices           []chatChoice   `json:"choices"`
	Usage             backends.Usage `json:"usage"`
}

type chatChoice struct {
	Index        int                  `json:"index"`
	Message      backends.ChatMessage `json:"message"`
	FinishReason string               `json:"finish_reason"`
}

type chatCompletionChunk struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	Created           int64              `json:"created"`
	Model             string             `json:"model"`
	SystemFingerprint string             `json:"system_fingerprint"`
	Choices           []chatStreamChoice `json:"choices"`
	Usage             *backends.Usage    `json:"usage,omitempty"`
}

type chatStreamChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason,omitempty"`
}

type chatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type completionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []completionChoice `json:"choices"`
	Usage   backends.Usage     `json:"usage"`
}

type completionChunk struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	Created           int64              `json:"created"`
	Model             string             `json:"model"`
	SystemFingerprint string             `json:"system_fingerprint,omitempty"`
	Choices           []completionChoice `json:"choices"`
}

type completionChoice struct {
	Text         string  `json:"text"`
	Index        int     `json:"index"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

func completionFinishReason(backendReason string, maxTokens *int, completionTokens int) string {
	switch strings.ToLower(strings.TrimSpace(backendReason)) {
	case "length":
		return "length"
	case "stop", "stopped", "end_turn", "eos", "unload", "unloaded":
		return "stop"
	case "tool_calls", "content_filter":
		return strings.ToLower(strings.TrimSpace(backendReason))
	}
	return "stop"
}

func newChatCompletionResponse(model, content string, promptTokens, completionTokens int, finishReason string) chatCompletionResponse {
	return chatCompletionResponse{
		ID:                responseID("chatcmpl"),
		Object:            "chat.completion",
		Created:           time.Now().Unix(),
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []chatChoice{{
			Index:        0,
			Message:      backends.ChatMessage{Role: "assistant", Content: backends.NewMessageContent(content)},
			FinishReason: finishReason,
		}},
		Usage: backends.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

const systemFingerprint = "aegis-local"

func newCompletionResponse(model, content string, promptTokens, completionTokens int, finish string) completionResponse {
	return completionResponse{
		ID:      responseID("cmpl"),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []completionChoice{{Text: content, Index: 0, FinishReason: &finish}},
		Usage: backends.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}
