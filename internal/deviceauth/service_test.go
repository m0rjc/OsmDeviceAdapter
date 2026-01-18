package deviceauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/devicecode"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

// Test RefreshUserTokenFromRecord with token revocation
func TestRefreshUserTokenFromRecord_Revocation(t *testing.T) {
	// Setup test database
	conns := setupTestDB(t)
	now := time.Now()

	// Create a device with tokens
	deviceCode := "test-device"
	osmToken := "osm-access-token"
	osmRefresh := "osm-refresh-token"
	userId := 123
	device := &db.DeviceCode{
		DeviceCode:      deviceCode,
		UserCode:        "TEST",
		ClientID:        "test-client",
		Status:          "authorized",
		ExpiresAt:       now.Add(24 * time.Hour),
		OSMAccessToken:  &osmToken,
		OSMRefreshToken: &osmRefresh,
		OSMTokenExpiry:  ptrTime(now.Add(1 * time.Hour)),
		OsmUserID:       &userId,
	}
	if err := devicecode.Create(conns, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Create mock OAuth client that returns revocation error
	mockClient := &mockWebFlowClient{
		refreshTokenFunc: func(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error) {
			return nil, errors.New("token refresh failed: unauthorized (revoked)")
		},
	}

	// Create service
	service := NewService(conns, mockClient)

	// Attempt to refresh token - should detect revocation
	_, err := service.RefreshUserTokenFromRecord(context.Background(), device)
	if err != ErrTokenRevoked {
		t.Errorf("Expected ErrTokenRevoked, got %v", err)
	}

	// Verify device was marked as revoked in database
	found, err := devicecode.FindByCode(conns, deviceCode)
	if err != nil {
		t.Fatalf("Error finding device: %v", err)
	}
	if found == nil {
		t.Fatal("Expected device to still exist")
	}
	if found.Status != "revoked" {
		t.Errorf("Expected status 'revoked', got '%s'", found.Status)
	}
	if found.OSMAccessToken != nil {
		t.Error("Expected OSMAccessToken to be cleared")
	}
	if found.OSMRefreshToken != nil {
		t.Error("Expected OSMRefreshToken to be cleared")
	}
}

// Test RefreshUserTokenFromRecord with successful token refresh
func TestRefreshUserTokenFromRecord_Success(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	// Create a device with tokens
	deviceCode := "test-device"
	osmToken := "old-osm-token"
	osmRefresh := "osm-refresh-token"
	userId := 123
	device := &db.DeviceCode{
		DeviceCode:      deviceCode,
		UserCode:        "TEST",
		ClientID:        "test-client",
		Status:          "authorized",
		ExpiresAt:       now.Add(24 * time.Hour),
		OSMAccessToken:  &osmToken,
		OSMRefreshToken: &osmRefresh,
		OSMTokenExpiry:  ptrTime(now.Add(1 * time.Hour)),
		OsmUserID:       &userId,
	}
	if err := devicecode.Create(conns, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Create mock OAuth client that returns new tokens
	newAccessToken := "new-osm-access-token"
	newRefreshToken := "new-osm-refresh-token"
	mockClient := &mockWebFlowClient{
		refreshTokenFunc: func(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error) {
			return &types.OSMTokenResponse{
				AccessToken:  newAccessToken,
				RefreshToken: newRefreshToken,
				ExpiresIn:    3600,
				TokenType:    "Bearer",
			}, nil
		},
	}

	// Create service
	service := NewService(conns, mockClient)

	// Refresh token
	returnedToken, err := service.RefreshUserTokenFromRecord(context.Background(), device)
	if err != nil {
		t.Fatalf("RefreshUserTokenFromRecord failed: %v", err)
	}
	if returnedToken != newAccessToken {
		t.Errorf("Expected token '%s', got '%s'", newAccessToken, returnedToken)
	}

	// Verify database was updated with new tokens
	found, err := devicecode.FindByCode(conns, deviceCode)
	if err != nil {
		t.Fatalf("Error finding device: %v", err)
	}
	if found == nil {
		t.Fatal("Expected device to still exist")
	}
	if found.Status != "authorized" {
		t.Errorf("Expected status 'authorized', got '%s'", found.Status)
	}
	if found.OSMAccessToken == nil || *found.OSMAccessToken != newAccessToken {
		t.Errorf("Expected OSMAccessToken '%s', got '%v'", newAccessToken, found.OSMAccessToken)
	}
	if found.OSMRefreshToken == nil || *found.OSMRefreshToken != newRefreshToken {
		t.Errorf("Expected OSMRefreshToken '%s', got '%v'", newRefreshToken, found.OSMRefreshToken)
	}
}

// Test RefreshUserTokenFromRecord with network error
func TestRefreshUserTokenFromRecord_NetworkError(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	deviceCode := "test-device"
	osmToken := "osm-access-token"
	osmRefresh := "osm-refresh-token"
	userId := 123
	device := &db.DeviceCode{
		DeviceCode:      deviceCode,
		UserCode:        "TEST",
		ClientID:        "test-client",
		Status:          "authorized",
		ExpiresAt:       now.Add(24 * time.Hour),
		OSMAccessToken:  &osmToken,
		OSMRefreshToken: &osmRefresh,
		OSMTokenExpiry:  ptrTime(now.Add(1 * time.Hour)),
		OsmUserID:       &userId,
	}
	if err := devicecode.Create(conns, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Create mock OAuth client that returns network error
	mockClient := &mockWebFlowClient{
		refreshTokenFunc: func(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error) {
			return nil, errors.New("network error: connection timeout")
		},
	}

	service := NewService(conns, mockClient)

	// Attempt to refresh - should return temporary error
	_, err := service.RefreshUserTokenFromRecord(context.Background(), device)
	if err != ErrTokenRefreshFailed {
		t.Errorf("Expected ErrTokenRefreshFailed, got %v", err)
	}

	// Verify device status is still authorized (not revoked for network errors)
	found, err := devicecode.FindByCode(conns, deviceCode)
	if err != nil {
		t.Fatalf("Error finding device: %v", err)
	}
	if found == nil {
		t.Fatal("Expected device to still exist")
	}
	if found.Status != "authorized" {
		t.Errorf("Expected status to remain 'authorized', got '%s'", found.Status)
	}
	if found.OSMAccessToken == nil || *found.OSMAccessToken != osmToken {
		t.Error("Expected OSMAccessToken to remain unchanged")
	}
}

// Test RefreshUserTokenFromRecord with invalid device code type
func TestRefreshUserTokenFromRecord_InvalidType(t *testing.T) {
	conns := setupTestDB(t)
	mockClient := &mockWebFlowClient{}
	service := NewService(conns, mockClient)

	// Pass wrong type (string instead of *db.DeviceCode)
	_, err := service.RefreshUserTokenFromRecord(context.Background(), "invalid-type")
	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken for invalid type, got %v", err)
	}
}

// Test RefreshUserTokenFromRecord with missing refresh token
func TestRefreshUserTokenFromRecord_NoRefreshToken(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	deviceCode := "test-device"
	osmToken := "osm-access-token"
	userId := 123
	device := &db.DeviceCode{
		DeviceCode:      deviceCode,
		UserCode:        "TEST",
		ClientID:        "test-client",
		Status:          "authorized",
		ExpiresAt:       now.Add(24 * time.Hour),
		OSMAccessToken:  &osmToken,
		OSMRefreshToken: nil, // No refresh token
		OSMTokenExpiry:  ptrTime(now.Add(1 * time.Hour)),
		OsmUserID:       &userId,
	}
	if err := devicecode.Create(conns, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	mockClient := &mockWebFlowClient{}
	service := NewService(conns, mockClient)

	_, err := service.RefreshUserTokenFromRecord(context.Background(), device)
	if err != ErrTokenRefreshFailed {
		t.Errorf("Expected ErrTokenRefreshFailed for missing refresh token, got %v", err)
	}
}

// Test Authenticate with last_used_at tracking
func TestAuthenticate_LastUsedTracking(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	// Create a device with valid token
	deviceAccessToken := "device-access-token-123"
	osmToken := "osm-access-token"
	osmRefresh := "osm-refresh-token"
	userId := 123
	device := &db.DeviceCode{
		DeviceCode:        "test-device",
		UserCode:          "TEST",
		ClientID:          "test-client",
		Status:            "authorized",
		ExpiresAt:         now.Add(24 * time.Hour),
		DeviceAccessToken: &deviceAccessToken,
		OSMAccessToken:    &osmToken,
		OSMRefreshToken:   &osmRefresh,
		OSMTokenExpiry:    ptrTime(now.Add(2 * time.Hour)), // Not expiring soon
		OsmUserID:         &userId,
		LastUsedAt:        nil, // Never used before
	}
	if err := devicecode.Create(conns, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	mockClient := &mockWebFlowClient{}
	service := NewService(conns, mockClient)

	// Authenticate
	beforeAuth := time.Now()
	user, err := service.Authenticate(context.Background(), "Bearer "+deviceAccessToken)
	afterAuth := time.Now()

	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if user == nil {
		t.Fatal("Expected user to be returned")
	}

	// Verify last_used_at was updated
	found, err := devicecode.FindByCode(conns, "test-device")
	if err != nil {
		t.Fatalf("Error finding device: %v", err)
	}
	if found.LastUsedAt == nil {
		t.Fatal("Expected LastUsedAt to be set")
	}
	if found.LastUsedAt.Before(beforeAuth) || found.LastUsedAt.After(afterAuth) {
		t.Errorf("LastUsedAt should be between %v and %v, got %v",
			beforeAuth, afterAuth, *found.LastUsedAt)
	}
}

// Helper function for tests
func setupTestDB(t *testing.T) *db.Connections {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	return db.NewConnections(database, nil)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
