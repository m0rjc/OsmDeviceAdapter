package osm

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// UpdatePatrolScore updates the points score for a single patrol in OSM.
// This sets the absolute score value (not incremental).
//
// Parameters:
// - sectionID: The OSM section ID
// - patrolID: The patrol ID to update
// - newScore: The new absolute score value to set
// - user: The authenticated user
//
// Returns:
// - error: Any error that occurred during the update, for example osm.ErrUserBlocked or osm.ErrServiceBlocked
func (c *Client) UpdatePatrolScore(ctx context.Context, user types.User, sectionID int, patrolID string, newScore int) error {
	slog.Debug("osm.patrol_scores.updating",
		"component", "patrol_scores",
		"event", "patrol.update.start",
		"section_id", sectionID,
		"patrol_id", patrolID,
		"new_score", newScore,
	)

	// Build form data for POST body
	formData := url.Values{}
	formData.Set("points", strconv.Itoa(newScore))
	formData.Set("patrolid", patrolID)

	// OSM returns an empty array on success
	var result []any
	_, err := c.Request(ctx, "POST", &result,
		WithPath("/ext/members/patrols/"),
		WithQueryParameters(map[string]string{
			"action":    "updatePatrolPoints",
			"sectionid": strconv.Itoa(sectionID),
		}),
		WithUrlEncodedBody(&formData),
		WithUser(user),
	)
	if err != nil {
		slog.Error("osm.patrol_scores.update_failed",
			"component", "patrol_scores",
			"event", "patrol.update.error",
			"section_id", sectionID,
			"patrol_id", patrolID,
			"new_score", newScore,
			"error", err,
		)
		return fmt.Errorf("failed to update patrol score: %w", err)
	}

	slog.Info("osm.patrol_scores.updated",
		"component", "patrol_scores",
		"event", "patrol.update.success",
		"section_id", sectionID,
		"patrol_id", patrolID,
		"new_score", newScore,
	)

	return nil
}
