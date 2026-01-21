package patrolscoreservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// RateLimitState represents the current rate limiting state
type RateLimitState string

const (
	RateLimitStateNone               RateLimitState = "NONE"                 // Normal operation (remaining > 200)
	RateLimitStateDegraded           RateLimitState = "DEGRADED"             // Rate limit approaching (remaining < 200)
	RateLimitStateUserTemporaryBlock RateLimitState = "USER_TEMPORARY_BLOCK" // User temporarily blocked (HTTP 429)
	RateLimitStateServiceBlocked     RateLimitState = "SERVICE_BLOCKED"      // Service blocked by OSM (X-Blocked header)
)

// CachedPatrolScores represents cached patrol score data with metadata
type CachedPatrolScores struct {
	Patrols        []types.PatrolScore `json:"patrols"`
	CachedAt       time.Time           `json:"cached_at"`
	ValidUntil     time.Time           `json:"valid_until"`
	RateLimitState RateLimitState      `json:"rate_limit_state"`
}

// PatrolScoreResponse represents the API response for patrol scores
type PatrolScoreResponse struct {
	Patrols        []types.PatrolScore `json:"patrols"`
	FromCache      bool                `json:"from_cache"`
	CachedAt       time.Time           `json:"cached_at"`
	CacheExpiresAt time.Time           `json:"cache_expires_at"`
	RateLimitState RateLimitState      `json:"rate_limit_state"`
}

// PatrolScoreService orchestrates patrol score fetching with caching and rate limiting
type PatrolScoreService struct {
	osmClient *osm.Client
	conns     *db.Connections
	config    *config.Config
}

// New creates a new patrol score service
func New(
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

// GetPatrolScores fetches patrol scores for a user's section, managing term discovery,
// caching, and rate limiting automatically.
func (s *PatrolScoreService) GetPatrolScores(ctx context.Context, user types.User, sectionId int) (*PatrolScoreResponse, error) {
	var err error

	// Check patrol scores cache
	cached, err := s.getCachedPatrolScores(ctx, user.UserID(), sectionId)
	if err == nil && time.Now().Before(cached.ValidUntil) {
		// Cache is still valid
		return &PatrolScoreResponse{
			Patrols:        cached.Patrols,
			FromCache:      true,
			CachedAt:       cached.CachedAt,
			CacheExpiresAt: cached.ValidUntil,
			RateLimitState: cached.RateLimitState,
		}, nil
	}

	// Cache miss or expired - need to fetch fresh data
	// First, ensure we have term information
	var profile *types.OSMProfileData
	var term *types.OSMTerm
	var patrols []types.PatrolScore
	var rateLimitInfo osm.UserRateLimitInfo

	// Collect the error for a single error handling block. This is the reverse of the normal fast failure
	// pattern
	profile, err = s.osmClient.FetchOSMProfile(ctx, user)
	if err == nil {
		term, err = profile.GetCurrentTermForSection(sectionId)
	}
	if err == nil {
		patrols, rateLimitInfo, err = s.osmClient.FetchPatrolScores(ctx, user, sectionId, term.TermID)
	}
	if err != nil {
		// Try to make the cache last long enough if we have one
		cacheUntil := time.Now().Add(10 * time.Minute)     // TODO: Configure. This is the fallback block time if we can't deduce it.
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
				s.cachePatrolScores(ctx, user.UserID(), sectionId, cached)
			}
			return &PatrolScoreResponse{
				Patrols:        cached.Patrols,
				FromCache:      true,
				CachedAt:       cached.CachedAt,
				CacheExpiresAt: cached.ValidUntil,
				RateLimitState: rateLimitState,
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
	s.cachePatrolScores(ctx, user.UserID(), sectionId, &CachedPatrolScores{
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
	}, nil
}
