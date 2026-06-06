package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

func requestOwnerID(r *http.Request) string {
	if userID := auth.UserIDFromContext(r.Context()); userID != "" {
		return userID
	}
	if ownerID := auth.KeyOwnerIDFromContext(r.Context()); ownerID != "" {
		return ownerID
	}
	return auth.KeyIDFromContext(r.Context())
}

func (s *Server) ListConversations(w http.ResponseWriter, r *http.Request) {
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 200 {
		limit = 200
	}
	items, total, err := s.DB.ListConversations(r.Context(), requestOwnerID(r), limit, offset)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "conversation query failed", "CONVERSATION_QUERY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": total})
}

func (s *Server) CreateConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Model string `json:"model"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	item, err := s.newConversation(r, req.Title, req.Model)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "conversation creation failed", "CONVERSATION_CREATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) newConversation(r *http.Request, title, model string) (db.Conversation, error) {
	id, err := auth.GenerateID("con")
	if err != nil {
		return db.Conversation{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New conversation"
	}
	if len([]rune(title)) > 160 {
		title = string([]rune(title)[:160])
	}
	now := time.Now().UTC()
	item := db.Conversation{ID: id, OwnerID: requestOwnerID(r), Title: title, Model: strings.TrimSpace(model), CreatedAt: now, UpdatedAt: now}
	return item, s.DB.CreateConversation(r.Context(), item)
}

func (s *Server) GetConversation(w http.ResponseWriter, r *http.Request) {
	item, ok, err := s.DB.ConversationByID(r.Context(), chi.URLParam(r, "id"), requestOwnerID(r))
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "conversation query failed", "CONVERSATION_QUERY_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found", "CONVERSATION_NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) PatchConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title *string `json:"title"`
		Model *string `json:"model"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	if req.Title == nil && req.Model == nil {
		writeError(w, http.StatusBadRequest, "title or model is required", "INVALID_REQUEST")
		return
	}
	if req.Title != nil {
		value := strings.TrimSpace(*req.Title)
		if value == "" || len([]rune(value)) > 160 {
			writeError(w, http.StatusBadRequest, "title must be between 1 and 160 characters", "INVALID_REQUEST")
			return
		}
		req.Title = &value
	}
	ok, err := s.DB.PatchConversation(r.Context(), chi.URLParam(r, "id"), requestOwnerID(r), req.Title, req.Model, time.Now().UTC())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "conversation update failed", "CONVERSATION_UPDATE_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found", "CONVERSATION_NOT_FOUND")
		return
	}
	s.GetConversation(w, r)
}

func (s *Server) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	ok, err := s.DB.DeleteConversation(r.Context(), chi.URLParam(r, "id"), requestOwnerID(r))
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "conversation delete failed", "CONVERSATION_DELETE_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found", "CONVERSATION_NOT_FOUND")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ExportConversation(w http.ResponseWriter, r *http.Request) {
	item, ok, err := s.DB.ConversationByID(r.Context(), chi.URLParam(r, "id"), requestOwnerID(r))
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "conversation export failed", "CONVERSATION_EXPORT_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "conversation not found", "CONVERSATION_NOT_FOUND")
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" || format == "json" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, item.ID+".json"))
		writeJSON(w, http.StatusOK, item)
		return
	}
	if format != "markdown" {
		writeError(w, http.StatusBadRequest, "format must be markdown or json", "INVALID_EXPORT_FORMAT")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: %s\ntitle: %s\nmodel: %s\ndate: %s\n---\n", item.ID, strconv.Quote(item.Title), strconv.Quote(item.Model), item.CreatedAt.Format(time.RFC3339))
	for _, message := range item.Messages {
		role := message.Role
		if role != "" {
			role = strings.ToUpper(role[:1]) + role[1:]
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", role, message.Content)
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, item.ID+".md"))
	_, _ = w.Write([]byte(b.String()))
}
