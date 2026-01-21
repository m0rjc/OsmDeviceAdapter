package osm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

var (
	ErrNotInTerm           = fmt.Errorf("section is not currently in an active term")
	ErrSectionNotFound     = fmt.Errorf("section not found in user's sections")
	ErrNoSectionConfigured = fmt.Errorf("device has no section configured")
)

// TermInfo contains information about a section's active term.
type TermInfo struct {
	TermID  int
	EndDate time.Time
	UserID  int
}

// FindActiveTerm finds the currently active term for a section.
// A term is active if the current date falls between its start and end dates (inclusive).
// Returns ErrNotInTerm if no active term is found.
func FindActiveTerm(section *types.OSMSection) (*types.OSMTerm, error) {
	now := time.Now()
	const osmTimeLayout = "2006-01-02"

	for i := range section.Terms {
		term := &section.Terms[i]

		// Parse start and end dates
		startDate, err := time.Parse(osmTimeLayout, term.StartDate)
		if err != nil {
			slog.Warn("osm.term_discovery.invalid_start_date",
				"component", "term_discovery",
				"event", "term.parse_error",
				"term_id", term.TermID,
				"start_date", term.StartDate,
				"error", err,
			)
			continue
		}

		endDate, err := time.Parse(osmTimeLayout, term.EndDate)
		if err != nil {
			slog.Warn("osm.term_discovery.invalid_end_date",
				"component", "term_discovery",
				"event", "term.parse_error",
				"term_id", term.TermID,
				"end_date", term.EndDate,
				"error", err,
			)
			continue
		}

		// Check if current date is within term boundaries
		// Use >= for start and <= for end to be inclusive
		if (now.After(startDate) || now.Equal(startDate)) && (now.Before(endDate) || now.Equal(endDate)) {
			return term, nil
		}
	}

	return nil, ErrNotInTerm
}

// FetchActiveTermForSection fetches the active term for a given section.
// It queries the OAuth resource endpoint to get the user's profile and sections,
// then finds the active term based on the current date.
//
// Returns:
// - TermInfo with term details if an active term is found
// - ErrSectionNotFound if the section is not in the user's profile
// - ErrNotInTerm if no active term exists for the current date
// - ErrUserBlocked (wrapped) is the user account is temporarily blocked
// - Other errors for API or parsing failures
func (c *Client) FetchActiveTermForSection(ctx context.Context, user types.User, sectionID int) (*TermInfo, error) {
	slog.Debug("osm.term_discovery.fetching",
		"component", "term_discovery",
		"event", "term.fetch.start",
		"section_id", sectionID,
	)

	profileResp, err := c.FetchOSMProfile(ctx, user)
	if err != nil {
		slog.Error("osm.term_discovery.fetch_failed",
			"component", "term_discovery",
			"event", "term.error",
			"section_id", sectionID,
			"error", err,
		)
		return nil, fmt.Errorf("failed to fetch user profile: %w", err)
	}

	if !profileResp.Status || profileResp.Data == nil {
		errorMsg := "unknown error"
		if profileResp.Error != nil {
			errorMsg = *profileResp.Error
		}
		slog.Error("osm.term_discovery.api_error",
			"component", "term_discovery",
			"event", "term.error",
			"section_id", sectionID,
			"error", errorMsg,
		)
		return nil, fmt.Errorf("OSM API error: %s", errorMsg)
	}

	// Find the section with the given section ID
	var targetSection *types.OSMSection
	for i := range profileResp.Data.Sections {
		if profileResp.Data.Sections[i].SectionID == sectionID {
			targetSection = &profileResp.Data.Sections[i]
			break
		}
	}

	if targetSection == nil {
		slog.Warn("osm.term_discovery.section_not_found",
			"component", "term_discovery",
			"event", "term.error",
			"section_id", sectionID,
			"user_id", profileResp.Data.UserID,
			"available_sections", len(profileResp.Data.Sections),
		)
		return nil, ErrSectionNotFound
	}

	// Find the active term using the helper function
	activeTerm, err := FindActiveTerm(targetSection)
	if err != nil {
		slog.Warn("osm.term_discovery.no_active_term",
			"component", "term_discovery",
			"event", "term.not_found",
			"section_id", sectionID,
			"user_id", profileResp.Data.UserID,
			"total_terms", len(targetSection.Terms),
		)
		return nil, err
	}

	const osmTimeLayout = "2006-01-02"
	endDate, _ := time.Parse(osmTimeLayout, activeTerm.EndDate)

	slog.Info("osm.term_discovery.success",
		"component", "term_discovery",
		"event", "term.found",
		"section_id", sectionID,
		"term_id", activeTerm.TermID,
		"term_name", activeTerm.Name,
		"end_date", activeTerm.EndDate,
		"user_id", profileResp.Data.UserID,
	)

	return &TermInfo{
		TermID:  activeTerm.TermID,
		EndDate: endDate,
		UserID:  profileResp.Data.UserID,
	}, nil
}
