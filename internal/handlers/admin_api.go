package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/services/scoreupdateservice"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Response types for admin API endpoints

// AdminSessionResponse is returned by GET /api/admin/session
type AdminSessionResponse struct {
	Authenticated     bool           `json:"authenticated"`
	User              *AdminUserInfo `json:"user,omitempty"`
	SelectedSectionID *int           `json:"selectedSectionId,omitempty"`
	CSRFToken         string         `json:"csrfToken,omitempty"`
}

// AdminUserInfo contains user information for the session response
type AdminUserInfo struct {
	OSMUserID int    `json:"osmUserId"`
	Name      string `json:"name"`
}

// AdminSectionsResponse is returned by GET /api/admin/sections
type AdminSectionsResponse struct {
	Sections []AdminSection `json:"sections"`
}

// AdminSection represents a section the user has access to
type AdminSection struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	GroupName string `json:"groupName"`
}

// AdminScoresResponse is returned by GET /api/admin/sections/{sectionId}/scores
type AdminScoresResponse struct {
	Section   AdminSectionInfo    `json:"section"`
	TermID    int                 `json:"termId"`
	Patrols   []types.PatrolScore `json:"patrols"`
	FetchedAt time.Time           `json:"fetchedAt"`
}

// AdminSectionInfo contains section info for scores response
type AdminSectionInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AdminUpdateRequest is the request body for POST /api/admin/sections/{sectionId}/scores
type AdminUpdateRequest struct {
	Updates []AdminScoreUpdate `json:"updates"`
}

// AdminScoreUpdate represents a single patrol score update
type AdminScoreUpdate struct {
	PatrolID string `json:"patrolId"`
	Points   int    `json:"points"`
}

// AdminUpdateResponse is returned by POST /api/admin/sections/{sectionId}/scores
type AdminUpdateResponse struct {
	Patrols []AdminPatrolResult `json:"patrols"`
}

// AdminPatrolResult contains the result of a single patrol score update
type AdminPatrolResult struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Success          bool       `json:"success"`
	IsTemporaryError *bool      `json:"isTemporaryError,omitempty"`
	RetryAfter       *time.Time `json:"retryAfter,omitempty"`
	ErrorMessage     *string    `json:"error,omitempty"`
	PreviousScore    *int       `json:"previousScore,omitempty"`
	NewScore         *int       `json:"newScore,omitempty"`
}

// AdminErrorResponse is used for error responses
type AdminErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// writeJSONError writes a JSON error response
func writeJSONError(w http.ResponseWriter, statusCode int, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(AdminErrorResponse{
		Error:   errorCode,
		Message: message,
	})
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// AdminSessionHandler returns the current session information including CSRF token.
// GET /api/admin/session
func AdminSessionHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}

		ctx := r.Context()
		session, ok := middleware.WebSessionFromContext(ctx)
		if !ok {
			// This shouldn't happen if middleware is applied correctly
			slog.Error("admin.api.session.no_session",
				"component", "admin_api",
				"event", "session.error",
			)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		// Fetch user profile from OSM to get the name
		user := session.User()
		profile, err := deps.OSM.FetchOSMProfile(ctx, user)
		if err != nil {
			slog.Error("admin.api.session.profile_fetch_failed",
				"component", "admin_api",
				"event", "session.error",
				"error", err,
			)
			// Return session info without name if profile fetch fails
			writeJSON(w, AdminSessionResponse{
				Authenticated:     true,
				User:              &AdminUserInfo{OSMUserID: session.OSMUserID},
				SelectedSectionID: session.SelectedSectionID,
				CSRFToken:         session.CSRFToken,
			})
			return
		}

		userName := profile.FullName

		slog.Info("admin.api.session.success",
			"component", "admin_api",
			"event", "session.fetched",
			"user_id", session.OSMUserID,
		)

		writeJSON(w, AdminSessionResponse{
			Authenticated:     true,
			User:              &AdminUserInfo{OSMUserID: session.OSMUserID, Name: userName},
			SelectedSectionID: session.SelectedSectionID,
			CSRFToken:         session.CSRFToken,
		})
	}
}

// AdminSectionsHandler returns the list of sections the user has access to.
// GET /api/admin/sections
func AdminSectionsHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}

		ctx := r.Context()
		session, ok := middleware.WebSessionFromContext(ctx)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		user := session.User()
		profile, err := deps.OSM.FetchOSMProfile(ctx, user)
		if err != nil {
			slog.Error("admin.api.sections.profile_fetch_failed",
				"component", "admin_api",
				"event", "sections.error",
				"error", err,
			)
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch sections from OSM")
			return
		}

		// Convert OSM sections to admin sections
		sections := make([]AdminSection, 0, len(profile.Sections))
		for _, s := range profile.Sections {
			sections = append(sections, AdminSection{
				ID:        s.SectionID,
				Name:      s.SectionName,
				GroupName: s.GroupName,
			})
		}

		slog.Info("admin.api.sections.success",
			"component", "admin_api",
			"event", "sections.fetched",
			"user_id", session.OSMUserID,
			"section_count", len(sections),
		)

		writeJSON(w, AdminSectionsResponse{Sections: sections})
	}
}

// AdminScoresHandler handles both GET and POST for /api/admin/sections/{sectionId}/scores
func AdminScoresHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, profile, ok := getCompleteWebSession(w, r, deps.OSM)
		if !ok {
			return
		}

		sectionStr := r.PathValue("sectionId")
		sectionID, err := strconv.Atoi(sectionStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid section ID")
			return
		}

		// Find the section and validate access
		targetSection, err := profile.GetSection(sectionID)
		if err != nil {
			writeJSONError(w, http.StatusForbidden, "forbidden", "You do not have access to this section")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetScores(w, r, deps, session, targetSection)
		case http.MethodPost:
			handleUpdateScores(w, r, deps, session, targetSection)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
	}
}

// handleGetScores handles GET /api/admin/sections/{sectionId}/scores
func handleGetScores(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, section *types.OSMSection) {
	ctx := r.Context()

	// Find the active term for the section using the helper
	activeTerm, err := section.GetCurrentTerm()
	if activeTerm == nil {
		slog.Error("admin.api.scores.term_not_found",
			"component", "admin_api",
			"event", "scores.error",
			"section_id", section.SectionID,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to determine current term")
		return
	}

	termID := activeTerm.TermID

	// Fetch patrol scores
	patrols, _, err := deps.OSM.FetchPatrolScores(ctx, session.User(), section.SectionID, termID)
	if err != nil {
		slog.Error("admin.api.scores.fetch_failed",
			"component", "admin_api",
			"event", "scores.error",
			"section_id", section.SectionID,
			"term_id", termID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch patrol scores")
		return
	}

	slog.Debug("admin.api.scores.fetched",
		"component", "admin_api",
		"event", "scores.success",
		"user_id", session.OSMUserID,
		"section_id", section.SectionID,
		"patrol_count", len(patrols),
	)

	writeJSON(w, AdminScoresResponse{
		Section: AdminSectionInfo{
			ID:   section.SectionID,
			Name: section.SectionName,
		},
		TermID:    termID,
		Patrols:   patrols,
		FetchedAt: time.Now().UTC(),
	})
}

// handleUpdateScores handles POST /api/admin/sections/{sectionId}/scores
func handleUpdateScores(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, section *types.OSMSection) {
	ctx := r.Context()

	if !validateWebCsrfToken(w, r, session) {
		return
	}

	// Parse request body
	var req AdminUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	if len(req.Updates) == 0 {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "No updates provided")
		return
	}

	// Validate updates and form requests for the service
	serviceRequests := make([]scoreupdateservice.UpdateRequest, len(req.Updates))
	for i, update := range req.Updates {
		if update.Points < -1000 || update.Points > 1000 {
			writeJSONError(w, http.StatusBadRequest, "validation_error", "Points must be between -1000 and 1000")
			return
		}
		serviceRequests[i] = scoreupdateservice.UpdateRequest{
			PatrolID: update.PatrolID,
			Delta:    update.Points,
		}
	}

	service := scoreupdateservice.New(deps.OSM, deps.Conns)
	results, err := service.UpdateScores(ctx, session.User(), section.SectionID, serviceRequests)
	if err != nil {
		slog.Error("admin.api.scores.update_failed",
			"component", "admin_api",
			"event", "scores.update_error",
			"section_id", section.SectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to update scores")
		return
	}

	patrolResults := make([]AdminPatrolResult, len(results))
	for i, result := range results {
		patrolResults[i] = AdminPatrolResult{
			ID:               result.PatrolID,
			Name:             result.PatrolName,
			Success:          result.Success,
			IsTemporaryError: result.IsTemporaryError,
			RetryAfter:       result.RetryAfter,
			ErrorMessage:     result.ErrorMessage,
			PreviousScore:    result.PreviousScore,
			NewScore:         result.NewScore,
		}
	}

	w.WriteHeader(http.StatusOK)
	writeJSON(w, AdminUpdateResponse{
		Patrols: patrolResults, // Return optimistic results
	})
}

// vaidateWebCsrfToken validates the CSRF Token required for write. It will return the required error response if needed.
// returns true if OK.
func validateWebCsrfToken(w http.ResponseWriter, r *http.Request, session *db.WebSession) bool {
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken == "" {
		writeJSONError(w, http.StatusForbidden, "csrf_required", "CSRF token required")
		return false
	}
	if csrfToken != session.CSRFToken {
		slog.Warn("admin.api.scores.csrf_invalid",
			"component", "admin_api",
			"event", "scores.csrf_error",
			"user_id", session.OSMUserID,
		)
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return false
	}
	return true
}

// getCompleteWebSession reads the current session from the request (via middleware) and finds the OSM User information.
// OSM user information may be cached.
func getCompleteWebSession(w http.ResponseWriter, r *http.Request, osm *osm.Client) (*db.WebSession, *types.OSMProfileData, bool) {
	ctx := r.Context()
	session, ok := middleware.WebSessionFromContext(ctx) // We should not get this far
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return nil, nil, false
	}

	// Validate user has access to this section
	user := session.User()
	profile, err := osm.FetchOSMProfile(ctx, user)
	if err != nil {
		slog.Error("admin.api.scores.profile_fetch_failed",
			"component", "admin_api",
			"event", "scores.error",
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to validate section access")
		return nil, nil, false
	}

	return session, profile, true
}
