package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/services/patrolscoreservice"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// GetPatrolScoresHandler handles GET /api/v1/patrols requests.
// Expects authentication middleware to have already run and added User to context.
// Returns patrol scores with intelligent caching and rate limiting.
func GetPatrolScoresHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx := r.Context()

		// Get authenticated user from context (set by middleware)
		user, ok := middleware.UserFromContext(ctx)
		if !ok {
			// This should never happen if middleware is properly configured
			slog.Error("api.patrol_scores.no_user_in_context",
				"component", "api",
				"event", "auth.error",
				"error", "user not found in context - middleware not configured?",
			)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Get device code record from auth context
		authCtx, ok := user.(interface{ DeviceCode() *db.DeviceCode })
		if !ok {
			slog.Error("api.patrol_scores.auth_context_error",
				"component", "api",
				"event", "auth.error",
				"error", "user does not implement DeviceCode() method",
			)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		device := authCtx.DeviceCode()

		// Create patrol score service
		patrolService := patrolscoreservice.New(
			deps.OSM,
			deps.Conns,
			deps.Config,
		)

		if device.SectionID == nil {
			// TODO: Utility function to write a HTTP error
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "section_not_configured",
				"message": "Device has not selected a section",
			})
			return
		}

		// Get patrol scores with caching and term management
		response, err := patrolService.GetPatrolScores(ctx, user, *device.SectionID)
		if err != nil {
			slog.Error("api.patrol_scores.fetch_error",
				"component", "api",
				"event", "patrol.error",
				"device_code_hash", device.DeviceCode[:8],
				"error", err,
			)

			// Handle specific error types
			if errors.Is(err, types.ErrNotInActiveTerm) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "not_in_term",
					"message": "Section is not currently in an active term",
				})
				return
			}

			if errors.Is(err, types.ErrCannotFindSection) {
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
					"error":         "user_temporary_block",
					"message":       "User temporarily blocked due to rate limiting",
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
