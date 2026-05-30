package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/config"
)

func TestEnsureLoadedWarmsModelWithoutShellingOut(t *testing.T) {
	cfg := lifecycleConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Model     string `json:"model"`
			KeepAlive string `json:"keep_alive"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "llama3:8b" || req.KeepAlive != "10m" {
			t.Fatalf("unexpected warm request: %#v", req)
		}
		_, _ = w.Write([]byte(`{"done":true}`))
	}))
	defer server.Close()
	setOllamaBaseURL(t, cfg, server.URL)

	manager := NewManager(cfg)
	manager.command = func(ctx context.Context, name string, args ...string) error {
		t.Fatalf("command should not run when warmup succeeds: %s %v", name, args)
		return nil
	}

	if err := manager.EnsureLoaded(context.Background(), "llama3:8b"); err != nil {
		t.Fatal(err)
	}
	if state := manager.Status("llama3:8b"); state != StateLoaded {
		t.Fatalf("got state %s", state)
	}
}

func TestEnsureLoadedFallsBackToCommandAndPoll(t *testing.T) {
	cfg := lifecycleConfig(t)
	var tagsCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/generate":
			http.Error(w, "warmup failed", http.StatusServiceUnavailable)
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/tags":
			tagsCalls++
			_, _ = w.Write([]byte(`{"models":[{"name":"phi3:mini"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	setOllamaBaseURL(t, cfg, server.URL)

	var commands []string
	manager := NewManager(cfg)
	manager.pollInterval = time.Millisecond
	manager.command = func(ctx context.Context, name string, args ...string) error {
		commands = append(commands, fmt.Sprintf("%s %v", name, args))
		return nil
	}

	if err := manager.EnsureLoaded(context.Background(), "phi3:mini"); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "ollama [run phi3:mini ]" {
		t.Fatalf("unexpected commands: %#v", commands)
	}
	if tagsCalls == 0 {
		t.Fatal("expected /api/tags polling")
	}
}

func TestMarkIdleUnloadsAfterTimeout(t *testing.T) {
	cfg := lifecycleConfig(t)
	idle := 1
	if _, err := cfg.PatchEditable(config.EditablePatch{IdleTimeoutMinutes: &idle}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(cfg)
	manager.entryLockedForTest("llama3:8b").state = StateLoaded

	var mu sync.Mutex
	var commands []string
	manager.command = func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		commands = append(commands, fmt.Sprintf("%s %v", name, args))
		return nil
	}
	manager.cfg = fixedIdleConfig{Manager: cfg, idle: 5 * time.Millisecond}
	manager.MarkIdle("llama3:8b")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(commands)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(commands) != 1 || commands[0] != "ollama [stop llama3:8b]" {
		t.Fatalf("unexpected unload commands: %#v", commands)
	}
	if state := manager.Status("llama3:8b"); state != StateUnloaded {
		t.Fatalf("got state %s", state)
	}
}

func TestShutdownUnloadsResidentModels(t *testing.T) {
	cfg := lifecycleConfig(t)
	manager := NewManager(cfg)
	manager.entryLockedForTest("loaded").state = StateLoaded
	idle := manager.entryLockedForTest("idle")
	idle.state = StateIdle
	idle.timer = time.AfterFunc(time.Hour, func() {
		t.Error("idle timer should have been stopped")
	})
	manager.entryLockedForTest("loading").state = StateLoading
	manager.entryLockedForTest("unloaded").state = StateUnloaded

	var mu sync.Mutex
	var commands []string
	manager.command = func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		commands = append(commands, fmt.Sprintf("%s %v", name, args))
		return nil
	}

	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := map[string]bool{
		"ollama [stop loaded]":  false,
		"ollama [stop idle]":    false,
		"ollama [stop loading]": false,
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %#v", commands)
	}
	for _, command := range commands {
		if _, ok := want[command]; !ok {
			t.Fatalf("unexpected command %s in %#v", command, commands)
		}
		want[command] = true
	}
	for command, seen := range want {
		if !seen {
			t.Fatalf("missing command %s in %#v", command, commands)
		}
	}
	for _, model := range []string{"loaded", "idle", "loading", "unloaded"} {
		if state := manager.Status(model); state != StateUnloaded {
			t.Fatalf("%s state = %s", model, state)
		}
	}
}

func TestPullModelTracksProgressAndCompletion(t *testing.T) {
	cfg := lifecycleConfig(t)
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer ollama.Close()
	setOllamaBaseURL(t, cfg, ollama.URL)

	manager := NewManager(cfg)
	manager.pullCommand = func(ctx context.Context, model string, update func(string)) error {
		if model != "phi3:mini" {
			t.Fatalf("model = %s", model)
		}
		update("pulling manifest")
		update("pulling layer 42%")
		return nil
	}

	job, err := manager.PullModel(context.Background(), "phi3:mini")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "downloading" {
		t.Fatalf("initial status = %#v", job)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, _ = manager.PullJob("phi3:mini")
		if job.Status == "installed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if job.Status != "installed" || job.ProgressPct != 100 {
		t.Fatalf("final job = %#v", job)
	}
}

func TestPullModelRejectsInvalidName(t *testing.T) {
	cfg := lifecycleConfig(t)
	manager := NewManager(cfg)
	if _, err := manager.PullModel(context.Background(), "bad model; rm -rf"); err == nil {
		t.Fatal("expected invalid model name error")
	}
}

func TestParsePullProgress(t *testing.T) {
	message, pct := parsePullProgress("\x1b[?25lpulling layer 73%")
	if message != "pulling layer 73%" || pct != 73 {
		t.Fatalf("message=%q pct=%d", message, pct)
	}
}

type fixedIdleConfig struct {
	*config.Manager
	idle time.Duration
}

func (f fixedIdleConfig) IdleTimeout() time.Duration {
	return f.idle
}

func (m *Manager) entryLockedForTest(model string) *modelState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entryLocked(model)
}

func lifecycleConfig(t *testing.T) *config.Manager {
	t.Helper()
	cfg, err := config.LoadManager(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func setOllamaBaseURL(t *testing.T, cfg *config.Manager, url string) {
	t.Helper()
	if _, err := cfg.PatchEditable(config.EditablePatch{OllamaBaseURL: &url}); err != nil {
		t.Fatal(err)
	}
}
