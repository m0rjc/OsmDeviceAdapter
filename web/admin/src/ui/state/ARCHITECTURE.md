# UI State Management Architecture

## Overview

This application uses a **hybrid thunk + listener middleware** architecture for managing async worker communication and side effects. This combines the best of both imperative and reactive patterns.

## Core Principles

### 1. Dual-Purpose Error Identification

**Correlation IDs (requestId) - Request Lifecycle Management**

All worker requests include a unique `requestId` (UUID) that is echoed back in responses:

```typescript
// Request
worker.sendRefreshRequest(userId, sectionId) → returns requestId

// Response (success)
PatrolsChangeMessage { requestId?, userId, sectionId, scores }

// Response (error)
ServiceErrorMessage { requestId?, userId, sectionId?, patrolId?, function, error }
```

**Correlation ID Benefits:**
- Track request lifecycle (add to pending, remove from pending)
- Prevent duplicate requests (check pending requests before sending)
- Distinguish solicited (requestId present) vs unsolicited (requestId absent) messages
- Handle rapid UI changes gracefully (user clicks A → B → A)
- Logging/debugging - trace request/response pairs
- Future: timeouts and request cancellation

**Contextual Information (userId, sectionId, patrolId) - Error Routing**

Errors include context about what failed:

```typescript
ServiceErrorMessage {
  requestId?: string;       // Correlation for pending request cleanup
  userId: number;           // Mandatory - which user context
  sectionId?: number;       // Optional - which section failed
  patrolId?: string;        // Optional - which patrol failed (future)
  function: string;         // What operation failed
  error: string;            // Error message
}
```

**Context Benefits:**
- **Self-describing errors** - Don't need to look up pending requests to know what failed
- **Works for unsolicited errors** - Background sync errors have no requestId but still route correctly
- **Enables inline error display** - Can show error icon on specific patrol (future)
- **Resilient** - Works even if pending request tracking is cleared or missed

**The Pattern:**
1. Use **context (sectionId, patrolId) as PRIMARY** for error routing to UI
2. Use **correlation (requestId) as SECONDARY** for request lifecycle cleanup

### 2. Loading State Tracking

Each section tracks its patrol loading state:

```typescript
type PatrolsLoadingState = 'uninitialized' | 'loading' | 'ready' | 'error';
```

**State Transitions:**
- `uninitialized` → `loading`: User selects section / fetch triggered
- `loading` → `ready`: PatrolsChangeMessage received
- `loading` → `error`: ServiceErrorMessage received (no cached data)
- `error` → `loading`: User retries

**Note:** Worker may send both `PatrolsChangeMessage` (cached) and `ServiceErrorMessage` (refresh failed). This results in `ready` state + error dialog, which is correct behavior.

### 3. Pending Request Tracking

The `pendingRequestsSlice` tracks all in-flight worker requests:

```typescript
interface PendingRequest {
  requestId: string;
  type: 'get-profile' | 'refresh' | 'submit-scores';
  sectionId?: number;
  userId?: number;
  timestamp: number;
}
```

**Used for:**
- Correlating `ServiceErrorMessage` with the section that failed
- Preventing duplicate requests
- Future: timeouts (detect stale requests via timestamp)

## Hybrid Architecture Pattern

### Thunks (Imperative)

**Use for explicit user actions** where causality should be obvious.

```typescript
// User manually selects a section
dispatch(selectSectionWithPatrolFetch(sectionId))
```

**Advantages:**
- Clear, traceable flow: user action → thunk → worker call
- Explicit control over when side effects occur
- Easy to test (just test the thunk)
- No cascading effects

**Disadvantages:**
- Can be bypassed if someone dispatches `selectSection` directly
- Requires discipline ("use the thunk, not the action")

### Listeners (Reactive)

**Use as a safety net** for implicit state changes.

```typescript
// Listener auto-fetches when section is selected
listenerMiddleware.startListening({
  matcher: isAnyOf(setCanonicalSections, selectSection),
  effect: async (action, listenerApi) => {
    // Check guards: loading state, pending requests
    // Then: dispatch(fetchPatrolScores(sectionId))
  }
})
```

**Advantages:**
- Cannot be bypassed - always responds to state changes
- Declarative: "whenever X changes, do Y"
- Catches edge cases (e.g., `setCanonicalSections` auto-selecting)

**Disadvantages:**
- Can be harder to trace ("why did this API call happen?")
- Risk of cascading effects if not carefully guarded
- May trigger in unexpected situations

### Our Hybrid Approach

**Thunks for the happy path:**
- `selectSectionWithPatrolFetch(sectionId)` - user manually selects section
- `fetchPatrolScores(sectionId)` - explicit fetch/retry

**Listeners as safety nets:**
- Auto-fetch when `setCanonicalSections` selects first section on load
- Catch any edge cases where section changes through other means

**Guards prevent duplicate work:**
- Listener checks: already loaded? already loading? already pending request?
- Only triggers fetch if all guards pass

## Request/Response Flow

### 1. Sending a Request (fetchPatrolScores thunk)

```typescript
async (sectionId, { dispatch, getState }) => {
  // Generate correlation ID
  const requestId = worker.sendRefreshRequest(userId, sectionId);

  // Track pending request
  dispatch(addPendingRequest({
    requestId,
    type: 'refresh',
    sectionId,
    userId,
    timestamp: Date.now()
  }));

  // Set UI loading state
  dispatch(setPatrolsLoading({ sectionId }));
}
```

### 2. Receiving a Success Response (useWorkerBootstrap)

```typescript
case 'patrols-change':
  // If response includes requestId, remove from pending
  if (message.requestId) {
    const pendingRequest = state.pendingRequests.requests[message.requestId];
    if (pendingRequest) {
      dispatch(removePendingRequest(message.requestId));
    }
  }

  // Update patrol data (sets loading state to 'ready')
  dispatch(setCanonicalPatrols({ sectionId, patrols }));
  break;
```

### 3. Receiving an Error Response (useWorkerBootstrap)

```typescript
case 'service-error':
  // FIRST: Use correlation to clean up pending request (if present)
  if (message.requestId) {
    const pendingRequest = state.pendingRequests.requests[message.requestId];
    if (pendingRequest) {
      dispatch(removePendingRequest(message.requestId));
    }
  }

  // SECOND: Use context to route error to UI (PRIMARY routing mechanism)
  // This works for both solicited (with requestId) and unsolicited (background) errors
  if (message.sectionId) {
    dispatch(setPatrolsError({
      sectionId: message.sectionId,
      error: message.error
    }));
  }

  // TODO: If patrolId provided, set error on specific patrol (future enhancement)

  // FINALLY: Always show error dialog for user feedback
  dispatch(showErrorDialog({ title: message.function, message: message.error }));
  break;
```

### 4. Unsolicited Updates (Background Sync)

```typescript
case 'patrols-change':
  // No requestId = background sync or update from another client
  if (!message.requestId) {
    // Still update the data, just don't look for pending request
  }
  dispatch(setCanonicalPatrols({ sectionId, patrols }));
  break;
```

### 5. Why Both Correlation AND Context?

**Example 1: Rapid Section Changes**
```
User clicks: Section A → Section B → Section A
Requests sent: reqA1, reqB1, reqA2

If responses arrive: resB1, resA1, resA2
- Without correlation: Can't tell if resA1 or resA2 should clear "loading"
- With correlation: Each response matches its request precisely
```

**Example 2: Background Sync Error**
```
Background sync fails for Section 3, Patrol "Red"
Error has: { sectionId: 3, patrolId: "Red", NO requestId }

- Without context: Can't route error (no requestId to look up)
- With context: Can show error on Section 3, even inline on Patrol "Red"
```

**Example 3: Cached Data + Error**
```
User refreshes Section 2
Worker sends: PatrolsChangeMessage (cached) then ServiceErrorMessage
Both include: requestId="abc-123", sectionId=2

Flow:
1. PatrolsChangeMessage arrives → removes pending request, shows data
2. ServiceErrorMessage arrives → no pending request (already removed)
3. Context (sectionId=2) routes error → shows warning on Section 2
4. Result: User sees stale data + warning, rather than blank screen
```

## Error Display Strategy

We have two components for different error scenarios:

### ErrorDialog (Transient Errors)
- **Location:** `ui/components/ErrorDialog.tsx`
- **Use case:** Service errors from worker (e.g., "Failed to refresh scores")
- **Behavior:** Modal dialog, dismissable with "OK" button
- **Triggered by:** `ServiceErrorMessage` from worker

### MessageCard (Persistent Errors)
- **Location:** `ui/components/MessageCard.tsx`
- **Use case:** Persistent states that block UI (e.g., "No section selected", "Session mismatch")
- **Behavior:** Card in main content area, optional retry action
- **Triggered by:** Application state (no section selected, load failed with no cached data)

## Selectors

### Loading State Selectors

```typescript
// Get loading state for selected section
selectPatrolsLoadingStateForSelectedSection(state)
// → 'uninitialized' | 'loading' | 'ready' | 'error'

// Check if currently loading
selectIsPatrolsLoading(state) // → boolean

// Get error message if failed
selectPatrolsError(state) // → string | null

// Check if user can retry
selectCanRetryPatrolsLoad(state) // → boolean
```

### Pending Request Selectors

```typescript
// Check if refresh is pending for a section
selectHasPendingRefreshForSection(sectionId)(state) // → boolean

// Get specific pending request
selectPendingRequest(requestId)(state) // → PendingRequest | undefined
```

## Future Enhancements

### Request Timeout Handling

```typescript
// In a periodic effect or separate listener:
const staleRequests = Object.values(state.pendingRequests.requests)
  .filter(req => Date.now() - req.timestamp > 30000); // 30s timeout

staleRequests.forEach(req => {
  dispatch(removePendingRequest(req.requestId));
  dispatch(setPatrolsError({ sectionId: req.sectionId, error: 'Request timed out' }));
});
```

### Request Registry Pattern

As we add more request types, we may want a registry:

```typescript
interface RequestHandler {
  onSuccess: (response: WorkerMessage, pendingRequest: PendingRequest) => void;
  onError: (error: ServiceErrorMessage, pendingRequest: PendingRequest) => void;
  onTimeout: (pendingRequest: PendingRequest) => void;
}

const requestRegistry: Record<PendingRequestType, RequestHandler> = {
  'refresh': { onSuccess, onError, onTimeout },
  'submit-scores': { onSuccess, onError, onTimeout },
  // ...
};
```

This would centralize error handling logic and make it easier to add new request types.

## Best Practices

1. **Always use thunks for user actions** - Don't bypass them with direct action dispatch
2. **Use correlation IDs for all worker requests** - Required for request lifecycle management
3. **Always include context in errors** - userId (mandatory), sectionId/patrolId (when applicable)
4. **Route errors by context, not correlation** - Context works for both solicited and unsolicited errors
5. **Check loading state before fetching** - Prevent duplicate requests
6. **Handle both cached and error responses** - Worker may send both (cached data first, error second)
7. **Show transient errors in dialogs** - Use ErrorDialog for dismissable warnings
8. **Show persistent errors in cards** - Use MessageCard for blocking states
9. **Add comprehensive guards to listeners** - Prevent unwanted side effects
10. **Future: Enable inline patrol errors** - Use patrolId to show error icon on specific patrol

## Testing Strategy

### Unit Tests
- Test thunks in isolation (mock worker, check dispatched actions)
- Test reducers (ensure state transitions are correct)
- Test selectors (verify computed values)

### Integration Tests
- Test request/response flow with mock worker
- Verify correlation IDs are properly tracked
- Ensure loading states transition correctly

### E2E Tests
- Test real worker communication in browser
- Verify background sync updates work
- Test error recovery flows
