# Story 003: Server-Side Outbox Pattern - Implementation Planning

**Status:** In Progress (Phases 1-3 Complete)
**Created:** 2026-01-19
**Last Updated:** 2026-01-19

## Design Decisions from Planning

### Worker Configuration
- **Single worker** (not concurrent) to reduce OSM API load
- **30-second poll interval** to minimize OSM requests
- Rationale: OSM is a large multi-user system not in our control; we must be conservative with API usage

### Partitioning Strategy
- **Partition by user** for outbox processing
- Rationale:
  - Patrol IDs are only unique within section (OSM API requires both section+patrol)
  - Preserves audit trail integrity (one user per batch of changes)
  - Simplifies session/credential management (one user's tokens per sync)
  - Maintains authorization boundaries (don't use User A's tokens for User B's changes)
  - Still achieves coalescing benefits (multiple updates from same user for same patrol)

### Credential Lifecycle
- **Separate `user_credentials` table** decoupled from web sessions
- Web sessions are ephemeral (7-day expiry, per-login)
- User credentials persist as long as needed for offline processing
- Multiple web sessions for same user share the same credentials
- Credentials deleted only when: no active sessions AND no pending writes (+ 7-day grace period)

### Testing Strategy
- **Cannot test rate limiting manually** against live OSM
- Will implement mocked OSM tests for rate limit scenarios
- Manual testing limited to happy path and basic error cases

---

## Architecture: Web Sessions vs. User Credentials

### The Problem
Workers need OSM credentials to process outbox entries in the background, but web sessions are ephemeral:
- Users may log out while writes are pending
- Sessions expire after 7 days
- Users may have multiple simultaneous sessions (different browsers/devices)

### The Solution
Separate concerns with two tables:

**`web_sessions`** - Ephemeral login sessions
- One per OAuth login (multiple concurrent sessions allowed)
- Includes session cookie, CSRF token, selected section
- 7-day expiry with sliding window
- Deleted on explicit logout or expiry

**`user_credentials`** - Persistent OSM credentials
- One per OSM user (shared across all their sessions)
- Contains only OSM tokens (access, refresh, expiry) and user name
- Created/updated on every login (keeps tokens fresh)
- Deleted only when: no sessions + no pending writes + 7-day grace period

### Data Flow

**On Login (OAuth callback):**
1. Create new `web_sessions` record with cookie
2. Upsert `user_credentials` record (refresh tokens)

**On Score Submit:**
1. Validate via `web_sessions` (user is logged in)
2. Create `score_update_outbox` entry referencing `user_credentials.osm_user_id`
3. Return 202 Accepted

**Background Worker:**
1. Find pending entries partitioned by `osm_user_id`
2. Load credentials from `user_credentials` (not web session)
3. Refresh tokens if needed
4. Apply update to OSM
5. Update `user_credentials.last_used_at`

**On Logout:**
1. Delete only the specific `web_sessions` record
2. Keep `user_credentials` intact (other sessions or pending writes may exist)

**Cleanup Job:**
1. Delete `user_credentials` where:
   - `SELECT COUNT(*) FROM web_sessions WHERE osm_user_id = ? ` returns 0
   - `SELECT COUNT(*) FROM score_update_outbox WHERE osm_user_id = ? AND status IN ('pending','processing')` returns 0
   - `last_used_at < NOW() - 7 days`

### Benefits
- Users can log out without blocking pending writes
- Multiple concurrent logins share credentials (no token conflicts)
- Clean separation: sessions for HTTP auth, credentials for API calls
- Automatic cleanup when user is truly done

---

## Implementation Phases

### Phase 1: Database Schema & Store Functions ✅ COMPLETED

**New Models** - Add to `internal/db/models.go`:

```go
// UserCredential stores persistent OSM credentials for offline processing.
// Decoupled from web sessions to allow credentials to outlive ephemeral logins.
// Multiple web sessions for the same user share these credentials.
type UserCredential struct {
    // OSMUserID is the OSM user identifier (primary key)
    OSMUserID int `gorm:"primaryKey;column:osm_user_id"`

    // OSMUserName is the user's display name from OSM (for audit readability)
    // Updated on every login to reflect current name
    OSMUserName string `gorm:"column:osm_user_name;size:255;not null"`

    // OSMEmail is the user's email from OSM (for debugging/support)
    // May be empty if not provided by OSM
    OSMEmail string `gorm:"column:osm_email;size:255"`

    // OSMAccessToken is the current OAuth access token
    OSMAccessToken string `gorm:"column:osm_access_token;type:text;not null"`

    // OSMRefreshToken is the OAuth refresh token
    OSMRefreshToken string `gorm:"column:osm_refresh_token;type:text;not null"`

    // OSMTokenExpiry is when the access token expires
    OSMTokenExpiry time.Time `gorm:"column:osm_token_expiry;not null"`

    // CreatedAt is when credentials were first stored (first login)
    CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`

    // UpdatedAt is last token refresh time (login or background refresh)
    UpdatedAt time.Time `gorm:"column:updated_at;default:CURRENT_TIMESTAMP"`

    // LastUsedAt is when credentials were last used for outbox processing
    // Used by cleanup job to determine if credentials are still needed
    LastUsedAt *time.Time `gorm:"column:last_used_at;index:idx_user_credentials_last_used"`
}

func (UserCredential) TableName() string {
    return "user_credentials"
}

// ScoreUpdateOutbox stores pending score updates for background processing.
// Partitioned by user to preserve audit trail and simplify credential management.
type ScoreUpdateOutbox struct {
    ID              uint       `gorm:"primaryKey"`
    IdempotencyKey  string     `gorm:"uniqueIndex;size:255;not null"`
    OSMUserID       int        `gorm:"index:idx_outbox_user_section_patrol;not null"` // Foreign key to user_credentials
    SectionID       int        `gorm:"index:idx_outbox_user_section_patrol;not null"`
    PatrolID        string     `gorm:"index:idx_outbox_user_section_patrol;type:varchar(255);not null"`
    PatrolName      string     `gorm:"size:255;not null"`
    PointsDelta     int        `gorm:"not null"`
    Status          string     `gorm:"size:20;index;not null;default:'pending'"` // pending, processing, completed, failed, auth_revoked
    AttemptCount    int        `gorm:"not null;default:0"`
    NextRetryAt     *time.Time `gorm:"index"`
    LastError       string     `gorm:"type:text"`
    BatchID         string     `gorm:"size:255;index"`
    CreatedAt       time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP"`
    ProcessedAt     *time.Time
}

func (ScoreUpdateOutbox) TableName() string {
    return "score_update_outbox"
}
```

**Files to create/modify:**
- `internal/db/models.go` - Add models, update AutoMigrate
- `internal/db/usercredentials/store.go` - New store package for credentials
- `internal/db/scoreoutbox/store.go` - New store package for outbox
- `internal/db/scoreoutbox/store_test.go` - Unit tests

**UserCredentials store functions:**
| Function | Purpose |
|----------|---------|
| `CreateOrUpdate` | Upsert credentials on login |
| `Get` | Get credentials by user ID |
| `UpdateTokens` | Update after token refresh |
| `UpdateLastUsed` | Update last used timestamp |
| `FindStaleCredentials` | Find credentials for cleanup (no sessions, no pending writes, +7 days) |
| `Delete` | Delete credentials |

**ScoreUpdateOutbox store functions:**
| Function | Purpose |
|----------|---------|
| `Create` | Insert single entry |
| `CreateBatch` | Batch insert |
| `FindByIdempotencyKey` | Lookup for deduplication |
| `ClaimPendingForUserPatrol` | SELECT FOR UPDATE SKIP LOCKED (user + section + patrol) |
| `MarkCompleted` | Mark success |
| `MarkFailed` | Mark failure with backoff |
| `MarkAuthRevoked` | Mark all user entries as auth_revoked |
| `RecoverAuthRevoked` | Reset to pending on re-auth |
| `CountPendingByUser` | For session endpoint |
| `GetPendingDeltasBySection` | Aggregate pending deltas (for user's section) |
| `FindUserPatrolsWithPending` | For worker to iterate (returns user+section+patrol tuples) |
| `DeleteExpired` | Cleanup old entries |

**Completion Summary:**
- ✅ UserCredential and ScoreUpdateOutbox models added to `internal/db/models.go`
- ✅ AutoMigrate updated to include new models
- ✅ TokenHolder interface implemented for UserCredential
- ✅ `internal/db/helpers.go` created with ForUpdateSkipLocked helper
- ✅ `internal/db/usercredentials/store.go` package created with all 6 functions
- ✅ `internal/db/scoreoutbox/store.go` package created with all 11 functions
- ✅ `internal/db/scoreoutbox/store_test.go` created with comprehensive test coverage
- ✅ All tests passing (12/12 scoreoutbox tests, all db tests pass)
- ✅ Build successful

---

### Phase 2: Background Worker & Sync Service ✅ COMPLETED

**Design constraints:**
- Single worker goroutine (no concurrency)
- 30-second poll interval
- Redis distributed lock with 60-second TTL
- User-partitioned processing (lock key includes user ID)

**Files to create:**
- `internal/worker/redis_lock.go` - Distributed locking utilities
- `internal/worker/patrol_sync.go` - PatrolSyncService
- `internal/worker/outbox_processor.go` - Background worker
- `internal/worker/credential_manager.go` - Token refresh service for user credentials

**Modify:**
- `cmd/server/main.go` - Worker startup/shutdown
- `internal/handlers/dependencies.go` - Add PatrolSyncService

**PatrolSyncService.SyncPatrol algorithm:**
1. Acquire Redis lock for user+patrol (key: `outbox:lock:{userID}:{sectionID}:{patrolID}`, 60s TTL)
2. Claim pending entries via SKIP LOCKED (for this user+section+patrol)
3. Get user credentials from `user_credentials` table
4. Refresh tokens if needed (via CredentialManager)
5. Fetch current score from OSM using user's tokens
6. Calculate net delta from all claimed entries
7. Apply single OSM update
8. Mark entries completed, create audit log (attributed to user)
9. Update `user_credentials.last_used_at`
10. Release lock

**CredentialManager responsibilities:**
- Refresh OSM tokens when near expiry (5-minute threshold, same as deviceauth)
- Update `user_credentials` table after refresh
- Handle auth revoked (401) by:
  - Mark all user's pending outbox entries as `auth_revoked`
  - Keep credentials in table (user may re-login and trigger `RecoverAuthRevoked`)
  - Log warning for monitoring
- User re-login will:
  - Update `user_credentials` with new tokens
  - Call `RecoverAuthRevoked` to reset `auth_revoked` entries back to `pending`

**OutboxProcessor configuration:**
```go
type OutboxProcessorConfig struct {
    PollInterval time.Duration // 30 seconds
    WorkerCount  int           // 1 (single worker)
}
```

**Worker loop:**
```go
for {
    // Get all (userID, sectionID, patrolID) with pending entries
    userPatrols := store.FindUserPatrolsWithPending()

    for _, up := range userPatrols {
        err := syncService.SyncPatrol(up.UserID, up.SectionID, up.PatrolID)
        if err != nil {
            // Log and continue to next patrol
        }
    }

    time.Sleep(pollInterval)
}
```

**Completion Summary:**
- ✅ `internal/worker/redis_lock.go` created with Redis distributed locking (60s TTL)
- ✅ `internal/worker/credential_manager.go` created with token refresh and auth revocation handling
- ✅ `internal/worker/patrol_sync.go` created with 9-step coalescing sync algorithm
- ✅ `internal/worker/patrol_sync_test.go` created with 7 comprehensive unit tests (all passing)
- ✅ `internal/worker/outbox_processor.go` created with 30-second polling, single worker
- ✅ `cmd/server/main.go` updated with worker lifecycle (startup/shutdown)
- ✅ `internal/handlers/dependencies.go` updated to include PatrolSyncService
- ✅ Worker successfully processes outbox entries with coalescing (3 entries → 1 OSM call)
- ✅ Auth revoked handling marks entries and recovers on re-login
- ✅ Exponential backoff implemented (1min → 8 hours, max 10 attempts)
- ✅ All unit tests passing with mocked OSM endpoints

---

### Phase 3: Handler Changes ✅ COMPLETED

**Modify** `internal/handlers/admin_oauth.go` - OAuth callback handler:
- After successful OAuth, create/update `user_credentials` entry
- Store user name from OSM resource endpoint
- Use `CreateOrUpdate` to handle multiple logins

**Modify** `internal/handlers/admin_api.go` - Score update handler (lines ~341-486):

1. Require `X-Idempotency-Key` header (400 if missing)
2. Check for existing entry with same key → return cached result
3. Ensure user credentials exist (should already exist from login, but defensive check)
4. Create outbox entries and commit immediately
5. Respond 202 Accepted
6. If `X-Sync-Mode: interactive` (default): trigger immediate SyncPatrol
7. If `X-Sync-Mode: background`: let worker handle

**Response format:**
```json
{
  "status": "accepted",
  "batchId": "uuid",
  "entriesCreated": 3
}
```

**Update session endpoint** to include `pendingWrites` count.

**Credential lifecycle management:**
- Created/updated on every OAuth login (callback handler)
- Updated by CredentialManager during token refresh
- Deleted by cleanup job when: no active web sessions + no pending writes + 7 days since last use

**Completion Summary:**
- ✅ `internal/handlers/admin_oauth.go` updated to create/update user_credentials on login
- ✅ OAuth callback now calls `RecoverAuthRevoked()` to reset auth_revoked entries
- ✅ `internal/handlers/admin_api.go` completely rewritten for outbox pattern
- ✅ Score update handler requires `X-Idempotency-Key` header (400 if missing)
- ✅ Duplicate idempotency keys return cached 202 responses
- ✅ Handler creates outbox entries with unique composite keys (baseKey:patrolID:index)
- ✅ Returns 202 Accepted with batch ID immediately
- ✅ Dual sync modes implemented: interactive (immediate) and background (30s polling)
- ✅ Session endpoint updated to include `pendingWrites` count
- ✅ `internal/handlers/admin_api_integration_test.go` created with 4 comprehensive integration tests
- ✅ All integration tests passing (FullFlow, Idempotency, ConcurrentSubmissions, InteractiveMode)
- ✅ Tests validate end-to-end flow from HTTP → Outbox → Worker → Mocked OSM
- ✅ Coalescing verified in tests (multiple entries → single OSM call)
- ✅ Fixed idempotency key bug for multiple updates to same patrol
- ✅ Fixed `FindByIdempotencyKey()` to handle both exact and prefix matches

---

### Phase 4: Client Changes

**New IndexedDB schema** in `web/admin/src/api/offlineQueue.ts`:
- `working-scores` store: `{sectionId, patrolId} → {snapshotScore, localDelta}`
- `client-outbox` store: `{id, idempotencyKey, sectionId, patrolId, delta, status}`

**Flow:**
1. User enters changes → update `working-scores` (mutable)
2. User clicks submit → snapshot to `client-outbox` with new idempotency keys
3. Clear working-scores for submitted patrols
4. Send with `X-Idempotency-Key` header
5. On 202: delete from client-outbox (server owns it now)
6. On network error: keep in client-outbox, retry with same keys

**Files:**
- `web/admin/src/api/offlineQueue.ts` - Replace event queue with outbox
- `web/admin/src/api/client.ts` - Add headers, handle 202
- `web/admin/src/sw.ts` - Update sync logic, send `X-Sync-Mode: background`

---

### Phase 5: Device API Changes

**Modify** `internal/handlers/api.go`:

Add to patrol response:
```json
{
  "patrolId": "123",
  "name": "Eagles",
  "points": 100,
  "pendingDelta": 5,
  "hasPending": true
}
```

---

### Phase 6: Cleanup & Monitoring

**Prometheus metrics** in `internal/metrics/metrics.go`:
- `score_outbox_entries_created_total`
- `score_outbox_entries_processed_total{status}`
- `score_outbox_sync_duration_seconds`
- `score_outbox_pending_entries` (gauge)
- `user_credentials_active` (gauge) - count of active credentials
- `user_credentials_cleaned_total` - cleanup counter

**Cleanup retention:**

*Outbox entries:*
- Completed: 24 hours
- Failed/auth_revoked: 7 days

*User credentials:*
- Delete when ALL conditions met:
  - No active web sessions for this user
  - No pending/processing outbox entries for this user
  - Last used > 7 days ago (grace period)
- Cleanup job runs every 6 hours

---

## Deployment Strategy

1. **Phase 1** (Schema): Deploy first, workers idle
2. **Phase 2** (Workers): Deploy worker code, still idle
3. **Phase 3** (Handlers): Entries start flowing
4. **Phase 4** (Client): Gradual rollout
5. **Phase 5** (Device API): Non-breaking (new fields)
6. **Phase 6** (Cleanup): Deploy cleanup job

---

## Testing Strategy

### Unit Tests (can run against mocks)
- Store functions: CRUD, idempotency, SKIP LOCKED behavior
- Redis locks: acquire/release, concurrent acquire
- PatrolSyncService: success path, auth errors, rate limits (mocked OSM)
- Handler: idempotency key required, 202 response

### Integration Tests (mocked OSM)
- Full flow: HTTP request → outbox → background sync → mocked OSM call
- Concurrent submissions for same patrol → single OSM call
- Rate limit simulation → retry scheduling

### Manual Testing (live OSM, limited)
- Normal submit → verify audit created
- Check pending indicator shows and clears
- Two browser tabs submitting → both updates applied

---

## Critical Files Summary

| File | Action |
|------|--------|
| `internal/db/models.go` | Add UserCredential and ScoreUpdateOutbox models |
| `internal/db/usercredentials/store.go` | New - credential store package |
| `internal/db/scoreoutbox/store.go` | New - outbox store package |
| `internal/db/scoreoutbox/store_test.go` | New - tests |
| `internal/worker/redis_lock.go` | New - distributed locking |
| `internal/worker/credential_manager.go` | New - token refresh for credentials |
| `internal/worker/patrol_sync.go` | New - sync service |
| `internal/worker/outbox_processor.go` | New - background worker |
| `cmd/server/main.go` | Start/stop worker |
| `internal/handlers/admin_oauth.go` | Modify to create/update user_credentials |
| `internal/handlers/admin_api.go` | Modify score handler for outbox |
| `internal/handlers/api.go` | Add pending fields to device API |
| `internal/metrics/metrics.go` | Add outbox and credential metrics |
| `web/admin/src/api/offlineQueue.ts` | Replace with outbox pattern |
| `web/admin/src/sw.ts` | Update sync logic |
