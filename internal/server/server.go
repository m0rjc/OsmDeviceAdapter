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
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewServer(cfg *config.Config, deps *handlers.Dependencies) *http.Server {
	mux := http.NewServeMux()

	// Device OAuth Flow endpoints (configurable path prefix)
	mux.HandleFunc(fmt.Sprintf("%s/authorize", cfg.Paths.DevicePrefix), handlers.DeviceAuthorizeHandler(deps))
	mux.HandleFunc(fmt.Sprintf("%s/token", cfg.Paths.DevicePrefix), handlers.DeviceTokenHandler(deps))
	mux.HandleFunc(cfg.Paths.DevicePrefix, handlers.OAuthAuthorizeHandler(deps))                          // User verification page
	mux.HandleFunc("/d/", handlers.ShortCodeRedirectHandler(deps))                                        // Short URL redirect for QR codes
	mux.HandleFunc(fmt.Sprintf("%s/confirm", cfg.Paths.DevicePrefix), handlers.OAuthConfirmHandler(deps)) // Device authorization confirmation
	mux.HandleFunc(fmt.Sprintf("%s/cancel", cfg.Paths.DevicePrefix), handlers.OAuthCancelHandler(deps))   // Device authorization cancellation

	// OAuth Web Flow endpoints (for OSM) (configurable path prefix)
	mux.HandleFunc(fmt.Sprintf("%s/authorize", cfg.Paths.OAuthPrefix), handlers.OAuthAuthorizeHandler(deps))
	mux.HandleFunc(fmt.Sprintf("%s/callback", cfg.Paths.OAuthPrefix), handlers.OAuthCallbackHandler(deps))
	mux.HandleFunc(fmt.Sprintf("%s/select-section", cfg.Paths.DevicePrefix), handlers.OAuthSelectSectionHandler(deps))

	// API endpoints for scoreboard (requires authentication) (configurable path prefix)
	deviceAuthMiddleware := middleware.DeviceAuthMiddleware(deps.DeviceAuth)
	mux.Handle(fmt.Sprintf("%s/v1/patrols", cfg.Paths.APIPrefix), deviceAuthMiddleware(handlers.GetPatrolScoresHandler(deps)))

	// Apply middleware chain:
	// 1. Remote metadata (Cloudflare headers, HTTPS redirect, HSTS) - applied to all routes
	// 2. Logging middleware - applied to all routes
	handler := loggingMiddleware(
		middleware.RemoteMetadataMiddleware(cfg.ExternalDomains.ExposedDomain)(mux),
	)

	return &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
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
