package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/db"
)

type contextKey string

const keyIDContextKey contextKey = "aegis.key_id"

// Store captures database methods required by auth middleware.
type Store interface {
	ActiveKeySecrets(ctx context.Context) ([]db.APIKeySecret, error)
	LogAuthFailure(ctx context.Context, ip string, at time.Time) error
	MarkKeyUsed(ctx context.Context, id string, at time.Time) error
}

// Middleware authenticates Bearer API keys and applies rate limits.
type Middleware struct {
	store   Store
	limiter *RateLimiter
	rpmFunc func() int
	nowFunc func() time.Time
}

// NewMiddleware creates chi-compatible auth middleware.
func NewMiddleware(store Store, limiter *RateLimiter, rpmFunc func() int) *Middleware {
	return &Middleware{
		store:   store,
		limiter: limiter,
		rpmFunc: rpmFunc,
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

// Handler wraps an HTTP handler with Bearer auth and rate limiting.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			m.unauthorized(w, r, ip)
			return
		}

		secrets, err := m.store.ActiveKeySecrets(r.Context())
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "auth store unavailable", "AUTH_STORE_ERROR")
			return
		}
		keyID, _ := matchKeyID(token, secrets)
		if keyID == "" {
			m.unauthorized(w, r, ip)
			return
		}

		limitKey := ip + ":" + keyID
		if ok, retryAfter := m.limiter.Allow(limitKey, m.rpmFunc(), m.nowFunc()); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded", "RATE_LIMITED")
			return
		}
		if err := m.store.MarkKeyUsed(r.Context(), keyID, m.nowFunc()); err != nil {
			writeAuthError(w, http.StatusInternalServerError, "auth store unavailable", "AUTH_STORE_ERROR")
			return
		}
		ctx := context.WithValue(r.Context(), keyIDContextKey, keyID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func matchKeyID(token string, secrets []db.APIKeySecret) (string, int) {
	keyID := ""
	comparisons := 0
	for _, secret := range secrets {
		candidate := HashKey(token, secret.Salt)
		comparisons++
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(secret.Hash)) == 1 {
			keyID = secret.ID
		}
	}
	return keyID, comparisons
}

// KeyIDFromContext extracts the authenticated API key ID.
func KeyIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(keyIDContextKey).(string); ok {
		return value
	}
	return ""
}

func (m *Middleware) unauthorized(w http.ResponseWriter, r *http.Request, ip string) {
	_ = m.store.LogAuthFailure(r.Context(), ip, m.nowFunc())
	writeAuthError(w, http.StatusUnauthorized, "unauthorized", "UNAUTHORIZED")
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeAuthError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{Error: message, Code: code})
}
