package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

// ListUsers handles GET /v1/users — admin only.
func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireRole(w, r, "admin") {
		return
	}
	users, err := s.DB.ListUsers(r.Context())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "user query failed", "USER_QUERY_FAILED", err)
		return
	}
	if users == nil {
		users = []db.UserView{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": users})
}

// CreateUser handles POST /v1/users — admin only.
func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	if !requireRole(w, r, "admin") {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required", "INVALID_REQUEST")
		return
	}
	if !validUserRole(req.Role) {
		req.Role = "operator"
	}
	hash, salt, err := auth.HashPassword(req.Password)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "password hashing failed", "PASSWORD_HASH_FAILED", err)
		return
	}
	id, err := auth.GenerateID("usr")
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "id generation failed", "ID_GENERATE_FAILED", err)
		return
	}
	user := db.User{
		ID:        id,
		Username:  req.Username,
		Hash:      hash,
		Salt:      salt,
		Role:      req.Role,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.DB.CreateUser(r.Context(), user); err != nil {
		if err == db.ErrDuplicateUsername {
			writeError(w, http.StatusConflict, "username already exists", "DUPLICATE_USERNAME")
			return
		}
		writePrivateError(w, r, http.StatusInternalServerError, "user creation failed", "USER_CREATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         id,
		"username":   req.Username,
		"role":       req.Role,
		"created_at": user.CreatedAt,
	})
}

// DeleteUser handles DELETE /v1/users/{id} — admin only, cannot delete self.
func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireRole(w, r, "admin") {
		return
	}
	targetID := chi.URLParam(r, "id")
	callerID := auth.UserIDFromContext(r.Context())
	if targetID == callerID {
		writeError(w, http.StatusConflict, "cannot delete your own account", "SELF_DELETE_DENIED")
		return
	}
	if err := s.DB.DeleteUser(r.Context(), targetID); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "user deletion failed", "USER_DELETE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateUserRole handles PATCH /v1/users/{id}/role — admin only, cannot demote last admin.
func (s *Server) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	if !requireRole(w, r, "admin") {
		return
	}
	targetID := chi.URLParam(r, "id")
	var req struct {
		Role string `json:"role"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	if !validUserRole(req.Role) {
		writeError(w, http.StatusBadRequest, "role must be admin, operator, or viewer", "INVALID_ROLE")
		return
	}
	if req.Role != "admin" {
		user, err := s.DB.GetUserByID(r.Context(), targetID)
		if err != nil || user == nil {
			writeError(w, http.StatusNotFound, "user not found", "USER_NOT_FOUND")
			return
		}
		if user.Role == "admin" {
			count, err := s.DB.CountAdmins(r.Context())
			if err != nil {
				writePrivateError(w, r, http.StatusInternalServerError, "admin count failed", "ADMIN_COUNT_FAILED", err)
				return
			}
			if count <= 1 {
				writeError(w, http.StatusConflict, "cannot demote the last admin", "LAST_ADMIN_DEMOTION_DENIED")
				return
			}
		}
	}
	if err := s.DB.UpdateUserRole(r.Context(), targetID, req.Role); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "role update failed", "ROLE_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": targetID, "role": req.Role})
}

func validUserRole(role string) bool {
	switch role {
	case "admin", "operator", "viewer":
		return true
	}
	return false
}

// requireRole checks that the caller has at minimum the given role, writes 403 if not.
func requireRole(w http.ResponseWriter, r *http.Request, required string) bool {
	role := auth.UserRoleFromContext(r.Context())
	if role == "" {
		role = auth.KeyRoleFromContext(r.Context())
		if role == "admin" {
			return true
		}
		writeError(w, http.StatusForbidden, "insufficient privileges", "FORBIDDEN")
		return false
	}
	switch required {
	case "admin":
		if role != "admin" {
			writeError(w, http.StatusForbidden, "admin role required", "FORBIDDEN")
			return false
		}
	case "operator":
		if role != "admin" && role != "operator" {
			writeError(w, http.StatusForbidden, "operator role required", "FORBIDDEN")
			return false
		}
	}
	return true
}
