package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/adhocpatrol"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
)

// AdhocPatrolResponse represents an ad-hoc patrol in API responses.
type AdhocPatrolResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Score    int    `json:"score"`
	Position int    `json:"position"`
}

// AdhocPatrolRequest is the request body for creating/updating an ad-hoc patrol.
type AdhocPatrolRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// AdminAdhocPatrolsHandler handles GET and POST for /api/admin/adhoc/patrols
func AdminAdhocPatrolsHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := middleware.WebSessionFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleListAdhocPatrols(w, deps, session.OSMUserID)
		case http.MethodPost:
			handleCreateAdhocPatrol(w, r, deps, session)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
	}
}

// AdminAdhocPatrolHandler handles PUT and DELETE for /api/admin/adhoc/patrols/{id}
// Also handles POST /api/admin/adhoc/patrols/reset
func AdminAdhocPatrolHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := middleware.WebSessionFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		// Parse patrol ID from URL path: /api/admin/adhoc/patrols/{id}
		path := r.URL.Path
		prefix := "/api/admin/adhoc/patrols/"
		if !strings.HasPrefix(path, prefix) {
			writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
			return
		}
		idStr := path[len(prefix):]

		// Check for /reset suffix on POST
		if idStr == "reset" && r.Method == http.MethodPost {
			handleResetAdhocScores(w, r, deps, session)
			return
		}

		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid patrol ID")
			return
		}

		switch r.Method {
		case http.MethodPut:
			handleUpdateAdhocPatrol(w, r, deps, session, id)
		case http.MethodDelete:
			handleDeleteAdhocPatrol(w, r, deps, session, id)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
	}
}

func handleListAdhocPatrols(w http.ResponseWriter, deps *Dependencies, osmUserID int) {
	patrols, err := adhocpatrol.ListByUser(deps.Conns, osmUserID)
	if err != nil {
		slog.Error("admin.adhoc.list.failed",
			"component", "admin_adhoc",
			"event", "list.error",
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list patrols")
		return
	}

	resp := make([]AdhocPatrolResponse, len(patrols))
	for i, p := range patrols {
		resp[i] = AdhocPatrolResponse{
			ID:       strconv.FormatInt(p.ID, 10),
			Name:     p.Name,
			Color:    p.Color,
			Score:    p.Score,
			Position: p.Position,
		}
	}

	writeJSON(w, resp)
}

func handleCreateAdhocPatrol(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession) {
	if err := validateCSRFToken(r, session); err != nil {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", err.Error())
		return
	}

	var req AdhocPatrolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	if err := validateAdhocPatrolRequest(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	patrol := &db.AdhocPatrol{
		OSMUserID: session.OSMUserID,
		Name:      req.Name,
		Color:     req.Color,
	}

	if err := adhocpatrol.Create(deps.Conns, patrol); err != nil {
		if err == adhocpatrol.ErrMaxPatrolsReached {
			writeJSONError(w, http.StatusConflict, "max_patrols", err.Error())
			return
		}
		slog.Error("admin.adhoc.create.failed",
			"component", "admin_adhoc",
			"event", "create.error",
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create patrol")
		return
	}

	slog.Info("admin.adhoc.created",
		"component", "admin_adhoc",
		"event", "patrol.created",
		"user_id", session.OSMUserID,
		"patrol_id", patrol.ID,
		"patrol_name", patrol.Name,
	)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, AdhocPatrolResponse{
		ID:       strconv.FormatInt(patrol.ID, 10),
		Name:     patrol.Name,
		Color:    patrol.Color,
		Score:    patrol.Score,
		Position: patrol.Position,
	})
}

func handleUpdateAdhocPatrol(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, id int64) {
	if err := validateCSRFToken(r, session); err != nil {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", err.Error())
		return
	}

	var req AdhocPatrolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	if err := validateAdhocPatrolRequest(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	if err := adhocpatrol.Update(deps.Conns, id, session.OSMUserID, req.Name, req.Color); err != nil {
		if err == adhocpatrol.ErrNotFound {
			writeJSONError(w, http.StatusNotFound, "not_found", "Patrol not found")
			return
		}
		slog.Error("admin.adhoc.update.failed",
			"component", "admin_adhoc",
			"event", "update.error",
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update patrol")
		return
	}

	patrol, err := adhocpatrol.FindByIDAndUser(deps.Conns, id, session.OSMUserID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to fetch updated patrol")
		return
	}

	slog.Info("admin.adhoc.updated",
		"component", "admin_adhoc",
		"event", "patrol.updated",
		"user_id", session.OSMUserID,
		"patrol_id", id,
	)

	writeJSON(w, AdhocPatrolResponse{
		ID:       strconv.FormatInt(patrol.ID, 10),
		Name:     patrol.Name,
		Color:    patrol.Color,
		Score:    patrol.Score,
		Position: patrol.Position,
	})
}

func handleDeleteAdhocPatrol(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession, id int64) {
	if err := validateCSRFToken(r, session); err != nil {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", err.Error())
		return
	}

	if err := adhocpatrol.Delete(deps.Conns, id, session.OSMUserID); err != nil {
		if err == adhocpatrol.ErrNotFound {
			writeJSONError(w, http.StatusNotFound, "not_found", "Patrol not found")
			return
		}
		slog.Error("admin.adhoc.delete.failed",
			"component", "admin_adhoc",
			"event", "delete.error",
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete patrol")
		return
	}

	slog.Info("admin.adhoc.deleted",
		"component", "admin_adhoc",
		"event", "patrol.deleted",
		"user_id", session.OSMUserID,
		"patrol_id", id,
	)

	w.WriteHeader(http.StatusNoContent)
}

func handleResetAdhocScores(w http.ResponseWriter, r *http.Request, deps *Dependencies, session *db.WebSession) {
	if err := validateCSRFToken(r, session); err != nil {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", err.Error())
		return
	}

	if err := adhocpatrol.ResetAllScores(deps.Conns, session.OSMUserID); err != nil {
		slog.Error("admin.adhoc.reset.failed",
			"component", "admin_adhoc",
			"event", "reset.error",
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to reset scores")
		return
	}

	slog.Info("admin.adhoc.scores_reset",
		"component", "admin_adhoc",
		"event", "scores.reset",
		"user_id", session.OSMUserID,
	)

	writeJSON(w, map[string]bool{"success": true})
}

// validateCSRFToken checks the X-CSRF-Token header against the session's token.
func validateCSRFToken(r *http.Request, session *db.WebSession) error {
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken == "" {
		return fmt.Errorf("CSRF token required")
	}
	if csrfToken != session.CSRFToken {
		return fmt.Errorf("invalid CSRF token")
	}
	return nil
}

func validateAdhocPatrolRequest(req *AdhocPatrolRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(req.Name) > 50 {
		return fmt.Errorf("name must be 50 characters or less")
	}
	if req.Color != "" && !validColorNames[req.Color] {
		return fmt.Errorf("invalid color name")
	}
	return nil
}
