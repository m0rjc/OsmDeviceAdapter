package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreoutbox"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/usercredentials"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDeps creates test dependencies with in-memory database and Redis
func setupTestDeps(t *testing.T) (*db.Connections, *miniredis.Miniredis, func()) {
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
		mr.Close()
		t.Fatalf("Failed to create Redis client: %v", err)
	}

	// Create connections wrapper
	conns := db.NewConnections(database, redisClient)

	cleanup := func() {
		redisClient.Close()
		mr.Close()
	}

	return conns, mr, cleanup
}

// createTestCredentials creates user credentials for testing
func createTestCredentials(t *testing.T, conns *db.Connections, osmUserID int) {
	credential := &db.UserCredential{
		OSMUserID:       osmUserID,
		OSMUserName:     "Test User",
		OSMEmail:        "test@example.com",
		OSMAccessToken:  "test-access-token",
		OSMRefreshToken: "test-refresh-token",
		OSMTokenExpiry:  time.Now().Add(1 * time.Hour),
	}
	if err := usercredentials.CreateOrUpdate(conns, credential); err != nil {
		t.Fatalf("Failed to create test credentials: %v", err)
	}
}

// createTestOutboxEntries creates outbox entries for testing
func createTestOutboxEntries(t *testing.T, conns *db.Connections, osmUserID, sectionID int, patrolID string, count int) []db.ScoreUpdateOutbox {
	entries := make([]db.ScoreUpdateOutbox, count)
	for i := 0; i < count; i++ {
		entries[i] = db.ScoreUpdateOutbox{
			IdempotencyKey: fmt.Sprintf("test-key-%d", i),
			OSMUserID:      osmUserID,
			SectionID:      sectionID,
			PatrolID:       patrolID,
			PointsDelta:    10 * (i + 1), // 10, 20, 30, etc.
			Status:         "pending",
			BatchID:        "test-batch",
		}
	}
	if err := scoreoutbox.CreateBatch(conns, entries); err != nil {
		t.Fatalf("Failed to create test outbox entries: %v", err)
	}
	return entries
}

func TestSyncPatrol_Success(t *testing.T) {
	conns, _, cleanup := setupTestDeps(t)
	defer cleanup()

	osmUserID := 123
	sectionID := 456
	patrolID := "patrol-1"

	// Create credentials
	createTestCredentials(t, conns, osmUserID)

	// Create multiple outbox entries (to test coalescing)
	createTestOutboxEntries(t, conns, osmUserID, sectionID, patrolID, 3)

	// Mock OSM server
	osmAPICalls := 0
	var receivedNewScore int
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/resource":
			// Return user profile with sections and terms
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": true,
				"data": map[string]interface{}{
					"user_id":   osmUserID,
					"full_name": "Test User",
					"sections": []map[string]interface{}{
						{
							"section_id":   sectionID,
							"section_name": "Test Section",
							"group_name":   "Test Group",
							"terms": []map[string]interface{}{
								{
									"term_id":   1,
									"name":      "Spring 2024",
									"startdate": "2024-01-01",
									"enddate":   "2099-12-31", // Far future so it's always active
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
					patrolID: map[string]interface{}{
						"patrolid": patrolID,
						"name":     "Test Patrol",
						"points":   "100", // Current score (string format)
						"members":  []interface{}{map[string]interface{}{"member_id": "1"}}, // At least one member
					},
				})
			} else if action == "updatePatrolPoints" {
				// Record the update
				osmAPICalls++
				if err := r.ParseForm(); err != nil {
					t.Errorf("Failed to parse form: %v", err)
				}
				fmt.Sscanf(r.FormValue("points"), "%d", &receivedNewScore)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]interface{}{})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer osmServer.Close()

	// Create OSM client pointing to mock server
	osmClient := osm.NewClient(osmServer.URL, nil, nil)

	// Create worker services
	credentialMgr := NewCredentialManager(conns, nil) // No token refresh needed for this test
	rawRedis, _ := redis.ParseURL("redis://" + conns.Redis.Client().Options().Addr)
	redisClient := redis.NewClient(rawRedis)
	defer redisClient.Close()

	syncService := NewPatrolSyncService(conns, osmClient, credentialMgr, redisClient)

	// Execute sync
	ctx := context.Background()
	err := syncService.SyncPatrol(ctx, osmUserID, sectionID, patrolID)

	// Verify success
	if err != nil {
		t.Errorf("SyncPatrol failed: %v", err)
	}

	// Verify only ONE OSM API call was made (coalescing)
	if osmAPICalls != 1 {
		t.Errorf("Expected 1 OSM API call, got %d", osmAPICalls)
	}

	// Verify correct score was sent (100 + 10 + 20 + 30 = 160)
	expectedScore := 160
	if receivedNewScore != expectedScore {
		t.Errorf("Expected new score %d, got %d", expectedScore, receivedNewScore)
	}

	// Verify all entries marked as completed
	entries, err := scoreoutbox.FindByIdempotencyKey(conns, "test-key-0")
	if err != nil {
		t.Fatalf("Failed to fetch entry: %v", err)
	}
	if entries.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", entries.Status)
	}

	// Verify no pending entries remain
	pendingCount, err := scoreoutbox.CountPendingByUser(conns, osmUserID)
	if err != nil {
		t.Fatalf("Failed to count pending: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("Expected 0 pending entries, got %d", pendingCount)
	}
}

func TestSyncPatrol_AuthRevoked(t *testing.T) {
	conns, _, cleanup := setupTestDeps(t)
	defer cleanup()

	osmUserID := 123
	sectionID := 456
	patrolID := "patrol-1"

	// Create credentials
	createTestCredentials(t, conns, osmUserID)

	// Create outbox entries
	createTestOutboxEntries(t, conns, osmUserID, sectionID, patrolID, 2)

	// Mock OSM server that returns 401
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 401 Unauthorized
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "unauthorized",
		})
	}))
	defer osmServer.Close()

	// Create OSM client pointing to mock server
	osmClient := osm.NewClient(osmServer.URL, nil, nil)

	// Create worker services
	credentialMgr := NewCredentialManager(conns, nil)
	rawRedis, _ := redis.ParseURL("redis://" + conns.Redis.Client().Options().Addr)
	redisClient := redis.NewClient(rawRedis)
	defer redisClient.Close()

	syncService := NewPatrolSyncService(conns, osmClient, credentialMgr, redisClient)

	// Execute sync (should fail)
	ctx := context.Background()
	err := syncService.SyncPatrol(ctx, osmUserID, sectionID, patrolID)

	// Verify it failed
	if err == nil {
		t.Error("Expected SyncPatrol to fail with 401, but it succeeded")
	}

	// Verify entries are marked as failed (not auth_revoked, because we're not using token refresh in this test)
	// In real scenario with token refresh, they would be marked as auth_revoked
	entries, err := scoreoutbox.FindByIdempotencyKey(conns, "test-key-0")
	if err != nil {
		t.Fatalf("Failed to fetch entry: %v", err)
	}
	if entries.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", entries.Status)
	}
}

func TestSyncPatrol_PatrolNotFound(t *testing.T) {
	conns, _, cleanup := setupTestDeps(t)
	defer cleanup()

	osmUserID := 123
	sectionID := 456
	patrolID := "nonexistent-patrol"

	// Create credentials
	createTestCredentials(t, conns, osmUserID)

	// Create outbox entries for nonexistent patrol
	createTestOutboxEntries(t, conns, osmUserID, sectionID, patrolID, 1)

	// Mock OSM server that returns empty patrol list
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/resource" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": true,
				"data": map[string]interface{}{
					"user_id":   osmUserID,
					"full_name": "Test User",
					"sections": []map[string]interface{}{
						{
							"section_id":   sectionID,
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
			return
		}
		if r.URL.Path == "/ext/members/patrols/" {
			action := r.URL.Query().Get("action")
			if action == "getPatrolsWithPeople" {
				// Return empty patrol list
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					// Empty map - no patrols
				})
			}
		}
	}))
	defer osmServer.Close()

	// Create OSM client pointing to mock server
	osmClient := osm.NewClient(osmServer.URL, nil, nil)

	// Create worker services
	credentialMgr := NewCredentialManager(conns, nil)
	rawRedis, _ := redis.ParseURL("redis://" + conns.Redis.Client().Options().Addr)
	redisClient := redis.NewClient(rawRedis)
	defer redisClient.Close()

	syncService := NewPatrolSyncService(conns, osmClient, credentialMgr, redisClient)

	// Execute sync
	ctx := context.Background()
	err := syncService.SyncPatrol(ctx, osmUserID, sectionID, patrolID)

	// Verify it failed
	if err == nil {
		t.Error("Expected SyncPatrol to fail when patrol not found")
	}

	// Verify entry is marked as failed with no retry (patrol doesn't exist)
	entries, err := scoreoutbox.FindByIdempotencyKey(conns, "test-key-0")
	if err != nil {
		t.Fatalf("Failed to fetch entry: %v", err)
	}
	if entries.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", entries.Status)
	}
	if entries.NextRetryAt != nil {
		t.Error("Expected no retry for nonexistent patrol")
	}
}

func TestSyncPatrol_ConcurrentLock(t *testing.T) {
	conns, _, cleanup := setupTestDeps(t)
	defer cleanup()

	osmUserID := 123
	sectionID := 456
	patrolID := "patrol-1"

	// Create credentials
	createTestCredentials(t, conns, osmUserID)

	// Create outbox entries
	createTestOutboxEntries(t, conns, osmUserID, sectionID, patrolID, 1)

	// Mock OSM server
	osmAPICalls := 0
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/resource" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": true,
				"data": map[string]interface{}{
					"user_id":   osmUserID,
					"full_name": "Test User",
					"sections": []map[string]interface{}{
						{
							"section_id":   sectionID,
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
			return
		}
		if r.URL.Path == "/ext/members/patrols/" {
			action := r.URL.Query().Get("action")
			if action == "getPatrolsWithPeople" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					patrolID: map[string]interface{}{
						"patrolid": patrolID,
						"name":     "Test Patrol",
						"points":   "100", // string format
						"members":  []interface{}{map[string]interface{}{"member_id": "1"}},
					},
				})
			} else if action == "updatePatrolPoints" {
				osmAPICalls++
				// Simulate slow OSM API
				time.Sleep(100 * time.Millisecond)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]interface{}{})
			}
		}
	}))
	defer osmServer.Close()

	// Create OSM client pointing to mock server
	osmClient := osm.NewClient(osmServer.URL, nil, nil)

	// Create worker services
	credentialMgr := NewCredentialManager(conns, nil)
	rawRedis, _ := redis.ParseURL("redis://" + conns.Redis.Client().Options().Addr)
	redisClient := redis.NewClient(rawRedis)
	defer redisClient.Close()

	syncService := NewPatrolSyncService(conns, osmClient, credentialMgr, redisClient)

	// Execute two syncs concurrently
	ctx := context.Background()
	done := make(chan error, 2)

	go func() {
		done <- syncService.SyncPatrol(ctx, osmUserID, sectionID, patrolID)
	}()

	go func() {
		// Start second sync slightly after first
		time.Sleep(10 * time.Millisecond)
		done <- syncService.SyncPatrol(ctx, osmUserID, sectionID, patrolID)
	}()

	// Wait for both
	err1 := <-done
	err2 := <-done

	// At least one should succeed
	if err1 != nil && err2 != nil {
		t.Errorf("Both syncs failed: err1=%v, err2=%v", err1, err2)
	}

	// Only ONE OSM API call should be made (lock prevents concurrent processing)
	if osmAPICalls > 1 {
		t.Errorf("Expected 1 OSM API call due to locking, got %d", osmAPICalls)
	}
}

func TestSyncPatrol_NoEntriesAfterClaim(t *testing.T) {
	conns, _, cleanup := setupTestDeps(t)
	defer cleanup()

	osmUserID := 123
	sectionID := 456
	patrolID := "patrol-1"

	// Create credentials
	createTestCredentials(t, conns, osmUserID)

	// Don't create any outbox entries

	// Mock OSM server (should not be called)
	osmAPICalls := 0
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		osmAPICalls++
	}))
	defer osmServer.Close()

	// Create OSM client pointing to mock server
	osmClient := osm.NewClient(osmServer.URL, nil, nil)

	// Create worker services
	credentialMgr := NewCredentialManager(conns, nil)
	rawRedis, _ := redis.ParseURL("redis://" + conns.Redis.Client().Options().Addr)
	redisClient := redis.NewClient(rawRedis)
	defer redisClient.Close()

	syncService := NewPatrolSyncService(conns, osmClient, credentialMgr, redisClient)

	// Execute sync (should be no-op)
	ctx := context.Background()
	err := syncService.SyncPatrol(ctx, osmUserID, sectionID, patrolID)

	// Verify success (no-op is not an error)
	if err != nil {
		t.Errorf("SyncPatrol failed: %v", err)
	}

	// Verify no OSM API calls were made
	if osmAPICalls != 0 {
		t.Errorf("Expected 0 OSM API calls, got %d", osmAPICalls)
	}
}

func TestRedisLock_AcquireRelease(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	opt, _ := redis.ParseURL("redis://" + mr.Addr())
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()

	lock := NewRedisLock(redisClient, 123, 456, "patrol-1", 10*time.Second)

	// Acquire lock
	err = lock.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Try to acquire again (should fail)
	err = lock.Acquire(context.Background())
	if err != ErrLockNotAcquired {
		t.Errorf("Expected ErrLockNotAcquired, got: %v", err)
	}

	// Release lock
	err = lock.Release(context.Background())
	if err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}

	// Should be able to acquire again
	lock2 := NewRedisLock(redisClient, 123, 456, "patrol-1", 10*time.Second)
	err = lock2.Acquire(context.Background())
	if err != nil {
		t.Errorf("Failed to acquire lock after release: %v", err)
	}
}

func TestRedisLock_TryAcquire(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	opt, _ := redis.ParseURL("redis://" + mr.Addr())
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()

	lock := NewRedisLock(redisClient, 123, 456, "patrol-1", 10*time.Second)

	// Try acquire (should succeed)
	acquired, err := lock.TryAcquire(context.Background())
	if err != nil {
		t.Fatalf("TryAcquire failed: %v", err)
	}
	if !acquired {
		t.Error("Expected lock to be acquired")
	}

	// Try acquire again (should return false, not error)
	lock2 := NewRedisLock(redisClient, 123, 456, "patrol-1", 10*time.Second)
	acquired, err = lock2.TryAcquire(context.Background())
	if err != nil {
		t.Fatalf("TryAcquire failed: %v", err)
	}
	if acquired {
		t.Error("Expected lock not to be acquired")
	}
}
