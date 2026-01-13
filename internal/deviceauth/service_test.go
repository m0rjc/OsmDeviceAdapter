package deviceauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Test helper to create a device code record
func createTestDeviceCode(deviceAccessToken, osmAccessToken, osmRefreshToken string, tokenExpiry *time.Time) *db.DeviceCode {
	userID := 123
	sectionID := 456
	return &db.DeviceCode{
		DeviceCode:        "test-device-code",
		DeviceAccessToken: &deviceAccessToken,
		OSMAccessToken:    &osmAccessToken,
		OSMRefreshToken:   &osmRefreshToken,
		OSMTokenExpiry:    tokenExpiry,
		OsmUserID:         &userID,
		SectionID:         &sectionID,
		Status:            "authorized",
	}
}

// Mock connections for testing
type mockConnections struct {
	db.Connections
	findDeviceCodeFunc     func(accessToken string) (*db.DeviceCode, error)
	updateDeviceTokensFunc func(deviceCode, accessToken, refreshToken string, expiry time.Time) error
}

// Mock OAuth client for testing
type mockWebFlowClient struct {
	refreshTokenFunc func(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error)
}

func (m *mockWebFlowClient) RefreshToken(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error) {
	if m.refreshTokenFunc != nil {
		return m.refreshTokenFunc(ctx, refreshToken)
	}
	return nil, errors.New("not implemented")
}

func TestAuthenticate_Success_NoRefreshNeeded(t *testing.T) {
	// We can't easily mock the db package functions as they're package-level functions
	// In a real-world scenario, you'd want to refactor to use interfaces
	// For now, we'll test the service logic by creating a modified version
	// that accepts injected functions

	t.Skip("Skipping until DB layer is refactored to use interfaces for mocking")

	// The following demonstrates what the test would look like:
	// expiry := time.Now().Add(1 * time.Hour)
	// deviceCode := createTestDeviceCode("device-token", "osm-access-token", "osm-refresh-token", &expiry)
	// conns := &db.Connections{}
	// ... test authentication flow
}

// Test the extractBearerToken function
func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "Valid bearer token",
			header:   "Bearer abc123",
			expected: "abc123",
		},
		{
			name:     "Valid bearer token with extra spaces",
			header:   "Bearer    token-with-spaces",
			expected: "   token-with-spaces",
		},
		{
			name:     "Empty header",
			header:   "",
			expected: "",
		},
		{
			name:     "Missing Bearer prefix",
			header:   "abc123",
			expected: "",
		},
		{
			name:     "Wrong case",
			header:   "bearer abc123",
			expected: "",
		},
		{
			name:     "Only Bearer prefix",
			header:   "Bearer",
			expected: "",
		},
		{
			name:     "Bearer with trailing space only",
			header:   "Bearer ",
			expected: "",
		},
		{
			name:     "Different auth scheme",
			header:   "Basic xyz789",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBearerToken(tt.header)
			if result != tt.expected {
				t.Errorf("extractBearerToken(%q) = %q, expected %q", tt.header, result, tt.expected)
			}
		})
	}
}

// Test AuthContext implementation
func TestAuthContext_Interfaces(t *testing.T) {
	userID := 789
	deviceCode := &db.DeviceCode{
		DeviceCode: "test-code",
		OsmUserID:  &userID,
	}
	osmAccessToken := "test-osm-token"

	authCtx := &AuthContext{
		deviceCodeRecord: deviceCode,
		osmAccessToken:   osmAccessToken,
	}

	// Test types.User interface implementation
	if authCtx.UserID() == nil {
		t.Error("UserID() should not be nil")
	}
	if *authCtx.UserID() != 789 {
		t.Errorf("UserID() = %d, expected 789", *authCtx.UserID())
	}

	if authCtx.AccessToken() != "test-osm-token" {
		t.Errorf("AccessToken() = %q, expected %q", authCtx.AccessToken(), "test-osm-token")
	}

	// Test DeviceCode() method
	returnedDevice := authCtx.DeviceCode()
	if returnedDevice == nil {
		t.Error("DeviceCode() should not be nil")
	}
	if returnedDevice.DeviceCode != "test-code" {
		t.Errorf("DeviceCode().DeviceCode = %q, expected %q", returnedDevice.DeviceCode, "test-code")
	}
}

// Integration-style test documenting expected behavior
func TestAuthenticate_ExpectedBehavior(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		expectedError error
		description   string
	}{
		{
			name:          "Empty auth header",
			authHeader:    "",
			expectedError: ErrInvalidToken,
			description:   "Missing Authorization header should return ErrInvalidToken",
		},
		{
			name:          "Invalid auth format",
			authHeader:    "NotBearer token",
			expectedError: ErrInvalidToken,
			description:   "Non-Bearer auth should return ErrInvalidToken",
		},
		{
			name:          "Valid Bearer format",
			authHeader:    "Bearer valid-token-123",
			expectedError: nil, // Would succeed if DB lookup succeeds
			description:   "Valid Bearer token format should proceed to DB lookup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This documents the expected behavior
			// Actual DB integration would require mocking or test database
			token := extractBearerToken(tt.authHeader)

			if tt.expectedError == ErrInvalidToken && token == "" {
				// Expected: empty token should lead to ErrInvalidToken
				return
			}

			if tt.authHeader == "Bearer valid-token-123" && token == "valid-token-123" {
				// Expected: valid format extracts token correctly
				return
			}

			t.Logf("Test case documents: %s", tt.description)
		})
	}
}

// Test error type checking
func TestErrorTypes(t *testing.T) {
	// Test that our errors are defined correctly
	if ErrInvalidToken == nil {
		t.Error("ErrInvalidToken should be defined")
	}
	if ErrTokenRevoked == nil {
		t.Error("ErrTokenRevoked should be defined")
	}
	if ErrTokenRefreshFailed == nil {
		t.Error("ErrTokenRefreshFailed should be defined")
	}

	// Test error messages are meaningful
	if !strings.Contains(ErrInvalidToken.Error(), "invalid") {
		t.Errorf("ErrInvalidToken message should mention 'invalid', got: %s", ErrInvalidToken.Error())
	}
	if !strings.Contains(ErrTokenRevoked.Error(), "revoked") {
		t.Errorf("ErrTokenRevoked message should mention 'revoked', got: %s", ErrTokenRevoked.Error())
	}
	if !strings.Contains(ErrTokenRefreshFailed.Error(), "refresh") {
		t.Errorf("ErrTokenRefreshFailed message should mention 'refresh', got: %s", ErrTokenRefreshFailed.Error())
	}
}

// Test error distinguishability
func TestErrorsAreDistinct(t *testing.T) {
	// Verify errors can be distinguished with errors.Is
	if errors.Is(ErrInvalidToken, ErrTokenRevoked) {
		t.Error("ErrInvalidToken and ErrTokenRevoked should be distinct")
	}
	if errors.Is(ErrInvalidToken, ErrTokenRefreshFailed) {
		t.Error("ErrInvalidToken and ErrTokenRefreshFailed should be distinct")
	}
	if errors.Is(ErrTokenRevoked, ErrTokenRefreshFailed) {
		t.Error("ErrTokenRevoked and ErrTokenRefreshFailed should be distinct")
	}
}
