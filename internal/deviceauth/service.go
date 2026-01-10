package deviceauth

import (
	"net/http"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm/oauthclient"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Service handles device authentication and authorization
type Service struct {
	conns   *db.Connections
	osmAuth *oauthclient.WebFlowClient
}

// NewService creates a new device auth service
func NewService(conns *db.Connections, osmAuth *oauthclient.WebFlowClient) *Service {
	return &Service{
		conns:   conns,
		osmAuth: osmAuth,
	}
}

// AuthContext holds the authentication context for an authenticated API request
type AuthContext struct {
	deviceCodeRecord *db.DeviceCode
	osmAccessToken   string
}

// UserID implements types.User interface
func (a *AuthContext) UserID() *int {
	return a.deviceCodeRecord.OsmUserID
}

// AccessToken implements types.User interface
func (a *AuthContext) AccessToken() string {
	return a.osmAccessToken
}

// DeviceCode returns the device code record
func (a *AuthContext) DeviceCode() *db.DeviceCode {
	return a.deviceCodeRecord
}

// AuthenticateRequest handles authentication and authorization for API endpoints.
// It extracts the bearer token, verifies it, refreshes OSM tokens if needed, and returns
// the authentication context. Returns nil and false if authentication fails (error response
// is written to w).
func (s *Service) AuthenticateRequest(w http.ResponseWriter, r *http.Request) (types.User, bool) {
	// Extract bearer token from Authorization header
	authHeader := r.Header.Get("Authorization")
	accessToken := extractBearerToken(authHeader)

	if accessToken == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="API"`)
		http.Error(w, "Missing or invalid authorization", http.StatusUnauthorized)
		return nil, false
	}

	// Verify the access token belongs to a valid device
	deviceCodeRecord, err := db.FindDeviceCodeByAccessToken(s.conns, accessToken)
	if err != nil {
		http.Error(w, "Invalid access token", http.StatusUnauthorized)
		return nil, false
	}
	if deviceCodeRecord == nil {
		http.Error(w, "Invalid access token", http.StatusUnauthorized)
		return nil, false
	}

	osmRefreshToken := ""
	if deviceCodeRecord.OSMRefreshToken != nil {
		osmRefreshToken = *deviceCodeRecord.OSMRefreshToken
	}

	osmAccessToken := ""
	if deviceCodeRecord.OSMAccessToken != nil {
		osmAccessToken = *deviceCodeRecord.OSMAccessToken
	}

	// Check if we need to refresh the OSM token
	if deviceCodeRecord.OSMTokenExpiry != nil && time.Now().After(deviceCodeRecord.OSMTokenExpiry.Add(-5*time.Minute)) {
		// Token is expired or about to expire, refresh it
		newTokens, err := s.osmAuth.RefreshToken(r.Context(), osmRefreshToken)
		if err != nil {
			http.Error(w, "Failed to refresh token", http.StatusInternalServerError)
			return nil, false
		}

		// Update tokens in database
		newExpiry := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
		if err := db.UpdateDeviceCodeTokensOnly(s.conns, deviceCodeRecord.DeviceCode, newTokens.AccessToken, newTokens.RefreshToken, newExpiry); err != nil {
			http.Error(w, "Failed to update tokens", http.StatusInternalServerError)
			return nil, false
		}

		osmAccessToken = newTokens.AccessToken
	}

	return &AuthContext{
		deviceCodeRecord: deviceCodeRecord,
		osmAccessToken:   osmAccessToken,
	}, true
}

// extractBearerToken extracts the token from a Bearer authorization header
func extractBearerToken(authHeader string) string {
	const prefix = "Bearer "
	if len(authHeader) < len(prefix) {
		return ""
	}
	if authHeader[:len(prefix)] != prefix {
		return ""
	}
	return authHeader[len(prefix):]
}
