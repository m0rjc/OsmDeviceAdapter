package db

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	client    *redis.Client
	keyPrefix string
}

func NewRedisClient(redisURL string, keyPrefix string) (*RedisClient, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// Test the connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping Redis: %w", err)
	}

	return &RedisClient{
		client:    client,
		keyPrefix: keyPrefix,
	}, nil
}

func (r *RedisClient) Close() error {
	return r.client.Close()
}

func (r *RedisClient) Client() *redis.Client {
	return r.client
}

// prefixKey adds the configured prefix to a key
func (r *RedisClient) prefixKey(key string) string {
	if r.keyPrefix == "" {
		return key
	}
	return r.keyPrefix + key
}

// Get retrieves a value from Redis with the configured key prefix
func (r *RedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	return r.client.Get(ctx, r.prefixKey(key))
}

// Set stores a value in Redis with the configured key prefix
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	return r.client.Set(ctx, r.prefixKey(key), value, expiration)
}

// Del deletes a key from Redis with the configured key prefix
func (r *RedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.prefixKey(key)
	}
	return r.client.Del(ctx, prefixedKeys...)
}

// SetNX sets a value only if the key does not exist (with key prefix)
func (r *RedisClient) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	return r.client.SetNX(ctx, r.prefixKey(key), value, expiration)
}

// Eval executes a Lua script with prefixed keys
func (r *RedisClient) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.prefixKey(key)
	}
	return r.client.Eval(ctx, script, prefixedKeys, args...)
}

// RateLimitResult contains the result of a rate limit check
type RateLimitResult struct {
	Allowed   bool          // Whether the request is allowed
	Remaining int64         // Remaining requests in the current window
	RetryAfter time.Duration // Time until the rate limit resets (0 if allowed)
}

// CheckRateLimit checks if a request is within the rate limit using a sliding window counter.
// It returns a RateLimitResult indicating whether the request is allowed.
//
// Parameters:
//   - ctx: Context for the operation
//   - name: Rate limit name (e.g., "auth")
//   - key: Unique identifier for this rate limit bucket (e.g., "192.168.1.1:/device/authorize")
//   - limit: Maximum number of requests allowed in the window
//   - window: Time window for the rate limit
//
// Example usage:
//   result, err := redis.CheckRateLimit(ctx, "auth", "192.168.1.1:/device/authorize", 10, time.Minute)
func (r *RedisClient) CheckRateLimit(ctx context.Context, name, key string, limit int64, window time.Duration) (*RateLimitResult, error) {
	rateLimitKey := r.prefixKey(fmt.Sprintf("ratelimit:%s:%s", name, key))

	// Use a Lua script for atomic increment and TTL check
	script := redis.NewScript(`
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])

		local current = redis.call('INCR', key)

		if current == 1 then
			redis.call('EXPIRE', key, window)
		end

		local ttl = redis.call('TTL', key)
		if ttl == -1 then
			-- Key exists but has no expiry (shouldn't happen, but handle it)
			redis.call('EXPIRE', key, window)
			ttl = window
		end

		return {current, ttl}
	`)

	windowSeconds := int64(window.Seconds())
	result, err := script.Run(ctx, r.client, []string{rateLimitKey}, limit, windowSeconds).Result()
	if err != nil {
		return nil, fmt.Errorf("rate limit check failed: %w", err)
	}

	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 2 {
		return nil, fmt.Errorf("unexpected rate limit script result")
	}

	current, ok := resultSlice[0].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected current count type")
	}

	ttl, ok := resultSlice[1].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected TTL type")
	}

	allowed := current <= limit
	remaining := limit - current
	if remaining < 0 {
		remaining = 0
	}

	return &RateLimitResult{
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: time.Duration(ttl) * time.Second,
	}, nil
}

// ResetRateLimit manually resets a rate limit bucket. Useful for testing or administrative purposes.
func (r *RedisClient) ResetRateLimit(ctx context.Context, name, key string) error {
	rateLimitKey := r.prefixKey(fmt.Sprintf("ratelimit:%s:%s", name, key))
	return r.client.Del(ctx, rateLimitKey).Err()
}
