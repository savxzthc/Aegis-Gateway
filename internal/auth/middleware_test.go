package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/db"
)

type authStoreStub struct {
	secrets              []db.APIKeySecret
	failures             int
	usedKeyID            string
	markUsedErr          error
	failuresSeen         []string
	ActiveKeySecretsFunc func(context.Context) ([]db.APIKeySecret, error)
}

func (s *authStoreStub) ActiveKeySecrets(ctx context.Context) ([]db.APIKeySecret, error) {
	if s.ActiveKeySecretsFunc != nil {
		return s.ActiveKeySecretsFunc(ctx)
	}
	return s.secrets, nil
}

func (s *authStoreStub) LogAuthFailure(ctx context.Context, ip string, at time.Time) error {
	s.failures++
	s.failuresSeen = append(s.failuresSeen, ip)
	return nil
}

func (s *authStoreStub) MarkKeyUsed(ctx context.Context, id string, at time.Time) error {
	s.usedKeyID = id
	return s.markUsedErr
}

func (s *authStoreStub) GetSession(ctx context.Context, token string) (*db.Session, error) {
	return nil, nil
}

func (s *authStoreStub) GetUserByID(ctx context.Context, id string) (*db.User, error) {
	return nil, nil
}

func TestMiddlewareAuthenticatesAndInjectsKeyID(t *testing.T) {
	key := "secret"
	salt := "salt"
	store := &authStoreStub{secrets: []db.APIKeySecret{{
		ID:   "key_1",
		Salt: salt,
		Hash: HashKey(key, salt),
	}}}
	mw := NewMiddleware(store, NewRateLimiter(), func() int { return 60 })
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := KeyIDFromContext(r.Context()); got != "key_1" {
			t.Fatalf("got key id %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if store.usedKeyID != "key_1" || store.failures != 0 {
		t.Fatalf("unexpected store state: %#v", store)
	}
}

func TestMatchKeyIDScansAllSecrets(t *testing.T) {
	key := "secret"
	secrets := []db.APIKeySecret{
		{ID: "key_1", Salt: "salt-1", Hash: HashKey("other", "salt-1")},
		{ID: "key_2", Salt: "salt-2", Hash: HashKey(key, "salt-2")},
		{ID: "key_3", Salt: "salt-3", Hash: HashKey("different", "salt-3")},
	}

	id, _ := matchKeyWithMeta(key, secrets)
	if id != "key_2" {
		t.Fatalf("got key id %q", id)
	}
}

func TestMiddlewareCachesValidatedBearerToken(t *testing.T) {
	key := "secret"
	salt := "salt"
	store := &authStoreStub{secrets: []db.APIKeySecret{{
		ID:   "key_1",
		Salt: salt,
		Hash: HashKey(key, salt),
	}}}
	mw := NewMiddleware(store, NewRateLimiter(), func() int { return 60 })
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	mw.nowFunc = func() time.Time { return now }
	lookups := 0
	store.ActiveKeySecretsFunc = func(ctx context.Context) ([]db.APIKeySecret, error) {
		lookups++
		return store.secrets, nil
	}
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusNoContent {
			t.Fatalf("request %d got %d", i, res.Code)
		}
	}
	if lookups != 1 {
		t.Fatalf("active key lookup count = %d", lookups)
	}
}

func TestTokenCachePrunesExpiredEntries(t *testing.T) {
	mw := NewMiddleware(&authStoreStub{}, NewRateLimiter(), func() int { return 60 })
	defer mw.Stop()
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	mw.cacheKeyID("expired", "key_old", db.APIKeySecret{}, now.Add(-time.Second))
	mw.cacheKeyID("fresh", "key_new", db.APIKeySecret{}, now.Add(time.Second))
	mw.cacheMu.Lock()
	mw.pruneTokenCacheLocked(now)
	count := len(mw.tokenCache)
	mw.cacheMu.Unlock()
	if count != 1 {
		t.Fatalf("cache count = %d, want 1", count)
	}
	if got, _, ok := mw.cachedKeyID("fresh", now); !ok || got != "key_new" {
		t.Fatalf("fresh cache entry missing: got=%q ok=%v", got, ok)
	}
}

func TestMiddlewareRejectsInvalidKeyWithoutLoggingSecret(t *testing.T) {
	store := &authStoreStub{secrets: []db.APIKeySecret{{
		ID:   "key_1",
		Salt: "salt",
		Hash: HashKey("right", "salt"),
	}}}
	mw := NewMiddleware(store, NewRateLimiter(), func() int { return 60 })
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer wrong")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d", res.Code)
	}
	if store.failures != 1 || store.failuresSeen[0] != "127.0.0.1" {
		t.Fatalf("unexpected failure logging: %#v", store)
	}
	if res.Body.String() == "" || strings.Contains(res.Body.String(), "wrong") {
		t.Fatalf("response leaked key material: %s", res.Body.String())
	}
}

func TestMiddlewareIgnoresForwardedForWhenLoggingFailures(t *testing.T) {
	store := &authStoreStub{secrets: []db.APIKeySecret{{
		ID:   "key_1",
		Salt: "salt",
		Hash: HashKey("right", "salt"),
	}}}
	mw := NewMiddleware(store, NewRateLimiter(), func() int { return 60 })
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("Authorization", "Bearer wrong")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d", res.Code)
	}
	if store.failuresSeen[0] != "127.0.0.1" {
		t.Fatalf("trusted forwarded IP: %#v", store.failuresSeen)
	}
}

func TestMiddlewareTrustsForwardedForFromConfiguredProxy(t *testing.T) {
	store := &authStoreStub{}
	mw := NewMiddleware(store, NewRateLimiter(), func() int { return 60 }, func() []string {
		return []string{"127.0.0.0/8"}
	})
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 127.0.0.1")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized || len(store.failuresSeen) != 1 || store.failuresSeen[0] != "203.0.113.7" {
		t.Fatalf("forwarded IP not trusted: status=%d failures=%#v", res.Code, store.failuresSeen)
	}
}

func TestMiddlewareRateLimitsAuthFailures(t *testing.T) {
	store := &authStoreStub{secrets: []db.APIKeySecret{{
		ID:   "key_1",
		Salt: "salt",
		Hash: HashKey("right", "salt"),
	}}}
	mw := NewMiddleware(store, NewRateLimiter(), func() int { return 1 })
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	mw.nowFunc = func() time.Time { return now }
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run")
	}))

	for i, want := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer wrong")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("request %d got %d want %d", i, res.Code, want)
		}
	}
	if store.failures != 1 {
		t.Fatalf("logged failures = %d, want 1", store.failures)
	}
}

func TestMiddlewareRateLimitsPerKeyAndIP(t *testing.T) {
	key := "secret"
	salt := "salt"
	store := &authStoreStub{secrets: []db.APIKeySecret{{
		ID:   "key_1",
		Salt: salt,
		Hash: HashKey(key, salt),
	}}}
	mw := NewMiddleware(store, NewRateLimiter(), func() int { return 1 })
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	mw.nowFunc = func() time.Time { return now }
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i, want := range []int{http.StatusNoContent, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+key)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("request %d got %d want %d", i, res.Code, want)
		}
	}
}
