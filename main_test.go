package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/savxzthc/aegis-gateway/internal/config"
)

type readyStoreStub struct {
	err error
}

func (s readyStoreStub) HealthCheck(ctx context.Context) error {
	return s.err
}

func TestRootHandlerReadyzReportsReady(t *testing.T) {
	handler, err := rootHandler(http.NotFoundHandler(), readyStoreStub{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if res.Body.String() != "{\"status\":\"ready\"}\n" {
		t.Fatalf("unexpected body %s", res.Body.String())
	}
}

func TestRootHandlerReadyzReportsUnready(t *testing.T) {
	handler, err := rootHandler(http.NotFoundHandler(), readyStoreStub{err: errors.New("closed")})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
}

func TestRootHandlerHealthzMethodErrorIsJSON(t *testing.T) {
	handler, err := rootHandler(http.NotFoundHandler(), readyStoreStub{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", res.Header().Get("Content-Type"))
	}
	if res.Body.String() == "" || !strings.Contains(res.Body.String(), "METHOD_NOT_ALLOWED") {
		t.Fatalf("unexpected body %s", res.Body.String())
	}
}

func TestRootHandlerAddsVersionHeader(t *testing.T) {
	handler, err := rootHandler(http.NotFoundHandler(), readyStoreStub{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if got := res.Header().Get("X-Aegis-Version"); got != version {
		t.Fatalf("version header = %q", got)
	}
}

func TestOpenGatewayListenerMovesWhenPortIsOllama(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ollama := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Ollama is running."))
	}))
	ollama.Listener = occupied
	ollama.Start()
	defer ollama.Close()

	_, rawPort, err := net.SplitHostPort(strings.TrimPrefix(ollama.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.PatchEditable(config.EditablePatch{Port: &port}); err != nil {
		t.Fatal(err)
	}

	listener, info, err := openGatewayListener(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if strings.HasSuffix(info.URL, ":"+rawPort) {
		t.Fatalf("Aegis stayed on Ollama port: %#v", info)
	}
	if got := info.OllamaBaseURL; got != "http://127.0.0.1:"+rawPort {
		t.Fatalf("ollama discovery = %q", got)
	}
	if len(info.Warnings) == 0 {
		t.Fatal("expected a collision warning")
	}
}

func TestOpenGatewayListenerMovesWhenOllamaUsesConfiguredPort(t *testing.T) {
	port := freeTCPPort(t)
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.PatchEditable(config.EditablePatch{Port: &port}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRuntimeOllamaBaseURL("http://127.0.0.1:" + strconv.Itoa(port)); err != nil {
		t.Fatal(err)
	}

	listener, info, err := openGatewayListener(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if strings.HasSuffix(info.URL, ":"+strconv.Itoa(port)) {
		t.Fatalf("Aegis bound the configured Ollama port: %#v", info)
	}
	if len(info.Warnings) == 0 {
		t.Fatal("expected a same-port warning")
	}
}

func TestNormalizeOllamaHostAddsSchemeAndDefaultPort(t *testing.T) {
	got, err := normalizeOllamaHost("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:11434" {
		t.Fatalf("normalized host = %q", got)
	}
}

func TestNormalizeOllamaHostConvertsWildcardToLoopback(t *testing.T) {
	got, err := normalizeOllamaHost("0.0.0.0:9000")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:9000" {
		t.Fatalf("normalized host = %q", got)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected addr %T", listener.Addr())
	}
	return addr.Port
}
