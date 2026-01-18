package webauth

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
	ErrSessionExpired     = errors.New("session expired")
	ErrTokenRevoked       = errors.New("OSM access revoked by user")
	ErrTokenRefreshFailed = errors.New("temporary failure refreshing token")
)

// OAuthClient defines the interface for OAuth operations needed by the service
type OAuthClient interface {
	RefreshToken(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error)
}

// Service handles web session authentication and token refresh
type Service struct {
	conns   *db.Connections
	osmAuth OAuthClient
}

// NewService creates a new web auth service
func NewService(conns *db.Connections, osmAuth OAuthClient) *Service {
	return &Service{
		conns:   conns,
		osmAuth: osmAuth,
	}
}

// RefreshWebSessionToken refreshes the OSM token for a web session.
// It updates the database with the new tokens and returns the new access token.
func (s *Service) RefreshWebSessionToken(ctx context.Context, session *db.WebSession) (string, error) {
	if session.OSMRefreshToken == "" {
		slog.Error("webauth.refresh_token.no_refresh_token",
			"component", "webauth",
			"event", "token.refresh_error",
			"session_id", session.ID[:8],
		)
		return "", ErrTokenRefreshFailed
	}

	// Attempt to refresh the token
	newTokens, err := s.osmAuth.RefreshToken(ctx, session.OSMRefreshToken)
	if err != nil {
		// Check if this is an authorization error (user revoked access)
		if strings.Contains(err.Error(), "unauthorized (revoked)") {
			slog.Warn("webauth.refresh_token.revoked",
				"component", "webauth",
				"event", "token.revoked",
				"session_id", session.ID[:8],
				"error", err,
			)
			// Delete the session since access was revoked
			if delErr := db.DeleteWebSession(s.conns, session.ID); delErr != nil {
				slog.Error("webauth.refresh_token.session_delete_failed",
					"component", "webauth",
					"event", "token.revoke_error",
					"session_id", session.ID[:8],
					"error", delErr,
				)
			}
			return "", ErrTokenRevoked
		}

		// Temporary error (network, OSM server issue, etc.)
		slog.Error("webauth.refresh_token.failed",
			"component", "webauth",
			"event", "token.refresh_error",
			"session_id", session.ID[:8],
			"error", err,
		)
		return "", ErrTokenRefreshFailed
	}

	// Update tokens in database
	newExpiry := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
	if err := db.UpdateWebSessionTokens(s.conns, session.ID, newTokens.AccessToken, newTokens.RefreshToken, newExpiry); err != nil {
		slog.Error("webauth.refresh_token.update_failed",
			"component", "webauth",
			"event", "token.update_error",
			"session_id", session.ID[:8],
			"error", err,
		)
		return "", ErrTokenRefreshFailed
	}

	slog.Info("webauth.refresh_token.success",
		"component", "webauth",
		"event", "token.refreshed",
		"session_id", session.ID[:8],
	)

	return newTokens.AccessToken, nil
}
