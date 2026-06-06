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
	var keys []db.APIKeyView
	var err error
	if (auth.UserRoleFromContext(r.Context()) == "" && auth.KeyRoleFromContext(r.Context()) == "" && auth.KeyIDFromContext(r.Context()) == "") ||
		auth.UserRoleFromContext(r.Context()) == "admin" || auth.KeyRoleFromContext(r.Context()) == "admin" {
		keys, err = s.DB.ListAPIKeys(r.Context())
	} else if userID := auth.UserIDFromContext(r.Context()); userID != "" {
		keys, err = s.DB.ListAPIKeysByOwner(r.Context(), userID)
	} else {
		all, queryErr := s.DB.ListAPIKeys(r.Context())
		err = queryErr
		keyID := auth.KeyIDFromContext(r.Context())
		for _, key := range all {
			if key.ID == keyID {
				keys = append(keys, key)
				break
			}
		}
	}
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key query failed", "KEY_QUERY_FAILED", err)
		return
	}
	normalizeKeyViews(keys)
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
	models, err := s.validModelAllowlist(req.AllowedModels)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_MODEL_ACL")
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
	role := strings.TrimSpace(req.KeyRole)
	if role == "" {
		role = "inference"
	}
	if role != "inference" && role != "operator" && role != "admin" {
		writeError(w, http.StatusBadRequest, "key_role must be inference, operator, or admin", "INVALID_KEY_ROLE")
		return
	}
	if role != "inference" && auth.UserRoleFromContext(r.Context()) != "admin" && auth.KeyRoleFromContext(r.Context()) != "admin" {
		writeError(w, http.StatusForbidden, "admin role required to create privileged keys", "FORBIDDEN")
		return
	}
	if err := s.DB.CreateAPIKey(r.Context(), db.NewAPIKey{
		ID:        id,
		Label:     label,
		Salt:      salt,
		Hash:      auth.HashKey(raw, salt),
		KeyRole:   role,
		OwnerID:   auth.UserIDFromContext(r.Context()),
		CreatedAt: now,
	}); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key create failed", "KEY_CREATE_FAILED", err)
		return
	}
	if len(models) > 0 {
		if ok, err := s.DB.SetAPIKeyAllowedModels(r.Context(), id, models, now); err != nil || !ok {
			writePrivateError(w, r, http.StatusInternalServerError, "key ACL update failed", "KEY_ACL_UPDATE_FAILED", err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, createKeyResponse{
		ID:        id,
		Label:     label,
		CreatedAt: now,
		Key:       raw,
	})
}

// UpdateKeyModels handles PATCH /v1/keys/{id}/models.
func (s *Server) UpdateKeyModels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.canManageKey(r, id) {
		writeError(w, http.StatusNotFound, "key not found", "KEY_NOT_FOUND")
		return
	}
	var req updateKeyModelsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body", "INVALID_JSON")
		return
	}
	models, err := s.validModelAllowlist(req.AllowedModels)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_MODEL_ACL")
		return
	}
	ok, err := s.DB.SetAPIKeyAllowedModels(r.Context(), id, models, time.Now().UTC())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key ACL update failed", "KEY_ACL_UPDATE_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "key not found", "KEY_NOT_FOUND")
		return
	}
	keys, err := s.DB.ListAPIKeys(r.Context())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "key query failed", "KEY_QUERY_FAILED", err)
		return
	}
	normalizeKeyViews(keys)
	writeJSON(w, http.StatusOK, keysResponse{Data: keys})
}

// DeleteKey handles DELETE /v1/keys/{id}.
func (s *Server) DeleteKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.canManageKey(r, id) {
		writeError(w, http.StatusNotFound, "key not found", "KEY_NOT_FOUND")
		return
	}
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

// UpdateKeyRateLimit handles PATCH /v1/keys/{id}/rate-limit — admin only.
func (s *Server) UpdateKeyRateLimit(w http.ResponseWriter, r *http.Request) {
	if !requireRole(w, r, "admin") {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		RateLimitRPM int `json:"rate_limit_rpm"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	if req.RateLimitRPM < 0 {
		writeError(w, http.StatusBadRequest, "rate_limit_rpm must be zero or greater", "INVALID_REQUEST")
		return
	}
	ok, err := s.DB.UpdateKeyRateLimit(r.Context(), id, req.RateLimitRPM)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "rate limit update failed", "RATE_LIMIT_UPDATE_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "key not found", "KEY_NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "rate_limit_rpm": req.RateLimitRPM})
}

type keysResponse struct {
	Data []db.APIKeyView `json:"data"`
}

type createKeyRequest struct {
	Label         string   `json:"label"`
	AllowedModels []string `json:"allowed_models"`
	KeyRole       string   `json:"key_role"`
}

type updateKeyModelsRequest struct {
	AllowedModels []string `json:"allowed_models"`
}

type createKeyResponse struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	Key       string    `json:"key"`
}

func normalizeKeyViews(keys []db.APIKeyView) {
	for i := range keys {
		if keys[i].AllowedModels == nil {
			keys[i].AllowedModels = []string{}
		}
	}
}

func (s *Server) validModelAllowlist(models []string) ([]string, error) {
	registry := s.Config.Get().Models.Registry
	seen := map[string]bool{}
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		if _, ok := registry[model]; !ok {
			return nil, errUnknownACLModel(model)
		}
		seen[model] = true
		out = append(out, model)
	}
	return out, nil
}

func errUnknownACLModel(model string) error {
	return keyValidationError("model " + model + " is not registered")
}

func (s *Server) canManageKey(r *http.Request, id string) bool {
	if auth.UserRoleFromContext(r.Context()) == "" && auth.KeyRoleFromContext(r.Context()) == "" && auth.KeyIDFromContext(r.Context()) == "" {
		return true
	}
	if auth.UserRoleFromContext(r.Context()) == "admin" || auth.KeyRoleFromContext(r.Context()) == "admin" {
		return true
	}
	if auth.KeyIDFromContext(r.Context()) == id {
		return true
	}
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		return false
	}
	keys, err := s.DB.ListAPIKeysByOwner(r.Context(), userID)
	if err != nil {
		return false
	}
	for _, key := range keys {
		if key.ID == id {
			return true
		}
	}
	return false
}

type keyValidationError string

func (e keyValidationError) Error() string {
	return string(e)
}
