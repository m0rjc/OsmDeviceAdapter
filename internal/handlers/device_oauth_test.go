package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
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
		ExposedDomain:      "https://example.com",
		DeviceCodeExpiry:   600,
		DevicePollInterval: 5,
		AllowedClientIDs:   allowedClientIDs,
	}

	// Create connections wrapper (Redis is nil for tests that don't need it)
	conns := db.NewConnections(database, nil)

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
