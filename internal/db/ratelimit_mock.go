package db

import (
	"context"
	"time"
)

// MockRateLimiter is a mock implementation of RateLimiter for testing
type MockRateLimiter struct {
	// AlwaysAllow determines if all requests should be allowed (default: true)
	AlwaysAllow bool

	// CheckRateLimitFunc allows custom rate limit logic for testing
	CheckRateLimitFunc func(ctx context.Context, name, key string, limit int64, window time.Duration) (*RateLimitResult, error)

	// ResetRateLimitFunc allows custom reset logic for testing
	ResetRateLimitFunc func(ctx context.Context, name, key string) error

	// Calls tracks the number of times CheckRateLimit was called
	Calls map[string]int
}

// NewMockRateLimiter creates a new mock rate limiter that allows all requests by default
func NewMockRateLimiter() *MockRateLimiter {
	return &MockRateLimiter{
		AlwaysAllow: true,
		Calls:       make(map[string]int),
	}
}

// CheckRateLimit implements RateLimiter interface
func (m *MockRateLimiter) CheckRateLimit(ctx context.Context, name, key string, limit int64, window time.Duration) (*RateLimitResult, error) {
	// Track calls
	callKey := name + ":" + key
	m.Calls[callKey]++

	// Use custom function if provided
	if m.CheckRateLimitFunc != nil {
		return m.CheckRateLimitFunc(ctx, name, key, limit, window)
	}

	// Default behavior
	if m.AlwaysAllow {
		return &RateLimitResult{
			Allowed:    true,
			Remaining:  limit - 1,
			RetryAfter: 0,
		}, nil
	}

	return &RateLimitResult{
		Allowed:    false,
		Remaining:  0,
		RetryAfter: window,
	}, nil
}

// ResetRateLimit implements RateLimiter interface
func (m *MockRateLimiter) ResetRateLimit(ctx context.Context, name, key string) error {
	// Use custom function if provided
	if m.ResetRateLimitFunc != nil {
		return m.ResetRateLimitFunc(ctx, name, key)
	}

	// Default: no-op for mock
	return nil
}

// GetCallCount returns the number of times CheckRateLimit was called for a given name:key
func (m *MockRateLimiter) GetCallCount(name, key string) int {
	callKey := name + ":" + key
	return m.Calls[callKey]
}

// ResetCalls clears the call tracking
func (m *MockRateLimiter) ResetCalls() {
	m.Calls = make(map[string]int)
}

// Ensure MockRateLimiter implements RateLimiter interface
var _ RateLimiter = (*MockRateLimiter)(nil)
