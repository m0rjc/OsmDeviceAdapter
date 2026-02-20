package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db/devicecode"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	wsinternal "github.com/m0rjc/OsmDeviceAdapter/internal/websocket"
)

// ScoreboardResponse represents a device scoreboard in API responses.
type ScoreboardResponse struct {
	DeviceCodePrefix string  `json:"deviceCodePrefix"`
	SectionID        *int    `json:"sectionId"`
	SectionName      string  `json:"sectionName"`
	ClientID         string  `json:"clientId"`
	LastUsedAt       *string `json:"lastUsedAt,omitempty"`
}

// ScoreboardSectionUpdateRequest is the request body for changing a device's section.
type ScoreboardSectionUpdateRequest struct {
	SectionID int `json:"sectionId"`
}

// AdminScoreboardsHandler handles GET /api/admin/scoreboards
func AdminScoreboardsHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}

		session, ok := middleware.WebSessionFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		devices, err := devicecode.FindByUser(deps.Conns, session.OSMUserID)
		if err != nil {
			slog.Error("admin.scoreboards.list.failed",
				"component", "admin_scoreboards",
				"event", "list.error",
				"error", err,
			)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list scoreboards")
			return
		}

		// Build section name lookup from OSM profile
		sectionNames := map[int]string{0: "Ad-hoc Teams"}
		user := session.User()
		profile, err := deps.OSM.FetchOSMProfile(user)
		if err == nil && profile.Data != nil {
			for _, s := range profile.Data.Sections {
				sectionNames[s.SectionID] = s.SectionName
			}
		}

		resp := make([]ScoreboardResponse, len(devices))
		for i, d := range devices {
			prefix := d.DeviceCode
			if len(prefix) > 8 {
				prefix = prefix[:8]
			}

			var sectionName string
			if d.SectionID != nil {
				sectionName = sectionNames[*d.SectionID]
				if sectionName == "" {
					sectionName = "Section " + strconv.Itoa(*d.SectionID)
				}
			}

			var lastUsed *string
			if d.LastUsedAt != nil {
				s := d.LastUsedAt.Format("2006-01-02T15:04:05Z")
				lastUsed = &s
			}

			resp[i] = ScoreboardResponse{
				DeviceCodePrefix: prefix,
				SectionID:        d.SectionID,
				SectionName:      sectionName,
				ClientID:         d.ClientID,
				LastUsedAt:       lastUsed,
			}
		}

		writeJSON(w, resp)
	}
}

// AdminScoreboardSectionHandler handles PUT /api/admin/scoreboards/{deviceCode}/section
func AdminScoreboardSectionHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}

		session, ok := middleware.WebSessionFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		if err := validateCSRFToken(r, session); err != nil {
			writeJSONError(w, http.StatusForbidden, "csrf_invalid", err.Error())
			return
		}

		// Parse device code from URL: /api/admin/scoreboards/{deviceCode}/section
		path := r.URL.Path
		prefix := "/api/admin/scoreboards/"
		suffix := "/section"
		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
			writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
			return
		}
		deviceCodePrefix := path[len(prefix) : len(path)-len(suffix)]

		// Parse request body
		var req ScoreboardSectionUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
			return
		}

		// Find the device and validate ownership
		devices, err := devicecode.FindByUser(deps.Conns, session.OSMUserID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to look up devices")
			return
		}

		var targetDevice *string
		for _, d := range devices {
			dp := d.DeviceCode
			if len(dp) > 8 {
				dp = dp[:8]
			}
			if dp == deviceCodePrefix {
				targetDevice = &d.DeviceCode
				break
			}
		}

		if targetDevice == nil {
			writeJSONError(w, http.StatusNotFound, "not_found", "Device not found")
			return
		}

		// Validate section access
		if req.SectionID > 0 {
			user := session.User()
			profile, err := deps.OSM.FetchOSMProfile(user)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, "osm_error", "Failed to validate section access")
				return
			}
			if profile.Data == nil {
				writeJSONError(w, http.StatusBadGateway, "osm_error", "Invalid response from OSM")
				return
			}
			found := false
			for _, s := range profile.Data.Sections {
				if s.SectionID == req.SectionID {
					found = true
					break
				}
			}
			if !found {
				writeJSONError(w, http.StatusForbidden, "forbidden", "You do not have access to this section")
				return
			}
		}

		// Update the section
		if err := devicecode.UpdateSectionID(deps.Conns, *targetDevice, req.SectionID); err != nil {
			slog.Error("admin.scoreboards.update_section.failed",
				"component", "admin_scoreboards",
				"event", "update_section.error",
				"error", err,
			)
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update section")
			return
		}

		// Invalidate device's patrol scores cache
		cacheKey := "patrol_scores:" + *targetDevice
		deps.Conns.Redis.Del(r.Context(), cacheKey)

		slog.Info("admin.scoreboards.section_updated",
			"component", "admin_scoreboards",
			"event", "section.updated",
			"user_id", session.OSMUserID,
			"device_code_prefix", deviceCodePrefix,
			"new_section_id", req.SectionID,
		)

		if deps.WebSocketHub != nil {
			deps.WebSocketHub.BroadcastToDevice(*targetDevice, wsinternal.ReconnectMessage())
		}

		writeJSON(w, map[string]bool{"success": true})
	}
}
