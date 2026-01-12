# Patrol Scores API

## Overview

The Patrol Scores API provides access to patrol competition scores from Online Scout Manager (OSM). The endpoint implements intelligent caching and rate limiting to protect both the client device and the upstream OSM service.

## Endpoint

```
GET /api/v1/patrols
```

## Authentication

All requests must include a device access token in the Authorization header using Bearer authentication:

```http
Authorization: Bearer {device_access_token}
```

The device access token is obtained through the Device OAuth flow (RFC 8628) during device authorization. See the main README for details on the authorization process.

### Authentication Errors

- **401 Unauthorized**: Missing, invalid, or expired device token
  - Response includes `WWW-Authenticate: Bearer realm="API"` header
  - Client should re-initiate the device authorization flow

## Request

### Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Authorization` | Yes | Bearer token with device access token |

### Query Parameters

None. The section and term are automatically determined based on the authorized device's configuration.

## Response

### Success Response (200 OK)

```json
{
  "patrols": [
    {
      "id": "123",
      "name": "Eagles",
      "score": 450
    },
    {
      "id": "456",
      "name": "Hawks",
      "score": 380
    }
  ],
  "from_cache": false,
  "cached_at": "2026-01-12T10:30:00Z",
  "cache_expires_at": "2026-01-12T10:35:00Z",
  "rate_limit_state": "NONE"
}
```

### Response Headers

| Header | Description |
|--------|-------------|
| `X-Cache` | Either `HIT` (served from cache) or `MISS` (fetched fresh from OSM) |
| `Content-Type` | Always `application/json` |

### Response Fields

| Field | Type | Description                                                                                           |
|-------|------|-------------------------------------------------------------------------------------------------------|
| `patrols` | Array | List of patrol scores (see Patrol Object below)                                                       |
| `from_cache` | Boolean | `true` if data was served from cache, `false` if freshly fetched                                      |
| `cached_at` | ISO 8601 Timestamp | When this data was originally cached (now if this is fresh data)                                      |
| `cache_expires_at` | ISO 8601 Timestamp | When the cache expires. Use this to determine when next to poll.                                      |
| `rate_limit_state` | String | Current rate limiting state: `"NONE"`, `"DEGRADED"`, `"USER_TEMPORARY_BLOCK"`, or `"SERVICE_BLOCKED"` |

The rate limit state is used in place of a HTTP Error return when cached data is available.

#### Patrol Object

| Field | Type | Description |
|-------|------|-------------|
| `id` | String | OSM patrol ID (unique within the section) |
| `name` | String | Patrol name (e.g., "Eagles", "Hawks") |
| `score` | Integer | Current patrol competition score (points) |

**Important Notes:**
- Patrols are sorted alphabetically by name for consistent ordering
- Only active patrols with members are included (excludes special groups like "Leaders", "Young Leaders", and empty patrols)
- Scores are as entered in OSM's Patrol Score feature.

## Caching Behavior

The server caches patrol scores to protect the OSM API. A default short lifetime is chosen, but this cache lifetime will
increase as OSM reports usage close to limits for the user.

### User Temporary Blocks (HTTP 429)

If a specific user exceeds OSM's rate limit, the state will become `"USER_TEMPORARY_BLOCK"` and the cache expiry will be set to the time given by OSM for the block to be released (typically 30 minutes).

It will not be possible to return any data if the server cache has expired and any old cache record has been cleared from the database. The server will respond with HTTP 429 in this case.

### Service-Wide Blocks (HTTP 503)

OSM can permanently block the entire application if it repeatedly exceeds request limits across multiple users (detected via the `X-Blocked` header). If this occurs:
- The state will become `"SERVICE_BLOCKED"`
- The service will return cached data for as long as possible (up to 6 hours)
- Once the cache expires and no cached data is available, the service returns HTTP 503
- **This requires manual intervention** by the service administrator to resolve with OSM

### Client Polling Strategy

**The `cache_expires_at` field controls all client polling behavior.** Clients MUST:

1. **Never poll before `cache_expires_at`** - The server will return identical cached data
2. **Poll shortly after `cache_expires_at`** - Add 5-10 seconds to allow for cache expiry
3. **Ignore `rate_limit_state` for polling decisions** - This field is informational only

### 400 Bad Request - Section Not Found

```json
{
  "error": "section_not_found",
  "message": "Section not found in user's profile"
}
```

**Cause:** The configured section ID doesn't exist in the user's OSM profile (may have been deleted or user lost access).

**Resolution:** Device should re-authorize to select a different section.

---

### 409 Conflict - Not In Term

```json
{
  "error": "not_in_term",
  "message": "Section is not currently in an active term"
}
```

**Cause:** The section has no active term that covers the current date. This typically happens:
- Between scout years (summer break)
- If terms haven't been configured in OSM
- If the term end date has passed

**Resolution:**
- Client should display a user-friendly message explaining the section is between terms
- Retry after 24 hours (terms are checked once per 24 hours)
- User may need to configure terms in OSM

---

### 429 Too Many Requests - User Temporarily Blocked

```json
{
  "error": "user_temporary_block",
  "message": "User temporarily blocked due to rate limiting",
  "blocked_until": "2026-01-12T11:00:00Z",
  "retry_after": 1800
}
```

**Headers:**
```http
Retry-After: 1800
```

**Cause:** The specific OSM user account has exceeded OSM's rate limits (user-specific temporary block) and no cached data is available.

**Response Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `blocked_until` | ISO 8601 Timestamp | When the block expires and requests may resume |
| `retry_after` | Integer | Seconds until block expires (matches `Retry-After` header) |

**Resolution:**
- **Clients MUST respect the `Retry-After` header** and not retry before `blocked_until`
- Display a user-friendly countdown timer
- Consider adding extra buffer time (30-60 seconds) for safety
- If the client has previously cached data, display it with a "temporarily unavailable" notice

---

### 503 Service Unavailable - Service Blocked

```json
{
  "error": "service_blocked",
  "message": "Service blocked by OSM"
}
```

**Cause:** The entire service has been blocked by OSM (service-wide block via `X-Blocked` header). This is typically due to repeated violations of rate limits across multiple users. This is a critical situation that requires manual intervention.

**Resolution:**
- **This is a service-wide issue, not user-specific** - All users are affected
- The service will serve cached data for as long as it can, but this will become stale.
- Display a message indicating the service is unavailable – contact the administrator to resolve.
- Retry after an extended period (suggested: 1+ hours)
- If the block persists, contact the service administrator
- The service administrator must investigate and resolve the issue with OSM

**Note:** Unlike 429 (user temporary blocks), this error has no `Retry-After` header since service-wide blocks require manual resolution and have indefinite duration.

## Security Notes

1. **Device access tokens** are isolated from OSM credentials
   - OSM access/refresh tokens are never exposed to clients
   - The service acts as a proxy, making OSM API calls server-side
   - See `docs/security.md` for detailed security architecture

2. **Token Security**
   - Store device access tokens securely on the device
   - Never log or transmit tokens in clear text
   - Tokens are valid until explicitly revoked

3. **HTTPS Required**
   - All production deployments must use HTTPS
   - Device tokens transmitted over plain HTTP are vulnerable to interception

## Related Documentation

- [Main README](../../README.md) - Service overview and deployment
- [Security Architecture](../security.md) - Token isolation and security model
- [Device OAuth Flow](../../README.md#device-flow-for-devices) - Authorization process
