package db

import (
	"context"
	"strconv"
	"time"
)

const (
	osmServiceBlockedKey = "osm:blocked:service"
	osmUserBlockedPrefix = "osm:blocked:user:"
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
	}
}

func (r *RedisClient) getUserBlockKey(userId int) string {
	return osmUserBlockedPrefix + strconv.Itoa(userId)
}

// GetUserBlockEndTime retrieves the block end time for a user from Redis.
// Returns zero time if the user is not blocked.
func (r *RedisClient) GetUserBlockEndTime(ctx context.Context, userId int) time.Time {
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
