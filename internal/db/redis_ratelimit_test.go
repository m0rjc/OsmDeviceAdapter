package db

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) (*RedisClient, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	redisClient := &RedisClient{
		client:    client,
		keyPrefix: "test:",
	}

	return redisClient, mr
}

func TestCheckRateLimit_AllowsWithinLimit(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer mr.Close()

	ctx := context.Background()

	// Allow 5 requests per minute
	limit := int64(5)
	window := time.Minute

	// Make 5 requests - all should be allowed
	for i := int64(1); i <= 5; i++ {
		result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1:/device/authorize", limit, window)
		require.NoError(t, err)
		assert.True(t, result.Allowed, "Request %d should be allowed", i)
		assert.Equal(t, limit-i, result.Remaining, "Remaining count should be %d", limit-i)
	}
}

func TestCheckRateLimit_BlocksOverLimit(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer mr.Close()

	ctx := context.Background()

	limit := int64(3)
	window := time.Minute

	// Make 3 requests - all should be allowed
	for i := int64(1); i <= 3; i++ {
		result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1:/device/authorize", limit, window)
		require.NoError(t, err)
		assert.True(t, result.Allowed, "Request %d should be allowed", i)
	}

	// 4th request should be blocked
	result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1:/device/authorize", limit, window)
	require.NoError(t, err)
	assert.False(t, result.Allowed, "Request 4 should be blocked")
	assert.Equal(t, int64(0), result.Remaining)
	assert.Greater(t, result.RetryAfter, time.Duration(0), "RetryAfter should be set")
}

func TestCheckRateLimit_IndependentBuckets(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer mr.Close()

	ctx := context.Background()

	limit := int64(2)
	window := time.Minute

	// Make 2 requests from IP1
	result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1:/device/authorize", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1:/device/authorize", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// 3rd request from IP1 should be blocked
	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1:/device/authorize", limit, window)
	require.NoError(t, err)
	assert.False(t, result.Allowed)

	// But IP2 should still have full quota
	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.2:/device/authorize", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, int64(1), result.Remaining)
}

func TestCheckRateLimit_DifferentNames(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer mr.Close()

	ctx := context.Background()

	limit := int64(2)
	window := time.Minute

	// Make 2 requests with "auth" name
	result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// 3rd request with "auth" name should be blocked
	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.False(t, result.Allowed)

	// But "api" name should have independent quota
	result, err = redisClient.CheckRateLimit(ctx, "api", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, int64(1), result.Remaining)
}

func TestCheckRateLimit_ExpiresAfterWindow(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer mr.Close()

	ctx := context.Background()

	limit := int64(2)
	window := 2 * time.Second

	// Use up the quota
	result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// Should be blocked now
	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.False(t, result.Allowed)

	// Fast forward time in miniredis
	mr.FastForward(3 * time.Second)

	// Should be allowed again after window expires
	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Request should be allowed after window expires")
	assert.Equal(t, int64(1), result.Remaining)
}

func TestResetRateLimit(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer mr.Close()

	ctx := context.Background()

	limit := int64(2)
	window := time.Minute

	// Use up the quota
	result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// Should be blocked
	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.False(t, result.Allowed)

	// Reset the rate limit
	err = redisClient.ResetRateLimit(ctx, "auth", "192.168.1.1")
	require.NoError(t, err)

	// Should be allowed again
	result, err = redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Request should be allowed after reset")
	assert.Equal(t, int64(1), result.Remaining)
}

func TestCheckRateLimit_KeyPrefix(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer mr.Close()

	ctx := context.Background()

	limit := int64(1)
	window := time.Minute

	// Make a request
	result, err := redisClient.CheckRateLimit(ctx, "auth", "192.168.1.1", limit, window)
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// Verify the key in Redis has the prefix
	keys := mr.Keys()
	require.Len(t, keys, 1)
	assert.Contains(t, keys[0], "test:ratelimit:auth:192.168.1.1", "Key should have prefix")
}
