# Story: Server-Side Outbox Pattern for Reliable Score Updates

**Status:** Planned
**Priority:** Medium
**Estimated Effort:** Large

## Problem Statement

The current score update implementation has a partial failure risk:

1. The handler loops through patrols, calling OSM once per patrol
2. If patrol 3 fails (e.g., rate limit), patrols 1-2 are already committed to OSM
3. Client receives an error and may retry, double-counting patrols 1-2
4. No idempotency protection exists

This creates data integrity issues where scores can be incorrectly incremented multiple times.

## Design Decisions

- **Processing**: Sync-first approach - attempt immediate OSM writes, return 202 Accepted with delay indication if any fail
- **Client SW queue**: Keep service worker offline queue as fallback for offline-to-server scenarios
- **Rollout**: Schema-first deployment (deploy schema + worker first, then switch handler in second deploy)
- **Tables**: Separate outbox table (operational, temporary) from audit table (historical, immutable)

## Solution Overview

Implement a server-side outbox pattern for reliable, exactly-once delivery of score updates.

### High-Level Flow

```
Client Request → Store in Outbox → Attempt OSM Write →
  ├─ All Success → Mark Complete, Create Audit, Return 200
  └─ Any Failure → Keep Failed in Outbox, Return 202 Accepted
                          ↓
                   Background Worker retries at appropriate time
                          ↓
                   On Success: Create Audit, Delete from Outbox
```

### Entry Lifecycle

```
Outbox entry created (pending)
    ↓
Sync attempt (or background worker)
    ↓
OSM write succeeds
    ↓
Audit record created (with actual before/after scores)
    ↓
Outbox entry deleted (after 24h idempotency window)
```

---

## Implementation Phases

### Phase 1: Database Schema & Store Functions

**Files:**
- `internal/db/models.go` - Add ScoreUpdateOutbox model
- `internal/db/score_outbox_store.go` - New file

**ScoreUpdateOutbox table:**

```go
type ScoreUpdateOutbox struct {
    ID              int64      `gorm:"primaryKey"`
    IdempotencyKey  string     `gorm:"uniqueIndex;not null"`
    WebSessionID    string     `gorm:"index;not null"`
    OSMUserID       int        `gorm:"index;not null"`
    SectionID       int        `gorm:"index;not null"`
    PatrolID        string     `gorm:"not null"`
    PatrolName      string     `gorm:"not null"`
    PointsDelta     int        `gorm:"not null"`
    Status          string     `gorm:"index;default:'pending'"` // pending|processing|completed|failed|auth_revoked
    AttemptCount    int        `gorm:"default:0"`
    NextRetryAt     *time.Time `gorm:"index"`
    LastError       *string
    BatchID         string     `gorm:"index"`
    CreatedAt       time.Time  `gorm:"index"`
    ProcessedAt     *time.Time
}
```

**Store functions:**
- `CreateOutboxEntries(entries []ScoreUpdateOutbox) error`
- `FindOutboxByIdempotencyKey(key string) (*ScoreUpdateOutbox, error)`
- `FindPendingOutboxEntries(limit int) ([]ScoreUpdateOutbox, error)` - with `SELECT FOR UPDATE SKIP LOCKED`
- `ClaimOutboxEntry(id int64) (*ScoreUpdateOutbox, error)` - atomic claim
- `MarkOutboxCompleted(id int64) error`
- `MarkOutboxFailed(id int64, err string, nextRetry *time.Time) error`
- `MarkOutboxAuthRevoked(osmUserID int) error`
- `RecoverAuthRevokedEntries(osmUserID int) error` - reset auth_revoked → pending on re-auth
- `CountPendingOutboxByUser(osmUserID int) (int64, error)`
- `GetPendingOutboxBySection(sessionID string, sectionID int) ([]ScoreUpdateOutbox, error)`
- `DeleteExpiredOutboxEntries() error` - respects different retention per status

### Phase 2: Background Worker

**File:** `internal/worker/outbox_processor.go` - New file

```go
type OutboxProcessor struct {
    conns     *db.Connections
    osmClient *osm.Client
}

func (p *OutboxProcessor) Start(ctx context.Context)
func (p *OutboxProcessor) processEntry(ctx context.Context, entry db.ScoreUpdateOutbox)
```

**Retry strategy:**
- Rate limit (429): Use `Retry-After` header value from OSM
- Service blocked: 10-minute backoff
- Auth revoked (401): Mark entries as `auth_revoked` (kept for 7 days, can be recovered)
- Other errors: Exponential backoff (1s, 2s, 4s, 8s, 16s), max 5 attempts

**Auth recovery:**
- When user re-authenticates (new session created for same OSMUserID)
- Check for `auth_revoked` entries belonging to that user
- Reset status to `pending` so worker will retry them
- User's pending changes are preserved across re-authentication

**Startup:** Add to `cmd/server/main.go` - start worker goroutine with graceful shutdown

### Phase 3: Handler Changes

**File:** `internal/handlers/admin_api.go`

**Modified `handleUpdateScores`:**
1. Require `X-Idempotency-Key` header - return 400 if missing
2. Check for existing entry with same key → return cached result
3. Fetch current scores from OSM
4. Create outbox entries (batch insert, transactional)
5. Attempt synchronous processing for each entry
6. Return response:
   - 200 OK if all succeeded (with results)
   - 202 Accepted if any pending (with partial results + pending count)

**Idempotency key handling:**
- Required header: `X-Idempotency-Key: <uuid>`
- If missing: return 400 Bad Request with message "Idempotency key required"
- If key exists in outbox: return cached result (status based on outbox entry status)
- Key format: UUID recommended, but any string up to 255 chars accepted
- Keys are unique per user (same key from different users = different entries)

**Response for 202 Accepted:**
```json
{
  "success": true,
  "patrols": [...],
  "pending": {
    "count": 2,
    "message": "Some updates are pending due to rate limiting. They will be applied automatically."
  }
}
```

**Session endpoint changes:**
- Add `pendingWrites` count to `AdminSessionResponse`
- Query `CountPendingOutboxByUser()` for the logged-in user

### Phase 4: Client Changes

**Files:**
- `web/admin/src/api/offlineQueue.ts` - Replace event queue with outbox pattern
- `web/admin/src/api/client.ts` - Add idempotency key header
- `web/admin/src/api/types.ts` - Update response types
- `web/admin/src/context/AuthContext.tsx` - Track `pendingWrites`
- `web/admin/src/components/Header.tsx` - Show pending indicator
- `web/admin/src/sw.ts` - Update to use client outbox

**Client-side outbox pattern (required for stable idempotency keys):**

The current approach (queue events, consolidate at sync time) breaks idempotency because the payload changes between retries if new events are added. The client needs its own outbox:

```
IndexedDB Schema:
  working-scores: { sectionId, patrolId } → { points: number }  // mutable as user works
  client-outbox:  { id, idempotencyKey, sectionId, patrolId, points, status }  // immutable once created
```

**Flow:**
1. User changes score → update `working-scores` (running total per patrol)
2. User submits → snapshot `working-scores` into `client-outbox` with new idempotency keys
3. Clear `working-scores` for submitted patrols
4. Send `client-outbox` items to server with their keys
5. On 200 OK: delete from `client-outbox`
6. On 202 Accepted: mark as "server-pending", delete from `client-outbox` (server owns it now)
7. On network error: keep in `client-outbox`, retry with same keys
8. On 4xx error: delete from `client-outbox` (rejected, don't retry)

**Why this matters:**
- Old approach: consolidate at sync time → payload changes between retries → idempotency broken
- New approach: snapshot to outbox → payload frozen → same key on retry → true idempotency

**Client behavior:**
- Generate UUID idempotency key when moving to outbox (not at sync time)
- Handle 202 response - server has accepted responsibility, clear client outbox
- Poll session endpoint to detect when server pending clears
- Show combined indicator: client-outbox pending + server pending

### Phase 5: Device API Changes

**Files:**
- `internal/handlers/api.go` - Modify patrols endpoint to include pending deltas
- `internal/db/score_outbox_store.go` - Add function to get pending by section (all users)

**Extended patrol response:**
```json
{
  "patrols": [
    {
      "id": "123",
      "name": "Eagles",
      "score": 100,
      "pendingDelta": 5,
      "hasPending": true
    }
  ]
}
```

**Device display:**
- Show adjusted score: `score + pendingDelta` (e.g., "105")
- Show pending indicator when `hasPending: true`
- For low-res displays: red star or dot next to score
- Indicator clears when pending changes are applied

**Implementation:**
1. Add `GetPendingDeltasBySection(sectionID int) (map[string]int, error)` to store
   - Aggregates `points_delta` by `patrol_id` for all pending entries in section
   - Includes `pending` and `processing` status entries
2. Modify `handleGetPatrols` in `api.go`:
   - Fetch pending deltas for section
   - Add `pendingDelta` and `hasPending` to each patrol in response
3. Device firmware update (`client-python/`):
   - Parse new fields from API response
   - Display adjusted score when pending
   - Show indicator (e.g., red dot) for pending scores

### Phase 6: Cleanup & Monitoring

**Cleanup retention periods:**
- `completed` entries: 24 hours (for idempotency window)
- `failed` entries: 7 days (for debugging)
- `auth_revoked` entries: 7 days (allows recovery on re-auth)

**Add Prometheus metrics:**
- `score_outbox_pending_total` - gauge of pending entries
- `score_outbox_processed_total` - counter by status (completed/failed)
- `score_outbox_retry_total` - counter of retry attempts

---

## Files Summary

| File | Action | Phase |
|------|--------|-------|
| `internal/db/models.go` | Add ScoreUpdateOutbox, update AutoMigrate | 1 |
| `internal/db/score_outbox_store.go` | New file - store functions | 1 |
| `internal/worker/outbox_processor.go` | New file - background worker | 2 |
| `cmd/server/main.go` | Start worker goroutine | 2 |
| `internal/handlers/admin_api.go` | Modify handler, extend session | 3 |
| `web/admin/src/api/offlineQueue.ts` | Replace event queue with outbox pattern | 4 |
| `web/admin/src/api/client.ts` | Idempotency key, handle 202 | 4 |
| `web/admin/src/api/types.ts` | Update response types | 4 |
| `web/admin/src/context/AuthContext.tsx` | Track pendingWrites | 4 |
| `web/admin/src/components/Header.tsx` | Pending indicator | 4 |
| `web/admin/src/sw.ts` | Update to use client outbox | 4 |
| `internal/handlers/api.go` | Add pending deltas to patrol response | 5 |
| `client-python/` | Parse pending fields, show indicator | 5 |

---

## Acceptance Criteria

1. **Idempotency key required**: Requests without `X-Idempotency-Key` header return 400 Bad Request
2. **Idempotency works**: Submitting the same update twice (with same key) returns the same result without double-applying
3. **Partial failure handling**: If 3 patrols are submitted and 1 fails, the 2 successful ones are committed and the failed one retries automatically
4. **Rate limit handling**: Rate-limited requests are retried at the time specified by OSM's `Retry-After` header
5. **Auth recovery**: If user re-authenticates after auth revocation, their pending changes are recovered and retried
6. **Admin UI visibility**: Users see a "pending" indicator showing combined client-outbox + server-pending count
7. **Device visibility**: Scoreboard devices show adjusted scores (current + pending) with a visual indicator (red dot) for pending changes
8. **Audit trail**: Audit records are only created when updates are successfully applied to OSM

## Verification Plan

1. **Unit tests:**
   - Store functions (CRUD, locking, idempotency)
   - Worker retry logic
   - Handler idempotency check

2. **Integration test:**
   - Submit update → verify outbox entry created
   - Simulate OSM 429 → verify retry scheduled at correct time
   - Verify audit created only on completion

3. **Manual testing:**
   - Normal submit → 200 response, audit created
   - Rate limited → 202 response, pending indicator shows
   - Wait for retry → pending clears, audit created
   - Session endpoint shows correct pending count

---

## Future Considerations

- **Scoreboard device API**: Could expose pending deltas so devices display "pending: +5" alongside current scores. This would require a new endpoint or extending the existing patrols API.
- **Conflict detection**: If other leaders modify scores in OSM directly, we could detect this by comparing base_score with current score before applying delta.

---

## Related Files

- Current handler: `internal/handlers/admin_api.go` (lines 340-485)
- OSM client: `internal/osm/patrol_scores_update.go`
- Rate limit handling: `internal/osm/request.go` (lines 185-205, 320-328)
- Existing audit model: `internal/db/models.go` (ScoreAuditLog)
- Client offline queue: `web/admin/src/api/offlineQueue.ts`
- Service worker sync: `web/admin/src/sw.ts`
