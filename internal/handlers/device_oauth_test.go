package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDeps(t *testing.T, allowedClientIDs []string) *Dependencies {
	// Use in-memory SQLite for testing
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Auto-migrate tables
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{
		ExposedDomain:            "https://example.com",
		DeviceCodeExpiry:         300,
		DevicePollInterval:       5,
		AllowedClientIDs:         allowedClientIDs,
		DeviceAuthorizeRateLimit: 6,
		DeviceEntryRateLimit:     5,
	}

	// Create connections wrapper with mock rate limiter (no Redis required for tests)
	conns := db.NewConnections(database, nil)
	conns.RateLimiter = db.NewMockRateLimiter() // Use mock that allows all requests

	return &Dependencies{
		Config: cfg,
		Conns:  conns,
	}
}

func TestDeviceAuthorizeHandler_ValidClientID(t *testing.T) {
	deps := setupTestDeps(t, []string{"test-client-1", "test-client-2"})
	handler := DeviceAuthorizeHandler(deps)

	reqBody := DeviceAuthorizationRequest{
		ClientID: "test-client-1",
		Scope:    "read",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add remote metadata to context (simulating middleware)
	ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{
		IP:       "192.168.1.1",
		Protocol: "https",
		Country:  "US",
	})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp DeviceAuthorizationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.DeviceCode == "" {
		t.Error("Expected device_code to be present")
	}
	if resp.UserCode == "" {
		t.Error("Expected user_code to be present")
	}
	if resp.VerificationURI == "" {
		t.Error("Expected verification_uri to be present")
	}
}

func TestDeviceAuthorizeHandler_InvalidClientID(t *testing.T) {
	deps := setupTestDeps(t, []string{"test-client-1", "test-client-2"})
	handler := DeviceAuthorizeHandler(deps)

	reqBody := DeviceAuthorizationRequest{
		ClientID: "unauthorized-client",
		Scope:    "read",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add remote metadata to context
	ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{IP: "192.168.1.1"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d. Body: %s", w.Code, w.Body.String())
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("invalid client_id")) {
		t.Errorf("Expected error message 'invalid client_id', got: %s", w.Body.String())
	}
}

func TestDeviceAuthorizeHandler_EmptyClientID(t *testing.T) {
	deps := setupTestDeps(t, []string{"test-client-1"})
	handler := DeviceAuthorizeHandler(deps)

	reqBody := DeviceAuthorizationRequest{
		ClientID: "",
		Scope:    "read",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add remote metadata to context
	ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{IP: "192.168.1.1"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d. Body: %s", w.Code, w.Body.String())
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("client_id is required")) {
		t.Errorf("Expected error message 'client_id is required', got: %s", w.Body.String())
	}
}

func TestDeviceAuthorizeHandler_MissingClientID(t *testing.T) {
	deps := setupTestDeps(t, []string{"test-client-1"})
	handler := DeviceAuthorizeHandler(deps)

	// Send request with no client_id field
	body := []byte(`{"scope": "read"}`)

	req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add remote metadata to context
	ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{IP: "192.168.1.1"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestDeviceAuthorizeHandler_MultipleAllowedClients(t *testing.T) {
	allowedClients := []string{"client-1", "client-2", "client-3"}
	deps := setupTestDeps(t, allowedClients)
	handler := DeviceAuthorizeHandler(deps)

	// Test each allowed client
	for _, clientID := range allowedClients {
		reqBody := DeviceAuthorizationRequest{
			ClientID: clientID,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		// Add remote metadata to context
		ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{IP: "192.168.1.1"})
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 for client %s, got %d. Body: %s", clientID, w.Code, w.Body.String())
		}
	}
}

func TestIsClientIDAllowed(t *testing.T) {
	allowedClients := []string{"client-1", "client-2", "client-3"}

	tests := []struct {
		name     string
		clientID string
		expected bool
	}{
		{"Valid client 1", "client-1", true},
		{"Valid client 2", "client-2", true},
		{"Valid client 3", "client-3", true},
		{"Invalid client", "unauthorized", false},
		{"Empty client", "", false},
		{"Similar but not exact", "client-", false},
		{"Case sensitive", "Client-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isClientIDAllowed(tt.clientID, allowedClients)
			if result != tt.expected {
				t.Errorf("isClientIDAllowed(%q) = %v, expected %v", tt.clientID, result, tt.expected)
			}
		})
	}
}

func TestIsClientIDAllowed_EmptyWhitelist(t *testing.T) {
	result := isClientIDAllowed("any-client", []string{})
	if result != false {
		t.Error("Expected isClientIDAllowed to return false with empty whitelist")
	}
}

func TestGenerateDeviceAccessToken(t *testing.T) {
	token1, err := generateDeviceAccessToken()
	if err != nil {
		t.Fatalf("Failed to generate device access token: %v", err)
	}

	if token1 == "" {
		t.Error("Expected non-empty device access token")
	}

	// Token should be long and secure (64 bytes = ~86 characters in base64)
	if len(token1) < 80 {
		t.Errorf("Device access token too short: %d characters", len(token1))
	}

	// Generate another token and verify they're different (randomness check)
	token2, err := generateDeviceAccessToken()
	if err != nil {
		t.Fatalf("Failed to generate second device access token: %v", err)
	}

	if token1 == token2 {
		t.Error("Expected different tokens for each generation (randomness failure)")
	}
}

func TestDeviceAccessTokenFlow(t *testing.T) {
	deps := setupTestDeps(t, []string{"test-client"})

	// 1. Create a device code with full authorization
	deviceCode := "test-device-code-123"
	userCode := "TEST-CODE"
	osmToken := "osm-token-xyz"
	osmRefreshToken := "osm-refresh-xyz"
	deviceAccessToken, err := generateDeviceAccessToken()
	if err != nil {
		t.Fatalf("Failed to generate device access token: %v", err)
	}

	// Create device code in database
	record := &db.DeviceCode{
		DeviceCode:        deviceCode,
		UserCode:          userCode,
		ClientID:          "test-client",
		Status:            "authorized",
		DeviceAccessToken: &deviceAccessToken,
		OSMAccessToken:    &osmToken,
		OSMRefreshToken:   &osmRefreshToken,
	}
	if err := db.CreateDeviceCode(deps.Conns, record); err != nil {
		t.Fatalf("Failed to create device code: %v", err)
	}

	// 2. Test finding by device access token
	found, err := db.FindDeviceCodeByDeviceAccessToken(deps.Conns, deviceAccessToken)
	if err != nil {
		t.Fatalf("Failed to find device code by device access token: %v", err)
	}
	if found == nil {
		t.Fatal("Expected to find device code by device access token")
	}
	if found.DeviceCode != deviceCode {
		t.Errorf("Found wrong device code: got %s, expected %s", found.DeviceCode, deviceCode)
	}

	// 3. Verify OSM token is present but separate
	if found.OSMAccessToken == nil || *found.OSMAccessToken != osmToken {
		t.Error("OSM access token should be stored server-side")
	}
	if found.DeviceAccessToken == nil || *found.DeviceAccessToken != deviceAccessToken {
		t.Error("Device access token mismatch")
	}

	// 4. Test that invalid token returns nil
	invalidFound, err := db.FindDeviceCodeByDeviceAccessToken(deps.Conns, "invalid-token")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if invalidFound != nil {
		t.Error("Expected nil for invalid device access token")
	}
}

func TestDeviceAccessTokenUniqueness(t *testing.T) {
	deps := setupTestDeps(t, []string{"test-client"})

	token := "unique-device-token-123"

	// Create first device code with token
	record1 := &db.DeviceCode{
		DeviceCode:        "device-1",
		UserCode:          "CODE-001",
		ClientID:          "test-client",
		Status:            "authorized",
		DeviceAccessToken: &token,
	}
	if err := db.CreateDeviceCode(deps.Conns, record1); err != nil {
		t.Fatalf("Failed to create first device code: %v", err)
	}

	// Try to create second device code with same token (should fail due to unique constraint)
	record2 := &db.DeviceCode{
		DeviceCode:        "device-2",
		UserCode:          "CODE-002",
		ClientID:          "test-client",
		Status:            "authorized",
		DeviceAccessToken: &token,
	}
	err := db.CreateDeviceCode(deps.Conns, record2)
	if err == nil {
		t.Error("Expected error when creating device code with duplicate device access token")
	}
}
