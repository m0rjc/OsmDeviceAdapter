package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreaudit"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/sectionsettings"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
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
	Success bool                `json:"success"`
	Patrols []AdminPatrolResult `json:"patrols"`
}

// AdminPatrolResult contains the result of a single patrol score update
type AdminPatrolResult struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Success          bool       `json:"success"`
	PreviousScore    int        `json:"previousScore"`
	NewScore         int        `json:"newScore"`
	IsTemporaryError *bool      `json:"isTemporaryError,omitempty"`
	RetryAfter       *time.Time `json:"retryAfter,omitempty"`
	ErrorMessage     *string    `json:"errorMessage,omitempty"`
}

// AdminErrorResponse is used for error responses
type AdminErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// AdminSettingsResponse is returned by GET /api/admin/sections/{sectionId}/settings
type AdminSettingsResponse struct {
	SectionID    int                 `json:"sectionId"`
	PatrolColors map[string]string   `json:"patrolColors"`
	Patrols      []types.PatrolInfo  `json:"patrols"` // Canonical list for UI
}

// AdminSettingsUpdateRequest is the request body for PUT /api/admin/sections/{sectionId}/settings
type AdminSettingsUpdateRequest struct {
	PatrolColors map[string]string `json:"patrolColors"`
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
		profile, err := deps.OSM.FetchOSMProfile(user)
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

		var userName string
		if profile.Data != nil {
			userName = profile.Data.FullName
		}

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
		profile, err := deps.OSM.FetchOSMProfile(user)
		if err != nil {
			slog.Error("admin.api.sections.profile_fetch_failed",
				"component", "admin_api",
				"event", "sections.error",
				"error", err,
			)
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch sections from OSM")
			return
		}

		if profile.Data == nil {
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Invalid response from OSM")
			return
		}

		// Convert OSM sections to admin sections
		sections := make([]AdminSection, 0, len(profile.Data.Sections))
		for _, s := range profile.Data.Sections {
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
		ctx := r.Context()
		session, ok := middleware.WebSessionFromContext(ctx)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		// Parse section ID from URL path
		// Expected format: /api/admin/sections/{sectionId}/scores
		path := r.URL.Path
		prefix := "/api/admin/sections/"
		suffix := "/scores"

		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
			writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
			return
		}

		sectionStr := path[len(prefix) : len(path)-len(suffix)]
		sectionID, err := strconv.Atoi(sectionStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid section ID")
			return
		}

		// Validate user has access to this section
		user := session.User()
		profile, err := deps.OSM.FetchOSMProfile(user)
		if err != nil {
			slog.Error("admin.api.scores.profile_fetch_failed",
				"component", "admin_api",
				"event", "scores.error",
				"error", err,
			)
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to validate section access")
			return
		}

		if profile.Data == nil {
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Invalid response from OSM")
			return
		}

		// Find the section and validate access
		var targetSection *types.OSMSection
		for i := range profile.Data.Sections {
			if profile.Data.Sections[i].SectionID == sectionID {
				targetSection = &profile.Data.Sections[i]
				break
			}
		}

		if targetSection == nil {
			writeJSONError(w, http.StatusForbidden, "forbidden", "You do not have access to this section")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetScores(w, r, deps, session, user, sectionID, targetSection)
		case http.MethodPost:
			handleUpdateScores(w, r, deps, session, user, sectionID, targetSection)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
	}
}

// handleGetScores handles GET /api/admin/sections/{sectionId}/scores
func handleGetScores(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, user types.User, sectionID int, section *types.OSMSection) {
	ctx := r.Context()

	// Get the current term for the section
	termInfo, err := deps.OSM.FetchActiveTermForSection(ctx, user, sectionID)
	if err != nil {
		slog.Error("admin.api.scores.term_fetch_failed",
			"component", "admin_api",
			"event", "scores.error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to determine current term")
		return
	}

	// Fetch patrol scores
	patrols, _, err := deps.OSM.FetchPatrolScores(ctx, user, sectionID, termInfo.TermID)
	if err != nil {
		slog.Error("admin.api.scores.fetch_failed",
			"component", "admin_api",
			"event", "scores.error",
			"section_id", sectionID,
			"term_id", termInfo.TermID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch patrol scores")
		return
	}

	slog.Info("admin.api.scores.fetched",
		"component", "admin_api",
		"event", "scores.success",
		"user_id", session.OSMUserID,
		"section_id", sectionID,
		"patrol_count", len(patrols),
	)

	writeJSON(w, AdminScoresResponse{
		Section: AdminSectionInfo{
			ID:   sectionID,
			Name: section.SectionName,
		},
		TermID:    termInfo.TermID,
		Patrols:   patrols,
		FetchedAt: time.Now().UTC(),
	})
}

// handleUpdateScores handles POST /api/admin/sections/{sectionId}/scores
func handleUpdateScores(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, user types.User, sectionID int, _ *types.OSMSection) {
	ctx := r.Context()

	// Validate CSRF token
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken == "" {
		writeJSONError(w, http.StatusForbidden, "csrf_required", "CSRF token required")
		return
	}
	if csrfToken != session.CSRFToken {
		slog.Warn("admin.api.scores.csrf_invalid",
			"component", "admin_api",
			"event", "scores.csrf_error",
			"user_id", session.OSMUserID,
		)
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
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

	// Validate points range
	for _, update := range req.Updates {
		if update.Points < -1000 || update.Points > 1000 {
			writeJSONError(w, http.StatusBadRequest, "validation_error", "Points must be between -1000 and 1000")
			return
		}
	}

	// Convert to service request format
	serviceRequests := make([]scoreupdateservice.UpdateRequest, len(req.Updates))
	for i, update := range req.Updates {
		serviceRequests[i] = scoreupdateservice.UpdateRequest{
			PatrolID: update.PatrolID,
			Delta:    update.Points,
		}
	}

	// Call the score update service
	serviceResults, err := deps.ScoreUpdateService.UpdateScores(ctx, user, sectionID, serviceRequests)
	if err != nil {
		slog.Error("admin.api.scores.service_error",
			"component", "admin_api",
			"event", "scores.update_error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to update scores")
		return
	}

	// Convert service results to API response format and prepare audit logs
	results := make([]AdminPatrolResult, 0, len(serviceResults))
	auditLogs := make([]db.ScoreAuditLog, 0, len(serviceResults))

	for _, serviceResult := range serviceResults {
		result := AdminPatrolResult{
			ID:               serviceResult.PatrolID,
			Name:             serviceResult.PatrolName,
			Success:          serviceResult.Success,
			IsTemporaryError: serviceResult.IsTemporaryError,
			RetryAfter:       serviceResult.RetryAfter,
			ErrorMessage:     serviceResult.ErrorMessage,
		}

		if serviceResult.PreviousScore != nil {
			result.PreviousScore = *serviceResult.PreviousScore
		}
		if serviceResult.NewScore != nil {
			result.NewScore = *serviceResult.NewScore
		}

		results = append(results, result)

		// Only create audit log for successful updates
		if serviceResult.Success && serviceResult.PreviousScore != nil && serviceResult.NewScore != nil {
			pointsAdded := *serviceResult.NewScore - *serviceResult.PreviousScore
			auditLogs = append(auditLogs, db.ScoreAuditLog{
				OSMUserID:     session.OSMUserID,
				SectionID:     sectionID,
				PatrolID:      serviceResult.PatrolID,
				PatrolName:    serviceResult.PatrolName,
				PreviousScore: *serviceResult.PreviousScore,
				NewScore:      *serviceResult.NewScore,
				PointsAdded:   pointsAdded,
			})
		}
	}

	// Create audit log entries
	if len(auditLogs) > 0 {
		if err := scoreaudit.CreateBatch(deps.Conns, auditLogs); err != nil {
			slog.Error("admin.api.scores.audit_log_failed",
				"component", "admin_api",
				"event", "scores.audit_error",
				"error", err,
			)
			// Don't fail the request, just log the error
		}
	}

	slog.Info("admin.api.scores.updated",
		"component", "admin_api",
		"event", "scores.update_success",
		"user_id", session.OSMUserID,
		"section_id", sectionID,
		"update_count", len(results),
	)

	writeJSON(w, AdminUpdateResponse{
		Success: true,
		Patrols: results,
	})
}

// validColorNames is the set of allowed color names for patrol colors.
// These match the COLOR_PALETTE defined in the admin UI (PatrolColorRow.tsx).
var validColorNames = map[string]bool{
	"red":     true,
	"green":   true,
	"blue":    true,
	"yellow":  true,
	"cyan":    true,
	"magenta": true,
	"orange":  true,
	"white":   true,
}

// AdminSettingsHandler handles both GET and PUT for /api/admin/sections/{sectionId}/settings
func AdminSettingsHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		session, ok := middleware.WebSessionFromContext(ctx)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		// Parse section ID from URL path
		// Expected format: /api/admin/sections/{sectionId}/settings
		path := r.URL.Path
		prefix := "/api/admin/sections/"
		suffix := "/settings"

		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
			writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
			return
		}

		sectionStr := path[len(prefix) : len(path)-len(suffix)]
		sectionID, err := strconv.Atoi(sectionStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid section ID")
			return
		}

		// Validate user has access to this section
		user := session.User()
		profile, err := deps.OSM.FetchOSMProfile(user)
		if err != nil {
			slog.Error("admin.api.settings.profile_fetch_failed",
				"component", "admin_api",
				"event", "settings.error",
				"error", err,
			)
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to validate section access")
			return
		}

		if profile.Data == nil {
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Invalid response from OSM")
			return
		}

		// Find the section and validate access
		var targetSection *types.OSMSection
		for i := range profile.Data.Sections {
			if profile.Data.Sections[i].SectionID == sectionID {
				targetSection = &profile.Data.Sections[i]
				break
			}
		}

		if targetSection == nil {
			writeJSONError(w, http.StatusForbidden, "forbidden", "You do not have access to this section")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetSettings(w, r, deps, session, user, sectionID)
		case http.MethodPut:
			handleUpdateSettings(w, r, deps, session, sectionID)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
	}
}

// handleGetSettings handles GET /api/admin/sections/{sectionId}/settings
func handleGetSettings(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, user types.User, sectionID int) {
	ctx := r.Context()

	// Get the current term for the section to fetch patrols
	termInfo, err := deps.OSM.FetchActiveTermForSection(ctx, user, sectionID)
	if err != nil {
		slog.Error("admin.api.settings.term_fetch_failed",
			"component", "admin_api",
			"event", "settings.error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to determine current term")
		return
	}

	// Fetch patrol list from OSM (canonical list)
	patrols, _, err := deps.OSM.FetchPatrolScores(ctx, user, sectionID, termInfo.TermID)
	if err != nil {
		slog.Error("admin.api.settings.patrols_fetch_failed",
			"component", "admin_api",
			"event", "settings.error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch patrol list")
		return
	}

	// Get settings from database
	settings, err := sectionsettings.GetParsed(deps.Conns, session.OSMUserID, sectionID)
	if err != nil {
		slog.Error("admin.api.settings.db_fetch_failed",
			"component", "admin_api",
			"event", "settings.error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to fetch settings")
		return
	}

	// Convert patrols to PatrolInfo
	patrolInfos := make([]types.PatrolInfo, len(patrols))
	for i, p := range patrols {
		patrolInfos[i] = types.PatrolInfo{
			ID:   p.ID,
			Name: p.Name,
		}
	}

	slog.Info("admin.api.settings.fetched",
		"component", "admin_api",
		"event", "settings.success",
		"user_id", session.OSMUserID,
		"section_id", sectionID,
		"patrol_count", len(patrols),
	)

	writeJSON(w, AdminSettingsResponse{
		SectionID:    sectionID,
		PatrolColors: settings.PatrolColors,
		Patrols:      patrolInfos,
	})
}

// handleUpdateSettings handles PUT /api/admin/sections/{sectionId}/settings
func handleUpdateSettings(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, sectionID int) {
	// Validate CSRF token
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken == "" {
		writeJSONError(w, http.StatusForbidden, "csrf_required", "CSRF token required")
		return
	}
	if csrfToken != session.CSRFToken {
		slog.Warn("admin.api.settings.csrf_invalid",
			"component", "admin_api",
			"event", "settings.csrf_error",
			"user_id", session.OSMUserID,
		)
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	// Parse request body
	var req AdminSettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	// Validate patrol colors
	if req.PatrolColors == nil {
		req.PatrolColors = make(map[string]string)
	}

	for patrolID, color := range req.PatrolColors {
		if color != "" && !validColorNames[color] {
			writeJSONError(w, http.StatusBadRequest, "validation_error",
				"Invalid color for patrol "+patrolID+": must be a valid color name")
			return
		}
	}

	// Update settings in database
	if err := sectionsettings.UpsertPatrolColors(deps.Conns, session.OSMUserID, sectionID, req.PatrolColors); err != nil {
		slog.Error("admin.api.settings.db_update_failed",
			"component", "admin_api",
			"event", "settings.error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save settings")
		return
	}

	slog.Info("admin.api.settings.updated",
		"component", "admin_api",
		"event", "settings.update_success",
		"user_id", session.OSMUserID,
		"section_id", sectionID,
		"color_count", len(req.PatrolColors),
	)

	// Return the updated settings
	writeJSON(w, AdminSettingsResponse{
		SectionID:    sectionID,
		PatrolColors: req.PatrolColors,
		Patrols:      nil, // Don't need to fetch patrols again for PUT response
	})
}
