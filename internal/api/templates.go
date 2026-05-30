package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

const (
	maxTemplateNameRunes   = 80
	maxTemplatePromptRunes = 20000
)

// ListTemplates handles GET /v1/templates.
func (s *Server) ListTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.DB.ListPromptTemplates(r.Context())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "template query failed", "TEMPLATE_QUERY_FAILED", err)
		return
	}
	if templates == nil {
		templates = []db.PromptTemplate{}
	}
	writeJSON(w, http.StatusOK, templatesResponse{Data: templates})
}

// CreateTemplate handles POST /v1/templates.
func (s *Server) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req templateRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body", "INVALID_JSON")
		return
	}
	template, err := s.templateFromRequest("", req, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_TEMPLATE")
		return
	}
	id, err := auth.GenerateID("tpl")
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "template create failed", "TEMPLATE_CREATE_FAILED", err)
		return
	}
	template.ID = id
	template.CreatedAt = template.UpdatedAt
	if err := s.DB.CreatePromptTemplate(r.Context(), template); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "template create failed", "TEMPLATE_CREATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, template)
}

// UpdateTemplate handles PATCH /v1/templates/{id}.
func (s *Server) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, ok, err := s.DB.PromptTemplateByID(r.Context(), id)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "template query failed", "TEMPLATE_QUERY_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "template not found", "TEMPLATE_NOT_FOUND")
		return
	}
	var req templateRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body", "INVALID_JSON")
		return
	}
	template, err := s.templateFromRequest(id, req, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_TEMPLATE")
		return
	}
	template.CreatedAt = existing.CreatedAt
	ok, err = s.DB.UpdatePromptTemplate(r.Context(), template)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "template update failed", "TEMPLATE_UPDATE_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "template not found", "TEMPLATE_NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, template)
}

// DeleteTemplate handles DELETE /v1/templates/{id}.
func (s *Server) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	ok, err := s.DB.DeletePromptTemplate(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "template delete failed", "TEMPLATE_DELETE_FAILED", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "template not found", "TEMPLATE_NOT_FOUND")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type templatesResponse struct {
	Data []db.PromptTemplate `json:"data"`
}

type templateRequest struct {
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	Prompt       string `json:"prompt"`
	Model        string `json:"model"`
}

func (s *Server) templateFromRequest(id string, req templateRequest, now time.Time) (db.PromptTemplate, error) {
	name := strings.TrimSpace(req.Name)
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	prompt := strings.TrimSpace(req.Prompt)
	model := strings.TrimSpace(req.Model)
	if name == "" {
		return db.PromptTemplate{}, templateValidationError("name is required")
	}
	if len([]rune(name)) > maxTemplateNameRunes {
		return db.PromptTemplate{}, templateValidationError("name must be 80 characters or fewer")
	}
	if prompt == "" {
		return db.PromptTemplate{}, templateValidationError("prompt is required")
	}
	if len([]rune(prompt))+len([]rune(systemPrompt)) > maxTemplatePromptRunes {
		return db.PromptTemplate{}, templateValidationError("template text is too large")
	}
	if model != "" {
		if _, ok := s.Config.Get().Models.Registry[model]; !ok {
			return db.PromptTemplate{}, templateValidationError("model is not registered")
		}
	}
	return db.PromptTemplate{
		ID:           id,
		Name:         name,
		SystemPrompt: systemPrompt,
		Prompt:       prompt,
		Model:        model,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

type templateValidationError string

func (e templateValidationError) Error() string {
	return string(e)
}
