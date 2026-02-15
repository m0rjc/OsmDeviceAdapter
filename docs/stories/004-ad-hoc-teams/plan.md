# Ad-hoc Teams Implementation Plan

## Context

Leaders sometimes run games where temporary teams need scores tracked on the scoreboard -- teams that don't exist in OSM. This feature adds a "virtual" ad-hoc section per user, with patrols stored entirely in our PostgreSQL database. Since we own the data, the read/write paths are far simpler than the OSM proxy paths: no rate limiting, no term management, no token refresh concerns.

**Key design decision:** Use `section_id = 0` as a sentinel value for the ad-hoc section. OSM section IDs are positive integers, and `nil` already means "not configured". This avoids schema changes to existing tables and lets the ad-hoc section flow through existing section-aware code paths naturally.

**One ad-hoc section per user** is sufficient. Running multiple simultaneous games is unlikely, and the user can always reset scores and rename teams between games.

---

## Phase 1: Database Schema & Store

- [ ] Add `AdhocPatrol` model to `internal/db/models.go`
- [ ] Register in `AutoMigrate()`
- [ ] Create `internal/db/adhocpatrol/store.go`
- [ ] Write store tests

### New model in `internal/db/models.go`

```go
type AdhocPatrol struct {
    ID        int64     `gorm:"primaryKey;autoIncrement"`
    OSMUserID int       `gorm:"not null;uniqueIndex:idx_adhoc_user_position"`
    Position  int       `gorm:"not null;uniqueIndex:idx_adhoc_user_position"`
    Name      string    `gorm:"type:varchar(100);not null"`
    Color     string    `gorm:"type:varchar(50);not null;default:''"`
    Score     int       `gorm:"not null;default:0"`
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

- `ID` (int64 PK) exposed as string in API to match existing `PatrolScore.ID` pattern
- Composite unique index `(osm_user_id, position)` for ordering
- Max 20 patrols per user (enforced in store)

### New store: `internal/db/adhocpatrol/store.go`

```
ListByUser(conns, osmUserID) → []AdhocPatrol
Create(conns, patrol) → error          // assigns next position, enforces max 20
Update(conns, id, osmUserID, name, color) → error  // ownership check
Delete(conns, id, osmUserID) → error   // ownership check
UpdateScore(conns, id, osmUserID, newScore) → error
ResetAllScores(conns, osmUserID) → error
```

All queries filter by `osm_user_id` to enforce ownership.

**Files:** `internal/db/models.go`, new `internal/db/adhocpatrol/store.go`

---

## Phase 2: Ad-hoc Patrol CRUD API

- [ ] Create `internal/handlers/admin_adhoc.go`
- [ ] Register routes in `internal/server/server.go`
- [ ] Write handler tests

### New handler: `internal/handlers/admin_adhoc.go`

Behind existing `adminMiddleware` (session + token refresh + CSRF):

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/admin/adhoc/patrols` | List user's ad-hoc patrols |
| POST | `/api/admin/adhoc/patrols` | Create patrol `{name, color}` |
| PUT | `/api/admin/adhoc/patrols/{id}` | Update patrol `{name, color}` |
| DELETE | `/api/admin/adhoc/patrols/{id}` | Delete patrol |
| POST | `/api/admin/adhoc/patrols/reset` | Reset all scores to 0 |

Validation:
- Name: non-empty, max 50 chars, trimmed
- Color: must be in existing `validColorNames` set from `admin_api.go`
- Max 20 patrols per user

**Files:** new `internal/handlers/admin_adhoc.go`, `internal/server/server.go`

---

## Phase 3: Alternative Score Read Path

- [ ] Add ad-hoc branch in `PatrolScoreService.GetPatrolScores()` (device API)
- [ ] Add ad-hoc branch in `AdminScoresHandler` GET (admin API)
- [ ] Write tests for both paths

### 3a. Device API — `GET /api/v1/patrols`

In `PatrolScoreService.GetPatrolScores()` (`internal/services/patrol_score_service.go`), branch early:

```go
if *device.SectionID == 0 {
    return s.getAdhocPatrolScores(ctx, device)
}
```

`getAdhocPatrolScores`:
- Reads from `adhocpatrol.ListByUser(conns, device.OsmUserID)`
- Redis cache with **15-second TTL** (key: `adhoc_scores:{userId}`)
- Returns `PatrolScoreResponse` with `RateLimitState: NONE`
- Builds patrol colors directly from `AdhocPatrol.Color` field (no `section_settings` lookup needed)
- No term management, no OSM API calls

### 3b. Admin API — `GET /api/admin/sections/0/scores`

In `AdminScoresHandler` (`internal/handlers/admin_api.go`), intercept when `sectionID == 0`:

- Read from `adhocpatrol.ListByUser()`
- Return same `AdminScoresResponse` shape with `TermID: 0`, section name "Ad-hoc Teams"
- No OSM profile validation needed

**Files:** `internal/services/patrol_score_service.go`, `internal/handlers/admin_api.go`

---

## Phase 4: Alternative Score Write Path

- [ ] Add ad-hoc branch in `AdminScoresHandler` POST
- [ ] Add ad-hoc branch in `AdminSettingsHandler` GET/PUT
- [ ] Write tests

### Admin API — `POST /api/admin/sections/0/scores`

In `AdminScoresHandler`, intercept when `sectionID == 0` before calling `ScoreUpdateService`:

New function `handleUpdateAdhocScores` in `internal/handlers/admin_api.go` (or `admin_adhoc.go`):
- Parse same `AdminUpdateRequest` format (patrol ID + delta)
- For each update: read current score from DB, apply delta, write new score
- **No Redis locks** (single user owns the data)
- **No rate limiting, no OSM API calls**
- Still create `ScoreAuditLog` entries with `SectionID = 0`
- Invalidate Redis cache key `adhoc_scores:{userId}`

### Admin Settings — `GET/PUT /api/admin/sections/0/settings`

Intercept in `AdminSettingsHandler`:
- GET: return patrol list + colors from `adhocpatrol.ListByUser()` (colors live on patrol records)
- PUT: update patrol colors via `adhocpatrol.Update()` for each changed patrol

**Files:** `internal/handlers/admin_api.go` (or `admin_adhoc.go`)

---

## Phase 5: Scoreboard Configuration

- [ ] Prepend ad-hoc section in `AdminSectionsHandler`
- [ ] Create `internal/handlers/admin_scoreboards.go`
- [ ] Add `FindByUser` and `UpdateSectionID` to device code store
- [ ] Register routes
- [ ] Write tests

### 5a. Include ad-hoc section in sections list

In `AdminSectionsHandler` (`internal/handlers/admin_api.go`), prepend the ad-hoc section:

```go
sections := []AdminSection{{ID: 0, Name: "Ad-hoc Teams", GroupName: "Local"}}
// append OSM sections...
```

This makes it selectable in the existing `SectionSelector` dropdown with no frontend changes to that component.

### 5b. New endpoints for scoreboard management

New handler: `internal/handlers/admin_scoreboards.go`

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/admin/scoreboards` | List user's authorized devices |
| PUT | `/api/admin/scoreboards/{deviceCode}/section` | Change device's display section |

**GET /api/admin/scoreboards** returns:
- Device code prefix (first 8 chars for display)
- Current section ID + name
- Last used timestamp
- Client ID / comment

**PUT .../section** with `{"sectionId": 0}`:
- Validate device belongs to user (`osm_user_id` match)
- If `sectionId > 0`: validate user has access via OSM profile
- If `sectionId == 0`: always allowed (user's own ad-hoc section)
- Update `device_codes.section_id`
- Clear Redis cache for that device (`patrol_scores:{deviceCode}`)
- Clear term info fields (nil out `term_id`, `term_checked_at`, `term_end_date`)

### 5c. New store functions in `internal/db/devicecode/store.go`

```
FindByUser(conns, osmUserID) → []DeviceCode    // status='authorized'
UpdateSectionID(conns, deviceCode, sectionID) → error
```

**Files:** new `internal/handlers/admin_scoreboards.go`, `internal/db/devicecode/store.go`, `internal/server/server.go`

---

## Phase 6: Frontend UI

- [ ] Verify ad-hoc section appears in section selector (should be free from Phase 5a)
- [ ] Add "Teams" tab in `App.tsx` (conditional on section 0)
- [ ] Create teams management components
- [ ] Create `teamsSlice.ts` Redux slice
- [ ] Add ad-hoc API methods to `server.ts`
- [ ] Create scoreboard settings component
- [ ] Verify service worker compatibility

### 6a. Ad-hoc section appears in section selector (free)

The API change in Phase 5a means the existing `SectionSelector` shows "Ad-hoc Teams" automatically. Selecting it loads scores via the existing `GET /api/admin/sections/0/scores` path.

### 6b. New "Teams" tab (only when ad-hoc section selected)

In `App.tsx`, add a conditional tab:

```tsx
type Tab = 'scores' | 'settings' | 'teams';
// "Teams" tab visible only when selectedSectionId === 0
```

New components in `web/admin/src/ui/components/teams/`:
- `TeamsPage.tsx` — main container
- `TeamList.tsx` — list of teams with inline editing
- `TeamRow.tsx` — name input, color picker (reuse existing color palette), delete button
- `AddTeamButton.tsx` — creates new team
- `ResetScoresButton.tsx` — with confirmation dialog

### 6c. New Redux slice: `teamsSlice.ts`

- Entity adapter keyed by patrol ID string
- Async thunks: `fetchTeams`, `createTeam`, `updateTeam`, `deleteTeam`, `resetScores`
- Loading/error/saving states

### 6d. New API methods in `worker/server/server.ts`

```typescript
fetchAdhocPatrols(): Promise<AdhocPatrol[]>
createAdhocPatrol(name, color): Promise<AdhocPatrol>
updateAdhocPatrol(id, name, color): Promise<AdhocPatrol>
deleteAdhocPatrol(id): Promise<void>
resetAdhocScores(): Promise<void>
```

### 6e. Scoreboard settings section

New component `ScoreboardSettings.tsx` within the Settings page:
- Table of user's authorized devices
- Section dropdown per device (includes "Ad-hoc Teams")
- Calls `PUT /api/admin/scoreboards/{deviceCode}/section`

New API methods:
```typescript
fetchScoreboards(): Promise<Scoreboard[]>
updateScoreboardSection(deviceCode, sectionId): Promise<void>
```

### 6f. Service worker / offline

The existing offline sync and service worker flow works without changes because:
- API shape is identical for section 0 (`/api/admin/sections/0/scores`)
- IndexedDB keying by `(userId, sectionId)` naturally handles section 0
- Ad-hoc updates won't get rate limited, but the retry logic is harmless to keep

**Files:** `web/admin/src/ui/App.tsx`, new `web/admin/src/ui/components/teams/`, new `web/admin/src/ui/state/teamsSlice.ts`, `web/admin/src/worker/server/server.ts`

---

## Phase 7: Mock Server Updates

- [ ] Add ad-hoc patrol CRUD endpoints with in-memory storage
- [ ] Add section 0 to sections list response
- [ ] Add scoreboard listing/section switching endpoints

Update admin mock server (`cmd/mock-admin-server/`) to support the new endpoints. This enables `make dev` to work for frontend development of the new features.

---

## Implementation Order

1. **Phase 1** — DB schema + store (no dependencies)
2. **Phase 2** — CRUD API (depends on 1)
3. **Phase 3** — Score read branching (depends on 1, parallel with 2)
4. **Phase 4** — Score write branching (depends on 1, parallel with 2-3)
5. **Phase 5** — Scoreboard config (depends on 1, independent of 2-4)
6. **Phase 6a-6b** — Frontend teams tab (depends on 2)
7. **Phase 6e** — Frontend scoreboard settings (depends on 5)
8. **Phase 7** — Mock server (parallel with frontend work)

Suggested PR grouping:
- **PR 1:** Phases 1-4 (backend: schema, CRUD, score read/write)
- **PR 2:** Phase 5 (backend: scoreboard config)
- **PR 3:** Phases 6-7 (frontend + mock server)

---

## Verification

1. **Unit tests**: Store CRUD, handler tests for each endpoint, patrol score service branching
2. **Mock server**: `make dev` exercises the full frontend with mock ad-hoc data
3. **Manual E2E**: Create ad-hoc patrols in admin UI → switch device to section 0 → verify device API returns ad-hoc scores at 15s intervals → update scores in admin → verify device sees changes
4. **Regression**: Existing OSM section flows must be unaffected (section_id > 0 paths unchanged)
