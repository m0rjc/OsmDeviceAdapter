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
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/adhocpatrol"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/devicecode"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/sectionsettings"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"github.com/redis/go-redis/v9"
)

// RateLimitState represents the current rate limiting state
type RateLimitState string

const (
	RateLimitStateNone              RateLimitState = "NONE"                // Normal operation (remaining > 200)
	RateLimitStateDegraded          RateLimitState = "DEGRADED"            // Rate limit approaching (remaining < 200)
	RateLimitStateUserTemporaryBlock RateLimitState = "USER_TEMPORARY_BLOCK" // User temporarily blocked (HTTP 429)
	RateLimitStateServiceBlocked    RateLimitState = "SERVICE_BLOCKED"     // Service blocked by OSM (X-Blocked header)
)

// CachedPatrolScores represents cached patrol score data with metadata
type CachedPatrolScores struct {
	Patrols        []types.PatrolScore `json:"patrols"`
	CachedAt       time.Time           `json:"cached_at"`
	ValidUntil     time.Time           `json:"valid_until"`
	RateLimitState RateLimitState      `json:"rate_limit_state"`
}

// WebSocketInfo is included in every patrol score response to signal WebSocket availability.
type WebSocketInfo struct {
	Requested bool `json:"requested"`
}

// PatrolScoreResponse represents the API response for patrol scores
type PatrolScoreResponse struct {
	Patrols        []types.PatrolScore   `json:"patrols"`
	FromCache      bool                  `json:"from_cache"`
	CachedAt       time.Time             `json:"cached_at"`
	CacheExpiresAt time.Time             `json:"cache_expires_at"`
	RateLimitState RateLimitState        `json:"rate_limit_state"`
	Settings       *types.DeviceSettings `json:"settings,omitempty"`
	WebSocket      WebSocketInfo         `json:"websocket"`
}

// PatrolScoreService orchestrates patrol score fetching with caching and rate limiting
type PatrolScoreService struct {
	osmClient *osm.Client
	conns     *db.Connections
	config    *config.Config
}

// NewPatrolScoreService creates a new patrol score service
func NewPatrolScoreService(
	osmClient *osm.Client,
	conns *db.Connections,
	cfg *config.Config,
) *PatrolScoreService {
	return &PatrolScoreService{
		osmClient: osmClient,
		conns:     conns,
		config:    cfg,
	}
}

// GetPatrolScores fetches patrol scores for a device, managing term discovery,
// caching, and rate limiting automatically.
// Accepts user and device from the authentication middleware to avoid redundant database queries.
func (s *PatrolScoreService) GetPatrolScores(ctx context.Context, user types.User, device *db.DeviceCode) (*PatrolScoreResponse, error) {
	var err error

	if device.SectionID == nil {
		return nil, osm.ErrNoSectionConfigured
	}

	// Ad-hoc section: serve from local database instead of OSM
	if *device.SectionID == 0 {
		return s.getAdhocPatrolScores(ctx, device)
	}

	// Fetch device settings (best effort - settings errors don't fail the request)
	settings := s.fetchDeviceSettings(device)

	// Check patrol scores cache
	cached, err := s.getCachedPatrolScores(ctx, device.DeviceCode)
	if err == nil && time.Now().Before(cached.ValidUntil) {
		// Cache is still valid
		return &PatrolScoreResponse{
			Patrols:        cached.Patrols,
			FromCache:      true,
			CachedAt:       cached.CachedAt,
			CacheExpiresAt: cached.ValidUntil,
			RateLimitState: cached.RateLimitState,
			Settings:       settings,
			WebSocket:      WebSocketInfo{Requested: true},
		}, nil
	}

	// Cache miss or expired - need to fetch fresh data
	// First, ensure we have term information
	var termID int
	var patrols []types.PatrolScore
	var rateLimitInfo osm.UserRateLimitInfo
	termID, err = s.ensureTermInfo(ctx, user, device)
	if err == nil {
		// Fetch patrol scores from OSM
		patrols, rateLimitInfo, err = s.osmClient.FetchPatrolScores(ctx, user, *device.SectionID, termID)
	}
	if err != nil {
		// Try to make the cache last long enough if we have one
		cacheUntil := time.Now().Add(10 * time.Minute) // TODO: Configure. This is the fallback block time if we can't deduce it.
		rateLimitState := RateLimitStateUserTemporaryBlock // Default assumption

		var blockedError *osm.ErrUserBlocked
		if errors.As(err, &blockedError) {
			cacheUntil = blockedError.BlockedUntil
			rateLimitState = RateLimitStateUserTemporaryBlock
		} else if errors.Is(err, osm.ErrServiceBlocked) {
			rateLimitState = RateLimitStateServiceBlocked
		}

		// If fetch failed, try to serve stale cache as fallback
		if cached != nil {
			// Extend the cache time if needed to cover any block
			if cached.ValidUntil.Before(cacheUntil) {
				cached.ValidUntil = cacheUntil
				s.cachePatrolScores(ctx, device.DeviceCode, cached)
			}
			return &PatrolScoreResponse{
				Patrols:        cached.Patrols,
				FromCache:      true,
				CachedAt:       cached.CachedAt,
				CacheExpiresAt: cached.ValidUntil,
				RateLimitState: rateLimitState,
				Settings:       settings,
				WebSocket:      WebSocketInfo{Requested: true},
			}, nil
		}
		return nil, fmt.Errorf("failed to fetch patrol scores: %w", err)
	}

	// Determine cache TTL based on current rate limiting state
	rateLimitState := s.determineRateLimitState(rateLimitInfo.Remaining)
	cacheTTL := s.calculateCacheTTL(rateLimitInfo.Remaining)

	// Cache the results with two-tier strategy
	// Caching is best effort
	now := time.Now()
	validUntil := now.Add(cacheTTL)
	s.cachePatrolScores(ctx, device.DeviceCode, &CachedPatrolScores{
		Patrols:        patrols,
		CachedAt:       now,
		ValidUntil:     validUntil,
		RateLimitState: rateLimitState,
	})

	return &PatrolScoreResponse{
		Patrols:        patrols,
		FromCache:      false,
		CachedAt:       now,
		CacheExpiresAt: validUntil,
		RateLimitState: rateLimitState,
		Settings:       settings,
		WebSocket:      WebSocketInfo{Requested: true},
	}, nil
}

// fetchDeviceSettings fetches user settings for the device's section.
// Returns nil if settings cannot be fetched (best effort - never fails the request).
func (s *PatrolScoreService) fetchDeviceSettings(device *db.DeviceCode) *types.DeviceSettings {
	if device.OsmUserID == nil || device.SectionID == nil {
		return nil
	}

	settings, err := sectionsettings.GetParsed(s.conns, *device.OsmUserID, *device.SectionID)
	if err != nil {
		slog.Error("patrol_score_service.settings_fetch_failed",
			"component", "patrol_score_service",
			"event", "settings.fetch.error",
			"device_code_hash", device.DeviceCode[:8],
			"error", err,
		)
		return nil
	}

	// Only return settings if there's actual content
	if len(settings.PatrolColors) == 0 {
		return nil
	}

	return &types.DeviceSettings{
		PatrolColors: settings.PatrolColors,
	}
}

// getAdhocPatrolScores returns patrol scores from the local ad-hoc patrols table.
// Uses a short Redis cache (15 seconds) to avoid hitting the database on every poll.
func (s *PatrolScoreService) getAdhocPatrolScores(ctx context.Context, device *db.DeviceCode) (*PatrolScoreResponse, error) {
	if device.OsmUserID == nil {
		return nil, fmt.Errorf("device has no user ID")
	}

	cacheKey := fmt.Sprintf("adhoc_scores:%d", *device.OsmUserID)
	const adhocCacheTTL = 15 * time.Second

	// Try cache first
	data, err := s.conns.Redis.Get(ctx, cacheKey).Result()
	if err == nil {
		var cached CachedPatrolScores
		if json.Unmarshal([]byte(data), &cached) == nil && time.Now().Before(cached.ValidUntil) {
			return &PatrolScoreResponse{
				Patrols:        cached.Patrols,
				FromCache:      true,
				CachedAt:       cached.CachedAt,
				CacheExpiresAt: cached.ValidUntil,
				RateLimitState: RateLimitStateNone,
				WebSocket:      WebSocketInfo{Requested: true},
			}, nil
		}
	}

	// Fetch from database
	patrols, err := adhocpatrol.ListByUser(s.conns, *device.OsmUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ad-hoc patrols: %w", err)
	}

	scores := make([]types.PatrolScore, len(patrols))
	for i, p := range patrols {
		scores[i] = types.PatrolScore{
			ID:    fmt.Sprintf("%d", p.ID),
			Name:  p.Name,
			Score: p.Score,
		}
	}

	// Build settings from patrol colors
	var settings *types.DeviceSettings
	patrolColors := make(map[string]string)
	for _, p := range patrols {
		if p.Color != "" {
			patrolColors[fmt.Sprintf("%d", p.ID)] = p.Color
		}
	}
	if len(patrolColors) > 0 {
		settings = &types.DeviceSettings{PatrolColors: patrolColors}
	}

	// Cache the result
	now := time.Now()
	validUntil := now.Add(adhocCacheTTL)
	cacheRecord := &CachedPatrolScores{
		Patrols:        scores,
		CachedAt:       now,
		ValidUntil:     validUntil,
		RateLimitState: RateLimitStateNone,
	}
	if cacheData, err := json.Marshal(cacheRecord); err == nil {
		s.conns.Redis.Set(ctx, cacheKey, cacheData, adhocCacheTTL)
	}

	return &PatrolScoreResponse{
		Patrols:        scores,
		FromCache:      false,
		CachedAt:       now,
		CacheExpiresAt: validUntil,
		RateLimitState: RateLimitStateNone,
		Settings:       settings,
		WebSocket:      WebSocketInfo{Requested: true},
	}, nil
}

// ensureTermInfo ensures that the device has valid term information.
// It refreshes the term if needed (24 hours old or expired).
// TODO: Move this into a separate Term Service. We're going to have to fix dependency injection. I think we'll
// need to borrow GPS-Game's Command Pattern
func (s *PatrolScoreService) ensureTermInfo(ctx context.Context, user types.User, device *db.DeviceCode) (int, error) {
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
	termInfo, err := s.osmClient.FetchActiveTermForSection(ctx, user, *device.SectionID)
	if err != nil {
		return 0, err
	}

	// Update device with new term information
	now = time.Now()
	if err := devicecode.UpdateTermInfo(s.conns, device.DeviceCode, termInfo.UserID, termInfo.TermID, now, termInfo.EndDate); err != nil {
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

// calculateCacheTTL calculates the cache TTL based on absolute rate limit remaining count.
// Uses adaptive caching strategy with absolute thresholds:
// - > 500 remaining: 1 minute (fresh data when capacity available)
// - 200-500: 5 minutes (baseline)
// - 100-200: 10 minutes (starting to conserve)
// - 50-100: 15 minutes (more conservative)
// - < 50: 30 minutes (very conservative)
func (s *PatrolScoreService) calculateCacheTTL(remaining int) time.Duration {
	switch {
	case remaining > 500:
		return 1 * time.Minute
	case remaining >= 200:
		return 5 * time.Minute
	case remaining >= 100:
		return 10 * time.Minute
	case remaining >= 50:
		return 15 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// determineRateLimitState determines the rate limit state based on remaining requests.
// This is used for reporting in the API response.
func (s *PatrolScoreService) determineRateLimitState(remaining int) RateLimitState {
	if remaining >= 200 {
		return RateLimitStateNone
	}
	return RateLimitStateDegraded
}

// getCachedPatrolScores retrieves patrol scores from cache
func (s *PatrolScoreService) getCachedPatrolScores(ctx context.Context, deviceCode string) (*CachedPatrolScores, error) {
	// TODO: This needs to be a store method
	key := fmt.Sprintf("patrol_scores:%s", deviceCode)
	data, err := s.conns.Redis.Get(ctx, key).Result()
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
// This is a best effort. Errors are logged but not returned as loss of cache is not fatal.
func (s *PatrolScoreService) cachePatrolScores(
	ctx context.Context,
	deviceCode string,
	cacheRecord *CachedPatrolScores,
) {
	data, err := json.Marshal(cacheRecord)
	if err != nil {
		slog.Error("patrol_score_service.cachePatrolScores", "message", "cannot marshal cache record", "error", err)
	}

	key := fmt.Sprintf("patrol_scores:%s", deviceCode)
	// Use fallback TTL for Redis (8 days) to keep stale data for emergency use
	// TODO: Configure this as a Duration
	fallbackTTL := time.Duration(s.config.Cache.CacheFallbackTTL) * time.Second
	err = s.conns.Redis.Set(ctx, key, data, fallbackTTL).Err()
	if err != nil {
		slog.Error("patrol_score_service.cachePatrolScores", "message", "cannot write to REDIS cache", "error", err)
	}
}
