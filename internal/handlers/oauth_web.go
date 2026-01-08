package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"gorm.io/gorm"
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

		// Look up the device code from user code
		var deviceCodeRecord db.DeviceCode
		err := deps.DB.Where("user_code = ? AND expires_at > ?", strings.ToUpper(userCode), time.Now()).First(&deviceCodeRecord).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Invalid or expired user code", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
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
		if err := deps.DB.Create(session).Error; err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		// Redirect to OSM OAuth authorization
		authURL := buildOSMAuthURL(deps.Config, sessionID)
		http.Redirect(w, r, authURL, http.StatusFound)
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
				markDeviceCodeStatus(deps.DB, state, "denied")
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
		var session db.DeviceSession
		err := deps.DB.Where("session_id = ? AND expires_at > ?", state, time.Now()).First(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Invalid or expired session", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Exchange authorization code for access token
		tokenResp, err := exchangeCodeForToken(deps.Config, code)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to exchange code: %v", err), http.StatusInternalServerError)
			return
		}

		// Fetch user profile to get sections
		profile, err := fetchOSMProfile(deps.Config, tokenResp.AccessToken)
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
		updates := map[string]interface{}{
			"status":            "awaiting_section",
			"osm_access_token":  tokenResp.AccessToken,
			"osm_refresh_token": tokenResp.RefreshToken,
			"osm_token_expiry":  tokenExpiry,
		}
		if err := deps.DB.Model(&db.DeviceCode{}).Where("device_code = ?", session.DeviceCode).Updates(updates).Error; err != nil {
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
		var session db.DeviceSession
		err := deps.DB.Where("session_id = ? AND expires_at > ?", sessionID, time.Now()).First(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Invalid or expired session", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Update device code with section ID and mark as authorized
		updates := map[string]interface{}{
			"status":     "authorized",
			"section_id": sectionID,
		}
		if err := deps.DB.Model(&db.DeviceCode{}).Where("device_code = ?", session.DeviceCode).Updates(updates).Error; err != nil {
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

func buildOSMAuthURL(cfg *config.Config, state string) string {
	params := url.Values{}
	params.Set("client_id", cfg.OSMClientID)
	params.Set("redirect_uri", cfg.OSMRedirectURI)
	params.Set("response_type", "code")
	params.Set("state", state)
	params.Set("scope", "section:member:read") // Adjust scope as needed

	return fmt.Sprintf("%s/oauth/authorize?%s", cfg.OSMDomain, params.Encode())
}

func exchangeCodeForToken(cfg *config.Config, code string) (*types.OSMTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", cfg.OSMRedirectURI)
	data.Set("client_id", cfg.OSMClientID)
	data.Set("client_secret", cfg.OSMClientSecret)

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		fmt.Sprintf("%s/oauth/token", cfg.OSMDomain),
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp types.OSMTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func markDeviceCodeStatus(database *gorm.DB, sessionID, status string) {
	var session db.DeviceSession
	if err := database.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		return
	}
	database.Model(&db.DeviceCode{}).Where("device_code = ?", session.DeviceCode).Update("status", status)
}

func fetchOSMProfile(cfg *config.Config, accessToken string) (*types.OSMProfileResponse, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/oauth/resource", cfg.OSMDomain),
		nil,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("profile fetch failed: %s - %s", resp.Status, string(body))
	}

	var profileResp types.OSMProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profileResp); err != nil {
		return nil, err
	}

	return &profileResp, nil
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
