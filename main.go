package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
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
	resetAdminKey := flag.Bool("reset-admin-key", false, "revoke all active API keys, create one replacement key, print it once, and exit")
	flag.Parse()

	cfg, err := config.LoadManager("config.toml")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	startupWarnings, err := applyOllamaHostOverride(cfg)
	if err != nil {
		log.Fatalf("ollama host: %v", err)
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
	if *resetAdminKey {
		raw, err := resetAdminAPIKey(ctx, store)
		if err != nil {
			log.Fatalf("reset admin key: %v", err)
		}
		printResetKey(raw)
		return
	}
	activeKeyCount, err := ensureFirstKey(ctx, cfg, store)
	if err != nil {
		log.Fatalf("first key: %v", err)
	}
	listener, listenInfo, err := openGatewayListener(cfg)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	listenInfo.Warnings = append(startupWarnings, listenInfo.Warnings...)
	defer listener.Close()
	if listenInfo.OllamaBaseURL != "" {
		if err := cfg.SetRuntimeOllamaBaseURL(listenInfo.OllamaBaseURL); err != nil {
			log.Fatalf("ollama host: %v", err)
		}
	}
	if bindsAllInterfaces(cfg.Get().Server.Host) {
		listenInfo.Warnings = append(listenInfo.Warnings, "Aegis is bound to all network interfaces; API keys may be sent over your LAN in plaintext unless you use a local-only host.")
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
	listenInfo.Warnings = append(listenInfo.Warnings, backendReachabilityWarnings(app)...)
	shutdownStreams := make(chan struct{})
	app.Shutdown = shutdownStreams

	rateLimiter := auth.NewRateLimiter()
	authMiddleware := auth.NewMiddleware(store, rateLimiter, cfg.RateLimitRPM)
	apiHandler := api.NewRouter(app, authMiddleware.Handler)
	handler, err := rootHandler(apiHandler, store)
	if err != nil {
		log.Fatalf("frontend: %v", err)
	}

	current := cfg.Get()
	printBanner(listenInfo.URL, current, listenInfo.Warnings, activeKeyCount)
	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	server.RegisterOnShutdown(func() {
		close(shutdownStreams)
	})

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
		info.Warnings = append(info.Warnings, fmt.Sprintf("Port %d is already serving Ollama; Aegis moved to the next free port. Close the conflicting Ollama process to use port %d for Aegis.", port, port))
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
			if port != startPort {
				info.Warnings = append(info.Warnings, fmt.Sprintf("Aegis skipped occupied fallback ports and selected %d; use the printed dashboard URL.", port))
			}
			info.URL = displayURL(host, listener.Addr())
			return listener, info, nil
		}
		if !isAddrInUse(err) {
			return nil, gatewayListenInfo{}, fmt.Errorf("listen on %s:%d failed: %w", host, port, err)
		}
	}
	return nil, gatewayListenInfo{}, fmt.Errorf("no free Aegis port found after %d", startPort-1)
}

func applyOllamaHostOverride(cfg *config.Manager) ([]string, error) {
	env := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if env == "" {
		return nil, nil
	}
	baseURL, err := normalizeOllamaHost(env)
	if err != nil {
		return nil, err
	}
	current := cfg.Get()
	if current.Backend.OllamaBaseURL != config.Defaults().Backend.OllamaBaseURL {
		return nil, nil
	}
	if serverSharesOllamaPort(current.Server.Host, current.Server.Port, baseURL) {
		return []string{fmt.Sprintf("Ignoring OLLAMA_HOST=%s because it conflicts with the Aegis dashboard port %d; Ollama will use %s instead.", env, current.Server.Port, current.Backend.OllamaBaseURL)}, nil
	}
	if err := cfg.SetRuntimeOllamaBaseURL(baseURL); err != nil {
		return nil, err
	}
	log.Printf("aegis: using OLLAMA_HOST for Ollama backend: %s", baseURL)
	return nil, nil
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

func ensureFirstKey(ctx context.Context, cfg *config.Manager, store *db.Store) (int, error) {
	if !cfg.Get().Security.AutoGenerateKey {
		return store.CountActiveKeys(ctx)
	}
	count, err := store.CountActiveKeys(ctx)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return count, nil
	}
	raw, _, err := createStoredAPIKey(ctx, store, "Initial local key")
	if err != nil {
		return 0, err
	}
	printInitialKey(raw)
	return 1, nil
}

func createStoredAPIKey(ctx context.Context, store *db.Store, label string) (string, string, error) {
	raw, err := auth.GenerateKey()
	if err != nil {
		return "", "", err
	}
	salt, err := auth.GenerateSalt()
	if err != nil {
		return "", "", err
	}
	id, err := auth.GenerateID("key")
	if err != nil {
		return "", "", err
	}
	return raw, id, store.CreateAPIKey(ctx, db.NewAPIKey{
		ID:        id,
		Label:     label,
		Salt:      salt,
		Hash:      auth.HashKey(raw, salt),
		CreatedAt: time.Now().UTC(),
	})
}

func resetAdminAPIKey(ctx context.Context, store *db.Store) (string, error) {
	raw, err := auth.GenerateKey()
	if err != nil {
		return "", err
	}
	salt, err := auth.GenerateSalt()
	if err != nil {
		return "", err
	}
	id, err := auth.GenerateID("key")
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if err := store.ResetAPIKeys(ctx, db.NewAPIKey{
		ID:        id,
		Label:     "Local recovery key",
		Salt:      salt,
		Hash:      auth.HashKey(raw, salt),
		CreatedAt: now,
	}, now); err != nil {
		return "", err
	}
	return raw, nil
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
			serveFrontendIndex(w, dist)
			return
		}
		if file, err := dist.Open(path); err == nil {
			_ = file.Close()
			if path == "index.html" {
				w.Header().Set("Cache-Control", "no-store")
			} else if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, "assets/") {
			if strings.HasSuffix(path, ".js") {
				serveStaleAssetRecovery(w)
				return
			}
			http.NotFound(w, r)
			return
		}
		if strings.Contains(path[strings.LastIndex(path, "/")+1:], ".") {
			http.NotFound(w, r)
			return
		}
		serveFrontendIndex(w, dist)
	})
	return securityHeaders(mux), nil
}

func serveStaleAssetRecovery(w http.ResponseWriter) {
	body := `window.location.replace(window.location.pathname + window.location.search + (window.location.search ? '&' : '?') + 'aegis_cache_bust=' + Date.now());`
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write([]byte(body))
}

func serveFrontendIndex(w http.ResponseWriter, dist fs.FS) {
	index, err := dist.Open("index.html")
	if err != nil {
		writeRootJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "frontend unavailable", "code": "FRONTEND_UNAVAILABLE"})
		return
	}
	defer index.Close()
	body, err := io.ReadAll(index)
	if err != nil {
		writeRootJSON(w, http.StatusInternalServerError, map[string]string{"error": "frontend unavailable", "code": "FRONTEND_UNAVAILABLE"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

func writeRootJSON(w http.ResponseWriter, status int, value interface{}) {
	var buf strings.Builder
	if err := json.NewEncoder(&buf).Encode(value); err != nil {
		http.Error(w, `{"error":"response encoding failed","code":"RESPONSE_ENCODING_FAILED"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(buf.String()))
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

func printBanner(url string, cfg config.Config, warnings []string, activeKeyCount int) {
	fmt.Println()
	fmt.Println("Aegis Gateway")
	fmt.Println("privacy-first local AI gateway")
	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	fmt.Printf("dashboard: %s\n", url)
	fmt.Printf("version: %s\n", version)
	fmt.Printf("build time: %s\n", buildTime)
	fmt.Printf("active API keys: %d\n", activeKeyCount)
	if activeKeyCount > 0 {
		fmt.Println("key recovery: .\\aegis-gateway.exe --reset-admin-key")
	}
	fmt.Printf("default backend: %s\n", cfg.Backend.DefaultType)
	fmt.Printf("ollama backend: %s\n", cfg.Backend.OllamaBaseURL)
	fmt.Printf("llama.cpp backend: %s\n", cfg.Backend.LlamaCppBaseURL)
	fmt.Println()
}

func backendReachabilityWarnings(server *api.Server) []string {
	cfg := server.Config.Get()
	used := map[string]bool{}
	for _, model := range cfg.Models.Registry {
		backendType := model.Backend
		if backendType == "" {
			backendType = cfg.Backend.DefaultType
		}
		used[backendType] = true
	}
	var warnings []string
	for backendType := range used {
		server.BackendsMu.RLock()
		backend := server.Backends[backendType]
		server.BackendsMu.RUnlock()
		if backend == nil {
			warnings = append(warnings, fmt.Sprintf("%s backend is configured but unavailable.", backendType))
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := backend.Ping(ctx)
		cancel()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s backend is not reachable yet: %v", backendType, err))
		}
	}
	return warnings
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

func printResetKey(key string) {
	fmt.Println()
	fmt.Println("+-------------------------------------------------------------+")
	fmt.Println("| Aegis Gateway recovery API key                              |")
	fmt.Println("| Previous active keys were revoked. Save this new key now.   |")
	fmt.Println("+-------------------------------------------------------------+")
	fmt.Printf("%s\n", key)
	fmt.Println("+-------------------------------------------------------------+")
	fmt.Println()
}
