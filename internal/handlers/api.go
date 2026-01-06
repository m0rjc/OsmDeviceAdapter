package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"gorm.io/gorm"
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
		var deviceCodeRecord db.DeviceCode
		err := deps.DB.Where("osm_access_token = ? AND status = ?", accessToken, "authorized").First(&deviceCodeRecord).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Invalid access token", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "Invalid access token", http.StatusUnauthorized)
			return
		}

		osmAccessToken := ""
		if deviceCodeRecord.OSMAccessToken != nil {
			osmAccessToken = *deviceCodeRecord.OSMAccessToken
		}
		osmRefreshToken := ""
		if deviceCodeRecord.OSMRefreshToken != nil {
			osmRefreshToken = *deviceCodeRecord.OSMRefreshToken
		}

		// Check if we need to refresh the OSM token
		if deviceCodeRecord.OSMTokenExpiry != nil && time.Now().After(deviceCodeRecord.OSMTokenExpiry.Add(-5*time.Minute)) {
			// Token is expired or about to expire, refresh it
			newTokens, err := refreshOSMToken(deps.Config, osmRefreshToken)
			if err != nil {
				http.Error(w, "Failed to refresh token", http.StatusInternalServerError)
				return
			}

			// Update tokens in database
			newExpiry := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
			updates := map[string]interface{}{
				"osm_access_token":  newTokens.AccessToken,
				"osm_refresh_token": newTokens.RefreshToken,
				"osm_token_expiry":  newExpiry,
			}
			if err := deps.DB.Model(&db.DeviceCode{}).Where("device_code = ?", deviceCodeRecord.DeviceCode).Updates(updates).Error; err != nil {
				http.Error(w, "Failed to update tokens", http.StatusInternalServerError)
				return
			}

			osmAccessToken = newTokens.AccessToken
		}

		// Check cache first
		ctx := r.Context()
		cacheKey := fmt.Sprintf("patrol_scores:%s", deviceCodeRecord.DeviceCode)

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
