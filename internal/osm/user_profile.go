package osm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

const (
	// Cache OSM profile data for 10 minutes (term boundaries don't change often)
	profileCacheTTL = 10 * time.Minute
)

// FetchOSMProfile fetches the user's profile from OSM, including sections and terms.
// Results are cached in Redis for 10 minutes to reduce API calls during a typical session.
func (c *Client) FetchOSMProfile(ctx context.Context, user types.User) (*types.OSMProfileResponse, error) {
	// Only cache if we have a user ID
	// TODO: Move caching out of the OSM layer into a higher profile service
	// FIXME: Handle the payload error from OSM in this function. The outside world shouldn't care
	// TODO: Consider bringing the session types into this package. They belong here conceptually
	userID := user.UserID()
	if userID != nil && c.redisCache != nil {
		cacheKey := fmt.Sprintf("osm_profile:%d", *userID)

		// Try to get from cache first
		cached, err := c.redisCache.Get(ctx, cacheKey).Result()
		if err == nil && cached != "" {
			var profileResp types.OSMProfileResponse
			if err := json.Unmarshal([]byte(cached), &profileResp); err == nil {
				slog.Debug("osm.profile.cache_hit",
					"component", "osm_profile",
					"event", "profile.cache.hit",
					"user_id", *userID,
				)
				return &profileResp, nil
			}
			// Cache corruption - continue to fetch fresh data
			slog.Warn("osm.profile.cache_corrupt",
				"component", "osm_profile",
				"event", "profile.cache.error",
				"user_id", *userID,
				"error", err,
			)
		}
	}

	// Fetch from OSM API
	var profileResp types.OSMProfileResponse
	_, err := c.Request(ctx, http.MethodGet, &profileResp,
		WithPath("/oauth/resource"),
		WithUser(user),
	)
	if err != nil {
		return nil, err
	}

	// Cache the result if we have a user ID
	if userID != nil && c.redisCache != nil {
		cacheKey := fmt.Sprintf("osm_profile:%d", *userID)
		if data, err := json.Marshal(profileResp); err == nil {
			if err := c.redisCache.Set(ctx, cacheKey, data, profileCacheTTL).Err(); err != nil {
				slog.Warn("osm.profile.cache_write_failed",
					"component", "osm_profile",
					"event", "profile.cache.error",
					"user_id", *userID,
					"error", err,
				)
			} else {
				slog.Debug("osm.profile.cache_write",
					"component", "osm_profile",
					"event", "profile.cache.write",
					"user_id", *userID,
					"ttl_minutes", profileCacheTTL.Minutes(),
				)
			}
		}
	}

	return &profileResp, nil
}
