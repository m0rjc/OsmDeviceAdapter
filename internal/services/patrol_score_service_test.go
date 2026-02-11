package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/sectionsettings"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// mockStore implements osm.RateLimitStore and osm.LatencyRecorder for tests.
type mockStore struct{}

func (m *mockStore) MarkOsmServiceBlocked(ctx context.Context)                              {}
func (m *mockStore) IsOsmServiceBlocked(ctx context.Context) bool                           { return false }
func (m *mockStore) MarkUserTemporarilyBlocked(ctx context.Context, userId int, until time.Time) {}
func (m *mockStore) GetUserBlockEndTime(ctx context.Context, userId int) time.Time          { return time.Time{} }
func (m *mockStore) RecordOsmLatency(endpoint string, statusCode int, latency time.Duration) {}
func (m *mockStore) RecordRateLimit(userId *int, limitRemaining int, limitTotal int, limitResetSeconds int) {
}

// testHarness bundles all the pieces needed for a PatrolScoreService test.
type testHarness struct {
	conns     *db.Connections
	osmServer *httptest.Server
	service   *PatrolScoreService
	mr        *miniredis.Miniredis
	device    *db.DeviceCode
	user      types.User
}

const (
	testSectionID = 12345
	testTermID    = 999
	testUserID    = 42
	testToken     = "test-osm-token"
	testDevCode   = "test-device-code-abcdef"
)

// newTestHarness creates a fully wired test harness with a mock OSM server
// that serves profile and patrol-score endpoints.
func newTestHarness(t *testing.T, patrolMap map[string]osm.PatrolData) *testHarness {
	t.Helper()

	// ---------- mock OSM HTTP server ----------
	now := time.Now()
	osmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Rate-limit headers expected by the client
		w.Header().Set("X-RateLimit-Remaining", "500")
		w.Header().Set("X-RateLimit-Limit", "1000")
		w.Header().Set("X-RateLimit-Reset", "60")

		switch r.URL.Path {
		case "/oauth/resource":
			// Profile response with one section and an active term
			resp := types.OSMProfileResponse{
				Status: true,
				Data: &types.OSMProfileData{
					UserID: testUserID,
					Sections: []types.OSMSection{
						{
							SectionID:   testSectionID,
							SectionName: "Cubs",
							Terms: []types.OSMTerm{
								{
									TermID:    testTermID,
									Name:      "Spring 2026",
									StartDate: now.AddDate(0, -1, 0).Format("2006-01-02"),
									EndDate:   now.AddDate(0, 1, 0).Format("2006-01-02"),
								},
							},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)

		case "/ext/members/patrols/":
			json.NewEncoder(w).Encode(patrolMap)

		default:
			http.NotFound(w, r)
		}
	}))

	// ---------- database (SQLite in-memory) ----------
	conns := db.SetupTestDB(t)

	// ---------- Redis (miniredis) ----------
	mr := miniredis.RunT(t)
	conns.Redis = newTestRedisClient(t, mr.Addr())

	// ---------- OSM client ----------
	store := &mockStore{}
	osmClient := osm.NewClient(osmServer.URL, store, store)

	// ---------- config ----------
	cfg := &config.Config{
		Cache: config.CacheConfig{
			CacheFallbackTTL: 691200, // 8 days
		},
	}

	// ---------- service ----------
	svc := NewPatrolScoreService(osmClient, conns, cfg)

	// ---------- device record ----------
	sectionID := testSectionID
	userID := testUserID
	accessToken := testToken
	deviceAccessToken := "device-access-token-xyz"

	device := &db.DeviceCode{
		DeviceCode:        testDevCode,
		UserCode:          "TEST-CODE",
		ClientID:          "test-client",
		Status:            "authorized",
		ExpiresAt:         now.Add(24 * time.Hour),
		SectionID:         &sectionID,
		OsmUserID:         &userID,
		OSMAccessToken:    &accessToken,
		DeviceAccessToken: &deviceAccessToken,
	}

	if err := conns.DB.Create(device).Error; err != nil {
		t.Fatalf("failed to create device record: %v", err)
	}

	user := types.NewUser(&userID, testToken)

	return &testHarness{
		conns:     conns,
		osmServer: osmServer,
		service:   svc,
		mr:        mr,
		device:    device,
		user:      user,
	}
}

// newTestRedisClient creates a db.RedisClient pointing at miniredis.
// This is needed because db.NewRedisClient does a Ping (which miniredis supports)
// but expects a redis:// URL.
func newTestRedisClient(t *testing.T, addr string) *db.RedisClient {
	t.Helper()
	url := fmt.Sprintf("redis://%s", addr)
	rc, err := db.NewRedisClient(url, "test:")
	if err != nil {
		t.Fatalf("failed to create test redis client: %v", err)
	}
	return rc
}

// samplePatrolMap returns a realistic patrol map as the OSM API would return.
func samplePatrolMap() map[string]osm.PatrolData {
	return map[string]osm.PatrolData{
		"1": {PatrolID: "1", Name: "Eagles", Points: "45", Members: []any{"a"}},
		"2": {PatrolID: "2", Name: "Hawks", Points: "30", Members: []any{"b"}},
		"3": {PatrolID: "3", Name: "Owls", Points: "20", Members: []any{"c"}},
	}
}

func TestGetPatrolScores_ColorNamesIncludedInResponse(t *testing.T) {
	h := newTestHarness(t, samplePatrolMap())
	defer h.osmServer.Close()

	// Insert section settings with color names
	colors := map[string]string{
		"1": "red",
		"2": "blue",
		"3": "green",
	}
	if err := sectionsettings.UpsertPatrolColors(h.conns, testUserID, testSectionID, colors); err != nil {
		t.Fatalf("failed to upsert patrol colors: %v", err)
	}

	resp, err := h.service.GetPatrolScores(context.Background(), h.user, h.device)
	if err != nil {
		t.Fatalf("GetPatrolScores failed: %v", err)
	}

	if resp.Settings == nil {
		t.Fatal("expected Settings to be non-nil when colors are configured")
	}

	if len(resp.Settings.PatrolColors) != 3 {
		t.Fatalf("expected 3 patrol colors, got %d", len(resp.Settings.PatrolColors))
	}

	expected := map[string]string{"1": "red", "2": "blue", "3": "green"}
	for id, wantColor := range expected {
		got, ok := resp.Settings.PatrolColors[id]
		if !ok {
			t.Errorf("missing color for patrol %s", id)
			continue
		}
		if got != wantColor {
			t.Errorf("patrol %s: want color %q, got %q", id, wantColor, got)
		}
	}
}

func TestGetPatrolScores_NoColorsConfigured(t *testing.T) {
	h := newTestHarness(t, samplePatrolMap())
	defer h.osmServer.Close()

	// No section settings inserted — should get nil Settings
	resp, err := h.service.GetPatrolScores(context.Background(), h.user, h.device)
	if err != nil {
		t.Fatalf("GetPatrolScores failed: %v", err)
	}

	if resp.Settings != nil {
		t.Errorf("expected Settings to be nil when no colors configured, got %+v", resp.Settings)
	}

	// Verify patrols are still returned
	if len(resp.Patrols) != 3 {
		t.Errorf("expected 3 patrols, got %d", len(resp.Patrols))
	}
}

func TestGetPatrolScores_ColorsSurviveCacheRoundTrip(t *testing.T) {
	h := newTestHarness(t, samplePatrolMap())
	defer h.osmServer.Close()

	// Insert colors
	colors := map[string]string{"1": "red", "2": "blue", "3": "green"}
	if err := sectionsettings.UpsertPatrolColors(h.conns, testUserID, testSectionID, colors); err != nil {
		t.Fatalf("failed to upsert patrol colors: %v", err)
	}

	// First call — fetches from OSM and caches
	resp1, err := h.service.GetPatrolScores(context.Background(), h.user, h.device)
	if err != nil {
		t.Fatalf("first GetPatrolScores failed: %v", err)
	}
	if resp1.FromCache {
		t.Error("expected first call to NOT be from cache")
	}
	if resp1.Settings == nil {
		t.Fatal("expected Settings on first call")
	}

	// Second call — should come from cache
	resp2, err := h.service.GetPatrolScores(context.Background(), h.user, h.device)
	if err != nil {
		t.Fatalf("second GetPatrolScores failed: %v", err)
	}
	if !resp2.FromCache {
		t.Error("expected second call to be from cache")
	}

	// Settings should still be present on the cached response
	if resp2.Settings == nil {
		t.Fatal("expected Settings on cached response")
	}

	for id, wantColor := range colors {
		got, ok := resp2.Settings.PatrolColors[id]
		if !ok {
			t.Errorf("cached response: missing color for patrol %s", id)
			continue
		}
		if got != wantColor {
			t.Errorf("cached response: patrol %s: want color %q, got %q", id, wantColor, got)
		}
	}
}

func TestGetPatrolScores_SettingsReturnedOnCachedScoreResponse(t *testing.T) {
	h := newTestHarness(t, samplePatrolMap())
	defer h.osmServer.Close()

	// Pre-populate Redis cache with patrol scores (simulating a previous fetch)
	cached := &CachedPatrolScores{
		Patrols: []types.PatrolScore{
			{ID: "1", Name: "Eagles", Score: 45},
			{ID: "2", Name: "Hawks", Score: 30},
		},
		CachedAt:       time.Now(),
		ValidUntil:     time.Now().Add(10 * time.Minute),
		RateLimitState: RateLimitStateNone,
	}
	cacheData, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("failed to marshal cache data: %v", err)
	}
	cacheKey := fmt.Sprintf("patrol_scores:%s", testDevCode)
	if err := h.conns.Redis.Set(context.Background(), cacheKey, cacheData, 10*time.Minute).Err(); err != nil {
		t.Fatalf("failed to pre-populate redis cache: %v", err)
	}

	// Insert colors into DB
	colors := map[string]string{"1": "red", "2": "blue"}
	if err := sectionsettings.UpsertPatrolColors(h.conns, testUserID, testSectionID, colors); err != nil {
		t.Fatalf("failed to upsert patrol colors: %v", err)
	}

	resp, err := h.service.GetPatrolScores(context.Background(), h.user, h.device)
	if err != nil {
		t.Fatalf("GetPatrolScores failed: %v", err)
	}

	if !resp.FromCache {
		t.Error("expected response to be from cache")
	}

	// Settings should be fetched fresh from DB even though scores came from cache
	if resp.Settings == nil {
		t.Fatal("expected Settings even on cached score response")
	}

	if len(resp.Settings.PatrolColors) != 2 {
		t.Fatalf("expected 2 patrol colors, got %d", len(resp.Settings.PatrolColors))
	}

	if resp.Settings.PatrolColors["1"] != "red" {
		t.Errorf("expected patrol 1 color 'red', got %q", resp.Settings.PatrolColors["1"])
	}
	if resp.Settings.PatrolColors["2"] != "blue" {
		t.Errorf("expected patrol 2 color 'blue', got %q", resp.Settings.PatrolColors["2"])
	}
}
