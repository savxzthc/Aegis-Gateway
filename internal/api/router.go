package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/savxzthc/aegis-gateway/internal/backends"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
	"github.com/savxzthc/aegis-gateway/internal/hardware"
	"github.com/savxzthc/aegis-gateway/internal/lifecycle"
	modelrouter "github.com/savxzthc/aegis-gateway/internal/router"
)

// Server holds API dependencies.
type Server struct {
	Config           *config.Manager
	DB               *db.Store
	HardwareProvider hardware.Provider
	VRAMRouter       *modelrouter.VRAMRouter
	Lifecycle        *lifecycle.Manager
	BackendsMu       sync.RWMutex
	Backends         map[string]backends.Backend
	StartedAt        time.Time
	Version          string
	BuildTime        string
	GoVersion        string
	Shutdown         <-chan struct{}
}

// NewRouter wires middleware and Aegis API routes.
func NewRouter(server *Server, authMiddleware func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(responseMetadata(server.Version))
	r.Use(recoverJSON)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found", "NOT_FOUND")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED")
	})

	r.Route("/auth", func(r chi.Router) {
		r.Use(localCORSMiddleware)
		r.Options("/*", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		r.Post("/login", server.Login)
		r.Post("/logout", server.Logout)
		r.With(authMiddleware).Get("/me", server.Me)
		r.With(authMiddleware).Post("/totp/setup", server.TOTPSetup)
		r.With(authMiddleware).Post("/totp/confirm", server.TOTPConfirm)
		r.With(authMiddleware).Delete("/totp", server.TOTPDisable)
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(localCORSMiddleware)
		r.Use(authMiddleware)
		r.Options("/*", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		r.Post("/chat/completions", server.ChatCompletions)
		r.Post("/completions", server.Completions)
		r.Post("/embeddings", server.Embeddings)
		r.Get("/models", server.Models)
		r.Get("/models/catalog", server.ModelCatalog)
		r.Get("/models/local", server.LocalModels)
		r.Post("/models/register", server.RegisterInstalledModel)
		r.Post("/models/pull", server.PullModel)
		r.Get("/models/pull/*", server.PullModelStatus)
		r.Post("/models/unload", server.UnloadModel)
		r.Get("/hardware", server.Hardware)
		r.Get("/hardware/stream", server.HardwareStream)
		r.Get("/stats", server.Stats)
		r.Get("/logs", server.Logs)
		r.Get("/keys", server.ListKeys)
		r.Post("/keys", server.CreateKey)
		r.Patch("/keys/{id}/models", server.UpdateKeyModels)
		r.Patch("/keys/{id}/rate-limit", server.UpdateKeyRateLimit)
		r.Delete("/keys/{id}", server.DeleteKey)
		r.Get("/users", server.ListUsers)
		r.Post("/users", server.CreateUser)
		r.Delete("/users/{id}", server.DeleteUser)
		r.Patch("/users/{id}/role", server.UpdateUserRole)
		r.Get("/templates", server.ListTemplates)
		r.Post("/templates", server.CreateTemplate)
		r.Patch("/templates/{id}", server.UpdateTemplate)
		r.Delete("/templates/{id}", server.DeleteTemplate)
		r.Get("/config", server.GetConfig)
		r.Patch("/config", server.PatchConfig)
		r.Post("/config/reload", server.ReloadConfig)
		r.Post("/batch", server.BatchCompletions)
	})

	return r
}

func responseMetadata(version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requestID := middleware.GetReqID(r.Context()); requestID != "" {
				w.Header().Set(middleware.RequestIDHeader, requestID)
			}
			if version != "" {
				w.Header().Set("X-Aegis-Version", version)
			}
			next.ServeHTTP(w, r)
		})
	}
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(value); err != nil {
		http.Error(w, `{"error":"response encoding failed","code":"RESPONSE_ENCODING_FAILED"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, errorResponse{Error: message, Code: code})
}

func writePrivateError(w http.ResponseWriter, r *http.Request, status int, message, code string, err error) {
	requestID := middleware.GetReqID(r.Context())
	log.Printf("aegis: request failure request_id=%s code=%s method=%s path=%s error=%v", requestID, code, r.Method, r.URL.Path, err)
	writeError(w, status, message, code)
}

type statusRecorder struct {
	http.ResponseWriter
	wrote bool
}

func (r *statusRecorder) WriteHeader(status int) {
	r.wrote = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(body)
}

func (r *statusRecorder) Flush() {
	flusher, ok := r.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	if !r.wrote {
		r.wrote = true
	}
	flusher.Flush()
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func recoverJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w}
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			requestID := middleware.GetReqID(r.Context())
			log.Printf("aegis: recovered panic request_id=%s method=%s path=%s panic=%v", requestID, r.Method, r.URL.Path, recovered)
			if recorder.wrote {
				return
			}
			writeError(recorder, http.StatusInternalServerError, "internal server error", "INTERNAL_ERROR")
		}()
		next.ServeHTTP(recorder, r)
	})
}

func localCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !isLocalOrigin(origin) {
			writeError(w, http.StatusForbidden, "origin is not allowed", "CORS_ORIGIN_DENIED")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Expose-Headers", "X-Aegis-Routed-Model, X-Aegis-Fallback, X-Request-Id, X-Aegis-Version")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.Header().Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON value")
	}
	return nil
}

func (s *Server) backendFor(model string) (string, backends.Backend, bool) {
	cfg := s.Config.Get()
	modelCfg := cfg.Models.Registry[model]
	backendType := modelCfg.Backend
	if backendType == "" {
		backendType = cfg.Backend.DefaultType
	}
	s.BackendsMu.RLock()
	defer s.BackendsMu.RUnlock()
	backend, ok := s.Backends[backendType]
	return backendType, backend, ok
}
