package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/handlers"
	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewServer(cfg *config.Config, deps *handlers.Dependencies) *http.Server {
	mux := http.NewServeMux()

	// Device OAuth Flow endpoints
	mux.HandleFunc("/device/authorize", handlers.DeviceAuthorizeHandler(deps))
	mux.HandleFunc("/device/token", handlers.DeviceTokenHandler(deps))
	mux.HandleFunc("/device", handlers.OAuthAuthorizeHandler(deps)) // User verification page

	// OAuth Web Flow endpoints (for OSM)
	mux.HandleFunc("/oauth/authorize", handlers.OAuthAuthorizeHandler(deps))
	mux.HandleFunc("/oauth/callback", handlers.OAuthCallbackHandler(deps))
	mux.HandleFunc("/device/select-section", handlers.OAuthSelectSectionHandler(deps))

	// API endpoints for scoreboard
	mux.HandleFunc("/api/v1/patrols", handlers.GetPatrolScoresHandler(deps))

	// Wrap with middleware
	handler := loggingMiddleware(mux)

	return &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler: handler,
	}
}

// NewMetricsServer creates a new HTTP server for internal metrics and health checks
// This server should not be exposed to the public internet
func NewMetricsServer(deps *handlers.Dependencies) *http.Server {
	mux := http.NewServeMux()

	// Health check endpoints
	mux.HandleFunc("/health", handlers.HealthHandler)
	mux.HandleFunc("/ready", handlers.ReadyHandler(deps))

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	return &http.Server{
		Addr:    ":9090",
		Handler: mux,
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Serve the request
		next.ServeHTTP(sw, r)

		// Calculate duration
		duration := time.Since(start)

		// Record metrics
		metrics.HTTPRequestDuration.WithLabelValues(
			r.Method,
			r.URL.Path,
			strconv.Itoa(sw.statusCode),
		).Observe(duration.Seconds())

		metrics.HTTPRequestsTotal.WithLabelValues(
			r.Method,
			r.URL.Path,
			strconv.Itoa(sw.statusCode),
		).Inc()

		// Log the request
		slog.Info("http.request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.statusCode,
			"duration_ms", duration.Milliseconds(),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code
type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.statusCode = code
	sw.ResponseWriter.WriteHeader(code)
}
