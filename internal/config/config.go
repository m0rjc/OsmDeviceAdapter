package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Server configuration
	Port int
	Host string

	// External domains
	ExposedDomain string // The domain where this service is exposed
	OSMDomain     string // Online Scout Manager domain

	// OAuth configuration for OSM
	OSMClientID     string
	OSMClientSecret string
	OSMRedirectURI  string

	// Database
	DatabaseURL string

	// Redis
	RedisURL       string
	RedisKeyPrefix string

	// Device OAuth configuration
	DeviceCodeExpiry   int      // seconds
	DevicePollInterval int      // seconds
	AllowedClientIDs   []string // List of allowed OAuth client IDs

	// Cache configuration (for patrol scores and other data)
	CacheFallbackTTL int // seconds - how long to keep stale data for emergency use (default: 8 days)

	// Rate limiting thresholds for dynamic cache TTL
	RateLimitCaution  int // remaining requests threshold for caution level (default: 200)
	RateLimitWarning  int // remaining requests threshold for warning level (default: 100)
	RateLimitCritical int // remaining requests threshold for critical level (default: 20)
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:               getEnvAsInt("PORT", 8080),
		Host:               getEnv("HOST", "0.0.0.0"),
		ExposedDomain:      getEnv("EXPOSED_DOMAIN", ""),
		OSMDomain:          getEnv("OSM_DOMAIN", "https://www.onlinescoutmanager.co.uk"),
		OSMClientID:        getEnv("OSM_CLIENT_ID", ""),
		OSMClientSecret:    getEnv("OSM_CLIENT_SECRET", ""),
		OSMRedirectURI:     getEnv("OSM_REDIRECT_URI", ""),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		RedisURL:           getEnv("REDIS_URL", "redis://localhost:6379"),
		RedisKeyPrefix:     getEnv("REDIS_KEY_PREFIX", ""),
		DeviceCodeExpiry:   getEnvAsInt("DEVICE_CODE_EXPIRY", 600),      // 10 minutes default
		DevicePollInterval: getEnvAsInt("DEVICE_POLL_INTERVAL", 5),      // 5 seconds default
		AllowedClientIDs:   parseClientIDs(getEnv("ALLOWED_CLIENT_IDS", "")),
		CacheFallbackTTL:   getEnvAsInt("CACHE_FALLBACK_TTL", 691200),   // 8 days default (for weekly scout meetings)
		RateLimitCaution:   getEnvAsInt("RATE_LIMIT_CAUTION", 200),      // Start extending cache TTL
		RateLimitWarning:   getEnvAsInt("RATE_LIMIT_WARNING", 100),      // Extend cache TTL further
		RateLimitCritical:  getEnvAsInt("RATE_LIMIT_CRITICAL", 20),      // Maximum cache TTL extension
	}

	// Validate required configuration
	if cfg.ExposedDomain == "" {
		return nil, fmt.Errorf("EXPOSED_DOMAIN is required")
	}
	if cfg.OSMClientID == "" {
		return nil, fmt.Errorf("OSM_CLIENT_ID is required")
	}
	if cfg.OSMClientSecret == "" {
		return nil, fmt.Errorf("OSM_CLIENT_SECRET is required")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.AllowedClientIDs) == 0 {
		return nil, fmt.Errorf("ALLOWED_CLIENT_IDS is required")
	}

	// Set OSM redirect URI if not explicitly provided
	if cfg.OSMRedirectURI == "" {
		cfg.OSMRedirectURI = fmt.Sprintf("%s/oauth/callback", cfg.ExposedDomain)
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func parseClientIDs(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := strings.Split(value, ",")
	clientIDs := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			clientIDs = append(clientIDs, trimmed)
		}
	}

	return clientIDs
}
