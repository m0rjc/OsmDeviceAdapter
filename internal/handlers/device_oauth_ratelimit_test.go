package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestDeviceAuthorizeHandler_RateLimitExceeded demonstrates how to test rate limiting behavior
func TestDeviceAuthorizeHandler_RateLimitExceeded(t *testing.T) {
	// Set up test database
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create connections wrapper
	conns := db.NewConnections(database, nil)

	// Insert allowed client ID into database
	allowedClient := &db.AllowedClientID{
		ClientID:     "test-client",
		Comment:      "Test client",
		ContactEmail: "test@example.com",
		Enabled:      true,
	}
	if err := db.CreateAllowedClientID(conns, allowedClient); err != nil {
		t.Fatalf("Failed to create allowed client ID: %v", err)
	}

	cfg := &config.Config{
		ExternalDomains: config.ExternalDomainsConfig{
			ExposedDomain: "https://example.com",
		},
		DeviceOAuth: config.DeviceOAuthConfig{
			DeviceCodeExpiry:   300,
			DevicePollInterval: 5,
		},
		RateLimit: config.RateLimitConfig{
			DeviceAuthorizeRateLimit: 6,
		},
		Paths: config.PathConfig{
			DevicePrefix: "/device",
		},
	}

	// Create mock rate limiter that denies requests
	mockRateLimiter := db.NewMockRateLimiter()
	mockRateLimiter.AlwaysAllow = false // Deny all requests

	conns.RateLimiter = mockRateLimiter

	deps := &Dependencies{
		Config: cfg,
		Conns:  conns,
	}

	handler := DeviceAuthorizeHandler(deps)

	// Create request
	reqBody := DeviceAuthorizationRequest{
		ClientID: "test-client",
		Scope:    "read",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add remote metadata to context
	ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{
		IP: "192.168.1.1",
	})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	// Make request
	handler(w, req)

	// Should return 429 Too Many Requests
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Should have Retry-After header
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header to be set")
	}
}

// TestDeviceAuthorizeHandler_RateLimitCustomBehavior demonstrates custom rate limit logic
func TestDeviceAuthorizeHandler_RateLimitCustomBehavior(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create connections wrapper
	conns := db.NewConnections(database, nil)

	// Insert allowed client ID into database
	allowedClient := &db.AllowedClientID{
		ClientID:     "test-client",
		Comment:      "Test client",
		ContactEmail: "test@example.com",
		Enabled:      true,
	}
	if err := db.CreateAllowedClientID(conns, allowedClient); err != nil {
		t.Fatalf("Failed to create allowed client ID: %v", err)
	}

	cfg := &config.Config{
		ExternalDomains: config.ExternalDomainsConfig{
			ExposedDomain: "https://example.com",
		},
		DeviceOAuth: config.DeviceOAuthConfig{
			DeviceCodeExpiry:   300,
			DevicePollInterval: 5,
		},
		RateLimit: config.RateLimitConfig{
			DeviceAuthorizeRateLimit: 3, // Allow 3 requests
		},
		Paths: config.PathConfig{
			DevicePrefix: "/device",
		},
	}

	// Create mock with custom logic: allow first 3 requests, deny the 4th
	mockRateLimiter := db.NewMockRateLimiter()
	requestCount := 0
	mockRateLimiter.CheckRateLimitFunc = func(ctx context.Context, name, key string, limit int64, window time.Duration) (*db.RateLimitResult, error) {
		requestCount++
		allowed := requestCount <= int(limit)
		remaining := limit - int64(requestCount)
		if remaining < 0 {
			remaining = 0
		}

		return &db.RateLimitResult{
			Allowed:    allowed,
			Remaining:  remaining,
			RetryAfter: func() time.Duration {
				if allowed {
					return 0
				}
				return window
			}(),
		}, nil
	}

	conns.RateLimiter = mockRateLimiter

	deps := &Dependencies{
		Config: cfg,
		Conns:  conns,
	}

	handler := DeviceAuthorizeHandler(deps)

	// Make 3 requests - all should succeed
	for i := 1; i <= 3; i++ {
		reqBody := DeviceAuthorizationRequest{ClientID: "test-client"}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{IP: "192.168.1.1"})
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i, w.Code)
		}
	}

	// 4th request should be rate limited
	reqBody := DeviceAuthorizationRequest{ClientID: "test-client"}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{IP: "192.168.1.1"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("4th request: Expected status 429, got %d", w.Code)
	}
}

// TestDeviceTokenHandler_SlowDown demonstrates testing the slow_down error
func TestDeviceTokenHandler_SlowDown(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create a device code that's pending authorization
	deviceCode := "test-device-code"
	record := &db.DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   "TEST-CODE",
		ClientID:   "test-client",
		Status:     "pending",
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}
	if err := db.CreateDeviceCode(&db.Connections{DB: database}, record); err != nil {
		t.Fatalf("Failed to create device code: %v", err)
	}

	cfg := &config.Config{
		DeviceOAuth: config.DeviceOAuthConfig{
			DevicePollInterval: 5,
			AllowedClientIDs:   "test-client",
		},
	}

	// Mock rate limiter that denies (too fast polling)
	mockRateLimiter := db.NewMockRateLimiter()
	mockRateLimiter.AlwaysAllow = false

	conns := db.NewConnections(database, nil)
	conns.RateLimiter = mockRateLimiter

	deps := &Dependencies{
		Config: cfg,
		Conns:  conns,
	}

	handler := DeviceTokenHandler(deps)

	// Create token request
	reqBody := DeviceTokenRequest{
		GrantType:  "urn:ietf:params:oauth:grant-type:device_code",
		DeviceCode: deviceCode,
		ClientID:   "test-client",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/device/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	// Should return 400 with slow_down error
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var errorResp DeviceTokenErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errorResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	if errorResp.Error != "slow_down" {
		t.Errorf("Expected error 'slow_down', got '%s'", errorResp.Error)
	}
}

// TestMockRateLimiter_CallTracking demonstrates how to verify rate limit was called
func TestMockRateLimiter_CallTracking(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create connections wrapper
	conns := db.NewConnections(database, nil)

	// Insert allowed client ID into database
	allowedClient := &db.AllowedClientID{
		ClientID:     "test-client",
		Comment:      "Test client",
		ContactEmail: "test@example.com",
		Enabled:      true,
	}
	if err := db.CreateAllowedClientID(conns, allowedClient); err != nil {
		t.Fatalf("Failed to create allowed client ID: %v", err)
	}

	cfg := &config.Config{
		ExternalDomains: config.ExternalDomainsConfig{
			ExposedDomain: "https://example.com",
		},
		DeviceOAuth: config.DeviceOAuthConfig{
			DeviceCodeExpiry: 300,
		},
		RateLimit: config.RateLimitConfig{
			DeviceAuthorizeRateLimit: 6,
		},
		Paths: config.PathConfig{
			DevicePrefix: "/device",
		},
	}

	mockRateLimiter := db.NewMockRateLimiter()

	conns.RateLimiter = mockRateLimiter

	deps := &Dependencies{
		Config: cfg,
		Conns:  conns,
	}

	handler := DeviceAuthorizeHandler(deps)

	// Make a request
	reqBody := DeviceAuthorizationRequest{ClientID: "test-client"}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/device/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := middleware.ContextWithRemote(req.Context(), middleware.RemoteMetadata{IP: "10.0.0.1"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	// Verify rate limiter was called
	callCount := mockRateLimiter.GetCallCount("device_authorize", "10.0.0.1:/device/authorize")
	if callCount != 1 {
		t.Errorf("Expected rate limiter to be called once, got %d calls", callCount)
	}
}
