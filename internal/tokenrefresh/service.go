package tokenrefresh

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm/oauthclient"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Errors returned by the token refresh service
var (
	ErrTokenRevoked       = errors.New("OSM access revoked by user")
	ErrTokenRefreshFailed = errors.New("temporary failure refreshing token")
)

// OAuthClient defines the interface for OAuth operations needed by the service
type OAuthClient interface {
	RefreshToken(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error)
}

// Service handles OAuth token refresh with proper error handling.
// It calls the OAuth client and invokes callbacks for storage updates.
type Service struct {
	oauthClient OAuthClient
}

// NewService creates a new token refresh service
func NewService(oauthClient OAuthClient) *Service {
	return &Service{
		oauthClient: oauthClient,
	}
}

// RefreshToken implements osm.TokenRefresher interface.
// It refreshes the OSM access token and calls the appropriate callback.
func (s *Service) RefreshToken(
	ctx context.Context,
	refreshToken string,
	identifier string,
	onSuccess func(accessToken, refreshToken string, expiry time.Time) error,
	onRevoked func() error,
) (string, error) {
	if refreshToken == "" {
		slog.Error("tokenrefresh.no_refresh_token",
			"component", "tokenrefresh",
			"event", "token.refresh_error",
			"identifier", identifier,
		)
		return "", ErrTokenRefreshFailed
	}

	// Attempt to refresh the token
	newTokens, err := s.oauthClient.RefreshToken(ctx, refreshToken)
	if err != nil {
		// Check if this is an authorization error (user revoked access)
		if errors.Is(err, oauthclient.ErrAccessRevoked) {
			slog.Warn("tokenrefresh.revoked",
				"component", "tokenrefresh",
				"event", "token.revoked",
				"identifier", identifier,
			)
			if onRevoked != nil {
				if revokeErr := onRevoked(); revokeErr != nil {
					slog.Error("tokenrefresh.revoke_callback_failed",
						"component", "tokenrefresh",
						"event", "token.revoke_error",
						"identifier", identifier,
						"error", revokeErr,
					)
				}
			}
			return "", ErrTokenRevoked
		}

		// Temporary error (network, OSM server issue, etc.)
		slog.Error("tokenrefresh.failed",
			"component", "tokenrefresh",
			"event", "token.refresh_error",
			"identifier", identifier,
			"error", err,
		)
		return "", ErrTokenRefreshFailed
	}

	// Calculate expiry and call success callback
	newExpiry := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
	if onSuccess != nil {
		if err := onSuccess(newTokens.AccessToken, newTokens.RefreshToken, newExpiry); err != nil {
			slog.Error("tokenrefresh.success_callback_failed",
				"component", "tokenrefresh",
				"event", "token.update_error",
				"identifier", identifier,
				"error", err,
			)
			return "", ErrTokenRefreshFailed
		}
	}

	slog.Info("tokenrefresh.success",
		"component", "tokenrefresh",
		"event", "token.refreshed",
		"identifier", identifier,
	)

	return newTokens.AccessToken, nil
}

// Ensure Service implements osm.TokenRefresher
var _ osm.TokenRefresher = (*Service)(nil)
