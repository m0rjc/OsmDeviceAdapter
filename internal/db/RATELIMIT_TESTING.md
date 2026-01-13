# Rate Limiting Mock for Testing

This document explains how to use the mock rate limiter for testing components that depend on rate limiting functionality.

## Overview

The `MockRateLimiter` provides a simple way to test rate-limited functionality without requiring Redis or miniredis. It implements the `RateLimiter` interface and can be configured to allow/deny requests or use custom logic.

## Basic Usage

### Default Behavior (Allow All Requests)

```go
func TestMyHandler(t *testing.T) {
    // Create a mock that allows all requests by default
    mockRateLimiter := db.NewMockRateLimiter()

    conns := db.NewConnections(database, nil)
    conns.RateLimiter = mockRateLimiter

    // Use conns in your test...
}
```

### Deny All Requests

```go
func TestRateLimitExceeded(t *testing.T) {
    mockRateLimiter := db.NewMockRateLimiter()
    mockRateLimiter.AlwaysAllow = false // Deny all requests

    conns := db.NewConnections(database, nil)
    conns.RateLimiter = mockRateLimiter

    // Test rate limit exceeded behavior...
}
```

### Custom Rate Limit Logic

```go
func TestCustomRateLimiting(t *testing.T) {
    mockRateLimiter := db.NewMockRateLimiter()
    requestCount := 0

    // Allow first 3 requests, then deny
    mockRateLimiter.CheckRateLimitFunc = func(ctx context.Context, name, key string, limit int64, window time.Duration) (*db.RateLimitResult, error) {
        requestCount++
        allowed := requestCount <= 3
        remaining := int64(3 - requestCount)
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
                return time.Minute
            }(),
        }, nil
    }

    conns := db.NewConnections(database, nil)
    conns.RateLimiter = mockRateLimiter

    // Test behavior...
}
```

## Call Tracking

The mock tracks how many times `CheckRateLimit` was called for each name:key combination:

```go
func TestCallTracking(t *testing.T) {
    mockRateLimiter := db.NewMockRateLimiter()

    // Make some requests...
    mockRateLimiter.CheckRateLimit(ctx, "auth", "192.168.1.1", 5, time.Minute)
    mockRateLimiter.CheckRateLimit(ctx, "auth", "192.168.1.1", 5, time.Minute)

    // Verify call count
    callCount := mockRateLimiter.GetCallCount("auth", "192.168.1.1")
    if callCount != 2 {
        t.Errorf("Expected 2 calls, got %d", callCount)
    }

    // Reset call tracking if needed
    mockRateLimiter.ResetCalls()
}
```

## Example Test Cases

See `internal/handlers/device_oauth_ratelimit_test.go` for complete examples:

- `TestDeviceAuthorizeHandler_RateLimitExceeded` - Testing rate limit denial
- `TestDeviceAuthorizeHandler_RateLimitCustomBehavior` - Custom rate limit logic
- `TestDeviceTokenHandler_SlowDown` - Testing slow_down OAuth error
- `TestMockRateLimiter_CallTracking` - Verifying rate limiter was called

## RateLimiter Interface

The `RateLimiter` interface is defined in `internal/db/ratelimit.go`:

```go
type RateLimiter interface {
    CheckRateLimit(ctx context.Context, name, key string, limit int64, window time.Duration) (*RateLimitResult, error)
    ResetRateLimit(ctx context.Context, name, key string) error
}
```

Both `RedisClient` and `MockRateLimiter` implement this interface, allowing seamless substitution in tests.

## Production vs Testing

- **Production**: Uses `RedisClient` which implements real rate limiting with Redis
- **Testing**: Uses `MockRateLimiter` for fast, isolated tests without external dependencies

The `Connections` struct automatically uses `Redis` as the rate limiter when creating connections with `NewConnections()`. In tests, you can override this by setting `conns.RateLimiter` to a mock.
