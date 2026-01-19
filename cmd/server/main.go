package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/deviceauth"
	"github.com/m0rjc/OsmDeviceAdapter/internal/handlers"
	"github.com/m0rjc/OsmDeviceAdapter/internal/logging"
	_ "github.com/m0rjc/OsmDeviceAdapter/internal/metrics" // Initialize metrics
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm/oauthclient"
	"github.com/m0rjc/OsmDeviceAdapter/internal/server"
	"github.com/m0rjc/OsmDeviceAdapter/internal/tokenrefresh"
	"github.com/m0rjc/OsmDeviceAdapter/internal/webauth"
)

func main() {
	// Initialize structured logging
	logging.InitLogger()

	slog.Info("starting OSM Device Adapter")

	// Load configuration from environment
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	slog.Info("configuration loaded successfully")

	// Initialize database connections (GORM now handles migrations automatically)
	dbConn, err := db.NewPostgresConnection(cfg.Database.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	sqlDB, err := dbConn.DB()
	if err != nil {
		slog.Error("failed to get underlying database connection", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()
	slog.Info("database connection established")

	// Initialize Redis cache
	redisClient, err := db.NewRedisClient(cfg.Redis.RedisURL, cfg.Redis.RedisKeyPrefix)
	if err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	slog.Info("redis connection established", "key_prefix", cfg.Redis.RedisKeyPrefix)

	// Create database connections wrapper
	conns := db.NewConnections(dbConn, redisClient)

	// Initialize services in dependency order
	rlStore := osm.NewPrometheusRateLimitDecorator(redisClient)
	recorder := osm.NewPrometheusLatencyRecorder()

	// Create OAuth client for token operations
	oauthClient := oauthclient.New(cfg.OAuth.OSMClientID, cfg.OAuth.OSMClientSecret, cfg.OAuth.OSMRedirectURI, cfg.ExternalDomains.OSMDomain)

	// Create central token refresh service
	tokenRefreshService := tokenrefresh.NewService(oauthClient)

	// Create device auth service
	deviceAuthService := deviceauth.NewService(conns, tokenRefreshService)

	// Create web auth service for admin session management
	webAuthService := webauth.NewService(conns, tokenRefreshService)

	// Create OSM client (token refresh is handled via context-bound functions)
	osmClient := osm.NewClient(cfg.ExternalDomains.OSMDomain, rlStore, recorder)

	// Create handler dependencies
	deps := &handlers.Dependencies{
		Config:     cfg,
		Conns:      conns,
		OSM:        osmClient,
		OSMAuth:    oauthClient,
		DeviceAuth: deviceAuthService,
		WebAuth:    webAuthService,
	}

	// Create and configure HTTP server
	srv := server.NewServer(cfg, deps)

	// Create and configure metrics/health server (internal only)
	metricsSrv := server.NewMetricsServer(deps)

	// Start metrics server in a goroutine
	go func() {
		addr := ":9090"
		slog.Info("metrics server listening", "address", addr, "port", 9090)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
			os.Exit(1)
		}
	}()

	// Start main server in a goroutine
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		slog.Info("server listening", "address", addr, "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("received shutdown signal, shutting down gracefully")

	// Graceful shutdown with 30 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown both servers concurrently
	errChan := make(chan error, 2)
	go func() {
		if err := srv.Shutdown(ctx); err != nil {
			errChan <- fmt.Errorf("main server shutdown error: %w", err)
		} else {
			errChan <- nil
		}
	}()
	go func() {
		if err := metricsSrv.Shutdown(ctx); err != nil {
			errChan <- fmt.Errorf("metrics server shutdown error: %w", err)
		} else {
			errChan <- nil
		}
	}()

	// Wait for both shutdowns to complete
	for i := 0; i < 2; i++ {
		if err := <-errChan; err != nil {
			slog.Error("server forced to shutdown", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("servers exited successfully")
}
