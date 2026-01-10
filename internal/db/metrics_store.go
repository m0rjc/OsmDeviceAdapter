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

// MarkUserTemporarilyBlocked marks a user as temporarily blocked.
func (r *RedisClient) MarkUserTemporarilyBlocked(ctx context.Context, userId int, retryAfter time.Duration) {
	key := r.getUserBlockKey(userId)
	r.Set(ctx, key, "1", retryAfter)
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
