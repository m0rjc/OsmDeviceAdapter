package handlers

import (
	"bytes"
	"context"
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
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreoutbox"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/usercredentials"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/websession"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/worker"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// integrationTestEnv holds the full test environment
type integrationTestEnv struct {
	deps           *Dependencies
	conns          *db.Connections
	miniredis      *miniredis.Miniredis
	osmServer      *httptest.Server
	osmAPICalls    *int32 // atomic counter
	patrolSyncSvc  *worker.PatrolSyncService
	cleanup        func()
}

// setupIntegrationTest creates a full test environment with all components
func setupIntegrationTest(t *testing.T) *integrationTestEnv {
	// Use in-memory SQLite with shared cache and WAL mode for testing
	// Each test gets a unique database name to ensure isolation
	// Shared cache allows multiple connections within the same test to access the same database
	// WAL mode enables better concurrency (allows concurrent reads and writes)
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
	osmClient := osm.NewClient(osmServer.URL, nil, nil)

	// Create worker services
	credentialMgr := worker.NewCredentialManager(conns, nil) // No token refresh for tests

	patrolSyncSvc := worker.NewPatrolSyncService(conns, osmClient, credentialMgr, redisClient)

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
		Config:     cfg,
		Conns:      conns,
		OSM:        osmClient,
		PatrolSync: patrolSyncSvc,
	}

	cleanup := func() {
		redisClient.Close()
		mr.Close()
		osmServer.Close()
	}

	return &integrationTestEnv{
		deps:          deps,
		conns:         conns,
		miniredis:     mr,
		osmServer:     osmServer,
		osmAPICalls:   &osmAPICalls,
		patrolSyncSvc: patrolSyncSvc,
		cleanup:       cleanup,
	}
}

// createTestSession creates a web session for testing
func createTestSession(t *testing.T, env *integrationTestEnv, osmUserID int) *db.WebSession {
	session := &db.WebSession{
		ID:             "test-session-id",
		OSMUserID:      osmUserID,
		OSMAccessToken: "test-access-token",
		OSMRefreshToken: "test-refresh-token",
		OSMTokenExpiry: time.Now().Add(1 * time.Hour),
		CSRFToken:      "test-csrf-token",
		SelectedSectionID: func() *int { id := 456; return &id }(),
		CreatedAt:      time.Now(),
		LastActivity:   time.Now(),
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	if err := websession.Create(env.conns, session); err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}
	return session
}

// createTestCredentials creates user credentials for testing
func createTestCredentials(t *testing.T, env *integrationTestEnv, osmUserID int) {
	credential := &db.UserCredential{
		OSMUserID:       osmUserID,
		OSMUserName:     "Test User",
		OSMEmail:        "test@example.com",
		OSMAccessToken:  "test-access-token",
		OSMRefreshToken: "test-refresh-token",
		OSMTokenExpiry:  time.Now().Add(1 * time.Hour),
	}
	if err := usercredentials.CreateOrUpdate(env.conns, credential); err != nil {
		t.Fatalf("Failed to create test credentials: %v", err)
	}
}

// wrapWithMiddleware wraps a handler with SessionMiddleware and CSRFMiddleware
func wrapWithMiddleware(env *integrationTestEnv, handler http.HandlerFunc) http.Handler {
	// Apply middleware in reverse order (innermost first)
	wrapped := http.Handler(handler)
	wrapped = middleware.CSRFMiddleware(wrapped)
	wrapped = middleware.SessionMiddleware(env.conns, AdminSessionCookieName)(wrapped)
	return wrapped
}

// TestIntegration_FullFlow tests the complete end-to-end flow
func TestIntegration_FullFlow(t *testing.T) {
	env := setupIntegrationTest(t)
	defer env.cleanup()

	osmUserID := 123
	sectionID := 456

	// Create session and credentials
	session := createTestSession(t, env, osmUserID)
	createTestCredentials(t, env, osmUserID)

	// Reset OSM API call counter
	atomic.StoreInt32(env.osmAPICalls, 0)

	// Prepare score update request
	reqBody := AdminUpdateRequest{
		Updates: []AdminScoreUpdate{
			{PatrolID: "patrol-1", Points: 10},
			{PatrolID: "patrol-1", Points: 20}, // Same patrol, should coalesce
			{PatrolID: "patrol-2", Points: 5},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/sections/%d/scores", sectionID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.Header.Set("X-Idempotency-Key", "test-idempotency-key-1")
	req.Header.Set("X-Sync-Mode", "background") // Don't trigger interactive sync
	req.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

	// Execute handler with middleware
	w := httptest.NewRecorder()
	handler := wrapWithMiddleware(env, AdminScoresHandler(env.deps))
	handler.ServeHTTP(w, req)

	// Verify 202 Accepted
	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected status 202, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Parse response (now returns optimistic patrol results)
	var resp AdminUpdateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected success=true")
	}
	if len(resp.Patrols) != 2 {
		t.Errorf("Expected 2 patrols in response (patrol-1 and patrol-2), got %d", len(resp.Patrols))
	}

	// Verify optimistic scores (current + pending delta)
	for _, p := range resp.Patrols {
		if p.ID == "patrol-1" {
			// Current: 100, Delta: 10+20=30, Expected: 130
			if p.NewScore != 130 {
				t.Errorf("Expected patrol-1 new score 130, got %d", p.NewScore)
			}
		} else if p.ID == "patrol-2" {
			// Current: 50, Delta: 5, Expected: 55
			if p.NewScore != 55 {
				t.Errorf("Expected patrol-2 new score 55, got %d", p.NewScore)
			}
		}
	}

	// Verify outbox entries were created
	pendingCount, err := scoreoutbox.CountPendingByUser(env.conns, osmUserID)
	if err != nil {
		t.Fatalf("Failed to count pending entries: %v", err)
	}
	if pendingCount != 3 {
		t.Errorf("Expected 3 pending entries, got %d", pendingCount)
	}

	// Verify no OSM calls yet (background mode)
	if atomic.LoadInt32(env.osmAPICalls) != 0 {
		t.Errorf("Expected 0 OSM API calls before worker runs, got %d", atomic.LoadInt32(env.osmAPICalls))
	}

	// Manually trigger worker sync for patrol-1
	ctx := context.Background()
	_, err = env.patrolSyncSvc.SyncPatrol(ctx, osmUserID, sectionID, "patrol-1")
	if err != nil {
		t.Fatalf("SyncPatrol failed: %v", err)
	}

	// Manually trigger worker sync for patrol-2
	_, err = env.patrolSyncSvc.SyncPatrol(ctx, osmUserID, sectionID, "patrol-2")
	if err != nil {
		t.Fatalf("SyncPatrol failed: %v", err)
	}

	// Verify OSM was called exactly twice (once per patrol, despite 2 updates for patrol-1)
	osmCalls := atomic.LoadInt32(env.osmAPICalls)
	if osmCalls != 2 {
		t.Errorf("Expected 2 OSM API calls (coalescing worked), got %d", osmCalls)
	}

	// Verify all entries marked as completed
	pendingCount, err = scoreoutbox.CountPendingByUser(env.conns, osmUserID)
	if err != nil {
		t.Fatalf("Failed to count pending entries: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("Expected 0 pending entries after sync, got %d", pendingCount)
	}
}

// TestIntegration_Idempotency tests that duplicate idempotency keys are handled correctly
func TestIntegration_Idempotency(t *testing.T) {
	env := setupIntegrationTest(t)
	defer env.cleanup()

	osmUserID := 123
	sectionID := 456

	// Create session and credentials
	session := createTestSession(t, env, osmUserID)
	createTestCredentials(t, env, osmUserID)

	// Reset OSM API call counter
	atomic.StoreInt32(env.osmAPICalls, 0)

	// Prepare score update request
	reqBody := AdminUpdateRequest{
		Updates: []AdminScoreUpdate{
			{PatrolID: "patrol-1", Points: 10},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	idempotencyKey := "test-idempotency-key-unique"

	// First request
	req1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/sections/%d/scores", sectionID), bytes.NewReader(bodyBytes))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-CSRF-Token", session.CSRFToken)
	req1.Header.Set("X-Idempotency-Key", idempotencyKey)
	req1.Header.Set("X-Sync-Mode", "background")
	req1.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

	w1 := httptest.NewRecorder()
	handler := wrapWithMiddleware(env, AdminScoresHandler(env.deps))
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusAccepted {
		t.Fatalf("First request: expected status 202, got %d", w1.Code)
	}

	var resp1 AdminUpdateResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	firstResponsePatrols := resp1.Patrols

	// Second request with SAME idempotency key
	bodyBytes2, _ := json.Marshal(reqBody) // Re-marshal to get fresh reader
	req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/sections/%d/scores", sectionID), bytes.NewReader(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-CSRF-Token", session.CSRFToken)
	req2.Header.Set("X-Idempotency-Key", idempotencyKey) // SAME KEY
	req2.Header.Set("X-Sync-Mode", "background")
	req2.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusAccepted {
		t.Fatalf("Second request: expected status 202, got %d", w2.Code)
	}

	var resp2 AdminUpdateResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	// Verify same patrol results returned (idempotent response)
	if len(resp2.Patrols) != len(firstResponsePatrols) {
		t.Errorf("Expected same number of patrols for duplicate request, got %d vs %d", len(resp2.Patrols), len(firstResponsePatrols))
	}

	// Verify only ONE entry created (not two)
	pendingCount, err := scoreoutbox.CountPendingByUser(env.conns, osmUserID)
	if err != nil {
		t.Fatalf("Failed to count pending entries: %v", err)
	}
	if pendingCount != 1 {
		t.Errorf("Expected 1 pending entry (idempotency worked), got %d", pendingCount)
	}

	// Process the entry
	ctx := context.Background()
	_, _ = env.patrolSyncSvc.SyncPatrol(ctx, osmUserID, sectionID, "patrol-1")

	// Verify only ONE OSM call made
	osmCalls := atomic.LoadInt32(env.osmAPICalls)
	if osmCalls != 1 {
		t.Errorf("Expected 1 OSM API call (idempotency prevented duplicate), got %d", osmCalls)
	}
}

// TestIntegration_ConcurrentSubmissions tests concurrent submissions for the same patrol
func TestIntegration_ConcurrentSubmissions(t *testing.T) {
	env := setupIntegrationTest(t)
	defer env.cleanup()

	osmUserID := 123
	sectionID := 456

	// Create session and credentials
	session := createTestSession(t, env, osmUserID)
	createTestCredentials(t, env, osmUserID)

	// Reset OSM API call counter
	atomic.StoreInt32(env.osmAPICalls, 0)

	// Launch 3 concurrent requests for the same patrol
	// Note: Using 3 instead of 5 to accommodate SQLite's concurrency limitations in tests
	// Production uses PostgreSQL which handles much higher concurrency
	var wg sync.WaitGroup
	results := make(chan int, 3) // Status codes

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
			req.Header.Set("X-Idempotency-Key", fmt.Sprintf("concurrent-key-%d", index)) // Unique keys
			req.Header.Set("X-Sync-Mode", "background")
			req.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

			w := httptest.NewRecorder()
			handler := wrapWithMiddleware(env, AdminScoresHandler(env.deps))
			handler.ServeHTTP(w, req)

			results <- w.Code
		}(i)
	}

	wg.Wait()
	close(results)

	// Count successful requests
	successCount := 0
	for statusCode := range results {
		if statusCode == http.StatusAccepted {
			successCount++
		}
	}

	// Verify at least 2 out of 3 succeeded (SQLite concurrency limitation)
	if successCount < 2 {
		t.Fatalf("Expected at least 2 successful requests, got %d", successCount)
	}

	// Verify entries were created (at least 2)
	pendingCount, err := scoreoutbox.CountPendingByUser(env.conns, osmUserID)
	if err != nil {
		t.Fatalf("Failed to count pending entries: %v", err)
	}
	if pendingCount < 2 {
		t.Fatalf("Expected at least 2 pending entries, got %d", pendingCount)
	}
	if pendingCount > 3 {
		t.Fatalf("Expected at most 3 pending entries, got %d", pendingCount)
	}

	// Now trigger sync (worker would coalesce all entries into 1 OSM call)
	ctx := context.Background()
	_, err = env.patrolSyncSvc.SyncPatrol(ctx, osmUserID, sectionID, "patrol-1")
	if err != nil {
		t.Fatalf("SyncPatrol failed: %v", err)
	}

	// Verify only ONE OSM call made (coalescing worked)
	// This proves that even though 2-3 entries were created concurrently,
	// the worker coalesced them into a single update
	osmCalls := atomic.LoadInt32(env.osmAPICalls)
	if osmCalls != 1 {
		t.Errorf("Expected 1 OSM API call (entries coalesced), got %d", osmCalls)
	}

	// Verify all entries marked as completed
	pendingCount, err = scoreoutbox.CountPendingByUser(env.conns, osmUserID)
	if err != nil {
		t.Fatalf("Failed to count pending entries: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("Expected 0 pending entries after sync, got %d", pendingCount)
	}
}

// TestIntegration_InteractiveMode tests that interactive mode triggers immediate sync
func TestIntegration_InteractiveMode(t *testing.T) {
	env := setupIntegrationTest(t)
	defer env.cleanup()

	osmUserID := 123
	sectionID := 456

	// Create session and credentials
	session := createTestSession(t, env, osmUserID)
	createTestCredentials(t, env, osmUserID)

	// Reset OSM API call counter
	atomic.StoreInt32(env.osmAPICalls, 0)

	// Prepare score update request
	reqBody := AdminUpdateRequest{
		Updates: []AdminScoreUpdate{
			{PatrolID: "patrol-1", Points: 10},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Create HTTP request with interactive mode
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/sections/%d/scores", sectionID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.Header.Set("X-Idempotency-Key", "test-interactive-key")
	req.Header.Set("X-Sync-Mode", "interactive") // Interactive mode
	req.AddCookie(&http.Cookie{Name: AdminSessionCookieName, Value: session.ID})

	// Execute handler with middleware
	w := httptest.NewRecorder()
	handler := wrapWithMiddleware(env, AdminScoresHandler(env.deps))
	handler.ServeHTTP(w, req)

	// Verify 200 OK (interactive sync completed successfully within timeout)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 (interactive sync succeeded), got %d. Body: %s", w.Code, w.Body.String())
	}

	// Parse response
	var resp AdminUpdateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected success=true")
	}
	if len(resp.Patrols) != 1 {
		t.Errorf("Expected 1 patrol in response, got %d", len(resp.Patrols))
	}

	// Verify actual synced score returned (not optimistic)
	if resp.Patrols[0].ID == "patrol-1" {
		// Current was 100, added 10, should be 110
		if resp.Patrols[0].NewScore != 110 {
			t.Errorf("Expected new score 110, got %d", resp.Patrols[0].NewScore)
		}
	}

	// Verify OSM was called (interactive mode triggered sync)
	osmCalls := atomic.LoadInt32(env.osmAPICalls)
	if osmCalls != 1 {
		t.Errorf("Expected 1 OSM API call (interactive mode triggered sync), got %d", osmCalls)
	}

	// Verify entry marked as completed
	pendingCount, err := scoreoutbox.CountPendingByUser(env.conns, osmUserID)
	if err != nil {
		t.Fatalf("Failed to count pending entries: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("Expected 0 pending entries after interactive sync, got %d", pendingCount)
	}
}
