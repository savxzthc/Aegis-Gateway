package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/api"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/backends"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
	"github.com/savxzthc/aegis-gateway/internal/hardware"
	"github.com/savxzthc/aegis-gateway/internal/lifecycle"
	modelrouter "github.com/savxzthc/aegis-gateway/internal/router"
)

//go:embed frontend/dist
var frontend embed.FS

var (
	version   = "dev"
	buildTime = "development"
)

var probeHTTPClient = &http.Client{Timeout: 3 * time.Second}

func main() {
	cfg, err := config.LoadManager("config.toml")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := applyOllamaHostOverride(cfg); err != nil {
		log.Fatalf("ollama host: %v", err)
	}
	listener, listenInfo, err := openGatewayListener(cfg)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	defer listener.Close()
	if listenInfo.OllamaBaseURL != "" {
		if err := cfg.SetRuntimeOllamaBaseURL(listenInfo.OllamaBaseURL); err != nil {
			log.Fatalf("ollama host: %v", err)
		}
	}
	if bindsAllInterfaces(cfg.Get().Server.Host) {
		listenInfo.Warnings = append(listenInfo.Warnings, "Aegis is bound to all network interfaces; API keys may be sent over your LAN in plaintext unless you use a local-only host.")
	}
	store, err := db.Open("aegis.db")
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.PruneOldMetadata(ctx, time.Now().UTC()); err != nil {
		log.Fatalf("metadata retention: %v", err)
	}
	if err := ensureFirstKey(ctx, cfg, store); err != nil {
		log.Fatalf("first key: %v", err)
	}

	gpu := hardware.NewNVIDIAProvider()
	lifecycleManager := lifecycle.NewManager(cfg)
	ollamaService := lifecycle.NewOllamaService(cfg.Get().Backend.OllamaBaseURL)
	if usesOllamaBackend(cfg.Get()) {
		ollamaCtx, ollamaCancel := context.WithTimeout(context.Background(), 12*time.Second)
		if err := ollamaService.Ensure(ollamaCtx); err != nil {
			listenInfo.Warnings = append(listenInfo.Warnings, fmt.Sprintf("Ollama could not be started automatically: %v", err))
		}
		ollamaCancel()
	}
	app := &api.Server{
		Config:           cfg,
		DB:               store,
		HardwareProvider: gpu,
		VRAMRouter:       modelrouter.NewVRAMRouter(cfg, gpu),
		Lifecycle:        lifecycleManager,
		Backends: map[string]backends.Backend{
			"ollama":   backends.NewOllamaBackend(cfg.Get().Backend.OllamaBaseURL),
			"llamacpp": backends.NewLlamaCppBackend(cfg.Get().Backend.LlamaCppBaseURL),
		},
		StartedAt: time.Now().UTC(),
		Version:   version,
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
	}

	rateLimiter := auth.NewRateLimiter()
	authMiddleware := auth.NewMiddleware(store, rateLimiter, cfg.RateLimitRPM)
	apiHandler := api.NewRouter(app, authMiddleware.Handler)
	handler, err := rootHandler(apiHandler, store)
	if err != nil {
		log.Fatalf("frontend: %v", err)
	}

	current := cfg.Get()
	printBanner(listenInfo.URL, current, listenInfo.Warnings)
	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}

	serverShutdownCtx, serverShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer serverShutdownCancel()
	if err := server.Shutdown(serverShutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	modelShutdownCtx, modelShutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer modelShutdownCancel()
	if err := lifecycleManager.Shutdown(modelShutdownCtx); err != nil {
		log.Printf("model shutdown: %v", err)
	}
	ollamaShutdownCtx, ollamaShutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ollamaShutdownCancel()
	if err := ollamaService.Shutdown(ollamaShutdownCtx); err != nil {
		log.Printf("ollama shutdown: %v", err)
	}
	authMiddleware.Stop()
	rateLimiter.Stop()
}

type gatewayListenInfo struct {
	URL           string
	Warnings      []string
	OllamaBaseURL string
}

func openGatewayListener(cfg *config.Manager) (net.Listener, gatewayListenInfo, error) {
	current := cfg.Get()
	host := current.Server.Host
	port := current.Server.Port
	info := gatewayListenInfo{}
	if serverSharesOllamaPort(host, port, current.Backend.OllamaBaseURL) {
		info.Warnings = append(info.Warnings, fmt.Sprintf("Aegis port %d matches the configured Ollama port; moving Aegis to the next free port.", port))
		return openFallbackListener(host, port+1, info)
	}

	probeURL := probeURL(host, port)
	portHasOllama := isOllamaRoot(probeURL)
	if portHasOllama {
		info.Warnings = append(info.Warnings, fmt.Sprintf("Port %d is already serving Ollama; Aegis moved to the next free port.", port))
		if current.Backend.OllamaBaseURL == config.Defaults().Backend.OllamaBaseURL {
			info.OllamaBaseURL = strings.TrimRight(probeURL, "/")
		}
		return openFallbackListener(host, port+1, info)
	}
	if portAcceptsConnections(host, port) {
		if port == config.Defaults().Server.Port {
			info.Warnings = append(info.Warnings, fmt.Sprintf("Port %d is already in use; Aegis moved to the next free port.", port))
			return openFallbackListener(host, port+1, info)
		}
		return nil, gatewayListenInfo{}, fmt.Errorf("browser URL %s is already in use; change server.port in config.toml", probeURL)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err == nil {
		info.URL = displayURL(host, listener.Addr())
		return listener, info, nil
	}

	if port == config.Defaults().Server.Port && isAddrInUse(err) {
		info.Warnings = append(info.Warnings, fmt.Sprintf("Port %d is already in use; Aegis moved to the next free port.", port))
		return openFallbackListener(host, port+1, info)
	}
	return nil, gatewayListenInfo{}, fmt.Errorf("listen on %s:%d failed: %w", host, port, err)
}

func openFallbackListener(host string, startPort int, info gatewayListenInfo) (net.Listener, gatewayListenInfo, error) {
	for port := startPort; port < startPort+100 && port <= 65535; port++ {
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			info.URL = displayURL(host, listener.Addr())
			return listener, info, nil
		}
		if !isAddrInUse(err) {
			return nil, gatewayListenInfo{}, fmt.Errorf("listen on %s:%d failed: %w", host, port, err)
		}
	}
	return nil, gatewayListenInfo{}, fmt.Errorf("no free Aegis port found after %d", startPort-1)
}

func applyOllamaHostOverride(cfg *config.Manager) error {
	env := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if env == "" {
		return nil
	}
	baseURL, err := normalizeOllamaHost(env)
	if err != nil {
		return err
	}
	if cfg.Get().Backend.OllamaBaseURL != config.Defaults().Backend.OllamaBaseURL {
		return nil
	}
	if err := cfg.SetRuntimeOllamaBaseURL(baseURL); err != nil {
		return err
	}
	log.Printf("aegis: using OLLAMA_HOST for Ollama backend: %s", baseURL)
	return nil
}

func normalizeOllamaHost(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", fmt.Errorf("OLLAMA_HOST is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("OLLAMA_HOST must use http or https")
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("OLLAMA_HOST must include a host")
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "11434"
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return "", fmt.Errorf("OLLAMA_HOST port must be between 1 and 65535")
	}
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	parsed.Host = net.JoinHostPort(host, port)
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func serverSharesOllamaPort(serverHost string, serverPort int, baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Port() == "" {
		return false
	}
	backendPort, err := strconv.Atoi(parsed.Port())
	if err != nil || backendPort != serverPort {
		return false
	}
	return hostsOverlap(serverHost, parsed.Hostname())
}

func hostsOverlap(serverHost, backendHost string) bool {
	serverHost = strings.Trim(serverHost, "[]")
	backendHost = strings.Trim(backendHost, "[]")
	if serverHost == "" || serverHost == "0.0.0.0" || serverHost == "::" {
		return true
	}
	if serverHost == backendHost {
		return true
	}
	if isLoopbackHost(serverHost) && isLoopbackHost(backendHost) {
		return true
	}
	serverIP := net.ParseIP(serverHost)
	backendIP := net.ParseIP(backendHost)
	return serverIP != nil && backendIP != nil && serverIP.Equal(backendIP)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isOllamaRoot(baseURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/", nil)
	if err != nil {
		return false
	}
	res, err := probeHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 512))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(body)), "ollama is running")
}

func portAcceptsConnections(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(displayHost(host), strconv.Itoa(port)), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func probeURL(host string, port int) string {
	return "http://" + net.JoinHostPort(displayHost(host), strconv.Itoa(port))
}

func displayURL(host string, addr net.Addr) string {
	port := ""
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		port = strconv.Itoa(tcpAddr.Port)
	}
	if port == "" {
		_, parsedPort, err := net.SplitHostPort(addr.String())
		if err == nil {
			port = parsedPort
		}
	}
	return "http://" + net.JoinHostPort(displayHost(host), port)
}

func displayHost(host string) string {
	trimmed := strings.Trim(host, "[]")
	switch trimmed {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	default:
		return trimmed
	}
}

func usesOllamaBackend(cfg config.Config) bool {
	if cfg.Backend.DefaultType == "ollama" {
		return true
	}
	for _, model := range cfg.Models.Registry {
		backend := model.Backend
		if backend == "" {
			backend = cfg.Backend.DefaultType
		}
		if backend == "ollama" {
			return true
		}
	}
	return false
}

func bindsAllInterfaces(host string) bool {
	trimmed := strings.Trim(host, "[]")
	return trimmed == "" || trimmed == "0.0.0.0" || trimmed == "::"
}

func ensureFirstKey(ctx context.Context, cfg *config.Manager, store *db.Store) error {
	if !cfg.Get().Security.AutoGenerateKey {
		return nil
	}
	count, err := store.CountActiveKeys(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	raw, err := auth.GenerateKey()
	if err != nil {
		return err
	}
	salt, err := auth.GenerateSalt()
	if err != nil {
		return err
	}
	id, err := auth.GenerateID("key")
	if err != nil {
		return err
	}
	if err := store.CreateAPIKey(ctx, db.NewAPIKey{
		ID:        id,
		Label:     "Initial local key",
		Salt:      salt,
		Hash:      auth.HashKey(raw, salt),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	printInitialKey(raw)
	return nil
}

type readinessStore interface {
	HealthCheck(ctx context.Context) error
}

func rootHandler(apiHandler http.Handler, store readinessStore) (http.Handler, error) {
	dist, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(dist))
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeRootJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed", "code": "METHOD_NOT_ALLOWED"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeRootJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed", "code": "METHOD_NOT_ALLOWED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := store.HealthCheck(ctx); err != nil {
			writeRootJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready", "error": "database unavailable"})
			return
		}
		if index, err := dist.Open("index.html"); err != nil {
			writeRootJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready", "error": "frontend unavailable"})
			return
		} else {
			_ = index.Close()
		}
		writeRootJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.Handle("/v1/", apiHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if file, err := dist.Open(path); err == nil {
			_ = file.Close()
			files.ServeHTTP(w, r)
			return
		}
		index, err := dist.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer index.Close()
		body, err := io.ReadAll(index)
		if err != nil {
			writeRootJSON(w, http.StatusInternalServerError, map[string]string{"error": "frontend unavailable", "code": "FRONTEND_UNAVAILABLE"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	})
	return securityHeaders(mux), nil
}

func writeRootJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Aegis-Version", version)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; font-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self' 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func printBanner(url string, cfg config.Config, warnings []string) {
	fmt.Println()
	fmt.Println("Aegis Gateway")
	fmt.Println("privacy-first local AI gateway")
	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	fmt.Printf("dashboard: %s\n", url)
	fmt.Printf("default backend: %s\n", cfg.Backend.DefaultType)
	fmt.Printf("ollama backend: %s\n", cfg.Backend.OllamaBaseURL)
	fmt.Printf("llama.cpp backend: %s\n", cfg.Backend.LlamaCppBaseURL)
	fmt.Println()
}

func printInitialKey(key string) {
	fmt.Println()
	fmt.Println("+-------------------------------------------------------------+")
	fmt.Println("| Aegis Gateway initial API key                               |")
	fmt.Println("| Save this key now. It is stored hashed and shown only once. |")
	fmt.Println("+-------------------------------------------------------------+")
	fmt.Printf("%s\n", key)
	fmt.Println("+-------------------------------------------------------------+")
	fmt.Println()
}
