package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db/devicecode"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	wsinternal "github.com/m0rjc/OsmDeviceAdapter/internal/websocket"
)

// timerCommandRequest is the request body for timer control commands.
type timerCommandRequest struct {
	Command  string `json:"command"`
	Duration int    `json:"duration,omitempty"`
}

// AdminScoreboardTimerHandler handles POST /api/admin/scoreboards/{deviceCode}/timer
func AdminScoreboardTimerHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
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

		// Parse device code prefix from URL: /api/admin/scoreboards/{deviceCode}/timer
		path := r.URL.Path
		prefix := "/api/admin/scoreboards/"
		suffix := "/timer"
		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
			writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
			return
		}
		deviceCodePrefix := path[len(prefix) : len(path)-len(suffix)]

		// Parse request body
		var req timerCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
			return
		}

		// Validate command
		switch req.Command {
		case "start", "pause", "resume", "reset":
			// valid
		default:
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Unknown command: "+req.Command)
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

		// Map command to WebSocket message
		var msg wsinternal.Message
		switch req.Command {
		case "start":
			msg = wsinternal.TimerStartMessage(req.Duration)
		case "pause":
			msg = wsinternal.TimerPauseMessage()
		case "resume":
			msg = wsinternal.TimerResumeMessage()
		case "reset":
			msg = wsinternal.TimerResetMessage()
		}

		if deps.WebSocketHub != nil {
			deps.WebSocketHub.BroadcastToDevice(*targetDevice, msg)
		}

		slog.Info("admin.scoreboards.timer_command",
			"component", "admin_timer",
			"event", "timer.command",
			"user_id", session.OSMUserID,
			"device_code_prefix", deviceCodePrefix,
			"command", req.Command,
			"duration", req.Duration,
		)

		writeJSON(w, map[string]bool{"success": true})
	}
}
