package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/redis/go-redis/v9"
)

const (
	osmServiceBlockedKey   = "osm:blocked:service"
	osmUserBlockedPrefix   = "osm:blocked:user:"
	osmUserRateLimitPrefix = "osm:ratelimit:user:"

	// In-memory cache TTL for rate limit info (30 seconds)
	rateLimitCacheTTL = 30 * time.Second
	// Redis TTL for rate limit info (5 minutes)
	rateLimitRedisTTL = 5 * time.Minute
)

// MarkOsmServiceBlocked marks the OSM service as blocked.
func (r *RedisClient) MarkOsmServiceBlocked(ctx context.Context) {
	r.Set(ctx, osmServiceBlockedKey, "1", 0)
}

// IsOsmServiceBlocked returns true if the OSM service is marked as blocked.
func (r *RedisClient) IsOsmServiceBlocked(ctx context.Context) bool {
	val, err := r.Get(ctx, osmServiceBlockedKey).Result()
	return err == nil && val == "1"
}

// MarkUserTemporarilyBlocked marks a user as temporarily blocked until the specified time.
func (r *RedisClient) MarkUserTemporarilyBlocked(ctx context.Context, userId int, blockedUntil time.Time) {
	key := r.getUserBlockKey(userId)
	// Calculate how long from now until the block expires
	ttl := time.Until(blockedUntil)
	if ttl > 0 {
		r.Set(ctx, key, blockedUntil.Format(time.RFC3339), ttl)

		// Invalidate cached rate limit info for this user
		r.cacheMutex.Lock()
		delete(r.rateLimitCache, userId)
		r.cacheMutex.Unlock()
	}
}

// IsUserTemporarilyBlocked returns true if the user is currently blocked.
func (r *RedisClient) IsUserTemporarilyBlocked(userId int) bool {
	ctx := context.Background()
	key := r.getUserBlockKey(userId)
	val, err := r.Get(ctx, key).Result()
	return err == nil && val == "1"
}

func (r *RedisClient) getUserBlockKey(userId int) string {
	return osmUserBlockedPrefix + strconv.Itoa(userId)
}

// RecordOsmLatency records the latency and status of an OSM request.
func (r *RedisClient) RecordOsmLatency(endpoint string, statusCode int, latency time.Duration) {
	if statusCode == 200 {
		// If we got a 200, ensure the service blocked flag is cleared in Redis.
		r.Del(context.Background(), osmServiceBlockedKey)
	}
}

// UpdateUserRateLimit stores the current rate limit information for a user
func (r *RedisClient) UpdateUserRateLimit(ctx context.Context, userId int, remaining, limit, resetSeconds int) {
	now := time.Now()

	// Get block info (stored separately in the block key)
	blockedUntil := r.getUserBlockEndTime(ctx, userId)
	isBlocked := !blockedUntil.IsZero() && blockedUntil.After(now)

	info := &osm.UserRateLimitInfo{
		Remaining:    remaining,
		Limit:        limit,
		ResetSeconds: resetSeconds,
		IsBlocked:    isBlocked,
		BlockedUntil: blockedUntil,
		LastUpdated:  now,
	}

	// Store in Redis
	key := r.getUserRateLimitKey(userId)
	data, err := json.Marshal(info)
	if err != nil {
		return
	}
	r.Set(ctx, key, data, rateLimitRedisTTL)

	// Update in-memory cache
	r.cacheMutex.Lock()
	r.rateLimitCache[userId] = &cachedRateLimitInfo{
		info:      info,
		expiresAt: now.Add(rateLimitCacheTTL),
	}
	r.cacheMutex.Unlock()
}

// GetUserRateLimitInfo retrieves the current rate limit state for a user
func (r *RedisClient) GetUserRateLimitInfo(ctx context.Context, userId int) (*osm.UserRateLimitInfo, error) {
	now := time.Now()

	// Check in-memory cache first
	r.cacheMutex.RLock()
	cached, exists := r.rateLimitCache[userId]
	r.cacheMutex.RUnlock()

	if exists && now.Before(cached.expiresAt) {
		// Update block status in case it changed
		isBlocked := r.IsUserTemporarilyBlocked(userId)
		if isBlocked != cached.info.IsBlocked {
			// Block status changed, invalidate cache
			r.cacheMutex.Lock()
			delete(r.rateLimitCache, userId)
			r.cacheMutex.Unlock()
		} else {
			return cached.info, nil
		}
	}

	// Cache miss or expired - fetch from Redis
	key := r.getUserRateLimitKey(userId)
	data, err := r.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // No rate limit info available
		}
		return nil, fmt.Errorf("failed to get rate limit info from Redis: %w", err)
	}

	var info osm.UserRateLimitInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rate limit info: %w", err)
	}

	// Update block status (it might have changed)
	blockedUntil := r.getUserBlockEndTime(ctx, userId)
	info.IsBlocked = !blockedUntil.IsZero() && blockedUntil.After(now)
	info.BlockedUntil = blockedUntil

	// Store in cache
	r.cacheMutex.Lock()
	r.rateLimitCache[userId] = &cachedRateLimitInfo{
		info:      &info,
		expiresAt: now.Add(rateLimitCacheTTL),
	}
	r.cacheMutex.Unlock()

	return &info, nil
}

func (r *RedisClient) getUserRateLimitKey(userId int) string {
	return osmUserRateLimitPrefix + strconv.Itoa(userId)
}

// getUserBlockEndTime retrieves the block end time for a user from Redis.
// Returns zero time if the user is not blocked.
func (r *RedisClient) getUserBlockEndTime(ctx context.Context, userId int) time.Time {
	key := r.getUserBlockKey(userId)
	val, err := r.Get(ctx, key).Result()
	if err != nil {
		return time.Time{} // Not blocked or error
	}

	// Parse the stored timestamp
	blockedUntil, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}

	return blockedUntil
}
