package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

const maxKeyLabelRunes = 80

// ListKeys handles GET /v1/keys.
func (s *Server) ListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.DB.ListAPIKeys(r.Context())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key query failed", "KEY_QUERY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, keysResponse{Data: keys})
}

// CreateKey handles POST /v1/keys.
func (s *Server) CreateKey(w http.ResponseWriter, r *http.Request) {
	var req createKeyRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body", "INVALID_JSON")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "Local key"
	}
	if len([]rune(label)) > maxKeyLabelRunes {
		writeError(w, http.StatusBadRequest, "label must be 80 characters or fewer", "INVALID_KEY_LABEL")
		return
	}
	raw, err := auth.GenerateKey()
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key generation failed", "KEY_GENERATION_FAILED", err)
		return
	}
	salt, err := auth.GenerateSalt()
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key generation failed", "KEY_GENERATION_FAILED", err)
		return
	}
	id, err := auth.GenerateID("key")
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key generation failed", "KEY_GENERATION_FAILED", err)
		return
	}
	now := time.Now().UTC()
	if err := s.DB.CreateAPIKey(r.Context(), db.NewAPIKey{
		ID:        id,
		Label:     label,
		Salt:      salt,
		Hash:      auth.HashKey(raw, salt),
		CreatedAt: now,
	}); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key create failed", "KEY_CREATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, createKeyResponse{
		ID:        id,
		Label:     label,
		CreatedAt: now,
		Key:       raw,
	})
}

// DeleteKey handles DELETE /v1/keys/{id}.
func (s *Server) DeleteKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, lastActive, err := s.DB.RevokeAPIKeyIfNotLast(r.Context(), id, time.Now().UTC())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key revoke failed", "KEY_REVOKE_FAILED", err)
		return
	}
	if lastActive {
		writeError(w, http.StatusConflict, "cannot revoke the final active API key", "LAST_KEY_REVOKE_DENIED")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "key not found", "KEY_NOT_FOUND")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type keysResponse struct {
	Data []db.APIKeyView `json:"data"`
}

type createKeyRequest struct {
	Label string `json:"label"`
}

type createKeyResponse struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	Key       string    `json:"key"`
}
