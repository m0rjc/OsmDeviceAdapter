package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreoutbox"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/usercredentials"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/websession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

const (
	// AdminSessionCookieName is the name of the session cookie for admin UI
	AdminSessionCookieName = "osm_admin_session"
	// AdminOAuthStateTTL is how long OAuth state tokens are valid
	AdminOAuthStateTTL = 15 * time.Minute
	// AdminSessionDuration is the default session duration (7 days)
	AdminSessionDuration = 7 * 24 * time.Hour
	// AdminOAuthScope is the OAuth scope required for admin operations
	AdminOAuthScope = "section:member:write"
)

// AdminLoginHandler initiates the OAuth flow for admin login.
// GET /admin/login: Generate state, redirect to OSM with write scope
func AdminLoginHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Generate a random state for CSRF protection
		state, err := generateSecureToken(32)
		if err != nil {
			slog.Error("admin.login.state_generation_failed",
				"component", "admin_oauth",
				"event", "login.error",
				"error", err,
			)
			http.Error(w, "Failed to initiate login", http.StatusInternalServerError)
			return
		}

		// Store state in Redis with TTL
		ctx := r.Context()
		if err := storeAdminOAuthState(ctx, deps.Conns.Redis, state); err != nil {
			slog.Error("admin.login.state_store_failed",
				"component", "admin_oauth",
				"event", "login.error",
				"error", err,
			)
			http.Error(w, "Failed to initiate login", http.StatusInternalServerError)
			return
		}

		// Build the authorization URL with write scope
		adminCallbackURL := fmt.Sprintf("%s/admin/callback", deps.Config.ExternalDomains.ExposedDomain)
		authURL := buildAdminAuthURL(deps, state, adminCallbackURL)

		slog.Info("admin.login.initiated",
			"component", "admin_oauth",
			"event", "login.redirect",
		)

		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// AdminCallbackHandler handles the OAuth callback from OSM.
// GET /admin/callback: Exchange code for tokens, create session, redirect to admin UI
func AdminCallbackHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errorParam := r.URL.Query().Get("error")

		// Handle OAuth errors
		if errorParam != "" {
			slog.Warn("admin.callback.oauth_error",
				"component", "admin_oauth",
				"event", "callback.error",
				"oauth_error", errorParam,
			)
			http.Error(w, "Authorization denied", http.StatusUnauthorized)
			return
		}

		if code == "" || state == "" {
			slog.Warn("admin.callback.missing_params",
				"component", "admin_oauth",
				"event", "callback.error",
			)
			http.Error(w, "Missing required parameters", http.StatusBadRequest)
			return
		}

		// Verify state for CSRF protection
		ctx := r.Context()
		valid, err := verifyAndDeleteAdminOAuthState(ctx, deps.Conns.Redis, state)
		if err != nil {
			slog.Error("admin.callback.state_verify_failed",
				"component", "admin_oauth",
				"event", "callback.error",
				"error", err,
			)
			http.Error(w, "Failed to verify state", http.StatusInternalServerError)
			return
		}
		if !valid {
			slog.Warn("admin.callback.invalid_state",
				"component", "admin_oauth",
				"event", "callback.error",
			)
			http.Error(w, "Invalid or expired state", http.StatusBadRequest)
			return
		}

		// Exchange authorization code for tokens
		adminCallbackURL := fmt.Sprintf("%s/admin/callback", deps.Config.ExternalDomains.ExposedDomain)
		tokenResp, err := exchangeAdminCode(ctx, deps, code, adminCallbackURL)
		if err != nil {
			slog.Error("admin.callback.token_exchange_failed",
				"component", "admin_oauth",
				"event", "callback.error",
				"error", err,
			)
			http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
			return
		}

		// Fetch user profile to get user ID
		profile, err := deps.OSM.FetchOSMProfile(r.Context(), types.NewUser(nil, tokenResp.AccessToken))
		if err != nil {
			slog.Error("admin.callback.profile_fetch_failed",
				"component", "admin_oauth",
				"event", "callback.error",
				"error", err,
			)
			http.Error(w, "Failed to fetch user profile", http.StatusInternalServerError)
			return
		}

		if profile.Data == nil {
			slog.Warn("admin.callback.no_profile_data",
				"component", "admin_oauth",
				"event", "callback.error",
			)
			http.Error(w, "No profile data returned", http.StatusBadRequest)
			return
		}

		// Generate session ID and CSRF token
		sessionID, err := generateUUID()
		if err != nil {
			slog.Error("admin.callback.session_id_generation_failed",
				"component", "admin_oauth",
				"event", "callback.error",
				"error", err,
			)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}
		csrfToken, err := generateSecureToken(32)
		if err != nil {
			slog.Error("admin.callback.csrf_generation_failed",
				"component", "admin_oauth",
				"event", "callback.error",
				"error", err,
			)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		// Calculate expiry times
		now := time.Now()
		tokenExpiry := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		sessionExpiry := now.Add(AdminSessionDuration)

		// Create web session
		session := &db.WebSession{
			ID:              sessionID,
			OSMUserID:       profile.Data.UserID,
			OSMAccessToken:  tokenResp.AccessToken,
			OSMRefreshToken: tokenResp.RefreshToken,
			OSMTokenExpiry:  tokenExpiry,
			CSRFToken:       csrfToken,
			CreatedAt:       now,
			LastActivity:    now,
			ExpiresAt:       sessionExpiry,
		}

		if err := websession.Create(deps.Conns, session); err != nil {
			slog.Error("admin.callback.session_create_failed",
				"component", "admin_oauth",
				"event", "callback.error",
				"error", err,
			)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		// Create or update user credentials for background processing
		// This allows outbox entries to be processed even after web session expires
		credential := &db.UserCredential{
			OSMUserID:       profile.Data.UserID,
			OSMUserName:     profile.Data.FullName,
			OSMEmail:        profile.Data.Email,
			OSMAccessToken:  tokenResp.AccessToken,
			OSMRefreshToken: tokenResp.RefreshToken,
			OSMTokenExpiry:  tokenExpiry,
		}

		if err := usercredentials.CreateOrUpdate(deps.Conns, credential); err != nil {
			slog.Error("admin.callback.credentials_create_failed",
				"component", "admin_oauth",
				"event", "callback.error",
				"user_id", profile.Data.UserID,
				"error", err,
			)
			// Don't fail the login - web session is still valid
			// Outbox processing will fail gracefully if credentials missing
		} else {
			// If credentials were updated, recover any auth_revoked outbox entries
			// User has re-authorized, so we can retry pending writes
			if err := scoreoutbox.RecoverAuthRevoked(deps.Conns, profile.Data.UserID); err != nil {
				slog.Error("admin.callback.recover_auth_revoked_failed",
					"component", "admin_oauth",
					"event", "callback.warning",
					"user_id", profile.Data.UserID,
					"error", err,
				)
				// Don't fail - this is a recovery operation
			}
		}

		// Set secure session cookie
		setSessionCookie(w, sessionID, sessionExpiry)

		slog.Info("admin.callback.success",
			"component", "admin_oauth",
			"event", "callback.success",
			"user_id", profile.Data.UserID,
		)

		// Redirect to admin UI
		http.Redirect(w, r, "/admin/", http.StatusFound)
	}
}

// AdminLogoutHandler handles admin logout.
// GET /admin/logout: Clear session from DB and cookie, redirect to home
func AdminLogoutHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get session ID from cookie
		cookie, err := r.Cookie(AdminSessionCookieName)
		if err == nil && cookie.Value != "" {
			// Delete session from database
			if err := websession.Delete(deps.Conns, cookie.Value); err != nil {
				slog.Error("admin.logout.session_delete_failed",
					"component", "admin_oauth",
					"event", "logout.error",
					"error", err,
				)
				// Continue with logout even if DB delete fails
			}
		}

		// Clear the session cookie
		clearSessionCookie(w)

		slog.Info("admin.logout.success",
			"component", "admin_oauth",
			"event", "logout.success",
		)

		// Redirect to home page
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// Helper functions

// generateSecureToken generates a cryptographically secure random token
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// generateUUID generates a random UUID v4 string
func generateUUID() (string, error) {
	uuid := make([]byte, 16)
	if _, err := rand.Read(uuid); err != nil {
		return "", err
	}
	// Set version (4) and variant (RFC 4122)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

// storeAdminOAuthState stores an OAuth state in Redis with TTL
func storeAdminOAuthState(ctx context.Context, redis *db.RedisClient, state string) error {
	key := fmt.Sprintf("admin_oauth_state:%s", state)
	return redis.Set(ctx, key, "1", AdminOAuthStateTTL).Err()
}

// verifyAndDeleteAdminOAuthState verifies an OAuth state exists and deletes it (one-time use)
func verifyAndDeleteAdminOAuthState(ctx context.Context, redis *db.RedisClient, state string) (bool, error) {
	key := fmt.Sprintf("admin_oauth_state:%s", state)

	// Check if state exists
	result, err := redis.Get(ctx, key).Result()
	if err != nil {
		// Key doesn't exist or error
		return false, nil
	}
	if result == "" {
		return false, nil
	}

	// Delete the state (one-time use)
	if err := redis.Del(ctx, key).Err(); err != nil {
		slog.Warn("admin.oauth.state_delete_failed",
			"component", "admin_oauth",
			"error", err,
		)
		// Continue even if delete fails - state will expire naturally
	}

	return true, nil
}

// buildAdminAuthURL builds the OAuth authorization URL for admin login
func buildAdminAuthURL(deps *Dependencies, state, callbackURL string) string {
	osmDomain := deps.Config.ExternalDomains.OSMDomain
	clientID := deps.Config.OAuth.OSMClientID

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", callbackURL)
	params.Set("response_type", "code")
	params.Set("state", state)
	params.Set("scope", AdminOAuthScope)

	return fmt.Sprintf("%s/oauth/authorize?%s", osmDomain, params.Encode())
}

// exchangeAdminCode exchanges an authorization code for tokens using the admin callback URL
func exchangeAdminCode(ctx context.Context, deps *Dependencies, code, callbackURL string) (*types.OSMTokenResponse, error) {
	osmDomain := deps.Config.ExternalDomains.OSMDomain
	clientID := deps.Config.OAuth.OSMClientID
	clientSecret := deps.Config.OAuth.OSMClientSecret

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", callbackURL)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	tokenURL := osmDomain + "/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp types.OSMTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// setSessionCookie sets the secure session cookie
func setSessionCookie(w http.ResponseWriter, sessionID string, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     AdminSessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie clears the session cookie
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     AdminSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
