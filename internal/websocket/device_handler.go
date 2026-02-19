package websocket

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	ws "github.com/gorilla/websocket"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// deviceAuthenticator authenticates a device access token.
type deviceAuthenticator interface {
	Authenticate(ctx context.Context, authHeader string) (types.User, error)
}

// deviceCodeProvider is satisfied by the AuthContext returned by deviceauth.Service.
type deviceCodeProvider interface {
	DeviceCode() *db.DeviceCode
}

// DeviceWebSocketHandler returns an http.HandlerFunc for GET /ws/device.
//
// The device supplies its access token as the "token" query parameter.
// The handler validates the token, upgrades the connection, registers the
// device with the hub, and runs the read/write pumps.
func DeviceWebSocketHandler(hub *Hub, deviceAuth deviceAuthenticator, exposedDomain string) http.HandlerFunc {
	upgrader := ws.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// No Origin header — allow (e.g. native clients, curl).
				return true
			}
			// Strip trailing slash for comparison.
			return strings.TrimSuffix(origin, "/") == strings.TrimSuffix(exposedDomain, "/")
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Accept GET only — the upgrader will handle the actual protocol switch.
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// --- Authentication ---
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := deviceAuth.Authenticate(r.Context(), "Bearer "+token)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		dcp, ok := user.(deviceCodeProvider)
		if !ok {
			slog.Error("websocket.handler.no_device_code",
				"component", "websocket",
				"event", "handler.auth_error",
			)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		device := dcp.DeviceCode()

		if device.SectionID == nil {
			http.Error(w, "Device section not configured", http.StatusBadRequest)
			return
		}
		sectionID := strconv.Itoa(*device.SectionID)

		// Ad-hoc devices (sectionID == 0) are scoped per user to avoid cross-user
		// notifications. Regular sections are globally unique in OSM so no user
		// scoping is required there.
		var channelKey string
		if *device.SectionID == 0 {
			if device.OsmUserID == nil {
				slog.Error("websocket.handler.adhoc_no_user",
					"component", "websocket",
					"event", "handler.auth_error",
				)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			channelKey = "adhoc:" + strconv.Itoa(*device.OsmUserID)
		} else {
			channelKey = "section:" + sectionID
		}

		// --- WebSocket upgrade ---
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			// Upgrader writes the error response itself.
			slog.Error("websocket.handler.upgrade_failed",
				"component", "websocket",
				"event", "handler.upgrade_error",
				"error", err,
			)
			return
		}

		slog.Info("websocket.handler.connected",
			"component", "websocket",
			"event", "handler.connected",
			"device_code_prefix", device.DeviceCode[:min(8, len(device.DeviceCode))],
			"section_id", sectionID,
			"channel_key", channelKey,
			"remote_addr", r.RemoteAddr,
		)

		dc := &deviceConn{
			hub:        hub,
			conn:       conn,
			send:       make(chan Message, sendBufferSize),
			deviceCode: device.DeviceCode,
			channelKey: channelKey,
		}

		hub.RegisterDevice(device.DeviceCode, channelKey, dc)

		// writePump runs in a separate goroutine; readPump blocks until the
		// connection closes (and then unregisters the device from the hub).
		go dc.writePump()
		dc.readPump()
	}
}
