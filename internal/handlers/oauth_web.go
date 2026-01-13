package handlers

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

func OAuthAuthorizeHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// This endpoint is called when a user visits the verification URL
		// and enters their user code
		userCode := r.URL.Query().Get("user_code")

		if userCode == "" && r.Method == http.MethodGet {
			// Show form to enter user code
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Device Authorization</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 500px; margin: 50px auto; padding: 20px; }
        input { padding: 10px; font-size: 16px; width: 200px; text-transform: uppercase; }
        button { padding: 10px 20px; font-size: 16px; background: #007bff; color: white; border: none; cursor: pointer; }
        button:hover { background: #0056b3; }
    </style>
</head>
<body>
    <h1>Device Authorization</h1>
    <p>Enter the code displayed on your device:</p>
    <form method="GET" action="/device">
        <input type="text" name="user_code" placeholder="XXXX-XXXX" required />
        <button type="submit">Continue</button>
    </form>
</body>
</html>
			`)
			return
		}

		if userCode == "" {
			http.Error(w, "user_code is required", http.StatusBadRequest)
			return
		}

		// Rate limit device entry submissions (1 per N seconds per IP)
		remoteMetadata := middleware.RemoteFromContext(r.Context())
		clientIP := remoteMetadata.IP

		entryRateLimit := time.Duration(deps.Config.DeviceEntryRateLimit) * time.Second
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
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Please Slow Down</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 500px; margin: 50px auto; padding: 20px; }
        .warning { color: #ff6b6b; }
    </style>
</head>
<body>
    <h1 class="warning">Please Slow Down</h1>
    <p>You're submitting codes too quickly. Please wait %d seconds before trying again.</p>
    <p><a href="/device">Return to device authorization</a></p>
</body>
</html>
			`, deps.Config.DeviceEntryRateLimit)
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
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<title>Authorization Cancelled</title>
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<style>
		body { 
			font-family: Arial, sans-serif; 
			max-width: 600px; 
			margin: 50px auto; 
			padding: 20px;
			text-align: center;
			background: #f5f5f5;
		}
		.cancelled-icon {
			color: #dc3545;
			font-size: 72px;
			margin: 20px 0;
		}
		.content {
			background: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		h1 {
			color: #dc3545;
			margin: 20px 0;
		}
		p {
			color: #666;
			line-height: 1.6;
			margin: 15px 0;
		}
	</style>
</head>
<body>
	<div class="content">
		<div class="cancelled-icon">✖</div>
		<h1>Authorization Cancelled</h1>
		<p>You have denied access to the device. The authorization request has been cancelled.</p>
		<p>The device will not be able to access your patrol scores.</p>
		<p style="margin-top: 30px; font-size: 14px; color: #999;">You may close this window.</p>
	</div>
</body>
</html>
		`)
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
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<body>
    <h1>Authorization Denied</h1>
    <p>You have denied access to your device. You may close this window.</p>
</body>
</html>
			`)
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
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Authorization Successful</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 500px; margin: 50px auto; padding: 20px; text-align: center; }
        .success { color: #28a745; font-size: 48px; }
    </style>
</head>
<body>
    <h1 class="success">Authorization Successful</h1>
    <p>Your device has been authorized and configured for the selected scout section.</p>
    <p>You may close this window and return to your device.</p>
</body>
</html>
		`)
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
	w.Header().Set("Content-Type", "text/html")

	// Extract device metadata (handle NULL for old codes)
	// HTML-escape all values to prevent XSS injection
	deviceIP := "Unknown"
	if deviceCode.DeviceRequestIP != nil {
		deviceIP = html.EscapeString(*deviceCode.DeviceRequestIP)
	}

	deviceCountry := "Unknown"
	if deviceCode.DeviceRequestCountry != nil {
		deviceCountry = html.EscapeString(*deviceCode.DeviceRequestCountry)
	}

	deviceTime := "Unknown"
	if deviceCode.DeviceRequestTime != nil {
		deviceTime = html.EscapeString(deviceCode.DeviceRequestTime.Format("2006-01-02 15:04:05 MST"))
	}

	// Current user metadata
	// HTML-escape to prevent header injection attacks
	currentIP := html.EscapeString(currentMetadata.IP)
	currentCountry := currentMetadata.Country
	if currentCountry == "" {
		currentCountry = "Unknown"
	} else {
		currentCountry = html.EscapeString(currentCountry)
	}

	// Build country mismatch warning HTML
	countryWarning := ""
	if deviceCountry != "Unknown" && currentCountry != "Unknown" && deviceCountry != currentCountry {
		countryWarning = fmt.Sprintf(`
	<div class="warning">
		<div class="warning-title">⚠️ Country Mismatch Detected</div>
		<p>The device made its request from <strong>%s</strong>, but you are currently connecting from <strong>%s</strong>.</p>
		<p>This could indicate:</p>
		<ul>
			<li>You are using a VPN or proxy</li>
			<li>You are traveling</li>
			<li>Someone else may be attempting to authorize a device</li>
		</ul>
		<p><strong>Only continue if you recognize this device and initiated this authorization request.</strong></p>
	</div>
		`, deviceCountry, currentCountry)
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<title>Confirm Device Authorization</title>
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<style>
		body { 
			font-family: Arial, sans-serif; 
			max-width: 600px; 
			margin: 50px auto; 
			padding: 20px;
			background: #f5f5f5;
		}
		h1 {
			color: #333;
			border-bottom: 2px solid #4CAF50;
			padding-bottom: 10px;
		}
		.intro {
			background: white;
			padding: 20px;
			border-radius: 5px;
			margin: 20px 0;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.user-code {
			font-size: 32px;
			font-weight: bold;
			letter-spacing: 3px;
			margin: 20px 0;
			padding: 20px;
			background: #f0f0f0;
			border-radius: 5px;
			text-align: center;
			border: 2px solid #4CAF50;
			font-family: 'Courier New', monospace;
		}
		.info-section {
			background: white;
			margin: 20px 0;
			padding: 20px;
			border: 1px solid #ddd;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.info-section h3 {
			margin-top: 0;
			color: #555;
			border-bottom: 1px solid #ddd;
			padding-bottom: 10px;
		}
		.info-row {
			margin: 12px 0;
			display: flex;
		}
		.label {
			font-weight: bold;
			min-width: 120px;
			color: #666;
		}
		.value {
			color: #333;
		}
		.warning {
			background: #fff3cd;
			border: 2px solid #ffc107;
			padding: 20px;
			margin: 20px 0;
			border-radius: 5px;
			color: #856404;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.warning-title {
			font-weight: bold;
			font-size: 20px;
			margin-bottom: 15px;
		}
		.warning ul {
			margin: 10px 0;
			padding-left: 25px;
		}
		.warning li {
			margin: 8px 0;
		}
		.buttons {
			margin-top: 30px;
			display: flex;
			gap: 15px;
		}
		button {
			padding: 15px 30px;
			font-size: 16px;
			font-weight: bold;
			border: none;
			border-radius: 5px;
			cursor: pointer;
			transition: background 0.3s;
		}
		.btn-confirm {
			background: #28a745;
			color: white;
			flex: 1;
		}
		.btn-confirm:hover {
			background: #218838;
		}
		.btn-cancel {
			background: #dc3545;
			color: white;
			flex: 1;
		}
		.btn-cancel:hover {
			background: #c82333;
		}
		@media (max-width: 600px) {
			body {
				margin: 10px;
				padding: 10px;
			}
			.user-code {
				font-size: 24px;
			}
			.buttons {
				flex-direction: column;
			}
		}
	</style>
</head>
<body>
	<h1>Confirm Device Authorization</h1>
	
	<div class="intro">
		<p><strong>A device is requesting access to view Patrol Scores for your scout section.</strong></p>
		<p>Before proceeding, please verify the information below.</p>
	</div>

	<div class="info-section">
		<h3>Verify Device Code</h3>
		<p>Ensure that your device displays this code:</p>
		<div class="user-code">%s</div>
	</div>

	<div class="info-section">
		<h3>Device Information</h3>
		<div class="info-row">
			<span class="label">IP Address:</span>
			<span class="value">%s</span>
		</div>
		<div class="info-row">
			<span class="label">Country:</span>
			<span class="value">%s</span>
		</div>
		<div class="info-row">
			<span class="label">Requested:</span>
			<span class="value">%s</span>
		</div>
	</div>

	<div class="info-section">
		<h3>Your Current Connection</h3>
		<div class="info-row">
			<span class="label">IP Address:</span>
			<span class="value">%s</span>
		</div>
		<div class="info-row">
			<span class="label">Country:</span>
			<span class="value">%s</span>
		</div>
	</div>

	%s

	<form method="POST" action="/device/confirm">
		<input type="hidden" name="user_code" value="%s">
		<input type="hidden" name="session_id" value="%s">
		<div class="buttons">
			<button type="submit" class="btn-confirm">Confirm and Continue</button>
			<button type="button" class="btn-cancel" onclick="if(confirm('Are you sure you want to cancel this authorization?')) { window.location.href='/device/cancel?user_code=%s'; }">Cancel</button>
		</div>
	</form>
</body>
</html>
	`, html.EscapeString(userCode), deviceIP, deviceCountry, deviceTime, currentIP, currentCountry, countryWarning, html.EscapeString(userCode), html.EscapeString(sessionID), html.EscapeString(userCode))
}


func showSectionSelectionPage(w http.ResponseWriter, sessionID string, sections []types.OSMSection) {
	w.Header().Set("Content-Type", "text/html")

	// Build section options HTML
	sectionOptions := ""
	for _, section := range sections {
		sectionOptions += fmt.Sprintf(`
        <div class="section-option">
            <input type="radio" id="section_%d" name="section_id" value="%d" required>
            <label for="section_%d">
                <strong>%s</strong><br>
                <span class="group-name">%s</span>
            </label>
        </div>
		`, section.SectionID, section.SectionID, section.SectionID, section.SectionName, section.GroupName)
	}

	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Select Scout Section</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
        }
        h1 { color: #333; }
        .section-option {
            margin: 15px 0;
            padding: 15px;
            border: 2px solid #ddd;
            border-radius: 5px;
            cursor: pointer;
        }
        .section-option:hover {
            background-color: #f5f5f5;
        }
        .section-option input[type="radio"] {
            margin-right: 10px;
        }
        .section-option label {
            cursor: pointer;
            display: block;
        }
        .group-name {
            color: #666;
            font-size: 0.9em;
        }
        button {
            padding: 12px 24px;
            font-size: 16px;
            background: #007bff;
            color: white;
            border: none;
            border-radius: 5px;
            cursor: pointer;
            margin-top: 20px;
        }
        button:hover {
            background: #0056b3;
        }
    </style>
</head>
<body>
    <h1>Select Your Scout Section</h1>
    <p>Please select which scout section/troop you want to connect to your device:</p>
    <form method="POST" action="/device/select-section">
        <input type="hidden" name="session_id" value="%s">
        %s
        <button type="submit">Continue</button>
    </form>
</body>
</html>
	`, sessionID, sectionOptions)
}
