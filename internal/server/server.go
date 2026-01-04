package server

import (
	"fmt"
	"net/http"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/handlers"
)

func NewServer(cfg *config.Config, deps *handlers.Dependencies) *http.Server {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", handlers.HealthHandler)
	mux.HandleFunc("/ready", handlers.ReadyHandler(deps))

	// Device OAuth Flow endpoints
	mux.HandleFunc("/device/authorize", handlers.DeviceAuthorizeHandler(deps))
	mux.HandleFunc("/device/token", handlers.DeviceTokenHandler(deps))

	// OAuth Web Flow endpoints (for OSM)
	mux.HandleFunc("/oauth/authorize", handlers.OAuthAuthorizeHandler(deps))
	mux.HandleFunc("/oauth/callback", handlers.OAuthCallbackHandler(deps))

	// API endpoints for scoreboard
	mux.HandleFunc("/api/v1/patrols", handlers.GetPatrolScoresHandler(deps))

	// Wrap with middleware
	handler := loggingMiddleware(mux)

	return &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler: handler,
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simple logging middleware
		// TODO: Replace with structured logging (e.g., zerolog, zap)
		next.ServeHTTP(w, r)
	})
}
