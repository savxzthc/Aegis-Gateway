package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/config"
)

// State is the lifecycle state of a model.
type State string

const (
	// StateUnloaded means a model is not holding VRAM.
	StateUnloaded State = "unloaded"
	// StateLoading means a load command is in progress.
	StateLoading State = "loading"
	// StateLoaded means a model is actively serving a request.
	StateLoaded State = "loaded"
	// StateIdle means a model is loaded but waiting for its idle timer.
	StateIdle State = "idle"
)

type modelState struct {
	state State
	timer *time.Timer
	err   error
}

type commandRunner func(ctx context.Context, name string, args ...string) error

type configProvider interface {
	Get() config.Config
	IdleTimeout() time.Duration
}

// Manager controls on-demand Ollama model loading and unloading.
type Manager struct {
	mu            sync.RWMutex
	cfg           configProvider
	client        *http.Client
	states        map[string]*modelState
	command       commandRunner
	loadTimeout   time.Duration
	runTimeout    time.Duration
	unloadTimeout time.Duration
	pollInterval  time.Duration
}

// NewManager creates a lifecycle manager.
func NewManager(cfg *config.Manager) *Manager {
	return &Manager{
		cfg:           cfg,
		client:        &http.Client{},
		states:        map[string]*modelState{},
		command:       runCommand,
		loadTimeout:   120 * time.Second,
		runTimeout:    60 * time.Second,
		unloadTimeout: 15 * time.Second,
		pollInterval:  2 * time.Second,
	}
}

// EnsureLoaded blocks until an Ollama model is loaded or the timeout expires.
func (m *Manager) EnsureLoaded(ctx context.Context, model string) error {
	deadline := time.Now().Add(m.loadTimeout)
	for {
		m.mu.Lock()
		entry := m.entryLocked(model)
		switch entry.state {
		case StateLoaded, StateIdle:
			if entry.timer != nil {
				entry.timer.Stop()
				entry.timer = nil
			}
			entry.state = StateLoaded
			m.mu.Unlock()
			return nil
		case StateLoading:
			m.mu.Unlock()
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for %s to load", model)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
				continue
			}
		default:
			entry.state = StateLoading
			entry.err = nil
			m.mu.Unlock()
			err := m.load(ctx, model)
			m.mu.Lock()
			entry = m.entryLocked(model)
			if err != nil {
				entry.state = StateUnloaded
				entry.err = err
				m.mu.Unlock()
				return err
			}
			entry.state = StateLoaded
			entry.err = nil
			m.mu.Unlock()
			return nil
		}
	}
}

// MarkIdle starts or resets a model idle unload timer.
func (m *Manager) MarkIdle(model string) {
	timeout := m.cfg.IdleTimeout()
	m.mu.Lock()
	entry := m.entryLocked(model)
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.state = StateIdle
	entry.timer = time.AfterFunc(timeout, func() {
		m.unload(model)
	})
	m.mu.Unlock()
}

// Shutdown stops idle timers and unloads all models Aegis knows are resident.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	models := make([]string, 0, len(m.states))
	for model, entry := range m.states {
		if entry.timer != nil {
			entry.timer.Stop()
			entry.timer = nil
		}
		if entry.state == StateLoaded || entry.state == StateIdle || entry.state == StateLoading {
			models = append(models, model)
		}
		entry.state = StateUnloaded
	}
	m.mu.Unlock()

	var errs []error
	for _, model := range models {
		if err := m.unloadWithContext(ctx, model); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", model, err))
		}
	}
	return errors.Join(errs...)
}

// Status returns the current model lifecycle state.
func (m *Manager) Status(model string) State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.states[model]
	if !ok {
		return StateUnloaded
	}
	return entry.state
}

// ActiveModel returns the first loaded or idle model name, if any.
func (m *Manager) ActiveModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for model, entry := range m.states {
		if entry.state == StateLoaded || entry.state == StateIdle {
			return model
		}
	}
	return ""
}

func (m *Manager) entryLocked(model string) *modelState {
	entry, ok := m.states[model]
	if !ok {
		entry = &modelState{state: StateUnloaded}
		m.states[model] = entry
	}
	return entry
}

func (m *Manager) load(ctx context.Context, model string) error {
	loadCtx, cancel := context.WithTimeout(ctx, m.loadTimeout)
	defer cancel()

	if err := m.warmModel(loadCtx, model); err == nil {
		return nil
	}
	if err := m.runOllama(loadCtx, model); err != nil {
		return err
	}

	return m.waitForModel(loadCtx, model)
}

func (m *Manager) warmModel(ctx context.Context, model string) error {
	baseURL := m.cfg.Get().Backend.OllamaBaseURL
	keepAlive := fmt.Sprintf("%dm", m.cfg.Get().Server.IdleTimeoutMinutes)
	body := struct {
		Model     string `json:"model"`
		Prompt    string `json:"prompt"`
		Stream    bool   `json:"stream"`
		KeepAlive string `json:"keep_alive"`
	}{
		Model:     model,
		Prompt:    "",
		Stream:    false,
		KeepAlive: keepAlive,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/generate", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("ollama warmup failed with status %d", res.StatusCode)
	}
	return nil
}

func (m *Manager) runOllama(ctx context.Context, model string) error {
	runCtx, cancel := context.WithTimeout(ctx, m.runTimeout)
	defer cancel()
	if err := m.command(runCtx, "ollama", "run", model, ""); err != nil {
		return fmt.Errorf("ollama run %s failed: %w", model, err)
	}
	return nil
}

func (m *Manager) waitForModel(ctx context.Context, model string) error {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	for {
		if m.ollamaModelRunning(ctx, model) || m.ollamaHasModel(ctx, model) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout loading %s", model)
		case <-ticker.C:
		}
	}
}

func (m *Manager) unload(model string) {
	m.mu.Lock()
	entry := m.entryLocked(model)
	entry.state = StateUnloaded
	entry.timer = nil
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), m.unloadTimeout)
	defer cancel()
	if err := m.unloadWithContext(ctx, model); err != nil {
		log.Printf("aegis: failed to unload %s: %v", model, err)
	}
}

func (m *Manager) unloadWithContext(ctx context.Context, model string) error {
	return m.command(ctx, "ollama", "stop", model)
}

func (m *Manager) ollamaModelRunning(ctx context.Context, model string) bool {
	baseURL := m.cfg.Get().Backend.OllamaBaseURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/ps", nil)
	if err != nil {
		return false
	}
	res, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false
	}
	var ps struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if json.NewDecoder(res.Body).Decode(&ps) != nil {
		return false
	}
	for _, item := range ps.Models {
		if item.Name == model || item.Model == model {
			return true
		}
	}
	return false
}

func runCommand(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func (m *Manager) ollamaHasModel(ctx context.Context, model string) bool {
	baseURL := m.cfg.Get().Backend.OllamaBaseURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	res, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.NewDecoder(res.Body).Decode(&tags) != nil {
		return false
	}
	for _, item := range tags.Models {
		if item.Name == model {
			return true
		}
	}
	return false
}
