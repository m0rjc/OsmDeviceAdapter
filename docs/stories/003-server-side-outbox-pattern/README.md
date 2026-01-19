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

### Distributed Coordination & Concurrency

With multiple Kubernetes replicas (currently 2), we need distributed coordination for the read-calculate-write cycle that spans external OSM API calls. The design handles concurrent leaders coming online simultaneously.

**Core principles:**
1. **Always create new outbox records** - never merge at insert time, preserves audit trail
2. **Batch on sync** - when syncing, greedily grab all pending entries for the patrol
3. **Redis for distributed locking** - coordinates which replica handles sync for a patrol
4. **SKIP LOCKED as safety layer** - database-level protection if Redis coordination fails

**Interactive vs Background sync modes:**

| Mode | Source | Debounce | Behavior |
|------|--------|----------|----------|
| Interactive | User clicks submit | None | Immediate sync attempt |
| Background | Service worker comes online | 10-30 seconds | Waits to batch with other leaders |

Interactive syncs remain fast regardless of debounce length. A longer debounce window allows more background syncs to accumulate, reducing OSM API calls.

**Redis keys:**
- `patrol:{sectionId}:{patrolId}:sync_lock` - Distributed lock during sync (TTL: 30s)
- `patrol:{sectionId}:{patrolId}:debounce` - Signals debounce window is active (TTL: configurable, e.g., 15s)

**Sync flow:**

```
Request arrives (interactive or background):
  1. Write outbox entry → commit immediately (visible to all replicas)
  2. Respond 202 Accepted to client (entry is persisted)
  3. If interactive:
       - Immediately attempt sync (skip debounce)
  4. If background:
       - SETNX patrol:{id}:debounce → if SET succeeded, schedule sync after TTL
       - If already set, our entry will be picked up by scheduled sync

Sync execution (immediate for interactive, after debounce for background):
  1. Try SETNX patrol:{id}:sync_lock {replica-id} EX 30
  2. If lock NOT acquired:
       - Another replica is handling it
       - Our entries will be picked up by that sync OR by next worker poll (≤5s)
       - Note: If we just committed rows and the lock holder already queried before
         our commit was visible, the worker poll provides the safety net. This is
         an acceptable trade-off vs. adding re-query complexity to the lock holder.
       - Return early (respond 202 to client if interactive)
  3. If lock acquired:
       - SELECT all pending outbox entries for patrol FOR UPDATE SKIP LOCKED
       - Sum all deltas (e.g., Leader A: +5, Leader B: +3 = +8)
       - Read current score from OSM
       - Write new score to OSM (single API call)
       - On success: mark all selected entries as completed, create audit entries
       - Delete sync_lock
```

**Why this works for concurrent leaders:**
- Leader A and Leader B both come online and submit updates
- Both create separate outbox entries (preserving attribution)
- Whichever sync executes first grabs ALL pending entries for that patrol
- Single OSM API call applies the combined delta
- Each original entry gets its own audit record

**Safety properties:**
- **Greedy grab**: Interactive sync picks up waiting background entries
- **SKIP LOCKED**: If Redis lock expires mid-sync, another replica won't grab already-processing rows
- **Idempotency preserved**: Each entry has its own key, unaffected by batching
- **Audit trail intact**: One audit record per original outbox entry

**Acknowledged risk:**
If database write-back fails after OSM succeeds (connection drops between OSM response and our commit), retrying would re-apply the delta. This window is small and requires a specific failure mode. Mitigation: structured logging of OSM success before DB commit allows manual reconciliation if needed.

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
- `internal/db/scoreoutbox/store.go` - New file

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

**Store functions (in `internal/db/scoreoutbox/` package):**
- `scoreoutbox.Create(conns, entry) error` - single entry insert, commits immediately
- `scoreoutbox.CreateBatch(conns, entries) error` - batch insert
- `scoreoutbox.FindByIdempotencyKey(conns, key) (*ScoreUpdateOutbox, error)`
- `scoreoutbox.ClaimPendingForPatrol(conns, sectionID, patrolID) ([]ScoreUpdateOutbox, error)` - `SELECT FOR UPDATE SKIP LOCKED` all pending entries for a patrol, marks as `processing`
- `scoreoutbox.MarkCompleted(conns, ids, processedAt) error` - batch mark completed
- `scoreoutbox.MarkFailed(conns, ids, err, nextRetry) error` - batch mark failed
- `scoreoutbox.MarkAuthRevoked(conns, osmUserID) error`
- `scoreoutbox.RecoverAuthRevoked(conns, osmUserID) error` - reset auth_revoked → pending on re-auth
- `scoreoutbox.CountPendingByUser(conns, osmUserID) (int64, error)`
- `scoreoutbox.GetPendingBySection(conns, sessionID, sectionID) ([]ScoreUpdateOutbox, error)`
- `scoreoutbox.FindPatrolsWithPending(conns, sectionID) ([]string, error)` - returns patrol IDs that have pending entries (for worker to iterate)
- `scoreoutbox.DeleteExpired(conns) error` - respects different retention per status

**Key query for claiming entries:**
```sql
UPDATE score_update_outbox
SET status = 'processing', attempt_count = attempt_count + 1
WHERE section_id = $1 AND patrol_id = $2 AND status = 'pending'
RETURNING *
-- Note: Use FOR UPDATE SKIP LOCKED in SELECT variant if needed
```

### Phase 2: Background Worker & Sync Service

**Files:**
- `internal/worker/outbox_processor.go` - Background worker for scheduled syncs
- `internal/worker/patrol_sync.go` - Shared sync logic (used by worker and handler)
- `internal/worker/redis_lock.go` - Redis distributed locking utilities

```go
// Shared sync service - used by both handler (interactive) and worker (background)
type PatrolSyncService struct {
    conns     *db.Connections
    osmClient *osm.Client
    replicaID string  // Unique identifier for this replica (e.g., pod name)
}

// SyncPatrol attempts to sync all pending entries for a patrol
// Returns: synced count, error
func (s *PatrolSyncService) SyncPatrol(ctx context.Context, sectionID int, patrolID string) (int, error)

// Redis lock helpers
func (s *PatrolSyncService) AcquireSyncLock(ctx context.Context, sectionID int, patrolID string) (bool, error)
func (s *PatrolSyncService) ReleaseSyncLock(ctx context.Context, sectionID int, patrolID string) error
func (s *PatrolSyncService) SetDebounce(ctx context.Context, sectionID int, patrolID string, ttl time.Duration) (bool, error)
```

```go
// Background worker - polls for patrols needing sync
type OutboxProcessor struct {
    syncService *PatrolSyncService
    conns       *db.Connections
}

func (p *OutboxProcessor) Start(ctx context.Context)
func (p *OutboxProcessor) processPatrol(ctx context.Context, sectionID int, patrolID string)
```

**SyncPatrol implementation:**
```go
func (s *PatrolSyncService) SyncPatrol(ctx context.Context, sectionID int, patrolID string) (int, error) {
    // 1. Try to acquire Redis lock
    acquired, err := s.AcquireSyncLock(ctx, sectionID, patrolID)
    if err != nil || !acquired {
        return 0, err // Another replica is handling it
    }
    defer s.ReleaseSyncLock(ctx, sectionID, patrolID)

    // 2. Claim all pending entries for this patrol (SKIP LOCKED)
    entries, err := scoreoutbox.ClaimPendingForPatrol(s.conns, sectionID, patrolID)
    if err != nil || len(entries) == 0 {
        return 0, err
    }

    // 3. Sum deltas and get user context (use first entry's session for OSM auth)
    totalDelta := 0
    for _, e := range entries {
        totalDelta += e.PointsDelta
    }

    // 4. Read current score from OSM
    // 5. Write new score to OSM
    // 6. On success: mark all entries completed, create audit records
    // 7. On failure: mark entries failed with appropriate retry time
}
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

**Worker loop:**
```go
func (p *OutboxProcessor) Start(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Find all section/patrol pairs with pending or failed (ready to retry) entries
            patrols, _ := scoreoutbox.FindPatrolsWithPending(p.conns)
            for _, patrol := range patrols {
                p.syncService.SyncPatrol(ctx, patrol.SectionID, patrol.PatrolID)
            }
        }
    }
}
```

**Startup:** Add to `cmd/server/main.go` - start worker goroutine with graceful shutdown. Pass replica ID from `HOSTNAME` env var (set by Kubernetes).

### Phase 3: Handler Changes

**File:** `internal/handlers/admin_api.go`

**Modified `handleUpdateScores`:**
1. Require `X-Idempotency-Key` header - return 400 if missing
2. Check for existing entry with same key → return cached result
3. Create outbox entries and commit immediately (makes entries visible to all replicas)
4. Respond 202 Accepted (entry is persisted, safe from client perspective)
5. Determine sync mode and attempt sync:
   - **Interactive**: Immediately call `SyncPatrol()` for each affected patrol
   - **Background**: Set debounce key, let worker handle after delay
6. Return final response based on sync result

**Sync mode detection:**
- Header: `X-Sync-Mode: interactive` (default) or `X-Sync-Mode: background`
- Service worker requests use `background` mode
- User-initiated submits use `interactive` mode (or omit header)

**Interactive flow (no debounce):**
```
1. Create outbox entries → commit
2. For each patrol in request:
     - Call syncService.SyncPatrol(sectionID, patrolID)
     - This greedily grabs ALL pending entries (including from other leaders)
3. Return 200 if all synced, 202 if any pending (lock contention or failure)
```

**Background flow (with debounce):**
```
1. Create outbox entries → commit
2. For each patrol in request:
     - SETNX debounce key with TTL (e.g., 15 seconds)
     - If SET succeeded: we're first, worker will pick up after TTL
     - If SET failed: debounce already active, our entry joins the batch
3. Return 202 Accepted immediately
```

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

**Response for 200 OK (all synced):**
```json
{
  "success": true,
  "patrols": [
    {"id": "123", "name": "Eagles", "score": 105}
  ]
}
```

**Session endpoint changes:**
- Add `pendingWrites` count to `AdminSessionResponse`
- Query `scoreoutbox.CountPendingByUser()` for the logged-in user

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
- `internal/db/scoreoutbox/store.go` - Add function to get pending by section (all users)

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
1. Add `scoreoutbox.GetPendingDeltasBySection(conns, sectionID) (map[string]int, error)` to store
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
| `internal/db/scoreoutbox/store.go` | New file - store functions with SKIP LOCKED support | 1 |
| `internal/worker/patrol_sync.go` | New file - shared sync service with Redis locking | 2 |
| `internal/worker/redis_lock.go` | New file - Redis distributed lock utilities | 2 |
| `internal/worker/outbox_processor.go` | New file - background worker | 2 |
| `cmd/server/main.go` | Start worker goroutine, pass replica ID | 2 |
| `internal/handlers/admin_api.go` | Modify handler with interactive/background modes | 3 |
| `web/admin/src/api/offlineQueue.ts` | Replace event queue with outbox pattern | 4 |
| `web/admin/src/api/client.ts` | Idempotency key, X-Sync-Mode header, handle 202 | 4 |
| `web/admin/src/api/types.ts` | Update response types | 4 |
| `web/admin/src/context/AuthContext.tsx` | Track pendingWrites | 4 |
| `web/admin/src/components/Header.tsx` | Pending indicator | 4 |
| `web/admin/src/sw.ts` | Update to use client outbox, send X-Sync-Mode: background | 4 |
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
8. **Audit trail**: Audit records are only created when updates are successfully applied to OSM (one per original outbox entry, preserving attribution)
9. **Concurrent leader handling**: When two leaders come online simultaneously with updates for the same patrol, both updates are captured separately and batched into a single OSM API call
10. **Interactive mode fast**: Interactive updates (user-initiated) sync immediately without debounce delay
11. **Background mode batches**: Background updates (service worker) are debounced and batched together
12. **Distributed safety**: With multiple replicas, only one replica processes a given patrol at a time (Redis lock + SKIP LOCKED)

## Verification Plan

1. **Unit tests:**
   - Store functions (CRUD, locking with SKIP LOCKED, idempotency)
   - Worker retry logic
   - Handler idempotency check
   - Redis lock acquire/release
   - Debounce key handling

2. **Integration tests:**
   - Submit update → verify outbox entry created
   - Simulate OSM 429 → verify retry scheduled at correct time
   - Verify audit created only on completion
   - Interactive mode → immediate sync attempted
   - Background mode → debounce key set, no immediate sync

3. **Concurrency tests:**
   - Two simultaneous requests for same patrol → both entries created, single OSM call, both audited
   - Lock contention → second request gets 202, entry picked up by first sync
   - SKIP LOCKED behavior → locked rows not grabbed by concurrent query

4. **Manual testing:**
   - Normal submit → 200 response, audit created
   - Rate limited → 202 response, pending indicator shows
   - Wait for retry → pending clears, audit created
   - Session endpoint shows correct pending count
   - Two browser tabs submitting simultaneously → both updates applied correctly

---

## Future Considerations

- **Conflict detection**: If other leaders modify scores in OSM directly, we could detect this by comparing expected score (before our delta) with actual current score before applying delta. Log warning if mismatch detected.
- **Configurable debounce**: Allow debounce duration to be configured via environment variable (default 15 seconds).
- **Metrics dashboard**: Grafana dashboard showing outbox queue depth, sync latency, retry rates.

### Rejected Alternative: This Tool as Source of Truth

**Idea**: Store authoritative scores in our database rather than treating OSM as the source of truth. This would eliminate the read-calculate-write cycle, distributed coordination complexity, and OSM rate limit concerns.

**Why rejected**:
1. Other leaders want to maintain scores directly in OSM (not everyone uses this tool)
2. OSM is used for start-of-term score resets - we don't want to duplicate this functionality
3. Would require migrating existing score data and changing leader workflows

The current design accepts the complexity of syncing to OSM in exchange for preserving the existing OSM-centric workflow that leaders are familiar with.

---

## Related Files

- Current handler: `internal/handlers/admin_api.go` (lines 340-485)
- OSM client: `internal/osm/patrol_scores_update.go`
- Rate limit handling: `internal/osm/request.go` (lines 185-205, 320-328)
- Existing audit model: `internal/db/models.go` (ScoreAuditLog)
- Client offline queue: `web/admin/src/api/offlineQueue.ts`
- Service worker sync: `web/admin/src/sw.ts`
