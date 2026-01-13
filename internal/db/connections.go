package db

import "gorm.io/gorm"

// Connections holds database and cache connections
type Connections struct {
	DB          *gorm.DB
	Redis       *RedisClient
	RateLimiter RateLimiter // Rate limiter (defaults to Redis if nil, can be mocked for testing)
}

// NewConnections creates a new Connections instance
func NewConnections(db *gorm.DB, redis *RedisClient) *Connections {
	return &Connections{
		DB:          db,
		Redis:       redis,
		RateLimiter: redis, // Default to using Redis as the rate limiter
	}
}

// GetRateLimiter returns the rate limiter to use (Redis if RateLimiter is nil)
func (c *Connections) GetRateLimiter() RateLimiter {
	if c.RateLimiter != nil {
		return c.RateLimiter
	}
	return c.Redis
}
