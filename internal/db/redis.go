package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/redis/go-redis/v9"
)

// cachedRateLimitInfo holds rate limit info with expiry for in-memory caching
type cachedRateLimitInfo struct {
	info      *osm.UserRateLimitInfo
	expiresAt time.Time
}

type RedisClient struct {
	client    *redis.Client
	keyPrefix string

	// In-memory cache for rate limit info to reduce Redis hits
	rateLimitCache map[int]*cachedRateLimitInfo
	cacheMutex     sync.RWMutex
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
		client:         client,
		keyPrefix:      keyPrefix,
		rateLimitCache: make(map[int]*cachedRateLimitInfo),
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
