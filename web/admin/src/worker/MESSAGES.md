# Worker Messages Documentation

This document describes all messages exchanged between the service worker and clients, including request/response flows and state versioning.

## Message Flow Overview

The service worker uses a versioned state model where each state change increments a revision number. Clients compare revision numbers to detect and discard stale messages.

### Versioning Strategy

#### Section List Version (`sectionsListRevision`)
- **Stored in:** `UserMetadata` record (IndexedDB)
- **Incremented when:**
  - Sections added/removed/changed
  - Profile fetch error state changes
- **Included in:** `UserProfileMessage`, `SectionListChangeMessage`

#### Section Version (`uiRevision`)
- **Stored in:** `Section` record (IndexedDB)
- **Incremented when:**
  - Patrol list updated
  - Patrol scores change (pending or committed)
  - Section refresh error state changes
- **Included in:** `PatrolsChangeMessage`

#### UI Version Checking
Clients should:
1. Compare message version with last known version
2. If `message.version <= lastKnownVersion` → discard as stale
3. If `message.version > lastKnownVersion` → accept and update state

---

## 1. Login / Get Profile Flow

### Client Request
```typescript
{
  type: 'get-profile',
  requestId: string
}
```

### Worker Responses

#### Success Case

**UserProfileMessage** (sent to requesting client)
```typescript
{
  type: 'user-profile',
  requestId: string,              // Matches request
  userId: number,
  userName: string,
  sections: Section[],            // List of available sections
  sectionsListRevision: number,   // Global revision for section list
  lastError?: undefined,          // No error on success
  lastErrorTime?: undefined
}
```

**SectionListChangeMessage** (broadcast to all clients if section list changed)
```typescript
{
  type: 'section-list-change',
  sections: Section[],
  sectionsListRevision: number
}
```

#### Authentication Required

**AuthenticationRequiredMessage**
```typescript
{
  type: 'authentication-required',
  requestId: string,              // Matches request
  loginUrl: string                // URL to redirect to for OAuth login
}
```

Sent when:
- User is not authenticated
- Session fetch fails (no userId available yet)

#### Error Case (Server Unreachable)

**UserProfileMessage** (with error state and cached sections if available)
```typescript
{
  type: 'user-profile',
  requestId: string,
  userId: number,
  userName: string,
  sections: Section[],            // Cached sections (may be empty)
  sectionsListRevision: number,   // Incremented when error state changes
  lastError: string,              // Error message
  lastErrorTime: number           // Timestamp (milliseconds)
}
```

Sent when:
- `fetchSections()` fails but user is authenticated
- Error is stored in `UserMetadata` record
- Revision is bumped to notify clients of state change

#### Wrong User

**ClientIsWrongUserMessage**
```typescript
{
  type: 'wrong-user',
  requestedUserId: number,        // User ID the client expected
  currentUserId: number           // User ID actually logged in
}
```

Sent when:
- Client requests data for a different user than is currently logged in
- Can occur if user logs out and logs back in as different user while client is open

---

## 2. Get Latest Scores (Refresh) Flow

### Client Request
```typescript
{
  type: 'refresh',
  requestId: string,
  userId: number,
  sectionId: number
}
```

### Worker Responses

#### Success Case

**PatrolsChangeMessage**
```typescript
{
  type: 'patrols-change',
  requestId: string,              // Matches request
  userId: number,
  sectionId: number,
  scores: PatrolScore[],          // Fresh patrol list with scores
  uiRevision: number,             // Section revision (incremented)
  lastError?: undefined,          // Error cleared on success
  lastErrorTime?: undefined
}
```

`PatrolScore` structure:
```typescript
{
  id: string,                     // Patrol ID (string from OSM API)
  name: string,
  committedScore: number,         // Last known score from server
  pendingScore: number,           // Local changes not yet synced
  // Per-patrol error state (present when sync fails):
  retryAfter?: number,            // -1 = permanent error (user must acknowledge)
                                  //  0 = can retry now
                                  // >0 = retry after this timestamp (ms)
  errorMessage?: string           // Error message for this specific patrol
}
```

#### Error Case (Server Unreachable)

**PatrolsChangeMessage** (with error state and cached patrols if available)
```typescript
{
  type: 'patrols-change',
  requestId: string,
  userId: number,
  sectionId: number,
  scores: PatrolScore[],          // Cached patrols from previous fetch
  uiRevision: number,             // Incremented when error state changes
  lastError: string,              // Error message
  lastErrorTime: number           // Timestamp (milliseconds)
}
```

Sent when:
- `fetchScores()` fails but section exists in cache
- Error is stored in `Section` record
- Revision is bumped to notify clients of state change

#### Authentication Required / Wrong User

Same as Login flow.

---

## 3. Update Scores Flow

### Client Request
```typescript
{
  type: 'submit-scores',
  requestId: string,
  userId: number,
  sectionId: number,
  deltas: ScoreDelta[]            // Array of { patrolId: string, score: number }
}
```

### Worker Responses

#### Immediate Optimistic Update

**PatrolsChangeMessage** (broadcast to all clients immediately)
```typescript
{
  type: 'patrols-change',
  requestId: undefined,           // NO requestId - unsolicited broadcast
  userId: number,
  sectionId: number,
  scores: PatrolScore[],          // Updated with pending changes
  uiRevision: number,             // Incremented
  lastError?: string,             // Preserved from previous state
  lastErrorTime?: number
}
```

**Flow:**
1. Worker receives `submit-scores` request
2. Worker adds points to `pendingScore` field for each patrol
3. Worker bumps section `uiRevision`
4. Worker broadcasts updated state to all clients immediately
5. Worker then attempts to sync with server (if online and authenticated)

#### After Sync Completes

**PatrolsChangeMessage** (broadcast to all clients)
```typescript
{
  type: 'patrols-change',
  requestId: undefined,           // NO requestId - unsolicited broadcast
  userId: number,
  sectionId: number,
  scores: PatrolScore[],          // Committed scores + any remaining pending
  uiRevision: number,             // Incremented again
  lastError?: string,             // Section-level errors only
  lastErrorTime?: number
}
```

**Sync outcomes per patrol:**
- **Success:** `pendingScore` cleared, `committedScore` updated, error cleared
- **Temporary error:** `retryAfter` set to future timestamp, `errorMessage` set
- **Permanent error:** `retryAfter` set to -1, `errorMessage` set

**Note:** Individual patrol errors are stored in the `PatrolScore` objects themselves, not in section-level `lastError`.

#### Authentication Required / Wrong User

Same as Login flow (may occur after optimistic update if session expired).

---

## 4. Background Sync

Background sync is triggered automatically when:
- User comes back online
- Sync lock expires
- Service worker detects pending changes that need syncing

**No client request** - service worker initiates autonomously.

### Worker Response

**PatrolsChangeMessage** (broadcast to all clients)
```typescript
{
  type: 'patrols-change',
  requestId: undefined,           // NO requestId - unsolicited background update
  userId: number,
  sectionId: number,
  scores: PatrolScore[],          // Updated scores after sync
  uiRevision: number,             // Incremented
  lastError?: string,             // Section-level errors
  lastErrorTime?: number
}
```

**When background sync fires:**
- Polls `getPendingForSyncNow()` to find patrols ready for sync
- Acquires sync lock and submits batch to server
- Updates patrol states based on server response
- Broadcasts updated state to all clients

---

## Message Types Reference

### Client-to-Worker Messages

```typescript
type ClientMessage =
  | { type: 'get-profile', requestId: string }
  | { type: 'refresh', requestId: string, userId: number, sectionId: number }
  | { type: 'submit-scores', requestId: string, userId: number, sectionId: number, deltas: ScoreDelta[] }
```

### Worker-to-Client Messages

```typescript
type WorkerMessage =
  | AuthenticationRequiredMessage
  | ClientIsWrongUserMessage
  | UserProfileMessage
  | SectionListChangeMessage
  | PatrolsChangeMessage
```

### Complete Message Type Definitions

```typescript
type AuthenticationRequiredMessage = {
  type: 'authentication-required';
  requestId?: string;
  loginUrl: string;
}

type ClientIsWrongUserMessage = {
  type: 'wrong-user';
  requestedUserId: number;
  currentUserId: number;
}

type UserProfileMessage = {
  type: 'user-profile';
  requestId: string;
  userId: number;
  userName: string;
  sections: Section[];
  sectionsListRevision: number;
  lastError?: string;
  lastErrorTime?: number;
}

type SectionListChangeMessage = {
  type: 'section-list-change';
  sections: Section[];
  sectionsListRevision: number;
}

type PatrolsChangeMessage = {
  type: 'patrols-change';
  requestId?: string;
  userId: number;
  sectionId: number;
  scores: PatrolScore[];
  uiRevision: number;
  lastError?: string;
  lastErrorTime?: number;
}

type Section = {
  id: number;
  name: string;
  groupName: string;
}

type PatrolScore = {
  id: string;
  name: string;
  committedScore: number;
  pendingScore: number;
  retryAfter?: number;
  errorMessage?: string;
}

type ScoreDelta = {
  patrolId: string;
  score: number;
}
```

---

## Error Handling Summary

### Two Levels of Errors

#### 1. Section-Level Errors
Stored in `Section.lastError` / `Section.lastErrorTime`
- **When:** Section refresh fails (can't fetch patrol list)
- **Included in:** `PatrolsChangeMessage.lastError`
- **Routing:** Errors for a section go to the section's error reducer

#### 2. Profile-Level Errors
Stored in `UserMetadata.lastError` / `UserMetadata.lastErrorTime`
- **When:** Profile fetch or section list fetch fails
- **Included in:** `UserProfileMessage.lastError`
- **Routing:** Errors with no `sectionId` go to bootstrap error reducer

#### 3. Patrol-Level Errors
Stored in `Patrol.retryAfter` / `Patrol.errorMessage`
- **When:** Individual patrol score update fails during sync
- **Included in:** `PatrolScore` objects within `PatrolsChangeMessage.scores`
- **Not separate messages:** Patrol errors are part of patrol state

### Error Routing Rules

| Error Type | sectionId present? | Routes to |
|------------|-------------------|-----------|
| Profile/Section List | No | Bootstrap error reducer |
| Section Refresh | Yes | Section error reducer |
| Individual Patrol | N/A | Patrol state (not a message-level error) |

---

## Correlation IDs (`requestId`)

### When Present
- All responses to explicit client requests include the `requestId`
- Allows client to match responses to requests
- Used for request/response patterns

### When Absent
- Unsolicited broadcasts (e.g., after background sync)
- Section list change notifications (broadcast when another client's profile fetch detected changes)
- Optimistic updates after score submission

### Client Handling
Clients should:
- Match `requestId` to pending requests for synchronous UI updates
- Handle messages without `requestId` as asynchronous state updates
- Always check version numbers regardless of `requestId`
