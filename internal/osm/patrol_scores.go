package osm

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// PatrolData represents a patrol in the OSM API response.
// The response is a map where keys are patrol IDs (or "unallocated").
type PatrolData struct {
	PatrolID string        `json:"patrolid"`
	Name     string        `json:"name"`
	Points   string        `json:"points"`
	Members  []interface{} `json:"members"`
}

// FetchPatrolScores fetches patrol scores from the OSM API for a given section and term.
// It filters out special patrols (negative IDs, empty members) and returns only regular patrols.
//
// Parameters:
// - sectionID: The OSM section ID
// - termID: The OSM term ID
// - user: The authenticated user
//
// Returns:
// - []types.PatrolScore: Array of patrol scores sorted by name
// - UserRateLimitInfo: Rate limit information from the API response
// - error: Any error that occurred during fetching or parsing
func (c *Client) FetchPatrolScores(ctx context.Context, user types.User, sectionID, termID int) ([]types.PatrolScore, UserRateLimitInfo, error) {
	slog.Debug("osm.patrol_scores.fetching",
		"component", "patrol_scores",
		"event", "patrol.fetch.start",
		"section_id", sectionID,
		"term_id", termID,
	)

	// The response is a map with patrol IDs as keys
	var patrolMap map[string]PatrolData
	resp, err := c.Request(ctx, "GET", &patrolMap,
		WithPath("/ext/members/patrols/"),
		WithQueryParameters(map[string]string{
			"action":            "getPatrolsWithPeople",
			"sectionid":         strconv.Itoa(sectionID),
			"termid":            strconv.Itoa(termID),
			"include_no_patrol": "y",
		}),
		WithUser(user),
	)
	if err != nil {
		slog.Error("osm.patrol_scores.fetch_failed",
			"component", "patrol_scores",
			"event", "patrol.error",
			"section_id", sectionID,
			"term_id", termID,
			"error", err,
		)
		return nil, UserRateLimitInfo{}, fmt.Errorf("failed to fetch patrol scores: %w", err)
	}

	// Filter and convert patrols
	var patrols []types.PatrolScore
	for patrolID, patrol := range patrolMap {
		// Skip special keys
		if patrolID == "unallocated" {
			continue
		}

		// Skip patrols with negative IDs (Leaders, Young Leaders, etc.)
		if patrolID[0] == '-' {
			slog.Debug("osm.patrol_scores.skipping_special",
				"component", "patrol_scores",
				"event", "patrol.filter",
				"patrol_id", patrolID,
				"patrol_name", patrol.Name,
				"reason", "negative_id",
			)
			continue
		}

		// Skip patrols with empty members arrays
		if len(patrol.Members) == 0 {
			slog.Debug("osm.patrol_scores.skipping_empty",
				"component", "patrol_scores",
				"event", "patrol.filter",
				"patrol_id", patrolID,
				"patrol_name", patrol.Name,
				"reason", "empty_members",
			)
			continue
		}

		// Parse points string to integer
		points, err := strconv.Atoi(patrol.Points)
		if err != nil {
			slog.Warn("osm.patrol_scores.invalid_points",
				"component", "patrol_scores",
				"event", "patrol.parse_error",
				"patrol_id", patrolID,
				"patrol_name", patrol.Name,
				"points", patrol.Points,
				"error", err,
			)
			// Default to 0 if points can't be parsed
			points = 0
		}

		patrols = append(patrols, types.PatrolScore{
			ID:    patrolID,
			Name:  patrol.Name,
			Score: points,
		})
	}

	// Sort patrols by name for consistent ordering
	sort.Slice(patrols, func(i, j int) bool {
		return patrols[i].Name < patrols[j].Name
	})

	slog.Info("osm.patrol_scores.success",
		"component", "patrol_scores",
		"event", "patrol.fetch.complete",
		"section_id", sectionID,
		"term_id", termID,
		"patrol_count", len(patrols),
	)

	return patrols, resp.Limits, nil
}
