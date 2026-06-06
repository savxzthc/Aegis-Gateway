package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/db"
)

type contextKey string

const (
	keyIDContextKey    contextKey = "aegis.key_id"
	userIDContextKey   contextKey = "aegis.user_id"
	userRoleContextKey contextKey = "aegis.user_role"
	keyRoleContextKey  contextKey = "aegis.key_role"
	keyMetaContextKey  contextKey = "aegis.key_meta"
	keyOwnerContextKey contextKey = "aegis.key_owner"
)

const (
	// Keep revocation lag small while avoiding a full password-hash scan on
	// every request.
	tokenCacheTTL      = 3 * time.Second
	tokenCacheMaxItems = 4096
	authFailureRPM     = 30
	sessionCookieName  = "aegis_session"
)

// Store captures database methods required by auth middleware.
type Store interface {
	ActiveKeySecrets(ctx context.Context) ([]db.APIKeySecret, error)
	LogAuthFailure(ctx context.Context, ip string, at time.Time) error
	MarkKeyUsed(ctx context.Context, id string, at time.Time) error
	GetSession(ctx context.Context, token string) (*db.Session, error)
	GetUserByID(ctx context.Context, id string) (*db.User, error)
}

// Middleware authenticates Bearer API keys and session cookies.
type Middleware struct {
	store          Store
	limiter        *RateLimiter
	rpmFunc        func() int
	nowFunc        func() time.Time
	cacheMu        sync.RWMutex
	tokenCache     map[string]cachedToken
	stopCache      chan struct{}
	trustedProxies func() []string
}

type cachedToken struct {
	keyID     string
	keyMeta   db.APIKeySecret
	expiresAt time.Time
}

// NewMiddleware creates chi-compatible auth middleware.
func NewMiddleware(store Store, limiter *RateLimiter, rpmFunc func() int, trustedProxies ...func() []string) *Middleware {
	middleware := &Middleware{
		store:      store,
		limiter:    limiter,
		rpmFunc:    rpmFunc,
		nowFunc:    func() time.Time { return time.Now().UTC() },
		tokenCache: map[string]cachedToken{},
		stopCache:  make(chan struct{}),
	}
	if len(trustedProxies) > 0 {
		middleware.trustedProxies = trustedProxies[0]
	}
	go middleware.pruneTokenCacheLoop(time.Minute)
	return middleware
}

// Stop stops background cache maintenance.
func (m *Middleware) Stop() {
	select {
	case <-m.stopCache:
	default:
		close(m.stopCache)
	}
}

// Handler wraps an HTTP handler with Bearer auth and rate limiting.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := m.clientIP(r)
		now := m.nowFunc()

		bearerTok := bearerToken(r.Header.Get("Authorization"))
		if bearerTok != "" {
			m.handleBearerAuth(w, r, bearerTok, ip, now, next)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && cookie.Value != "" {
			m.handleSessionAuth(w, r, cookie.Value, ip, now, next)
			return
		}

		m.unauthorized(w, r, ip)
	})
}

func (m *Middleware) handleBearerAuth(w http.ResponseWriter, r *http.Request, token, ip string, now time.Time, next http.Handler) {
	keyID, meta, ok := m.cachedKeyID(token, now)
	if !ok {
		secrets, err := m.store.ActiveKeySecrets(r.Context())
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "auth store unavailable", "AUTH_STORE_ERROR")
			return
		}
		keyID, meta = matchKeyWithMeta(token, secrets)
		if keyID != "" {
			m.cacheKeyID(token, keyID, meta, now.Add(tokenCacheTTL))
		}
	}
	if keyID == "" {
		m.unauthorized(w, r, ip)
		return
	}

	limitKey := ip + ":" + keyID
	effectiveRPM := meta.RateLimitRPM
	if effectiveRPM <= 0 {
		effectiveRPM = m.rpmFunc()
	}
	if ok, retryAfter := m.limiter.Allow(limitKey, effectiveRPM, now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded", "RATE_LIMITED")
		return
	}
	if err := m.store.MarkKeyUsed(r.Context(), keyID, now); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "auth store unavailable", "AUTH_STORE_ERROR")
		return
	}

	if meta.MaxPromptTokens > 0 {
		r = r.WithContext(context.WithValue(r.Context(), keyMetaContextKey, meta))
	}

	ctx := context.WithValue(r.Context(), keyIDContextKey, keyID)
	ctx = context.WithValue(ctx, keyRoleContextKey, meta.KeyRole)
	ctx = context.WithValue(ctx, keyOwnerContextKey, meta.OwnerID)
	next.ServeHTTP(w, r.WithContext(ctx))
}

func (m *Middleware) handleSessionAuth(w http.ResponseWriter, r *http.Request, token, ip string, now time.Time, next http.Handler) {
	sess, err := m.store.GetSession(r.Context(), token)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "auth store unavailable", "AUTH_STORE_ERROR")
		return
	}
	if sess == nil {
		m.unauthorized(w, r, ip)
		return
	}
	user, err := m.store.GetUserByID(r.Context(), sess.UserID)
	if err != nil || user == nil {
		m.unauthorized(w, r, ip)
		return
	}
	ctx := context.WithValue(r.Context(), userIDContextKey, user.ID)
	ctx = context.WithValue(ctx, userRoleContextKey, user.Role)
	ctx = context.WithValue(ctx, keyRoleContextKey, "admin")
	next.ServeHTTP(w, r.WithContext(ctx))
}

func matchKeyWithMeta(token string, secrets []db.APIKeySecret) (string, db.APIKeySecret) {
	for _, secret := range secrets {
		candidate := HashKey(token, secret.Salt)
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(secret.Hash)) == 1 {
			return secret.ID, secret
		}
	}
	return "", db.APIKeySecret{}
}

func (m *Middleware) cachedKeyID(token string, now time.Time) (string, db.APIKeySecret, bool) {
	cacheKey := tokenCacheKey(token)
	m.cacheMu.RLock()
	cached, ok := m.tokenCache[cacheKey]
	m.cacheMu.RUnlock()
	if !ok || !now.Before(cached.expiresAt) {
		if ok {
			m.cacheMu.Lock()
			delete(m.tokenCache, cacheKey)
			m.cacheMu.Unlock()
		}
		return "", db.APIKeySecret{}, false
	}
	return cached.keyID, cached.keyMeta, true
}

func (m *Middleware) cacheKeyID(token, keyID string, meta db.APIKeySecret, expiresAt time.Time) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if len(m.tokenCache) >= tokenCacheMaxItems {
		m.pruneTokenCacheLocked(time.Now().UTC())
	}
	if len(m.tokenCache) >= tokenCacheMaxItems {
		m.tokenCache = map[string]cachedToken{}
	}
	m.tokenCache[tokenCacheKey(token)] = cachedToken{keyID: keyID, keyMeta: meta, expiresAt: expiresAt}
}

func (m *Middleware) pruneTokenCacheLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.cacheMu.Lock()
			m.pruneTokenCacheLocked(time.Now().UTC())
			m.cacheMu.Unlock()
		case <-m.stopCache:
			return
		}
	}
}

func (m *Middleware) pruneTokenCacheLocked(now time.Time) {
	for key, cached := range m.tokenCache {
		if !now.Before(cached.expiresAt) {
			delete(m.tokenCache, key)
		}
	}
}

func tokenCacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// KeyIDFromContext extracts the authenticated API key ID.
func KeyIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(keyIDContextKey).(string); ok {
		return value
	}
	return ""
}

// UserIDFromContext extracts the authenticated user ID (session auth).
func UserIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(userIDContextKey).(string); ok {
		return value
	}
	return ""
}

// UserRoleFromContext extracts the authenticated user role.
func UserRoleFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(userRoleContextKey).(string); ok {
		return value
	}
	return ""
}

// KeyRoleFromContext extracts the API key role from context.
func KeyRoleFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(keyRoleContextKey).(string); ok {
		return value
	}
	return ""
}

// KeyMetaFromContext returns the full key metadata if present.
func KeyMetaFromContext(ctx context.Context) (db.APIKeySecret, bool) {
	meta, ok := ctx.Value(keyMetaContextKey).(db.APIKeySecret)
	return meta, ok
}

// KeyOwnerIDFromContext returns the API key owner, when assigned.
func KeyOwnerIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(keyOwnerContextKey).(string); ok {
		return value
	}
	return ""
}

func (m *Middleware) unauthorized(w http.ResponseWriter, r *http.Request, ip string) {
	now := m.nowFunc()
	if ok, retryAfter := m.limiter.Allow("auth-failure:"+ip, authFailureLimit(m.rpmFunc()), now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		writeAuthError(w, http.StatusTooManyRequests, "too many authentication failures", "AUTH_FAILURE_RATE_LIMITED")
		return
	}
	_ = m.store.LogAuthFailure(r.Context(), ip, now)
	writeAuthError(w, http.StatusUnauthorized, "unauthorized", "UNAUTHORIZED")
}

func authFailureLimit(configured int) int {
	if configured <= 0 || configured > authFailureRPM {
		return authFailureRPM
	}
	return configured
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

func (m *Middleware) clientIP(r *http.Request) string {
	direct := clientIP(r)
	if m.trustedProxies == nil || !ipInCIDRs(direct, m.trustedProxies()) {
		return direct
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
		return forwarded
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(realIP) != nil {
		return realIP
	}
	return direct
}

func ipInCIDRs(value string, cidrs []string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func writeAuthError(w http.ResponseWriter, status int, message, code string) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{Error: message, Code: code})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
