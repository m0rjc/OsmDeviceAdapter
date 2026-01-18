package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSessionTestDB(t *testing.T) *db.Connections {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	return db.NewConnections(database, nil)
}

func createTestSession(t *testing.T, conns *db.Connections, id string) *db.WebSession {
	session := &db.WebSession{
		ID:              id,
		OSMUserID:       12345,
		OSMAccessToken:  "test-access-token",
		OSMRefreshToken: "test-refresh-token",
		OSMTokenExpiry:  time.Now().Add(time.Hour),
		CSRFToken:       "test-csrf-token",
		CreatedAt:       time.Now(),
		LastActivity:    time.Now(),
		ExpiresAt:       time.Now().Add(7 * 24 * time.Hour),
	}

	if err := db.CreateWebSession(conns, session); err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}

	return session
}

func TestSessionMiddleware_ValidSession(t *testing.T) {
	conns := setupSessionTestDB(t)
	session := createTestSession(t, conns, "valid-session-id")

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true

		// Verify session is in context
		ctxSession, ok := WebSessionFromContext(r.Context())
		if !ok {
			t.Error("Expected session in context")
		}
		if ctxSession.ID != session.ID {
			t.Errorf("Expected session ID %s, got %s", session.ID, ctxSession.ID)
		}

		// Verify user is in context
		user, userOk := UserFromContext(r.Context())
		if !userOk || user == nil {
			t.Error("Expected user in context")
		}
		if user.AccessToken() != session.OSMAccessToken {
			t.Errorf("Expected access token %s, got %s", session.OSMAccessToken, user.AccessToken())
		}

		w.WriteHeader(http.StatusOK)
	})

	handler := SessionMiddleware(conns, "test_session")(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
	req.AddCookie(&http.Cookie{Name: "test_session", Value: "valid-session-id"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Error("Inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestSessionMiddleware_MissingCookie(t *testing.T) {
	conns := setupSessionTestDB(t)

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := SessionMiddleware(conns, "test_session")(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if called {
		t.Error("Inner handler should not have been called")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestSessionMiddleware_InvalidSessionID(t *testing.T) {
	conns := setupSessionTestDB(t)

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := SessionMiddleware(conns, "test_session")(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
	req.AddCookie(&http.Cookie{Name: "test_session", Value: "nonexistent-session"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if called {
		t.Error("Inner handler should not have been called")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	// Check that cookie was cleared
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "test_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.MaxAge != -1 {
		t.Error("Expected session cookie to be cleared")
	}
}

func TestSessionMiddleware_ExpiredSession(t *testing.T) {
	conns := setupSessionTestDB(t)

	// Create an expired session directly (bypass normal creation)
	expiredSession := &db.WebSession{
		ID:              "expired-session-id",
		OSMUserID:       12345,
		OSMAccessToken:  "test-token",
		OSMRefreshToken: "test-refresh",
		OSMTokenExpiry:  time.Now().Add(-time.Hour),
		CSRFToken:       "test-csrf",
		CreatedAt:       time.Now().Add(-24 * time.Hour),
		LastActivity:    time.Now().Add(-24 * time.Hour),
		ExpiresAt:       time.Now().Add(-time.Hour), // Expired
	}
	if err := conns.DB.Create(expiredSession).Error; err != nil {
		t.Fatalf("Failed to create expired session: %v", err)
	}

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := SessionMiddleware(conns, "test_session")(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
	req.AddCookie(&http.Cookie{Name: "test_session", Value: "expired-session-id"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if called {
		t.Error("Inner handler should not have been called for expired session")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestCSRFMiddleware_ValidToken(t *testing.T) {
	session := &db.WebSession{
		ID:        "test-session",
		CSRFToken: "valid-csrf-token",
	}

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := CSRFMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/scores", nil)
	req.Header.Set("X-CSRF-Token", "valid-csrf-token")
	ctx := contextWithWebSession(req.Context(), session)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Error("Inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestCSRFMiddleware_MissingToken(t *testing.T) {
	session := &db.WebSession{
		ID:        "test-session",
		CSRFToken: "valid-csrf-token",
	}

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := CSRFMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/scores", nil)
	// No X-CSRF-Token header
	ctx := contextWithWebSession(req.Context(), session)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if called {
		t.Error("Inner handler should not have been called")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

func TestCSRFMiddleware_InvalidToken(t *testing.T) {
	session := &db.WebSession{
		ID:        "test-session",
		CSRFToken: "valid-csrf-token",
	}

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := CSRFMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/scores", nil)
	req.Header.Set("X-CSRF-Token", "invalid-token")
	ctx := contextWithWebSession(req.Context(), session)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if called {
		t.Error("Inner handler should not have been called")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

func TestCSRFMiddleware_GetRequestBypass(t *testing.T) {
	session := &db.WebSession{
		ID:        "test-session",
		CSRFToken: "valid-csrf-token",
	}

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := CSRFMiddleware(innerHandler)

	// GET requests should not require CSRF token
	req := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	// No X-CSRF-Token header
	ctx := contextWithWebSession(req.Context(), session)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Error("Inner handler was not called for GET request")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestCSRFMiddleware_AllMethodsValidated(t *testing.T) {
	methodsToValidate := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, method := range methodsToValidate {
		t.Run(method, func(t *testing.T) {
			session := &db.WebSession{
				ID:        "test-session",
				CSRFToken: "valid-csrf-token",
			}

			called := false
			innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			})

			handler := CSRFMiddleware(innerHandler)

			req := httptest.NewRequest(method, "/api/admin/test", nil)
			// No CSRF token - should fail
			ctx := contextWithWebSession(req.Context(), session)
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if called {
				t.Errorf("%s request without CSRF token should have been blocked", method)
			}
			if w.Code != http.StatusForbidden {
				t.Errorf("%s expected status 403, got %d", method, w.Code)
			}
		})
	}
}

// MockWebSessionAuthenticator for testing token refresh
type MockWebSessionAuthenticator struct {
	RefreshCalled bool
	NewToken      string
	Err           error
}

func (m *MockWebSessionAuthenticator) RefreshWebSessionToken(ctx context.Context, session *db.WebSession) (string, error) {
	m.RefreshCalled = true
	if m.Err != nil {
		return "", m.Err
	}
	return m.NewToken, nil
}

func TestTokenRefreshMiddleware_TokenFresh(t *testing.T) {
	conns := setupSessionTestDB(t)
	authenticator := &MockWebSessionAuthenticator{NewToken: "new-token"}

	session := &db.WebSession{
		ID:             "test-session",
		OSMTokenExpiry: time.Now().Add(time.Hour), // Not near expiry
	}

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := TokenRefreshMiddleware(conns, authenticator)(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
	ctx := contextWithWebSession(req.Context(), session)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Error("Inner handler was not called")
	}
	if authenticator.RefreshCalled {
		t.Error("Refresh should not have been called for fresh token")
	}
}

func TestTokenRefreshMiddleware_TokenNearExpiry(t *testing.T) {
	conns := setupSessionTestDB(t)
	authenticator := &MockWebSessionAuthenticator{NewToken: "new-refreshed-token"}

	session := &db.WebSession{
		ID:             "test-session",
		OSMAccessToken: "old-token",
		OSMTokenExpiry: time.Now().Add(3 * time.Minute), // Within 5-minute threshold
	}

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that the session was updated with new token
		ctxSession, _ := WebSessionFromContext(r.Context())
		if ctxSession.OSMAccessToken != "new-refreshed-token" {
			t.Errorf("Expected updated access token, got %s", ctxSession.OSMAccessToken)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := TokenRefreshMiddleware(conns, authenticator)(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
	ctx := contextWithWebSession(req.Context(), session)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !authenticator.RefreshCalled {
		t.Error("Refresh should have been called for near-expiry token")
	}
}

func TestTokenRefreshMiddleware_NoSession(t *testing.T) {
	conns := setupSessionTestDB(t)
	authenticator := &MockWebSessionAuthenticator{NewToken: "new-token"}

	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := TokenRefreshMiddleware(conns, authenticator)(innerHandler)

	// Request without session in context
	req := httptest.NewRequest(http.MethodGet, "/api/admin/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Error("Inner handler was not called")
	}
	if authenticator.RefreshCalled {
		t.Error("Refresh should not have been called without session")
	}
}

func TestWebSessionFromContext(t *testing.T) {
	session := &db.WebSession{
		ID:        "test-session",
		OSMUserID: 12345,
	}

	// Test with session in context
	ctx := contextWithWebSession(context.Background(), session)
	retrieved, ok := WebSessionFromContext(ctx)
	if !ok {
		t.Error("Expected session to be found in context")
	}
	if retrieved.ID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, retrieved.ID)
	}

	// Test without session
	retrieved, ok = WebSessionFromContext(context.Background())
	if ok {
		t.Error("Expected no session in empty context")
	}
	if retrieved != nil {
		t.Error("Expected nil session for empty context")
	}
}
