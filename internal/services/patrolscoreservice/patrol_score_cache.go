package patrolscoreservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrPatrolCacheMiss = errors.New("patrol cache miss")

// getCachedPatrolScores retrieves patrol scores from cache
// TODO: This needs to be a store method
func (s *PatrolScoreService) getCachedPatrolScores(ctx context.Context, userId *int, sectionId int) (*CachedPatrolScores, error) {
	if userId == nil {
		// If we have no userId we cannot cache, so consider it a miss.
		// We shouldn't be in that situation.
		slog.Warn("patrol_score_service.getCachedPatrolScores: userId is nil")
		return nil, ErrPatrolCacheMiss
	}
	key := s.cacheKey(*userId, sectionId)
	data, err := s.conns.Redis.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrPatrolCacheMiss
		}
		return nil, err
	}

	var cached CachedPatrolScores
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return &cached, nil
}

// cachePatrolScores stores patrol scores in cache with two-tier TTL strategy
// This is a best effort. Errors are logged but not returned as loss of cache is not fatal.
func (s *PatrolScoreService) cachePatrolScores(
	ctx context.Context,
	userId *int,
	sectionId int,
	cacheRecord *CachedPatrolScores,
) {
	if userId == nil {
		slog.Warn("patrol_score_cache: userId is nil")
		return
	}

	data, err := json.Marshal(cacheRecord)
	if err != nil {
		slog.Error("patrol_score_service.cachePatrolScores", "message", "cannot marshal cache record", "error", err)
	}

	key := s.cacheKey(*userId, sectionId)
	// Use fallback TTL for Redis (8 days) to keep stale data for emergency use
	// TODO: Configure this as a Duration
	fallbackTTL := time.Duration(s.config.Cache.CacheFallbackTTL) * time.Second
	err = s.conns.Redis.Set(ctx, key, data, fallbackTTL).Err()
	if err != nil {
		slog.Error("patrol_score_service.cachePatrolScores", "message", "cannot write to REDIS cache", "error", err)
	}
}

func (s *PatrolScoreService) cacheKey(userId, sectionId int) string {
	return fmt.Sprintf("patrol_scores:%d:%d", userId, sectionId)
}
