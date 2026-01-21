package main

import (
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/devicecode"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/devicesession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreaudit"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/websession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/logging"
)

func main() {
	// Initialize structured logging
	logging.InitLogger()

	// Parse command line flags
	unusedThreshold := flag.Int("unused-threshold", 30, "Days of inactivity before a device is considered unused")
	auditRetention := flag.Int("audit-retention", 14, "Days to retain score audit logs")
	flag.Parse()

	slog.Info("starting database cleanup",
		"unused_threshold_days", *unusedThreshold,
	)

	// Load minimal configuration (only database and Redis)
	cfg, err := config.LoadMinimal()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize database connections
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

	// Initialize Redis cache
	redisClient, err := db.NewRedisClient(cfg.Redis.RedisURL, cfg.Redis.RedisKeyPrefix)
	if err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// Create database connections wrapper
	conns := db.NewConnections(dbConn, redisClient)

	slog.Info("database connections established")

	// Run cleanup operations
	exitCode := 0

	// Clean up expired device codes
	slog.Info("cleaning up expired device codes")
	if err := devicecode.DeleteExpired(conns); err != nil {
		slog.Error("failed to delete expired device codes", "error", err)
		exitCode = 1
	} else {
		slog.Info("expired device codes cleaned up successfully")
	}

	// Clean up expired sessions
	slog.Info("cleaning up expired device sessions")
	if err := devicesession.DeleteExpired(conns); err != nil {
		slog.Error("failed to delete expired device sessions", "error", err)
		exitCode = 1
	} else {
		slog.Info("expired device sessions cleaned up successfully")
	}

	// Clean up unused devices
	slog.Info("cleaning up unused devices",
		"threshold_days", *unusedThreshold,
	)
	if err := devicecode.DeleteUnused(conns, time.Duration(*unusedThreshold)*24*time.Hour); err != nil {
		slog.Error("failed to delete unused device codes", "error", err)
		exitCode = 1
	} else {
		slog.Info("unused device codes cleaned up successfully")
	}

	// Clean up expired web sessions
	slog.Info("cleaning up expired web sessions")
	if err := websession.DeleteExpired(conns); err != nil {
		slog.Error("failed to delete expired web sessions", "error", err)
		exitCode = 1
	} else {
		slog.Info("expired web sessions cleaned up successfully")
	}

	// Clean up old score audit logs
	slog.Info("cleaning up old score audit logs",
		"retention_days", *auditRetention,
	)
	if err := scoreaudit.DeleteExpired(conns, time.Duration(*auditRetention)*24*time.Hour); err != nil {
		slog.Error("failed to delete old score audit logs", "error", err)
		exitCode = 1
	} else {
		slog.Info("old score audit logs cleaned up successfully")
	}

	if exitCode == 0 {
		slog.Info("database cleanup completed successfully")
	} else {
		slog.Error("database cleanup completed with errors")
	}

	os.Exit(exitCode)
}
