# Score Entry User Interface

## Problem Statement

Editing scores using the OSM interface is error prone and has resulted in corruption. It would be nice if we had
a simple UI to add (or remove) score from a patrol.

## Overview

The aim is for an admin UI. This will present each patrol name with the current score.
Below this is a number input field which can be used to enter the points to add.
A submit button sends the added points to the patrols and resets the inputs.
A refresh button allows the user to update the current score display at any time.

The most common form factor for this is mobile phone.

Something like (if there are multiple sections then allow change of section. I've shown a dropdown here.)

```
Incas Scouts  \/
============

Eagles                 20
                    [  0]

Lions                  35
                    [  0]

Tigers                 25
                    [  0]

[Refresh]  [Clear]  [Add Scores]
```

---

## Architectural Decisions

### Decision 1: OAuth Callback Strategy

The existing device authorization flow uses `/oauth/callback`. We need to decide how to handle the web admin OAuth callback.

**Option A: Separate Callback (`/admin/callback`)** ← Recommended

- Register a second callback URL in OSM (same client ID, additional callback)
- Dedicated handler with clear separation of concerns
- Device flow creates `DeviceCode` records; admin flow creates `WebSession` records
- Different redirect destinations after success (device flow → section selection for device; admin flow → score entry page)
- Easier to test and reason about
- Same OAuth client ID/secret can be reused; only the callback URL and requested scopes differ

**Option B: Shared Callback with State Parameter**

- Single `/oauth/callback` handler for both flows
- Encode flow type in the OAuth state parameter (e.g., `device:<session_id>` vs `admin:<session_id>`)
- Handler branches based on decoded state
- Fewer routes, single callback registration
- Risk: State parameter becomes overloaded (CSRF protection + flow routing)
- Note: Cannot distinguish by scope since OSM doesn't echo requested scopes in the callback

**Recommendation**: Option A. The flows have different lifecycles and destinations. Separate handlers are cleaner.
Both flows can share the same OAuth client credentials - just register both callback URLs with the client.

---

### Decision 2: API Authentication Strategy

The existing `/api/v1/patrols` endpoint uses `Authorization: Bearer <device_token>` for device authentication. The web admin UI needs API access for score reading and writing.

**Option A: Separate Admin API (`/api/admin/...`)** ← Recommended for this story

- Dedicated endpoints under `/api/admin/` prefix
- Cookie-based authentication with CSRF protection
- Clear separation: devices use `/api/v1/`, web users use `/api/admin/`
- Can evolve independently
- No changes to existing device APWI

**Option B: Dual Authentication on Shared Endpoints**

- Single set of endpoints accepts both Bearer tokens and session cookies
- Auth middleware checks for Bearer header first, then falls back to cookie
- CSRF validation only applies to cookie-authenticated requests
- DRY: same business logic for both clients
- More complex auth middleware

**Option C: Unified API with Device Write Scopes (Future)**

- Extend device authorization to include optional write scopes
- Devices could update scores directly via `/api/v1/patrols` with POST
- Requires: scope model for devices, UI for granting write access during device auth
- **Out of scope for this story** - noted as future expansion

**Recommendation**: Option A for this story. Keep the admin API separate. This avoids changes to the stable device API and simplifies authentication logic. Option C is a natural future evolution but requires its own story for scope management.

---

### Decision 3: Score Update API Location

Given the above, where should the score update endpoint live?

**For this story**: Use `/api/admin/sections/{sectionId}/scores` (POST) with cookie auth.

**Future consideration**: If devices gain write capability, we could:
1. Add POST to `/api/v1/sections/{sectionId}/scores` with Bearer auth
2. Or have devices call the admin endpoint with a device-specific auth mechanism

This is deferred to the device write scopes story.

---

## UI Architecture

### Hybrid Approach

The site uses two rendering strategies:

| Area | Technology | Rationale |
|------|------------|-----------|
| Home page, device pages | Server-rendered HTML (Go templates) | Lightweight, simple, works everywhere |
| Admin area (`/admin/*`) | React SPA | Rich UX, PWA support, mobile-first |

This keeps device-facing pages fast and simple while providing a modern experience for the admin UI.

### Admin SPA Structure

```
/admin/                    → React SPA entry point (Go serves index.html)
/admin/scores              → Score entry (client-side route)
/admin/login               → Initiates OAuth (server redirect, returns to SPA)
/admin/callback            → OAuth callback (server, redirects to /admin/)

/api/admin/*               → JSON API consumed by SPA
```

The Go server:
1. Serves the React SPA shell for any `/admin/*` route (except `/admin/login`, `/admin/callback`, `/admin/logout`)
2. Handles OAuth endpoints server-side
3. Provides JSON API at `/api/admin/*`

### PWA Capabilities

The admin SPA can be installed as a Progressive Web App:

- **Web App Manifest**: App name, icons, theme colour, standalone display mode
- **Service Worker**: Cache static assets, enable offline shell
- **HTTPS**: Already provided via Cloudflare

**Scope for this story**: Basic PWA (installable, cached assets). Offline score entry with sync is a future enhancement.

### Frontend Stack

| Tool | Purpose |
|------|---------|
| React | UI framework |
| React Router | Client-side routing |
| Redux | State management (if needed - may start without) |
| Vite | Build tooling, dev server |
| TypeScript | Type safety |

**Build output**: Static files in `web/admin/dist/`, embedded in Go binary or served from filesystem.

---

## User Flows

### Flow 1: First-Time Login

1. User visits home page and clicks "Admin Login" link
2. System redirects to `/admin/login` which initiates OAuth Web Flow
3. User is redirected to OSM login page
4. User authenticates with OSM credentials and grants "section members write" permission
5. OSM redirects back to `/admin/callback` with authorization code
6. System exchanges code for OSM tokens and creates a web session
7. System sets secure session cookie and redirects to section selection (if multiple sections) or score entry page

### Flow 2: Returning User (Valid Session)

1. User visits `/admin/scores` with valid session cookie
2. System validates session and checks OSM token validity
3. If token near expiry, system refreshes token automatically
4. User sees score entry page with current patrol scores loaded

### Flow 3: Session Expired or Invalid

1. User visits `/admin/scores` with expired/invalid session cookie
2. System detects invalid session and clears cookie
3. System redirects to `/admin/login` to restart OAuth flow
4. User re-authenticates (may be automatic if OSM session still valid)

### Flow 4: Score Entry

1. User views current scores for all patrols in selected section
2. User enters point values in input fields (positive to add, negative to subtract)
3. User clicks "Add Scores" button
4. System shows confirmation dialog: "Add scores to patrols?"
5. On confirm, system submits scores and shows loading indicator
6. On success, inputs reset to 0 and updated scores display
7. On failure, error message shown and inputs preserved

### Flow 5: Section Switching

1. User clicks section dropdown (visible only if user has access to multiple sections)
2. User selects different section from list
3. System loads patrol scores for new section
4. Previous input values are cleared

### Flow 6: Manual Refresh

1. User clicks "Refresh" button
2. System fetches current scores from OSM (bypassing cache)
3. Updated scores display; any unsaved input values are preserved with a warning

---

## Security Model

### Session Management

- **Session Storage**: Server-side sessions stored in `web_sessions` database table
- **Session Cookie**: HTTP-only, Secure, SameSite=Lax cookie containing session ID
- **Session Lifetime**: Configurable, default 7 days. Sliding expiration from last activity. Tradeoff: longer is convenient (covers a camp), shorter reduces risk if left logged in on another device.
- **Session Contents**: Links to OSM user ID, OSM access/refresh tokens, selected section ID

### CSRF Protection

- All state-changing operations (POST/PUT/DELETE) require CSRF token
- CSRF token stored in session, validated on each request
- Token delivered via:
  - Hidden form field for traditional forms
  - `X-CSRF-Token` header for AJAX requests
  - Meta tag in HTML for JavaScript access

### Authorization Model

```
┌─────────────────────────────────────────────────────┐
│                    Web Session                       │
│  ┌───────────────┐    ┌─────────────────────────┐   │
│  │ Session ID    │───▶│ OSM User ID             │   │
│  │ (cookie)      │    │ OSM Access Token        │   │
│  │               │    │ OSM Refresh Token       │   │
│  │               │    │ Token Expiry            │   │
│  │               │    │ Selected Section ID     │   │
│  └───────────────┘    └─────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

- User must have OSM "section members write" permission for the selected section
- Section access list fetched from OSM and cached in session
- Score updates validated server-side: user must have write access to target section

### Security Headers

All admin pages should include:
- `Content-Security-Policy`: Restrict script sources
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`

---

## API Design

### Endpoints

#### `GET /admin/login`
Initiates OAuth Web Flow for admin login.

**Response**: 302 redirect to OSM OAuth authorization URL

---

#### `GET /admin/callback`
OAuth callback handler.

**Query Parameters**:
- `code`: Authorization code from OSM
- `state`: CSRF state parameter

**Success Response**: 302 redirect to `/admin/scores`

**Error Response**: 302 redirect to `/admin/login?error=<code>`

---

#### `GET /admin/logout`
Terminates user session.

**Response**: 302 redirect to home page with session cookie cleared

**Logout Behaviour**:

OAuth 2.0 core doesn't define logout. Related standards:
- **RFC 7009 (Token Revocation)**: Endpoint to revoke access/refresh tokens
- **OpenID Connect**: Has logout specs, but OSM uses OAuth 2.0 not OIDC

Options for our implementation:

| Option | Behaviour | Pros | Cons |
|--------|-----------|------|------|
| **1. Local only** | Clear `WebSession` and cookie | Simple, reliable | OSM tokens remain valid until expiry |
| **2. Revoke tokens** | Local cleanup + POST to OSM revocation endpoint | Cleaner, tokens invalidated immediately | OSM may not support RFC 7009 (undocumented) |

**Recommendation**: Implement Option 1 as baseline. Attempt Option 2 if OSM supports it - test whether
`POST /oauth/revoke` or similar endpoint exists. Fail gracefully to Option 1 if revocation unavailable.

---

#### `GET /admin/*` (SPA shell)
Serves the React SPA for any admin route not handled by other endpoints.

**Response**: HTML shell (`index.html`) containing the React application

**Note**: Authentication is checked client-side via API calls. If session invalid, SPA redirects to `/admin/login`.

---

#### `GET /api/admin/session`
Returns current session info. Used by SPA on load to check authentication.

**Request Headers**:
- `Cookie`: Session cookie (required)

**Success Response** (200):
```json
{
  "authenticated": true,
  "user": {
    "osmUserId": "12345",
    "name": "Joe Leader"
  },
  "selectedSectionId": "67890",
  "csrfToken": "abc123..."
}
```

**Error Response** (401):
```json
{
  "authenticated": false
}
```

The SPA calls this on load. If `authenticated: false`, redirect to `/admin/login`.

---

#### `GET /api/admin/sections`
Returns list of sections the user has write access to.

**Request Headers**:
- `Cookie`: Session cookie (required)

**Success Response** (200):
```json
{
  "sections": [
    {"id": "12345", "name": "Incas Scouts"},
    {"id": "12346", "name": "Incas Cubs"}
  ]
}
```

**Error Responses**:
- `401 Unauthorized`: Invalid or missing session
- `403 Forbidden`: User has no sections with write access

---

#### `GET /api/admin/sections/{sectionId}/scores`
Returns current patrol scores for a section.

**Request Headers**:
- `Cookie`: Session cookie (required)

**Success Response** (200):
```json
{
  "section": {
    "id": "12345",
    "name": "Incas Scouts"
  },
  "patrols": [
    {"id": "1", "name": "Eagles", "score": 20},
    {"id": "2", "name": "Lions", "score": 35},
    {"id": "3", "name": "Tigers", "score": 25}
  ],
  "lastUpdated": "2024-01-15T10:30:00Z"
}
```

**Error Responses**:
- `401 Unauthorized`: Invalid or missing session
- `403 Forbidden`: User lacks access to this section
- `404 Not Found`: Section does not exist

---

#### `POST /api/admin/sections/{sectionId}/scores`
Adds points to patrol scores (incremental update).

**Request Headers**:
- `Cookie`: Session cookie (required)
- `X-CSRF-Token`: CSRF token (required)
- `Content-Type: application/json`

**Request Body**:
```json
{
  "updates": [
    {"patrolId": "1", "points": 5},
    {"patrolId": "2", "points": -3},
    {"patrolId": "3", "points": 0}
  ]
}
```

**Success Response** (200):
```json
{
  "success": true,
  "patrols": [
    {"id": "1", "name": "Eagles", "previousScore": 20, "newScore": 25},
    {"id": "2", "name": "Lions", "previousScore": 35, "newScore": 32},
    {"id": "3", "name": "Tigers", "previousScore": 25, "newScore": 25}
  ]
}
```

**Error Responses**:
- `400 Bad Request`: Invalid request body or validation error
- `401 Unauthorized`: Invalid or missing session
- `403 Forbidden`: User lacks write access to this section or invalid CSRF token
- `404 Not Found`: Section or patrol does not exist
- `429 Too Many Requests`: Rate limited by OSM
- `502 Bad Gateway`: OSM API error

**Error Response Body**:
```json
{
  "error": "validation_error",
  "message": "Points value out of range",
  "details": {
    "patrolId": "1",
    "field": "points",
    "constraint": "must be between -1000 and 1000"
  }
}
```

---

### Error Codes

| Code | Meaning |
|------|---------|
| `session_expired` | Session has expired, re-login required |
| `session_invalid` | Session is invalid or corrupted |
| `csrf_invalid` | CSRF token missing or invalid |
| `access_denied` | User lacks permission for this operation |
| `validation_error` | Request body failed validation |
| `osm_error` | Error communicating with OSM API |
| `osm_rate_limited` | OSM rate limit exceeded |

---

## UI/UX Details

### Input Validation

**Points Input Field**:
- Type: Number input with step="1"
- Range: -1000 to +1000 (prevents accidental large values)
- Default: 0
- Client-side validation: Reject non-numeric input immediately
- Server-side validation: Same constraints enforced

**Visual Feedback**:
- Positive values: Green text or highlight
- Negative values: Red text or highlight
- Zero: Neutral/grey styling

### Confirmation Dialogs

**Before Submit**:
```
┌────────────────────────────────────────┐
│         Add Scores?                    │
│                                        │
│  Eagles:    +5                         │
│  Lions:     -3                         │
│                                        │
│        [Cancel]    [Confirm]           │
└────────────────────────────────────────┘
```

Only show patrols with non-zero changes.

### Loading States

- **Initial Load**: Skeleton UI with pulsing placeholders
- **Submitting**: Button shows spinner, inputs disabled, overlay prevents interaction
- **Refreshing**: Spinner on refresh button, scores greyed out

### Error Messages

**Toast Notifications** (auto-dismiss after 5 seconds):
- Success: "Scores updated successfully" (green)
- Warning: "Scores may have changed. Please review." (amber)
- Error: "Failed to update scores. Please try again." (red)

**Inline Errors** (persistent until resolved):
- Field-level validation errors shown below input
- Form-level errors shown in alert banner at top

### Empty and Edge States

- **No Patrols**: "No patrols found in this section"
- **No Sections**: "You don't have write access to any sections"
- **Offline**: "Unable to connect. Check your internet connection."

### Mobile Responsiveness

**Breakpoints**:
- Mobile: < 640px (primary target)
- Tablet: 640px - 1024px
- Desktop: > 1024px

**Mobile Optimizations**:
- Full-width inputs with large touch targets (min 44px height)
- Sticky header with section selector
- Sticky footer with action buttons
- Native number keyboard on input focus (inputmode="numeric")
- Swipe gestures disabled (prevent accidental navigation)

### Accessibility

- **ARIA Labels**: All inputs labeled, buttons have accessible names
- **Focus Management**: Logical tab order, visible focus indicators
- **Screen Reader**: Live regions announce updates and errors
- **Keyboard Navigation**: All actions accessible via keyboard
- **Color Contrast**: WCAG AA compliant (4.5:1 minimum)
- **Reduced Motion**: Respect `prefers-reduced-motion` media query

---

## Data Model

### Web Sessions Table

```sql
CREATE TABLE web_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    osm_user_id VARCHAR(255) NOT NULL,
    osm_access_token TEXT NOT NULL,
    osm_refresh_token TEXT NOT NULL,
    osm_token_expiry TIMESTAMP NOT NULL,
    csrf_token VARCHAR(64) NOT NULL,
    selected_section_id VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_web_sessions_expiry ON web_sessions(expires_at);
CREATE INDEX idx_web_sessions_user ON web_sessions(osm_user_id);
```

### OSM Users Table (for preferences)

```sql
CREATE TABLE osm_users (
    osm_user_id VARCHAR(255) PRIMARY KEY,
    preferred_section_id VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Score Audit Log Table

```sql
CREATE TABLE score_audit_log (
    id BIGSERIAL PRIMARY KEY,
    osm_user_id VARCHAR(255) NOT NULL,
    section_id VARCHAR(255) NOT NULL,
    patrol_id VARCHAR(255) NOT NULL,
    patrol_name VARCHAR(255) NOT NULL,
    previous_score INTEGER NOT NULL,
    new_score INTEGER NOT NULL,
    points_added INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_score_audit_section ON score_audit_log(section_id, created_at);
CREATE INDEX idx_score_audit_user ON score_audit_log(osm_user_id, created_at);
```

**Scope**: This story only logs changes made via our UI. Entries created for each non-zero score change.

**Cleanup**: Add to existing cleanup job - expire audit entries after 14 days (configurable).

**Future story - upstream change detection**: Detecting changes made directly in OSM is more complex:
- Would need to compare cached vs fresh scores on each poll
- Risk of double-logging changes made via our UI
- Race condition if scoreboard polls during our read-update-write
- Would need to mark entries with source ("ui" vs "upstream") to deduplicate

Defer upstream detection to a separate story.

---

## Authentication Integration

The home page will have a new link to allow administration. This will direct to an OAuth Web Flow allowing them
to log in via OSM. They then create a Session in our system.

We should record their OSM user ID so that we can support preferences. An OSM user has Sessions.

We should use secure cookies pointing to session records in the database, which themselves have the OSM tokens.

### Token Holder Interface

The OSM Client and OAuth client code needs to be able to work with either `WebSession` records or `DeviceCode` records.
Both hold OSM tokens that may need refreshing. Extract a common interface:

```go
// TokenHolder represents any entity that holds OSM OAuth tokens
type TokenHolder interface {
    GetOSMAccessToken() string
    GetOSMRefreshToken() string
    GetOSMTokenExpiry() time.Time
    SetOSMTokens(accessToken, refreshToken string, expiry time.Time) error
}
```

Both `DeviceCode` and `WebSession` implement this interface. The OSM client's token refresh logic can then work
with either type transparently.

### Required OAuth Scopes

OAuth scopes are requested dynamically in the authorization redirect, not fixed at client registration. The admin
flow must request **section members write** access, which is broader than the device flow (read-only).

When initiating the OAuth redirect from `/admin/login`, the authorization URL must include:

```
scope=section:member:write
```

Per OSM documentation, scope format is `section:{scope_name}:{permission_level}`. The `member` scope covers
"Personal details, adding/removing/transferring members" which includes patrol scores.

This means we can potentially reuse the same OAuth client ID/secret for both flows, just with different:
- Requested scopes (read-only for devices, read+write for admin)
- Callback URLs (if using separate callbacks)
- State parameter encoding (to route back to the correct flow)

**Note**: If using a shared callback (Option B from Decision 1), the scope difference doesn't help distinguish
flows since OSM doesn't echo the requested scope back. The state parameter remains the primary routing mechanism.

# Services

The update is incremental. This means that we have to load the current scores (not from the cache), perform the addition,
then write the scores. The OSM web app does a full reload after this. We can do the same before returning the new scores
to the client. The newly reloaded scores then enter the cache for the device to read when it next polls based on the 
existing cache lifecycle logic.

---

## Open Questions

Items requiring investigation or decisions during implementation:

1. **OSM Token Revocation**: Does OSM support RFC 7009 token revocation? Test for `/oauth/revoke` or similar endpoint. *(Test during implementation, graceful fallback if unavailable)*

2. ~~**OSM Write Scope String**~~: **Resolved** - `section:member:write` per OSM documentation.

3. **Concurrent Sessions**: Allow multiple active web sessions per OSM user. Use case: leader logs in on their phone and a young leader's phone during an event.

   **Risk analysis** (logging in on someone else's device):

   | Risk | Severity | Mitigation |
   |------|----------|------------|
   | Left logged into OSM browser session | **High** - full OSM access | User education; out of our control |
   | Left logged into our score entry app | **Low** - can only affect scores | Audit logging; session expiry; logout button |

   Messing up scores is recoverable if we audit. Audit log should capture: who, when, which patrols, old/new values.

   **Future story**: Explore delegated access or PIN-based session sharing to avoid leaders sharing full OSM credentials on other devices.

4. ~~**Token Refresh Failure**~~: **Resolved** - Treat as revocation of our app from OSM. Clear session and redirect to login.

5. ~~**Rate Limiting**~~: **Resolved** - If rate limited during read-modify-write, show error to user. Low likelihood given 1000 requests/user/hour limit. No transaction support in OSM, so partial failure is possible but rare.

---

# Future expansion - shortcut buttons

We could provide quick shortcuts to set the points in each field, for example

```
Eagles              20
   [0][1][3][5]  [  0]
```

The user would want to configure these, so we'd need a setup interface to do it.
Pressing 0 resets, the others add the points. Size is a concern here.
