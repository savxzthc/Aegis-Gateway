package api

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/savxzthc/aegis-gateway/internal/backends"
	"github.com/savxzthc/aegis-gateway/internal/config"
	"github.com/savxzthc/aegis-gateway/internal/db"
)

// Stats handles GET /v1/stats.
func (s *Server) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.DB.GetStats(r.Context(), time.Now().UTC())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "stats query failed", "STATS_QUERY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, statsResponse{
		RequestsToday:     stats.RequestsToday,
		RequestsYesterday: stats.RequestsYesterday,
		RequestsTotal:     stats.RequestsTotal,
		AvgLatencyMS:      stats.AvgLatencyMS,
		TopModels:         stats.TopModels,
		FallbackRatePct:   stats.FallbackRatePct,
		RequestsPerHour:   stats.RequestsPerHour,
		ActiveModel:       s.Lifecycle.ActiveModel(),
	})
}

// Logs handles GET /v1/logs.
func (s *Server) Logs(w http.ResponseWriter, r *http.Request) {
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 200 {
		limit = 200
	}
	logs, err := s.DB.ListRequestLogs(r.Context(), limit, offset)
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "log query failed", "LOG_QUERY_FAILED", err)
		return
	}
	total, err := s.DB.CountRequestLogs(r.Context())
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "log query failed", "LOG_QUERY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, logsResponse{Data: logs, Limit: limit, Offset: offset, Total: total})
}

// GetConfig handles GET /v1/config.
func (s *Server) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config.Get()
	writeJSON(w, http.StatusOK, configResponse{
		Config: cfg,
		Runtime: runtimeInfo{
			GoVersion:  s.GoVersion,
			UptimeSec:  int64(time.Since(s.StartedAt).Seconds()),
			BuildTime:  s.BuildTime,
			AppVersion: s.Version,
		},
	})
}

// PatchConfig handles PATCH /v1/config.
func (s *Server) PatchConfig(w http.ResponseWriter, r *http.Request) {
	var patch config.EditablePatch
	if err := decodeJSONBody(w, r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body", "INVALID_JSON")
		return
	}
	previous := s.Config.Get()
	cfg, err := s.Config.PatchEditable(patch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_CONFIG")
		return
	}
	restartRequired := patch.Port != nil && *patch.Port != previous.Server.Port
	s.BackendsMu.Lock()
	s.Backends["ollama"] = backends.NewOllamaBackend(cfg.Backend.OllamaBaseURL)
	s.Backends["llamacpp"] = backends.NewLlamaCppBackend(cfg.Backend.LlamaCppBaseURL)
	s.BackendsMu.Unlock()
	writeJSON(w, http.StatusOK, configResponse{
		Config: cfg,
		Runtime: runtimeInfo{
			GoVersion:  runtime.Version(),
			UptimeSec:  int64(time.Since(s.StartedAt).Seconds()),
			BuildTime:  s.BuildTime,
			AppVersion: s.Version,
		},
		RestartRequired: restartRequired,
	})
}

type statsResponse struct {
	RequestsToday     int64                  `json:"requests_today"`
	RequestsYesterday int64                  `json:"requests_yesterday"`
	RequestsTotal     int64                  `json:"requests_total"`
	AvgLatencyMS      int64                  `json:"avg_latency_ms"`
	TopModels         []db.TopModelStat      `json:"top_models"`
	FallbackRatePct   float64                `json:"fallback_rate_pct"`
	RequestsPerHour   []db.HourlyRequestStat `json:"requests_per_hour"`
	ActiveModel       string                 `json:"active_model"`
}

type logsResponse struct {
	Data   []db.RequestLog `json:"data"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
	Total  int64           `json:"total"`
}

type configResponse struct {
	Config          config.Config `json:"config"`
	Runtime         runtimeInfo   `json:"runtime"`
	RestartRequired bool          `json:"restart_required"`
}

type runtimeInfo struct {
	GoVersion  string `json:"go_version"`
	UptimeSec  int64  `json:"uptime_sec"`
	BuildTime  string `json:"build_time"`
	AppVersion string `json:"app_version"`
}

func parsePositiveInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
