package webauth

import (
	"context"
	"errors"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/websession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/tokenrefresh"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Authentication errors
var (
	ErrSessionExpired     = errors.New("session expired")
	ErrTokenRevoked       = tokenrefresh.ErrTokenRevoked
	ErrTokenRefreshFailed = tokenrefresh.ErrTokenRefreshFailed
)

// Service handles web session authentication and token refresh
type Service struct {
	conns          *db.Connections
	tokenRefresher osm.TokenRefresher
}

// NewService creates a new web auth service
func NewService(conns *db.Connections, tokenRefresher osm.TokenRefresher) *Service {
	return &Service{
		conns:          conns,
		tokenRefresher: tokenRefresher,
	}
}

// RefreshWebSessionToken refreshes the OSM token for a web session.
// It updates the database with the new tokens and returns the new access token.
func (s *Service) RefreshWebSessionToken(ctx context.Context, session *db.WebSession) (string, error) {
	identifier := session.ID[:8]

	return s.tokenRefresher.RefreshToken(
		ctx,
		session.OSMRefreshToken,
		identifier,
		// onSuccess: update tokens in database
		func(accessToken, refreshToken string, expiry time.Time) error {
			return websession.UpdateTokens(s.conns, session.ID, accessToken, refreshToken, expiry)
		},
		// onRevoked: delete the session
		func() error {
			return websession.Delete(s.conns, session.ID)
		},
	)
}

// CreateRefreshFunc creates a bound refresh function for a web session.
// This function can be stored in context for automatic token refresh on 401.
func (s *Service) CreateRefreshFunc(session *db.WebSession) types.TokenRefreshFunc {
	return func(ctx context.Context) (string, error) {
		return s.RefreshWebSessionToken(ctx, session)
	}
}
