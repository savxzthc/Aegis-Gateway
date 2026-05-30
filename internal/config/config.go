package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// Config contains every user-editable Aegis Gateway setting.
type Config struct {
	Server   ServerConfig   `toml:"server" json:"server"`
	Security SecurityConfig `toml:"security" json:"security"`
	Backend  BackendConfig  `toml:"backend" json:"backend"`
	Models   ModelsConfig   `toml:"models" json:"models"`
}

// ServerConfig contains listener and lifecycle settings.
type ServerConfig struct {
	Host                  string `toml:"host" json:"host"`
	Port                  int    `toml:"port" json:"port"`
	IdleTimeoutMinutes    int    `toml:"idle_timeout_minutes" json:"idle_timeout_minutes"`
	RequestTimeoutSeconds int    `toml:"request_timeout_seconds" json:"request_timeout_seconds"`
}

// SecurityConfig contains local authentication and rate-limit settings.
type SecurityConfig struct {
	RateLimitRPM    int  `toml:"rate_limit_rpm" json:"rate_limit_rpm"`
	AutoGenerateKey bool `toml:"auto_generate_key" json:"auto_generate_key"`
}

// BackendConfig contains backend connection settings.
type BackendConfig struct {
	DefaultType     string `toml:"default_type" json:"default_type"`
	OllamaBaseURL   string `toml:"ollama_base_url" json:"ollama_base_url"`
	LlamaCppBaseURL string `toml:"llamacpp_base_url" json:"llamacpp_base_url"`
}

// ModelsConfig contains the model registry.
type ModelsConfig struct {
	Registry map[string]ModelConfig `toml:"registry" json:"registry"`
}

// ModelConfig describes a known model.
type ModelConfig struct {
	VRAMGB      float64 `toml:"vram_gb" json:"vram_gb"`
	Backend     string  `toml:"backend" json:"backend"`
	Description string  `toml:"description" json:"description"`
}

// Manager provides concurrency-safe access to runtime configuration.
type Manager struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// EditablePatch contains settings the dashboard may update at runtime.
type EditablePatch struct {
	Port               *int    `json:"port"`
	IdleTimeoutMinutes *int    `json:"idle_timeout_minutes"`
	RateLimitRPM       *int    `json:"rate_limit_rpm"`
	OllamaBaseURL      *string `json:"ollama_base_url"`
}

// Defaults returns the built-in default configuration.
func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Host:                  "0.0.0.0",
			Port:                  9000,
			IdleTimeoutMinutes:    10,
			RequestTimeoutSeconds: 300,
		},
		Security: SecurityConfig{
			RateLimitRPM:    60,
			AutoGenerateKey: true,
		},
		Backend: BackendConfig{
			DefaultType:     "ollama",
			OllamaBaseURL:   "http://127.0.0.1:11434",
			LlamaCppBaseURL: "http://127.0.0.1:8080",
		},
		Models: ModelsConfig{
			Registry: map[string]ModelConfig{
				"llama3:8b": {
					VRAMGB:      5.5,
					Backend:     "ollama",
					Description: "Meta Llama 3 8B - fast, general purpose",
				},
				"deepseek-coder:6.7b": {
					VRAMGB:      4.2,
					Backend:     "ollama",
					Description: "DeepSeek Coder 6.7B - optimized for code generation",
				},
				"phi3:mini": {
					VRAMGB:      2.3,
					Backend:     "ollama",
					Description: "Phi-3 Mini - lightweight fallback for low VRAM",
				},
			},
		},
	}
}

// LoadManager loads TOML from path and returns a concurrency-safe manager.
func LoadManager(path string) (*Manager, error) {
	cfg := Defaults()
	missing := false
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		missing = true
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	if missing {
		if err := writeTOML(path, cfg); err != nil {
			return nil, err
		}
	}
	return &Manager{path: path, cfg: cfg}, nil
}

// Get returns a copy of the current configuration.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConfig(m.cfg)
}

// IdleTimeout returns the configured model idle timeout.
func (m *Manager) IdleTimeout() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Duration(m.cfg.Server.IdleTimeoutMinutes) * time.Minute
}

// RateLimitRPM returns the current per-key request limit.
func (m *Manager) RateLimitRPM() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.Security.RateLimitRPM
}

// RequestTimeout returns the configured request timeout.
func (m *Manager) RequestTimeout() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Duration(m.cfg.Server.RequestTimeoutSeconds) * time.Second
}

// SetRuntimeOllamaBaseURL updates the Ollama URL for this process without rewriting config.toml.
func (m *Manager) SetRuntimeOllamaBaseURL(baseURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := cloneConfig(m.cfg)
	next.Backend.OllamaBaseURL = strings.TrimRight(baseURL, "/")
	if err := Validate(&next); err != nil {
		return err
	}
	m.cfg = next
	return nil
}

// PatchEditable updates supported dashboard settings and writes config.toml.
func (m *Manager) PatchEditable(patch EditablePatch) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := cloneConfig(m.cfg)
	if patch.Port != nil {
		next.Server.Port = *patch.Port
	}
	if patch.IdleTimeoutMinutes != nil {
		next.Server.IdleTimeoutMinutes = *patch.IdleTimeoutMinutes
	}
	if patch.RateLimitRPM != nil {
		next.Security.RateLimitRPM = *patch.RateLimitRPM
	}
	if patch.OllamaBaseURL != nil {
		next.Backend.OllamaBaseURL = strings.TrimRight(*patch.OllamaBaseURL, "/")
	}
	if err := Validate(&next); err != nil {
		return Config{}, err
	}
	if err := writeTOML(m.path, next); err != nil {
		return Config{}, err
	}
	m.cfg = next
	return cloneConfig(m.cfg), nil
}

// SortedModels returns model names sorted by ascending name.
func SortedModels(registry map[string]ModelConfig) []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate normalizes and validates configuration.
func Validate(cfg *Config) error {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if cfg.Server.IdleTimeoutMinutes <= 0 {
		return fmt.Errorf("server.idle_timeout_minutes must be greater than zero")
	}
	if cfg.Server.RequestTimeoutSeconds == 0 {
		cfg.Server.RequestTimeoutSeconds = 300
	}
	if cfg.Server.RequestTimeoutSeconds < 30 {
		return fmt.Errorf("server.request_timeout_seconds must be at least 30")
	}
	if cfg.Security.RateLimitRPM < 0 {
		return fmt.Errorf("security.rate_limit_rpm must be zero or greater")
	}
	cfg.Backend.DefaultType = normalizeBackend(cfg.Backend.DefaultType)
	if cfg.Backend.DefaultType == "" {
		return fmt.Errorf("backend.default_type must be ollama or llamacpp")
	}
	cfg.Backend.OllamaBaseURL = strings.TrimRight(cfg.Backend.OllamaBaseURL, "/")
	cfg.Backend.LlamaCppBaseURL = strings.TrimRight(cfg.Backend.LlamaCppBaseURL, "/")
	if cfg.Backend.OllamaBaseURL == "" {
		return fmt.Errorf("backend.ollama_base_url is required")
	}
	if cfg.Backend.LlamaCppBaseURL == "" {
		return fmt.Errorf("backend.llamacpp_base_url is required")
	}
	if len(cfg.Models.Registry) == 0 {
		return fmt.Errorf("at least one model must be registered")
	}
	for name, model := range cfg.Models.Registry {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("model name cannot be empty")
		}
		if model.VRAMGB < 0 {
			return fmt.Errorf("model %s vram_gb must be zero or greater", name)
		}
		model.Backend = normalizeBackend(model.Backend)
		if model.Backend == "" {
			model.Backend = cfg.Backend.DefaultType
		}
		cfg.Models.Registry[name] = model
	}
	return nil
}

func normalizeBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default":
		return ""
	case "ollama":
		return "ollama"
	case "llamacpp", "llama.cpp", "llama-cpp":
		return "llamacpp"
	default:
		return ""
	}
}

func cloneConfig(cfg Config) Config {
	cfg.Models.Registry = cloneRegistry(cfg.Models.Registry)
	return cfg
}

func cloneRegistry(in map[string]ModelConfig) map[string]ModelConfig {
	out := make(map[string]ModelConfig, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func writeTOML(path string, cfg Config) error {
	return os.WriteFile(path, []byte(commentedTOML(cfg)), 0o644)
}

func commentedTOML(cfg Config) string {
	var b strings.Builder
	fmt.Fprintln(&b, "[server]")
	fmt.Fprintln(&b, "# The host and port Aegis listens on. 0.0.0.0 allows LAN access.")
	fmt.Fprintf(&b, "host = %s\n", strconv.Quote(cfg.Server.Host))
	fmt.Fprintf(&b, "port = %d\n", cfg.Server.Port)
	fmt.Fprintln(&b, "# Minutes of inactivity before a loaded model is unloaded from VRAM.")
	fmt.Fprintf(&b, "idle_timeout_minutes = %d\n\n", cfg.Server.IdleTimeoutMinutes)
	fmt.Fprintln(&b, "# Maximum seconds a model request may run before Aegis cancels it.")
	fmt.Fprintf(&b, "request_timeout_seconds = %d\n\n", cfg.Server.RequestTimeoutSeconds)

	fmt.Fprintln(&b, "[security]")
	fmt.Fprintln(&b, "# Maximum requests per minute per API key. 0 = unlimited.")
	fmt.Fprintf(&b, "rate_limit_rpm = %d\n", cfg.Security.RateLimitRPM)
	fmt.Fprintln(&b, "# Auto-generate a key on first boot if none exist.")
	fmt.Fprintf(&b, "auto_generate_key = %t\n\n", cfg.Security.AutoGenerateKey)

	fmt.Fprintln(&b, "[backend]")
	fmt.Fprintln(&b, "# Default backend type: \"ollama\" or \"llamacpp\"")
	fmt.Fprintf(&b, "default_type = %s\n", strconv.Quote(cfg.Backend.DefaultType))
	fmt.Fprintln(&b, "# Base URL of your Ollama instance.")
	fmt.Fprintf(&b, "ollama_base_url = %s\n", strconv.Quote(cfg.Backend.OllamaBaseURL))
	fmt.Fprintln(&b, "# Base URL of your llama.cpp server (if used).")
	fmt.Fprintf(&b, "llamacpp_base_url = %s\n\n", strconv.Quote(cfg.Backend.LlamaCppBaseURL))

	fmt.Fprintln(&b, "# Model registry. Add any model you want Aegis to know about.")
	fmt.Fprintln(&b, "# vram_gb is the approximate VRAM this model uses when loaded at default quant.")
	fmt.Fprintln(&b, "# backend overrides the default_type for this specific model.")
	fmt.Fprintln(&b, "[models.registry]")
	for _, name := range SortedModels(cfg.Models.Registry) {
		model := cfg.Models.Registry[name]
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "  [models.registry.%s]\n", strconv.Quote(name))
		fmt.Fprintf(&b, "  vram_gb = %.1f\n", model.VRAMGB)
		fmt.Fprintf(&b, "  backend = %s\n", strconv.Quote(model.Backend))
		fmt.Fprintf(&b, "  description = %s\n", strconv.Quote(model.Description))
	}
	return b.String()
}
