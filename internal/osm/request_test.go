package osm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mockStore struct {
	serviceBlocked   bool
	userBlocked      map[int]bool
	userBlockedUntil map[int]time.Time
	latencies        []latencyRecord
	rateLimits       []rateLimitRecord
}

type latencyRecord struct {
	endpoint   string
	statusCode int
	latency    time.Duration
}

type rateLimitRecord struct {
	userId            *int
	limitRemaining    int
	limitTotal        int
	limitResetSeconds int
}

func (m *mockStore) MarkOsmServiceBlocked(ctx context.Context)    { m.serviceBlocked = true }
func (m *mockStore) IsOsmServiceBlocked(ctx context.Context) bool { return m.serviceBlocked }
func (m *mockStore) MarkUserTemporarilyBlocked(ctx context.Context, userId int, blockedUntil time.Time) {
	if m.userBlocked == nil {
		m.userBlocked = make(map[int]bool)
	}
	if m.userBlockedUntil == nil {
		m.userBlockedUntil = make(map[int]time.Time)
	}
	m.userBlocked[userId] = true
	m.userBlockedUntil[userId] = blockedUntil
}
func (m *mockStore) GetUserBlockEndTime(ctx context.Context, userId int) time.Time {
	if m.userBlockedUntil == nil {
		return time.Time{}
	}
	return m.userBlockedUntil[userId]
}
func (m *mockStore) RecordOsmLatency(endpoint string, statusCode int, latency time.Duration) {
	m.latencies = append(m.latencies, latencyRecord{endpoint, statusCode, latency})
}
func (m *mockStore) RecordRateLimit(userId *int, limitRemaining int, limitTotal int, limitResetSeconds int) {
	m.rateLimits = append(m.rateLimits, rateLimitRecord{userId, limitRemaining, limitTotal, limitResetSeconds})
}

type mockUser struct {
	userId int
	token  string
}

func (m mockUser) UserID() *int {
	return &m.userId
}

func (m mockUser) AccessToken() string {
	return m.token
}

func newMockUser(userId int, token string) mockUser {
	return mockUser{userId, token}
}

func TestClient_Request(t *testing.T) {
	t.Run("successful request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		store := &mockStore{}
		client := NewClient(server.URL, store, store, nil)

		var target map[string]string
		resp, err := client.Request(context.Background(), http.MethodGet, &target, WithPath("/test"), WithUser(newMockUser(1, "test-token")))

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		if target["status"] != "ok" {
			t.Errorf("expected status ok, got %s", target["status"])
		}
		if len(store.latencies) != 1 {
			t.Errorf("expected 1 latency record, got %d", len(store.latencies))
		}
	})

	t.Run("service blocked in store", func(t *testing.T) {
		store := &mockStore{serviceBlocked: true}
		client := NewClient("http://osm.local", store, store, nil)

		_, err := client.Request(context.Background(), http.MethodGet, nil, WithPath("/test"))
		if err != ErrServiceBlocked {
			t.Errorf("expected ErrServiceBlocked, got %v", err)
		}
	})

	t.Run("user blocked in store", func(t *testing.T) {
		blockTime := time.Now().Add(1 * time.Hour)
		store := &mockStore{
			userBlocked:      map[int]bool{1: true},
			userBlockedUntil: map[int]time.Time{1: blockTime},
		}
		client := NewClient("http://osm.local", store, store, nil)

		_, err := client.Request(context.Background(), http.MethodGet, nil, WithPath("/test"), WithUser(newMockUser(1, "utoken")))
		var blockedErr *ErrUserBlocked
		if err == nil || !errors.As(err, &blockedErr) {
			t.Errorf("expected ErrUserBlocked, got %v", err)
		}
	})

	t.Run("detect service block from header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Blocked", "Too many invalid requests")
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		store := &mockStore{}
		client := NewClient(server.URL, store, store, nil)

		_, err := client.Request(context.Background(), http.MethodGet, nil, WithPath("/test"))
		if err == nil || !strings.Contains(err.Error(), "OSM service blocked") {
			t.Errorf("expected service block error, got %v", err)
		}
		if !store.serviceBlocked {
			t.Error("expected store to be marked as service blocked")
		}
	})

	t.Run("detect user block from 429", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		store := &mockStore{}
		client := NewClient(server.URL, store, store, nil)

		_, err := client.Request(context.Background(), http.MethodGet, nil, WithPath("/test"), WithUser(newMockUser(1, "utoken")))
		var blockedErr *ErrUserBlocked
		if err == nil || !errors.As(err, &blockedErr) {
			t.Errorf("expected ErrUserBlocked, got %v", err)
		}
		if !store.userBlocked[1] {
			t.Error("expected user1 to be marked as blocked in store")
		}
	})

	t.Run("redact sensitive info in logs", func(t *testing.T) {
		// This test doesn't check the actual log output but verifies the flow
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("secret key was leaked"))
		}))
		defer server.Close()

		store := &mockStore{}
		client := NewClient(server.URL, store, store, nil)

		_, err := client.Request(context.Background(), http.MethodPost, nil, WithPath("/oauth/token"))
		if err == nil {
			t.Fatal("expected error for 400 response")
		}
		if strings.Contains(err.Error(), "secret key") {
			t.Error("error message should not contain sensitive body")
		}
		if !strings.Contains(err.Error(), "[REDACTED]") {
			t.Error("error message should contain [REDACTED]")
		}
	})

	t.Run("withUser uses user token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "Bearer user-token" {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
		}))
		defer server.Close()

		store := &mockStore{}
		client := NewClient(server.URL, store, store, nil)

		_, err := client.Request(context.Background(), http.MethodGet, nil, WithPath("/test"), WithUser(newMockUser(1, "user-token")))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}
