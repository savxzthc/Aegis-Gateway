package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestLocalCORSMiddlewareAllowsLoopbackPreflight(t *testing.T) {
	handler := NewRouter(&Server{}, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("auth middleware should not run for allowed preflight")
		})
	})
	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := res.Header().Get("Access-Control-Expose-Headers"); got != "X-Aegis-Routed-Model, X-Aegis-Fallback, X-Aegis-Conversation-ID, X-Aegis-Search-Used, X-Aegis-TPS, X-Request-Id, X-Aegis-Version" {
		t.Fatalf("expose headers = %q", got)
	}
}

func TestAuthRouteAllowsPrivateNetworkPreflight(t *testing.T) {
	handler := NewRouter(&Server{}, func(next http.Handler) http.Handler {
		return next
	})
	req := httptest.NewRequest(http.MethodOptions, "/auth/login", nil)
	req.Header.Set("Origin", "http://192.168.1.25:9000")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://192.168.1.25:9000" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestRouterAddsRequestAndVersionHeaders(t *testing.T) {
	handler := NewRouter(&Server{Version: "test-version"}, func(next http.Handler) http.Handler {
		return next
	})
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	req.Header.Set("X-Request-Id", "req_test")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("X-Request-Id"); got != "req_test" {
		t.Fatalf("request id header = %q", got)
	}
	if got := res.Header().Get("X-Aegis-Version"); got != "test-version" {
		t.Fatalf("version header = %q", got)
	}
	var body errorResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "NOT_FOUND" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestRecoverJSONReturnsConsistentError(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)

	handler := middleware.RequestID(responseMetadata("test-version")(recoverJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("secret panic detail")
	}))))
	req := httptest.NewRequest(http.MethodGet, "/v1/panic", nil)
	req.Header.Set("X-Request-Id", "req_panic")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("X-Request-Id"); got != "req_panic" {
		t.Fatalf("request id header = %q", got)
	}
	if got := res.Header().Get("X-Aegis-Version"); got != "test-version" {
		t.Fatalf("version header = %q", got)
	}
	var body errorResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "INTERNAL_ERROR" || body.Error != "internal server error" {
		t.Fatalf("unexpected body: %#v", body)
	}
	if strings.Contains(res.Body.String(), "secret panic detail") {
		t.Fatalf("panic detail leaked to client: %s", res.Body.String())
	}
	if !strings.Contains(logs.String(), "req_panic") || !strings.Contains(logs.String(), "secret panic detail") {
		t.Fatalf("panic log missing request context: %s", logs.String())
	}
}

func TestRecoverJSONPreservesFlusher(t *testing.T) {
	flusherAvailable := false
	handler := recoverJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flusherAvailable = w.(http.Flusher)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/stream", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if !flusherAvailable {
		t.Fatal("recover middleware hid http.Flusher from streaming handlers")
	}
}

func TestLocalCORSMiddlewareRejectsNonLocalOrigin(t *testing.T) {
	handler := localCORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run for disallowed origin")
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Origin", "https://example.com")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
}

func TestIsLocalOrigin(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:3000",
		"https://localhost",
		"http://127.0.0.1:5173",
		"http://[::1]:5173",
		"http://192.168.1.25:5173",
		"http://10.0.0.25:5173",
		"http://172.16.0.25:5173",
		"https://[fd00::25]:8443",
	} {
		if !isLocalOrigin(origin) {
			t.Fatalf("%s should be local", origin)
		}
	}
	for _, origin := range []string{
		"https://example.com",
		"file://local",
		"not a url",
	} {
		if isLocalOrigin(origin) {
			t.Fatalf("%s should not be local", origin)
		}
	}
}
