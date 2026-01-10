package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

func GetPatrolScoresHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Authenticate the request
		user, ok := deps.DeviceAuth.AuthenticateRequest(w, r)
		if !ok {
			return // Error response already written
		}

		// Check cache first
		ctx := r.Context()
		cacheKey := fmt.Sprintf("patrol_scores:%d", *user.UserID())

		if deps.Conns.Redis != nil {
			cachedData, err := deps.Conns.Redis.Get(ctx, cacheKey).Result()
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
		}

		// Cache miss - fetch from OSM
		patrols, err := osm.GetPatrolScores(ctx, deps.OSM, user)
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
		if deps.Conns.Redis != nil {
			responseJSON, _ := json.Marshal(response)
			deps.Conns.Redis.Set(ctx, cacheKey, responseJSON, cacheDuration)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		json.NewEncoder(w).Encode(response)
	}
}
