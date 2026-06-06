package lifecycle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
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
	ready chan struct{}
}

type commandRunner func(ctx context.Context, name string, args ...string) error
type pullRunner func(ctx context.Context, model string, update func(string)) error

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
	pullCommand   pullRunner
	loadTimeout   time.Duration
	runTimeout    time.Duration
	unloadTimeout time.Duration
	pollInterval  time.Duration
	pullsMu       sync.RWMutex
	pulls         map[string]*PullJob
	tagsMu        sync.Mutex
	tagsCache     []ollamaTag
	tagsExpires   time.Time
}

// NewManager creates a lifecycle manager.
func NewManager(cfg *config.Manager) *Manager {
	return &Manager{
		cfg:           cfg,
		client:        &http.Client{},
		states:        map[string]*modelState{},
		command:       runCommand,
		pullCommand:   runPullCommand,
		loadTimeout:   120 * time.Second,
		runTimeout:    60 * time.Second,
		unloadTimeout: 15 * time.Second,
		pollInterval:  2 * time.Second,
		pulls:         map[string]*PullJob{},
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
			if entry.ready == nil {
				entry.ready = make(chan struct{})
			}
			ready := entry.ready
			m.mu.Unlock()
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for %s to load", model)
			}
			wait := time.Until(deadline)
			if wait <= 0 {
				return fmt.Errorf("timeout waiting for %s to load", model)
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-ready:
				timer.Stop()
				continue
			case <-timer.C:
				return fmt.Errorf("timeout waiting for %s to load", model)
			}
		default:
			entry.state = StateLoading
			entry.err = nil
			entry.ready = make(chan struct{})
			m.mu.Unlock()
			err := m.load(ctx, model)
			m.mu.Lock()
			entry = m.entryLocked(model)
			if err != nil {
				entry.state = StateUnloaded
				entry.err = err
				m.notifyReadyLocked(entry)
				m.mu.Unlock()
				return err
			}
			entry.state = StateLoaded
			entry.err = nil
			m.notifyReadyLocked(entry)
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
		m.notifyReadyLocked(entry)
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

// ForceUnload immediately unloads a model from VRAM.
func (m *Manager) ForceUnload(model string) error {
	m.mu.Lock()
	entry := m.entryLocked(model)
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	entry.state = StateUnloaded
	m.notifyReadyLocked(entry)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), m.unloadTimeout)
	defer cancel()
	return m.unloadWithContext(ctx, model)
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

// PullJob describes the current or latest Ollama model download state.
type PullJob struct {
	Model       string     `json:"model"`
	Status      string     `json:"status"`
	ProgressPct int        `json:"progress_pct"`
	Message     string     `json:"message"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// PullModel starts an Ollama model download if one is not already running.
func (m *Manager) PullModel(ctx context.Context, model string) (PullJob, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return PullJob{}, fmt.Errorf("model is required")
	}
	if !validOllamaModelName(model) {
		return PullJob{}, fmt.Errorf("model name is invalid")
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	installed := m.ollamaHasModel(checkCtx, model)
	cancel()
	if installed {
		now := time.Now().UTC()
		job := PullJob{Model: model, Status: "installed", ProgressPct: 100, Message: "Installed", StartedAt: now, CompletedAt: &now}
		m.setPullJob(job)
		return job, nil
	}

	m.pullsMu.Lock()
	if existing, ok := m.pulls[model]; ok && existing.Status == "downloading" {
		copy := *existing
		m.pullsMu.Unlock()
		return copy, nil
	}
	job := &PullJob{
		Model:     model,
		Status:    "downloading",
		Message:   "Starting download",
		StartedAt: time.Now().UTC(),
	}
	m.pulls[model] = job
	m.pullsMu.Unlock()

	go m.runPull(model)
	return *job, nil
}

// PullJobs returns snapshots for known download jobs.
func (m *Manager) PullJobs() []PullJob {
	m.pullsMu.RLock()
	defer m.pullsMu.RUnlock()
	jobs := make([]PullJob, 0, len(m.pulls))
	for _, job := range m.pulls {
		copy := *job
		jobs = append(jobs, copy)
	}
	return jobs
}

// PullJob returns a download job snapshot for model.
func (m *Manager) PullJob(model string) (PullJob, bool) {
	m.pullsMu.RLock()
	defer m.pullsMu.RUnlock()
	job, ok := m.pulls[model]
	if !ok {
		return PullJob{}, false
	}
	return *job, true
}

// OllamaInstalled reports whether the model is present in Ollama's local tag list.
func (m *Manager) OllamaInstalled(ctx context.Context, model string) bool {
	return m.ollamaHasModel(ctx, model)
}

// OllamaModels returns the installed Ollama model names visible through /api/tags.
func (m *Manager) OllamaModels(ctx context.Context) map[string]bool {
	return m.ollamaModels(ctx)
}

// OllamaModelInfo describes a model present in the local Ollama install.
type OllamaModelInfo struct {
	Name      string
	SizeBytes int64
}

// OllamaModelDetails returns the installed Ollama models with their on-disk size.
func (m *Manager) OllamaModelDetails(ctx context.Context) []OllamaModelInfo {
	tags, err := m.ollamaTags(ctx)
	if err != nil {
		return nil
	}
	out := make([]OllamaModelInfo, 0, len(tags))
	for _, item := range tags {
		out = append(out, OllamaModelInfo{Name: item.Name, SizeBytes: item.Size})
	}
	return out
}

func (m *Manager) entryLocked(model string) *modelState {
	entry, ok := m.states[model]
	if !ok {
		entry = &modelState{state: StateUnloaded}
		m.states[model] = entry
	}
	return entry
}

func (m *Manager) setPullJob(job PullJob) {
	m.pullsMu.Lock()
	defer m.pullsMu.Unlock()
	copy := job
	m.pulls[job.Model] = &copy
}

func (m *Manager) runPull(model string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	err := m.pullCommand(ctx, model, func(line string) {
		m.updatePull(model, line)
	})
	now := time.Now().UTC()
	m.pullsMu.Lock()
	defer m.pullsMu.Unlock()
	job := m.pulls[model]
	if job == nil {
		job = &PullJob{Model: model, StartedAt: now}
		m.pulls[model] = job
	}
	job.CompletedAt = &now
	if err != nil {
		job.Status = "failed"
		job.Error = "ollama pull failed"
		job.Message = "Download failed"
		return
	}
	job.Status = "installed"
	job.ProgressPct = 100
	job.Message = "Installed"
	job.Error = ""
}

func (m *Manager) updatePull(model, raw string) {
	message, pct := parsePullProgress(raw)
	if message == "" && pct < 0 {
		return
	}
	m.pullsMu.Lock()
	defer m.pullsMu.Unlock()
	job := m.pulls[model]
	if job == nil {
		return
	}
	if message != "" {
		job.Message = message
	}
	if pct >= 0 && pct > job.ProgressPct {
		job.ProgressPct = pct
	}
}

func (m *Manager) load(ctx context.Context, model string) error {
	loadCtx, cancel := context.WithTimeout(ctx, m.loadTimeout)
	defer cancel()
	// The first warm attempt is deliberately short so an unreachable Ollama
	// endpoint fails before the longer CLI fallback/load timeout is engaged.
	probeCtx, probeCancel := context.WithTimeout(loadCtx, 2*time.Second)
	probeErr := m.warmModel(probeCtx, model)
	probeCancel()
	if probeErr == nil {
		return nil
	}
	var networkErr net.Error
	if errors.As(probeErr, &networkErr) {
		return fmt.Errorf("ollama is unreachable: %w", probeErr)
	}
	if loadCtx.Err() != nil {
		return loadCtx.Err()
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
	defer drainAndClose(res.Body)
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
	m.notifyReadyLocked(entry)
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), m.unloadTimeout)
	defer cancel()
	if err := m.unloadWithContext(ctx, model); err != nil {
		log.Printf("aegis: failed to unload %s: %v", model, err)
	}
}

func (m *Manager) notifyReadyLocked(entry *modelState) {
	if entry.ready == nil {
		return
	}
	select {
	case <-entry.ready:
	default:
		close(entry.ready)
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
	defer drainAndClose(res.Body)
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
		if ollamaModelMatches(item.Name, model) || ollamaModelMatches(item.Model, model) {
			return true
		}
	}
	return false
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	prepareBackgroundCommand(cmd)
	return cmd.Run()
}

func runPullCommand(ctx context.Context, model string, update func(string)) error {
	cmd := exec.CommandContext(ctx, "ollama", "pull", model)
	prepareBackgroundCommand(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	readPipe := func(scanner *bufio.Scanner) {
		defer wg.Done()
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		scanner.Split(scanProgressLines)
		for scanner.Scan() {
			update(scanner.Text())
		}
	}
	wg.Add(2)
	go readPipe(bufio.NewScanner(stdout))
	go readPipe(bufio.NewScanner(stderr))
	waitErr := cmd.Wait()
	wg.Wait()
	return waitErr
}

func scanProgressLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, bytes.TrimSpace(data[:i]), nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), bytes.TrimSpace(data), nil
	}
	return 0, nil, nil
}

var (
	progressPercentRE = regexp.MustCompile(`\b(\d{1,3})%`)
	ansiRE            = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	modelNameRE       = regexp.MustCompile(`^[A-Za-z0-9_.:/-]+$`)
)

func parsePullProgress(raw string) (string, int) {
	line := strings.TrimSpace(ansiRE.ReplaceAllString(raw, ""))
	line = strings.Join(strings.Fields(line), " ")
	if line == "" {
		return "", -1
	}
	pct := -1
	if match := progressPercentRE.FindStringSubmatch(line); len(match) == 2 {
		if parsed, err := strconv.Atoi(match[1]); err == nil {
			if parsed > 100 {
				parsed = 100
			}
			pct = parsed
		}
	}
	return line, pct
}

func validOllamaModelName(model string) bool {
	return len(model) <= 160 && modelNameRE.MatchString(model)
}

func (m *Manager) ollamaHasModel(ctx context.Context, model string) bool {
	return m.ollamaModels(ctx)[model]
}

func (m *Manager) ollamaModels(ctx context.Context) map[string]bool {
	tags, err := m.ollamaTags(ctx)
	if err != nil {
		return map[string]bool{}
	}
	models := make(map[string]bool, len(tags))
	for _, item := range tags {
		models[item.Name] = true
		models[normalizeOllamaModelName(item.Name)] = true
	}
	return models
}

type ollamaTag struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (m *Manager) ollamaTags(ctx context.Context) ([]ollamaTag, error) {
	m.tagsMu.Lock()
	if time.Now().Before(m.tagsExpires) {
		cached := append([]ollamaTag(nil), m.tagsCache...)
		m.tagsMu.Unlock()
		return cached, nil
	}
	m.tagsMu.Unlock()

	baseURL := m.cfg.Get().Backend.OllamaBaseURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama tags failed with status %d", res.StatusCode)
	}
	var tags struct {
		Models []ollamaTag `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tags); err != nil {
		return nil, err
	}
	m.tagsMu.Lock()
	m.tagsCache = append([]ollamaTag(nil), tags.Models...)
	m.tagsExpires = time.Now().Add(30 * time.Second)
	m.tagsMu.Unlock()
	return append([]ollamaTag(nil), tags.Models...), nil
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func ollamaModelMatches(found, requested string) bool {
	return normalizeOllamaModelName(found) == normalizeOllamaModelName(requested)
}

func normalizeOllamaModelName(model string) string {
	return strings.TrimSuffix(strings.TrimSpace(model), ":latest")
}
