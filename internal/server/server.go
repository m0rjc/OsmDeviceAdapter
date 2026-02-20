package server

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/admin"
	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/handlers"
	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	wsinternal "github.com/m0rjc/OsmDeviceAdapter/internal/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewServer(cfg *config.Config, deps *handlers.Dependencies) *http.Server {
	mux := http.NewServeMux()

	// Home page
	mux.HandleFunc("/", handlers.HomeHandler(deps))

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

	// Device WebSocket endpoint — token auth via query param
	if deps.WebSocketHub != nil {
		mux.HandleFunc("/ws/device", wsinternal.DeviceWebSocketHandler(
			deps.WebSocketHub,
			deps.DeviceAuth,
			cfg.ExternalDomains.ExposedDomain,
		))
	}

	// Admin OAuth flow endpoints (server-handled, not SPA)
	mux.HandleFunc("/admin/login", handlers.AdminLoginHandler(deps))
	mux.HandleFunc("/admin/callback", handlers.AdminCallbackHandler(deps))
	mux.HandleFunc("/admin/logout", handlers.AdminLogoutHandler(deps))

	// Admin API endpoints (authenticated via session cookie)
	adminSessionMw := middleware.SessionMiddleware(deps.Conns, handlers.AdminSessionCookieName)
	adminTokenMw := middleware.TokenRefreshMiddleware(deps.Conns, deps.WebAuth)
	adminSecurityMw := middleware.SecurityHeadersMiddleware
	adminMiddleware := func(h http.Handler) http.Handler {
		return adminSecurityMw(adminSessionMw(adminTokenMw(h)))
	}

	mux.Handle("/api/admin/session", adminMiddleware(handlers.AdminSessionHandler(deps)))
	mux.Handle("/api/admin/sections", adminMiddleware(handlers.AdminSectionsHandler(deps)))
	// Route settings before scores - Go's mux uses longest match, but we need to check path suffix
	// Settings endpoint: /api/admin/sections/{id}/settings
	// Scores endpoint: /api/admin/sections/{id}/scores
	mux.Handle("/api/admin/sections/", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/settings") {
			handlers.AdminSettingsHandler(deps).ServeHTTP(w, r)
		} else {
			handlers.AdminScoresHandler(deps).ServeHTTP(w, r)
		}
	})))

	// Ad-hoc patrol CRUD endpoints
	mux.Handle("/api/admin/adhoc/patrols", adminMiddleware(handlers.AdminAdhocPatrolsHandler(deps)))
	mux.Handle("/api/admin/adhoc/patrols/", adminMiddleware(handlers.AdminAdhocPatrolHandler(deps)))

	// Scoreboard management endpoints
	mux.Handle("/api/admin/scoreboards", adminMiddleware(handlers.AdminScoreboardsHandler(deps)))
	mux.Handle("/api/admin/scoreboards/", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/timer") {
			handlers.AdminScoreboardTimerHandler(deps).ServeHTTP(w, r)
		} else {
			handlers.AdminScoreboardSectionHandler(deps).ServeHTTP(w, r)
		}
	})))

	// Admin SPA (serves static files for /admin/*)
	// Note: More specific routes (/admin/login, /admin/callback, /admin/logout, /api/admin/*)
	// are registered above and take precedence over this catch-all
	mux.Handle("/admin/", adminSecurityMw(admin.NewSPAHandler()))

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

	// Prometheus metrics endpoint (using custom registry without Go runtime metrics)
	mux.Handle("/metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))

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

// Hijack implements http.Hijacker so that WebSocket upgrades work through this wrapper.
func (sw *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return sw.ResponseWriter.(http.Hijacker).Hijack()
}
