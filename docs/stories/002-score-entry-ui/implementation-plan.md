# Score Entry UI - Implementation Plan

This document outlines the implementation phases for the Score Entry UI feature as specified in `specification.md`.

---

## Phase 1: Database Schema & Models ✓

### 1.1 Web Sessions Table ✓
- [x] Add `WebSession` model to `internal/db/models.go`
- [x] Fields: id (UUID), osm_user_id, osm_access_token, osm_refresh_token, osm_token_expiry, csrf_token, selected_section_id, created_at, last_activity, expires_at
- [x] GORM AutoMigrate will create the table

### 1.2 Web Session Store ✓
- [x] Create `internal/db/web_session_store.go`
- [x] CRUD operations: Create, GetByID, Update, Delete, DeleteExpired
- [x] Update last_activity on access (sliding expiration)

### 1.3 Score Audit Log Table ✓
- [x] Add `ScoreAuditLog` model to `internal/db/models.go`
- [x] Fields: id, osm_user_id, section_id, patrol_id, patrol_name, previous_score, new_score, points_added, created_at
- [x] Create `internal/db/score_audit_store.go` with CRUD and cleanup operations

### 1.4 OSM Users Table (optional - for preferences)
- Deferred - not needed for MVP

---

## Phase 2: Token Holder Interface & Refactoring ✓

### 2.1 Extract TokenHolder Interface ✓
- [x] Created interface in `internal/types/types.go`:
  ```go
  type TokenHolder interface {
      GetOSMAccessToken() string
      GetOSMRefreshToken() string
      GetOSMTokenExpiry() time.Time
      GetIdentifier() string
  }
  ```

### 2.2 Implement on DeviceCode ✓
- [x] Added interface methods to existing `DeviceCode` model
- [x] Handles nullable pointer fields appropriately

### 2.3 Implement on WebSession ✓
- [x] Added interface methods to `WebSession` model
- [x] Added `User()` method for consistency with DeviceCode

---

## Phase 3: Admin OAuth Flow ✓

### 3.1 OAuth State Management ✓
- [x] Using Redis for OAuth state storage (simpler than DB, native TTL support)
- [x] Key format: `admin_oauth_state:{state}` with 15-minute TTL
- [x] Implemented in `internal/handlers/admin_oauth.go`

### 3.2 Admin Login Handler ✓
- [x] Created `internal/handlers/admin_oauth.go`
- [x] `GET /admin/login`: Generate state, redirect to OSM with `scope=section:member:write`
- [x] Uses separate callback URL (`/admin/callback`) from device flow

### 3.3 Admin Callback Handler ✓
- [x] `GET /admin/callback`: Exchange code for tokens
- [x] Create WebSession record with UUID session ID
- [x] Set secure session cookie (`osm_admin_session`)
- [x] Redirect to `/admin/` (SPA)

### 3.4 Admin Logout Handler ✓
- [x] `GET /admin/logout`: Clear session from DB and cookie
- [x] Redirect to home page
- [x] Token revocation not possible (OSM has no revocation endpoint)

### 3.5 Register Callback URL with OSM
- Document that `/admin/callback` must be registered as additional callback URL in OSM OAuth app settings

---

## Phase 4: Session Middleware & Authentication ✓

### 4.1 Session Cookie Middleware ✓
- [x] Created `internal/middleware/session.go`
- [x] Extract session ID from cookie
- [x] Load WebSession from database
- [x] Attach session and user to request context
- [x] Handle expired/invalid sessions (clear cookie, return 401)
- [x] Async update of last_activity for sliding expiration

### 4.2 CSRF Middleware ✓
- [x] Validate `X-CSRF-Token` header on POST/PUT/DELETE/PATCH
- [x] Compare against session's csrf_token
- [x] Return 403 on mismatch

### 4.3 Token Refresh Integration ✓
- [x] Created `internal/webauth/service.go` for web session token refresh
- [x] Check token expiry on each authenticated request (5-minute threshold)
- [x] Update session record with new tokens
- [x] Handle revoked tokens (delete session)

---

## Phase 5: Admin API Endpoints ✓

### 5.1 Session Endpoint ✓
- [x] `GET /api/admin/session`
- [x] Returns authentication status, user info, selected section, CSRF token
- [x] Used by SPA on load to check auth state

### 5.2 Sections Endpoint ✓
- [x] `GET /api/admin/sections`
- [x] Fetch sections from OSM where user has access
- [x] Return list with id, name, groupName

### 5.3 Get Scores Endpoint ✓
- [x] `GET /api/admin/sections/{sectionId}/scores`
- [x] Validate user has access to section
- [x] Fetch patrol scores from OSM (fresh data)
- [x] Return section info, patrol list with scores, timestamp

### 5.4 Update Scores Endpoint ✓
- [x] `POST /api/admin/sections/{sectionId}/scores`
- [x] Validate CSRF token via `X-CSRF-Token` header
- [x] Validate user has access to section
- [x] Read current scores from OSM
- [x] Apply increments (points added to current score)
- [x] Write updated scores to OSM
- [x] Create audit log entries
- [x] Return updated patrol scores with previous/new values

### 5.5 OSM Client Extensions ✓
- [x] Created `internal/osm/patrol_scores_update.go`
- [x] `UpdatePatrolScore()` method for writing scores to OSM
- [x] POST to `/ext/members/patrols/?action=updatePatrolPoints&sectionid={id}`

---

## Phase 6: React SPA Setup ✓

### 6.1 Project Structure ✓
- [x] Created `web/admin/` directory
- [x] Initialized with Vite + React + TypeScript
- [x] Configured build output to `web/admin/dist/`
- [x] Set base path to `/admin/` in vite.config.ts

### 6.2 Build Integration ✓
- [x] Added npm scripts for dev/build
- [x] Updated Makefile with `ui-build`, `ui-dev`, `ui-clean` targets
- [x] `make build` now builds frontend before Go binary
- [x] Added `make build-server` for backend-only builds

### 6.3 SPA Shell Serving ✓
- [x] Created `web/admin/assets.go` with embed directive
- [x] Created `internal/admin/handler.go` with SPA handler
- [x] Handler serves `index.html` for unmatched `/admin/*` paths
- [x] OAuth routes (`/admin/login`, `/admin/callback`, `/admin/logout`) take precedence

### 6.4 Docker Build ✓
- [x] Updated Dockerfile with Node.js build stage
- [x] Frontend built in separate stage, copied to Go build stage
- [x] Single binary with embedded SPA assets

---

## Phase 7: React SPA Implementation ✓

### 7.1 Router Setup ✓
- [x] React Router with routes:
  - `/admin/` - redirect to scores or login
  - `/admin/scores` - main score entry page

### 7.2 Auth Context ✓
- [x] Call `/api/admin/session` on load
- [x] Store auth state, user info, CSRF token
- [x] Redirect to `/admin/login` if not authenticated

### 7.3 Section Selector Component ✓
- [x] Dropdown for section selection (if multiple sections)
- [x] Fetch sections from `/api/admin/sections`
- [x] Store selected section in context

### 7.4 Score Entry Component ✓
- [x] Display patrol list with current scores
- [x] Number input for each patrol (range -1000 to +1000)
- [x] Visual feedback for positive/negative values

### 7.5 Action Buttons ✓
- [x] Refresh: Reload scores from API
- [x] Clear: Reset all inputs to 0
- [x] Add Scores: Show confirmation dialog, submit changes

### 7.6 Confirmation Dialog ✓
- [x] Show patrols with non-zero changes
- [x] Display patrol name and points delta
- [x] Cancel/Confirm buttons

### 7.7 Error Handling & Loading States ✓
- [x] Toast notifications for success/error
- [x] Loading spinners during API calls
- [x] Inline validation errors

---

## Phase 8: PWA Capabilities

### 8.1 Web App Manifest
- Create `manifest.json` with app name, icons, theme
- Configure standalone display mode

### 8.2 Service Worker
- Cache static assets (JS, CSS, images)
- Offline shell support
- Use Workbox or similar for simplicity

### 8.3 HTTPS
- Already provided via Cloudflare (no action needed)

---

## Phase 9: Security Hardening

### 9.1 Security Headers
- Add headers to admin routes:
  - Content-Security-Policy
  - X-Content-Type-Options: nosniff
  - X-Frame-Options: DENY
  - Referrer-Policy: strict-origin-when-cross-origin

### 9.2 Cookie Security
- HttpOnly, Secure, SameSite=Lax flags
- Appropriate expiry (configurable, default 7 days)

### 9.3 Input Validation
- Server-side validation of all inputs
- Points range validation (-1000 to +1000)
- Section access validation

---

## Phase 10: Testing

### 10.1 Unit Tests
- Web session store CRUD operations
- Token holder interface implementations
- Score update logic

### 10.2 Integration Tests
- OAuth flow (mock OSM responses)
- API endpoints with session auth
- CSRF validation

### 10.3 Frontend Tests
- Component tests for score entry
- Auth flow tests

---

## Phase 11: Documentation & Deployment

### 11.1 Update CLAUDE.md
- Document new endpoints
- Document new database tables
- Update architecture section

### 11.2 Update README
- Add admin UI section
- Document OAuth callback registration requirement

### 11.3 Helm Chart Updates
- Add any new environment variables
- Update values documentation

### 11.4 Home Page Link
- Add "Admin Login" link to home page

---

## Implementation Order Recommendation

For incremental delivery, suggested order:

1. **Phases 1-2**: Database foundation and token abstraction
2. **Phases 3-4**: OAuth flow and session management (testable via curl)
3. **Phase 5**: Admin API (testable via curl/Postman)
4. **Phase 6**: SPA scaffolding
5. **Phase 7**: SPA implementation
6. **Phase 8**: PWA (can be done in parallel with Phase 7)
7. **Phases 9-11**: Hardening, testing, documentation

Each phase can be merged independently, allowing for iterative review and testing.

---

## Caching Strategy: User Sections & Terms

### Problem
When a user logs in, we need their accessible sections (and section terms) to:
- Populate the section dropdown
- Validate section access on API calls
- Know which term to use for score operations

This data comes from OSM and could change mid-session (e.g., leader added to new section).

### Approach: Redis Cache with TTL + Refresh Trigger

**Storage:**
- Key: `user_sections:{osm_user_id}`
- Value: JSON with sections list, terms, and fetch timestamp
- TTL: 24 hours (configurable) - long TTL is appropriate since section membership changes are rare

**Fetch Logic:**
1. Check Redis for cached sections
2. If cache hit → use cached data
3. If cache miss → fetch from OSM, update cache

**Staleness Mitigation:**
- **Manual**: "Refresh sections" option in UI forces cache invalidation and refetch (for rare cases where membership changed)
- **On error**: If OSM returns 403 for a section, invalidate cache and refetch
- **On login**: Refresh cache if older than TTL

**Rate Limiting Consideration:**
Long TTL preferred to minimise OSM API calls. Section membership rarely changes, and manual refresh handles edge cases.

**Data Structure:**
```json
{
  "sections": [
    {
      "id": "12345",
      "name": "Incas Scouts",
      "terms": [
        {"id": "t1", "name": "Spring 2024", "startDate": "2024-01-01", "endDate": "2024-04-01", "current": true},
        {"id": "t2", "name": "Summer 2024", "startDate": "2024-04-01", "endDate": "2024-07-01", "current": false}
      ]
    }
  ],
  "fetchedAt": "2024-01-15T10:30:00Z"
}
```

**Term Selection:**
- Use the current term (based on date range) for score operations
- If no current term, show warning to user

### Alternative Considered: Store in WebSession
- Simpler (no Redis lookup)
- But: data goes stale for long-lived sessions (7 days)
- Would require logout/login to refresh
- Rejected in favour of Redis cache

### Implementation Notes
- Add `internal/cache/user_sections.go` for cache operations
- Cache invalidation helper for manual refresh
- Consider prefixing keys with existing `REDIS_KEY_PREFIX`

---

## Open Items / Research Needed

- [x] OSM API endpoint for writing patrol scores (verified: POST `/ext/members/patrols/?action=updatePatrolPoints`)
- [x] OSM API endpoint for fetching user's sections (using existing `FetchOSMProfile`)
- [x] OSM token revocation not available (tested /oauth/revoke, /oauth2/revoke, /oauth/token/revoke - all 404)
- [x] Session cookie name: `osm_admin_session` (implemented)
- [x] Session duration: 7 days (implemented in `AdminSessionDuration` constant)
- [x] Audit log retention period (14 days default, configurable via --audit-retention flag in cleanup job)
