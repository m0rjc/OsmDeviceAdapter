package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/websession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newMockOSMClient creates an OSM client for testing that uses the provided base URL
func newMockOSMClient(baseURL string) *osm.Client {
	return osm.NewClient(baseURL, nil, nil)
}

// setupAdminTestDeps creates test dependencies with miniredis for admin OAuth tests
func setupAdminTestDeps(t *testing.T) (*Dependencies, *miniredis.Miniredis) {
	// Use in-memory SQLite for testing
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Auto-migrate tables
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create miniredis server
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	// Create Redis client connected to miniredis
	redisClient, err := db.NewRedisClient("redis://"+mr.Addr(), "test:")
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}

	// Create connections wrapper
	conns := db.NewConnections(database, redisClient)

	cfg := &config.Config{
		ExternalDomains: config.ExternalDomainsConfig{
			ExposedDomain: "https://example.com",
			OSMDomain:     "https://osm.example.com",
		},
		OAuth: config.OAuthConfig{
			OSMClientID:     "test-client-id",
			OSMClientSecret: "test-client-secret",
		},
	}

	return &Dependencies{
		Config: cfg,
		Conns:  conns,
	}, mr
}

func TestAdminLoginHandler_RedirectsToOSM(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	handler := AdminLoginHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	// Should redirect
	if w.Code != http.StatusFound {
		t.Errorf("Expected status %d, got %d", http.StatusFound, w.Code)
	}

	// Check redirect location
	location := w.Header().Get("Location")
	if location == "" {
		t.Fatal("Expected Location header")
	}

	// Parse the redirect URL
	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Failed to parse redirect URL: %v", err)
	}

	// Verify the redirect URL components
	expectedHost := "osm.example.com"
	if redirectURL.Host != expectedHost {
		t.Errorf("Expected host %s, got %s", expectedHost, redirectURL.Host)
	}

	if redirectURL.Path != "/oauth/authorize" {
		t.Errorf("Expected path /oauth/authorize, got %s", redirectURL.Path)
	}

	// Verify query parameters
	params := redirectURL.Query()
	if params.Get("client_id") != "test-client-id" {
		t.Errorf("Expected client_id=test-client-id, got %s", params.Get("client_id"))
	}
	if params.Get("scope") != AdminOAuthScope {
		t.Errorf("Expected scope=%s, got %s", AdminOAuthScope, params.Get("scope"))
	}
	if params.Get("response_type") != "code" {
		t.Errorf("Expected response_type=code, got %s", params.Get("response_type"))
	}
	if params.Get("state") == "" {
		t.Error("Expected state parameter to be present")
	}
	expectedRedirectURI := "https://example.com/admin/callback"
	if params.Get("redirect_uri") != expectedRedirectURI {
		t.Errorf("Expected redirect_uri=%s, got %s", expectedRedirectURI, params.Get("redirect_uri"))
	}

	// Verify state was stored in Redis
	state := params.Get("state")
	stateKey := "admin_oauth_state:" + state
	val, err := mr.Get("test:" + stateKey)
	if err != nil {
		t.Errorf("State not stored in Redis: %v", err)
	}
	if val != "1" {
		t.Errorf("Expected state value '1', got '%s'", val)
	}
}

func TestAdminCallbackHandler_MissingCode(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	handler := AdminCallbackHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/admin/callback?state=test-state", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	if !strings.Contains(w.Body.String(), "Missing required parameters") {
		t.Errorf("Expected error about missing parameters, got: %s", w.Body.String())
	}
}

func TestAdminCallbackHandler_MissingState(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	handler := AdminCallbackHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/admin/callback?code=test-code", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestAdminCallbackHandler_InvalidState(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	handler := AdminCallbackHandler(deps)

	// Don't store the state in Redis - it should fail validation
	req := httptest.NewRequest(http.MethodGet, "/admin/callback?code=test-code&state=invalid-state", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	if !strings.Contains(w.Body.String(), "Invalid or expired state") {
		t.Errorf("Expected error about invalid state, got: %s", w.Body.String())
	}
}

func TestAdminCallbackHandler_OAuthError(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	handler := AdminCallbackHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/admin/callback?error=access_denied", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	if !strings.Contains(w.Body.String(), "Authorization denied") {
		t.Errorf("Expected authorization denied message, got: %s", w.Body.String())
	}
}

func TestAdminCallbackHandler_SuccessfulLogin(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	// Create a mock OSM server for token exchange and profile fetch
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			// Token exchange
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(types.OSMTokenResponse{
				AccessToken:  "test-access-token",
				RefreshToken: "test-refresh-token",
				ExpiresIn:    3600,
				TokenType:    "Bearer",
			})
		case "/oauth/resource":
			// Profile fetch (uses /oauth/resource endpoint)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(types.OSMProfileResponse{
				Status: true,
				Data: &types.OSMProfileData{
					UserID:   12345,
					FullName: "Test User",
					Email:    "test@example.com",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer osmServer.Close()

	// Update config to use mock server
	deps.Config.ExternalDomains.OSMDomain = osmServer.URL

	// Create a mock OSM client that uses the mock server
	deps.OSM = newMockOSMClient(osmServer.URL)

	handler := AdminCallbackHandler(deps)

	// Store a valid state in Redis
	state := "valid-test-state"
	mr.Set("test:admin_oauth_state:"+state, "1")

	req := httptest.NewRequest(http.MethodGet, "/admin/callback?code=test-code&state="+state, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	// Should redirect to admin UI
	if w.Code != http.StatusFound {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusFound, w.Code, w.Body.String())
	}

	location := w.Header().Get("Location")
	if location != "/admin/" {
		t.Errorf("Expected redirect to /admin/, got %s", location)
	}

	// Check that session cookie was set
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == AdminSessionCookieName {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("Expected session cookie to be set")
	}

	// Verify cookie properties
	if !sessionCookie.HttpOnly {
		t.Error("Expected HttpOnly cookie")
	}
	if !sessionCookie.Secure {
		t.Error("Expected Secure cookie")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Error("Expected SameSite=Lax")
	}

	// Verify session was created in database
	session, err := websession.FindByID(deps.Conns, sessionCookie.Value)
	if err != nil {
		t.Fatalf("Failed to get session from database: %v", err)
	}
	if session == nil {
		t.Fatal("Expected session to be created in database")
	}
	if session.OSMUserID != 12345 {
		t.Errorf("Expected OSM user ID 12345, got %d", session.OSMUserID)
	}
	if session.OSMAccessToken != "test-access-token" {
		t.Errorf("Expected access token test-access-token, got %s", session.OSMAccessToken)
	}
	if session.CSRFToken == "" {
		t.Error("Expected CSRF token to be set")
	}

	// Verify state was deleted from Redis (one-time use)
	_, err = mr.Get("test:admin_oauth_state:" + state)
	if err == nil {
		t.Error("Expected state to be deleted from Redis after use")
	}
}

func TestAdminLogoutHandler_WithSession(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	// Create a session in the database
	sessionID := "test-session-id"
	session := &db.WebSession{
		ID:              sessionID,
		OSMUserID:       12345,
		OSMAccessToken:  "test-token",
		OSMRefreshToken: "test-refresh",
		OSMTokenExpiry:  time.Now().Add(time.Hour),
		CSRFToken:       "test-csrf",
		CreatedAt:       time.Now(),
		LastActivity:    time.Now(),
		ExpiresAt:       time.Now().Add(7 * 24 * time.Hour),
	}
	if err := websession.Create(deps.Conns, session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	handler := AdminLogoutHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: sessionID})
	w := httptest.NewRecorder()

	handler(w, req)

	// Should redirect to home
	if w.Code != http.StatusFound {
		t.Errorf("Expected status %d, got %d", http.StatusFound, w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/" {
		t.Errorf("Expected redirect to /, got %s", location)
	}

	// Check that session cookie was cleared
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == AdminSessionCookieName {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("Expected session cookie to be present (cleared)")
	}

	// Cookie should be cleared (MaxAge -1 or Expires in past)
	if sessionCookie.MaxAge != -1 && !sessionCookie.Expires.Before(time.Now()) {
		t.Error("Expected session cookie to be cleared")
	}

	// Verify session was deleted from database
	deletedSession, err := websession.FindByID(deps.Conns, sessionID)
	if err != nil {
		t.Fatalf("Error checking for deleted session: %v", err)
	}
	if deletedSession != nil {
		t.Error("Expected session to be deleted from database")
	}
}

func TestAdminLogoutHandler_WithoutSession(t *testing.T) {
	deps, mr := setupAdminTestDeps(t)
	defer mr.Close()

	handler := AdminLogoutHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/admin/logout", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	// Should still redirect to home even without a session
	if w.Code != http.StatusFound {
		t.Errorf("Expected status %d, got %d", http.StatusFound, w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/" {
		t.Errorf("Expected redirect to /, got %s", location)
	}
}

func TestGenerateSecureToken(t *testing.T) {
	token1, err := generateSecureToken(32)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if len(token1) != 32 {
		t.Errorf("Expected token length 32, got %d", len(token1))
	}

	// Generate another token to verify randomness
	token2, err := generateSecureToken(32)
	if err != nil {
		t.Fatalf("Failed to generate second token: %v", err)
	}

	if token1 == token2 {
		t.Error("Expected unique tokens, got duplicates")
	}
}

func TestGenerateUUID(t *testing.T) {
	uuid1, err := generateUUID()
	if err != nil {
		t.Fatalf("Failed to generate UUID: %v", err)
	}

	// Check UUID format (8-4-4-4-12)
	parts := strings.Split(uuid1, "-")
	if len(parts) != 5 {
		t.Errorf("Expected UUID with 5 parts, got %d", len(parts))
	}

	expectedLengths := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expectedLengths[i] {
			t.Errorf("UUID part %d: expected length %d, got %d", i, expectedLengths[i], len(part))
		}
	}

	// Generate another UUID to verify uniqueness
	uuid2, err := generateUUID()
	if err != nil {
		t.Fatalf("Failed to generate second UUID: %v", err)
	}

	if uuid1 == uuid2 {
		t.Error("Expected unique UUIDs, got duplicates")
	}
}
