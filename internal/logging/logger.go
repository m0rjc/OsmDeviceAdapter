package logging

import (
	"log/slog"
	"os"
	"strings"
)

// InitLogger initializes the structured logger based on environment configuration
func InitLogger() {
	logLevel := getLogLevel()
	logFormat := getLogFormat()

	var handler slog.Handler

	handlerOpts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true, // Include file and line number
	}

	switch logFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	default:
		handler = slog.NewTextHandler(os.Stdout, handlerOpts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	slog.Info("logger initialized",
		"level", logLevel.String(),
		"format", logFormat,
	)
}

// getLogLevel reads the LOG_LEVEL environment variable and returns the corresponding slog.Level
func getLogLevel() slog.Level {
	levelStr := strings.ToLower(os.Getenv("LOG_LEVEL"))
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// getLogFormat reads the LOG_FORMAT environment variable and returns the format
func getLogFormat() string {
	format := strings.ToLower(os.Getenv("LOG_FORMAT"))
	switch format {
	case "json":
		return "json"
	case "text", "":
		return "text"
	default:
		return "text"
	}
}
