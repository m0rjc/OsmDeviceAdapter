package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreoutbox"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/usercredentials"
	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"github.com/m0rjc/OsmDeviceAdapter/internal/worker"
)

// Response types for admin API endpoints

// AdminSessionResponse is returned by GET /api/admin/session
type AdminSessionResponse struct {
	Authenticated     bool           `json:"authenticated"`
	User              *AdminUserInfo `json:"user,omitempty"`
	SelectedSectionID *int           `json:"selectedSectionId,omitempty"`
	CSRFToken         string         `json:"csrfToken,omitempty"`
	PendingWrites     int            `json:"pendingWrites"` // Count of pending/processing outbox entries
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
// DEPRECATED: Will be replaced by AdminOutboxResponse after outbox pattern rollout
type AdminUpdateResponse struct {
	Success bool                `json:"success"`
	Patrols []AdminPatrolResult `json:"patrols"`
}

// AdminPatrolResult contains the result of a single patrol score update
type AdminPatrolResult struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	PreviousScore int    `json:"previousScore"`
	NewScore      int    `json:"newScore"`
}

// AdminOutboxResponse is returned by POST /api/admin/sections/{sectionId}/scores (outbox pattern)
type AdminOutboxResponse struct {
	Status          string `json:"status"`           // "accepted"
	BatchID         string `json:"batchId"`          // UUID for this batch of updates
	EntriesCreated  int    `json:"entriesCreated"`   // Number of outbox entries created
	IdempotencyKey  string `json:"idempotencyKey"`   // Echo back the idempotency key
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

		// Count pending writes for this user
		pendingCount, err := scoreoutbox.CountPendingByUser(deps.Conns, session.OSMUserID)
		if err != nil {
			slog.Error("admin.api.session.pending_count_failed",
				"component", "admin_api",
				"event", "session.warning",
				"error", err,
			)
			// Continue with 0 - non-critical error
			pendingCount = 0
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
				PendingWrites:     int(pendingCount),
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
			"pending_writes", pendingCount,
		)

		writeJSON(w, AdminSessionResponse{
			Authenticated:     true,
			User:              &AdminUserInfo{OSMUserID: session.OSMUserID, Name: userName},
			SelectedSectionID: session.SelectedSectionID,
			CSRFToken:         session.CSRFToken,
			PendingWrites:     int(pendingCount),
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
			handleGetScores(w, r, deps, session, user, sectionID, targetSection, profile.Data.UserID)
		case http.MethodPost:
			handleUpdateScores(w, r, deps, session, user, sectionID, targetSection)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
	}
}

// handleGetScores handles GET /api/admin/sections/{sectionId}/scores
func handleGetScores(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, user types.User, sectionID int, section *types.OSMSection, userID int) {
	ctx := r.Context()

	// Find the active term for the section using the helper
	activeTerm, err := osm.FindActiveTerm(section)
	if err != nil {
		slog.Error("admin.api.scores.term_not_found",
			"component", "admin_api",
			"event", "scores.error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to determine current term")
		return
	}

	termID := activeTerm.TermID

	// Fetch patrol scores
	patrols, _, err := deps.OSM.FetchPatrolScores(ctx, user, sectionID, termID)
	if err != nil {
		slog.Error("admin.api.scores.fetch_failed",
			"component", "admin_api",
			"event", "scores.error",
			"section_id", sectionID,
			"term_id", termID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch patrol scores")
		return
	}

	slog.Info("admin.api.scores.fetched",
		"component", "admin_api",
		"event", "scores.success",
		"user_id", userID,
		"section_id", sectionID,
		"patrol_count", len(patrols),
	)

	writeJSON(w, AdminScoresResponse{
		Section: AdminSectionInfo{
			ID:   sectionID,
			Name: section.SectionName,
		},
		TermID:    termID,
		Patrols:   patrols,
		FetchedAt: time.Now().UTC(),
	})
}

// handleUpdateScores handles POST /api/admin/sections/{sectionId}/scores
// Uses the outbox pattern: creates pending entries for background processing
func handleUpdateScores(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, user types.User, sectionID int, section *types.OSMSection) {
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

	// Require idempotency key for outbox pattern
	idempotencyKey := r.Header.Get("X-Idempotency-Key")
	if idempotencyKey == "" {
		writeJSONError(w, http.StatusBadRequest, "idempotency_key_required", "X-Idempotency-Key header is required")
		return
	}

	// Check for duplicate idempotency key (return cached result)
	existing, err := scoreoutbox.FindByIdempotencyKey(deps.Conns, idempotencyKey)
	if err != nil {
		slog.Error("admin.api.scores.idempotency_check_failed",
			"component", "admin_api",
			"event", "scores.update_error",
			"idempotency_key", idempotencyKey,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to check idempotency")
		return
	}
	if existing != nil {
		// Request already processed - need to return fresh scores + optimistic pending delta
		// Create user context for API calls
		user := session.User()

		// Find the active term using the helper
		activeTerm, err := osm.FindActiveTerm(section)
		if err != nil {
			slog.Error("admin.api.scores.term_not_found",
				"component", "admin_api",
				"event", "scores.duplicate_error",
				"error", err,
			)
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to determine current term")
			return
		}

		// Fetch current scores to provide fresh data even for duplicate requests
		currentScores, _, err := deps.OSM.FetchPatrolScores(ctx, user, sectionID, activeTerm.TermID)
		if err != nil {
			slog.Error("admin.api.scores.fetch_failed",
				"component", "admin_api",
				"event", "scores.duplicate_error",
				"error", err,
			)
			writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch current scores")
			return
		}

		// Build map of current scores
		patrolScoreMap := make(map[string]types.PatrolScore)
		for _, p := range currentScores {
			patrolScoreMap[p.ID] = p
		}

		// Find all entries for this batch (same batch_id)
		var allEntries []db.ScoreUpdateOutbox
		err = deps.Conns.DB.Where("batch_id = ?", existing.BatchID).Find(&allEntries).Error
		if err != nil {
			slog.Error("admin.api.scores.duplicate_fetch_failed",
				"component", "admin_api",
				"event", "scores.duplicate_error",
				"batch_id", existing.BatchID,
				"error", err,
			)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to fetch duplicate entries")
			return
		}

		// Build optimistic results
		patrolResults := buildOptimisticResults(allEntries, patrolScoreMap)

		slog.Info("admin.api.scores.duplicate_request",
			"component", "admin_api",
			"event", "scores.duplicate",
			"idempotency_key", idempotencyKey,
			"status", existing.Status,
			"patrol_count", len(patrolResults),
		)
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, AdminUpdateResponse{
			Success: true,
			Patrols: patrolResults,
		})
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

	// Defensive check: ensure user credentials exist
	// (Should already exist from login, but check anyway)
	credentials, err := usercredentials.Get(deps.Conns, session.OSMUserID)
	if err != nil {
		slog.Error("admin.api.scores.credentials_check_failed",
			"component", "admin_api",
			"event", "scores.update_error",
			"user_id", session.OSMUserID,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to verify credentials")
		return
	}
	if credentials == nil {
		slog.Error("admin.api.scores.no_credentials",
			"component", "admin_api",
			"event", "scores.update_error",
			"user_id", session.OSMUserID,
		)
		writeJSONError(w, http.StatusInternalServerError, "no_credentials", "User credentials not found - please re-login")
		return
	}

	// Fetch current patrol scores to get patrol names AND current scores
	// We need:
	// - Names for the outbox entries and audit trail
	// - Current scores to return to client (even on 202 Accepted)
	activeTerm, err := osm.FindActiveTerm(section)
	if err != nil {
		slog.Error("admin.api.scores.term_not_found",
			"component", "admin_api",
			"event", "scores.update_error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to determine current term")
		return
	}

	currentScores, _, err := deps.OSM.FetchPatrolScores(ctx, user, sectionID, activeTerm.TermID)
	if err != nil {
		slog.Error("admin.api.scores.fetch_failed",
			"component", "admin_api",
			"event", "scores.update_error",
			"section_id", sectionID,
			"error", err,
		)
		writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to fetch current scores")
		return
	}

	// Build map for quick lookup of current scores and names
	patrolScoreMap := make(map[string]types.PatrolScore)
	for _, p := range currentScores {
		patrolScoreMap[p.ID] = p
	}

	// Generate batch ID for this submission
	batchID, err := generateUUID()
	if err != nil {
		slog.Error("admin.api.scores.batch_id_generation_failed",
			"component", "admin_api",
			"event", "scores.update_error",
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to generate batch ID")
		return
	}

	// Create outbox entries
	outboxEntries := make([]db.ScoreUpdateOutbox, 0, len(req.Updates))
	for i, update := range req.Updates {
		// Validate patrol exists in section
		_, exists := patrolScoreMap[update.PatrolID]
		if !exists {
			slog.Warn("admin.api.scores.patrol_not_found",
				"component", "admin_api",
				"event", "scores.update_warning",
				"patrol_id", update.PatrolID,
			)
			continue // Skip unknown patrols
		}

		// Generate a unique idempotency key per entry (based on main key + patrol ID + index)
		// Index ensures uniqueness even if same patrol appears multiple times in request
		entryIdempotencyKey := fmt.Sprintf("%s:%s:%d", idempotencyKey, update.PatrolID, i)

		outboxEntries = append(outboxEntries, db.ScoreUpdateOutbox{
			IdempotencyKey: entryIdempotencyKey,
			OSMUserID:      session.OSMUserID,
			SectionID:      sectionID,
			PatrolID:       update.PatrolID,
			PointsDelta:    update.Points,
			Status:         "pending",
			BatchID:        batchID,
		})
	}

	if len(outboxEntries) == 0 {
		writeJSONError(w, http.StatusBadRequest, "no_valid_patrols", "No valid patrols to update")
		return
	}

	// Create outbox entries in database
	if err := scoreoutbox.CreateBatch(deps.Conns, outboxEntries); err != nil {
		slog.Error("admin.api.scores.outbox_create_failed",
			"component", "admin_api",
			"event", "scores.update_error",
			"batch_id", batchID,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create outbox entries")
		return
	}

	// Record metrics for created entries
	metrics.ScoreOutboxEntriesCreated.Add(float64(len(outboxEntries)))

	slog.Info("admin.api.scores.outbox_created",
		"component", "admin_api",
		"event", "scores.outbox_created",
		"user_id", session.OSMUserID,
		"section_id", sectionID,
		"batch_id", batchID,
		"entry_count", len(outboxEntries),
	)

	// Build optimistic patrol results from current scores + pending deltas
	// This is our "belief" of what the scores will be after sync
	// We can return this immediately, regardless of sync status
	patrolResults := buildOptimisticResults(outboxEntries, patrolScoreMap)

	// Check sync mode header (default: interactive)
	syncMode := r.Header.Get("X-Sync-Mode")
	if syncMode == "" {
		syncMode = "interactive"
	}

	// If interactive mode, try to sync immediately and return actual results on success
	if syncMode == "interactive" {
		// Create channel to collect sync results
		type syncResult struct {
			result *worker.SyncResult
			err    error
		}
		results := make(chan syncResult, len(outboxEntries))

		// Launch sync operations in parallel
		for _, entry := range outboxEntries {
			go func(osmUserID, sectionID int, patrolID string) {
				// Use background context with 2-minute timeout for individual sync
				syncCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()

				result, err := deps.PatrolSync.SyncPatrol(syncCtx, osmUserID, sectionID, patrolID)
				results <- syncResult{result: result, err: err}
			}(entry.OSMUserID, entry.SectionID, entry.PatrolID)
		}

		// Wait for all results with timeout (30 seconds total)
		timeout := time.After(30 * time.Second)
		actualResults := make([]AdminPatrolResult, 0, len(outboxEntries))
		successCount := 0

		for i := 0; i < len(outboxEntries); i++ {
			select {
			case result := <-results:
				if result.err != nil {
					slog.Error("admin.api.scores.interactive_sync_failed",
						"component", "admin_api",
						"event", "scores.sync_error",
						"error", result.err,
					)
					// Sync failed - return optimistic results with 202
					goto acceptedResponse
				}
				if result.result != nil {
					// Sync succeeded - collect actual result
					successCount++
					actualResults = append(actualResults, AdminPatrolResult{
						ID:            result.result.PatrolID,
						Name:          result.result.PatrolName,
						PreviousScore: result.result.PreviousScore,
						NewScore:      result.result.NewScore,
					})
				}
			case <-timeout:
				slog.Warn("admin.api.scores.interactive_sync_timeout",
					"component", "admin_api",
					"event", "scores.sync_timeout",
					"completed", successCount,
					"total", len(outboxEntries),
				)
				// Timeout - return optimistic results with 202
				goto acceptedResponse
			}
		}

		// All syncs completed successfully - return actual results with 200 OK
		slog.Info("admin.api.scores.interactive_sync_success",
			"component", "admin_api",
			"event", "scores.sync_success",
			"patrol_count", len(actualResults),
		)

		w.WriteHeader(http.StatusOK)
		writeJSON(w, AdminUpdateResponse{
			Success: true,
			Patrols: actualResults,
		})
		return
	}

acceptedResponse:
	// Return 202 Accepted with optimistic patrol results
	// Client can immediately show current scores + pending updates
	// This is reached for:
	// 1. Background sync mode (non-interactive)
	// 2. Interactive sync timeout
	// 3. Interactive sync errors
	slog.Info("admin.api.scores.accepted",
		"component", "admin_api",
		"event", "scores.accepted",
		"batch_id", batchID,
		"patrol_count", len(patrolResults),
	)

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, AdminUpdateResponse{
		Success: true,
		Patrols: patrolResults, // Return optimistic results
	})
}

// buildOptimisticResults constructs patrol results from current scores + pending deltas
// This represents our "belief" of what the scores will be after sync completes
func buildOptimisticResults(outboxEntries []db.ScoreUpdateOutbox, currentScores map[string]types.PatrolScore) []AdminPatrolResult {
	// Aggregate deltas by patrol (multiple entries may target same patrol)
	deltasByPatrol := make(map[string]int)
	for _, entry := range outboxEntries {
		deltasByPatrol[entry.PatrolID] += entry.PointsDelta
	}

	// Build results
	results := make([]AdminPatrolResult, 0, len(deltasByPatrol))
	for patrolID, delta := range deltasByPatrol {
		current := currentScores[patrolID]
		results = append(results, AdminPatrolResult{
			ID:            patrolID,
			Name:          current.Name,
			PreviousScore: current.Score,        // Current score from OSM
			NewScore:      current.Score + delta, // Optimistic: current + pending
		})
	}

	return results
}
