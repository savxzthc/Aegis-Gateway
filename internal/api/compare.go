package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/backends"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

func (s *Server) StartComparison(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt  string `json:"prompt"`
		ModelA  string `json:"model_a"`
		ModelB  string `json:"model_b"`
		IsBlind *bool  `json:"is_blind"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	req.Prompt, req.ModelA, req.ModelB = strings.TrimSpace(req.Prompt), strings.TrimSpace(req.ModelA), strings.TrimSpace(req.ModelB)
	if req.Prompt == "" || req.ModelA == "" || req.ModelB == "" || req.ModelA == req.ModelB {
		writeError(w, http.StatusBadRequest, "prompt and two different models are required", "INVALID_REQUEST")
		return
	}
	keyID := auth.KeyIDFromContext(r.Context())
	for _, model := range []string{req.ModelA, req.ModelB} {
		if keyID != "" {
			allowed, err := s.DB.KeyAllowsModel(r.Context(), keyID, model)
			if err != nil {
				writePrivateError(w, r, http.StatusInternalServerError, "model ACL check failed", "MODEL_ACL_CHECK_FAILED", err)
				return
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "API key is not allowed to use both models", "MODEL_NOT_ALLOWED")
				return
			}
		}
	}
	id, err := auth.GenerateID("cmp")
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "comparison creation failed", "COMPARISON_CREATE_FAILED", err)
		return
	}
	isBlind := true
	if req.IsBlind != nil {
		isBlind = *req.IsBlind
	}
	blindMap := `{"left":"a","right":"b"}`
	var randomByte [1]byte
	if _, err := rand.Read(randomByte[:]); err == nil && randomByte[0]&1 == 1 {
		blindMap = `{"left":"b","right":"a"}`
	}
	item := db.Comparison{ID: id, OwnerID: requestOwnerID(r), Prompt: req.Prompt, ModelA: req.ModelA, ModelB: req.ModelB, IsBlind: isBlind, BlindMap: blindMap, CreatedAt: time.Now().UTC()}
	if err := s.DB.CreateComparison(r.Context(), item); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "comparison creation failed", "COMPARISON_CREATE_FAILED", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.Config.RequestTimeout())
	defer cancel()
	var wg sync.WaitGroup
	var responseA, responseB string
	var errA, errB error
	wg.Add(2)
	go func() { defer wg.Done(); responseA, errA = s.compareChat(ctx, req.ModelA, req.Prompt) }()
	go func() { defer wg.Done(); responseB, errB = s.compareChat(ctx, req.ModelB, req.Prompt) }()
	wg.Wait()
	if errA != nil || errB != nil {
		writeError(w, http.StatusBadGateway, "comparison backend request failed", "COMPARISON_BACKEND_ERROR")
		return
	}
	completed := time.Now().UTC()
	if err := s.DB.CompleteComparison(r.Context(), id, item.OwnerID, responseA, responseB, completed); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "comparison save failed", "COMPARISON_SAVE_FAILED", err)
		return
	}
	item.ResponseA, item.ResponseB, item.CompletedAt = responseA, responseB, &completed
	writeJSON(w, http.StatusOK, comparisonView(item))
}

func (s *Server) compareChat(ctx context.Context, model, prompt string) (string, error) {
	selected, _, err := s.VRAMRouter.SelectModel(ctx, model)
	if err != nil {
		return "", err
	}
	backendType, backend, _ := s.backendFor(selected)
	if backend == nil {
		return "", context.Canceled
	}
	if backendType == "ollama" {
		if err := s.Lifecycle.EnsureLoaded(ctx, selected); err != nil {
			return "", err
		}
		defer s.Lifecycle.MarkIdle(selected)
	}
	resp, err := backend.Chat(ctx, &backends.ChatRequest{Model: selected, Messages: []backends.ChatMessage{{Role: "user", Content: backends.NewMessageContent(prompt)}}}, false)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (s *Server) GetComparison(w http.ResponseWriter, r *http.Request) {
	item, ok, err := s.DB.ComparisonByID(r.Context(), chi.URLParam(r, "id"), requestOwnerID(r))
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "comparison query failed", "COMPARISON_QUERY_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "comparison not found", "COMPARISON_NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, comparisonView(item))
}

func (s *Server) ListComparisons(w http.ResponseWriter, r *http.Request) {
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20)
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 100 {
		limit = 100
	}
	items, err := s.DB.ListComparisons(r.Context(), requestOwnerID(r), limit, offset)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "comparison query failed", "COMPARISON_QUERY_FAILED", err)
		return
	}
	views := make([]db.Comparison, 0, len(items))
	for _, item := range items {
		views = append(views, comparisonView(item))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": views})
}

func (s *Server) VoteComparison(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Winner string `json:"winner"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	if req.Winner != "a" && req.Winner != "b" && req.Winner != "tie" {
		writeError(w, http.StatusBadRequest, "winner must be a, b, or tie", "INVALID_REQUEST")
		return
	}
	id := chi.URLParam(r, "id")
	item, found, err := s.DB.ComparisonByID(r.Context(), id, requestOwnerID(r))
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "comparison query failed", "COMPARISON_QUERY_FAILED", err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "comparison not found", "COMPARISON_NOT_FOUND")
		return
	}
	winner := req.Winner
	if item.IsBlind && winner != "tie" {
		var mapping struct {
			Left  string `json:"left"`
			Right string `json:"right"`
		}
		if json.Unmarshal([]byte(item.BlindMap), &mapping) == nil {
			if winner == "a" {
				winner = mapping.Left
			} else {
				winner = mapping.Right
			}
		}
	}
	ok, err := s.DB.VoteComparison(r.Context(), id, requestOwnerID(r), winner)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "comparison vote failed", "COMPARISON_VOTE_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "comparison not found", "COMPARISON_NOT_FOUND")
		return
	}
	s.GetComparison(w, r)
}

func comparisonView(item db.Comparison) db.Comparison {
	if !item.IsBlind || item.Winner != nil {
		return item
	}
	var mapping struct{ Left, Right string }
	_ = json.Unmarshal([]byte(item.BlindMap), &mapping)
	if mapping.Left == "b" {
		item.ResponseA, item.ResponseB = item.ResponseB, item.ResponseA
	}
	item.ModelA, item.ModelB = "Model A", "Model B"
	item.BlindMap = ""
	return item
}
