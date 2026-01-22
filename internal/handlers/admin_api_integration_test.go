package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/websession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// integrationTestEnv holds the full test environment
type integrationTestEnv struct {
	deps        *Dependencies
	conns       *db.Connections
	miniredis   *miniredis.Miniredis
	osmServer   *httptest.Server
	osmAPICalls *int32 // atomic counter
	cleanup     func()
}

// setupIntegrationTest creates a full test environment with all components
func setupIntegrationTest(t *testing.T) *integrationTestEnv {
	// Use in-memory SQLite with shared cache and WAL mode for testing
	dbName := fmt.Sprintf("file:%s?mode=memory&cache=shared&_journal_mode=WAL", t.Name())
	database, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
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
		mr.Close()
		t.Fatalf("Failed to create Redis client: %v", err)
	}

	// Create connections wrapper
	conns := db.NewConnections(database, redisClient)

	// Create atomic counter for OSM API calls
	var osmAPICalls int32

	// Mock OSM server
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/resource":
			// Return user profile with sections and terms
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": true,
				"data": map[string]interface{}{
					"user_id":   123,
					"full_name": "Test User",
					"sections": []map[string]interface{}{
						{
							"section_id":   456,
							"section_name": "Test Section",
							"group_name":   "Test Group",
							"terms": []map[string]interface{}{
								{
									"term_id":   1,
									"name":      "Spring 2024",
									"startdate": "2024-01-01",
									"enddate":   "2099-12-31",
								},
							},
						},
					},
				},
			})
		case "/ext/members/patrols/":
			action := r.URL.Query().Get("action")
			if action == "getPatrolsWithPeople" {
				// Return patrol scores
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"patrol-1": map[string]interface{}{
						"patrolid": "patrol-1",
						"name":     "Eagles",
						"points":   "100",
						"members":  []interface{}{map[string]interface{}{"member_id": "1"}},
					},
					"patrol-2": map[string]interface{}{
						"patrolid": "patrol-2",
						"name":     "Hawks",
						"points":   "50",
						"members":  []interface{}{map[string]interface{}{"member_id": "2"}},
					},
				})
			} else if action == "updatePatrolPoints" {
				// Increment API call counter
				atomic.AddInt32(&osmAPICalls, 1)
				// Return success
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]interface{}{})
			}
		default:
			http.NotFound(w, r)
		}
	}))

	// Create OSM client pointing to mock server
	osmClient := osm.NewClient(osmServer.URL, nil, nil, nil)

	// Create handler dependencies
	cfg := &config.Config{
		ExternalDomains: config.ExternalDomainsConfig{
			ExposedDomain: "https://example.com",
			OSMDomain:     osmServer.URL,
		},
		OAuth: config.OAuthConfig{
			OSMClientID:     "test-client-id",
			OSMClientSecret: "test-client-secret",
		},
	}

	deps := &Dependencies{
		Config: cfg,
		Conns:  conns,
		OSM:    osmClient,
	}

	cleanup := func() {
		redisClient.Close()
		mr.Close()
		osmServer.Close()
	}

	return &integrationTestEnv{
		deps:        deps,
		conns:       conns,
		miniredis:   mr,
		osmServer:   osmServer,
		osmAPICalls: &osmAPICalls,
		cleanup:     cleanup,
	}
}

// createTestSession creates a web session for testing
func createTestSession(t *testing.T, env *integrationTestEnv, osmUserID int) *db.WebSession {
	session := &db.WebSession{
		ID:                "test-session-id",
		OSMUserID:         osmUserID,
		OSMAccessToken:    "test-access-token",
		OSMRefreshToken:   "test-refresh-token",
		OSMTokenExpiry:    time.Now().Add(1 * time.Hour),
		CSRFToken:         "test-csrf-token",
		SelectedSectionID: func() *int { id := 456; return &id }(),
		CreatedAt:         time.Now(),
		LastActivity:      time.Now(),
		ExpiresAt:         time.Now().Add(24 * time.Hour),
	}
	if err := websession.Create(env.conns, session); err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}
	return session
}

// wrapWithMiddleware wraps a handler with SessionMiddleware and CSRFMiddleware
// and registers it on a ServeMux for proper path parameter handling
func wrapWithMiddleware(env *integrationTestEnv, handler http.HandlerFunc) http.Handler {
	// Create a ServeMux for proper path parameter handling
	mux := http.NewServeMux()

	// Apply middleware in reverse order (innermost first)
	wrapped := http.Handler(handler)
	wrapped = middleware.CSRFMiddleware(wrapped)
	wrapped = middleware.SessionMiddleware(env.conns, AdminSessionCookieName)(wrapped)

	// Register the handler with the path pattern
	mux.Handle("/api/admin/sections/{sectionId}/scores", wrapped)

	return mux
}

// TestIntegration_BasicScoreUpdate tests a simple successful score update
func TestIntegration_BasicScoreUpdate(t *testing.T) {
	env := setupIntegrationTest(t)
	defer env.cleanup()

	osmUserID := 123
	sectionID := 456

	// Create session
	session := createTestSession(t, env, osmUserID)

	// Reset OSM API call counter
	atomic.StoreInt32(env.osmAPICalls, 0)

	// Prepare score update request
	reqBody := AdminUpdateRequest{
		Updates: []AdminScoreUpdate{
			{PatrolID: "patrol-1", Points: 10},
			{PatrolID: "patrol-2", Points: 5},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/sections/%d/scores", sectionID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

	// Execute handler with middleware
	w := httptest.NewRecorder()
	handler := wrapWithMiddleware(env, AdminScoresHandler(env.deps))
	handler.ServeHTTP(w, req)

	// Verify 200 OK (synchronous)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Parse response
	var resp AdminUpdateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify we got results for 2 patrols
	if len(resp.Patrols) != 2 {
		t.Errorf("Expected 2 patrols in response, got %d", len(resp.Patrols))
	}

	// Verify patrol-1 result
	patrol1 := findPatrolResult(resp.Patrols, "patrol-1")
	if patrol1 == nil {
		t.Fatal("Expected patrol-1 in response")
	}
	if !patrol1.Success {
		t.Errorf("Expected patrol-1 success=true, got false. Error: %v", patrol1.ErrorMessage)
	}
	if patrol1.PreviousScore == nil || *patrol1.PreviousScore != 100 {
		t.Errorf("Expected patrol-1 previous score 100, got %v", patrol1.PreviousScore)
	}
	if patrol1.NewScore == nil || *patrol1.NewScore != 110 {
		t.Errorf("Expected patrol-1 new score 110 (100+10), got %v", patrol1.NewScore)
	}

	// Verify patrol-2 result
	patrol2 := findPatrolResult(resp.Patrols, "patrol-2")
	if patrol2 == nil {
		t.Fatal("Expected patrol-2 in response")
	}
	if !patrol2.Success {
		t.Errorf("Expected patrol-2 success=true, got false. Error: %v", patrol2.ErrorMessage)
	}
	if patrol2.PreviousScore == nil || *patrol2.PreviousScore != 50 {
		t.Errorf("Expected patrol-2 previous score 50, got %v", patrol2.PreviousScore)
	}
	if patrol2.NewScore == nil || *patrol2.NewScore != 55 {
		t.Errorf("Expected patrol-2 new score 55 (50+5), got %v", patrol2.NewScore)
	}

	// Verify OSM was called twice (once per patrol)
	osmCalls := atomic.LoadInt32(env.osmAPICalls)
	if osmCalls != 2 {
		t.Errorf("Expected 2 OSM API calls, got %d", osmCalls)
	}
}

// TestIntegration_ConcurrentUpdates tests concurrent updates to the same patrol
func TestIntegration_ConcurrentUpdates(t *testing.T) {
	env := setupIntegrationTest(t)
	defer env.cleanup()

	osmUserID := 123
	sectionID := 456

	// Create session
	session := createTestSession(t, env, osmUserID)

	// Reset OSM API call counter
	atomic.StoreInt32(env.osmAPICalls, 0)

	// Launch 3 concurrent requests for the same patrol
	var wg sync.WaitGroup
	results := make([]*AdminUpdateResponse, 3)
	statusCodes := make([]int, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			reqBody := AdminUpdateRequest{
				Updates: []AdminScoreUpdate{
					{PatrolID: "patrol-1", Points: 1},
				},
			}
			bodyBytes, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/sections/%d/scores", sectionID), bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
			req.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

			w := httptest.NewRecorder()
			handler := wrapWithMiddleware(env, AdminScoresHandler(env.deps))
			handler.ServeHTTP(w, req)

			statusCodes[index] = w.Code

			var resp AdminUpdateResponse
			if w.Code == http.StatusOK {
				json.Unmarshal(w.Body.Bytes(), &resp)
				results[index] = &resp
			}
		}(i)
	}

	wg.Wait()

	// Count successful requests (should get lock) vs locked requests (couldn't get lock)
	successCount := 0
	lockedCount := 0
	for i := 0; i < 3; i++ {
		if statusCodes[i] == http.StatusOK && results[i] != nil && len(results[i].Patrols) > 0 {
			if results[i].Patrols[0].Success {
				successCount++
			} else if results[i].Patrols[0].IsTemporaryError != nil && *results[i].Patrols[0].IsTemporaryError {
				lockedCount++
			}
		}
	}

	// Only one request should succeed (got the lock)
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful request (got lock), got %d", successCount)
	}

	// The other 2 should fail with lock contention
	if lockedCount != 2 {
		t.Errorf("Expected 2 requests to fail with lock contention, got %d", lockedCount)
	}

	// Verify only ONE OSM call was made (only the successful one)
	osmCalls := atomic.LoadInt32(env.osmAPICalls)
	if osmCalls != 1 {
		t.Errorf("Expected 1 OSM API call (only successful request), got %d", osmCalls)
	}
}

// TestIntegration_MultiplePatrols tests updating multiple different patrols
func TestIntegration_MultiplePatrols(t *testing.T) {
	env := setupIntegrationTest(t)
	defer env.cleanup()

	osmUserID := 123
	sectionID := 456

	// Create session
	session := createTestSession(t, env, osmUserID)

	// Reset OSM API call counter
	atomic.StoreInt32(env.osmAPICalls, 0)

	// Update both patrols
	reqBody := AdminUpdateRequest{
		Updates: []AdminScoreUpdate{
			{PatrolID: "patrol-1", Points: 25},
			{PatrolID: "patrol-2", Points: 15},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/sections/%d/scores", sectionID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

	w := httptest.NewRecorder()
	handler := wrapWithMiddleware(env, AdminScoresHandler(env.deps))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp AdminUpdateResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Both should succeed
	if len(resp.Patrols) != 2 {
		t.Fatalf("Expected 2 patrols, got %d", len(resp.Patrols))
	}

	for _, p := range resp.Patrols {
		if !p.Success {
			t.Errorf("Expected patrol %s to succeed, got error: %v", p.ID, p.ErrorMessage)
		}
	}

	// Verify both OSM calls were made
	osmCalls := atomic.LoadInt32(env.osmAPICalls)
	if osmCalls != 2 {
		t.Errorf("Expected 2 OSM API calls, got %d", osmCalls)
	}
}

// Helper function to find a patrol result by ID
func findPatrolResult(patrols []AdminPatrolResult, patrolID string) *AdminPatrolResult {
	for _, p := range patrols {
		if p.ID == patrolID {
			return &p
		}
	}
	return nil
}
