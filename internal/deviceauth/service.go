package deviceauth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/devicecode"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/tokenrefresh"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Authentication errors
var (
	ErrInvalidToken       = errors.New("invalid access token")
	ErrTokenRevoked       = tokenrefresh.ErrTokenRevoked
	ErrTokenRefreshFailed = tokenrefresh.ErrTokenRefreshFailed
)

// Service handles device authentication and authorization
type Service struct {
	conns          *db.Connections
	tokenRefresher osm.TokenRefresher
}

// NewService creates a new device auth service
func NewService(conns *db.Connections, tokenRefresher osm.TokenRefresher) *Service {
	return &Service{
		conns:          conns,
		tokenRefresher: tokenRefresher,
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
	deviceCodeRecord, err := devicecode.FindByDeviceAccessToken(s.conns, accessToken)
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
		newAccessToken, err := s.refreshDeviceToken(ctx, deviceCodeRecord)
		if err != nil {
			return nil, err
		}

		osmAccessToken = newAccessToken
	}

	// Update last_used_at timestamp for this device
	if err := devicecode.UpdateLastUsed(s.conns, deviceCodeRecord.DeviceCode); err != nil {
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

// refreshDeviceToken refreshes the OSM token for a device using the central token refresh service.
func (s *Service) refreshDeviceToken(ctx context.Context, deviceCodeRecord *db.DeviceCode) (string, error) {
	refreshToken := ""
	if deviceCodeRecord.OSMRefreshToken != nil {
		refreshToken = *deviceCodeRecord.OSMRefreshToken
	}

	identifier := deviceCodeRecord.DeviceCode[:8]

	return s.tokenRefresher.RefreshToken(
		ctx,
		refreshToken,
		identifier,
		// onSuccess: update tokens in database
		func(accessToken, newRefreshToken string, expiry time.Time) error {
			return devicecode.UpdateTokensOnly(s.conns, deviceCodeRecord.DeviceCode, accessToken, newRefreshToken, expiry)
		},
		// onRevoked: mark device as revoked
		func() error {
			return devicecode.Revoke(s.conns, deviceCodeRecord.DeviceCode)
		},
	)
}

// CreateRefreshFunc creates a bound refresh function for a device code record.
// This function can be stored in context for automatic token refresh on 401.
func (s *Service) CreateRefreshFunc(deviceCodeRecord *db.DeviceCode) types.TokenRefreshFunc {
	return func(ctx context.Context) (string, error) {
		return s.refreshDeviceToken(ctx, deviceCodeRecord)
	}
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
