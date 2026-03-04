package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

type mockAuthOutcomeWriter struct {
	kind   string
	result string
}

func (m *mockAuthOutcomeWriter) Header() http.Header {
	return http.Header{}
}

func (m *mockAuthOutcomeWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (m *mockAuthOutcomeWriter) WriteHeader(statusCode int) {
}

func (m *mockAuthOutcomeWriter) SetAuthOutcome(kind, result string) {
	m.kind = kind
	m.result = result
}

func TestAuthOutcome_SetAuthOutcome(t *testing.T) {
	// Test device auth success
	userID := 123
	expectedUser := &mockUser{
		userID: &userID,
	}
	mockService := &mockAuthService{
		authenticateFunc: func(ctx context.Context, authHeader string) (types.User, error) {
			return expectedUser, nil
		},
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	middleware := DeviceAuthMiddleware(mockService)
	handler := middleware(testHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")

	w := &mockAuthOutcomeWriter{}
	handler.ServeHTTP(w, req)

	if w.kind != "device" || w.result != "ok" {
		t.Errorf("Expected device:ok, got %s:%s", w.kind, w.result)
	}
}
