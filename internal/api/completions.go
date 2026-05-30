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
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/backends"
	"github.com/savxzthc/aegis-gateway/internal/db"
	modelrouter "github.com/savxzthc/aegis-gateway/internal/router"
)

const maxRequestBodyBytes int64 = 4 << 20

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
	defer func() {
		s.logRequest(r.Context(), started, keyID, requested, used, fallback, backendType, status, promptTokens, completionTokens)
	}()

	var req backends.ChatRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, "invalid JSON request body", "INVALID_JSON")
		return
	}
	requested = req.Model
	promptTokens = estimateMessages(req.Messages)
	if err := validateChatRequest(&req); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, err.Error(), "INVALID_REQUEST")
		return
	}

	selected, didFallback, err := s.VRAMRouter.SelectModel(r.Context(), req.Model)
	if err != nil {
		status = routeErrorStatus(err)
		writeError(w, status, err.Error(), routeErrorCode(err))
		return
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

	if req.Stream {
		result := s.streamChat(w, r, backend, &req, selected)
		completionTokens = result.Tokens
		status = result.Status
		return
	}

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
	writeJSON(w, status, newChatCompletionResponse(selected, resp.Content, promptTokens, completionTokens))
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
	defer func() {
		s.logRequest(r.Context(), started, keyID, requested, used, fallback, backendType, status, promptTokens, completionTokens)
	}()

	var req backends.CompletionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, "invalid JSON request body", "INVALID_JSON")
		return
	}
	requested = req.Model
	prompt := req.Prompt.String()
	promptTokens = estimateText(prompt)
	if req.Model == "" || req.Prompt.Empty() {
		status = http.StatusBadRequest
		writeError(w, status, "model and prompt are required", "INVALID_REQUEST")
		return
	}
	if err := validateSampling(req.N, req.MaxTokens, req.Temperature, req.TopP, req.PresencePenalty, req.FrequencyPenalty); err != nil {
		status = http.StatusBadRequest
		writeError(w, status, err.Error(), "INVALID_REQUEST")
		return
	}

	selected, didFallback, err := s.VRAMRouter.SelectModel(r.Context(), req.Model)
	if err != nil {
		status = routeErrorStatus(err)
		writeError(w, status, err.Error(), routeErrorCode(err))
		return
	}
	used = selected
	fallback = didFallback
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
	if req.Stream {
		result := s.streamCompletion(w, r, backend, chatReq, selected)
		completionTokens = result.Tokens
		status = result.Status
		return
	}

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
	writeJSON(w, status, newCompletionResponse(selected, resp.Content, promptTokens, completionTokens))
}

type streamResult struct {
	Tokens int
	Status int
}

func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, backend backends.Backend, req *backends.ChatRequest, model string) streamResult {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "STREAMING_UNSUPPORTED")
		return streamResult{Status: http.StatusInternalServerError}
	}
	prepareSSE(w)
	ch := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		errCh <- backend.StreamChat(r.Context(), req, ch)
		close(ch)
	}()

	completionTokens := 0
	id := responseID("chatcmpl")
	created := time.Now().Unix()
	writeSSE(w, flusher, chatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chatStreamChoice{{
			Index: 0,
			Delta: chatDelta{Role: "assistant"},
		}},
	})
	for token := range ch {
		completionTokens += estimateText(token)
		writeSSE(w, flusher, chatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []chatStreamChoice{{
				Index: 0,
				Delta: chatDelta{Content: token},
			}},
		})
	}
	if err := <-errCh; err != nil && !isContextDone(r.Context()) {
		s.logPrivateFailure(r, "BACKEND_STREAM_ERROR", model, "")
		writeSSE(w, flusher, errorResponse{Error: "backend stream failed", Code: "BACKEND_STREAM_ERROR"})
		return streamResult{Tokens: completionTokens, Status: http.StatusBadGateway}
	}
	if isContextDone(r.Context()) {
		return streamResult{Tokens: completionTokens, Status: 499}
	}
	finish := "stop"
	writeSSE(w, flusher, chatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chatStreamChoice{{Index: 0, Delta: chatDelta{}, FinishReason: &finish}},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	return streamResult{Tokens: completionTokens, Status: http.StatusOK}
}

func (s *Server) streamCompletion(w http.ResponseWriter, r *http.Request, backend backends.Backend, req *backends.ChatRequest, model string) streamResult {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "STREAMING_UNSUPPORTED")
		return streamResult{Status: http.StatusInternalServerError}
	}
	prepareSSE(w)
	ch := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		errCh <- backend.StreamChat(r.Context(), req, ch)
		close(ch)
	}()

	completionTokens := 0
	id := responseID("cmpl")
	created := time.Now().Unix()
	for token := range ch {
		completionTokens += estimateText(token)
		writeSSE(w, flusher, completionChunk{
			ID:      id,
			Object:  "text_completion",
			Created: created,
			Model:   model,
			Choices: []completionChoice{{Text: token, Index: 0}},
		})
	}
	if err := <-errCh; err != nil && !isContextDone(r.Context()) {
		s.logPrivateFailure(r, "BACKEND_STREAM_ERROR", model, "")
		writeSSE(w, flusher, errorResponse{Error: "backend stream failed", Code: "BACKEND_STREAM_ERROR"})
		return streamResult{Tokens: completionTokens, Status: http.StatusBadGateway}
	}
	if isContextDone(r.Context()) {
		return streamResult{Tokens: completionTokens, Status: 499}
	}
	finish := "stop"
	writeSSE(w, flusher, completionChunk{
		ID:      id,
		Object:  "text_completion",
		Created: created,
		Model:   model,
		Choices: []completionChoice{{Text: "", Index: 0, FinishReason: &finish}},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	return streamResult{Tokens: completionTokens, Status: http.StatusOK}
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

func (s *Server) logRequest(ctx context.Context, started time.Time, keyID, requested, used string, fallback bool, backendType string, status int, promptTokens, completionTokens int) {
	if keyID == "" {
		return
	}
	if used == "" {
		used = requested
	}
	logCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.DB.InsertRequestLog(logCtx, db.RequestLog{
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
	})
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

func validateChatRequest(req *backends.ChatRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages are required")
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
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	chars := len([]rune(value))
	tokens := chars / 4
	if tokens < 1 {
		return 1
	}
	return tokens
}

func responseID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

type chatCompletionResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []chatChoice   `json:"choices"`
	Usage   backends.Usage `json:"usage"`
}

type chatChoice struct {
	Index        int                  `json:"index"`
	Message      backends.ChatMessage `json:"message"`
	FinishReason string               `json:"finish_reason"`
}

type chatCompletionChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []chatStreamChoice `json:"choices"`
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
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []completionChoice `json:"choices"`
}

type completionChoice struct {
	Text         string  `json:"text"`
	Index        int     `json:"index"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

func newChatCompletionResponse(model, content string, promptTokens, completionTokens int) chatCompletionResponse {
	return chatCompletionResponse{
		ID:      responseID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      backends.ChatMessage{Role: "assistant", Content: backends.NewMessageContent(content)},
			FinishReason: "stop",
		}},
		Usage: backends.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func newCompletionResponse(model, content string, promptTokens, completionTokens int) completionResponse {
	finish := "stop"
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
