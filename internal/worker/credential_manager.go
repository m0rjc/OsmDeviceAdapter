package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreoutbox"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/usercredentials"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/tokenrefresh"
)

// CredentialManager handles token refresh for user credentials used in background processing.
// Similar to deviceauth.Service but for persistent user credentials.
type CredentialManager struct {
	conns          *db.Connections
	tokenRefresher osm.TokenRefresher
}

// NewCredentialManager creates a new credential manager
func NewCredentialManager(conns *db.Connections, tokenRefresher osm.TokenRefresher) *CredentialManager {
	return &CredentialManager{
		conns:          conns,
		tokenRefresher: tokenRefresher,
	}
}

// GetCredentials retrieves user credentials and refreshes tokens if needed.
// Returns the current access token and any error.
// Uses a 5-minute threshold for token refresh (same as deviceauth).
func (m *CredentialManager) GetCredentials(ctx context.Context, osmUserID int) (string, error) {
	// Fetch credentials from database
	credential, err := usercredentials.Get(m.conns, osmUserID)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials: %w", err)
	}
	if credential == nil {
		return "", fmt.Errorf("no credentials found for user %d", osmUserID)
	}

	accessToken := credential.OSMAccessToken

	// Check if we need to refresh the token (5-minute threshold)
	if time.Now().After(credential.OSMTokenExpiry.Add(-5 * time.Minute)) {
		slog.Debug("worker.credential_manager.token_refresh_needed",
			"component", "worker.credential_manager",
			"event", "token.refresh_needed",
			"osm_user_id", osmUserID,
		)

		newAccessToken, err := m.refreshCredentials(ctx, credential)
		if err != nil {
			// Token refresh failed - could be revoked or temporary error
			return "", err
		}
		accessToken = newAccessToken
	}

	return accessToken, nil
}

// refreshCredentials refreshes the OSM tokens for user credentials.
// Handles success (update tokens) and revocation (mark outbox entries as auth_revoked).
func (m *CredentialManager) refreshCredentials(ctx context.Context, credential *db.UserCredential) (string, error) {
	identifier := fmt.Sprintf("user:%d", credential.OSMUserID)

	return m.tokenRefresher.RefreshToken(
		ctx,
		credential.OSMRefreshToken,
		identifier,
		// onSuccess: update tokens in user_credentials table
		func(accessToken, refreshToken string, expiry time.Time) error {
			return usercredentials.UpdateTokens(m.conns, credential.OSMUserID, accessToken, refreshToken, expiry)
		},
		// onRevoked: mark all user's pending outbox entries as auth_revoked
		func() error {
			slog.Warn("worker.credential_manager.auth_revoked",
				"component", "worker.credential_manager",
				"event", "auth.revoked",
				"osm_user_id", credential.OSMUserID,
				"message", "User revoked OSM access - marking outbox entries as auth_revoked",
			)

			// Mark all pending/processing entries for this user as auth_revoked
			if err := scoreoutbox.MarkAuthRevoked(m.conns, credential.OSMUserID); err != nil {
				slog.Error("worker.credential_manager.mark_auth_revoked_failed",
					"component", "worker.credential_manager",
					"event", "auth_revoked.mark_failed",
					"osm_user_id", credential.OSMUserID,
					"error", err,
				)
				return err
			}

			// Note: We keep the credentials in the table
			// User may re-login, which will update credentials and trigger RecoverAuthRevoked
			return nil
		},
	)
}

// UpdateLastUsed updates the last_used_at timestamp for credentials after successful use
func (m *CredentialManager) UpdateLastUsed(ctx context.Context, osmUserID int) error {
	return usercredentials.UpdateLastUsed(m.conns, osmUserID)
}

// EnsureTokenRefreshed ensures tokens are fresh before use.
// Returns error if tokens cannot be refreshed or are revoked.
func (m *CredentialManager) EnsureTokenRefreshed(ctx context.Context, osmUserID int) (string, error) {
	return m.GetCredentials(ctx, osmUserID)
}

// IsAuthRevoked checks if a specific error indicates auth was revoked
func IsAuthRevoked(err error) bool {
	return errors.Is(err, tokenrefresh.ErrTokenRevoked)
}
