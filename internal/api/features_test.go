package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
	"github.com/savxzthc/aegis-gateway/internal/lifecycle"
	"github.com/savxzthc/aegis-gateway/internal/uploads"
)

func TestNonAdminCannotCreateOrDeleteUsers(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raw, _ := auth.GenerateKey()
	salt, _ := auth.GenerateSalt()
	if err := store.CreateAPIKey(context.Background(), db.NewAPIKey{
		ID: "key_operator", Label: "operator", Salt: salt, Hash: auth.HashKey(raw, salt), KeyRole: "inference", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	limiter := auth.NewRateLimiter()
	defer limiter.Stop()
	middleware := auth.NewMiddleware(store, limiter, func() int { return 60 })
	defer middleware.Stop()
	handler := NewRouter(&Server{DB: store}, middleware.Handler)
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/users", `{"username":"blocked","password":"password"}`},
		{http.MethodDelete, "/v1/users/usr_missing", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+raw)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
}

func TestConversationRoutesCRUDAndExport(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := &Server{DB: store}

	createRes := httptest.NewRecorder()
	server.CreateConversation(createRes, httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(`{"title":"Test","model":"m"}`)))
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", createRes.Code, createRes.Body.String())
	}
	var created db.Conversation
	if err := json.NewDecoder(createRes.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	listRes := httptest.NewRecorder()
	server.ListConversations(listRes, httptest.NewRequest(http.MethodGet, "/v1/conversations", nil))
	if listRes.Code != http.StatusOK || !strings.Contains(listRes.Body.String(), created.ID) {
		t.Fatalf("list: %d %s", listRes.Code, listRes.Body.String())
	}

	patchRes := httptest.NewRecorder()
	server.PatchConversation(patchRes, requestWithParam(http.MethodPatch, "/v1/conversations/"+created.ID, "id", created.ID, `{"title":"Renamed"}`))
	if patchRes.Code != http.StatusOK || !strings.Contains(patchRes.Body.String(), "Renamed") {
		t.Fatalf("patch: %d %s", patchRes.Code, patchRes.Body.String())
	}

	exportRes := httptest.NewRecorder()
	exportReq := requestWithParam(http.MethodGet, "/v1/conversations/"+created.ID+"/export?format=markdown", "id", created.ID, "")
	server.ExportConversation(exportRes, exportReq)
	if exportRes.Code != http.StatusOK || !strings.Contains(exportRes.Body.String(), "title:") {
		t.Fatalf("export: %d %s", exportRes.Code, exportRes.Body.String())
	}

	deleteRes := httptest.NewRecorder()
	server.DeleteConversation(deleteRes, requestWithParam(http.MethodDelete, "/v1/conversations/"+created.ID, "id", created.ID, ""))
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", deleteRes.Code, deleteRes.Body.String())
	}
}

func TestUploadRoutesRoundTrip(t *testing.T) {
	store, err := uploads.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	manager, err := config.NewManagerFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Config: manager, Uploads: store}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "tiny.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0})
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	server.UploadFile(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.Code, res.Body.String())
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&uploaded)

	getRes := httptest.NewRecorder()
	server.GetUpload(getRes, requestWithParam(http.MethodGet, "/v1/uploads/"+uploaded.ID, "id", uploaded.ID, ""))
	if getRes.Code != http.StatusOK || getRes.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("get: %d %s", getRes.Code, getRes.Body.String())
	}
	deleteRes := httptest.NewRecorder()
	server.DeleteUpload(deleteRes, requestWithParam(http.MethodDelete, "/v1/uploads/"+uploaded.ID, "id", uploaded.ID, ""))
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", deleteRes.Code)
	}
}

func TestComparisonReadListVoteRoutes(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	item := db.Comparison{ID: "cmp_test", Prompt: "p", ModelA: "a", ModelB: "b", ResponseA: "A", ResponseB: "B", IsBlind: true, BlindMap: `{"left":"a","right":"b"}`, CreatedAt: now, CompletedAt: &now}
	if err := store.CreateComparison(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	server := &Server{DB: store}
	startRes := httptest.NewRecorder()
	server.StartComparison(startRes, httptest.NewRequest(http.MethodPost, "/v1/compare/start", strings.NewReader(`{"prompt":"p","model_a":"same","model_b":"same"}`)))
	if startRes.Code != http.StatusBadRequest {
		t.Fatalf("start validation: %d %s", startRes.Code, startRes.Body.String())
	}
	listRes := httptest.NewRecorder()
	server.ListComparisons(listRes, httptest.NewRequest(http.MethodGet, "/v1/compare", nil))
	if listRes.Code != http.StatusOK || !strings.Contains(listRes.Body.String(), "cmp_test") {
		t.Fatalf("list: %d %s", listRes.Code, listRes.Body.String())
	}
	getRes := httptest.NewRecorder()
	server.GetComparison(getRes, requestWithParam(http.MethodGet, "/v1/compare/cmp_test", "id", "cmp_test", ""))
	if getRes.Code != http.StatusOK || !strings.Contains(getRes.Body.String(), "Model A") {
		t.Fatalf("get: %d %s", getRes.Code, getRes.Body.String())
	}
	voteRes := httptest.NewRecorder()
	server.VoteComparison(voteRes, requestWithParam(http.MethodPost, "/v1/compare/cmp_test/vote", "id", "cmp_test", `{"winner":"a"}`))
	if voteRes.Code != http.StatusOK || !strings.Contains(voteRes.Body.String(), `"winner":"a"`) {
		t.Fatalf("vote: %d %s", voteRes.Code, voteRes.Body.String())
	}
}

func TestDiscoverModelsReturnsSanitizedShape(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"vision:test","size":1000000000,"digest":"secret"}]}`))
	}))
	defer ollama.Close()
	cfg := config.Defaults()
	cfg.Backend.OllamaBaseURL = ollama.URL
	manager, err := config.NewManagerFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Config: manager, Lifecycle: lifecycle.NewManager(manager)}
	res := httptest.NewRecorder()
	server.DiscoverModels(res, httptest.NewRequest(http.MethodGet, "/v1/models/discover", nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "vram_gb_estimate") || strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("discover: %d %s", res.Code, res.Body.String())
	}
}

func requestWithParam(method, target, key, value, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
}
