package lifecycle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaServiceEnsureReusesReachableServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	service := NewOllamaService(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if service.started {
		t.Fatal("reachable Ollama server should be reused, not started")
	}
}

func TestOllamaServiceSkipsRemoteBaseURL(t *testing.T) {
	service := NewOllamaService("http://192.0.2.10:11434")
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := service.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestOllamaServeEnvReplacesExistingHost(t *testing.T) {
	env := ollamaServeEnv([]string{"PATH=C:\\Tools", "OLLAMA_HOST=127.0.0.1:9999"}, "http://127.0.0.1:11434")
	got := strings.Join(env, "\n")
	if strings.Contains(got, "OLLAMA_HOST=127.0.0.1:9999") {
		t.Fatalf("old host was retained: %#v", env)
	}
	if !strings.Contains(got, "OLLAMA_HOST=127.0.0.1:11434") {
		t.Fatalf("new host missing: %#v", env)
	}
}

func TestOllamaHostPortDefaultsPort(t *testing.T) {
	if got := ollamaHostPort("http://localhost"); got != "localhost:11434" {
		t.Fatalf("host port = %q", got)
	}
}
