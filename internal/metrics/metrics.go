package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/savxzthc/aegis-gateway/internal/hardware"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aegis_requests_total",
		Help: "Total number of requests processed.",
	}, []string{"model", "backend", "status", "fallback"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "aegis_request_duration_seconds",
		Help:    "Request duration in seconds.",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
	}, []string{"model", "backend"})

	VRAMFreeGB = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aegis_vram_free_gb",
		Help: "Free VRAM in GB per GPU.",
	}, []string{"gpu_index"})

	ActiveKeysTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "aegis_active_keys_total",
		Help: "Number of active (non-revoked) API keys.",
	})

	AuthFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aegis_auth_failures_total",
		Help: "Total number of authentication failures.",
	}, []string{"reason"})
)

// StartCollection begins background Prometheus metric collection.
func StartCollection(provider hardware.Provider) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			info, err := provider.Query(ctx)
			cancel()
			if err == nil && info.Detected {
				VRAMFreeGB.WithLabelValues("0").Set(info.VRAMFreeGB)
			}
		}
	}()
}

// RecordRequest records a completed API request in Prometheus.
func RecordRequest(model, backend string, statusCode int, fallback bool, duration time.Duration) {
	status := "2xx"
	if statusCode >= 400 && statusCode < 500 {
		status = "4xx"
	} else if statusCode >= 500 {
		status = "5xx"
	}
	fb := "false"
	if fallback {
		fb = "true"
	}
	RequestsTotal.WithLabelValues(model, backend, status, fb).Inc()
	RequestDuration.WithLabelValues(model, backend).Observe(duration.Seconds())
}
