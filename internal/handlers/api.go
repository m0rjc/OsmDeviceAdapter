package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

func GetPatrolScoresHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract bearer token from Authorization header
		authHeader := r.Header.Get("Authorization")
		accessToken := extractBearerToken(authHeader)

		if accessToken == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="API"`)
			http.Error(w, "Missing or invalid authorization", http.StatusUnauthorized)
			return
		}

		// Verify the access token belongs to a valid device
		var deviceCode string
		var osmAccessToken, osmRefreshToken string
		var osmTokenExpiry time.Time

		err := deps.DB.QueryRow(`
			SELECT device_code, osm_access_token, osm_refresh_token, osm_token_expiry
			FROM device_codes
			WHERE osm_access_token = $1 AND status = 'authorized'
		`, accessToken).Scan(&deviceCode, &osmAccessToken, &osmRefreshToken, &osmTokenExpiry)

		if err != nil {
			http.Error(w, "Invalid access token", http.StatusUnauthorized)
			return
		}

		// Check if we need to refresh the OSM token
		if time.Now().After(osmTokenExpiry.Add(-5 * time.Minute)) {
			// Token is expired or about to expire, refresh it
			newTokens, err := refreshOSMToken(deps.Config, osmRefreshToken)
			if err != nil {
				http.Error(w, "Failed to refresh token", http.StatusInternalServerError)
				return
			}

			// Update tokens in database
			newExpiry := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
			_, err = deps.DB.Exec(`
				UPDATE device_codes
				SET osm_access_token = $1,
				    osm_refresh_token = $2,
				    osm_token_expiry = $3
				WHERE device_code = $4
			`, newTokens.AccessToken, newTokens.RefreshToken, newExpiry, deviceCode)

			if err != nil {
				http.Error(w, "Failed to update tokens", http.StatusInternalServerError)
				return
			}

			osmAccessToken = newTokens.AccessToken
		}

		// Check cache first
		ctx := r.Context()
		cacheKey := fmt.Sprintf("patrol_scores:%s", deviceCode)

		cachedData, err := deps.RedisClient.Client().Get(ctx, cacheKey).Result()
		if err == nil {
			// Cache hit
			var response types.PatrolScoresResponse
			if err := json.Unmarshal([]byte(cachedData), &response); err == nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				json.NewEncoder(w).Encode(response)
				return
			}
		}

		// Cache miss - fetch from OSM
		client := osm.NewClient(deps.Config.OSMDomain, osmAccessToken)
		patrols, err := client.GetPatrolScores(ctx)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch patrol scores: %v", err), http.StatusInternalServerError)
			return
		}

		// Build response
		now := time.Now()
		cacheDuration := 5 * time.Minute
		response := types.PatrolScoresResponse{
			Patrols:   patrols,
			CachedAt:  now,
			ExpiresAt: now.Add(cacheDuration),
		}

		// Cache the response
		responseJSON, _ := json.Marshal(response)
		deps.RedisClient.Client().Set(ctx, cacheKey, responseJSON, cacheDuration)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		json.NewEncoder(w).Encode(response)
	}
}

func refreshOSMToken(cfg *config.Config, refreshToken string) (*types.OSMTokenResponse, error) {
	client := osm.NewClient(cfg.OSMDomain, "")
	return client.RefreshToken(context.Background(), cfg.OSMClientID, cfg.OSMClientSecret, refreshToken)
}
