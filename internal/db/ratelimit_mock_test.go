package db

import (
	"context"
	"testing"
	"time"
)

func TestMockRateLimiter_DefaultAllow(t *testing.T) {
	mock := NewMockRateLimiter()
	ctx := context.Background()

	result, err := mock.CheckRateLimit(ctx, "test", "key1", 5, time.Minute)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.Allowed {
		t.Error("Expected request to be allowed by default")
	}
}

func TestMockRateLimiter_DenyAll(t *testing.T) {
	mock := NewMockRateLimiter()
	mock.AlwaysAllow = false
	ctx := context.Background()

	result, err := mock.CheckRateLimit(ctx, "test", "key1", 5, time.Minute)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Allowed {
		t.Error("Expected request to be denied")
	}
	if result.Remaining != 0 {
		t.Errorf("Expected Remaining to be 0, got %d", result.Remaining)
	}
	if result.RetryAfter != time.Minute {
		t.Errorf("Expected RetryAfter to be 1 minute, got %v", result.RetryAfter)
	}
}

func TestMockRateLimiter_CustomFunction(t *testing.T) {
	mock := NewMockRateLimiter()
	callCount := 0

	mock.CheckRateLimitFunc = func(ctx context.Context, name, key string, limit int64, window time.Duration) (*RateLimitResult, error) {
		callCount++
		// Allow first call, deny subsequent
		return &RateLimitResult{
			Allowed:    callCount == 1,
			Remaining:  limit - int64(callCount),
			RetryAfter: 0,
		}, nil
	}

	ctx := context.Background()

	// First call should be allowed
	result1, _ := mock.CheckRateLimit(ctx, "test", "key1", 5, time.Minute)
	if !result1.Allowed {
		t.Error("Expected first request to be allowed")
	}

	// Second call should be denied
	result2, _ := mock.CheckRateLimit(ctx, "test", "key1", 5, time.Minute)
	if result2.Allowed {
		t.Error("Expected second request to be denied")
	}
}

func TestMockRateLimiter_CallTracking(t *testing.T) {
	mock := NewMockRateLimiter()
	ctx := context.Background()

	// Make several calls with different keys
	mock.CheckRateLimit(ctx, "auth", "192.168.1.1", 5, time.Minute)
	mock.CheckRateLimit(ctx, "auth", "192.168.1.1", 5, time.Minute)
	mock.CheckRateLimit(ctx, "auth", "192.168.1.2", 5, time.Minute)
	mock.CheckRateLimit(ctx, "api", "192.168.1.1", 10, time.Minute)

	// Verify call counts
	if count := mock.GetCallCount("auth", "192.168.1.1"); count != 2 {
		t.Errorf("Expected 2 calls for auth:192.168.1.1, got %d", count)
	}
	if count := mock.GetCallCount("auth", "192.168.1.2"); count != 1 {
		t.Errorf("Expected 1 call for auth:192.168.1.2, got %d", count)
	}
	if count := mock.GetCallCount("api", "192.168.1.1"); count != 1 {
		t.Errorf("Expected 1 call for api:192.168.1.1, got %d", count)
	}
}

func TestMockRateLimiter_ResetCalls(t *testing.T) {
	mock := NewMockRateLimiter()
	ctx := context.Background()

	// Make some calls
	mock.CheckRateLimit(ctx, "auth", "key1", 5, time.Minute)
	mock.CheckRateLimit(ctx, "auth", "key1", 5, time.Minute)

	if count := mock.GetCallCount("auth", "key1"); count != 2 {
		t.Errorf("Expected 2 calls before reset, got %d", count)
	}

	// Reset
	mock.ResetCalls()

	if count := mock.GetCallCount("auth", "key1"); count != 0 {
		t.Errorf("Expected 0 calls after reset, got %d", count)
	}
}

func TestMockRateLimiter_ResetRateLimit(t *testing.T) {
	mock := NewMockRateLimiter()
	ctx := context.Background()

	// Default reset should not error
	err := mock.ResetRateLimit(ctx, "auth", "key1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Custom reset function
	resetCalled := false
	mock.ResetRateLimitFunc = func(ctx context.Context, name, key string) error {
		resetCalled = true
		return nil
	}

	err = mock.ResetRateLimit(ctx, "auth", "key1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !resetCalled {
		t.Error("Expected custom reset function to be called")
	}
}
