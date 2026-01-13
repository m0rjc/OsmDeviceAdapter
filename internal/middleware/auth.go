package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/m0rjc/OsmDeviceAdapter/internal/deviceauth"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Authenticator is an interface for authentication services
type Authenticator interface {
	Authenticate(ctx context.Context, authHeader string) (types.User, error)
}

// DeviceAuthMiddleware authenticates device API requests using bearer tokens
// and adds the authenticated User to the request context.
// Returns appropriate HTTP status codes based on the failure type:
// - 401: Invalid token or access revoked by user
// - 503: Temporary failure (network, OSM server issue, database error)
func DeviceAuthMiddleware(deviceAuthService Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Authenticate the request (handles token refresh internally)
			user, err := deviceAuthService.Authenticate(r.Context(), r.Header.Get("Authorization"))
			if err != nil {
				// Handle authentication errors
				if errors.Is(err, deviceauth.ErrInvalidToken) {
					w.Header().Set("WWW-Authenticate", `Bearer realm="API"`)
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}

				if errors.Is(err, deviceauth.ErrTokenRevoked) {
					w.Header().Set("WWW-Authenticate", `Bearer realm="API"`)
					http.Error(w, "Access revoked - please re-authorize this device", http.StatusUnauthorized)
					return
				}

				// Log an unexpected error (should not happen).
				// Use fallback path for any server (or upstream) error
				if !errors.Is(err, deviceauth.ErrTokenRefreshFailed) {
					slog.Error("unexpected error while refreshing token", "err", err)
				}
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Server Error", http.StatusServiceUnavailable)
				return
			}

			// Add user to context
			ctx := ContextWithUser(r.Context(), user)

			// Continue to next handler with authenticated context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
