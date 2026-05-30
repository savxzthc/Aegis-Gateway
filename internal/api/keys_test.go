package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
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
	return &Server{Config: cfg, DB: store}
}

func requestWithKeyID(id string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/v1/keys/"+id, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}
