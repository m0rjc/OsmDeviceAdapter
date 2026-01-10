# Patrol Scores Fetcher - Specification

## Overview

This specification describes the implementation of the patrol scores fetcher service that retrieves patrol information from OSM (Online Scout Manager) and provides it to scoreboard devices. The implementation includes active term management, rate limiting, error handling, and caching strategies to ensure reliable and compliant API usage.

## Assumptions

- **Structured logging is in place** - All critical operations, errors, and rate limiting events are logged using Go's `log/slog` package for monitoring and debugging.
- **Prometheus metrics are configured** - Rate limiting, API latency, and error metrics are exposed via Prometheus.
- **One section per device** - Each device is associated with a single section at a time.
- **Security practices enforced** - Following security guidelines in [docs/security.md](./security.md) for credential handling and logging.

## Architecture Components

### 1. Database Schema Changes

#### Device Codes Table Extension

Add the following fields to the `device_codes` table:

```sql
ALTER TABLE device_codes ADD COLUMN user_id INTEGER;
ALTER TABLE device_codes ADD COLUMN term_id INTEGER;
ALTER TABLE device_codes ADD COLUMN term_checked_at TIMESTAMP;
ALTER TABLE device_codes ADD COLUMN term_end_date TIMESTAMP;

CREATE INDEX idx_device_codes_user_id ON device_codes(user_id);
CREATE INDEX idx_device_codes_term_end_date ON device_codes(term_end_date);
```

**Field Descriptions:**
- `user_id`: OSM user ID from the OAuth resource endpoint, used for rate limiting key
- `term_id`: The active term ID for the section
- `term_checked_at`: Timestamp when term information was last fetched from OSM
- `term_end_date`: End date of the current term (from OSM API)

### 2. OSM API Integration

#### 2.1 Active Term Discovery

**Endpoint:** `GET https://www.onlinescoutmanager.co.uk/oauth/resource`

**Process:**
1. Make authenticated request to OAuth resource endpoint
2. Extract `user_id` from response: `data.user_id`
3. Find the section using the stored `section_id`
4. Iterate through `sections[].terms[]` array
5. Find term where `current_date >= startdate AND current_date <= enddate`
6. Store `term_id`, current timestamp as `term_checked_at`, and `enddate` as `term_end_date`

**Error Cases:**
- **No term found**: Return error "Not currently in a term" to device (HTTP 409 Conflict)
- **Section not found**: Return error "Section not configured" (HTTP 400 Bad Request)
- **API error**: Log structured error, return HTTP 502 Bad Gateway

**Term Refresh Strategy:**
- Recheck term on any OSM API error (invalidate cached term)
- Recheck term if `term_checked_at` is older than 24 hours

#### 2.2 Patrol Information Fetching

**Endpoint:** `GET https://www.onlinescoutmanager.co.uk/ext/members/patrols/`

**Parameters:**
- `action=getPatrolsWithPeople`
- `sectionid={section_id}`
- `termid={term_id}`
- `include_no_patrol=y`

**Response Processing:**
1. Parse JSON response (object with patrol IDs as keys)
2. Filter out special patrols:
   - Negative patrol IDs (e.g., "-2", "-3")
   - Patrols with empty `members` arrays
3. Extract regular patrols with:
   - `patrolid`: Patrol ID
   - `name`: Patrol name
   - `points`: Patrol points (string, convert to integer)
4. Sort patrols by name for consistent ordering
5. Return patrol scores array

**Special Keys:**
- `"unallocated"`: Skip this entry (not a patrol)
- Negative IDs: Skip (Leaders, Young Leaders, etc.)

### 3. Rate Limiting & Blocking Detection

#### 3.1 Rate Limit Headers

Monitor these headers on **every** OSM API response as documented in [docs/research/OSM-OAuth-Doc.md](./research/OSM-OAuth-Doc.md):

- `X-RateLimit-Limit`: Maximum requests per hour (per authenticated user)
- `X-RateLimit-Remaining`: Requests remaining before being blocked
- `X-RateLimit-Reset`: Seconds until the rate limit resets

**Implementation Reference:** See `internal/osm/client.go:274-352` for header parsing implementation.

#### 3.2 Rate Limiting Storage

**Prometheus Metrics** (already implemented in `internal/metrics/metrics.go`):
- `osm_rate_limit_total{user_id}`: Maximum requests per period
- `osm_rate_limit_remaining{user_id}`: Requests remaining before blocking
- `osm_rate_limit_reset_seconds{user_id}`: Seconds until rate limit resets

**Redis Cache** (for outbound request throttling to OSM):

**Key Format:** `osm_ratelimit:{user_id}`

**Stored Data Structure (JSON):**
```json
{
  "limit": 1000,
  "remaining": 950,
  "reset_at": "2026-01-08T15:30:00Z",
  "last_updated": "2026-01-08T14:30:00Z"
}
```

**Update Strategy:**
- Update both Prometheus metrics and Redis cache after every OSM API call
- Log warning when `remaining < 100` (see logging implementation below)
- Log critical alert when `remaining < 20` (see logging implementation below)

#### 3.3 Cache TTL Adjustment for Rate Limiting

**Dynamic Cache TTL Calculation:**

```go
func calculateCacheTTL(remaining, limit int) time.Duration {
    baselineTTL := 5 * time.Minute

    if remaining < 20 {
        // Critical: 30 minute cache
        return 30 * time.Minute
    } else if remaining < 100 {
        // Warning: 15 minute cache
        return 15 * time.Minute
    } else if remaining < 200 {
        // Caution: 10 minute cache
        return 10 * time.Minute
    }

    return baselineTTL
}
```

**Logging:**
- Log cache TTL changes with structured logging
- Include: user_id, remaining requests, new TTL, reason

#### 3.4 HTTP 429 - Too Many Requests

**Response Headers:**
- `Retry-After`: Seconds until retry is allowed

**Action:**
1. Log critical error (structured)
2. Store blocking info in Redis: `osm_blocked:{user_id}`
3. Set TTL to `Retry-After` duration
4. If we have cached scores then use them, but include in the response notification that this is stale.
   Otherwise return HTTP 429 to device with same `Retry-After` header
5. All subsequent requests for this user should immediately return 429 until Redis key expires

**Blocked User Storage:**
```json
{
  "blocked_at": "2026-01-08T14:30:00Z",
  "retry_after": 3600,
  "reason": "rate_limit_exceeded"
}
```

#### 3.5 X-Blocked Header Detection

**CRITICAL:** **Global service block** - The entire application has been blocked by OSM.

This is **NOT per-user**. When OSM sends the `X-Blocked` header, it means the application's client credentials have been blocked, affecting all users.

**Action:**
1. Log **CRITICAL** alert (structured, high priority) with `severity: "CRITICAL"`
2. Store global block in Redis: `osm_service_blocked` (no expiry, no user_id)
3. Update Prometheus metric: `osm_service_blocked` to 1
4. Increment counter: `osm_block_events_total`
5. **STOP ALL REQUESTS TO OSM IMMEDIATELY** (all users affected)
6. Return HTTP 503 Service Unavailable to all devices with message: "Service blocked by OSM"
7. Require manual intervention to unblock (operator must investigate and resolve with OSM)

**Global Block Storage (Redis):**
```json
{
  "blocked_at": "2026-01-08T14:30:00Z",
  "blocked_header_value": "<value from X-Blocked header>",
  "reason": "osm_blocked_header",
  "impact": "all_users",
  "requires_manual_resolution": true
}
```

**Implementation Reference:** See `internal/osm/client.go:100-116` for X-Blocked header detection and `internal/metrics/metrics.go:23-31` for blocking metrics.

**Recovery Process:**
1. Investigate cause of blocking (check logs for invalid requests, rate limit violations)
2. Contact OSM support if necessary
3. Manually remove Redis key `osm_service_blocked` after resolution
4. Reset Prometheus metric `osm_service_blocked` to 0

#### 3.6 X-Deprecated Header Detection

**Action:**
1. Log warning (structured)
2. Parse deprecation date from header
3. Store in monitoring system for alerting
4. Continue normal operation

### 4. Caching Strategy

#### 4.1 Cache Keys

**Patrol Scores Cache:**
- Key: `patrol_scores:{device_code}`
- **Two-Tier TTL Strategy:**
  - **Valid Until Time** (5-30 minutes, dynamic based on rate limiting): Stored in cache data, determines when we attempt to refresh
  - **Redis TTL** (8 days, configurable via `CACHE_FALLBACK_TTL`): How long Redis retains data for emergency fallback during blocking
  - **Rationale:** Scouts meet weekly, so even week-old scores are better than no scores if OSM blocks the service
- Format: JSON with patrol scores array, cached_at timestamp, and valid_until timestamp
- **Example Cache Data:**
  ```json
  {
    "patrols": [...],
    "cached_at": "2026-01-08T14:30:00Z",
    "valid_until": "2026-01-08T14:35:00Z"
  }
  ```
  (Redis TTL: 8 days, but considered stale after 5-30 minutes)
- **Note:** This is optimized for low-volume, weekly scout meetings. High-volume deployments would need different caching strategies.

**Term Information Cache:**
- Key: `osm_term:{section_id}`
- TTL: Until term end date (max 24 hours)
- Format: JSON with term_id, end_date

**OSM Rate Limit Cache (per-user):**
- Key: `osm_ratelimit:{user_id}`
- TTL: From `X-RateLimit-Reset` header
- Format: JSON with limit, remaining, reset_at

**OSM Temporary Block Cache (per-user, HTTP 429):**
- Key: `osm_blocked:{user_id}`
- TTL: From `Retry-After` header
- Format: JSON with blocked_at, retry_after

**OSM Service Block Cache (global, X-Blocked header):**
- Key: `osm_service_blocked`
- TTL: No expiry (requires manual removal)
- Format: JSON with blocked_at, reason, impact
- **Note:** This is a global block affecting all users, not per-user

#### 4.2 Cache Invalidation

**Invalidate patrol scores cache when:**
- Term end date is reached (automatic via TTL)
- OSM API returns error (any 4xx/5xx)
- Term ID changes

**Invalidate term cache when:**
- Term end date is reached
- OSM API returns error
- Manual refresh requested (admin endpoint)

### 5. Error Handling

#### 5.1 Error Categories

**1. Not In Term (HTTP 409 Conflict)**
- No active term found for current date
- Response: `{"error": "not_in_term", "message": "Section is not currently in an active term"}`

**2. Section Not Configured (HTTP 400 Bad Request)**
- Device has no section_id stored
- Response: `{"error": "section_not_configured", "message": "Device has not selected a section"}`

**3. Rate Limited (HTTP 429 Too Many Requests)**
- Temporarily blocked due to rate limit
- Include `Retry-After` header
- Response: `{"error": "rate_limited", "message": "Too many requests, retry after {seconds} seconds"}`

**4. Blocked (HTTP 403 Forbidden)**
- Permanently blocked by OSM
- Response: `{"error": "blocked", "message": "Authorization revoked by OSM"}`

**5. OSM API Error (HTTP 502 Bad Gateway)**
- OSM returned error or timeout
- Response: `{"error": "upstream_error", "message": "Failed to fetch data from OSM"}`

**6. Invalid Data (HTTP 500 Internal Server Error)**
- Unexpected response format
- Log structured error with response body
- Response: `{"error": "internal_error", "message": "Failed to process OSM response"}`

#### 5.2 Structured Logging

All errors must be logged with structured fields using Go's `log/slog` package:

```go
slog.Error("patrol_fetcher.fetch_failed",
    "component", "patrol_fetcher",
    "event", "fetch.error",
    "user_id", userID,
    "section_id", sectionID,
    "term_id", termID,
    "device_code_hash", deviceCodeHash, // Truncated hash only, never full code
    "error", err.Error(),
    "http_status", statusCode,
    "rate_limit_remaining", remaining,
    "severity", "ERROR",
)
```

**Log Levels (slog):**
- `slog.LevelInfo`: Successful operations, cache hits/misses
- `slog.LevelWarn`: Rate limit approaching (< 100), term expiring soon
- `slog.LevelError`: Rate limit critical (< 20), API errors, invalid data, cache failures
- **Custom CRITICAL**: Blocked by OSM (X-Blocked header), service-wide failures

**Security Note:** Following [docs/security.md](./security.md), never log:
- Full device codes (use truncated hash: first 8 chars only)
- OSM access tokens
- OSM refresh tokens
- Client secrets

**Implementation Reference:** See `internal/osm/client.go:49-60,90-140,327-351` for structured logging examples.

### 6. Implementation Flow

#### 6.1 Patrol Scores Request Flow

```
1. Device requests: GET /api/v1/patrols
   Header: Authorization: Bearer {device_code}

2. Validate device code (existing logic)

3. Check if service is globally blocked by OSM
   - Redis key: osm_service_blocked
   - Prometheus metric: osm_service_blocked
   - If exists: Return HTTP 503 Service Unavailable (affects all users)

4. Check if user is temporarily blocked (HTTP 429 from OSM)
   - Redis key: osm_blocked:{user_id}
   - If blocked AND cached scores exist:
     * Return cached scores with metadata: from_cache=true, cached_at timestamp
     * Include warning in response about stale data
   - If blocked AND no cache:
     * Return HTTP 429 with Retry-After header

5. Check patrol scores cache (normal operation)
   - Redis key: patrol_scores:{device_code}
   - If hit: Return cached data

6. Check if term needs refresh
   - If term_end_date < now + 24h OR term_checked_at < now - 24h:
     - Fetch term from OAuth resource endpoint
     - Update device_codes: user_id, term_id, term_checked_at, term_end_date
     - Update rate limit info in Redis

7. If no term_id or term expired:
   - Fetch term from OAuth resource endpoint
   - If no term found: Return HTTP 409
   - Store term info

8. Fetch patrol scores from OSM
   - Include section_id and term_id in request
   - Monitor response headers: X-RateLimit-*, X-Blocked, X-Deprecated
   - Update rate limit info in Redis

9. Process response
   - Check for X-Blocked header: Store permanent block
   - Check for X-Deprecated header: Log warning
   - Check for HTTP 429: Store temporary block
   - Parse patrol data
   - Filter special patrols

10. Calculate cache TTL based on rate limits

11. Store in cache with dynamic TTL

12. Return patrol scores to device
```

#### 6.2 Database Schema Migration

```go
// Migration: Add term and user tracking to device_codes
func MigrateAddTermTracking(db *gorm.DB) error {
    return db.Exec(`
        ALTER TABLE device_codes
        ADD COLUMN IF NOT EXISTS user_id INTEGER,
        ADD COLUMN IF NOT EXISTS term_id INTEGER,
        ADD COLUMN IF NOT EXISTS term_checked_at TIMESTAMP,
        ADD COLUMN IF NOT EXISTS term_end_date TIMESTAMP;

        CREATE INDEX IF NOT EXISTS idx_device_codes_user_id
        ON device_codes(user_id);

        CREATE INDEX IF NOT EXISTS idx_device_codes_term_end_date
        ON device_codes(term_end_date);
    `).Error
}
```

### 7. API Response Formats

#### 7.1 Success Response

All successful responses include cache and rate limit state metadata:

```json
{
  "patrols": [
    {"id": "72699", "name": "Eagles", "score": 32},
    {"id": "72700", "name": "Lions", "score": 30},
    {"id": "132322", "name": "Wolves", "score": 47}
  ],
  "from_cache": true,
  "cached_at": "2026-01-08T14:30:00Z",
  "cache_expires_at": "2026-01-08T14:35:00Z",
  "rate_limit_state": "NONE",
}
```

**Rate Limit State Values:**
- `NONE`: Normal operation (remaining > 200). 
- `DEGRADED`: Rate limit approaching (remaining < 200), cache TTL extended
- `BLOCKED`: Temporarily blocked by OSM (HTTP 429), serving stale cache

The client should wait until after the cache_expires_at timestamp before making another request. This is true regardless
of the rate_limit_state value.

The possible situations are:

* Fresh data: Client requested data after the cache has expired, so data is fresh.
  - `from_cache: false`
  - `cached_at: NOW`
  - `cache_expires_at: NOW + TTL`
  - `rate_limit_state: Either NONE or DEGRADED`
* Stale data: Client requested data before the cache has expired,
  - `from_cache: true`
  - `cached_at: Whenever it was cached`
  - `cache_expires_at: Whenever it will expire`
  - `rate_limit_state: NONE, DEGRADED or BLOCKED`

The only way we can have cached data and a BLOCKED rate limit state is if the REDIS cache TTL is longer than the
time for which we use cached data. We will have to make a request to OSM to receive the X-Blocked header, and will only
make that request if the cache has "expired". We need to store the valid until time in the cache data, but use a
longer REDIS TTL to have a chance to get stale scores. If the cache data's valid until time is in the past then we
make a request to OSM. This leaves one more situation

* No cached data and BLOCKED: Cached data has expired in REDIS, so we make a request to OSM and receive the X-Blocked header.
   - This is an error situation. All we can do is return a HTTP Retry After response based on OSM's Retry-After header.

#### 7.2 Error Responses

**Not In Term:**
```json
{
  "error": "not_in_term",
  "message": "Section is not currently in an active term"
}
```

**Rate Limited:**
```json
{
  "error": "rate_limited",
  "message": "Too many requests, retry after 3600 seconds",
  "retry_after": 3600
}
```

**Blocked:**
```json
{
  "error": "blocked",
  "message": "Authorization revoked by OSM"
}
```

### 8. Configuration

#### Environment Variables

```bash
# Cache configuration
CACHE_FALLBACK_TTL=192h  # 8 days - how long to keep stale data for emergency use
                         # Optimized for weekly scout meetings

# Rate limiting thresholds (for dynamic cache TTL)
RATE_LIMIT_CAUTION=200    # Start extending cache TTL
RATE_LIMIT_WARNING=100    # Extend cache TTL further
RATE_LIMIT_CRITICAL=20    # Maximum cache TTL extension
```

**Note:** These values are optimized for low-volume, weekly scout meeting scenarios. High-volume deployments would need different configuration.

### 9. Testing Strategy

#### 9.1 Unit Tests

- Term discovery logic
- Patrol data filtering (remove special patrols)
- Cache TTL calculation (including two-tier strategy)
- Rate limit header parsing
- Error response formatting
- Rate limit state determination

#### 9.2 Integration Tests

- Full patrol fetch flow with mocked OSM API
- Cache hit/miss scenarios with two-tier TTL
- Rate limiting triggers (NONE → DEGRADED → BLOCKED states)
- Block detection and handling (both HTTP 429 and X-Blocked header)
- Term expiration handling
- Stale cache serving during blocking

#### 9.3 Load Tests

- Concurrent device requests
- Cache effectiveness under load
- Rate limiting behavior
- Redis performance

### 10. Monitoring & Alerts

#### 10.1 Prometheus Metrics (Already Implemented)

From `internal/metrics/metrics.go`:
- `osm_rate_limit_total{user_id}`: Maximum requests per period
- `osm_rate_limit_remaining{user_id}`: Requests remaining before blocking
- `osm_rate_limit_reset_seconds{user_id}`: Seconds until rate limit resets
- `osm_service_blocked`: Global service block status (0=ok, 1=blocked)
- `osm_block_events_total`: Counter of blocking events
- `osm_api_request_duration_seconds{endpoint, status_code}`: API latency histogram

#### 10.2 Additional Metrics to Add

- Patrol fetch success rate
- Cache hit rate (patrol scores)
- Cache age when served (histogram)
- Rate limit state distribution (NONE/DEGRADED/BLOCKED)
- Error rates by type

#### 10.3 Alerts

**Critical:**
- `osm_service_blocked == 1` (X-Blocked header detected - affects all users)
- `osm_rate_limit_remaining < 10` (any user)
- Multiple users in BLOCKED state simultaneously

**Warning:**
- `osm_rate_limit_remaining < 100` (any user - DEGRADED state)
- Cache miss rate > 50%
- OSM API errors > 5% of requests
- Serving stale cache (valid_until in the past) for > 1 hour

**Info:**
- Term approaching expiration (< 7 days)
- X-Deprecated header detected
- Rate limit state transitions

### 11. Security Considerations

Following [docs/security.md](./security.md):

1. **User Isolation**: Rate limiting is per-user (HTTP 429) to prevent one user affecting others. However, X-Blocked header is a global service block.
2. **Input Validation**: Validate section_id and term_id before sending to OSM to prevent injection attacks
3. **Response Validation**: Verify OSM response structure before processing to detect tampering or API changes
4. **Token Security**: Never log full access tokens, refresh tokens, or device codes - use truncated hashes only (first 8 chars)
5. **Block Enforcement**:
   - HTTP 429: Temporarily block user, serve stale cache if available
   - X-Blocked: Immediately stop ALL OSM API calls for all users (global block)
6. **Logging Security**: Use slog structured logging, include only safe data (user_id, device_code_hash, not tokens)

### 12. Future Enhancements

1. **Multi-section Support**: Allow devices to switch between authorized sections, ideally automatically based on meeting schedule

## Implementation Checklist

**Database & Core Logic:**
- [ ] Database migration: Add user_id, term_id, term_checked_at, term_end_date
- [ ] Implement term discovery from OAuth resource endpoint
- [ ] Implement patrol information fetching from OSM API with proper filtering

**Rate Limiting & Blocking:**
- [x] Add rate limit header monitoring and Prometheus metrics (`internal/osm/client.go:274-352`)
- [ ] Implement Redis storage for rate limit data (`osm_ratelimit:{user_id}`)
- [ ] Implement dynamic cache TTL calculation based on rate limit state
- [ ] Add HTTP 429 handling with temporary blocking and stale cache serving
- [x] Add X-Blocked header detection with global service blocking (`internal/osm/client.go:100-116`)
- [ ] Add X-Deprecated header detection and logging

**Caching:**
- [ ] Implement two-tier caching strategy (valid_until + 8-day Redis TTL)
- [ ] Store cache metadata (cached_at, valid_until) with patrol scores
- [ ] Add rate_limit_state to API responses (NONE/DEGRADED/BLOCKED)
- [ ] Add configuration for CACHE_FALLBACK_TTL environment variable

**Logging & Security:**
- [x] Implement slog structured logging for all operations (`internal/osm/client.go`)
- [x] Ensure no tokens/secrets are logged (following `docs/security.md`)
- [ ] Add severity levels to all log entries
- [ ] Log only truncated device code hashes (first 8 chars)

**Testing:**
- [ ] Write unit tests for core logic (cache TTL, rate limit parsing)
- [ ] Write integration tests for full flow including blocking scenarios
- [ ] Test two-tier cache strategy with stale data serving
- [ ] Add monitoring metrics and alerts
- [ ] Update API documentation
- [ ] Update README with new functionality

## References

**OSM API Documentation:**
- [OSM OAuth Documentation](./research/OSM-OAuth-Doc.md) - Official rate limiting headers and API guidelines
- [Captured Payloads](./research/CapturedPayloads.md) - Example API responses for testing

**Internal Documentation:**
- [Security Architecture](./security.md) - Security practices, credential handling, logging guidelines
- [Observability Implementation](./OBSERVABILITY_IMPLEMENTATION.md) - Structured logging and monitoring patterns

**Implementation Files:**
- `internal/osm/client.go` - OSM API client with rate limiting and blocking detection
- `internal/metrics/metrics.go` - Prometheus metrics definitions
- `internal/config/config.go` - Configuration management

**External Resources:**
- [RFC 6749: OAuth 2.0](https://datatracker.ietf.org/doc/html/rfc6749) - OAuth 2.0 specification
- [Prometheus Best Practices](https://prometheus.io/docs/practices/naming/) - Metric naming conventions
