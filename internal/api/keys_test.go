package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
	"github.com/savxzthc/aegis-gateway/internal/lifecycle"
)

func TestCreateKeyRejectsLongLabel(t *testing.T) {
	server := keysTestServer(t)
	body := []byte(`{"label":"` + strings.Repeat("x", maxKeyLabelRunes+1) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/keys", bytes.NewReader(body))
	res := httptest.NewRecorder()

	server.CreateKey(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "INVALID_KEY_LABEL") {
		t.Fatalf("unexpected body %s", res.Body.String())
	}
}

func TestDeleteKeyRejectsLastActiveKey(t *testing.T) {
	server := keysTestServer(t)
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if err := server.DB.CreateAPIKey(context.Background(), db.NewAPIKey{
		ID:        "key_only",
		Label:     "Only",
		Salt:      "salt",
		Hash:      "hash",
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	req := requestWithKeyID("key_only")
	res := httptest.NewRecorder()
	server.DeleteKey(res, req)

	if res.Code != http.StatusConflict {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	count, err := server.DB.CountActiveKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d active keys, want 1", count)
	}
}

func TestDeleteKeyRevokesWhenAnotherKeyExists(t *testing.T) {
	server := keysTestServer(t)
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"key_one", "key_two"} {
		if err := server.DB.CreateAPIKey(context.Background(), db.NewAPIKey{
			ID:        id,
			Label:     id,
			Salt:      "salt",
			Hash:      "hash",
			CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := requestWithKeyID("key_one")
	res := httptest.NewRecorder()
	server.DeleteKey(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	count, err := server.DB.CountActiveKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d active keys, want 1", count)
	}
}

func TestEmptyListResponsesUseJSONArrays(t *testing.T) {
	server := keysTestServer(t)

	templateRes := httptest.NewRecorder()
	server.ListTemplates(templateRes, httptest.NewRequest(http.MethodGet, "/v1/templates", nil))
	assertJSONRaw(t, templateRes, "data", "[]")

	logRes := httptest.NewRecorder()
	server.Logs(logRes, httptest.NewRequest(http.MethodGet, "/v1/logs", nil))
	assertJSONRaw(t, logRes, "data", "[]")

	statsRes := httptest.NewRecorder()
	server.Stats(statsRes, httptest.NewRequest(http.MethodGet, "/v1/stats", nil))
	assertJSONRaw(t, statsRes, "top_models", "[]")

	if err := server.DB.CreateAPIKey(context.Background(), db.NewAPIKey{
		ID:        "key_no_acl",
		Label:     "No ACL",
		Salt:      "salt",
		Hash:      "hash",
		CreatedAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	keyRes := httptest.NewRecorder()
	server.ListKeys(keyRes, httptest.NewRequest(http.MethodGet, "/v1/keys", nil))
	if !strings.Contains(keyRes.Body.String(), `"allowed_models":[]`) {
		t.Fatalf("allowed models was not an array: %s", keyRes.Body.String())
	}
}

func assertJSONRaw(t *testing.T, res *httptest.ResponseRecorder, field, want string) {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := string(body[field]); got != want {
		t.Fatalf("%s = %s, want %s in body %s", field, got, want, res.Body.String())
	}
}

func keysTestServer(t *testing.T) *Server {
	t.Helper()
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return &Server{Config: cfg, DB: store, Lifecycle: lifecycle.NewManager(cfg)}
}

func requestWithKeyID(id string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/v1/keys/"+id, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}
