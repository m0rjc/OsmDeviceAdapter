package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/m0rjc/goconfig"
)

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port int    `key:"PORT" default:"8080" min:"1" max:"65535"`
	Host string `key:"HOST" default:"0.0.0.0"`
}

// ExternalDomainsConfig holds external domain configuration
type ExternalDomainsConfig struct {
	ExposedDomain string `key:"EXPOSED_DOMAIN" required:"true"` // The domain where this service is exposed
	OSMDomain     string `key:"OSM_DOMAIN" default:"https://www.onlinescoutmanager.co.uk"`
}

// OAuthConfig holds OAuth configuration for OSM
type OAuthConfig struct {
	OSMClientID     string `key:"OSM_CLIENT_ID" required:"true"`
	OSMClientSecret string `key:"OSM_CLIENT_SECRET" required:"true"`
	OSMRedirectURI  string `key:"OSM_REDIRECT_URI"` // Computed from ExposedDomain if not set
}

// DatabaseConfig holds database connection configuration
type DatabaseConfig struct {
	DatabaseURL string `key:"DATABASE_URL" required:"true"`
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	RedisURL       string `key:"REDIS_URL" default:"redis://localhost:6379"`
	RedisKeyPrefix string `key:"REDIS_KEY_PREFIX"`
}

// DeviceOAuthConfig holds device OAuth flow configuration
type DeviceOAuthConfig struct {
	DeviceCodeExpiry   int    `key:"DEVICE_CODE_EXPIRY" default:"300" min:"60"` // seconds (5 minutes default)
	DevicePollInterval int    `key:"DEVICE_POLL_INTERVAL" default:"5" min:"1"`  // seconds
	AllowedClientIDs   string `key:"ALLOWED_CLIENT_IDS" required:"true"`        // Comma-separated list
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	DeviceAuthorizeRateLimit int `key:"DEVICE_AUTHORIZE_RATE_LIMIT" default:"6" min:"1"` // max requests per minute per IP
	DeviceEntryRateLimit     int `key:"DEVICE_ENTRY_RATE_LIMIT" default:"5" min:"1"`     // seconds between entries
}

// CacheConfig holds cache configuration for patrol scores and other data
type CacheConfig struct {
	CacheFallbackTTL  int `key:"CACHE_FALLBACK_TTL" default:"691200" min:"0"` // seconds (8 days default for weekly scout meetings)
	RateLimitCaution  int `key:"RATE_LIMIT_CAUTION" default:"200" min:"0"`    // remaining requests threshold for caution
	RateLimitWarning  int `key:"RATE_LIMIT_WARNING" default:"100" min:"0"`    // remaining requests threshold for warning
	RateLimitCritical int `key:"RATE_LIMIT_CRITICAL" default:"20" min:"0"`    // remaining requests threshold for critical
}

// PathConfig holds configurable endpoint path prefixes
// These can be changed to make endpoints less predictable to automated scanners
type PathConfig struct {
	OAuthPrefix  string `key:"OAUTH_PATH_PREFIX" default:"/oauth"`   // OAuth web flow path prefix
	DevicePrefix string `key:"DEVICE_PATH_PREFIX" default:"/device"` // Device flow path prefix
	APIPrefix    string `key:"API_PATH_PREFIX" default:"/api"`       // API endpoints path prefix
}

// Config is the complete application configuration
type Config struct {
	Server          ServerConfig
	ExternalDomains ExternalDomainsConfig
	OAuth           OAuthConfig
	Database        DatabaseConfig
	Redis           RedisConfig
	DeviceOAuth     DeviceOAuthConfig
	RateLimit       RateLimitConfig
	Cache           CacheConfig
	Paths           PathConfig
}

// MinimalConfig is the minimal configuration needed for database cleanup jobs
type MinimalConfig struct {
	Database DatabaseConfig
	Redis    RedisConfig
}

// Load loads the complete application configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{}

	if err := goconfig.Load(context.Background(), cfg); err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Normalize path prefixes (remove trailing slashes)
	cfg.Paths.OAuthPrefix = strings.TrimSuffix(cfg.Paths.OAuthPrefix, "/")
	cfg.Paths.DevicePrefix = strings.TrimSuffix(cfg.Paths.DevicePrefix, "/")
	cfg.Paths.APIPrefix = strings.TrimSuffix(cfg.Paths.APIPrefix, "/")

	// Set OSM redirect URI if not explicitly provided
	if cfg.OAuth.OSMRedirectURI == "" {
		cfg.OAuth.OSMRedirectURI = fmt.Sprintf("%s%s/callback", cfg.ExternalDomains.ExposedDomain, cfg.Paths.OAuthPrefix)
	}

	return cfg, nil
}

// LoadMinimal loads only database and Redis configuration (for cleanup jobs)
func LoadMinimal() (*MinimalConfig, error) {
	cfg := &MinimalConfig{}

	if err := goconfig.Load(context.Background(), cfg); err != nil {
		return nil, fmt.Errorf("failed to load minimal configuration: %w", err)
	}

	return cfg, nil
}

// ParseClientIDs parses the comma-separated list of client IDs
func (d *DeviceOAuthConfig) ParseClientIDs() []string {
	if d.AllowedClientIDs == "" {
		return []string{}
	}

	parts := strings.Split(d.AllowedClientIDs, ",")
	clientIDs := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			clientIDs = append(clientIDs, trimmed)
		}
	}

	return clientIDs
}
