package deviceauth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Authentication errors
var (
	ErrInvalidToken       = errors.New("invalid access token")
	ErrTokenRevoked       = errors.New("OSM access revoked by user")
	ErrTokenRefreshFailed = errors.New("temporary failure refreshing token")
)

// OAuthClient defines the interface for OAuth operations needed by the service
type OAuthClient interface {
	RefreshToken(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error)
}

// Service handles device authentication and authorization
type Service struct {
	conns   *db.Connections
	osmAuth OAuthClient
}

// NewService creates a new device auth service
func NewService(conns *db.Connections, osmAuth OAuthClient) *Service {
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

// Authenticate verifies a bearer token and returns the authenticated user.
// It handles token refresh if the OSM token is near expiry.
// Returns ErrInvalidToken, ErrTokenRevoked, or ErrTokenRefreshFailed on failure.
func (s *Service) Authenticate(ctx context.Context, authHeader string) (types.User, error) {
	// Extract bearer token from Authorization header
	accessToken := extractBearerToken(authHeader)

	if accessToken == "" {
		return nil, ErrInvalidToken
	}

	// Verify the device access token belongs to a valid device
	deviceCodeRecord, err := db.FindDeviceCodeByDeviceAccessToken(s.conns, accessToken)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if deviceCodeRecord == nil {
		return nil, ErrInvalidToken
	}

	osmAccessToken := ""
	if deviceCodeRecord.OSMAccessToken != nil {
		osmAccessToken = *deviceCodeRecord.OSMAccessToken
	}

	// Check if we need to refresh the OSM token
	if deviceCodeRecord.OSMTokenExpiry != nil && time.Now().After(deviceCodeRecord.OSMTokenExpiry.Add(-5*time.Minute)) {
		// Token is expired or about to expire, refresh it
		newAccessToken, err := s.RefreshUserTokenFromRecord(ctx, deviceCodeRecord)
		if err != nil {
			// RefreshUserTokenFromRecord already handles logging and database updates
			return nil, err
		}

		osmAccessToken = newAccessToken
	}

	// Update last_used_at timestamp for this device
	if err := db.UpdateDeviceCodeLastUsed(s.conns, deviceCodeRecord.DeviceCode); err != nil {
		// Log the error but don't fail the authentication
		slog.Error("deviceauth.last_used_update_failed",
			"component", "deviceauth",
			"event", "last_used.update_error",
			"device_code_hash", deviceCodeRecord.DeviceCode[:8],
			"error", err,
		)
	}

	return &AuthContext{
		deviceCodeRecord: deviceCodeRecord,
		osmAccessToken:   osmAccessToken,
	}, nil
}

// RefreshUserTokenFromRecord implements the osm.TokenRefresher interface.
// It attempts to refresh the OSM access token for the given device code record.
// If refresh fails with 401 (user revoked access), it marks the device as revoked.
// Returns the new access token on success, or an error if refresh fails.
func (s *Service) RefreshUserTokenFromRecord(ctx context.Context, deviceCodeRecordInterface interface{}) (string, error) {
	// Type assert the interface{} to *db.DeviceCode
	deviceCodeRecord, ok := deviceCodeRecordInterface.(*db.DeviceCode)
	if !ok {
		slog.Error("deviceauth.refresh_user_token.invalid_type",
			"component", "deviceauth",
			"event", "token.refresh_error",
			"error", "deviceCodeRecord is not *db.DeviceCode",
		)
		return "", ErrInvalidToken
	}

	deviceCode := deviceCodeRecord.DeviceCode

	// Get the refresh token
	if deviceCodeRecord.OSMRefreshToken == nil {
		slog.Error("deviceauth.refresh_user_token.no_refresh_token",
			"component", "deviceauth",
			"event", "token.refresh_error",
			"device_code_hash", deviceCode[:8],
		)
		return "", ErrTokenRefreshFailed
	}

	// Attempt to refresh the token
	newTokens, err := s.osmAuth.RefreshToken(ctx, *deviceCodeRecord.OSMRefreshToken)
	if err != nil {
		// Check if this is an authorization error (user revoked access) // FIXME: Use sentinel error here.
		if strings.Contains(err.Error(), "unauthorized (revoked)") {
			slog.Warn("deviceauth.refresh_user_token.revoked",
				"component", "deviceauth",
				"event", "token.revoked",
				"device_code_hash", deviceCode[:8],
				"error", err,
			)
			// Mark device as revoked in database
			if revokeErr := db.RevokeDeviceCode(s.conns, deviceCode); revokeErr != nil {
				slog.Error("deviceauth.refresh_user_token.revoke_failed",
					"component", "deviceauth",
					"event", "token.revoke_error",
					"device_code_hash", deviceCode[:8],
					"error", revokeErr,
				)
			}
			return "", ErrTokenRevoked
		}

		// Temporary error (network, OSM server issue, etc.)
		slog.Error("deviceauth.refresh_user_token.failed",
			"component", "deviceauth",
			"event", "token.refresh_error",
			"device_code_hash", deviceCode[:8],
			"error", err,
		)
		return "", ErrTokenRefreshFailed
	}

	// Update tokens in database
	newExpiry := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
	if err := db.UpdateDeviceCodeTokensOnly(s.conns, deviceCode, newTokens.AccessToken, newTokens.RefreshToken, newExpiry); err != nil {
		slog.Error("deviceauth.refresh_user_token.update_failed",
			"component", "deviceauth",
			"event", "token.update_error",
			"device_code_hash", deviceCode[:8],
			"error", err,
		)
		return "", ErrTokenRefreshFailed
	}

	slog.Info("deviceauth.refresh_user_token.success",
		"component", "deviceauth",
		"event", "token.refreshed",
		"device_code_hash", deviceCode[:8],
	)

	return newTokens.AccessToken, nil
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
