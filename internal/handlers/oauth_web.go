package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/m0rjc/OsmDeviceAdapter/internal/templates"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// HomeHandler renders the home page with a welcome message and device code entry form
func HomeHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := templates.RenderHome(w); err != nil {
			slog.Error("template render failed", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

func OAuthAuthorizeHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// This endpoint is called when a user visits the verification URL
		// and enters their user code
		userCode := r.URL.Query().Get("user_code")

		if userCode == "" && r.Method == http.MethodGet {
			// Show form to enter user code
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := templates.RenderDeviceAuth(w); err != nil {
				slog.Error("template render failed", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		if userCode == "" {
			http.Error(w, "user_code is required", http.StatusBadRequest)
			return
		}

		// Rate limit device entry submissions (1 per N seconds per IP)
		remoteMetadata := middleware.RemoteFromContext(r.Context())
		clientIP := remoteMetadata.IP

		entryRateLimit := time.Duration(deps.Config.RateLimit.DeviceEntryRateLimit) * time.Second
		entryRateLimitKey := fmt.Sprintf("%s:device_entry", clientIP)

		rateLimitResult, err := deps.Conns.GetRateLimiter().CheckRateLimit(
			r.Context(),
			"device_entry",
			entryRateLimitKey,
			1, // Only 1 submission allowed per window
			entryRateLimit,
		)

		if err != nil {
			slog.Error("device.entry.rate_limit_error",
				"component", "oauth_web",
				"event", "entry.rate_limit_error",
				"client_ip", clientIP,
				"error", err,
			)
			// Continue on error - don't block legitimate requests
		} else if !rateLimitResult.Allowed {
			slog.Warn("device.entry.rate_limited",
				"component", "oauth_web",
				"event", "entry.rate_limited",
				"client_ip", clientIP,
				"retry_after", rateLimitResult.RetryAfter.Seconds(),
			)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := templates.RenderRateLimited(w, deps.Config.RateLimit.DeviceEntryRateLimit); err != nil {
				slog.Error("template render failed", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		// Look up the device code from user code
		deviceCodeRecord, err := db.FindDeviceCodeByUserCode(deps.Conns, strings.ToUpper(userCode))
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if deviceCodeRecord == nil {
			http.Error(w, "Invalid or expired user code", http.StatusBadRequest)
			return
		}

		if deviceCodeRecord.Status != "pending" {
			http.Error(w, "This code has already been used", http.StatusBadRequest)
			return
		}

		// Create session for this authorization flow
		sessionID, err := generateRandomString(32)
		if err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		sessionExpiry := time.Now().Add(15 * time.Minute)
		session := &db.DeviceSession{
			SessionID:  sessionID,
			DeviceCode: deviceCodeRecord.DeviceCode,
			ExpiresAt:  sessionExpiry,
			CreatedAt:  time.Now(),
		}
		if err := db.CreateDeviceSession(deps.Conns, session); err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		// Log confirmation page display
		deviceCountry := "unknown"
		if deviceCodeRecord.DeviceRequestCountry != nil {
			deviceCountry = *deviceCodeRecord.DeviceRequestCountry
		}
		slog.Info("device.confirmation.shown",
			"component", "oauth_web",
			"event", "confirmation.shown",
			"user_code", userCode,
			"device_country", deviceCountry,
			"user_country", remoteMetadata.Country,
		)

		// Show confirmation page instead of immediate OAuth redirect
		showDeviceConfirmationPage(w, userCode, deviceCodeRecord, remoteMetadata, sessionID)
	}
}

func OAuthConfirmHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse form
		if err := r.ParseForm(); err != nil {
			slog.Error("device.confirmation.parse_error",
				"component", "oauth_web",
				"event", "confirmation.parse_error",
				"error", err,
			)
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		userCode := r.FormValue("user_code")
		sessionID := r.FormValue("session_id")

		if userCode == "" || sessionID == "" {
			slog.Warn("device.confirmation.missing_fields",
				"component", "oauth_web",
				"event", "confirmation.missing_fields",
			)
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// Validate session exists and matches device code (CSRF protection)
		session, err := db.FindDeviceSessionByID(deps.Conns, sessionID)
		if err != nil || session == nil {
			slog.Warn("device.confirmation.invalid_session",
				"component", "oauth_web",
				"event", "confirmation.invalid_session",
				"session_id", sessionID,
			)
			http.Error(w, "Invalid or expired session", http.StatusBadRequest)
			return
		}

		// Lookup device code
		deviceCodeRecord, err := db.FindDeviceCodeByUserCode(deps.Conns, strings.ToUpper(userCode))
		if err != nil || deviceCodeRecord == nil {
			slog.Warn("device.confirmation.invalid_code",
				"component", "oauth_web",
				"event", "confirmation.invalid_code",
				"user_code", userCode,
			)
			http.Error(w, "Invalid or expired user code", http.StatusBadRequest)
			return
		}

		// Verify session belongs to this device code (CSRF protection)
		if session.DeviceCode != deviceCodeRecord.DeviceCode {
			slog.Error("device.confirmation.session_mismatch",
				"component", "oauth_web",
				"event", "confirmation.session_mismatch",
				"session_id", sessionID,
				"user_code", userCode,
			)
			http.Error(w, "Session mismatch", http.StatusBadRequest)
			return
		}

		// Check device code status
		if deviceCodeRecord.Status != "pending" {
			slog.Warn("device.confirmation.already_used",
				"component", "oauth_web",
				"event", "confirmation.already_used",
				"user_code", userCode,
				"status", deviceCodeRecord.Status,
			)
			http.Error(w, "This code has already been used", http.StatusBadRequest)
			return
		}

		// Log confirmation
		remoteMetadata := middleware.RemoteFromContext(r.Context())
		deviceCountry := "unknown"
		if deviceCodeRecord.DeviceRequestCountry != nil {
			deviceCountry = *deviceCodeRecord.DeviceRequestCountry
		}
		countryMatch := deviceCountry == remoteMetadata.Country

		slog.Info("device.confirmation.accepted",
			"component", "oauth_web",
			"event", "confirmation.accepted",
			"user_code", userCode,
			"device_country", deviceCountry,
			"user_country", remoteMetadata.Country,
			"country_match", countryMatch,
		)

		// Proceed with OAuth authorization
		authURL := deps.OSMAuth.BuildAuthURL("", sessionID)
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

func OAuthCancelHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userCode := r.URL.Query().Get("user_code")
		if userCode == "" {
			http.Error(w, "user_code is required", http.StatusBadRequest)
			return
		}

		// Look up device code
		deviceCodeRecord, err := db.FindDeviceCodeByUserCode(deps.Conns, strings.ToUpper(userCode))
		if err != nil || deviceCodeRecord == nil {
			slog.Warn("device.cancel.invalid_code",
				"component", "oauth_web",
				"event", "cancel.invalid_code",
				"user_code", userCode,
			)
			http.Error(w, "Invalid or expired user code", http.StatusBadRequest)
			return
		}

		// Mark as denied
		if err := db.UpdateDeviceCodeStatus(deps.Conns, deviceCodeRecord.DeviceCode, "denied"); err != nil {
			slog.Error("device.cancel.update_failed",
				"component", "oauth_web",
				"event", "cancel.update_failed",
				"user_code", userCode,
				"error", err,
			)
			http.Error(w, "Failed to cancel authorization", http.StatusInternalServerError)
			return
		}

		// Log cancellation
		remoteMetadata := middleware.RemoteFromContext(r.Context())
		slog.Info("device.confirmation.cancelled",
			"component", "oauth_web",
			"event", "confirmation.cancelled",
			"user_code", userCode,
			"client_ip", remoteMetadata.IP,
		)

		// Show cancellation page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := templates.RenderAuthCancelled(w); err != nil {
			slog.Error("template render failed", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}


func OAuthCallbackHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state") // This is our session_id
		errorParam := r.URL.Query().Get("error")

		if errorParam != "" {
			// User denied authorization
			if state != "" {
				markDeviceCodeStatus(deps.Conns, state, "denied")
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := templates.RenderAuthDenied(w); err != nil {
				slog.Error("template render failed", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		if code == "" || state == "" {
			http.Error(w, "Missing required parameters", http.StatusBadRequest)
			return
		}

		// Look up session to get device code
		session, err := db.FindDeviceSessionByID(deps.Conns, state)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if session == nil {
			http.Error(w, "Invalid or expired session", http.StatusBadRequest)
			return
		}

		// Exchange authorization code for access token
		tokenResp, err := deps.OSMAuth.ExchangeCodeForToken(code)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to exchange code: %v", err), http.StatusInternalServerError)
			return
		}

		// Fetch user profile to get sections  -- CLAUDE: I have fixed this
		profile, err := deps.OSM.FetchOSMProfile(types.NewUser(nil, tokenResp.AccessToken))
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch profile: %v", err), http.StatusInternalServerError)
			return
		}

		if profile.Data == nil || len(profile.Data.Sections) == 0 {
			http.Error(w, "No sections found for this account", http.StatusBadRequest)
			return
		}

		// Store tokens (but not mark as authorized yet - waiting for section selection)
		tokenExpiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		if err := db.UpdateDeviceCodeWithTokens(deps.Conns, session.DeviceCode, "awaiting_section", tokenResp.AccessToken, tokenResp.RefreshToken, tokenExpiry, profile.Data.UserID); err != nil {
			http.Error(w, "Failed to store tokens", http.StatusInternalServerError)
			return
		}

		// Show section selection page
		showSectionSelectionPage(w, state, profile.Data.Sections)
	}
}

func OAuthSelectSectionHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.FormValue("session_id")
		sectionIDStr := r.FormValue("section_id")

		if sessionID == "" || sectionIDStr == "" {
			http.Error(w, "Missing required parameters", http.StatusBadRequest)
			return
		}

		// Parse section ID
		var sectionID int
		if _, err := fmt.Sscanf(sectionIDStr, "%d", &sectionID); err != nil {
			http.Error(w, "Invalid section ID", http.StatusBadRequest)
			return
		}

		// Look up session to get device code
		session, err := db.FindDeviceSessionByID(deps.Conns, sessionID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if session == nil {
			http.Error(w, "Invalid or expired session", http.StatusBadRequest)
			return
		}

		// Generate device access token
		deviceAccessToken, err := generateDeviceAccessToken()
		if err != nil {
			http.Error(w, "Failed to generate device access token", http.StatusInternalServerError)
			return
		}

		// Update device code with section ID, device access token, and mark as authorized
		if err := db.UpdateDeviceCodeWithSection(deps.Conns, session.DeviceCode, "authorized", sectionID, deviceAccessToken); err != nil {
			http.Error(w, "Failed to update device code", http.StatusInternalServerError)
			return
		}

		// Show success page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := templates.RenderAuthSuccess(w); err != nil {
			slog.Error("template render failed", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

func markDeviceCodeStatus(conns *db.Connections, sessionID, status string) {
	session, err := db.FindDeviceSessionByID(conns, sessionID)
	if err != nil || session == nil {
		return
	}
	err = db.UpdateDeviceCodeStatus(conns, session.DeviceCode, status)
	if err != nil {
		// FIXME: Log this
		return
	}
}

func showDeviceConfirmationPage(w http.ResponseWriter, userCode string, deviceCode *db.DeviceCode, currentMetadata middleware.RemoteMetadata, sessionID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Extract device metadata (handle NULL for old codes)
	// Note: html/template provides automatic HTML escaping
	deviceIP := "Unknown"
	if deviceCode.DeviceRequestIP != nil {
		deviceIP = *deviceCode.DeviceRequestIP
	}

	deviceCountry := "Unknown"
	if deviceCode.DeviceRequestCountry != nil {
		deviceCountry = *deviceCode.DeviceRequestCountry
	}

	deviceTime := "Unknown"
	if deviceCode.DeviceRequestTime != nil {
		deviceTime = deviceCode.DeviceRequestTime.Format("2006-01-02 15:04:05 MST")
	}

	// Current user metadata
	currentIP := currentMetadata.IP
	currentCountry := currentMetadata.Country
	if currentCountry == "" {
		currentCountry = "Unknown"
	}

	// Determine if we should show country mismatch warning
	showCountryWarning := deviceCountry != "Unknown" && currentCountry != "Unknown" && deviceCountry != currentCountry

	if err := templates.RenderDeviceConfirm(w, userCode, deviceIP, deviceCountry, deviceTime, currentIP, currentCountry, sessionID, showCountryWarning); err != nil {
		slog.Error("template render failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}


func showSectionSelectionPage(w http.ResponseWriter, sessionID string, sections []types.OSMSection) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.RenderSectionSelect(w, sessionID, sections); err != nil {
		slog.Error("template render failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
