package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/services"
)

// GetPatrolScoresHandler handles GET /api/v1/patrols requests.
// It authenticates the device using the bearer token and returns patrol scores
// with intelligent caching and rate limiting.
func GetPatrolScoresHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract device access token from Authorization header
		authHeader := r.Header.Get("Authorization")
		deviceAccessToken := extractBearerToken(authHeader)

		if deviceAccessToken == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="API"`)
			http.Error(w, "Missing or invalid authorization", http.StatusUnauthorized)
			return
		}

		// Find device by access token
		ctx := r.Context()
		device, err := db.FindDeviceCodeByDeviceAccessToken(deps.Conns, deviceAccessToken)
		if err != nil {
			slog.Error("api.patrol_scores.database_error",
				"component", "api",
				"event", "auth.error",
				"error", err,
			)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if device == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="API"`)
			http.Error(w, "Invalid or expired device token", http.StatusUnauthorized)
			return
		}

		// Create patrol score service
		patrolService := services.NewPatrolScoreService(
			deps.OSM,
			deps.Conns,
			deps.Config,
		)

		// Get patrol scores with caching and term management
		response, err := patrolService.GetPatrolScores(ctx, device.DeviceCode)
		if err != nil {
			slog.Error("api.patrol_scores.fetch_error",
				"component", "api",
				"event", "patrol.error",
				"device_code_hash", device.DeviceCode[:8],
				"error", err,
			)

			// Handle specific error types
			if errors.Is(err, osm.ErrNotInTerm) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "not_in_term",
					"message": "Section is not currently in an active term",
				})
				return
			}

			if errors.Is(err, osm.ErrNoSectionConfigured) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "section_not_configured",
					"message": "Device has not selected a section",
				})
				return
			}

			if errors.Is(err, osm.ErrSectionNotFound) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "section_not_found",
					"message": "Section not found in user's profile",
				})
				return
			}

			if errors.Is(err, osm.ErrServiceBlocked) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "service_blocked",
					"message": "Service blocked by OSM",
				})
				return
			}

			var userBlockedErr *osm.ErrUserBlocked
			if errors.As(err, &userBlockedErr) {
				// Calculate seconds until the block expires
				retryAfterSeconds := int(time.Until(userBlockedErr.BlockedUntil).Seconds())
				if retryAfterSeconds < 0 {
					retryAfterSeconds = 0
				}

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":        "user_temporary_block",
					"message":      "User temporarily blocked due to rate limiting",
					"blocked_until": userBlockedErr.BlockedUntil.Format(time.RFC3339),
					"retry_after":   retryAfterSeconds,
				})
				return
			}

			// Generic error
			http.Error(w, "Failed to fetch patrol scores", http.StatusBadGateway)
			return
		}

		// Success - return patrol scores
		w.Header().Set("Content-Type", "application/json")
		if response.FromCache {
			w.Header().Set("X-Cache", "HIT")
		} else {
			w.Header().Set("X-Cache", "MISS")
		}
		json.NewEncoder(w).Encode(response)
	}
}
