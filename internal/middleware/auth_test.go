package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/m0rjc/OsmDeviceAdapter/internal/deviceauth"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// mockAuthService implements a mock authentication service for testing
type mockAuthService struct {
	authenticateFunc func(ctx context.Context, authHeader string) (types.User, error)
}

func (m *mockAuthService) Authenticate(ctx context.Context, authHeader string) (types.User, error) {
	if m.authenticateFunc != nil {
		return m.authenticateFunc(ctx, authHeader)
	}
	return nil, errors.New("not implemented")
}

func TestDeviceAuthMiddleware_Success(t *testing.T) {
	// Mock successful authentication
	userID := 456
	expectedUser := &mockUser{
		userID:      &userID,
		accessToken: "osm-token-123",
	}

	mockService := &mockAuthService{
		authenticateFunc: func(ctx context.Context, authHeader string) (types.User, error) {
			if authHeader != "Bearer device-token-abc" {
				t.Errorf("Expected auth header 'Bearer device-token-abc', got '%s'", authHeader)
			}
			return expectedUser, nil
		},
	}

	// Create test handler that verifies user in context
	var capturedUser types.User
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			t.Error("Expected user in context")
		}
		capturedUser = user
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	middleware := DeviceAuthMiddleware(mockService)
	handler := middleware(testHandler)

	// Create request with valid auth header
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer device-token-abc")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify user was added to context
	if capturedUser == nil {
		t.Fatal("User was not added to context")
	}

	if capturedUser.AccessToken() != "osm-token-123" {
		t.Errorf("Expected access token 'osm-token-123', got '%s'", capturedUser.AccessToken())
	}
}

func TestDeviceAuthMiddleware_InvalidToken(t *testing.T) {
	mockService := &mockAuthService{
		authenticateFunc: func(ctx context.Context, authHeader string) (types.User, error) {
			return nil, deviceauth.ErrInvalidToken
		},
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for invalid token")
	})

	middleware := DeviceAuthMiddleware(mockService)
	handler := middleware(testHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify 401 response
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	// Verify WWW-Authenticate header is set
	if w.Header().Get("WWW-Authenticate") != `Bearer realm="API"` {
		t.Errorf("Expected WWW-Authenticate header, got '%s'", w.Header().Get("WWW-Authenticate"))
	}

	// Verify error message
	body := w.Body.String()
	if body != "Unauthorized\n" {
		t.Errorf("Expected 'Unauthorized' error, got '%s'", body)
	}
}

func TestDeviceAuthMiddleware_TokenRevoked(t *testing.T) {
	mockService := &mockAuthService{
		authenticateFunc: func(ctx context.Context, authHeader string) (types.User, error) {
			return nil, deviceauth.ErrTokenRevoked
		},
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for revoked token")
	})

	middleware := DeviceAuthMiddleware(mockService)
	handler := middleware(testHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer revoked-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify 401 response
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	// Verify error message mentions revocation
	body := w.Body.String()
	if body != "Access revoked - please re-authorize this device\n" {
		t.Errorf("Expected revocation error message, got '%s'", body)
	}
}

func TestDeviceAuthMiddleware_TokenRefreshFailed(t *testing.T) {
	mockService := &mockAuthService{
		authenticateFunc: func(ctx context.Context, authHeader string) (types.User, error) {
			return nil, deviceauth.ErrTokenRefreshFailed
		},
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when token refresh fails")
	})

	middleware := DeviceAuthMiddleware(mockService)
	handler := middleware(testHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify 503 response (temporary error)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	// Verify Retry-After header
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("Expected Retry-After header '60', got '%s'", w.Header().Get("Retry-After"))
	}

	// Verify error message
	body := w.Body.String()
	if body != "Server Error\n" {
		t.Errorf("Expected 'Server Error' message, got '%s'", body)
	}
}

func TestDeviceAuthMiddleware_MissingAuthHeader(t *testing.T) {
	mockService := &mockAuthService{
		authenticateFunc: func(ctx context.Context, authHeader string) (types.User, error) {
			if authHeader == "" {
				return nil, deviceauth.ErrInvalidToken
			}
			t.Errorf("Expected empty auth header, got '%s'", authHeader)
			return nil, deviceauth.ErrInvalidToken
		},
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without auth header")
	})

	middleware := DeviceAuthMiddleware(mockService)
	handler := middleware(testHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	// No Authorization header set

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify 401 response
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestDeviceAuthMiddleware_UnknownError(t *testing.T) {
	mockService := &mockAuthService{
		authenticateFunc: func(ctx context.Context, authHeader string) (types.User, error) {
			return nil, errors.New("database connection failed")
		},
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for unknown error")
	})

	middleware := DeviceAuthMiddleware(mockService)
	handler := middleware(testHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer some-token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify 503 response (fallback for unknown errors - treat as server error)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	// Verify Retry-After header
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("Expected Retry-After header '60', got '%s'", w.Header().Get("Retry-After"))
	}

	// Verify generic error message
	body := w.Body.String()
	if body != "Server Error\n" {
		t.Errorf("Expected 'Server Error' message, got '%s'", body)
	}
}
