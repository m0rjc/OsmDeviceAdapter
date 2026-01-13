package db

import (
	"context"
	"time"
)

// RateLimiter defines the interface for rate limiting operations
type RateLimiter interface {
	// CheckRateLimit checks if a request is within the rate limit
	CheckRateLimit(ctx context.Context, name, key string, limit int64, window time.Duration) (*RateLimitResult, error)

	// ResetRateLimit manually resets a rate limit bucket
	ResetRateLimit(ctx context.Context, name, key string) error
}

// Ensure RedisClient implements RateLimiter interface
var _ RateLimiter = (*RedisClient)(nil)
