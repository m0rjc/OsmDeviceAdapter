package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
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
    <form method="GET" action="/oauth/authorize">
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
		var deviceCode string
		var status string
		err := deps.DB.QueryRow(`
			SELECT device_code, status FROM device_codes
			WHERE user_code = $1 AND expires_at > NOW()
		`, strings.ToUpper(userCode)).Scan(&deviceCode, &status)

		if err == sql.ErrNoRows {
			http.Error(w, "Invalid or expired user code", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		if status != "pending" {
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
		_, err = deps.DB.Exec(`
			INSERT INTO device_sessions (session_id, device_code, expires_at)
			VALUES ($1, $2, $3)
		`, sessionID, deviceCode, sessionExpiry)
		if err != nil {
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
		var deviceCode string
		err := deps.DB.QueryRow(`
			SELECT device_code FROM device_sessions
			WHERE session_id = $1 AND expires_at > NOW()
		`, state).Scan(&deviceCode)

		if err == sql.ErrNoRows {
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

		// Store tokens and mark device code as authorized
		tokenExpiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		_, err = deps.DB.Exec(`
			UPDATE device_codes
			SET status = 'authorized',
			    osm_access_token = $1,
			    osm_refresh_token = $2,
			    osm_token_expiry = $3
			WHERE device_code = $4
		`, tokenResp.AccessToken, tokenResp.RefreshToken, tokenExpiry, deviceCode)

		if err != nil {
			http.Error(w, "Failed to store tokens", http.StatusInternalServerError)
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
    <div class="success">✓</div>
    <h1>Authorization Successful</h1>
    <p>Your device has been authorized. You may close this window and return to your device.</p>
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

func markDeviceCodeStatus(db *sql.DB, sessionID, status string) {
	db.Exec(`
		UPDATE device_codes
		SET status = $1
		WHERE device_code = (
			SELECT device_code FROM device_sessions WHERE session_id = $2
		)
	`, status, sessionID)
}
