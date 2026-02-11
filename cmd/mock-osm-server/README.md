# Mock OSM Server

A development server that emulates the real OSM (Online Scout Manager) OAuth and API endpoints for end-to-end testing of the adapter without real OSM credentials.

## Features

- Full OAuth authorization code flow (authorize, token exchange, refresh)
- User profile endpoint with sections and terms
- Patrol scores fetch and update
- OSM-style rate limiting with `X-RateLimit-*` headers
- Service block simulation via `X-Blocked` header
- Auto-approve mode for automated testing
- Token prefixes (`mock_code_`, `mock_at_`, `mock_rt_`) for easy identification in logs
- Filtering edge cases: negative-ID patrols (Leaders), `"unallocated"` key, empty-member patrols

## Usage

### Standalone Mock OSM

```bash
make mock-osm
```

Server runs on http://localhost:8082

### With Adapter (End-to-End)

```bash
make dev-e2e
```

Starts both mock OSM server (8082) and the adapter (8080) with auto-approve enabled. Requires `DATABASE_URL` and `REDIS_URL`.

### Connect Any Adapter Instance

```bash
OSM_DOMAIN=http://localhost:8082 \
OSM_CLIENT_ID=mock-client-id \
OSM_CLIENT_SECRET=mock-client-secret \
make run
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `PORT` | `8082` | Server port |
| `MOCK_RATE_LIMIT` | `100` | Requests per rate limit window |
| `MOCK_RATE_LIMIT_WINDOW` | `3600` | Rate limit window in seconds |
| `MOCK_SERVICE_BLOCKED` | `false` | Simulate `X-Blocked` header on all requests |
| `MOCK_TOKEN_EXPIRY` | `3600` | Access token TTL in seconds |
| `MOCK_CLIENT_ID` | `mock-client-id` | Expected OAuth client ID |
| `MOCK_CLIENT_SECRET` | `mock-client-secret` | Expected OAuth client secret |
| `MOCK_AUTO_APPROVE` | `false` | Skip authorization page (redirect immediately) |

### Testing Rate Limiting

```bash
# Low rate limit for testing
MOCK_RATE_LIMIT=3 make mock-osm
```

After 3 requests, the server returns HTTP 429 with `Retry-After` header and rate limit headers, matching real OSM behavior.

### Testing Service Block

```bash
MOCK_SERVICE_BLOCKED=true make mock-osm
```

All authenticated API requests receive the `X-Blocked` header, triggering the adapter's `ErrServiceBlocked` path.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/oauth/authorize` | GET | Renders HTML authorization page |
| `/oauth/authorize` | POST | Processes grant/deny, redirects with code or error |
| `/oauth/token` | POST | Token exchange (`authorization_code`) and refresh (`refresh_token`) |
| `/oauth/resource` | GET | User profile with sections and terms (Bearer auth) |
| `/ext/members/patrols/` | GET | Patrol scores (`action=getPatrolsWithPeople`, Bearer auth) |
| `/ext/members/patrols/` | POST | Update patrol score (`action=updatePatrolPoints`, Bearer auth) |
| `/health` | GET | Health check |

## Mock Data

**User:**
- User ID: 12345
- Name: "Mock User"
- Email: mock@example.com

**Sections:**
- 1001: "1st Anytown Scouts" (1st Anytown Group) - Term 5001
- 1002: "2nd Anytown Scouts" (2nd Anytown Group) - Term 5002

**Patrols (Section 1001):**
- 101: Eagles (42 points)
- 102: Hawks (38 points)
- 103: Owls (45 points)
- -1: Leaders (edge case: negative ID, filtered by adapter)
- -2: Young Leaders (edge case: negative ID, filtered by adapter)
- 199: Empty Patrol (edge case: no members, filtered by adapter)
- unallocated: Unallocated (edge case: special key, filtered by adapter)

**Patrols (Section 1002):**
- 201: Panthers (51 points)
- 202: Tigers (47 points)
- 203: Wolves (49 points)
- -3: Leaders (edge case)
- unallocated: Unallocated (edge case)

## OAuth Flow

1. **GET `/oauth/authorize`** - Shows authorization page (or auto-redirects if `MOCK_AUTO_APPROVE=true`)
2. **POST `/oauth/authorize`** - User approves/denies; generates auth code and redirects
3. **POST `/oauth/token`** with `grant_type=authorization_code` - Exchanges code for access + refresh tokens
4. **POST `/oauth/token`** with `grant_type=refresh_token` - Refreshes token pair

Auth codes expire after 60 seconds and cannot be reused. Token refresh with a revoked token returns 401 (triggers `ErrAccessRevoked` in adapter).

## Differences from Real OSM

- Single mock user (no multi-user support)
- All sections/terms are returned for any authenticated user
- No scope enforcement (scope is accepted but not checked on API calls)
- Simplified rate limiting (per-token, not per-user-account)
