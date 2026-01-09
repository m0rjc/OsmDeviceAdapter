# Patrol Scores Fetcher - Specification

## Overview

This specification describes the implementation of the patrol scores fetcher service that retrieves patrol information from OSM (Online Scout Manager) and provides it to scoreboard devices. The implementation includes active term management, rate limiting, error handling, and caching strategies to ensure reliable and compliant API usage.

## Assumptions

- **Structured logging is in place** - All critical operations, errors, and rate limiting events will be logged using structured logging for monitoring and debugging.
- **One section per device** - Each device is associated with a single section at a time.

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

Monitor these headers on **every** OSM API response:

- `X-RateLimit-Limit`: Maximum requests per hour (per user)
- `X-RateLimit-Remaining`: Requests remaining
- `X-RateLimit-Reset`: Seconds until rate limit resets

#### 3.2 Rate Limiting Storage (Redis)

**Key Format:** `ratelimit:{user_id}`

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
- Update rate limit info after every OSM API call
- Log warning when `remaining < 100` (structured log)
- Log critical alert when `remaining < 20` (structured log)

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
2. Store blocking info in Redis: `blocked:{user_id}`
3. Set TTL to `Retry-After` duration
4. If we have cached scores then user them, but include in the response notification that this is stale. 
   Otherwise Return HTTP 429 to device with same `Retry-After` header
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

**Critical:** Application has been blocked by OSM.

**Action:**
1. Log **CRITICAL** alert (structured, high priority)
2. Store permanent block in Redis: `perm_blocked` (no expiry)
4. **STOP all requests to OSM immediately**
5. Return HTTP 403 Forbidden to device with message: "Authorization revoked by OSM"
6. Require manual intervention to unblock (operator must investigate and resolve)

**Permanent Block Storage:**
```json
{
  "blocked_at": "2026-01-08T14:30:00Z",
  "reason": "osm_blocked_header",
  "requires_manual_resolution": true
}
```

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
- TTL: Dynamic based on rate limiting (5-30 minutes)
- Format: JSON string of patrol scores array

**Term Information Cache:**
- Key: `term_info:{section_id}`
- TTL: Until term end date (max 24 hours)
- Format: JSON with term_id, end_date

**Rate Limit Cache:**
- Key: `ratelimit:{user_id}`
- TTL: From `X-RateLimit-Reset` header
- Format: JSON with limit, remaining, reset_at

**Blocked User Cache:**
- Key: `blocked:{user_id}`
- TTL: From `Retry-After` header
- Format: JSON with blocked_at, retry_after

**Permanent Block Cache:**
- Key: `perm_blocked:{user_id}`
- TTL: No expiry
- Format: JSON with blocked_at, reason

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

All errors must be logged with structured fields:

```go
log.WithFields(map[string]interface{}{
    "component": "patrol_fetcher",
    "user_id": userID,
    "section_id": sectionID,
    "term_id": termID,
    "device_code": deviceCode,
    "error": err.Error(),
    "http_status": statusCode,
    "rate_limit_remaining": remaining,
}).Error("Failed to fetch patrol scores")
```

**Log Levels:**
- `INFO`: Successful operations, cache hits/misses
- `WARN`: Rate limit approaching, term expiring soon
- `ERROR`: API errors, invalid data, cache failures
- `CRITICAL`: Blocked by OSM, permanent blocks

### 6. Implementation Flow

#### 6.1 Patrol Scores Request Flow

```
1. Device requests: GET /api/v1/patrols
   Header: Authorization: Bearer {device_code}

2. Validate device code (existing logic)

3. Check if user is permanently blocked
   - Redis key: perm_blocked:{user_id}
   - If exists: Return HTTP 403

4. Check if user is temporarily blocked
   - Redis key: blocked:{user_id}
   - If exists: Return HTTP 429 with Retry-After

5. Check patrol scores cache
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

```json
{
  "patrols": [
    {"id": "72699", "name": "Eagles", "score": 32},
    {"id": "72700", "name": "Lions", "score": 30},
    {"id": "132322", "name": "Wolves", "score": 47}
  ],
  "cached_at": "2026-01-08T14:30:00Z",
  "expires_at": "2026-01-08T14:35:00Z"
}
```

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

### 8. Testing Strategy

#### 8.1 Unit Tests

- Term discovery logic
- Patrol data filtering (remove special patrols)
- Cache TTL calculation
- Rate limit header parsing
- Error response formatting

#### 8.2 Integration Tests

- Full patrol fetch flow with mocked OSM API
- Cache hit/miss scenarios
- Rate limiting triggers
- Block detection and handling
- Term expiration handling

#### 8.3 Load Tests

- Concurrent device requests
- Cache effectiveness under load
- Rate limiting behavior
- Redis performance

### 9. Monitoring & Alerts

#### 9.1 Metrics to Track

- Patrol fetch success rate
- Cache hit rate
- Rate limit remaining (per user)
- OSM API latency
- Error rates by type
- Block occurrences

#### 9.2 Alerts

**Critical:**
- X-Blocked header detected
- Rate limit remaining < 10
- Multiple users blocked

**Warning:**
- Rate limit remaining < 100
- Cache miss rate > 50%
- OSM API errors > 5% of requests

**Info:**
- Term approaching expiration
- X-Deprecated header detected

### 10. Security Considerations

1. **User Isolation**: Rate limiting is per-user to prevent one user affecting others
2. **Input Validation**: Validate section_id and term_id before sending to OSM
3. **Response Validation**: Verify OSM response structure before processing
4. **Token Security**: Never log access tokens in structured logs
5. **Block Enforcement**: Immediately stop API calls when blocked

### 11. Future Enhancements

1. **Multi-section Support**: Allow devices to switch between authorized sections
2. **Patrol History**: Track patrol scores over time
3. **Predictive Rate Limiting**: Learn usage patterns and optimize cache TTL
4. **Admin Dashboard**: View rate limits, blocks, and system health
5. **Webhook Support**: Real-time updates when patrol scores change in OSM

## Implementation Checklist

- [ ] Database migration: Add user_id, term_id, term_checked_at, term_end_date
- [ ] Implement term discovery from OAuth resource endpoint
- [ ] Implement patrol information fetching from OSM API
- [ ] Add rate limit header monitoring and storage
- [ ] Implement dynamic cache TTL calculation
- [ ] Add HTTP 429 handling with temporary blocking
- [ ] Add X-Blocked header detection with permanent blocking
- [ ] Add X-Deprecated header detection and logging
- [ ] Implement structured logging for all operations
- [ ] Add error handling for all error categories
- [ ] Write unit tests for core logic
- [ ] Write integration tests for full flow
- [ ] Add monitoring metrics and alerts
- [ ] Update API documentation
- [ ] Update README with new functionality

## References

- [OSM OAuth Documentation](../docs/OSM-OAuth-Doc.md)
- [Captured Payloads](../docs/research/CapturedPayloads.md)
- [OSM Rate Limiting Best Practices](https://www.onlinescoutmanager.co.uk/api/)
