package auth

import (
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

const keyIDContextKey contextKey = "aegis.key_id"

const (
	tokenCacheTTL      = 10 * time.Second
	tokenCacheMaxItems = 4096
)

// Store captures database methods required by auth middleware.
type Store interface {
	ActiveKeySecrets(ctx context.Context) ([]db.APIKeySecret, error)
	LogAuthFailure(ctx context.Context, ip string, at time.Time) error
	MarkKeyUsed(ctx context.Context, id string, at time.Time) error
}

// Middleware authenticates Bearer API keys and applies rate limits.
type Middleware struct {
	store      Store
	limiter    *RateLimiter
	rpmFunc    func() int
	nowFunc    func() time.Time
	cacheMu    sync.RWMutex
	tokenCache map[string]cachedToken
	stopCache  chan struct{}
}

type cachedToken struct {
	keyID     string
	expiresAt time.Time
}

// NewMiddleware creates chi-compatible auth middleware.
func NewMiddleware(store Store, limiter *RateLimiter, rpmFunc func() int) *Middleware {
	middleware := &Middleware{
		store:      store,
		limiter:    limiter,
		rpmFunc:    rpmFunc,
		nowFunc:    func() time.Time { return time.Now().UTC() },
		tokenCache: map[string]cachedToken{},
		stopCache:  make(chan struct{}),
	}
	go middleware.pruneTokenCacheLoop(time.Minute)
	return middleware
}

// Stop stops background cache maintenance for tests or controlled shutdown.
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
		ip := clientIP(r)
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			m.unauthorized(w, r, ip)
			return
		}

		now := m.nowFunc()
		keyID, ok := m.cachedKeyID(token, now)
		if !ok {
			secrets, err := m.store.ActiveKeySecrets(r.Context())
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "auth store unavailable", "AUTH_STORE_ERROR")
				return
			}
			keyID = matchKeyID(token, secrets)
			if keyID != "" {
				m.cacheKeyID(token, keyID, now.Add(tokenCacheTTL))
			}
		}
		if keyID == "" {
			m.unauthorized(w, r, ip)
			return
		}

		limitKey := ip + ":" + keyID
		if ok, retryAfter := m.limiter.Allow(limitKey, m.rpmFunc(), now); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded", "RATE_LIMITED")
			return
		}
		if err := m.store.MarkKeyUsed(r.Context(), keyID, now); err != nil {
			writeAuthError(w, http.StatusInternalServerError, "auth store unavailable", "AUTH_STORE_ERROR")
			return
		}
		ctx := context.WithValue(r.Context(), keyIDContextKey, keyID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func matchKeyID(token string, secrets []db.APIKeySecret) string {
	for _, secret := range secrets {
		candidate := HashKey(token, secret.Salt)
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(secret.Hash)) == 1 {
			return secret.ID
		}
	}
	return ""
}

func (m *Middleware) cachedKeyID(token string, now time.Time) (string, bool) {
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
		return "", false
	}
	return cached.keyID, true
}

func (m *Middleware) cacheKeyID(token, keyID string, expiresAt time.Time) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if len(m.tokenCache) >= tokenCacheMaxItems {
		m.pruneTokenCacheLocked(time.Now().UTC())
	}
	if len(m.tokenCache) >= tokenCacheMaxItems {
		m.tokenCache = map[string]cachedToken{}
	}
	m.tokenCache[tokenCacheKey(token)] = cachedToken{keyID: keyID, expiresAt: expiresAt}
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
