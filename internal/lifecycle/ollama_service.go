package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// OllamaService starts and stops a local Ollama HTTP service owned by Aegis.
type OllamaService struct {
	mu      sync.Mutex
	baseURL string
	client  *http.Client
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	started bool
	done    chan error
}

// NewOllamaService creates a supervisor for a configured Ollama base URL.
func NewOllamaService(baseURL string) *OllamaService {
	return &OllamaService{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: time.Second},
	}
}

// Ensure starts ollama serve for local configurations when the API is offline.
func (s *OllamaService) Ensure(ctx context.Context) error {
	if !isLocalOllamaBaseURL(s.baseURL) {
		return nil
	}
	if s.ping(ctx) {
		return nil
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return s.waitReady(ctx)
	}
	path, err := exec.LookPath("ollama")
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("ollama executable not found on PATH")
	}
	serveCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(serveCtx, path, "serve")
	cmd.Env = ollamaServeEnv(os.Environ(), s.baseURL)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Stdin = nil
	prepareBackgroundCommand(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		s.mu.Unlock()
		return fmt.Errorf("start ollama serve: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	s.cmd = cmd
	s.cancel = cancel
	s.started = true
	s.done = done
	s.mu.Unlock()

	if err := s.waitReady(ctx); err != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = s.Shutdown(shutdownCtx)
		return err
	}
	return nil
}

// Shutdown terminates a local Ollama process started by Aegis.
func (s *OllamaService) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.started || s.cmd == nil {
		s.mu.Unlock()
		return nil
	}
	cmd := s.cmd
	cancel := s.cancel
	done := s.done
	s.cmd = nil
	s.cancel = nil
	s.done = nil
	s.started = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case err := <-done:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *OllamaService) waitReady(ctx context.Context) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		if s.ping(ctx) {
			return nil
		}
		s.mu.Lock()
		done := s.done
		s.mu.Unlock()
		select {
		case err := <-done:
			if err == nil {
				return fmt.Errorf("ollama serve exited before becoming ready")
			}
			return fmt.Errorf("ollama serve exited before becoming ready: %w", err)
		case <-ticker.C:
		case <-deadline.C:
			return fmt.Errorf("ollama serve did not become ready at %s", s.baseURL)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *OllamaService) ping(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	res, err := s.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode >= 200 && res.StatusCode < 300
}

func isLocalOllamaBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ollamaServeEnv(base []string, baseURL string) []string {
	hostPort := ollamaHostPort(baseURL)
	if hostPort == "" {
		return base
	}
	out := make([]string, 0, len(base)+1)
	for _, item := range base {
		if !strings.HasPrefix(strings.ToUpper(item), "OLLAMA_HOST=") {
			out = append(out, item)
		}
	}
	return append(out, "OLLAMA_HOST="+hostPort)
}

func ollamaHostPort(baseURL string) string {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		port = "11434"
	}
	return net.JoinHostPort(host, port)
}
