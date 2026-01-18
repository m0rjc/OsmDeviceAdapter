package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/websession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Context keys for session data
const (
	webSessionContextKey contextKey = "web_session"
)

// WebSessionAuthenticator handles token refresh for web sessions
type WebSessionAuthenticator interface {
	RefreshWebSessionToken(ctx context.Context, session *db.WebSession) (string, error)
}

// WebSessionFromContext retrieves the web session from the context
func WebSessionFromContext(ctx context.Context) (*db.WebSession, bool) {
	session, ok := ctx.Value(webSessionContextKey).(*db.WebSession)
	return session, ok
}

// contextWithWebSession adds a web session to the context
func contextWithWebSession(ctx context.Context, session *db.WebSession) context.Context {
	return context.WithValue(ctx, webSessionContextKey, session)
}

// SessionMiddleware extracts and validates admin web sessions from cookies.
// It loads the session from the database, validates expiry, updates last_activity,
// and attaches the session to the request context.
// If the session is invalid or expired, it clears the cookie and returns 401.
func SessionMiddleware(conns *db.Connections, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract session ID from cookie
			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value == "" {
				slog.Debug("session.middleware.no_cookie",
					"component", "session_middleware",
					"event", "session.missing",
					"path", r.URL.Path,
				)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			sessionID := cookie.Value

			// Load session from database
			session, err := websession.FindByID(conns, sessionID)
			if err != nil {
				slog.Error("session.middleware.db_error",
					"component", "session_middleware",
					"event", "session.error",
					"error", err,
				)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			if session == nil {
				slog.Debug("session.middleware.invalid_session",
					"component", "session_middleware",
					"event", "session.invalid",
					"path", r.URL.Path,
				)
				// Clear the invalid cookie
				clearCookie(w, cookieName)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Update last_activity for sliding expiration (async, don't block request)
			go func() {
				if err := websession.UpdateActivity(conns, sessionID); err != nil {
					slog.Warn("session.middleware.activity_update_failed",
						"component", "session_middleware",
						"event", "session.activity_error",
						"session_id", sessionID[:8],
						"error", err,
					)
				}
			}()

			// Add session to context
			ctx := contextWithWebSession(r.Context(), session)

			// Also add user to context for consistency with device auth
			user := session.User()
			ctx = ContextWithUser(ctx, user)

			// Continue to next handler
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFMiddleware validates CSRF tokens on state-changing requests (POST, PUT, DELETE, PATCH).
// It expects the CSRF token in the X-CSRF-Token header and compares it against
// the session's csrf_token. Returns 403 Forbidden on mismatch.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate CSRF on state-changing methods
		if r.Method == http.MethodPost || r.Method == http.MethodPut ||
			r.Method == http.MethodDelete || r.Method == http.MethodPatch {

			// Get session from context
			session, ok := WebSessionFromContext(r.Context())
			if !ok {
				slog.Warn("csrf.middleware.no_session",
					"component", "csrf_middleware",
					"event", "csrf.error",
					"path", r.URL.Path,
				)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Get CSRF token from header
			csrfToken := r.Header.Get("X-CSRF-Token")
			if csrfToken == "" {
				slog.Warn("csrf.middleware.missing_token",
					"component", "csrf_middleware",
					"event", "csrf.missing",
					"path", r.URL.Path,
				)
				http.Error(w, "CSRF token required", http.StatusForbidden)
				return
			}

			// Validate CSRF token (constant-time comparison would be ideal, but for
			// tokens of this length and random nature, timing attacks are not practical)
			if csrfToken != session.CSRFToken {
				slog.Warn("csrf.middleware.invalid_token",
					"component", "csrf_middleware",
					"event", "csrf.invalid",
					"path", r.URL.Path,
				)
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// TokenRefreshMiddleware checks if the OSM token is near expiry and refreshes it.
// It should be applied after SessionMiddleware.
// Uses the same 5-minute threshold as the device flow.
func TokenRefreshMiddleware(conns *db.Connections, authenticator WebSessionAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, ok := WebSessionFromContext(r.Context())
			if !ok {
				// No session, skip token refresh
				next.ServeHTTP(w, r)
				return
			}

			// Check if token is near expiry (within 5 minutes)
			if time.Now().After(session.OSMTokenExpiry.Add(-5 * time.Minute)) {
				slog.Debug("session.token_refresh.needed",
					"component", "session_middleware",
					"event", "token.refresh_needed",
					"session_id", session.ID[:8],
				)

				// Refresh the token
				newAccessToken, err := authenticator.RefreshWebSessionToken(r.Context(), session)
				if err != nil {
					slog.Error("session.token_refresh.failed",
						"component", "session_middleware",
						"event", "token.refresh_error",
						"session_id", session.ID[:8],
						"error", err,
					)
					// If refresh fails, we can still try to use the current token
					// It might still work if not actually expired yet
				} else {
					// Update the session in context with new token
					session.OSMAccessToken = newAccessToken
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// clearCookie clears a cookie by setting it to expire immediately
func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// WebSessionUser is an adapter that implements types.User for WebSession
type WebSessionUser struct {
	session *db.WebSession
}

// UserID returns the OSM user ID
func (u *WebSessionUser) UserID() *int {
	return &u.session.OSMUserID
}

// AccessToken returns the OSM access token
func (u *WebSessionUser) AccessToken() string {
	return u.session.OSMAccessToken
}

// NewWebSessionUser creates a new WebSessionUser from a WebSession
func NewWebSessionUser(session *db.WebSession) types.User {
	return &WebSessionUser{session: session}
}
