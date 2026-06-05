package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

const sessionTTL = 7 * 24 * time.Hour
const sessionCookieName = "aegis_session"

// Login handles POST /auth/login.
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required", "INVALID_REQUEST")
		return
	}

	user, err := s.DB.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "auth failed", "AUTH_STORE_ERROR", err)
		return
	}
	if user == nil || !auth.VerifyPassword(req.Password, user.Hash, user.Salt) {
		writeError(w, http.StatusUnauthorized, "invalid credentials", "INVALID_CREDENTIALS")
		return
	}
	if user.TOTPSecret != nil && *user.TOTPSecret != "" {
		if req.TOTPCode == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"totp_required": true,
			})
			return
		}
		if !auth.ValidateTOTP(*user.TOTPSecret, req.TOTPCode) {
			writeError(w, http.StatusUnauthorized, "invalid TOTP code", "INVALID_TOTP")
			return
		}
	}

	token, err := db.GenerateSessionToken()
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "session creation failed", "SESSION_CREATE_FAILED", err)
		return
	}
	now := time.Now().UTC()
	if err := s.DB.CreateSession(r.Context(), db.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: now.Add(sessionTTL),
		CreatedAt: now,
	}); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "session creation failed", "SESSION_CREATE_FAILED", err)
		return
	}
	_ = s.DB.UpdateUserLastLogin(r.Context(), user.ID, now)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
	})
}

// Logout handles POST /auth/logout.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		_ = s.DB.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /auth/me.
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	user, err := s.DB.GetUserByID(r.Context(), userID)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "user not found", "UNAUTHORIZED")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
	})
}

// TOTPSetup handles POST /auth/totp/setup.
func (s *Server) TOTPSetup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	user, err := s.DB.GetUserByID(r.Context(), userID)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "user not found", "UNAUTHORIZED")
		return
	}
	secret, qrURI, err := auth.GenerateTOTPSecret(user.Username, "Aegis Gateway")
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "totp generation failed", "TOTP_GENERATE_FAILED", err)
		return
	}
	if err := s.DB.SetUserTOTPPending(r.Context(), userID, secret); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "totp setup failed", "TOTP_SETUP_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": secret,
		"qr_uri": qrURI,
	})
}

// TOTPConfirm handles POST /auth/totp/confirm.
func (s *Server) TOTPConfirm(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_JSON")
		return
	}
	user, err := s.DB.GetUserByID(r.Context(), userID)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "user not found", "UNAUTHORIZED")
		return
	}
	if user.TOTPPending == nil || *user.TOTPPending == "" {
		writeError(w, http.StatusBadRequest, "no pending TOTP setup", "NO_TOTP_PENDING")
		return
	}
	if !auth.ValidateTOTP(*user.TOTPPending, req.Code) {
		writeError(w, http.StatusBadRequest, "invalid TOTP code", "INVALID_TOTP")
		return
	}
	if err := s.DB.ConfirmUserTOTP(r.Context(), userID); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "totp confirm failed", "TOTP_CONFIRM_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
}

// TOTPDisable handles DELETE /auth/totp.
func (s *Server) TOTPDisable(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	if err := s.DB.DisableUserTOTP(r.Context(), userID); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "totp disable failed", "TOTP_DISABLE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"disabled": true})
}
