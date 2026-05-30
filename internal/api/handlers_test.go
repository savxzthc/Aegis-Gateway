package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/backends"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
	"github.com/savxzthc/aegis-gateway/internal/hardware"
	"github.com/savxzthc/aegis-gateway/internal/lifecycle"
	modelrouter "github.com/savxzthc/aegis-gateway/internal/router"
)

type fakeBackend struct {
	chatReq   *backends.ChatRequest
	chatErr   error
	streamErr error
}

func (f *fakeBackend) Chat(ctx context.Context, req *backends.ChatRequest, stream bool) (*backends.ChatResponse, error) {
	copyReq := *req
	f.chatReq = &copyReq
	if f.chatErr != nil {
		return nil, f.chatErr
	}
	return &backends.ChatResponse{
		Model:   req.Model,
		Content: "hello from test",
		Usage:   backends.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
	}, nil
}

func (f *fakeBackend) StreamChat(ctx context.Context, req *backends.ChatRequest, ch chan<- string) error {
	if f.streamErr != nil {
		return f.streamErr
	}
	ch <- "hello"
	ch <- " stream"
	return nil
}

func (f *fakeBackend) Ping(ctx context.Context) error {
	return nil
}

type apiHardwareStub struct {
	info hardware.Info
}

func (s apiHardwareStub) Query(ctx context.Context) (hardware.Info, error) {
	return s.info, nil
}

func TestChatCompletionsReturnsOpenAIShapeAndRoutingHeaders(t *testing.T) {
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("unexpected lifecycle path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"done":true}`))
	}))
	defer ollama.Close()
	if _, err := cfg.PatchEditable(config.EditablePatch{OllamaBaseURL: &ollama.URL}); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	backend := &fakeBackend{}
	hw := apiHardwareStub{info: hardware.Info{Detected: true, VRAMFreeGB: 8}}
	server := &Server{
		Config:           cfg,
		DB:               store,
		HardwareProvider: hw,
		VRAMRouter:       modelrouter.NewVRAMRouter(cfg, hw),
		Lifecycle:        lifecycle.NewManager(cfg),
		Backends:         map[string]backends.Backend{"ollama": backend},
		StartedAt:        time.Now().UTC(),
		Version:          "test",
		BuildTime:        "test",
		GoVersion:        "test",
	}

	body := []byte(`{"model":"llama3:8b","messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	res := httptest.NewRecorder()
	server.ChatCompletions(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("X-Aegis-Routed-Model"); got != "llama3:8b" {
		t.Fatalf("routed model header %q", got)
	}
	if got := res.Header().Get("X-Aegis-Fallback"); got != "false" {
		t.Fatalf("fallback header %q", got)
	}
	if backend.chatReq == nil || backend.chatReq.Model != "llama3:8b" {
		t.Fatalf("backend did not receive routed model: %#v", backend.chatReq)
	}

	var out chatCompletionResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Object != "chat.completion" || out.Choices[0].Message.Content.String() != "hello from test" {
		t.Fatalf("unexpected response: %#v", out)
	}
	if out.Usage.CompletionTokens != 3 {
		t.Fatalf("usage not preserved: %#v", out.Usage)
	}
}

func TestChatCompletionsStreamsOpenAIChunks(t *testing.T) {
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"done":true}`))
	}))
	defer ollama.Close()
	if _, err := cfg.PatchEditable(config.EditablePatch{OllamaBaseURL: &ollama.URL}); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	backend := &fakeBackend{}
	hw := apiHardwareStub{info: hardware.Info{Detected: true, VRAMFreeGB: 8}}
	server := &Server{
		Config:           cfg,
		DB:               store,
		HardwareProvider: hw,
		VRAMRouter:       modelrouter.NewVRAMRouter(cfg, hw),
		Lifecycle:        lifecycle.NewManager(cfg),
		Backends:         map[string]backends.Backend{"ollama": backend},
		StartedAt:        time.Now().UTC(),
		Version:          "test",
		BuildTime:        "test",
		GoVersion:        "test",
	}

	body := []byte(`{"model":"llama3:8b","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	res := httptest.NewRecorder()
	server.ChatCompletions(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q", got)
	}
	out := res.Body.String()
	for _, want := range []string{
		`data: {"id":"chatcmpl_`,
		`"role":"assistant"`,
		`"content":"hello"`,
		`"content":" stream"`,
		`"finish_reason":"stop"`,
		"data: [DONE]\n\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stream missing %q in %s", want, out)
		}
	}
}

func TestChatCompletionsSanitizesBackendErrors(t *testing.T) {
	server := newTestCompletionServer(t, &fakeBackend{
		chatErr: errors.New("backend leaked prompt: my private prompt"),
	})

	body := []byte(`{"model":"llama3:8b","messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	res := httptest.NewRecorder()
	server.ChatCompletions(res, req)

	if res.Code != http.StatusBadGateway {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	var out errorResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Code != "BACKEND_ERROR" || out.Error != "backend request failed" {
		t.Fatalf("unexpected error body: %#v", out)
	}
	if strings.Contains(res.Body.String(), "private prompt") {
		t.Fatalf("backend detail leaked to client: %s", res.Body.String())
	}
}

func TestChatCompletionsSanitizesStreamErrors(t *testing.T) {
	server := newTestCompletionServer(t, &fakeBackend{
		streamErr: errors.New("backend leaked prompt: my private prompt"),
	})

	body := []byte(`{"model":"llama3:8b","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	res := httptest.NewRecorder()
	server.ChatCompletions(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	out := res.Body.String()
	if !strings.Contains(out, `"code":"BACKEND_STREAM_ERROR"`) || !strings.Contains(out, `"backend stream failed"`) {
		t.Fatalf("missing sanitized stream error in %s", out)
	}
	if strings.Contains(out, "private prompt") {
		t.Fatalf("backend detail leaked to stream: %s", out)
	}
}

func newTestCompletionServer(t *testing.T, backend backends.Backend) *Server {
	t.Helper()
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"done":true}`))
	}))
	t.Cleanup(ollama.Close)
	if _, err := cfg.PatchEditable(config.EditablePatch{OllamaBaseURL: &ollama.URL}); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(filepath.Join(t.TempDir(), "aegis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	hw := apiHardwareStub{info: hardware.Info{Detected: true, VRAMFreeGB: 8}}
	return &Server{
		Config:           cfg,
		DB:               store,
		HardwareProvider: hw,
		VRAMRouter:       modelrouter.NewVRAMRouter(cfg, hw),
		Lifecycle:        lifecycle.NewManager(cfg),
		Backends:         map[string]backends.Backend{"ollama": backend},
		StartedAt:        time.Now().UTC(),
		Version:          "test",
		BuildTime:        "test",
		GoVersion:        "test",
	}
}
