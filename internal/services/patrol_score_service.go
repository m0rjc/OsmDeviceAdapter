package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"github.com/redis/go-redis/v9"
)

// RateLimitState represents the current rate limiting state
type RateLimitState string

const (
	RateLimitStateNone     RateLimitState = "NONE"     // Normal operation (remaining > 200)
	RateLimitStateDegraded RateLimitState = "DEGRADED" // Rate limit approaching (remaining < 200)
	RateLimitStateBlocked  RateLimitState = "BLOCKED"  // Temporarily blocked (HTTP 429)
)

// CachedPatrolScores represents cached patrol score data with metadata
type CachedPatrolScores struct {
	Patrols    []types.PatrolScore `json:"patrols"`
	CachedAt   time.Time           `json:"cached_at"`
	ValidUntil time.Time           `json:"valid_until"`
}

// PatrolScoreResponse represents the API response for patrol scores
type PatrolScoreResponse struct {
	Patrols         []types.PatrolScore `json:"patrols"`
	FromCache       bool                `json:"from_cache"`
	CachedAt        time.Time           `json:"cached_at"`
	CacheExpiresAt  time.Time           `json:"cache_expires_at"`
	RateLimitState  RateLimitState      `json:"rate_limit_state"`
}

// PatrolScoreService orchestrates patrol score fetching with caching and rate limiting
type PatrolScoreService struct {
	osmClient   *osm.Client
	redisClient *db.RedisClient
	conns       *db.Connections
	config      *config.Config
}

// NewPatrolScoreService creates a new patrol score service
func NewPatrolScoreService(
	osmClient *osm.Client,
	redisClient *db.RedisClient,
	conns *db.Connections,
	cfg *config.Config,
) *PatrolScoreService {
	return &PatrolScoreService{
		osmClient:   osmClient,
		redisClient: redisClient,
		conns:       conns,
		config:      cfg,
	}
}

// GetPatrolScores fetches patrol scores for a device, managing term discovery,
// caching, and rate limiting automatically.
func (s *PatrolScoreService) GetPatrolScores(ctx context.Context, deviceCode string) (*PatrolScoreResponse, error) {
	// Get device from database
	device, err := db.FindDeviceCodeByCode(s.conns, deviceCode)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if device == nil {
		return nil, fmt.Errorf("device not found")
	}

	if device.SectionID == nil {
		return nil, osm.ErrNoSectionConfigured
	}

	user := device.User()
	if user == nil {
		return nil, fmt.Errorf("device not authorized")
	}

	// Check for global service block first
	if s.redisClient.IsOsmServiceBlocked(ctx) {
		// Try to serve stale cache
		return s.serveStaleCache(ctx, deviceCode, RateLimitStateBlocked)
	}

	// Check for user-specific temporary block
	if device.OsmUserID != nil && s.redisClient.IsUserTemporarilyBlocked(*device.OsmUserID) {
		// Try to serve stale cache
		return s.serveStaleCache(ctx, deviceCode, RateLimitStateBlocked)
	}

	// Check patrol scores cache
	cached, err := s.getCachedPatrolScores(ctx, deviceCode)
	if err == nil && time.Now().Before(cached.ValidUntil) {
		// Cache is still valid
		rateLimitState := s.determineRateLimitState(ctx, device.OsmUserID)
		return &PatrolScoreResponse{
			Patrols:        cached.Patrols,
			FromCache:      true,
			CachedAt:       cached.CachedAt,
			CacheExpiresAt: cached.ValidUntil,
			RateLimitState: rateLimitState,
		}, nil
	}

	// Cache miss or expired - need to fetch fresh data
	// First, ensure we have term information
	termID, err := s.ensureTermInfo(ctx, device)
	if err != nil {
		return nil, err
	}

	// Fetch patrol scores from OSM
	patrols, err := s.osmClient.FetchPatrolScores(ctx, user, *device.SectionID, termID)
	if err != nil {
		// If fetch failed, try to serve stale cache as fallback
		if errors.Is(err, osm.ErrServiceBlocked) || errors.Is(err, osm.ErrTemporaryBlocked) {
			return s.serveStaleCache(ctx, deviceCode, RateLimitStateBlocked)
		}
		return nil, fmt.Errorf("failed to fetch patrol scores: %w", err)
	}

	// Determine cache TTL based on current rate limiting state
	rateLimitState := s.determineRateLimitState(ctx, device.OsmUserID)
	cacheTTL := s.calculateCacheTTL(rateLimitState)

	// Cache the results with two-tier strategy
	now := time.Now()
	validUntil := now.Add(cacheTTL)
	err = s.cachePatrolScores(ctx, deviceCode, patrols, now, validUntil)
	if err != nil {
		slog.Warn("patrol_score_service.cache_failed",
			"component", "patrol_score_service",
			"event", "cache.error",
			"device_code_hash", deviceCode[:8],
			"error", err,
		)
	}

	return &PatrolScoreResponse{
		Patrols:        patrols,
		FromCache:      false,
		CachedAt:       now,
		CacheExpiresAt: validUntil,
		RateLimitState: rateLimitState,
	}, nil
}

// ensureTermInfo ensures that the device has valid term information.
// It refreshes the term if needed (24 hours old or expired).
func (s *PatrolScoreService) ensureTermInfo(ctx context.Context, device *db.DeviceCode) (int, error) {
	now := time.Now()

	// Check if we need to refresh term information
	needsRefresh := device.TermID == nil ||
		device.TermCheckedAt == nil ||
		device.TermEndDate == nil ||
		now.After(device.TermCheckedAt.Add(24*time.Hour)) ||
		now.After(*device.TermEndDate)

	if !needsRefresh {
		return *device.TermID, nil
	}

	// Fetch fresh term information
	user := device.User()
	termInfo, err := s.osmClient.FetchActiveTermForSection(ctx, user, *device.SectionID)
	if err != nil {
		return 0, err
	}

	// Update device with new term information
	now = time.Now()
	if err := db.UpdateDeviceCodeTermInfo(s.conns, device.DeviceCode, termInfo.UserID, termInfo.TermID, now, termInfo.EndDate); err != nil {
		slog.Error("patrol_score_service.term_update_failed",
			"component", "patrol_score_service",
			"event", "term.update.error",
			"device_code_hash", device.DeviceCode[:8],
			"error", err,
		)
		// Continue anyway - we have the term ID
	}

	return termInfo.TermID, nil
}

// determineRateLimitState determines the current rate limiting state for a user
func (s *PatrolScoreService) determineRateLimitState(ctx context.Context, userID *int) RateLimitState {
	if userID == nil {
		return RateLimitStateNone
	}

	// Get current rate limit info from the store
	rateLimitInfo, err := s.redisClient.GetUserRateLimitInfo(ctx, *userID)
	if err != nil {
		slog.Warn("patrol_score_service.rate_limit_info_error",
			"component", "patrol_score_service",
			"event", "rate_limit.error",
			"user_id", *userID,
			"error", err,
		)
		return RateLimitStateNone
	}

	// No rate limit info available yet
	if rateLimitInfo == nil {
		return RateLimitStateNone
	}

	// Check if user is blocked
	if rateLimitInfo.IsBlocked {
		return RateLimitStateBlocked
	}

	// Check if rate limit is getting low (< 200 requests remaining)
	if rateLimitInfo.Remaining > 0 && rateLimitInfo.Remaining < 200 {
		return RateLimitStateDegraded
	}

	return RateLimitStateNone
}

// calculateCacheTTL calculates the cache TTL based on rate limiting state
func (s *PatrolScoreService) calculateCacheTTL(state RateLimitState) time.Duration {
	baselineTTL := 5 * time.Minute

	switch state {
	case RateLimitStateBlocked:
		// Critical: 30 minute cache
		return 30 * time.Minute
	case RateLimitStateDegraded:
		// Degraded: 15 minute cache
		return 15 * time.Minute
	default:
		return baselineTTL
	}
}

// getCachedPatrolScores retrieves patrol scores from cache
func (s *PatrolScoreService) getCachedPatrolScores(ctx context.Context, deviceCode string) (*CachedPatrolScores, error) {
	key := fmt.Sprintf("patrol_scores:%s", deviceCode)
	data, err := s.redisClient.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("cache miss")
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
func (s *PatrolScoreService) cachePatrolScores(
	ctx context.Context,
	deviceCode string,
	patrols []types.PatrolScore,
	cachedAt, validUntil time.Time,
) error {
	cached := CachedPatrolScores{
		Patrols:    patrols,
		CachedAt:   cachedAt,
		ValidUntil: validUntil,
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	key := fmt.Sprintf("patrol_scores:%s", deviceCode)
	// Use fallback TTL for Redis (8 days) to keep stale data for emergency use
	fallbackTTL := time.Duration(s.config.CacheFallbackTTL) * time.Second
	return s.redisClient.Set(ctx, key, data, fallbackTTL).Err()
}

// serveStaleCache attempts to serve stale cached data when blocked
func (s *PatrolScoreService) serveStaleCache(ctx context.Context, deviceCode string, state RateLimitState) (*PatrolScoreResponse, error) {
	cached, err := s.getCachedPatrolScores(ctx, deviceCode)
	if err != nil {
		// No cache available
		return nil, fmt.Errorf("service blocked and no cached data available")
	}

	slog.Warn("patrol_score_service.serving_stale_cache",
		"component", "patrol_score_service",
		"event", "cache.stale",
		"device_code_hash", deviceCode[:8],
		"cached_at", cached.CachedAt,
		"valid_until", cached.ValidUntil,
		"age_minutes", time.Since(cached.CachedAt).Minutes(),
	)

	return &PatrolScoreResponse{
		Patrols:        cached.Patrols,
		FromCache:      true,
		CachedAt:       cached.CachedAt,
		CacheExpiresAt: cached.ValidUntil,
		RateLimitState: state,
	}, nil
}
